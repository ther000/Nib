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
	defaultP3Budget       = 800
	defaultP4Budget       = 1600
	defaultP5Budget       = 2800
	defaultP6Budget       = 1000
	defaultP7Budget       = 1200
	prevChapter1TailRunes = 1200
	prevChapter2TailRunes = 400
	recentCausalLimit     = 15
	maxSearchHits         = 3
	maxStyleSampleAnchors = 2
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

// MemoryDataSource 定义 Phase 2 动态记忆编译所需的只读扩展契约（直接由 *store.Store 满足）。
// 当传入 New(src) 的 src 同时实现该接口（或通过 WithMemorySource 显式注入）时，
// 编译器自动启用 P2 事件溯源快照、P3 伏笔追踪、P5 摘要金字塔与 P6 防剧透 FTS5 检索。
type MemoryDataSource interface {
	SnapshotAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntitySnapshot, error)
	ListForeshadowings(ctx context.Context, bookID int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error)
	ListArcSummaries(ctx context.Context, bookID int64, level domain.ArcSummaryLevel) ([]domain.ArcSummary, error)
	SearchMemory(ctx context.Context, bookID int64, query string, maxChapterSeq int) ([]domain.MemorySearchHit, error)
}

// StyleDataSource 定义 Phase 3 文风指纹、场景样章锚点与去 AI 味黑名单规则的只读契约（直接由 *store.Store 满足）。
type StyleDataSource interface {
	GetStyleProfile(ctx context.Context, bookID int64) (domain.StyleProfile, error)
	ListStyleSamples(ctx context.Context, bookID int64, sceneType domain.SceneType) ([]domain.StyleSample, error)
	ListDeaiRules(ctx context.Context, bookID int64) ([]domain.DeaiRule, error)
}

// RelationDataSource 定义实体关系网只读契约（直接由 *store.Store 自动满足），用于 P2 注入角色/势力关系。
type RelationDataSource interface {
	ListEntityRelations(ctx context.Context, bookID int64) ([]domain.EntityRelation, error)
}

// Option 用于定制 Compiler 可选扩展依赖。
type Option func(*Compiler)

// WithMemorySource 显式注入 Phase 2 记忆数据源。
func WithMemorySource(mem MemoryDataSource) Option {
	return func(c *Compiler) {
		if mem != nil {
			c.mem = mem
		}
	}
}

// WithStyleSource 显式注入 Phase 3 文风与去 AI 味规则数据源。
func WithStyleSource(style StyleDataSource) Option {
	return func(c *Compiler) {
		if style != nil {
			c.style = style
		}
	}
}

// Compiler 负责将书卷设定、世界铁律、节拍卡、实体快照、伏笔、前文衔接、摘要金字塔、精准检索与文风样章编译为透明的作战简报。
type Compiler struct {
	src       DataSource
	mem       MemoryDataSource
	style     StyleDataSource
	relations RelationDataSource
}

// New 创建上下文编译器实例。若 src 本身实现了 MemoryDataSource / StyleDataSource / RelationDataSource，将自动激活对应槽位。
func New(src DataSource, opts ...Option) *Compiler {
	c := &Compiler{src: src}
	if ms, ok := src.(MemoryDataSource); ok {
		c.mem = ms
	}
	if ss, ok := src.(StyleDataSource); ok {
		c.style = ss
	}
	if rs, ok := src.(RelationDataSource); ok {
		c.relations = rs
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

type p2Candidate struct {
	entity       domain.Entity
	highPriority bool // POV 或作者显式钉选实体
	appliedCount int
	pendingCount int
}

// Compile 按 P0/P1（永不裁剪）→ P2（实体状态快照）→ P3（未回收伏笔）→ P4（前文结尾衔接）→
// P5（摘要金字塔）→ P6（前文精确检索）优先级编译确定性作战简报。
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

	prevSeq := chap.GlobalSeq - 1
	if prevSeq < 0 {
		prevSeq = 0
	}

	// 收集 P2 候选实体 ID（POV > Pinned > Beat 关联，剔除 Excluded）。
	p2IDs, highPrioritySet := collectP2EntityIDs(beats, opts.PinnedEntityIDs, opts.ExcludedEntityIDs)

	entityByID := make(map[int64]domain.Entity, len(invariants)+len(p2IDs))
	for _, inv := range invariants {
		entityByID[inv.ID] = inv
	}

	p2Candidates := make([]p2Candidate, 0, len(p2IDs))
	if c.mem != nil {
		snapshots, snapErr := c.mem.SnapshotAt(ctx, book.ID, prevSeq, opts.IncludePending)
		if snapErr != nil {
			return domain.Briefing{}, fmt.Errorf("load entity snapshots at chapter %d: %w", prevSeq, snapErr)
		}
		snapByID := make(map[int64]domain.EntitySnapshot, len(snapshots))
		for _, snap := range snapshots {
			ent := applySnapshotAttributes(snap)
			entityByID[ent.ID] = ent
			snapByID[ent.ID] = snap
		}
		for _, id := range p2IDs {
			snap, ok := snapByID[id]
			if !ok {
				continue
			}
			p2Candidates = append(p2Candidates, p2Candidate{
				entity:       entityByID[id],
				highPriority: highPrioritySet[id],
				appliedCount: snap.AppliedEventCount,
				pendingCount: snap.PendingEventCount,
			})
		}
	} else if len(p2IDs) > 0 {
		fetchedEntities, fetchErr := c.src.GetEntitiesByIDs(ctx, p2IDs)
		if fetchErr != nil {
			return domain.Briefing{}, fmt.Errorf("load referenced entities: %w", fetchErr)
		}
		for _, ent := range fetchedEntities {
			entityByID[ent.ID] = ent
		}
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
	}

	// 一次查询最多 15 章前文：同时服务 P4（近 2 章正文尾巴）与 P5（近 15 章因果摘要）。
	prevAll, err := c.src.GetPreviousChapters(ctx, book.ID, chap.GlobalSeq, recentCausalLimit)
	if err != nil {
		return domain.Briefing{}, fmt.Errorf("load previous chapters: %w", err)
	}
	prevOrderedForTail := normalizePreviousChaptersForTail(prevAll, chap.GlobalSeq)
	prevOrderedForSummary := normalizePreviousChaptersForSummary(prevAll, chap.GlobalSeq)

	// 加载 Phase 2 可选数据：未回收伏笔（P3）、弧线/卷摘要（P5）、防剧透记忆检索（P6）。
	var allForeshadowings []domain.Foreshadowing
	var arcSummaries []domain.ArcSummary
	var searchHits []domain.MemorySearchHit
	if c.mem != nil {
		allForeshadowings, err = c.mem.ListForeshadowings(ctx, book.ID, "")
		if err != nil {
			return domain.Briefing{}, fmt.Errorf("load foreshadowings: %w", err)
		}
		arcSummaries, err = c.mem.ListArcSummaries(ctx, book.ID, "")
		if err != nil {
			return domain.Briefing{}, fmt.Errorf("load arc summaries: %w", err)
		}
		if prevSeq > 0 {
			keywords := buildSearchKeywords(opts.SearchKeywords, p2Candidates, beats)
			searchHits, err = runSpoilerFreeMemorySearch(ctx, c.mem, book.ID, prevSeq, keywords)
			if err != nil {
				return domain.Briefing{}, fmt.Errorf("search memory: %w", err)
			}
		}
	}

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

	// P2（高优）：出场实体与钉选实体状态快照 + 关联实体关系网。
	p2Cap := min(scaledBudget(totalBudget, defaultP2Budget), remaining)
	p2Content, includedEntityIDs, p2Truncated := buildP2Content(p2Candidates, p2Cap)
	if c.relations != nil && len(includedEntityIDs) > 0 && p2Cap > EstimateTokens(p2Content)+40 {
		if rels, rErr := c.relations.ListEntityRelations(ctx, book.ID); rErr == nil && len(rels) > 0 {
			if relText := formatRelevantRelations(rels, includedEntityIDs, p2Cap-EstimateTokens(p2Content)); relText != "" {
				if p2Content != "" {
					p2Content += "\n\n" + relText
				} else {
					p2Content = relText
				}
			}
		}
	}
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
	remaining = max(0, remaining-p2Est)

	// P3（高优）：未回收与临期/超期伏笔（按超期紧迫度与出场实体关联排序）。
	p3Cap := min(scaledBudget(totalBudget, defaultP3Budget), remaining)
	p3Content, p3Truncated, overdueCount := buildP3Content(allForeshadowings, chap.GlobalSeq, includedEntityIDs, p3Cap)
	p3Est := EstimateTokens(p3Content)
	if overdueCount > 0 {
		warnings = append(warnings, fmt.Sprintf("当前有 %d 条未回收伏笔已超过计划回收章号，建议在本章或近期章安排收网", overdueCount))
	}
	if p3Truncated {
		warnings = append(warnings, "P3 未回收伏笔因预算受限已按紧迫度截断低优伏笔")
	}
	p3Slot := domain.BriefingSlot{
		Key:         "p3_foreshadowings",
		Priority:    "P3",
		Title:       "未回收与临期伏笔",
		Content:     p3Content,
		TokenBudget: p3Cap,
		TokenEst:    p3Est,
		Truncated:   p3Truncated,
		Required:    false,
	}
	remaining = max(0, remaining-p3Est)

	// P4（衔接）：上一章尾部 ~1200 字 + 上两章尾部 ~400 字。
	p4Cap := min(scaledBudget(totalBudget, defaultP4Budget), remaining)
	p4Content, p4Truncated := buildP4Content(prevOrderedForTail, p4Cap)
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
	remaining = max(0, remaining-p4Est)

	// P5（记忆）：近 15 章因果摘要 + 10 章弧线摘要 + 历史卷摘要（紧张时优先从最远单章摘要向上收缩）。
	p5Cap := min(scaledBudget(totalBudget, defaultP5Budget), remaining)
	p5Content, p5Truncated := buildP5Content(prevOrderedForSummary, arcSummaries, prevSeq, p5Cap)
	p5Est := EstimateTokens(p5Content)
	if p5Truncated {
		warnings = append(warnings, "P5 历史摘要金字塔已按预算从较远章节起收缩")
	}
	p5Slot := domain.BriefingSlot{
		Key:         "p5_summary_pyramid",
		Priority:    "P5",
		Title:       "历史摘要金字塔",
		Content:     p5Content,
		TokenBudget: p5Cap,
		TokenEst:    p5Est,
		Truncated:   p5Truncated,
		Required:    false,
	}
	remaining = max(0, remaining-p5Est)

	// P6（检索）：最多 3 条防剧透 FTS5/短词前文检索命中片段。
	p6Cap := min(scaledBudget(totalBudget, defaultP6Budget), remaining)
	p6Content, p6Truncated := buildP6Content(searchHits, p6Cap)
	p6Est := EstimateTokens(p6Content)
	if p6Truncated {
		warnings = append(warnings, "P6 前文精确检索片段已按预算截断低相关度命中")
	}
	p6Slot := domain.BriefingSlot{
		Key:         "p6_memory_search",
		Priority:    "P6",
		Title:       "前文精确检索片段",
		Content:     p6Content,
		TokenBudget: p6Cap,
		TokenEst:    p6Est,
		Truncated:   p6Truncated,
		Required:    false,
	}
	remaining = max(0, remaining-p6Est)

	// P7（文风）：Style DNA 量化指标 + 去 AI 味黑名单要点 + 匹配当前主导节拍场景的 1~2 段样章锚点。
	p7Cap := min(scaledBudget(totalBudget, defaultP7Budget), remaining)
	var p7Content string
	var p7Truncated bool
	if c.style != nil {
		p7Content, p7Truncated, err = c.compileP7StyleSlot(ctx, book.ID, chap, beats, p7Cap)
		if err != nil {
			return domain.Briefing{}, fmt.Errorf("compile P7 style slot: %w", err)
		}
	}
	p7Est := EstimateTokens(p7Content)
	if p7Truncated {
		warnings = append(warnings, "P7 文风指纹与场景样章已按预算缩减样章锚点数量")
	}
	p7Slot := domain.BriefingSlot{
		Key:         "p7_style_dna",
		Priority:    "P7",
		Title:       "文风指纹与场景样章",
		Content:     p7Content,
		TokenBudget: p7Cap,
		TokenEst:    p7Est,
		Truncated:   p7Truncated,
		Required:    false,
	}

	slots := buildOrderedSlots(c.mem != nil, c.style != nil, p0Slot, p1Slot, p2Slot, p3Slot, p4Slot, p5Slot, p6Slot, p7Slot)
	totalEst := 0
	for _, s := range slots {
		totalEst += s.TokenEst
	}

	// 装配最终发送给模型的 SystemPrompt（P0）与 UserPrompt：
	// 长程设定与记忆（P2 -> P3 -> P5 -> P6 -> P7）→ 紧邻前文尾部（P4）→ 本章目标与节拍（P1，放末尾强化遵循）。
	userSections := make([]string, 0, 7)
	for _, sec := range []string{p2Content, p3Content, p5Content, p6Content, p7Content, p4Content, p1Content} {
		if sec != "" {
			userSections = append(userSections, sec)
		}
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

func buildOrderedSlots(
	hasMemory bool,
	hasStyle bool,
	p0, p1, p2, p3, p4, p5, p6, p7 domain.BriefingSlot,
) []domain.BriefingSlot {
	if hasMemory && hasStyle {
		return []domain.BriefingSlot{p0, p1, p2, p3, p4, p5, p6, p7}
	}
	if hasMemory {
		return []domain.BriefingSlot{p0, p1, p2, p3, p4, p5, p6}
	}
	// 无 MemoryDataSource 的纯 Phase 1 模式下保留核心槽位（若有因果摘要则附带 P5，若有 StyleDataSource 则附带 P7）。
	slots := []domain.BriefingSlot{p0, p1, p2, p4}
	if p5.Content != "" || p5.Truncated {
		slots = append(slots, p5)
	}
	if hasStyle {
		slots = append(slots, p7)
	}
	return slots
}

func applySnapshotAttributes(snap domain.EntitySnapshot) domain.Entity {
	ent := snap.Entity
	if len(snap.EffectiveAttributes) > 0 {
		merged := make(map[string]string, len(snap.EffectiveAttributes))
		for k, v := range snap.EffectiveAttributes {
			merged[k] = v
		}
		ent.Attributes = merged
	}
	return ent
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

func normalizePreviousChaptersForTail(chaps []domain.Chapter, currentSeq int) []domain.Chapter {
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

func normalizePreviousChaptersForSummary(chaps []domain.Chapter, currentSeq int) []domain.Chapter {
	out := make([]domain.Chapter, 0, len(chaps))
	for _, ch := range chaps {
		if ch.GlobalSeq >= currentSeq {
			continue
		}
		if strings.TrimSpace(ch.CausalSummary) == "" {
			continue
		}
		out = append(out, ch)
	}
	// 先按 GlobalSeq 降序截取最近 15 章，再按时间升序排列供阅读。
	slices.SortFunc(out, func(a, b domain.Chapter) int {
		return b.GlobalSeq - a.GlobalSeq
	})
	if len(out) > recentCausalLimit {
		out = out[:recentCausalLimit]
	}
	slices.SortFunc(out, func(a, b domain.Chapter) int {
		return a.GlobalSeq - b.GlobalSeq
	})
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

	const header = "【本章节拍出场与钉选实体状态快照】\n"
	if budget <= EstimateTokens(header) {
		return "", []int64{}, true
	}

	// 第 1 档：全量详细档案
	fullLines := make([]string, len(candidates))
	allIDs := make([]int64, len(candidates))
	for i, cand := range candidates {
		fullLines[i] = formatCandidateDetailed(cand)
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
			mixedLines[i] = formatCandidateDetailed(cand)
		} else {
			mixedLines[i] = formatEntityCompact(cand.entity)
		}
	}
	mixedText := strings.TrimSpace(header + strings.Join(mixedLines, "\n"))
	if EstimateTokens(mixedText) <= budget {
		return mixedText, allIDs, true
	}

	// 第 3 档：预算仍吃紧，按优先级逐条装配。
	keptLines := make([]string, 0, len(candidates))
	keptIDs := make([]int64, 0, len(candidates))
	for _, cand := range candidates {
		line := formatEntityCompact(cand.entity)
		if cand.highPriority {
			detailed := formatCandidateDetailed(cand)
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

func formatRelevantRelations(rels []domain.EntityRelation, includedEntityIDs []int64, remainingBudget int) string {
	if len(rels) == 0 || len(includedEntityIDs) == 0 || remainingBudget <= 30 {
		return ""
	}
	inc := make(map[int64]bool, len(includedEntityIDs))
	for _, id := range includedEntityIDs {
		inc[id] = true
	}
	lines := make([]string, 0, len(rels))
	for _, r := range rels {
		if !inc[r.SourceEntityID] && !inc[r.TargetEntityID] {
			continue
		}
		line := fmt.Sprintf("- %s —[%s]→ %s", r.SourceName, r.RelationType, r.TargetName)
		if d := strings.TrimSpace(r.Description); d != "" {
			line += "：" + d
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	header := "### 出场角色/势力关系网\n"
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		cand := header + strings.Join(append(slices.Clone(kept), l), "\n")
		if EstimateTokens(cand) > remainingBudget {
			break
		}
		kept = append(kept, l)
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.TrimSpace(header + strings.Join(kept, "\n"))
}

type scoredForeshadow struct {
	fs          domain.Foreshadowing
	overdue     bool
	overlapCnt  int
	urgencyDiff int // targetResolveSeq - currentSeq (越小越紧迫；未设目标则为极大值)
}

func buildP3Content(
	all []domain.Foreshadowing,
	currentSeq int,
	includedEntityIDs []int64,
	budget int,
) (string, bool, int) {
	includedSet := make(map[int64]bool, len(includedEntityIDs))
	for _, id := range includedEntityIDs {
		includedSet[id] = true
	}

	const noTargetDiff = 1_000_000
	candidates := make([]scoredForeshadow, 0, len(all))
	overdueCount := 0

	for _, fs := range all {
		if fs.Status != domain.ForeshadowingPlanted && fs.Status != domain.ForeshadowingHinted {
			continue
		}
		// 防剧透：绝不注入未来章节才埋设的伏笔。
		if fs.PlantedChapterSeq > 0 && fs.PlantedChapterSeq >= currentSeq {
			continue
		}

		overlap := 0
		for _, eid := range fs.InvolvedEntityIDs {
			if includedSet[eid] {
				overlap++
			}
		}
		overdue := fs.TargetResolveSeq > 0 && currentSeq > fs.TargetResolveSeq
		if overdue {
			overdueCount++
		}
		diff := noTargetDiff
		if fs.TargetResolveSeq > 0 {
			diff = fs.TargetResolveSeq - currentSeq
		}

		candidates = append(candidates, scoredForeshadow{
			fs:          fs,
			overdue:     overdue,
			overlapCnt:  overlap,
			urgencyDiff: diff,
		})
	}

	if len(candidates) == 0 {
		return "", false, 0
	}

	// 优先级排序：1) 超期伏笔优先（超期越久越靠前）→ 2) 关联本章节拍出场实体 → 3) 临近 target_resolve_seq → 4) ID 稳定序。
	slices.SortFunc(candidates, func(a, b scoredForeshadow) int {
		if a.overdue != b.overdue {
			if a.overdue {
				return -1
			}
			return 1
		}
		if a.overdue && b.overdue && a.urgencyDiff != b.urgencyDiff {
			return a.urgencyDiff - b.urgencyDiff
		}
		if (a.overlapCnt > 0) != (b.overlapCnt > 0) {
			if a.overlapCnt > 0 {
				return -1
			}
			return 1
		}
		if a.urgencyDiff != b.urgencyDiff {
			return a.urgencyDiff - b.urgencyDiff
		}
		if a.overlapCnt != b.overlapCnt {
			return b.overlapCnt - a.overlapCnt
		}
		return int(a.fs.ID - b.fs.ID)
	})

	const header = "【未回收伏笔与临期/超期预警】\n"
	if budget <= EstimateTokens(header) {
		return "", true, overdueCount
	}

	kept := make([]string, 0, len(candidates))
	truncated := false
	for _, cand := range candidates {
		line := formatForeshadowLine(cand, currentSeq)
		trial := strings.TrimSpace(header + strings.Join(append(slices.Clone(kept), line), "\n"))
		if EstimateTokens(trial) <= budget {
			kept = append(kept, line)
			continue
		}
		truncated = true
		if len(kept) == 0 {
			rem := budget - EstimateTokens(header)
			if cut := truncateHeadWithinTokens(line, rem); cut != "" {
				kept = append(kept, cut)
			}
		}
		break
	}
	if len(kept) == 0 {
		return "", true, overdueCount
	}
	return strings.TrimSpace(header + strings.Join(kept, "\n")), truncated, overdueCount
}

func formatForeshadowLine(cand scoredForeshadow, currentSeq int) string {
	fs := cand.fs
	var tag string
	switch {
	case cand.overdue:
		tag = fmt.Sprintf("【超期 %d 章】", currentSeq-fs.TargetResolveSeq)
	case fs.TargetResolveSeq > 0 && fs.TargetResolveSeq-currentSeq <= 3:
		tag = fmt.Sprintf("【临期·目标第%d章】", fs.TargetResolveSeq)
	case fs.TargetResolveSeq > 0:
		tag = fmt.Sprintf("【目标第%d章回收】", fs.TargetResolveSeq)
	default:
		tag = "【长期悬念】"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "- %s[%s] %s（第%d章埋设", tag, fs.Status.Label(), fs.Title, fs.PlantedChapterSeq)
	if fs.LastHintSeq > 0 {
		fmt.Fprintf(&b, "，第%d章暗示", fs.LastHintSeq)
	}
	b.WriteString("）")
	if strings.TrimSpace(fs.Description) != "" {
		fmt.Fprintf(&b, "：%s", strings.TrimSpace(fs.Description))
	}
	return b.String()
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

func buildP5Content(
	recentChapters []domain.Chapter,
	arcSummaries []domain.ArcSummary,
	maxSeq int,
	budget int,
) (string, bool) {
	// 过滤未来摘要（防剧透）并按层级拆分：卷摘要 + 10章弧线摘要 + 近15章单章因果摘要。
	var volLines []string
	var arcLines []string

	filteredArcs := make([]domain.ArcSummary, 0, len(arcSummaries))
	for _, a := range arcSummaries {
		if strings.TrimSpace(a.Summary) == "" {
			continue
		}
		if maxSeq > 0 && a.EndSeq > maxSeq {
			continue
		}
		if maxSeq == 0 {
			continue
		}
		filteredArcs = append(filteredArcs, a)
	}
	slices.SortFunc(filteredArcs, func(a, b domain.ArcSummary) int {
		if a.StartSeq != b.StartSeq {
			return a.StartSeq - b.StartSeq
		}
		return a.EndSeq - b.EndSeq
	})

	for _, a := range filteredArcs {
		switch a.Level {
		case domain.ArcSummaryLevelVolume:
			volLines = append(volLines, fmt.Sprintf("- [卷摘要 第%d~%d章] %s", a.StartSeq, a.EndSeq, strings.TrimSpace(a.Summary)))
		case domain.ArcSummaryLevelArc10:
			arcLines = append(arcLines, fmt.Sprintf("- [弧线摘要 第%d~%d章] %s", a.StartSeq, a.EndSeq, strings.TrimSpace(a.Summary)))
		}
	}

	chapLines := make([]string, 0, len(recentChapters))
	for _, ch := range recentChapters {
		chapLines = append(chapLines, fmt.Sprintf("- [第%d章《%s》] %s", ch.GlobalSeq, ch.Title, strings.TrimSpace(ch.CausalSummary)))
	}

	if len(volLines) == 0 && len(arcLines) == 0 && len(chapLines) == 0 {
		return "", false
	}

	const header = "【历史剧情摘要金字塔（卷摘要 / 10章弧线 / 近15章因果）】\n"
	render := func(vols, arcs, chaps []string) string {
		lines := make([]string, 0, len(vols)+len(arcs)+len(chaps))
		lines = append(lines, vols...)
		lines = append(lines, arcs...)
		lines = append(lines, chaps...)
		if len(lines) == 0 {
			return ""
		}
		return strings.TrimSpace(header + strings.Join(lines, "\n"))
	}

	full := render(volLines, arcLines, chapLines)
	if EstimateTokens(full) <= budget {
		return full, false
	}
	if budget <= EstimateTokens(header) {
		return "", true
	}

	// 按 docs/03 §1.3 裁剪策略：从最远的单章因果摘要开始向上收缩（保留最新单章与高浓缩弧线/卷摘要）。
	curChaps := slices.Clone(chapLines)
	for len(curChaps) > 0 {
		candidate := render(volLines, arcLines, curChaps)
		if EstimateTokens(candidate) <= budget {
			return candidate, true
		}
		curChaps = curChaps[1:]
	}

	// 若单章摘要全撤后仍超预算，则从最远弧线摘要开始收缩。
	curArcs := slices.Clone(arcLines)
	for len(curArcs) > 0 {
		candidate := render(volLines, curArcs, nil)
		if EstimateTokens(candidate) <= budget {
			return candidate, true
		}
		curArcs = curArcs[1:]
	}

	// 最后从最远卷摘要收缩。
	curVols := slices.Clone(volLines)
	for len(curVols) > 0 {
		candidate := render(curVols, nil, nil)
		if EstimateTokens(candidate) <= budget {
			return candidate, true
		}
		curVols = curVols[1:]
	}

	// 若连单条最近摘要都超过预算，则截断最新一条单章或弧线摘要作为保底。
	fallback := ""
	switch {
	case len(chapLines) > 0:
		fallback = chapLines[len(chapLines)-1]
	case len(arcLines) > 0:
		fallback = arcLines[len(arcLines)-1]
	case len(volLines) > 0:
		fallback = volLines[len(volLines)-1]
	}
	if cut := truncateHeadWithinTokens(fallback, budget-EstimateTokens(header)); cut != "" {
		return strings.TrimSpace(header + cut), true
	}
	return "", true
}

func buildSearchKeywords(explicit []string, p2Candidates []p2Candidate, beats []domain.Beat) []string {
	out := make([]string, 0, 5)
	seen := make(map[string]bool)
	add := func(raw string) {
		kw := strings.TrimSpace(raw)
		if kw == "" || len([]rune(kw)) < 2 {
			return
		}
		lower := strings.ToLower(kw)
		if seen[lower] {
			return
		}
		seen[lower] = true
		out = append(out, kw)
	}

	for _, kw := range explicit {
		add(kw)
	}
	if len(out) > 0 {
		if len(out) > 5 {
			return out[:5]
		}
		return out
	}

	// 未显式指定关键词时，自动从出场实体名（优先非 POV 的核心道具/对手/地点，再补 POV）中提取最多 3 个精确检索词。
	for _, cand := range p2Candidates {
		if !cand.highPriority {
			add(cand.entity.Name)
		}
		if len(out) >= 3 {
			return out
		}
	}
	for _, cand := range p2Candidates {
		add(cand.entity.Name)
		if len(out) >= 3 {
			return out
		}
	}
	// 若本章节拍无绑定实体，则从首个节拍摘要/冲突说明取短词作为回退。
	for _, b := range beats {
		for _, tok := range strings.FieldsFunc(b.ConflictNote+" "+b.Summary, isKeywordDelimiter) {
			if r := []rune(tok); len(r) >= 2 && len(r) <= 8 {
				add(tok)
				if len(out) >= 2 {
					return out
				}
			}
		}
	}
	return out
}

func isKeywordDelimiter(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '，', '。', '、', '；', '：', '！', '？', '（', '）', '《', '》', '“', '”', ',', '.', ';', ':', '!', '?', '/', '|', '-':
		return true
	default:
		return false
	}
}

func runSpoilerFreeMemorySearch(
	ctx context.Context,
	mem MemoryDataSource,
	bookID int64,
	maxChapterSeq int,
	keywords []string,
) ([]domain.MemorySearchHit, error) {
	if maxChapterSeq <= 0 || len(keywords) == 0 {
		return nil, nil
	}

	type hitKey struct {
		source    string
		chapterID int64
		entityID  int64
	}
	dedup := make(map[hitKey]domain.MemorySearchHit)
	for _, kw := range keywords {
		hits, err := mem.SearchMemory(ctx, bookID, kw, maxChapterSeq)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			// 双重防剧透守卫：即便底层实现返回超前章节，也在此硬过滤。
			if h.Source == domain.MemorySearchSourceChapter && h.ChapterSeq > maxChapterSeq {
				continue
			}
			if strings.TrimSpace(h.Snippet) == "" {
				continue
			}
			k := hitKey{source: h.Source, chapterID: h.ChapterID, entityID: h.EntityID}
			if existing, ok := dedup[k]; !ok || h.Score > existing.Score {
				dedup[k] = h
			}
		}
	}

	merged := make([]domain.MemorySearchHit, 0, len(dedup))
	for _, h := range dedup {
		merged = append(merged, h)
	}
	slices.SortFunc(merged, func(a, b domain.MemorySearchHit) int {
		// 优先章节原文片段，其次按相关度得分降序，最后按章号倒序稳定排序。
		if a.Source != b.Source {
			if a.Source == domain.MemorySearchSourceChapter {
				return -1
			}
			return 1
		}
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		if a.ChapterSeq != b.ChapterSeq {
			return b.ChapterSeq - a.ChapterSeq
		}
		return int(a.ChapterID - b.ChapterID)
	})
	if len(merged) > maxSearchHits {
		merged = merged[:maxSearchHits]
	}
	return merged, nil
}

func buildP6Content(hits []domain.MemorySearchHit, budget int) (string, bool) {
	if len(hits) == 0 {
		return "", false
	}

	const header = "【前文精确检索命中片段（历史出处核验）】\n"
	if budget <= EstimateTokens(header) {
		return "", true
	}

	kept := make([]string, 0, len(hits))
	truncated := false
	for _, h := range hits {
		var line string
		if h.Source == domain.MemorySearchSourceChapter {
			line = fmt.Sprintf("- [第%d章《%s》] %s", h.ChapterSeq, h.Title, strings.TrimSpace(h.Snippet))
		} else {
			line = fmt.Sprintf("- [设定《%s》] %s", h.Title, strings.TrimSpace(h.Snippet))
		}
		trial := strings.TrimSpace(header + strings.Join(append(slices.Clone(kept), line), "\n"))
		if EstimateTokens(trial) <= budget {
			kept = append(kept, line)
			continue
		}
		truncated = true
		if len(kept) == 0 {
			rem := budget - EstimateTokens(header)
			if cut := truncateHeadWithinTokens(line, rem); cut != "" {
				kept = append(kept, cut)
			}
		}
		break
	}
	if len(kept) == 0 {
		return "", true
	}
	return strings.TrimSpace(header + strings.Join(kept, "\n")), truncated
}

func formatCandidateDetailed(cand p2Candidate) string {
	base := formatEntityDetailed(cand.entity)
	if cand.pendingCount > 0 {
		base += fmt.Sprintf("\n  状态备注：包含 %d 条同批待审变更", cand.pendingCount)
	}
	return base
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

// InferDominantScene 根据章节节拍卡与情绪/钩子特征推断本章主导场景类型（combat / dialogue / suspense / daily）。
func InferDominantScene(beats []domain.Beat, chap domain.Chapter) domain.SceneType {
	scores := map[domain.SceneType]int{
		domain.SceneCombat:   0,
		domain.SceneDialogue: 0,
		domain.SceneSuspense: 0,
		domain.SceneDaily:    1,
	}

	switch chap.EmotionTag {
	case domain.EmotionBurst:
		scores[domain.SceneCombat] += 3
	case domain.EmotionTurn, domain.EmotionSuppress:
		scores[domain.SceneDialogue] += 2
	case domain.EmotionSetup, domain.EmotionHarvest:
		scores[domain.SceneDaily] += 2
	}
	if chap.HookType == domain.HookMystery || chap.HookType == domain.HookCrisis {
		scores[domain.SceneSuspense] += 2
	}

	combatWords := []string{"战", "杀", "斩", "剑", "拳", "雷", "阵", "交手", "反杀", "决战", "搏杀", "击溃", "轰", "死战"}
	dialogueWords := []string{"谈判", "对峙", "交锋", "试探", "嘲讽", "拍卖", "赌", "合同", "报价", "质问", "商议", "博弈", "当众"}
	suspenseWords := []string{"密库", "线索", "异象", "诡异", "潜入", "追踪", "暗流", "禁制", "秘密", "疑点", "调查", "窥探"}
	dailyWords := []string{"清点", "休整", "日常", "闭关", "赶路", "坊市", "盘点", "落袋"}

	for _, b := range beats {
		if b.Stage == domain.BeatStageBurst {
			scores[domain.SceneCombat] += 2
		}
		text := b.Summary + " " + b.ConflictNote
		for _, kw := range combatWords {
			if strings.Contains(text, kw) {
				scores[domain.SceneCombat] += 3
			}
		}
		for _, kw := range dialogueWords {
			if strings.Contains(text, kw) {
				scores[domain.SceneDialogue] += 3
			}
		}
		for _, kw := range suspenseWords {
			if strings.Contains(text, kw) {
				scores[domain.SceneSuspense] += 3
			}
		}
		for _, kw := range dailyWords {
			if strings.Contains(text, kw) {
				scores[domain.SceneDaily] += 2
			}
		}
	}

	order := []domain.SceneType{
		domain.SceneCombat,
		domain.SceneDialogue,
		domain.SceneSuspense,
		domain.SceneDaily,
	}
	best := domain.SceneDaily
	bestScore := -1
	for _, sc := range order {
		if scores[sc] > bestScore {
			bestScore = scores[sc]
			best = sc
		}
	}
	return best
}

func (c *Compiler) compileP7StyleSlot(
	ctx context.Context,
	bookID int64,
	chap domain.Chapter,
	beats []domain.Beat,
	budget int,
) (string, bool, error) {
	profile, err := c.style.GetStyleProfile(ctx, bookID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			profile = domain.DefaultStyleProfile(bookID)
		} else {
			return "", false, err
		}
	}

	rules, err := c.style.ListDeaiRules(ctx, bookID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", false, err
	}

	dominantScene := InferDominantScene(beats, chap)
	samples, err := c.style.ListStyleSamples(ctx, bookID, dominantScene)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", false, err
	}
	if len(samples) == 0 {
		// 若当前主导场景暂无专属样章，回退加载该书全场景样章。
		samples, _ = c.style.ListStyleSamples(ctx, bookID, "")
	}
	if len(samples) > maxStyleSampleAnchors {
		samples = samples[:maxStyleSampleAnchors]
	}

	content, truncated := buildP7Content(profile, rules, dominantScene, samples, budget)
	return content, truncated, nil
}

func buildP7Content(
	profile domain.StyleProfile,
	rules []domain.DeaiRule,
	dominantScene domain.SceneType,
	samples []domain.StyleSample,
	budget int,
) (string, bool) {
	const header = "【Style DNA 文风指纹与场景样章锚点】\n"
	if budget <= EstimateTokens(header) {
		return "", true
	}

	avgSent := profile.AvgSentenceChars
	if avgSent <= 0 {
		avgSent = domain.DefaultAvgSentenceChars
	}
	maxPara := profile.MaxParagraphChars
	if maxPara <= 0 {
		maxPara = domain.DefaultMaxParagraphChars
	}
	dlgMin := profile.DialogueRatioMin
	dlgMax := profile.DialogueRatioMax
	if dlgMax <= 0 {
		dlgMin = domain.DefaultDialogueRatioMin
		dlgMax = domain.DefaultDialogueRatioMax
	}
	pov := profile.PovRule
	if !pov.Valid() {
		pov = domain.PovThirdLimited
	}

	var dnaBuilder strings.Builder
	dnaBuilder.WriteString(header)
	fmt.Fprintf(
		&dnaBuilder,
		"量化文风约束：平均句长约 %.0f 字｜单段上限 %d 字｜对话占比 %.0f%%~%.0f%%｜视角：%s（%s）\n",
		avgSent, maxPara, dlgMin*100, dlgMax*100, pov.Label(), pov,
	)
	if strings.TrimSpace(profile.ToneDirectives) != "" {
		fmt.Fprintf(&dnaBuilder, "叙事语调指令：%s\n", strings.TrimSpace(profile.ToneDirectives))
	}
	if strings.TrimSpace(profile.ForbiddenTropesNote) != "" {
		fmt.Fprintf(&dnaBuilder, "作者自定义禁区：%s\n", strings.TrimSpace(profile.ForbiddenTropesNote))
	}

	// 汇总当前启用的高频套话黑名单（最多展示前 12 条）。
	banned := make([]string, 0, 12)
	for _, r := range rules {
		if !r.Enabled || r.Category != domain.DeaiCategoryCliche || r.MaxPerChapter > 0 {
			continue
		}
		pat := strings.TrimSpace(r.Pattern)
		if pat != "" {
			banned = append(banned, pat)
			if len(banned) >= 12 {
				break
			}
		}
	}
	if len(banned) > 0 {
		fmt.Fprintf(&dnaBuilder, "禁用套话黑名单：%s\n", strings.Join(banned, "、"))
	}

	baseDNA := strings.TrimSpace(dnaBuilder.String())
	if len(samples) == 0 {
		if EstimateTokens(baseDNA) <= budget {
			return baseDNA, false
		}
		return truncateHeadWithinTokens(baseDNA, budget), true
	}

	renderWithSamples := func(list []domain.StyleSample) string {
		if len(list) == 0 {
			return baseDNA
		}
		var b strings.Builder
		b.WriteString(baseDNA)
		fmt.Fprintf(&b, "\n\n【场景语感示范（主导场景：%s；只学句式节奏与动作力度，严禁抄袭样章人物或剧情）】", dominantScene.Label())
		for i, sp := range list {
			fmt.Fprintf(&b, "\n示范 %d《%s》（%s）：\n%s", i+1, sp.Title, sp.SceneType.Label(), strings.TrimSpace(sp.Excerpt))
		}
		return strings.TrimSpace(b.String())
	}

	// 1. 尝试完整装入 1~2 段样章锚点。
	full := renderWithSamples(samples)
	if EstimateTokens(full) <= budget {
		return full, false
	}

	// 2. 按 docs/03 §1.3 裁剪策略：超限时缩减为 1 段最贴合锚点。
	if len(samples) > 1 {
		oneSample := renderWithSamples(samples[:1])
		if EstimateTokens(oneSample) <= budget {
			return oneSample, true
		}
	}

	// 3. 若 1 段完整样章仍超出剩余预算，截断第 1 段样章或保留核心量化 DNA。
	if EstimateTokens(baseDNA) <= budget {
		rem := budget - EstimateTokens(baseDNA) - 24
		if rem > 30 {
			cutExcerpt := truncateHeadWithinTokens(strings.TrimSpace(samples[0].Excerpt), rem)
			if cutExcerpt != "" {
				partialSample := domain.StyleSample{
					SceneType: samples[0].SceneType,
					Title:     samples[0].Title,
					Excerpt:   cutExcerpt,
				}
				candidate := renderWithSamples([]domain.StyleSample{partialSample})
				if EstimateTokens(candidate) <= budget {
					return candidate, true
				}
			}
		}
		return baseDNA, true
	}

	return truncateHeadWithinTokens(baseDNA, budget), true
}
