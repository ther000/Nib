package compiler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"nib/internal/domain"
	"nib/internal/genres"
)

const (
	defaultP0Budget       = 1200
	defaultP1Budget       = 1000
	defaultP2Budget       = 2400
	defaultP4Budget       = 1600
	prevChapter1TailRunes = 1200
	prevChapter2TailRunes = 400
)

// DataSource 定义上下文编译器所需的最小只读仓储契约，解耦 internal/store 实现细节。
type DataSource interface {
	GetBook(ctx context.Context, id int64) (domain.Book, error)
	GetVolume(ctx context.Context, id int64) (domain.Volume, error)
	GetChapter(ctx context.Context, id int64) (domain.Chapter, error)
	ListBeats(ctx context.Context, chapterID int64) ([]domain.Beat, error)
	ListInvariants(ctx context.Context, bookID int64) ([]domain.Entity, error)
	GetEntitiesByIDs(ctx context.Context, ids []int64) ([]domain.Entity, error)
	GetPreviousChapters(ctx context.Context, bookID int64, beforeGlobalSeq int, limit int) ([]domain.Chapter, error)
}

// Compiler 负责将书卷设定、世界铁律、节拍卡、出场实体与前文衔接编译为透明的作战简报。
type Compiler struct {
	src DataSource
}

// New 创建上下文编译器实例。
func New(src DataSource) *Compiler {
	return &Compiler{src: src}
}

type p2Candidate struct {
	entity       domain.Entity
	highPriority bool // POV 或作者显式钉选实体
}

// Compile 按 P0/P1（永不裁剪）→ P2（实体档案）→ P4（前文结尾衔接）优先级编译 Phase 1 作战简报。
func (c *Compiler) Compile(ctx context.Context, chapterID int64, opts domain.BriefingOptions) (domain.Briefing, error) {
	if c == nil || c.src == nil {
		return domain.Briefing{}, errors.New("compiler data source is nil")
	}
	if chapterID <= 0 {
		return domain.Briefing{}, fmt.Errorf("%w: chapterId must be > 0", domain.ErrValidation)
	}
	if opts.TokenBudget < 0 {
		return domain.Briefing{}, fmt.Errorf("%w: tokenBudget cannot be negative", domain.ErrValidation)
	}

	chap, err := c.src.GetChapter(ctx, chapterID)
	if err != nil {
		return domain.Briefing{}, fmt.Errorf("load chapter %d: %w", chapterID, err)
	}
	book, err := c.src.GetBook(ctx, chap.BookID)
	if err != nil {
		return domain.Briefing{}, fmt.Errorf("load book %d: %w", chap.BookID, err)
	}
	vol, err := c.src.GetVolume(ctx, chap.VolumeID)
	if err != nil {
		return domain.Briefing{}, fmt.Errorf("load volume %d: %w", chap.VolumeID, err)
	}
	beats, err := c.src.ListBeats(ctx, chapterID)
	if err != nil {
		return domain.Briefing{}, fmt.Errorf("list beats for chapter %d: %w", chapterID, err)
	}
	invariants, err := c.src.ListInvariants(ctx, book.ID)
	if err != nil {
		return domain.Briefing{}, fmt.Errorf("list invariants for book %d: %w", book.ID, err)
	}

	// 保证确定性排序：节拍按 seq 升序，世界铁律按 id 升序。
	beats = slices.Clone(beats)
	slices.SortFunc(beats, func(a, b domain.Beat) int {
		if a.Seq != b.Seq {
			return a.Seq - b.Seq
		}
		return int(a.ID - b.ID)
	})
	invariants = slices.Clone(invariants)
	slices.SortFunc(invariants, func(a, b domain.Entity) int {
		return int(a.ID - b.ID)
	})

	// 收集 P2 候选实体 ID（POV > Pinned > Beat 关联，剔除 Excluded）。
	p2IDs, highPrioritySet := collectP2EntityIDs(beats, opts.PinnedEntityIDs, opts.ExcludedEntityIDs)
	var fetchedEntities []domain.Entity
	if len(p2IDs) > 0 {
		fetchedEntities, err = c.src.GetEntitiesByIDs(ctx, p2IDs)
		if err != nil {
			return domain.Briefing{}, fmt.Errorf("load referenced entities: %w", err)
		}
	}

	entityByID := make(map[int64]domain.Entity, len(fetchedEntities)+len(invariants))
	for _, inv := range invariants {
		entityByID[inv.ID] = inv
	}
	for _, ent := range fetchedEntities {
		entityByID[ent.ID] = ent
	}

	p2Candidates := make([]p2Candidate, 0, len(p2IDs))
	for _, id := range p2IDs {
		ent, ok := entityByID[id]
		if !ok {
			continue
		}
		p2Candidates = append(p2Candidates, p2Candidate{
			entity:       ent,
			highPriority: highPrioritySet[id],
		})
	}

	prevChaps, err := c.src.GetPreviousChapters(ctx, book.ID, chap.GlobalSeq, 2)
	if err != nil {
		return domain.Briefing{}, fmt.Errorf("load previous chapters: %w", err)
	}
	prevOrdered := normalizePreviousChapters(prevChaps, chap.GlobalSeq)

	totalBudget := opts.TokenBudget
	if totalBudget <= 0 {
		totalBudget = domain.DefaultBriefingTokenBudget
	}

	var warnings []string
	if len(beats) == 0 {
		warnings = append(warnings, "本章尚未编排节拍卡，建议至少配置 3 张节拍卡以稳住推进节奏")
	}

	// P0（永不裁剪）：系统指令 + 书籍核心设定 + 题材规范 + 全部世界铁律。
	p0Content := buildP0Content(book, vol, invariants)
	p0Est := EstimateTokens(p0Content)
	p0Slot := domain.BriefingSlot{
		Key:         "p0_core",
		Priority:    "P0",
		Title:       "系统指令、题材规范与世界铁律",
		Content:     p0Content,
		TokenBudget: scaledBudget(totalBudget, defaultP0Budget),
		TokenEst:    p0Est,
		Truncated:   false,
		Required:    true,
	}

	// P1（永不裁剪）：本章标题/情绪/钩子/节拍卡/作者附加指令。
	p1Content := buildP1Content(book, chap, beats, entityByID, opts.ExtraInstruction)
	p1Est := EstimateTokens(p1Content)
	p1Slot := domain.BriefingSlot{
		Key:         "p1_chapter_beats",
		Priority:    "P1",
		Title:       "本章目标、情绪钩子与节拍卡",
		Content:     p1Content,
		TokenBudget: scaledBudget(totalBudget, defaultP1Budget),
		TokenEst:    p1Est,
		Truncated:   false,
		Required:    true,
	}

	remaining := totalBudget - p0Est - p1Est
	if remaining < 0 {
		warnings = append(warnings, "P0/P1 必选内容已超出总 Token 预算，已完整保留铁律与节拍并压缩可选上下文槽位")
		remaining = 0
	}

	// P2（高优）：出场实体与钉选实体档案，超限时优先保留 POV/钉选详情，其余压缩或裁剪。
	p2Nominal := max(defaultP2Budget, totalBudget*defaultP2Budget/domain.DefaultBriefingTokenBudget)
	p2Cap := min(p2Nominal, remaining)
	p2Content, includedEntityIDs, p2Truncated := buildP2Content(p2Candidates, p2Cap)
	p2Est := EstimateTokens(p2Content)
	if p2Truncated {
		warnings = append(warnings, "P2 出场实体档案因预算受限已压缩或裁剪部分低优先级实体")
	}
	p2Slot := domain.BriefingSlot{
		Key:         "p2_entities",
		Priority:    "P2",
		Title:       "出场实体档案",
		Content:     p2Content,
		TokenBudget: p2Cap,
		TokenEst:    p2Est,
		Truncated:   p2Truncated,
		Required:    false,
	}
	remaining -= p2Est
	if remaining < 0 {
		remaining = 0
	}

	// P4（衔接）：上一章尾部 ~1200 字 + 上两章尾部 ~400 字，预算紧张时优先收缩上两章再收缩上一章头部。
	p4Nominal := max(defaultP4Budget, totalBudget*defaultP4Budget/domain.DefaultBriefingTokenBudget)
	p4Cap := min(p4Nominal, remaining)
	p4Content, p4Truncated := buildP4Content(prevOrdered, p4Cap)
	p4Est := EstimateTokens(p4Content)
	if p4Truncated {
		warnings = append(warnings, "P4 前文衔接片段已按预算自动收缩尾部字数")
	}
	p4Slot := domain.BriefingSlot{
		Key:         "p4_recent_tail",
		Priority:    "P4",
		Title:       "前文衔接片段",
		Content:     p4Content,
		TokenBudget: p4Cap,
		TokenEst:    p4Est,
		Truncated:   p4Truncated,
		Required:    false,
	}

	slots := []domain.BriefingSlot{p0Slot, p1Slot, p2Slot, p4Slot}
	totalEst := p0Est + p1Est + p2Est + p4Est

	// 装配最终发送给模型的 SystemPrompt（P0）与 UserPrompt（P2 -> P4 -> P1，把本章硬指令放末尾增强遵循度）。
	userSections := make([]string, 0, 3)
	if p2Content != "" {
		userSections = append(userSections, p2Content)
	}
	if p4Content != "" {
		userSections = append(userSections, p4Content)
	}
	if p1Content != "" {
		userSections = append(userSections, p1Content)
	}

	return domain.Briefing{
		ChapterID:        chap.ID,
		ChapterSeq:       chap.GlobalSeq,
		BookTitle:        book.Title,
		Genre:            book.Genre,
		TotalTokenBudget: totalBudget,
		TotalTokenEst:    totalEst,
		SystemPrompt:     p0Content,
		UserPrompt:       strings.Join(userSections, "\n\n"),
		Slots:            slots,
		IncludedEntities: includedEntityIDs,
		Warnings:         warnings,
		CompiledAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func scaledBudget(totalBudget, nominal int) int {
	if totalBudget >= domain.DefaultBriefingTokenBudget {
		return max(nominal, totalBudget*nominal/domain.DefaultBriefingTokenBudget)
	}
	return nominal
}

func collectP2EntityIDs(beats []domain.Beat, pinnedIDs, excludedIDs []int64) ([]int64, map[int64]bool) {
	excluded := make(map[int64]bool, len(excludedIDs))
	for _, id := range excludedIDs {
		if id > 0 {
			excluded[id] = true
		}
	}

	seen := make(map[int64]bool)
	highPriority := make(map[int64]bool)
	ordered := make([]int64, 0)

	add := func(id int64, isHigh bool) {
		if id <= 0 || excluded[id] {
			return
		}
		if isHigh {
			highPriority[id] = true
		}
		if seen[id] {
			return
		}
		seen[id] = true
		ordered = append(ordered, id)
	}

	// 1. POV 实体最高优先级
	for _, b := range beats {
		if b.POVEntityID != nil {
			add(*b.POVEntityID, true)
		}
	}
	// 2. 作者显式钉选实体
	for _, id := range pinnedIDs {
		add(id, true)
	}
	// 3. 节拍卡出场实体
	for _, b := range beats {
		for _, id := range b.EntityIDs {
			add(id, false)
		}
	}
	return ordered, highPriority
}

func normalizePreviousChapters(chaps []domain.Chapter, currentSeq int) []domain.Chapter {
	out := make([]domain.Chapter, 0, len(chaps))
	for _, ch := range chaps {
		if ch.GlobalSeq >= currentSeq {
			continue
		}
		if strings.TrimSpace(ch.Content) == "" {
			continue
		}
		out = append(out, ch)
	}
	// 无论底层 store 返回升序还是降序，统一排成 GlobalSeq 降序（[0]=N-1, [1]=N-2）。
	slices.SortFunc(out, func(a, b domain.Chapter) int {
		return b.GlobalSeq - a.GlobalSeq
	})
	if len(out) > 2 {
		out = out[:2]
	}
	return out
}

func buildP0Content(book domain.Book, vol domain.Volume, invariants []domain.Entity) string {
	var b strings.Builder
	b.WriteString("【写作引擎系统指令】\n")
	b.WriteString("你是专业中文男频长篇网文写作引擎。请严格遵循题材规则、世界铁律与本章节拍卡直接输出章节正文。\n")
	b.WriteString("输出约束：\n")
	b.WriteString("- 只输出纯净小说正文，不要输出“好的”“以下是正文”等客套话、不要输出 Markdown 标题或节拍复述。\n")
	b.WriteString("- 严禁违背世界铁律与已出场实体设定，人物对话、资源消耗与招式结果保持强因果自洽。\n")
	b.WriteString("- 保持中文手机端网文短段节奏，动作与对话优先于空洞心理总结。\n\n")

	targetWords := book.TargetChapterWords
	if targetWords <= 0 {
		targetWords = domain.DefaultTargetChapterWords
	}
	b.WriteString("【书籍核心设定】\n")
	fmt.Fprintf(&b, "书名：%s\n", book.Title)
	fmt.Fprintf(&b, "目标章字数：约 %d 字\n", targetWords)
	if strings.TrimSpace(book.Premise) != "" {
		fmt.Fprintf(&b, "核心卖点与主线：%s\n", strings.TrimSpace(book.Premise))
	}
	if strings.TrimSpace(book.GoldenFinger) != "" {
		fmt.Fprintf(&b, "金手指与规则：%s\n", strings.TrimSpace(book.GoldenFinger))
	}
	if vol.Title != "" || vol.Goal != "" || vol.ClimaxPlan != "" {
		fmt.Fprintf(&b, "当前卷：第%d卷 %s\n", vol.Seq, vol.Title)
		if strings.TrimSpace(vol.Goal) != "" {
			fmt.Fprintf(&b, "卷阶段目标：%s\n", strings.TrimSpace(vol.Goal))
		}
		if strings.TrimSpace(vol.ClimaxPlan) != "" {
			fmt.Fprintf(&b, "卷高潮规划：%s\n", strings.TrimSpace(vol.ClimaxPlan))
		}
	}

	b.WriteString("\n")
	b.WriteString(genres.PromptBlock(book.Genre))

	if len(invariants) > 0 {
		b.WriteString("\n\n【世界铁律（全书硬约束，严禁违背）】\n")
		for _, inv := range invariants {
			b.WriteString(formatEntityDetailed(inv))
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func buildP1Content(
	book domain.Book,
	chap domain.Chapter,
	beats []domain.Beat,
	entityByID map[int64]domain.Entity,
	extraInstruction string,
) string {
	var b strings.Builder
	b.WriteString("【本章创作任务与节拍编排】\n")
	fmt.Fprintf(&b, "章节：第%d章《%s》\n", chap.GlobalSeq, chap.Title)
	fmt.Fprintf(&b, "主情绪极性：%s（%s）\n", chap.EmotionTag.Label(), chap.EmotionTag)
	fmt.Fprintf(&b, "章末钩子类型：%s（%s）\n", chap.HookType.Label(), chap.HookType)
	if strings.TrimSpace(chap.HookNote) != "" {
		fmt.Fprintf(&b, "章末钩子落点要求：%s\n", strings.TrimSpace(chap.HookNote))
	}
	if strings.TrimSpace(chap.OutlineSummary) != "" {
		fmt.Fprintf(&b, "本章概要：%s\n", strings.TrimSpace(chap.OutlineSummary))
	}

	beatWordTotal := 0
	for _, bt := range beats {
		if bt.TargetWords > 0 {
			beatWordTotal += bt.TargetWords
		}
	}
	targetWords := book.TargetChapterWords
	if targetWords <= 0 {
		targetWords = domain.DefaultTargetChapterWords
	}
	if beatWordTotal > 0 {
		targetWords = beatWordTotal
	}
	fmt.Fprintf(&b, "本章目标字数：约 %d 字\n", targetWords)

	if len(beats) > 0 {
		b.WriteString("\n【按顺序执行以下节拍卡】\n")
		for i, bt := range beats {
			seq := bt.Seq
			if seq <= 0 {
				seq = i + 1
			}
			words := bt.TargetWords
			if words <= 0 {
				words = domain.DefaultBeatTargetWords
			}
			fmt.Fprintf(&b, "%d. [%s]（约 %d 字）%s", seq, bt.Stage.Label(), words, strings.TrimSpace(bt.Summary))
			if strings.TrimSpace(bt.ConflictNote) != "" {
				fmt.Fprintf(&b, "\n   核心冲突/博弈：%s", strings.TrimSpace(bt.ConflictNote))
			}
			if bt.POVEntityID != nil && *bt.POVEntityID > 0 {
				if ent, ok := entityByID[*bt.POVEntityID]; ok {
					fmt.Fprintf(&b, "\n   视角人物(POV)：%s", ent.Name)
				}
			}
			if len(bt.EntityIDs) > 0 {
				names := make([]string, 0, len(bt.EntityIDs))
				for _, eid := range bt.EntityIDs {
					if ent, ok := entityByID[eid]; ok {
						names = append(names, ent.Name)
					}
				}
				if len(names) > 0 {
					fmt.Fprintf(&b, "\n   出场实体：%s", strings.Join(names, "、"))
				}
			}
			b.WriteString("\n")
		}
	}

	if trimmed := strings.TrimSpace(extraInstruction); trimmed != "" {
		b.WriteString("\n【作者本轮附加指令】\n")
		b.WriteString(trimmed)
		b.WriteString("\n")
	}

	b.WriteString("\n请严格按以上节拍顺序直接撰写本章完整正文，确保结尾 200 字准确兑现章末钩子。")
	return strings.TrimSpace(b.String())
}

func buildP2Content(candidates []p2Candidate, budget int) (string, []int64, bool) {
	if len(candidates) == 0 {
		return "", []int64{}, false
	}

	const header = "【本章节拍出场与钉选实体档案】\n"
	if budget <= EstimateTokens(header) {
		return "", []int64{}, true
	}

	// 第 1 档：全量详细档案
	fullLines := make([]string, len(candidates))
	allIDs := make([]int64, len(candidates))
	for i, cand := range candidates {
		fullLines[i] = formatEntityDetailed(cand.entity)
		allIDs[i] = cand.entity.ID
	}
	fullText := strings.TrimSpace(header + strings.Join(fullLines, "\n"))
	if EstimateTokens(fullText) <= budget {
		return fullText, allIDs, false
	}

	// 第 2 档：POV/钉选保留详细属性，其余实体压缩为单行摘要
	mixedLines := make([]string, len(candidates))
	for i, cand := range candidates {
		if cand.highPriority {
			mixedLines[i] = formatEntityDetailed(cand.entity)
		} else {
			mixedLines[i] = formatEntityCompact(cand.entity)
		}
	}
	mixedText := strings.TrimSpace(header + strings.Join(mixedLines, "\n"))
	if EstimateTokens(mixedText) <= budget {
		return mixedText, allIDs, true
	}

	// 第 3 档：预算仍吃紧，按优先级逐条装配（优先详细/精简高优实体，再逐个追加其余精简实体）。
	keptLines := make([]string, 0, len(candidates))
	keptIDs := make([]int64, 0, len(candidates))
	for _, cand := range candidates {
		line := formatEntityCompact(cand.entity)
		if cand.highPriority {
			detailed := formatEntityDetailed(cand.entity)
			candidateText := strings.TrimSpace(header + strings.Join(append(slices.Clone(keptLines), detailed), "\n"))
			if EstimateTokens(candidateText) <= budget {
				keptLines = append(keptLines, detailed)
				keptIDs = append(keptIDs, cand.entity.ID)
				continue
			}
		}
		candidateText := strings.TrimSpace(header + strings.Join(append(slices.Clone(keptLines), line), "\n"))
		if EstimateTokens(candidateText) <= budget {
			keptLines = append(keptLines, line)
			keptIDs = append(keptIDs, cand.entity.ID)
			continue
		}
		// 若连第一条高优实体都超过单槽剩余预算，按 Token 截断单行以尽量保留核心标识。
		if len(keptLines) == 0 {
			rem := budget - EstimateTokens(header)
			if truncated := truncateHeadWithinTokens(line, rem); truncated != "" {
				keptLines = append(keptLines, truncated)
				keptIDs = append(keptIDs, cand.entity.ID)
			}
		}
		break
	}

	if len(keptLines) == 0 {
		return "", []int64{}, true
	}
	return strings.TrimSpace(header + strings.Join(keptLines, "\n")), keptIDs, true
}

func buildP4Content(prevOrdered []domain.Chapter, budget int) (string, bool) {
	if len(prevOrdered) == 0 {
		return "", false
	}

	const slotHeader = "【前文结尾衔接（保持场景与语感无缝衔接，勿重复前文已发生剧情）】\n"
	prev1 := prevOrdered[0]
	tail1, _ := tailRunes(prev1.Content, prevChapter1TailRunes)
	prev1Header := fmt.Sprintf("── 上一章（第%d章《%s》）结尾 ──\n", prev1.GlobalSeq, prev1.Title)

	var hasPrev2 bool
	var prev2Header, tail2 string
	if len(prevOrdered) > 1 {
		prev2 := prevOrdered[1]
		tail2, _ = tailRunes(prev2.Content, prevChapter2TailRunes)
		if tail2 != "" {
			hasPrev2 = true
			prev2Header = fmt.Sprintf("── 上两章（第%d章《%s》）结尾 ──\n", prev2.GlobalSeq, prev2.Title)
		}
	}

	renderFull := func(t2, t1 string) string {
		var parts []string
		if t2 != "" {
			parts = append(parts, prev2Header+t2)
		}
		if t1 != "" {
			parts = append(parts, prev1Header+t1)
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.TrimSpace(slotHeader + strings.Join(parts, "\n\n"))
	}

	// 标准窗口（N-1 1200 字 + N-2 400 字）未超预算 -> 完整输出且 Truncated=false。
	full := renderFull(tail2, tail1)
	if EstimateTokens(full) <= budget {
		return full, false
	}

	if budget <= EstimateTokens(slotHeader) {
		return "", true
	}

	// 超预算时先保留完整上一章（N-1），收缩或舍弃上两章（N-2）。
	onlyPrev1 := renderFull("", tail1)
	if EstimateTokens(onlyPrev1) <= budget {
		if hasPrev2 {
			// 计算剩余给 N-2 的 Token 配额并尝试保留 N-2 末尾片段。
			remForPrev2 := budget - EstimateTokens(onlyPrev1) - 4
			if fitted2, kept2 := fitTailWithinTokens(prev2Header, tail2, remForPrev2); kept2 > 0 {
				combined := strings.TrimSpace(slotHeader + fitted2 + "\n\n" + prev1Header + tail1)
				if EstimateTokens(combined) <= budget {
					return combined, true
				}
			}
		}
		return onlyPrev1, true
	}

	// 若仅上一章 1200 字仍超预算，则从头收缩上一章保留最末尾片段。
	remForPrev1 := budget - EstimateTokens(slotHeader)
	fitted1, kept1 := fitTailWithinTokens(prev1Header, tail1, remForPrev1)
	if kept1 == 0 {
		return "", true
	}
	return strings.TrimSpace(slotHeader + fitted1), true
}

func formatEntityDetailed(e domain.Entity) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- [%s] %s", e.Kind.Label(), e.Name)
	if len(e.Aliases) > 0 {
		fmt.Fprintf(&b, "（别名：%s）", strings.Join(e.Aliases, "/"))
	}
	if strings.TrimSpace(e.Summary) != "" {
		fmt.Fprintf(&b, "：%s", strings.TrimSpace(e.Summary))
	}
	if len(e.Attributes) > 0 {
		keys := sortedMapKeys(e.Attributes)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			v := strings.TrimSpace(e.Attributes[k])
			if v == "" {
				continue
			}
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
		}
		if len(pairs) > 0 {
			fmt.Fprintf(&b, "\n  属性：%s", strings.Join(pairs, "；"))
		}
	}
	return b.String()
}

func formatEntityCompact(e domain.Entity) string {
	var parts []string
	if strings.TrimSpace(e.Summary) != "" {
		parts = append(parts, strings.TrimSpace(e.Summary))
	}
	if len(e.Attributes) > 0 {
		keys := sortedMapKeys(e.Attributes)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			v := strings.TrimSpace(e.Attributes[k])
			if v == "" {
				continue
			}
			pairs = append(pairs, fmt.Sprintf("%s:%s", k, v))
		}
		if len(pairs) > 0 {
			parts = append(parts, strings.Join(pairs, "; "))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("- [%s] %s", e.Kind.Label(), e.Name)
	}
	return fmt.Sprintf("- [%s] %s：%s", e.Kind.Label(), e.Name, strings.Join(parts, " | "))
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
