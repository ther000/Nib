package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"nib/internal/domain"
	"nib/internal/llm"
)

// ArcSummaryStore 定义弧线/卷摘要读写所需的最小仓储接口（由 *store.Store 自动满足）。
type ArcSummaryStore interface {
	ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error)
	ListChapters(ctx context.Context, volumeID int64) ([]domain.Chapter, error)
	ListArcSummaries(ctx context.Context, bookID int64, level domain.ArcSummaryLevel) ([]domain.ArcSummary, error)
	UpsertArcSummary(ctx context.Context, in domain.UpsertArcSummaryInput) (domain.ArcSummary, error)
}

// GenerateArcSummary 自动汇总指定章号区间（10 章弧线）或整卷的剧情摘要并写入 arc_summaries。
func (r *Runner) GenerateArcSummary(ctx context.Context, req domain.GenerateArcSummaryRequest) (domain.ArcSummary, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.ArcSummary{}, errors.New("pipeline summary dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.ArcSummary{}, err
	}
	as, ok := r.store.(ArcSummaryStore)
	if !ok {
		return domain.ArcSummary{}, errors.New("store does not implement ArcSummaryStore")
	}

	book, err := r.store.GetBook(ctx, req.BookID)
	if err != nil {
		return domain.ArcSummary{}, fmt.Errorf("load book %d: %w", req.BookID, err)
	}

	var chaps []domain.Chapter
	var volTitle string
	startSeq := req.StartSeq
	endSeq := req.EndSeq

	if req.Level == domain.ArcSummaryLevelVolume && req.VolumeID != nil && *req.VolumeID > 0 {
		vol, vErr := r.store.GetVolume(ctx, *req.VolumeID)
		if vErr != nil {
			return domain.ArcSummary{}, fmt.Errorf("load volume %d: %w", *req.VolumeID, vErr)
		}
		volTitle = vol.Title
		chaps, err = as.ListChapters(ctx, *req.VolumeID)
		if err != nil {
			return domain.ArcSummary{}, err
		}
		if len(chaps) > 0 {
			if startSeq <= 0 {
				startSeq = chaps[0].GlobalSeq
			}
			if endSeq <= 0 {
				endSeq = chaps[len(chaps)-1].GlobalSeq
			}
		}
	} else {
		allChaps, lErr := as.ListBookChapters(ctx, req.BookID)
		if lErr != nil {
			return domain.ArcSummary{}, lErr
		}
		for _, ch := range allChaps {
			if ch.GlobalSeq >= startSeq && ch.GlobalSeq <= endSeq {
				chaps = append(chaps, ch)
			}
		}
	}

	if len(chaps) == 0 {
		return domain.ArcSummary{}, fmt.Errorf("%w: 所选范围内暂无章节可供汇总", domain.ErrValidation)
	}

	// 优先使用 extract 路由，依次回退 outline / draft。
	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StageExtract, domain.StageOutline, domain.StageDraft)
	if err != nil {
		return domain.ArcSummary{}, err
	}

	var material strings.Builder
	for _, ch := range chaps {
		fmt.Fprintf(&material, "【第 %d 章《%s》】\n", ch.GlobalSeq, ch.Title)
		switch {
		case strings.TrimSpace(ch.CausalSummary) != "":
			fmt.Fprintf(&material, "因果摘要：%s\n", strings.TrimSpace(ch.CausalSummary))
		case strings.TrimSpace(ch.Content) != "":
			fmt.Fprintf(&material, "正文摘录：%s\n", headRunes(strings.TrimSpace(ch.Content), 360))
		case strings.TrimSpace(ch.OutlineSummary) != "":
			fmt.Fprintf(&material, "章纲：%s\n", strings.TrimSpace(ch.OutlineSummary))
		default:
			material.WriteString("（暂无正文或摘要）\n")
		}
		material.WriteByte('\n')
	}

	scopeLabel := fmt.Sprintf("第 %d~%d 章弧线", startSeq, endSeq)
	if req.Level == domain.ArcSummaryLevelVolume {
		if volTitle != "" {
			scopeLabel = fmt.Sprintf("《%s》（第 %d~%d 章）卷级", volTitle, startSeq, endSeq)
		} else {
			scopeLabel = fmt.Sprintf("第 %d~%d 章卷级", startSeq, endSeq)
		}
	}

	sysPrompt := fmt.Sprintf(
		"你是长篇中文网文剧情档案员，负责为《%s》压缩提炼高质量的长期记忆摘要，供后续章节防止吃书和忘记主线。\n"+
			"输出规范：\n"+
			"1. 输出 250~450 字的凝练中文摘要（纯文本，不要标题和 Markdown 列表）；\n"+
			"2. 必须按因果顺序交代：主角核心目标进展、关键胜负与实力/资源变化、重要人物关系变动、遗留悬念或下一阶段引子；\n"+
			"3. 严禁虚构区间内未发生的剧情。",
		book.Title,
	)
	userPrompt := fmt.Sprintf("请根据以下分章资料，生成 %s 剧情摘要：\n\n%s", scopeLabel, material.String())

	chatReq := llm.ChatRequest{
		Model:       route.ModelName,
		System:      sysPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userPrompt}},
		Temperature: 0.25,
		MaxTokens:   1024,
	}

	var buf strings.Builder
	start := time.Now()
	usedProv, usedModel, usage, streamErr := r.streamWithFallback(ctx, route, provider, chatReq, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
		}
		return nil
	})
	if hasUsage(usage) {
		r.recordTokenUsage(ctx, req.BookID, 0, domain.StageExtract, usedProv.Name, usedModel, usage, time.Since(start).Milliseconds())
	}
	if streamErr != nil {
		return domain.ArcSummary{}, streamErr
	}

	summaryText := strings.TrimSpace(buf.String())
	if summaryText == "" {
		return domain.ArcSummary{}, fmt.Errorf("%w: 模型未返回有效摘要内容", domain.ErrUpstreamUnavailable)
	}

	// 若同一层级+起止章号已存在，则覆盖更新而非重复插入。
	var existingID int64
	if existingList, lErr := as.ListArcSummaries(ctx, req.BookID, req.Level); lErr == nil {
		for _, item := range existingList {
			if item.StartSeq == startSeq && item.EndSeq == endSeq {
				existingID = item.ID
				break
			}
		}
	}

	return as.UpsertArcSummary(ctx, domain.UpsertArcSummaryInput{
		ID:       existingID,
		BookID:   req.BookID,
		Level:    req.Level,
		VolumeID: req.VolumeID,
		StartSeq: startSeq,
		EndSeq:   endSeq,
		Summary:  summaryText,
	})
}

// maybeAutoRollArcSummary 在完章号为 10 的倍数时，若尚未存在对应 10 章弧线摘要则自动生成写入。
func (r *Runner) maybeAutoRollArcSummary(ctx context.Context, bookID int64, completedSeq int) (bool, error) {
	if completedSeq <= 0 || completedSeq%10 != 0 {
		return false, nil
	}
	as, ok := r.store.(ArcSummaryStore)
	if !ok {
		return false, nil
	}
	startSeq := completedSeq - 9
	existing, err := as.ListArcSummaries(ctx, bookID, domain.ArcSummaryLevelArc10)
	if err != nil {
		return false, err
	}
	for _, item := range existing {
		if item.StartSeq == startSeq && item.EndSeq == completedSeq {
			return false, nil
		}
	}
	_, err = r.GenerateArcSummary(ctx, domain.GenerateArcSummaryRequest{
		BookID:   bookID,
		Level:    domain.ArcSummaryLevelArc10,
		StartSeq: startSeq,
		EndSeq:   completedSeq,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
