package app

import (
	"context"
	"errors"
	"testing"

	"nib/internal/domain"
)

type fakeBatchStore struct {
	run     domain.BatchRun
	latest  *domain.BatchRun
	summary domain.UsageSummary
}

func (f *fakeBatchStore) GetBatchRun(_ context.Context, batchID int64) (domain.BatchRun, error) {
	r := f.run
	r.ID = batchID
	return r, nil
}

func (f *fakeBatchStore) GetLatestBatchRun(_ context.Context, _ int64) (*domain.BatchRun, error) {
	return f.latest, nil
}

func (f *fakeBatchStore) GetUsageSummary(_ context.Context, bookID int64) (domain.UsageSummary, error) {
	s := f.summary
	s.BookID = bookID
	return s, nil
}

type fakeBatchEngine struct {
	outlinesChapters []domain.Chapter
	run              domain.BatchRun
	lastMode         domain.BatchMode
	lastCount        int
}

func (e *fakeBatchEngine) BatchGenerateOutlines(_ context.Context, req domain.BatchOutlineRequest, emit func(domain.OutlineChunk) error) ([]domain.Chapter, error) {
	e.lastCount = req.Count
	if emit != nil {
		_ = emit(domain.OutlineChunk{Kind: domain.GenChunkCompleted, Chapters: e.outlinesChapters})
	}
	return e.outlinesChapters, nil
}

func (e *fakeBatchEngine) StartBatchRun(_ context.Context, bookID int64, chapterIDs []int64, mode domain.BatchMode) (domain.BatchRun, error) {
	e.lastMode = mode
	r := e.run
	r.ID = 77
	r.BookID = bookID
	r.Mode = mode
	r.TotalItems = len(chapterIDs)
	return r, nil
}

func (e *fakeBatchEngine) CancelBatchRun(_ context.Context, batchID int64) (domain.BatchRun, error) {
	r := e.run
	r.ID = batchID
	r.Status = domain.BatchRunCanceled
	return r, nil
}

func (e *fakeBatchEngine) RetryBatchItem(_ context.Context, _ int64) (domain.BatchRun, error) {
	r := e.run
	r.Status = domain.BatchRunRunning
	return r, nil
}

func TestBatchServiceAndPhase4Extensions(t *testing.T) {
	ctx := context.Background()
	emptyRT := NewRuntime()

	libEmpty := NewLibraryService(emptyRT)
	setEmpty := NewSettingsService(emptyRT)
	batchEmpty := NewBatchService(emptyRT)

	if _, err := libEmpty.GetVolumePacing(ctx, 1); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("GetVolumePacing err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := libEmpty.BatchGenerateOutlines(ctx, domain.BatchOutlineRequest{BookID: 1, VolumeID: 1}, nil); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("BatchGenerateOutlines err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := setEmpty.GetUsageSummary(ctx, 1); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("GetUsageSummary err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := batchEmpty.StartBatchRun(ctx, 1, []int64{1}, "draft_only"); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("StartBatchRun err = %v, want ErrRuntimeNotReady", err)
	}

	st := newFakeStore()
	ms := &fakeMemoryStore{
		foreshadowings: []domain.Foreshadowing{
			{ID: 5, BookID: 1, Title: "血玉令牌", Status: domain.ForeshadowingPlanted, TargetResolveSeq: 2},
		},
	}
	bs := &fakeBatchStore{
		summary: domain.UsageSummary{TotalCalls: 4, TotalTokens: 1600},
	}
	be := &fakeBatchEngine{
		outlinesChapters: []domain.Chapter{{ID: 10, Title: "自动章纲1"}},
		run:              domain.BatchRun{Status: domain.BatchRunRunning},
	}

	rt := NewRuntime()
	rt.Init(RuntimeDeps{
		Store:       st,
		MemoryStore: ms,
		BatchStore:  bs,
		BatchEngine: be,
	})

	lib := NewLibraryService(rt)
	settings := NewSettingsService(rt)
	batch := NewBatchService(rt)

	// 1. GetVolumePacing 结合章节与伏笔执行卷级诊断
	book, _ := lib.CreateBook(ctx, domain.CreateBookInput{Title: "太初剑渊", Genre: domain.GenreXianxia})
	vol, _ := lib.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Seq: 1, Title: "第一卷"})
	for _, title := range []string{"一", "二", "三"} {
		_, _ = lib.CreateChapter(ctx, domain.CreateChapterInput{
			BookID:     book.ID,
			VolumeID:   vol.ID,
			Title:      title,
			EmotionTag: domain.EmotionSuppress,
			HookType:   domain.HookCrisis,
		})
	}
	pacing, err := lib.GetVolumePacing(ctx, vol.ID)
	if err != nil || len(pacing.Points) != 3 || len(pacing.Alerts) < 2 {
		t.Fatalf("GetVolumePacing = (%+v, %v)", pacing, err)
	}

	// 2. BatchGenerateOutlines 默认 Count=5 与校验
	if _, err := lib.BatchGenerateOutlines(ctx, domain.BatchOutlineRequest{BookID: 1, VolumeID: 1, Count: 99}, nil); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for count=99, got %v", err)
	}
	genChaps, err := lib.BatchGenerateOutlines(ctx, domain.BatchOutlineRequest{BookID: book.ID, VolumeID: vol.ID}, nil)
	if err != nil || len(genChaps) != 1 || be.lastCount != domain.DefaultBatchOutlineCount {
		t.Fatalf("BatchGenerateOutlines = (%+v, %v) count=%d", genChaps, err, be.lastCount)
	}

	// 3. GetUsageSummary 归一化切片
	if _, err := settings.GetUsageSummary(ctx, -1); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bookId=-1, got %v", err)
	}
	usage, err := settings.GetUsageSummary(ctx, book.ID)
	if err != nil || usage.TotalCalls != 4 || usage.ByStage == nil || usage.ByModel == nil {
		t.Fatalf("GetUsageSummary = (%+v, %v)", usage, err)
	}

	// 4. BatchService 校验与调度
	if _, err := batch.StartBatchRun(ctx, book.ID, nil, ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty chapterIDs")
	}
	if _, err := batch.StartBatchRun(ctx, book.ID, []int64{1, 2, 3, 4, 5, 6}, ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for >5 chapterIDs")
	}
	if _, err := batch.StartBatchRun(ctx, book.ID, []int64{1, 1}, ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for duplicate chapterIDs")
	}
	if _, err := batch.StartBatchRun(ctx, book.ID, []int64{1}, "bad_mode"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bad batch mode")
	}

	startedRun, err := batch.StartBatchRun(ctx, book.ID, []int64{1, 2, 3}, "")
	if err != nil || startedRun.Mode != domain.BatchModeFullPipeline || startedRun.Items == nil {
		t.Fatalf("StartBatchRun = (%+v, %v)", startedRun, err)
	}

	gotRun, err := batch.GetBatchRun(ctx, startedRun.ID)
	if err != nil || gotRun.ID != startedRun.ID || gotRun.Items == nil {
		t.Fatalf("GetBatchRun = (%+v, %v)", gotRun, err)
	}
	if latest, err := batch.GetLatestBatchRun(ctx, book.ID); err != nil || latest != nil {
		t.Fatalf("GetLatestBatchRun expected nil, got (%+v, %v)", latest, err)
	}
	canceledRun, err := batch.CancelBatchRun(ctx, startedRun.ID)
	if err != nil || canceledRun.Status != domain.BatchRunCanceled {
		t.Fatalf("CancelBatchRun = (%+v, %v)", canceledRun, err)
	}
	retriedRun, err := batch.RetryBatchItem(ctx, 10)
	if err != nil || retriedRun.Status != domain.BatchRunRunning {
		t.Fatalf("RetryBatchItem = (%+v, %v)", retriedRun, err)
	}
}
