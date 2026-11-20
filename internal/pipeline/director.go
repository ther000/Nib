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
			Temperature: 0.4,
			MaxTokens:   maxTok,
		}

		start := time.Now()
		var buf strings.Builder
		var usage domain.TokenUsage
		streamUsage, streamErr := r.llm.Stream(ctx, provider, chatReq, func(ev llm.StreamEvent) error {
			if ev.Delta != "" {
				buf.WriteString(ev.Delta)
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
			r.recordTokenUsage(ctx, book.ID, req.ActiveChapterID, domain.StageOutline, provider.Name, route.ModelName, usage, time.Since(start).Milliseconds())
		}

		raw := strings.TrimSpace(buf.String())
		dec, parseErr := parseDirectorDecision(raw)
		if parseErr != nil {
			// 如果模型直接输出了自然语言回复（非 JSON），当作最终回复平滑兜底。
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

		// 执行工具
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

		resultText, didMutate, toolErr := r.executeDirectorTool(ctx, ds, book.ID, tc.Name, dec.ToolArgs)
		if didMutate {
			mutated = true
		}
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

		// 将本轮决策与工具观测写回对话，继续下一轮推理
		messages = append(messages,
			llm.ChatMessage{Role: "assistant", Content: raw},
			llm.ChatMessage{Role: "user", Content: fmt.Sprintf("【工具 %s 执行结果】\n%s\n\n如果任务已完成，请输出 action=\"final\" 的 JSON 并向作者简明汇报结果；如果还需继续操作，请调用下一个工具。", tc.Name, resultText)},
		)
	}

	if strings.TrimSpace(finalReply) == "" {
		finalReply = "已完成多步操作。如需继续调整，直接告诉我下一步想法。"
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
	default:
		return name
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
