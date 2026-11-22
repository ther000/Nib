package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo"

	"nib/internal/domain"
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
}
