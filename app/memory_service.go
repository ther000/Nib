package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo"

	"nib/internal/domain"
	"nib/internal/memory"
)

// MemoryService 提供 Phase 2 防吃书记忆账本、伏笔追踪、摘要金字塔、变更单审阅与混合检索的桌面 RPC 绑定。
type MemoryService struct {
	rt *Runtime
}

// NewMemoryService 构造绑定到共享 Runtime 的 MemoryService。
func NewMemoryService(rt *Runtime) *MemoryService {
	return &MemoryService{rt: rt}
}

// SnapshotAt 按时间轴折算截至 chapterSeq（含）时的全书实体状态快照（严禁透漏未来章节事件）。
func (s *MemoryService) SnapshotAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntitySnapshot, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	if chapterSeq < 0 {
		return nil, &domain.ValidationError{Field: "chapterSeq", Message: "章节序号不能为负数"}
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	snaps, err := ms.SnapshotAt(ctx, bookID, chapterSeq, includePending)
	if err != nil {
		return nil, err
	}
	if snaps == nil {
		return []domain.EntitySnapshot{}, nil
	}
	for i := range snaps {
		if snaps[i].EffectiveAttributes == nil {
			snaps[i].EffectiveAttributes = map[string]string{}
		}
	}
	return snaps, nil
}

// ListStateEvents 查询实体状态变更事件流水；entityID=0 表示查询全书，upToSeq=0 表示不限截止章号。
func (s *MemoryService) ListStateEvents(ctx context.Context, bookID int64, entityID int64, upToSeq int, includePending bool) ([]domain.EntityStateEvent, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	if entityID < 0 {
		return nil, &domain.ValidationError{Field: "entityId", Message: "实体 ID 不能为负数"}
	}
	if upToSeq < 0 {
		return nil, &domain.ValidationError{Field: "upToSeq", Message: "截止章节号不能为负数"}
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	events, err := ms.ListStateEvents(ctx, bookID, entityID, upToSeq, includePending)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return []domain.EntityStateEvent{}, nil
	}
	return events, nil
}

// ListForeshadowings 按书籍与可选状态筛选伏笔列表（status 留空返回全部状态）。
func (s *MemoryService) ListForeshadowings(ctx context.Context, bookID int64, status string) ([]domain.Foreshadowing, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(status)
	var stFilter domain.ForeshadowingStatus
	if trimmed != "" {
		stFilter = domain.ForeshadowingStatus(trimmed)
		if !stFilter.Valid() {
			return nil, &domain.ValidationError{Field: "status", Message: "无效的伏笔状态"}
		}
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	items, err := ms.ListForeshadowings(ctx, bookID, stFilter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []domain.Foreshadowing{}, nil
	}
	for i := range items {
		if items[i].InvolvedEntityIDs == nil {
			items[i].InvolvedEntityIDs = []int64{}
		}
	}
	return items, nil
}

// UpsertForeshadowing 创建或更新单条伏笔记录。
func (s *MemoryService) UpsertForeshadowing(ctx context.Context, in domain.UpsertForeshadowingInput) (domain.Foreshadowing, error) {
	if err := in.Validate(); err != nil {
		return domain.Foreshadowing{}, err
	}
	if in.Status == "" {
		in.Status = domain.ForeshadowingPlanted
	}
	if in.InvolvedEntityIDs == nil {
		in.InvolvedEntityIDs = []int64{}
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return domain.Foreshadowing{}, err
	}
	saved, err := ms.UpsertForeshadowing(ctx, in)
	if err != nil {
		return domain.Foreshadowing{}, err
	}
	if saved.InvolvedEntityIDs == nil {
		saved.InvolvedEntityIDs = []int64{}
	}
	return saved, nil
}

// DeleteForeshadowing 删除指定伏笔记录。
func (s *MemoryService) DeleteForeshadowing(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return err
	}
	return ms.DeleteForeshadowing(ctx, id)
}

// ListStoryDeadlines 按书籍与可选状态筛选故事内期限/倒计时（status 留空返回全部）。
func (s *MemoryService) ListStoryDeadlines(ctx context.Context, bookID int64, status string) ([]domain.StoryDeadline, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(status)
	var stFilter domain.StoryDeadlineStatus
	if trimmed != "" {
		stFilter = domain.StoryDeadlineStatus(trimmed)
		if !stFilter.Valid() {
			return nil, &domain.ValidationError{Field: "status", Message: "无效的期限状态"}
		}
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	items, err := ms.ListStoryDeadlines(ctx, bookID, stFilter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []domain.StoryDeadline{}, nil
	}
	return items, nil
}

// UpsertStoryDeadline 创建或更新单条故事内期限倒计时。
func (s *MemoryService) UpsertStoryDeadline(ctx context.Context, in domain.UpsertStoryDeadlineInput) (domain.StoryDeadline, error) {
	if err := in.Validate(); err != nil {
		return domain.StoryDeadline{}, err
	}
	if in.Status == "" {
		in.Status = domain.StoryDeadlineActive
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return domain.StoryDeadline{}, err
	}
	return ms.UpsertStoryDeadline(ctx, in)
}

// DeleteStoryDeadline 删除指定故事内期限倒计时记录。
func (s *MemoryService) DeleteStoryDeadline(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return err
	}
	return ms.DeleteStoryDeadline(ctx, id)
}

// ListArcSummaries 查询分层摘要金字塔（level 可为 "" / "arc_10" / "volume"）。
func (s *MemoryService) ListArcSummaries(ctx context.Context, bookID int64, level string) ([]domain.ArcSummary, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(level)
	var lvlFilter domain.ArcSummaryLevel
	if trimmed != "" {
		lvlFilter = domain.ArcSummaryLevel(trimmed)
		if !lvlFilter.Valid() {
			return nil, &domain.ValidationError{Field: "level", Message: "无效的摘要层级"}
		}
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	items, err := ms.ListArcSummaries(ctx, bookID, lvlFilter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []domain.ArcSummary{}, nil
	}
	return items, nil
}

// UpsertArcSummary 新增或更新 10 章弧线摘要或卷摘要。
func (s *MemoryService) UpsertArcSummary(ctx context.Context, in domain.UpsertArcSummaryInput) (domain.ArcSummary, error) {
	if err := in.Validate(); err != nil {
		return domain.ArcSummary{}, err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return domain.ArcSummary{}, err
	}
	return ms.UpsertArcSummary(ctx, in)
}

// GenerateArcSummary 由 AI 自动汇总指定章号区间（10 章弧线）或整卷剧情摘要并写入 arc_summaries。
func (s *MemoryService) GenerateArcSummary(ctx context.Context, req domain.GenerateArcSummaryRequest) (domain.ArcSummary, error) {
	if err := req.Validate(); err != nil {
		return domain.ArcSummary{}, err
	}
	gen, err := s.rt.arcSummaryGenerator()
	if err != nil {
		return domain.ArcSummary{}, err
	}
	return gen.GenerateArcSummary(ctx, req)
}

// ExtractChapterDelta 调用 extract 模型路由抽取本章增量状态与伏笔变更提案（写入 pending 态变更单，绝不静默入库）。
func (s *MemoryService) ExtractChapterDelta(ctx context.Context, chapterID int64) (domain.ChapterDelta, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return domain.ChapterDelta{}, err
	}
	ext, extCtx, release, err := s.rt.trackExtractor(ctx)
	if err != nil {
		return domain.ChapterDelta{}, err
	}
	defer release()

	delta, err := ext.ExtractChapterDelta(extCtx, chapterID)
	if err != nil {
		return domain.ChapterDelta{}, err
	}
	normalizeChapterDelta(&delta)
	return delta, nil
}

// ReverseExtractBook 从旧稿采样章节流式反推设定、关系、伏笔与摘要，仅返回审阅清单。
func (s *MemoryService) ReverseExtractBook(
	ctx context.Context,
	req domain.ReverseExtractRequest,
	ch *mygo.Channel[domain.ReverseExtractChunk],
) (domain.ReverseExtractPreview, error) {
	if err := req.Validate(); err != nil {
		return domain.ReverseExtractPreview{}, err
	}
	eng, genCtx, cancel, release, err := s.rt.trackReverseExtractEngine(ctx)
	if err != nil {
		return domain.ReverseExtractPreview{}, err
	}
	defer release()

	preview, err := eng.ReverseExtractBook(genCtx, req, channelEmitter(genCtx, cancel, ch))
	if err != nil {
		if genCtx.Err() != nil {
			return domain.ReverseExtractPreview{}, domain.ErrGenerationCanceled
		}
		return domain.ReverseExtractPreview{}, err
	}
	return preview, nil
}

// ApplyReverseExtract 将作者勾选后的旧稿反推结果写入设定账本。
func (s *MemoryService) ApplyReverseExtract(ctx context.Context, in domain.ApplyReverseExtractInput) (domain.ApplyReverseExtractResult, error) {
	if err := in.Validate(); err != nil {
		return domain.ApplyReverseExtractResult{}, err
	}
	eng, err := s.rt.reverseExtractEngine()
	if err != nil {
		return domain.ApplyReverseExtractResult{}, err
	}
	return eng.ApplyReverseExtract(ctx, in)
}

// GetPendingDelta 查询指定章节当前待审核的记忆变更单；若不存在返回 nil。
func (s *MemoryService) GetPendingDelta(ctx context.Context, chapterID int64) (*domain.ChapterDelta, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return nil, err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	delta, err := ms.GetPendingDelta(ctx, chapterID)
	if err != nil {
		return nil, err
	}
	if delta != nil {
		normalizeChapterDelta(delta)
	}
	return delta, nil
}

// ApplyDeltaDecisions 在事务内应用作者对记忆变更单的逐条审定结论，提交选定项为 committed 并对下游受影响章节标脏。
func (s *MemoryService) ApplyDeltaDecisions(ctx context.Context, in domain.ApplyDeltaInput) (domain.ApplyDeltaResult, error) {
	if err := in.Validate(); err != nil {
		return domain.ApplyDeltaResult{}, err
	}
	if in.Decisions == nil {
		in.Decisions = []domain.DeltaDecision{}
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return domain.ApplyDeltaResult{}, err
	}
	res, err := ms.ApplyDeltaDecisions(ctx, in)
	if err != nil {
		return domain.ApplyDeltaResult{}, err
	}
	normalizeChapterDelta(&res.Delta)
	if res.MarkedDirtyChapterIDs == nil {
		res.MarkedDirtyChapterIDs = []int64{}
	}
	return res, nil
}

// SearchMemory 执行 FTS5 trigram + 双字 LIKE 回退混合记忆检索（maxChapterSeq=0 表示不限章号上限）。
func (s *MemoryService) SearchMemory(ctx context.Context, bookID int64, query string, maxChapterSeq int) ([]domain.MemorySearchHit, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	if maxChapterSeq < 0 {
		return nil, &domain.ValidationError{Field: "maxChapterSeq", Message: "最大章节号不能为负数"}
	}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return []domain.MemorySearchHit{}, nil
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	hits, err := ms.SearchMemory(ctx, bookID, trimmed, maxChapterSeq)
	if err != nil {
		return nil, err
	}
	if hits == nil {
		return []domain.MemorySearchHit{}, nil
	}
	return hits, nil
}

func normalizeChapterDelta(d *domain.ChapterDelta) {
	if d.NewEntities == nil {
		d.NewEntities = []domain.DeltaNewEntity{}
	}
	if d.StatePatches == nil {
		d.StatePatches = []domain.DeltaStatePatch{}
	}
	if d.Foreshadows == nil {
		d.Foreshadows = []domain.DeltaForeshadowUpdate{}
	}
	if d.RelationChanges == nil {
		d.RelationChanges = []domain.DeltaRelationChange{}
	}
	if d.DeadlineChanges == nil {
		d.DeadlineChanges = []domain.DeltaDeadlineChange{}
	}
}

// RunLocalConsistencyAudit 执行第一层 0 成本本地全书秒扫，零模型消耗检出六类逻辑与流程隐患。
func (s *MemoryService) RunLocalConsistencyAudit(ctx context.Context, bookID int64, opts domain.LocalAuditOptions) (domain.LocalAuditReport, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.LocalAuditReport{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.LocalAuditReport{}, err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return domain.LocalAuditReport{}, err
	}

	entities, err := st.ListEntities(ctx, bookID, domain.EntityFilter{})
	if err != nil {
		return domain.LocalAuditReport{}, err
	}
	chapters, err := st.ListBookChapters(ctx, bookID)
	if err != nil {
		return domain.LocalAuditReport{}, err
	}
	beatsByChapter, err := st.ListBookBeats(ctx, bookID)
	if err != nil {
		return domain.LocalAuditReport{}, err
	}
	events, err := ms.ListStateEvents(ctx, bookID, 0, 0, false)
	if err != nil {
		return domain.LocalAuditReport{}, err
	}
	foreshadows, err := ms.ListForeshadowings(ctx, bookID, "")
	if err != nil {
		return domain.LocalAuditReport{}, err
	}
	deadlines, err := ms.ListStoryDeadlines(ctx, bookID, "")
	if err != nil {
		return domain.LocalAuditReport{}, err
	}
	pendingDeltas, err := ms.ListPendingDeltas(ctx, bookID)
	if err != nil {
		return domain.LocalAuditReport{}, err
	}

	return memory.RunLocalConsistencyAudit(memory.LocalAuditInput{
		BookID:         bookID,
		Entities:       entities,
		Chapters:       chapters,
		BeatsByChapter: beatsByChapter,
		StateEvents:    events,
		Foreshadowings: foreshadows,
		Deadlines:      deadlines,
		PendingDeltas:  pendingDeltas,
		Options:        opts,
	}), nil
}

// RunAIConsistencyAudit 对指定分卷 / 章节区间 / 全书执行第二层 AI 深度语义审计并持久化报告。
func (s *MemoryService) RunAIConsistencyAudit(ctx context.Context, req domain.RunAIConsistencyAuditRequest) (domain.BookConsistencyReport, error) {
	if err := req.Validate(); err != nil {
		return domain.BookConsistencyReport{}, err
	}
	eng, auditCtx, _, release, err := s.rt.trackConsistencyAuditEngine(ctx)
	if err != nil {
		return domain.BookConsistencyReport{}, err
	}
	defer release()

	rep, err := eng.RunAIConsistencyAudit(auditCtx, req)
	if err != nil {
		return domain.BookConsistencyReport{}, err
	}
	rep.Normalize()
	return rep, nil
}

// ListConsistencyReports 返回指定书籍的 AI 深度语义审计历史报告列表。
func (s *MemoryService) ListConsistencyReports(ctx context.Context, bookID int64) ([]domain.BookConsistencyReport, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	list, err := ms.ListConsistencyReports(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		return []domain.BookConsistencyReport{}, nil
	}
	for i := range list {
		list[i].Normalize()
	}
	return list, nil
}

// UpdateConsistencyFindingStatus 标记单条 AI 深度审计问题的处理状态（open / resolved / ignored）。
func (s *MemoryService) UpdateConsistencyFindingStatus(
	ctx context.Context,
	reportID int64,
	findingID string,
	status string,
) (domain.BookConsistencyReport, error) {
	if err := validatePositiveID("reportId", reportID); err != nil {
		return domain.BookConsistencyReport{}, err
	}
	st := domain.ConsistencyFindingStatus(strings.TrimSpace(status))
	if !st.Valid() {
		return domain.BookConsistencyReport{}, &domain.ValidationError{Field: "status", Message: "无效的问题处理状态"}
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return domain.BookConsistencyReport{}, err
	}
	rep, err := ms.UpdateConsistencyFindingStatus(ctx, reportID, findingID, st)
	if err != nil {
		return domain.BookConsistencyReport{}, err
	}
	rep.Normalize()
	return rep, nil
}

// DeleteConsistencyReport 删除指定 AI 深度语义审计报告。
func (s *MemoryService) DeleteConsistencyReport(ctx context.Context, reportID int64) error {
	if err := validatePositiveID("reportId", reportID); err != nil {
		return err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return err
	}
	return ms.DeleteConsistencyReport(ctx, reportID)
}

// ListDirtyChapters 查询全书处于 upstream_dirty 状态的章节队列及上游改动摘要。
func (s *MemoryService) ListDirtyChapters(ctx context.Context, bookID int64) ([]domain.DirtyChapterItem, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return nil, err
	}
	items, err := ms.ListDirtyChapters(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []domain.DirtyChapterItem{}, nil
	}
	return items, nil
}

// ClearDirtyChapters 清除指定章节（chapterIDs 为空表示全书）的 upstream_dirty 标记。
func (s *MemoryService) ClearDirtyChapters(ctx context.Context, bookID int64, chapterIDs []int64) (domain.ClearDirtyChaptersResult, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.ClearDirtyChaptersResult{}, err
	}
	ms, err := s.rt.memoryStore()
	if err != nil {
		return domain.ClearDirtyChaptersResult{}, err
	}
	res, err := ms.ClearDirtyChapters(ctx, bookID, chapterIDs)
	if err != nil {
		return domain.ClearDirtyChaptersResult{}, err
	}
	if res.ClearedChapterIDs == nil {
		res.ClearedChapterIDs = []int64{}
	}
	return res, nil
}
