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
	route, provider, err := r.resolveStageRouteAndProvider(ctx, domain.StageOutline)
	if err != nil {
		route, provider, err = r.resolveStageRouteAndProvider(ctx, domain.StageDraft)
		if err != nil {
			return domain.DirectorReply{}, err
		}
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
	finalReply := ""

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

	if hs, ok := r.store.(DirectorHistoryStore); ok {
		_, _ = hs.AppendDirectorMessage(ctx, book.ID, domain.DirectorMessage{
			Role:      "assistant",
			Content:   finalReply,
			ToolCalls: toolCalls,
		})
	}

	_ = send(domain.DirectorEvent{
		Kind:      domain.DirectorEventCompleted,
		Reply:     finalReply,
		ToolCalls: toolCalls,
		Mutated:   mutated,
	})
	return domain.DirectorReply{
		Reply:     finalReply,
		ToolCalls: toolCalls,
		Mutated:   mutated,
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
		_ = json.Unmarshal(rawArgs, &args)
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
		_ = json.Unmarshal(rawArgs, &args)
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
		_ = json.Unmarshal(rawArgs, &args)
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
		_ = json.Unmarshal(rawArgs, &args)
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
		_ = json.Unmarshal(rawArgs, &args)
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
			ID               int64  `json:"id"`
			Title            string `json:"title"`
			Description      string `json:"description"`
			PlantedSeq       int    `json:"planted_seq"`
			TargetResolveSeq int    `json:"target_resolve_seq"`
			Status           string `json:"status"`
		}
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", false, err
		}
		st := domain.ForeshadowingStatus(strings.ToLower(strings.TrimSpace(args.Status)))
		if !st.Valid() {
			st = domain.ForeshadowingPlanted
		}
		saved, err := ds.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
			ID:                args.ID,
			BookID:            bookID,
			Title:             strings.TrimSpace(args.Title),
			Description:       strings.TrimSpace(args.Description),
			PlantedChapterSeq: args.PlantedSeq,
			TargetResolveSeq:  args.TargetResolveSeq,
			Status:            st,
			InvolvedEntityIDs: []int64{},
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
		_ = json.Unmarshal(rawArgs, &args)
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
		_ = json.Unmarshal(rawArgs, &args)
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
		_ = json.Unmarshal(rawArgs, &args)
		chap, err := resolveDirectorChapter(ctx, r.store, ds, bookID, args.ChapterID, args.ChapterSeq)
		if err != nil {
			return "", false, err
		}
		if r.styleStore == nil {
			return "", false, errors.New("质检引擎未初始化")
		}
		audit, err := r.LintChapterLocal(ctx, chap.ID)
		if err != nil {
			return "", false, err
		}
		if _, _, rErr := r.resolveStageRouteAndProvider(ctx, domain.StageCritic); rErr == nil {
			if cAudit, cErr := r.RunCriticAudit(ctx, chap.ID); cErr == nil {
				audit = cAudit
			}
		}
		applied := 0
		if len(audit.LintIssues) > 0 || len(audit.CriticFindings) > 0 {
			if _, _, pErr := r.resolveStageRouteAndProvider(ctx, domain.StagePolish); pErr == nil {
				if diffAudit, dErr := r.GenerateSurgicalDiffs(ctx, chap.ID, nil); dErr == nil {
					for _, d := range diffAudit.Diffs {
						if d.Status == domain.DiffStatusPending {
							if _, aErr := r.ApplySurgicalDiff(ctx, domain.ApplySurgicalDiffInput{
								ChapterID: chap.ID,
								DiffID:    d.ID,
								Accept:    true,
							}); aErr == nil {
								applied++
							}
						}
					}
				}
			}
		}
		return fmt.Sprintf("第 %d 章《%s》质检完成：发现 %d 处本地问题、%d 条 AI 审校建议，已自动润色 %d 处段落。",
			chap.GlobalSeq, chap.Title, len(audit.LintIssues), len(audit.CriticFindings), applied), applied > 0, nil

	case "start_autopilot":
		var args struct {
			TargetChapters   int    `json:"target_chapters"`
			VolumeID         int64  `json:"volume_id"`
			ArcGoal          string `json:"arc_goal"`
			AutoOutline      *bool  `json:"auto_outline"`
			AutoPolish       *bool  `json:"auto_polish"`
			AutoCommitMemory *bool  `json:"auto_commit_memory"`
		}
		_ = json.Unmarshal(rawArgs, &args)
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
		autoCommit := true
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
	vols, _ := ds.ListVolumes(ctx, book.ID)
	chaps, _ := ds.ListBookChapters(ctx, book.ID)
	ents, _ := ds.ListEntities(ctx, book.ID, domain.EntityFilter{})
	fsList, _ := ds.ListForeshadowings(ctx, book.ID, "")

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
1. get_book_overview {} — 重新获取全书最新概览
2. read_chapter {"chapter_id": 0, "chapter_seq": 1} — 读取指定章节的详细大纲、节拍与正文
3. search_memory {"query": "关键词"} — 搜索全书正文与设定
4. update_book {"title": "", "premise": "", "golden_finger": "", "target_chapter_words": 2500} — 更新书籍简介、金手指或目标字数
5. upsert_entity {"kind": "character|faction|location|item|system|rule", "name": "", "summary": "", "attributes": {}, "is_invariant": false} — 新增或更新角色/势力/地点/道具/世界铁律
6. upsert_foreshadowing {"title": "", "description": "", "planted_seq": 1, "target_resolve_seq": 10, "status": "planted|hinted|resolved|abandoned"} — 新增或更新伏笔
7. upsert_volume {"seq": 1, "title": "", "goal": "", "climax_plan": ""} — 新增或更新分卷
8. upsert_chapter_outline {"chapter_id": 0, "chapter_seq": 0, "volume_id": 0, "title": "", "emotion_tag": "setup|suppress|turn|burst|harvest", "hook_type": "mystery|crisis|counterattack_eve|reward_pending|new_arc", "hook_note": "", "outline_summary": "", "beats": [{"stage": "setup", "summary": "", "conflict_note": "", "target_words": 500}]} — 新建或修改单章大纲与节拍
9. batch_generate_outlines {"volume_id": 0, "count": 3, "arc_goal": ""} — 一次性批量生成后续 1~10 章大纲与节拍
10. write_chapter_content {"chapter_id": 0, "chapter_seq": 1, "content": "..."} — 直接写入或修改某章完整正文（自动先存快照）
11. generate_chapter {"chapter_id": 0, "chapter_seq": 1} — 调用写作管线为某章生成正文
12. audit_and_polish_chapter {"chapter_id": 0, "chapter_seq": 1} — 对某章执行本地扫描 + AI 审校并自动应用局部润色
13. start_autopilot {"target_chapters": 3, "arc_goal": "", "auto_outline": true, "auto_polish": true, "auto_commit_memory": true} — 启动后台全自动托管连续写稿
14. stop_autopilot {} — 停止正在运行的后台托管

输出规范（极其重要）：
每一步只输出一个合法 JSON 对象，不要输出任何额外说明或 Markdown 围栏：
- 需要调用工具时：
  {"thought": "简短思考", "action": "tool", "tool_name": "工具名", "tool_args": {...}}
- 任务全部完成或只需直接回答作者时：
  {"thought": "简短总结", "action": "final", "reply": "面向作者的清晰中文回复（说明具体修改了哪些设定/章节以及下一步建议）"}

当前书籍：《%s》。请务必直接动手执行，不要只给口头建议。`, snapshot, book.Title)
}

func parseDirectorDecision(raw string) (directorStepDecision, error) {
	jsonStr, err := extractStyleJSONObject(raw)
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

func directorToolLabel(name string) string {
	switch name {
	case "get_book_overview":
		return "查看全书概览"
	case "read_chapter":
		return "阅读章节内容"
	case "search_memory":
		return "检索全书记忆"
	case "update_book":
		return "更新书籍设定"
	case "upsert_entity":
		return "更新角色与设定"
	case "upsert_foreshadowing":
		return "更新伏笔记录"
	case "upsert_volume":
		return "规划分卷"
	case "upsert_chapter_outline":
		return "调整章纲与节拍"
	case "batch_generate_outlines":
		return "批量生成章纲"
	case "write_chapter_content":
		return "修改章节正文"
	case "generate_chapter":
		return "生成章节正文"
	case "audit_and_polish_chapter":
		return "质检并局部润色"
	case "start_autopilot":
		return "启动 AI 自动托管"
	case "stop_autopilot":
		return "停止 AI 自动托管"
	case "list_chapters_range":
		return "查询指定区间目录"
	case "get_entity_detail":
		return "查看设定详情与关系"
	case "delete_chapter":
		return "删除章节"
	case "move_chapter":
		return "调整章节顺序"
	case "generate_arc_summary":
		return "自动生成弧线/卷摘要"
	case "upsert_entity_relation":
		return "更新角色/势力关系"
	case "replace_chapter_paragraphs":
		return "局部修改章节段落"
	default:
		return name
	}
}

func directorToolDefinitions() []llm.ToolDefinition {
	obj := func(props map[string]any, required ...string) map[string]any {
		m := map[string]any{
			"type":       "object",
			"properties": props,
		}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	num := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	bl := func(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

	return []llm.ToolDefinition{
		{Name: "get_book_overview", Description: "重新获取全书最新概览", Parameters: obj(map[string]any{})},
		{Name: "list_chapters_range", Description: "按起止章号查询全书目录（长篇超过 25 章时必用）", Parameters: obj(map[string]any{"start_seq": num("起始章号"), "end_seq": num("结束章号")})},
		{Name: "read_chapter", Description: "读取指定章节的大纲、节拍与正文", Parameters: obj(map[string]any{"chapter_id": num("章节ID"), "chapter_seq": num("全局章号")})},
		{Name: "get_entity_detail", Description: "按名称查看角色/势力/道具的完整属性与关系网", Parameters: obj(map[string]any{"name": str("实体名称或关键词")}, "name")},
		{Name: "search_memory", Description: "全文检索全书正文与设定记录", Parameters: obj(map[string]any{"query": str("检索关键词")}, "query")},
		{Name: "update_book", Description: "更新书籍名称、简介、金手指或目标章字数", Parameters: obj(map[string]any{"title": str("书名"), "premise": str("简介"), "golden_finger": str("金手指"), "target_chapter_words": num("每章目标字数")})},
		{Name: "upsert_entity", Description: "新增或更新角色、势力、地点、道具、体系或世界铁律", Parameters: obj(map[string]any{"kind": str("character|faction|location|item|system|rule"), "name": str("名称"), "summary": str("简介"), "is_invariant": bl("是否世界铁律"), "attributes": map[string]any{"type": "object", "description": "属性键值"}}, "name")},
		{Name: "upsert_entity_relation", Description: "记录两个实体之间的关系（如师徒、死敌、同盟、隶属）", Parameters: obj(map[string]any{"source_name": str("源实体名"), "target_name": str("目标实体名"), "relation_type": str("关系类型"), "description": str("关系说明")}, "source_name", "target_name", "relation_type")},
		{Name: "upsert_foreshadowing", Description: "新增或更新伏笔状态与目标回收章号", Parameters: obj(map[string]any{"title": str("伏笔标题"), "description": str("伏笔内容"), "planted_seq": num("埋设章号"), "target_resolve_seq": num("计划回收章号"), "status": str("planted|hinted|resolved|abandoned")}, "title")},
		{Name: "upsert_volume", Description: "新建或更新分卷", Parameters: obj(map[string]any{"seq": num("卷序"), "title": str("卷名"), "goal": str("卷目标"), "climax_plan": str("卷末高潮")}, "title")},
		{Name: "upsert_chapter_outline", Description: "新建或更新单章大纲、情绪、钩子与节拍卡", Parameters: obj(map[string]any{"chapter_id": num("章节ID"), "chapter_seq": num("全局章号"), "volume_id": num("分卷ID"), "title": str("章名"), "emotion_tag": str("setup|suppress|turn|burst|harvest"), "hook_type": str("mystery|crisis|counterattack_eve|reward_pending|new_arc"), "hook_note": str("钩子说明"), "outline_summary": str("章纲"), "beats": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}})},
		{Name: "batch_generate_outlines", Description: "一次性批量生成后续 1~10 章大纲与节拍", Parameters: obj(map[string]any{"volume_id": num("分卷ID"), "count": num("生成章数1-10"), "arc_goal": str("阶段剧情目标")})},
		{Name: "delete_chapter", Description: "删除指定章节并自动重排章序", Parameters: obj(map[string]any{"chapter_id": num("章节ID"), "chapter_seq": num("全局章号")})},
		{Name: "move_chapter", Description: "上移/下移章节或跨卷移动", Parameters: obj(map[string]any{"chapter_id": num("章节ID"), "chapter_seq": num("全局章号"), "direction": str("up|down"), "target_volume_id": num("目标卷ID")})},
		{Name: "generate_chapter", Description: "调用写作管线按作战简报生成某章完整正文", Parameters: obj(map[string]any{"chapter_id": num("章节ID"), "chapter_seq": num("全局章号")})},
		{Name: "replace_chapter_paragraphs", Description: "局部修改某章指定段落范围（优先用于局部修稿，比整章重写更稳）", Parameters: obj(map[string]any{"chapter_id": num("章节ID"), "chapter_seq": num("全局章号"), "start_paragraph": num("起始段落号，从1开始"), "end_paragraph": num("结束段落号，从1开始"), "new_text": str("替换后的段落文本")}, "start_paragraph", "end_paragraph", "new_text")},
		{Name: "audit_and_polish_chapter", Description: "对指定章节执行本地扫描 + AI 审校并自动应用局部润色", Parameters: obj(map[string]any{"chapter_id": num("章节ID"), "chapter_seq": num("全局章号")})},
		{Name: "generate_arc_summary", Description: "AI 汇总指定章号区间（arc_10）或整卷（volume）的长期记忆摘要", Parameters: obj(map[string]any{"level": str("arc_10|volume"), "volume_id": num("卷ID"), "start_seq": num("起始章号"), "end_seq": num("结束章号")})},
		{Name: "start_autopilot", Description: "启动后台全自动托管连续写稿", Parameters: obj(map[string]any{"target_chapters": num("章数1-20"), "arc_goal": str("剧情目标"), "auto_outline": bl("自动续写章纲"), "auto_polish": bl("自动审校润色"), "auto_commit_memory": bl("自动同步记忆")})},
		{Name: "stop_autopilot", Description: "停止正在运行的后台托管", Parameters: obj(map[string]any{})},
	}
}

func summarizeDirectorText(s string, maxRunes int) string {
	trimmed := strings.TrimSpace(s)
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes]) + "…"
}
