package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo"

	"nib/internal/domain"
)

const maxMicroBatchChapters = 5

// BatchProgressEvent 向前端广播串行微批队列的实时章节推进状态（映射为 src/mygo.ts 中的 events.batchProgress）。
var BatchProgressEvent = mygo.NewEvent[domain.BatchProgress]("batch:progress")

// EmitBatchProgress 将微批进度事件广播给当前打开的前端页面。
func EmitBatchProgress(p domain.BatchProgress) {
	_ = BatchProgressEvent.Broadcast(p)
}

// BatchService 提供 Phase 4 1~5 章串行接力微批调度队列的创建、查询、中止与单章重跑 RPC 绑定。
type BatchService struct {
	rt *Runtime
}

// NewBatchService 构造绑定到共享 Runtime 的 BatchService。
func NewBatchService(rt *Runtime) *BatchService {
	return &BatchService{rt: rt}
}

// StartBatchRun 校验并启动一次 1~5 章串行微批生成任务，后台按章接力并通过 events.batchProgress 推送进度。
func (s *BatchService) StartBatchRun(ctx context.Context, bookID int64, chapterIDs []int64, mode string) (domain.BatchRun, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.BatchRun{}, err
	}
	if len(chapterIDs) == 0 || len(chapterIDs) > maxMicroBatchChapters {
		return domain.BatchRun{}, &domain.ValidationError{
			Field:   "chapterIds",
			Message: "单次串行微批队列章节数量必须在 1 到 5 章之间",
		}
	}
	seen := make(map[int64]bool, len(chapterIDs))
	for _, cid := range chapterIDs {
		if cid <= 0 {
			return domain.BatchRun{}, &domain.ValidationError{
				Field:   "chapterIds",
				Message: "章节 ID 必须大于 0",
			}
		}
		if seen[cid] {
			return domain.BatchRun{}, &domain.ValidationError{
				Field:   "chapterIds",
				Message: "微批队列中不能包含重复章节",
			}
		}
		seen[cid] = true
	}

	batchMode := domain.BatchModeFullPipeline
	if trimmed := strings.TrimSpace(mode); trimmed != "" {
		batchMode = domain.BatchMode(trimmed)
		if !batchMode.Valid() {
			return domain.BatchRun{}, &domain.ValidationError{
				Field:   "mode",
				Message: "无效的微批执行模式",
			}
		}
	}

	eng, err := s.rt.batchEngine()
	if err != nil {
		return domain.BatchRun{}, err
	}
	run, err := eng.StartBatchRun(ctx, bookID, chapterIDs, batchMode)
	if err != nil {
		return domain.BatchRun{}, err
	}
	normalizeBatchRun(&run)
	return run, nil
}

// GetBatchRun 查询指定微批任务的当前状态与单章排队明细。
func (s *BatchService) GetBatchRun(ctx context.Context, batchID int64) (domain.BatchRun, error) {
	if err := validatePositiveID("batchId", batchID); err != nil {
		return domain.BatchRun{}, err
	}
	bs, err := s.rt.batchStore()
	if err != nil {
		return domain.BatchRun{}, err
	}
	run, err := bs.GetBatchRun(ctx, batchID)
	if err != nil {
		return domain.BatchRun{}, err
	}
	normalizeBatchRun(&run)
	return run, nil
}

// GetLatestBatchRun 查询指定书籍最近一次微批任务（无记录时返回 nil）。
func (s *BatchService) GetLatestBatchRun(ctx context.Context, bookID int64) (*domain.BatchRun, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	bs, err := s.rt.batchStore()
	if err != nil {
		return nil, err
	}
	run, err := bs.GetLatestBatchRun(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if run != nil {
		normalizeBatchRun(run)
	}
	return run, nil
}

// CancelBatchRun 请求中止正在运行的串行微批队列（正在出稿的章节会保底存为 partial 后停稳）。
func (s *BatchService) CancelBatchRun(ctx context.Context, batchID int64) (domain.BatchRun, error) {
	if err := validatePositiveID("batchId", batchID); err != nil {
		return domain.BatchRun{}, err
	}
	eng, err := s.rt.batchEngine()
	if err != nil {
		return domain.BatchRun{}, err
	}
	run, err := eng.CancelBatchRun(ctx, batchID)
	if err != nil {
		return domain.BatchRun{}, err
	}
	normalizeBatchRun(&run)
	return run, nil
}

// RetryBatchItem 对微批队列中失败（failed）或中断（canceled）的单章任务发起重跑。
func (s *BatchService) RetryBatchItem(ctx context.Context, itemID int64) (domain.BatchRun, error) {
	if err := validatePositiveID("itemId", itemID); err != nil {
		return domain.BatchRun{}, err
	}
	eng, err := s.rt.batchEngine()
	if err != nil {
		return domain.BatchRun{}, err
	}
	run, err := eng.RetryBatchItem(ctx, itemID)
	if err != nil {
		return domain.BatchRun{}, err
	}
	normalizeBatchRun(&run)
	return run, nil
}

func normalizeBatchRun(r *domain.BatchRun) {
	if r.Items == nil {
		r.Items = []domain.BatchRunItem{}
	}
}
