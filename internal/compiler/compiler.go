package compiler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"nib/internal/domain"
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

// TimedRelationDataSource 定义按章时间切片的实体关系查询契约（由 *store.Store 自动满足，严格隔离未来章节关系变更）。
type TimedRelationDataSource interface {
	ListEntityRelationsAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntityRelation, error)
}

// TimelineDataSource 定义故事内期限倒计时只读契约（由 *store.Store 自动满足）。
type TimelineDataSource interface {
	ListStoryDeadlines(ctx context.Context, bookID int64, status domain.StoryDeadlineStatus) ([]domain.StoryDeadline, error)
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
	src            DataSource
	mem            MemoryDataSource
	style          StyleDataSource
	relations      RelationDataSource
	timedRelations TimedRelationDataSource
	timeline       TimelineDataSource
}

// New 创建上下文编译器实例。若 src 本身实现了 MemoryDataSource / StyleDataSource / RelationDataSource / TimelineDataSource，将自动激活对应槽位。
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
	if trs, ok := src.(TimedRelationDataSource); ok {
		c.timedRelations = trs
	}
	if ts, ok := src.(TimelineDataSource); ok {
		c.timeline = ts
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
// P5（摘要金字塔）→ P6（前文精确检索）→ P7（文风指纹与样章）优先级编译确定性作战简报。
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

	var activeDeadlines []domain.StoryDeadline
	if c.timeline != nil {
		if dls, dlErr := c.timeline.ListStoryDeadlines(ctx, book.ID, domain.StoryDeadlineActive); dlErr == nil {
			for _, dl := range dls {
				if dl.StartChapterSeq <= chap.GlobalSeq {
					activeDeadlines = append(activeDeadlines, dl)
				}
			}
		}
	}

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

	// P1（永不裁剪）：本章标题/情绪/钩子/故事时间线与倒计时/节拍卡/作者附加指令。
	p1Content := buildP1Content(book, chap, prevAll, activeDeadlines, beats, entityByID, opts.ExtraInstruction)
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

	// P2（高优）：出场实体与钉选实体状态快照 + 关联实体关系网（优先按 prevSeq 时间切片）。
	p2Cap := min(scaledBudget(totalBudget, defaultP2Budget), remaining)
	p2Content, includedEntityIDs, p2Truncated := buildP2Content(p2Candidates, p2Cap)
	if len(includedEntityIDs) > 0 && p2Cap > EstimateTokens(p2Content)+40 && (c.timedRelations != nil || c.relations != nil) {
		var rels []domain.EntityRelation
		var rErr error
		if c.timedRelations != nil {
			rels, rErr = c.timedRelations.ListEntityRelationsAt(ctx, book.ID, prevSeq, opts.IncludePending)
		} else {
			rels, rErr = c.relations.ListEntityRelations(ctx, book.ID)
		}
		if rErr == nil && len(rels) > 0 {
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
	if totalBudget <= 0 || nominal <= 0 {
		return 0
	}
	if totalBudget >= domain.DefaultBriefingTokenBudget {
		return max(nominal, totalBudget*nominal/domain.DefaultBriefingTokenBudget)
	}
	return max(1, totalBudget*nominal/domain.DefaultBriefingTokenBudget)
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
