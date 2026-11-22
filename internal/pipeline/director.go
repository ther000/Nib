package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nib/internal/domain"
	"nib/internal/genres"
	"nib/internal/llm"
)

const (
	directorMaxSteps     = 8
	directorMaxTokens    = 4096
	directorMaxHistory   = 8
	directorMaxTextRunes = 4000
)

// DirectorStore 定义 AI 导演跨模块读写全书所需的仓储接口（由 *store.Store 自动满足）。
type DirectorStore interface {
	UpdateBook(ctx context.Context, in domain.UpdateBookInput) (domain.Book, error)
	ListVolumes(ctx context.Context, bookID int64) ([]domain.Volume, error)
	UpsertVolume(ctx context.Context, in domain.UpsertVolumeInput) (domain.Volume, error)
	ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error)
	CreateChapter(ctx context.Context, in domain.CreateChapterInput) (domain.Chapter, error)
	UpdateChapterMeta(ctx context.Context, in domain.UpdateChapterMetaInput) (domain.Chapter, error)
	SaveChapterDraft(ctx context.Context, in domain.SaveChapterDraftInput) (domain.Chapter, error)
	ListBeats(ctx context.Context, chapterID int64) ([]domain.Beat, error)
	ReplaceBeats(ctx context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error)
	ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error)
	UpsertEntity(ctx context.Context, in domain.UpsertEntityInput) (domain.Entity, error)
	ListForeshadowings(ctx context.Context, bookID int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error)
	UpsertForeshadowing(ctx context.Context, in domain.UpsertForeshadowingInput) (domain.Foreshadowing, error)
	SearchMemory(ctx context.Context, bookID int64, query string, maxChapterSeq int) ([]domain.MemorySearchHit, error)
}

// DirectorExtraStore 定义 AI 导演扩展能力所需的仓储接口（由 *store.Store 自动满足，测试替身可按需实现）。
type DirectorExtraStore interface {
	DeleteChapter(ctx context.Context, chapterID int64) error
	MoveChapter(ctx context.Context, in domain.MoveChapterInput) (domain.Chapter, error)
	ListChapterVersions(ctx context.Context, chapterID int64) ([]domain.ChapterVersion, error)
	ListEntityRelations(ctx context.Context, bookID int64) ([]domain.EntityRelation, error)
	UpsertEntityRelation(ctx context.Context, in domain.UpsertEntityRelationInput) (domain.EntityRelation, error)
}

// DirectorHistoryStore 定义 AI 导演对话持久化接口（由 *store.Store 自动满足）。
type DirectorHistoryStore interface {
	AppendDirectorMessage(ctx context.Context, bookID int64, msg domain.DirectorMessage) (domain.DirectorMessage, error)
}

// DirectorSafetyStore 定义 AI 导演写库前自动创建安全撤销点的接口（由 *store.Store 自动满足）。
type DirectorSafetyStore interface {
	CreateSafetySnapshot(ctx context.Context, bookID int64, source string, sourceRefID int64, summary string) (domain.SafetySnapshot, error)
}

type directorStepDecision struct {
	Thought   string `json:"thought"`
	Action    string `json:"action"` // "tool" | "final"
	ToolName  string `json:"tool_name"`
	ToolArgs  json.RawMessage `json:"tool_args"`
	Reply     string `json:"reply"`
}

// RunDirectorChat 执行一轮「全局 AI 导演」多步自主规划与工具调用循环。
func (r *Runner) RunDirectorChat(
	ctx context.Context,
	req domain.DirectorChatRequest,
	emit func(domain.DirectorEvent) error,
) (domain.DirectorReply, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.DirectorReply{}, errors.New("pipeline director dependencies not initialized")
	}
	ds, ok := r.store.(DirectorStore)
	if !ok {
		return domain.DirectorReply{}, errors.New("store does not implement DirectorStore")
	}
	if err := req.Validate(); err != nil {
		return domain.DirectorReply{}, err
	}

	send := func(ev domain.DirectorEvent) error {
		if emit == nil {
			return nil
		}
		return emit(ev)
	}

	book, err := r.store.GetBook(ctx, req.BookID)
	if err != nil {
		return domain.DirectorReply{}, fmt.Errorf("load book %d: %w", req.BookID, err)
	}

	// 优先使用 outline 路由做总控规划，未配置时回退 draft 路由。
	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StageOutline, domain.StageDraft)
	if err != nil {
		return domain.DirectorReply{}, err
	}

	if err := send(domain.DirectorEvent{Kind: domain.DirectorEventStarted, Message: "正在分析你的指令…"}); err != nil {
		return domain.DirectorReply{}, domain.ErrGenerationCanceled
	}

	overview, err := buildDirectorBookSnapshot(ctx, ds, book, req.ActiveChapterID)
	if err != nil {
		return domain.DirectorReply{}, err
	}

	sysPrompt := buildDirectorSystemPrompt(book, overview)
	messages := make([]llm.ChatMessage, 0, directorMaxHistory+directorMaxSteps*2+2)

	// 注入最近对话上下文
	hist := req.History
	if len(hist) > directorMaxHistory {
		hist = hist[len(hist)-directorMaxHistory:]
	}
	for _, h := range hist {
		role := "user"
		if h.Role == "assistant" {
			role = "assistant"
		}
		if strings.TrimSpace(h.Content) != "" {
			messages = append(messages, llm.ChatMessage{Role: role, Content: strings.TrimSpace(h.Content)})
		}
	}
	messages = append(messages, llm.ChatMessage{Role: "user", Content: strings.TrimSpace(req.Instruction)})

	if hs, ok := r.store.(DirectorHistoryStore); ok {
		_, _ = hs.AppendDirectorMessage(ctx, book.ID, domain.DirectorMessage{
			Role:    "user",
			Content: strings.TrimSpace(req.Instruction),
		})
	}

	toolDefs := directorToolDefinitions()
	toolCalls := make([]domain.DirectorToolCall, 0)
	mutated := false
	var turnUndoSnapshotID int64
	finalReply := ""

	ensureTurnSafetySnapshot := func(toolName string) {
		if turnUndoSnapshotID > 0 || !isDirectorMutatingTool(toolName) {
			return
		}
		if ss, ok := r.store.(DirectorSafetyStore); ok {
			if snap, snapErr := ss.CreateSafetySnapshot(
				ctx,
				book.ID,
				"director_turn",
				0,
				summarizeDirectorText("AI 导演："+strings.TrimSpace(req.Instruction), 80),
			); snapErr == nil && snap.ID > 0 {
				turnUndoSnapshotID = snap.ID
			}
		}
	}

	for step := 1; step <= directorMaxSteps; step++ {
		if ctx.Err() != nil {
			return domain.DirectorReply{}, domain.ErrGenerationCanceled
		}
		_ = send(domain.DirectorEvent{
			Kind:    domain.DirectorEventThinking,
			Step:    step,
			Message: fmt.Sprintf("第 %d 步：规划操作中…", step),
		})

		maxTok := route.MaxTokens
		if maxTok < directorMaxTokens {
			maxTok = directorMaxTokens
		}
		chatReq := llm.ChatRequest{
			Model:       route.ModelName,
			System:      sysPrompt,
			Messages:    messages,
			Tools:       toolDefs,
			Temperature: 0.4,
			MaxTokens:   maxTok,
		}

		start := time.Now()
		var buf strings.Builder
		var nativeCalls []llm.ToolCall
		var usage domain.TokenUsage
		usedProv, usedModel, streamUsage, streamErr := r.streamWithFallback(ctx, route, provider, chatReq, func(ev llm.StreamEvent) error {
			if ev.Delta != "" {
				buf.WriteString(ev.Delta)
			}
			if len(ev.ToolCalls) > 0 {
				nativeCalls = append(nativeCalls, ev.ToolCalls...)
			}
			if ev.Usage != nil {
				usage = *ev.Usage
			}
			return nil
		})
		if hasUsage(streamUsage) {
			usage = streamUsage
		}
		if streamErr != nil {
			_ = send(domain.DirectorEvent{Kind: domain.DirectorEventError, Error: streamErr.Error()})
			return domain.DirectorReply{}, streamErr
		}
		if hasUsage(usage) {
			r.recordTokenUsage(ctx, book.ID, req.ActiveChapterID, domain.StageOutline, usedProv.Name, usedModel, usage, time.Since(start).Milliseconds())
		}

		raw := strings.TrimSpace(buf.String())

		// 优先路径：模型返回了原生 Function Calling ToolCalls
		if len(nativeCalls) > 0 {
			messages = append(messages, llm.ChatMessage{
				Role:      "assistant",
				Content:   raw,
				ToolCalls: nativeCalls,
			})
			for idx, ntc := range nativeCalls {
				callID := ntc.ID
				if callID == "" {
					callID = fmt.Sprintf("tc_%d_%d_%d", step, idx, time.Now().UnixMilli())
				}
				argsRaw := json.RawMessage(strings.TrimSpace(ntc.Arguments))
				if len(argsRaw) == 0 {
					argsRaw = json.RawMessage("{}")
				}
				tc := domain.DirectorToolCall{
					ID:       callID,
					Name:     strings.TrimSpace(ntc.Name),
					Label:    directorToolLabel(strings.TrimSpace(ntc.Name)),
					ArgsJSON: string(argsRaw),
					Status:   "running",
				}
				tcCopy := tc
				if err := send(domain.DirectorEvent{
					Kind:     domain.DirectorEventToolStart,
					Step:     step,
					ToolCall: &tcCopy,
					Message:  tc.Label + "…",
				}); err != nil {
					return domain.DirectorReply{}, domain.ErrGenerationCanceled
				}

				ensureTurnSafetySnapshot(tc.Name)
				resultText, didMutate, undoChapID, undoVerID, toolErr := r.executeDirectorTool(ctx, ds, book.ID, tc.Name, argsRaw)
				if didMutate {
					mutated = true
				}
				tc.UndoChapterID = undoChapID
				tc.UndoVersionID = undoVerID
				if toolErr != nil {
					tc.Status = "error"
					tc.ResultSummary = toolErr.Error()
					resultText = "工具执行失败：" + toolErr.Error()
				} else {
					tc.Status = "ok"
					tc.ResultSummary = summarizeDirectorText(resultText, 120)
				}
				toolCalls = append(toolCalls, tc)
				tcDone := tc
				_ = send(domain.DirectorEvent{
					Kind:     domain.DirectorEventToolResult,
					Step:     step,
					ToolCall: &tcDone,
					Mutated:  mutated,
				})

				messages = append(messages, llm.ChatMessage{
					Role:       "tool",
					ToolCallID: callID,
					ToolName:   tc.Name,
					Content:    resultText,
				})
			}
			continue
		}

		// 兼容兜底路径：部分旧中转站忽略 tools 参数而输出 JSON 指令或直接输出自然语言回复
		dec, parseErr := parseDirectorDecision(raw)
		if parseErr != nil {
			finalReply = raw
			break
		}

		if dec.Action == "final" || strings.TrimSpace(dec.ToolName) == "" {
			finalReply = strings.TrimSpace(dec.Reply)
			if finalReply == "" {
				finalReply = strings.TrimSpace(dec.Thought)
			}
			if finalReply == "" {
				finalReply = "已按要求完成处理。"
			}
			break
		}

		// 执行文本协议解析出的工具
		callID := fmt.Sprintf("tc_%d_%d", step, time.Now().UnixMilli())
		tc := domain.DirectorToolCall{
			ID:       callID,
			Name:     strings.TrimSpace(dec.ToolName),
			Label:    directorToolLabel(strings.TrimSpace(dec.ToolName)),
			ArgsJSON: string(dec.ToolArgs),
			Status:   "running",
		}
		tcCopy := tc
		if err := send(domain.DirectorEvent{
			Kind:     domain.DirectorEventToolStart,
			Step:     step,
			ToolCall: &tcCopy,
			Message:  tc.Label + "…",
		}); err != nil {
			return domain.DirectorReply{}, domain.ErrGenerationCanceled
		}

		ensureTurnSafetySnapshot(tc.Name)
		resultText, didMutate, undoChapID, undoVerID, toolErr := r.executeDirectorTool(ctx, ds, book.ID, tc.Name, dec.ToolArgs)
		if didMutate {
			mutated = true
		}
		tc.UndoChapterID = undoChapID
		tc.UndoVersionID = undoVerID
		if toolErr != nil {
			tc.Status = "error"
			tc.ResultSummary = toolErr.Error()
			resultText = "工具执行失败：" + toolErr.Error()
		} else {
			tc.Status = "ok"
			tc.ResultSummary = summarizeDirectorText(resultText, 120)
		}
		toolCalls = append(toolCalls, tc)
		tcDone := tc
		_ = send(domain.DirectorEvent{
			Kind:     domain.DirectorEventToolResult,
			Step:     step,
			ToolCall: &tcDone,
			Mutated:  mutated,
		})

		messages = append(messages,
			llm.ChatMessage{Role: "assistant", Content: raw},
			llm.ChatMessage{Role: "user", Content: fmt.Sprintf("【工具 %s 执行结果】\n%s\n\n如果任务已完成，请直接用简洁中文向作者汇报结果；如还需操作，请继续调用工具。", tc.Name, resultText)},
		)
	}

	if strings.TrimSpace(finalReply) == "" {
		finalReply = "已完成多步操作。如需继续调整，直接告诉我下一步想法。"
	}

	var effectiveUndoSnapshotID int64
	if mutated {
		effectiveUndoSnapshotID = turnUndoSnapshotID
	}

	if hs, ok := r.store.(DirectorHistoryStore); ok {
		_, _ = hs.AppendDirectorMessage(ctx, book.ID, domain.DirectorMessage{
			Role:           "assistant",
			Content:        finalReply,
			ToolCalls:      toolCalls,
			UndoSnapshotID: effectiveUndoSnapshotID,
		})
	}

	_ = send(domain.DirectorEvent{
		Kind:           domain.DirectorEventCompleted,
		Reply:          finalReply,
		ToolCalls:      toolCalls,
		Mutated:        mutated,
		UndoSnapshotID: effectiveUndoSnapshotID,
	})
	return domain.DirectorReply{
		Reply:          finalReply,
		ToolCalls:      toolCalls,
		Mutated:        mutated,
		UndoSnapshotID: effectiveUndoSnapshotID,
	}, nil
}

func (r *Runner) executeDirectorTool(
	ctx context.Context,
	ds DirectorStore,
	bookID int64,
	name string,
	rawArgs json.RawMessage,
) (string, bool, int64, int64, error) {
	extra, _ := r.store.(DirectorExtraStore)
	latestUndoVersion := func(chapID int64) int64 {
		if extra == nil || chapID <= 0 {
			return 0
		}
		if vers, err := extra.ListChapterVersions(ctx, chapID); err == nil && len(vers) > 0 {
			return vers[0].ID
		}
		return 0
	}

	switch name {
	case "list_chapters_range":
		var args struct {
			StartSeq int `json:"start_seq"`
			EndSeq   int `json:"end_seq"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, 0, 0, err
		}
		chaps, err := ds.ListBookChapters(ctx, bookID)
		if err != nil {
			return "", false, 0, 0, err
		}
		var b strings.Builder
		matched := 0
		for _, ch := range chaps {
			if args.StartSeq > 0 && ch.GlobalSeq < args.StartSeq {
				continue
			}
			if args.EndSeq > 0 && ch.GlobalSeq > args.EndSeq {
				continue
			}
			fmt.Fprintf(&b, "- 第%d章《%s》(chapter_id=%d, status=%s, %d字) %s\n",
				ch.GlobalSeq, ch.Title, ch.ID, ch.Status, ch.WordCount, summarizeDirectorText(ch.OutlineSummary, 60))
			matched++
			if matched >= 50 {
				b.WriteString("（结果超过 50 章已截断，请缩小起止章号范围）\n")
				break
			}
		}
		if matched == 0 {
			return "该章号范围内暂无章节。", false, 0, 0, nil
		}
		return b.String(), false, 0, 0, nil

	case "get_entity_detail":
		var args struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Name) == "" {
			return "", false, 0, 0, errors.New("缺少实体名称 name")
		}
		q := strings.TrimSpace(args.Name)
		ents, err := ds.ListEntities(ctx, bookID, domain.EntityFilter{Query: q})
		if err != nil {
			return "", false, 0, 0, err
		}
		if len(ents) == 0 {
			return fmt.Sprintf("未找到名称包含「%s」的设定实体。", q), false, 0, 0, nil
		}
		var rels []domain.EntityRelation
		if extra != nil {
			rels, _ = extra.ListEntityRelations(ctx, bookID)
		}
		var b strings.Builder
		for _, e := range ents[:min(len(ents), 5)] {
			fmt.Fprintf(&b, "【%s】%s (id=%d, 铁律=%v)\n", e.Kind.Label(), e.Name, e.ID, e.IsInvariant)
			if len(e.Aliases) > 0 {
				fmt.Fprintf(&b, "别名：%s\n", strings.Join(e.Aliases, "、"))
			}
			if e.Summary != "" {
				fmt.Fprintf(&b, "简介：%s\n", e.Summary)
			}
			for k, v := range e.Attributes {
				fmt.Fprintf(&b, "  - %s: %s\n", k, v)
			}
			for _, rel := range rels {
				if rel.SourceEntityID == e.ID || rel.TargetEntityID == e.ID {
					fmt.Fprintf(&b, "  关系：%s —[%s]→ %s (%s)\n", rel.SourceName, rel.RelationType, rel.TargetName, rel.Description)
				}
			}
			b.WriteByte('\n')
		}
		return b.String(), false, 0, 0, nil

	case "delete_chapter":
		if extra == nil {
			return "", false, 0, 0, errors.New("当前存储不支持删除章节")
		}
		var args struct {
			ChapterID  int64 `json:"chapter_id"`
			ChapterSeq int   `json:"chapter_seq"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, 0, 0, err
		}
		chap, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
		if err != nil {
			return "", false, 0, 0, err
		}
		if err := extra.DeleteChapter(ctx, chap.ID); err != nil {
			return "", false, 0, 0, err
		}
		return fmt.Sprintf("已删除第 %d 章《%s》并自动重排剩余章序。", chap.GlobalSeq, chap.Title), true, 0, 0, nil

	case "move_chapter":
		if extra == nil {
			return "", false, 0, 0, errors.New("当前存储不支持调整章序")
		}
		var args struct {
			ChapterID      int64  `json:"chapter_id"`
			ChapterSeq     int    `json:"chapter_seq"`
			Direction      string `json:"direction"`
			TargetVolumeID int64  `json:"target_volume_id"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, 0, 0, err
		}
		chap, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
		if err != nil {
			return "", false, 0, 0, err
		}
		moved, err := extra.MoveChapter(ctx, domain.MoveChapterInput{
			ChapterID:      chap.ID,
			Direction:      args.Direction,
			TargetVolumeID: args.TargetVolumeID,
		})
		if err != nil {
			return "", false, 0, 0, err
		}
		return fmt.Sprintf("已调整《%s》顺序，当前为第 %d 章。", moved.Title, moved.GlobalSeq), true, 0, 0, nil

	case "generate_arc_summary":
		var args struct {
			Level    string `json:"level"`
			VolumeID int64  `json:"volume_id"`
			StartSeq int    `json:"start_seq"`
			EndSeq   int    `json:"end_seq"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, 0, 0, err
		}
		lvl := domain.ArcSummaryLevel(strings.TrimSpace(args.Level))
		if !lvl.Valid() {
			lvl = domain.ArcSummaryLevelArc10
		}
		var volPtr *int64
		if args.VolumeID > 0 {
			v := args.VolumeID
			volPtr = &v
		}
		arc, err := r.GenerateArcSummary(ctx, domain.GenerateArcSummaryRequest{
			BookID:   bookID,
			Level:    lvl,
			VolumeID: volPtr,
			StartSeq: args.StartSeq,
			EndSeq:   args.EndSeq,
		})
		if err != nil {
			return "", false, 0, 0, err
		}
		return fmt.Sprintf("已生成第 %d~%d 章摘要：%s", arc.StartSeq, arc.EndSeq, summarizeDirectorText(arc.Summary, 100)), true, 0, 0, nil

	case "upsert_entity_relation":
		if extra == nil {
			return "", false, 0, 0, errors.New("当前存储不支持实体关系管理")
		}
		var args struct {
			SourceName   string `json:"source_name"`
			TargetName   string `json:"target_name"`
			RelationType string `json:"relation_type"`
			Description  string `json:"description"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, 0, 0, err
		}
		ents, err := ds.ListEntities(ctx, bookID, domain.EntityFilter{})
		if err != nil {
			return "", false, 0, 0, err
		}
		findID := func(name string) int64 {
			t := strings.TrimSpace(name)
			for _, e := range ents {
				if strings.EqualFold(strings.TrimSpace(e.Name), t) {
					return e.ID
				}
			}
			return 0
		}
		srcID := findID(args.SourceName)
		dstID := findID(args.TargetName)
		if srcID == 0 || dstID == 0 {
			return "", false, 0, 0, fmt.Errorf("未在设定库中找到实体「%s」或「%s」，请先使用 upsert_entity 创建", args.SourceName, args.TargetName)
		}
		saved, err := extra.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
			BookID:         bookID,
			SourceEntityID: srcID,
			TargetEntityID: dstID,
			RelationType:   args.RelationType,
			Description:    args.Description,
		})
		if err != nil {
			return "", false, 0, 0, err
		}
		return fmt.Sprintf("已记录关系：%s —[%s]→ %s。", saved.SourceName, saved.RelationType, saved.TargetName), true, 0, 0, nil

	case "replace_chapter_paragraphs":
		var args struct {
			ChapterID      int64  `json:"chapter_id"`
			ChapterSeq     int    `json:"chapter_seq"`
			StartParagraph int    `json:"start_paragraph"` // 1-based inclusive
			EndParagraph   int    `json:"end_paragraph"`   // 1-based inclusive
			NewText        string `json:"new_text"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, 0, 0, err
		}
		chap, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
		if err != nil {
			return "", false, 0, 0, err
		}
		rawNorm := strings.ReplaceAll(chap.Content, "\r\n", "\n")
		paras := make([]string, 0)
		for _, p := range strings.Split(rawNorm, "\n") {
			if strings.TrimSpace(p) != "" {
				paras = append(paras, strings.TrimSpace(p))
			}
		}
		if len(paras) == 0 {
			return "", false, 0, 0, errors.New("当前章节暂无正文段落可替换")
		}
		sp := args.StartParagraph
		ep := args.EndParagraph
		if sp <= 0 {
			sp = 1
		}
		if ep <= 0 || ep < sp {
			ep = sp
		}
		if sp > len(paras) {
			return "", false, 0, 0, fmt.Errorf("起始段落 %d 超出章节总段数 %d", sp, len(paras))
		}
		if ep > len(paras) {
			ep = len(paras)
		}
		newParas := make([]string, 0, len(paras))
		newParas = append(newParas, paras[:sp-1]...)
		for _, np := range strings.Split(strings.ReplaceAll(strings.TrimSpace(args.NewText), "\r\n", "\n"), "\n") {
			if strings.TrimSpace(np) != "" {
				newParas = append(newParas, strings.TrimSpace(np))
			}
		}
		newParas = append(newParas, paras[ep:]...)
		merged := strings.Join(newParas, "\n\n")
		saved, err := ds.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
			ChapterID: chap.ID,
			Content:   merged,
			Status:    domain.ChapterStatusReviewing,
		})
		if err != nil {
			return "", false, 0, 0, err
		}
		undoVer := latestUndoVersion(chap.ID)
		return fmt.Sprintf("已局部修改第 %d 章《%s》第 %d~%d 段（当前共 %d 字，修改前已自动归档快照）。",
			saved.GlobalSeq, saved.Title, sp, ep, saved.WordCount), true, chap.ID, undoVer, nil

	default:
		res, mut, err := r.executeDirectorCoreTool(ctx, ds, bookID, name, rawArgs)
		if err != nil || !mut {
			return res, mut, 0, 0, err
		}
		// 若是修改正文的核心工具，解析章号并回传可一键撤回的快照 ID
		if name == "write_chapter_content" || name == "generate_chapter" || name == "audit_and_polish_chapter" {
			var cidArgs struct {
				ChapterID  int64 `json:"chapter_id"`
				ChapterSeq int   `json:"chapter_seq"`
			}
			_ = json.Unmarshal(rawArgs, &cidArgs)
			if ch, cErr := resolveDirectorChapter(ctx, r.store, ds, bookID, cidArgs.ChapterID, cidArgs.ChapterSeq); cErr == nil {
				return res, mut, ch.ID, latestUndoVersion(ch.ID), nil
			}
		}
		return res, mut, 0, 0, nil
	}
}

func (r *Runner) executeDirectorCoreTool(
	ctx context.Context,
	ds DirectorStore,
	bookID int64,
	name string,
	rawArgs json.RawMessage,
) (string, bool, error) {
	switch name {
	case "get_book_overview":
		book, err := r.store.GetBook(ctx, bookID)
		if err != nil {
			return "", false, err
		}
		snap, err := buildDirectorBookSnapshot(ctx, ds, book, 0)
		return snap, false, err

	case "read_chapter":
		var args struct {
			ChapterID  int64 `json:"chapter_id"`
			ChapterSeq int   `json:"chapter_seq"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, err
		}
		chap, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
		if err != nil {
			return "", false, err
		}
		beats, _ := ds.ListBeats(ctx, chap.ID)
		var b strings.Builder
		fmt.Fprintf(&b, "第%d章《%s》(chapter_id=%d, status=%s, 字数=%d, 情绪=%s, 钩子=%s/%s)\n",
			chap.GlobalSeq, chap.Title, chap.ID, chap.Status, chap.WordCount, chap.EmotionTag, chap.HookType, chap.HookNote)
		if strings.TrimSpace(chap.OutlineSummary) != "" {
			fmt.Fprintf(&b, "章纲：%s\n", chap.OutlineSummary)
		}
		if len(beats) > 0 {
			b.WriteString("节拍：\n")
			for _, bt := range beats {
				fmt.Fprintf(&b, "  %d. [%s] %s（冲突：%s）\n", bt.Seq, bt.Stage, bt.Summary, bt.ConflictNote)
			}
		}
		if strings.TrimSpace(chap.Content) != "" {
			fmt.Fprintf(&b, "\n正文：\n%s\n", summarizeDirectorText(chap.Content, directorMaxTextRunes))
		} else {
			b.WriteString("\n正文：（尚未撰写）\n")
		}
		return b.String(), false, nil

	case "search_memory":
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil || strings.TrimSpace(args.Query) == "" {
			return "", false, errors.New("缺少 query 关键词")
		}
		hits, err := ds.SearchMemory(ctx, bookID, strings.TrimSpace(args.Query), 0)
		if err != nil {
			return "", false, err
		}
		if len(hits) == 0 {
			return "未检索到相关记录。", false, nil
		}
		var b strings.Builder
		limit := min(len(hits), 10)
		for i := 0; i < limit; i++ {
			h := hits[i]
			fmt.Fprintf(&b, "- [%s] %s：%s\n", h.Source, h.Title, summarizeDirectorText(h.Snippet, 120))
		}
		return b.String(), false, nil

	case "update_book":
		var args struct {
			Title              string `json:"title"`
			Premise            string `json:"premise"`
			GoldenFinger       string `json:"golden_finger"`
			TargetChapterWords int    `json:"target_chapter_words"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, err
		}
		cur, err := r.store.GetBook(ctx, bookID)
		if err != nil {
			return "", false, err
		}
		in := domain.UpdateBookInput{
			ID:                 cur.ID,
			Title:              cur.Title,
			Genre:              cur.Genre,
			Premise:            cur.Premise,
			GoldenFinger:       cur.GoldenFinger,
			TargetChapterWords: cur.TargetChapterWords,
			ExportDir:          cur.ExportDir,
			POVMode:            cur.POVMode,
			PacingStyle:        cur.PacingStyle,
			WritingPrompt:      cur.WritingPrompt,
			DailyWordGoal:      cur.DailyWordGoal,
		}
		if strings.TrimSpace(args.Title) != "" {
			in.Title = strings.TrimSpace(args.Title)
		}
		if strings.TrimSpace(args.Premise) != "" {
			in.Premise = strings.TrimSpace(args.Premise)
		}
		if strings.TrimSpace(args.GoldenFinger) != "" {
			in.GoldenFinger = strings.TrimSpace(args.GoldenFinger)
		}
		if args.TargetChapterWords >= domain.MinTargetChapterWords && args.TargetChapterWords <= domain.MaxTargetChapterWords {
			in.TargetChapterWords = args.TargetChapterWords
		}
		updated, err := ds.UpdateBook(ctx, in)
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("已更新书籍《%s》的核心设定。", updated.Title), true, nil

	case "upsert_entity":
		var args struct {
			ID          int64             `json:"id"`
			Kind        string            `json:"kind"`
			Name        string            `json:"name"`
			Aliases     []string          `json:"aliases"`
			IsInvariant bool              `json:"is_invariant"`
			Summary     string            `json:"summary"`
			Attributes  map[string]string `json:"attributes"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, err
		}
		name := strings.TrimSpace(args.Name)
		if name == "" {
			return "", false, errors.New("缺少实体名称 name")
		}
		// 若未传 id，按名称匹配已有实体避免重复创建
		var existing *domain.Entity
		if args.ID == 0 {
			if ents, eErr := ds.ListEntities(ctx, bookID, domain.EntityFilter{}); eErr == nil {
				for i := range ents {
					if strings.EqualFold(strings.TrimSpace(ents[i].Name), name) {
						existing = &ents[i]
						break
					}
				}
			}
		}
		kind := domain.EntityKind(strings.ToLower(strings.TrimSpace(args.Kind)))
		if !kind.Valid() {
			if existing != nil {
				kind = existing.Kind
			} else {
				kind = domain.EntityKindCharacter
			}
		}
		attrs := map[string]string{}
		if existing != nil {
			for k, v := range existing.Attributes {
				attrs[k] = v
			}
		}
		for k, v := range args.Attributes {
			attrs[k] = v
		}
		aliases := args.Aliases
		if aliases == nil {
			if existing != nil && existing.Aliases != nil {
				aliases = existing.Aliases
			} else {
				aliases = []string{}
			}
		}
		summary := strings.TrimSpace(args.Summary)
		if summary == "" && existing != nil {
			summary = existing.Summary
		}
		id := args.ID
		if id == 0 && existing != nil {
			id = existing.ID
		}
		saved, err := ds.UpsertEntity(ctx, domain.UpsertEntityInput{
			ID:          id,
			BookID:      bookID,
			Kind:        kind,
			Name:        name,
			Aliases:     aliases,
			IsInvariant: args.IsInvariant || (existing != nil && existing.IsInvariant),
			Summary:     summary,
			Attributes:  attrs,
		})
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("已保存设定「%s」（%s，id=%d）。", saved.Name, saved.Kind.Label(), saved.ID), true, nil

	case "upsert_foreshadowing":
		var args struct {
			ID                int64   `json:"id"`
			Title             string  `json:"title"`
			Description       string  `json:"description"`
			PlantedSeq        int     `json:"planted_seq"`
			TargetResolveSeq  int     `json:"target_resolve_seq"`
			Status            string  `json:"status"`
			InvolvedEntityIDs []int64 `json:"involved_entity_ids"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, err
		}
		var existingFS *domain.Foreshadowing
		if args.ID > 0 {
			if fsList, lErr := ds.ListForeshadowings(ctx, bookID, ""); lErr == nil {
				for i := range fsList {
					if fsList[i].ID == args.ID {
						existingFS = &fsList[i]
						break
					}
				}
			}
		}
		st := domain.ForeshadowingStatus(strings.ToLower(strings.TrimSpace(args.Status)))
		if !st.Valid() {
			if existingFS != nil && existingFS.Status.Valid() {
				st = existingFS.Status
			} else {
				st = domain.ForeshadowingPlanted
			}
		}
		involvedIDs := args.InvolvedEntityIDs
		if involvedIDs == nil {
			if existingFS != nil && existingFS.InvolvedEntityIDs != nil {
				involvedIDs = existingFS.InvolvedEntityIDs
			} else {
				involvedIDs = []int64{}
			}
		}
		title := strings.TrimSpace(args.Title)
		if title == "" && existingFS != nil {
			title = existingFS.Title
		}
		desc := strings.TrimSpace(args.Description)
		if desc == "" && existingFS != nil {
			desc = existingFS.Description
		}
		plantedSeq := args.PlantedSeq
		if plantedSeq == 0 && existingFS != nil {
			plantedSeq = existingFS.PlantedChapterSeq
		}
		targetResolveSeq := args.TargetResolveSeq
		if targetResolveSeq == 0 && existingFS != nil {
			targetResolveSeq = existingFS.TargetResolveSeq
		}
		lastHintSeq := 0
		resolvedSeq := 0
		if existingFS != nil {
			lastHintSeq = existingFS.LastHintSeq
			resolvedSeq = existingFS.ResolvedChapterSeq
		}
		saved, err := ds.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
			ID:                 args.ID,
			BookID:             bookID,
			Title:              title,
			Description:        desc,
			PlantedChapterSeq:  plantedSeq,
			LastHintSeq:        lastHintSeq,
			TargetResolveSeq:   targetResolveSeq,
			ResolvedChapterSeq: resolvedSeq,
			Status:             st,
			InvolvedEntityIDs:  involvedIDs,
		})
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("已保存伏笔「%s」（id=%d，状态=%s）。", saved.Title, saved.ID, saved.Status), true, nil

	case "upsert_volume":
		var args struct {
			ID         int64  `json:"id"`
			Seq        int    `json:"seq"`
			Title      string `json:"title"`
			Goal       string `json:"goal"`
			ClimaxPlan string `json:"climax_plan"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, err
		}
		saved, err := ds.UpsertVolume(ctx, domain.UpsertVolumeInput{
			ID:         args.ID,
			BookID:     bookID,
			Seq:        args.Seq,
			Title:      strings.TrimSpace(args.Title),
			Goal:       strings.TrimSpace(args.Goal),
			ClimaxPlan: strings.TrimSpace(args.ClimaxPlan),
		})
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("已保存第 %d 卷《%s》（volume_id=%d）。", saved.Seq, saved.Title, saved.ID), true, nil

	case "upsert_chapter_outline":
		var args struct {
			ChapterID      int64  `json:"chapter_id"`
			ChapterSeq     int    `json:"chapter_seq"`
			VolumeID       int64  `json:"volume_id"`
			Title          string `json:"title"`
			EmotionTag     string `json:"emotion_tag"`
			HookType       string `json:"hook_type"`
			HookNote       string `json:"hook_note"`
			OutlineSummary string `json:"outline_summary"`
			Beats          []struct {
				Stage        string `json:"stage"`
				Summary      string `json:"summary"`
				ConflictNote string `json:"conflict_note"`
				TargetWords  int    `json:"target_words"`
			} `json:"beats"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, err
		}
		emotion := domain.EmotionTag(strings.ToLower(strings.TrimSpace(args.EmotionTag)))
		if !emotion.Valid() {
			emotion = domain.EmotionSetup
		}
		hookType := domain.HookType(strings.ToLower(strings.TrimSpace(args.HookType)))
		if !hookType.Valid() {
			hookType = domain.HookMystery
		}

		var chap domain.Chapter
		if args.ChapterID > 0 || args.ChapterSeq > 0 {
			existing, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
			if err != nil {
				return "", false, err
			}
			title := strings.TrimSpace(args.Title)
			if title == "" {
				title = existing.Title
			}
			if args.EmotionTag == "" {
				emotion = existing.EmotionTag
			}
			if args.HookType == "" {
				hookType = existing.HookType
			}
			hookNote := strings.TrimSpace(args.HookNote)
			if hookNote == "" {
				hookNote = existing.HookNote
			}
			outline := strings.TrimSpace(args.OutlineSummary)
			if outline == "" {
				outline = existing.OutlineSummary
			}
			status := existing.Status
			if status == domain.ChapterStatusPlanned {
				status = domain.ChapterStatusOutlined
			}
			chap, err = ds.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
				ID:             existing.ID,
				Title:          title,
				Status:         status,
				EmotionTag:     emotion,
				HookType:       hookType,
				HookNote:       hookNote,
				OutlineSummary: outline,
				CausalSummary:  existing.CausalSummary,
			})
			if err != nil {
				return "", false, err
			}
		} else {
			volID := args.VolumeID
			if volID <= 0 {
				vols, vErr := ds.ListVolumes(ctx, bookID)
				if vErr != nil {
					return "", false, vErr
				}
				if len(vols) == 0 {
					v, cErr := ds.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: bookID, Seq: 1, Title: "第一卷"})
					if cErr != nil {
						return "", false, cErr
					}
					volID = v.ID
				} else {
					volID = vols[len(vols)-1].ID
				}
			}
			title := strings.TrimSpace(args.Title)
			if title == "" {
				title = "新章节"
			}
			created, cErr := ds.CreateChapter(ctx, domain.CreateChapterInput{
				BookID:         bookID,
				VolumeID:       volID,
				Title:          title,
				EmotionTag:     emotion,
				HookType:       hookType,
				HookNote:       strings.TrimSpace(args.HookNote),
				OutlineSummary: strings.TrimSpace(args.OutlineSummary),
			})
			if cErr != nil {
				return "", false, cErr
			}
			chap, cErr = ds.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
				ID:             created.ID,
				Title:          created.Title,
				Status:         domain.ChapterStatusOutlined,
				EmotionTag:     emotion,
				HookType:       hookType,
				HookNote:       strings.TrimSpace(args.HookNote),
				OutlineSummary: strings.TrimSpace(args.OutlineSummary),
			})
			if cErr != nil {
				return "", false, cErr
			}
		}

		if len(args.Beats) > 0 {
			ents, _ := ds.ListEntities(ctx, bookID, domain.EntityFilter{})
			beats := make([]domain.Beat, 0, len(args.Beats))
			for i, b := range args.Beats {
				if strings.TrimSpace(b.Summary) == "" {
					continue
				}
				st := domain.BeatStage(strings.ToLower(strings.TrimSpace(b.Stage)))
				if !st.Valid() {
					st = domain.BeatStageSetup
				}
				tw := b.TargetWords
				if tw <= 0 {
					tw = domain.DefaultBeatTargetWords
				}
				beats = append(beats, domain.Beat{
					ChapterID:    chap.ID,
					Seq:          i + 1,
					Stage:        st,
					Summary:      strings.TrimSpace(b.Summary),
					ConflictNote: strings.TrimSpace(b.ConflictNote),
					EntityIDs:    matchMentionedEntityIDs(b.Summary+" "+b.ConflictNote, ents),
					TargetWords:  tw,
				})
			}
			if len(beats) > 0 {
				if _, bErr := ds.ReplaceBeats(ctx, chap.ID, beats); bErr != nil {
					return "", true, bErr
				}
			}
		}
		return fmt.Sprintf("已更新第 %d 章《%s》的大纲与节拍（chapter_id=%d）。", chap.GlobalSeq, chap.Title, chap.ID), true, nil

	case "batch_generate_outlines":
		var args struct {
			VolumeID int64  `json:"volume_id"`
			Count    int    `json:"count"`
			ArcGoal  string `json:"arc_goal"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, err
		}
		volID := args.VolumeID
		if volID <= 0 {
			vols, err := ds.ListVolumes(ctx, bookID)
			if err != nil {
				return "", false, err
			}
			if len(vols) == 0 {
				v, cErr := ds.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: bookID, Seq: 1, Title: "第一卷"})
				if cErr != nil {
					return "", false, cErr
				}
				volID = v.ID
			} else {
				volID = vols[len(vols)-1].ID
			}
		}
		count := args.Count
		if count <= 0 || count > 10 {
			count = 3
		}
		created, err := r.BatchGenerateOutlines(ctx, domain.BatchOutlineRequest{
			BookID:   bookID,
			VolumeID: volID,
			Count:    count,
			ArcGoal:  strings.TrimSpace(args.ArcGoal),
		}, nil)
		if err != nil {
			return "", false, err
		}
		titles := make([]string, 0, len(created))
		for _, ch := range created {
			titles = append(titles, fmt.Sprintf("第%d章《%s》", ch.GlobalSeq, ch.Title))
		}
		return fmt.Sprintf("已批量生成 %d 章大纲：%s。", len(created), strings.Join(titles, "、")), true, nil

	case "write_chapter_content":
		var args struct {
			ChapterID  int64  `json:"chapter_id"`
			ChapterSeq int    `json:"chapter_seq"`
			Content    string `json:"content"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(args.Content) == "" {
			return "", false, errors.New("正文内容不能为空")
		}
		chap, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
		if err != nil {
			return "", false, err
		}
		// SaveChapterDraft 底层严格执行先归档 chapter_versions 快照再覆写
		saved, err := ds.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
			ChapterID: chap.ID,
			Content:   args.Content,
			Status:    domain.ChapterStatusReviewing,
		})
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("已更新第 %d 章《%s》正文（共 %d 字，修改前已自动归档历史快照）。", saved.GlobalSeq, saved.Title, saved.WordCount), true, nil

	case "generate_chapter":
		var args struct {
			ChapterID  int64 `json:"chapter_id"`
			ChapterSeq int   `json:"chapter_seq"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, err
		}
		chap, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
		if err != nil {
			return "", false, err
		}
		saved, err := r.GenerateChapter(ctx, chap.ID, domain.GenerateChapterOptions{
			Briefing: domain.BriefingOptions{IncludePending: true},
		}, nil)
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("已生成第 %d 章《%s》正文，共 %d 字。", saved.GlobalSeq, saved.Title, saved.WordCount), true, nil

	case "audit_and_polish_chapter":
		var args struct {
			ChapterID  int64 `json:"chapter_id"`
			ChapterSeq int   `json:"chapter_seq"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, err
		}
		chap, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
		if err != nil {
			return "", false, err
		}
		if r.styleStore == nil {
			return "", false, errors.New("质检引擎未初始化")
		}
		passRes, passErr := r.runPolishPass(ctx, chap.ID, false, nil)
		if passErr != nil {
			return "", false, passErr
		}
		return fmt.Sprintf("第 %d 章《%s》质检完成：发现 %d 处本地问题、%d 条 AI 审校建议，已自动润色 %d 处段落。",
			chap.GlobalSeq, chap.Title, len(passRes.Audit.LintIssues), len(passRes.Audit.CriticFindings), passRes.AppliedDiffs), passRes.AppliedDiffs > 0, nil

	case "start_autopilot":
		var args struct {
			TargetChapters   int    `json:"target_chapters"`
			VolumeID         int64  `json:"volume_id"`
			ArcGoal          string `json:"arc_goal"`
			AutoOutline      *bool  `json:"auto_outline"`
			AutoPolish       *bool  `json:"auto_polish"`
			AutoCommitMemory *bool  `json:"auto_commit_memory"`
		}
		if err := unmarshalDirectorArgs(rawArgs, &args); err != nil {
			return "", false, err
		}
		count := args.TargetChapters
		if count <= 0 || count > 20 {
			count = 3
		}
		autoOutline := true
		if args.AutoOutline != nil {
			autoOutline = *args.AutoOutline
		}
		autoPolish := true
		if args.AutoPolish != nil {
			autoPolish = *args.AutoPolish
		}
		autoCommit := false
		if args.AutoCommitMemory != nil {
			autoCommit = *args.AutoCommitMemory
		}
		state, err := r.StartAutopilot(ctx, domain.AutopilotRequest{
			BookID:           bookID,
			VolumeID:         args.VolumeID,
			TargetChapters:   count,
			AutoOutline:      autoOutline,
			AutoPolish:       autoPolish,
			AutoCommitMemory: autoCommit,
			ArcGoal:          strings.TrimSpace(args.ArcGoal),
		})
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("已在后台启动全自动托管（目标 %d 章）：%s。", state.TargetChapters, state.Message), true, nil

	case "stop_autopilot":
		state, err := r.StopAutopilot(bookID)
		if err != nil {
			return "", false, err
		}
		return fmt.Sprintf("已请求停止托管：%s。", state.Message), true, nil

	default:
		return "", false, fmt.Errorf("未知工具：%s", name)
	}
}

func resolveDirectorChapter(
	ctx context.Context,
	st Store,
	ds DirectorStore,
	bookID int64,
	chapterID int64,
	chapterSeq int,
) (domain.Chapter, error) {
	if chapterID > 0 {
		ch, err := st.GetChapter(ctx, chapterID)
		if err != nil {
			return domain.Chapter{}, err
		}
		if ch.BookID != bookID {
			return domain.Chapter{}, errors.New("章节不属于当前书籍")
		}
		return ch, nil
	}
	if chapterSeq > 0 {
		chaps, err := ds.ListBookChapters(ctx, bookID)
		if err != nil {
			return domain.Chapter{}, err
		}
		for _, ch := range chaps {
			if ch.GlobalSeq == chapterSeq {
				return st.GetChapter(ctx, ch.ID)
			}
		}
		return domain.Chapter{}, fmt.Errorf("未找到第 %d 章", chapterSeq)
	}
	return domain.Chapter{}, errors.New("请指定 chapter_id 或 chapter_seq")
}

func buildDirectorBookSnapshot(ctx context.Context, ds DirectorStore, book domain.Book, activeChapterID int64) (string, error) {
	preset := genres.GetOrDefault(book.Genre)
	vols, err := ds.ListVolumes(ctx, book.ID)
	if err != nil {
		return "", fmt.Errorf("list book volumes: %w", err)
	}
	chaps, err := ds.ListBookChapters(ctx, book.ID)
	if err != nil {
		return "", fmt.Errorf("list book chapters: %w", err)
	}
	ents, err := ds.ListEntities(ctx, book.ID, domain.EntityFilter{})
	if err != nil {
		return "", fmt.Errorf("list book entities: %w", err)
	}
	fsList, err := ds.ListForeshadowings(ctx, book.ID, "")
	if err != nil {
		return "", fmt.Errorf("list book foreshadowings: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "书名：《%s》（题材：%s，每章目标 %d 字）\n", book.Title, preset.Name, book.TargetChapterWords)
	if strings.TrimSpace(book.Premise) != "" {
		fmt.Fprintf(&b, "简介：%s\n", strings.TrimSpace(book.Premise))
	}
	if strings.TrimSpace(book.GoldenFinger) != "" {
		fmt.Fprintf(&b, "金手指：%s\n", strings.TrimSpace(book.GoldenFinger))
	}
	if activeChapterID > 0 {
		fmt.Fprintf(&b, "作者当前打开的章节 ID：%d\n", activeChapterID)
	}

	b.WriteString("\n【分卷】\n")
	if len(vols) == 0 {
		b.WriteString("（暂无分卷）\n")
	} else {
		for _, v := range vols {
			fmt.Fprintf(&b, "- 第%d卷《%s》(volume_id=%d) 目标：%s\n", v.Seq, v.Title, v.ID, v.Goal)
		}
	}

	b.WriteString("\n【章节目录】\n")
	if len(chaps) == 0 {
		b.WriteString("（暂无章节）\n")
	} else {
		start := 0
		if len(chaps) > 25 {
			start = len(chaps) - 25
			fmt.Fprintf(&b, "（仅展示最近 25 章，共 %d 章）\n", len(chaps))
		}
		for _, ch := range chaps[start:] {
			fmt.Fprintf(&b, "- 第%d章《%s》(chapter_id=%d, status=%s, %d字, 情绪=%s) %s\n",
				ch.GlobalSeq, ch.Title, ch.ID, ch.Status, ch.WordCount, ch.EmotionTag, summarizeDirectorText(ch.OutlineSummary, 60))
		}
	}

	b.WriteString("\n【核心设定】\n")
	if len(ents) == 0 {
		b.WriteString("（暂无设定）\n")
	} else {
		limit := min(len(ents), 20)
		for i := 0; i < limit; i++ {
			e := ents[i]
			fmt.Fprintf(&b, "- [%s] %s(id=%d)：%s\n", e.Kind.Label(), e.Name, e.ID, summarizeDirectorText(e.Summary, 60))
		}
	}

	if len(fsList) > 0 {
		b.WriteString("\n【伏笔】\n")
		limit := min(len(fsList), 10)
		for i := 0; i < limit; i++ {
			f := fsList[i]
			fmt.Fprintf(&b, "- %s(id=%d, status=%s, 计划第%d章回收)\n", f.Title, f.ID, f.Status, f.TargetResolveSeq)
		}
	}
	return b.String(), nil
}

func buildDirectorSystemPrompt(book domain.Book, snapshot string) string {
	return fmt.Sprintf(`你是 Nib 写作工作台的「AI 导演」，拥有全书设定、分卷、章节大纲、正文写作、质检润色与后台托管流水线的直接操控权。
你可以连续多步调用工具，直到彻底完成作者的要求，然后再用简洁清晰的简体中文汇报你做了什么。

当前书籍实时状态：
%s

可用工具清单（只能调用以下工具）：
%s

输出规范（极其重要）：
每一步只输出一个合法 JSON 对象，不要输出任何额外说明或 Markdown 围栏：
- 需要调用工具时：
  {"thought": "简短思考", "action": "tool", "tool_name": "工具名", "tool_args": {...}}
- 任务全部完成或只需直接回答作者时：
  {"thought": "简短总结", "action": "final", "reply": "面向作者的清晰中文回复（说明具体修改了哪些设定/章节以及下一步建议）"}

当前书籍：《%s》。请务必直接动手执行，不要只给口头建议。`, snapshot, formatDirectorToolPromptList(), book.Title)
}

func parseDirectorDecision(raw string) (directorStepDecision, error) {
	jsonStr, err := llm.ExtractJSONObject(raw)
	if err != nil {
		return directorStepDecision{}, err
	}
	var dec directorStepDecision
	if err := json.Unmarshal([]byte(jsonStr), &dec); err != nil {
		return directorStepDecision{}, err
	}
	dec.Action = strings.ToLower(strings.TrimSpace(dec.Action))
	if dec.Action == "" {
		if strings.TrimSpace(dec.ToolName) != "" {
			dec.Action = "tool"
		} else {
			dec.Action = "final"
		}
	}
	return dec, nil
}

func summarizeDirectorText(s string, maxRunes int) string {
	trimmed := strings.TrimSpace(s)
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes]) + "…"
}
