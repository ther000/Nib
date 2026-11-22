package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"nib/internal/domain"
)

type fakeMemoryStore struct {
	snapshots         []domain.EntitySnapshot
	events            []domain.EntityStateEvent
	foreshadowings    []domain.Foreshadowing
	arcSummaries      []domain.ArcSummary
	pendingDelta      *domain.ChapterDelta
	pendingDeltas     []domain.ChapterDelta
	dirtyChapters     []domain.DirtyChapterItem
	clearDirtyRes     domain.ClearDirtyChaptersResult
	reports           []domain.BookConsistencyReport
	applyResult       domain.ApplyDeltaResult
	searchHits        []domain.MemorySearchHit
	lastSearchQuery   string
	lastFSStatus      domain.ForeshadowingStatus
	lastArcLevel      domain.ArcSummaryLevel
	deletedForeshadow int64
}

func (f *fakeMemoryStore) SnapshotAt(_ context.Context, _ int64, _ int, _ bool) ([]domain.EntitySnapshot, error) {
	return f.snapshots, nil
}

func (f *fakeMemoryStore) ListStateEvents(_ context.Context, _ int64, _ int64, _ int, _ bool) ([]domain.EntityStateEvent, error) {
	return f.events, nil
}

func (f *fakeMemoryStore) ListForeshadowings(_ context.Context, _ int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error) {
	f.lastFSStatus = status
	return f.foreshadowings, nil
}

func (f *fakeMemoryStore) UpsertForeshadowing(_ context.Context, in domain.UpsertForeshadowingInput) (domain.Foreshadowing, error) {
	return domain.Foreshadowing{
		ID:                 11,
		BookID:             in.BookID,
		Title:              in.Title,
		Description:        in.Description,
		PlantedChapterSeq:  in.PlantedChapterSeq,
		TargetResolveSeq:   in.TargetResolveSeq,
		Status:             in.Status,
		InvolvedEntityIDs:  in.InvolvedEntityIDs,
	}, nil
}

func (f *fakeMemoryStore) DeleteForeshadowing(_ context.Context, id int64) error {
	f.deletedForeshadow = id
	return nil
}

func (f *fakeMemoryStore) ListStoryDeadlines(_ context.Context, _ int64, _ domain.StoryDeadlineStatus) ([]domain.StoryDeadline, error) {
	return []domain.StoryDeadline{}, nil
}

func (f *fakeMemoryStore) UpsertStoryDeadline(_ context.Context, in domain.UpsertStoryDeadlineInput) (domain.StoryDeadline, error) {
	return domain.StoryDeadline{ID: 31, BookID: in.BookID, Title: in.Title, Status: in.Status}, nil
}

func (f *fakeMemoryStore) DeleteStoryDeadline(_ context.Context, _ int64) error {
	return nil
}

func (f *fakeMemoryStore) ListArcSummaries(_ context.Context, _ int64, level domain.ArcSummaryLevel) ([]domain.ArcSummary, error) {
	f.lastArcLevel = level
	return f.arcSummaries, nil
}

func (f *fakeMemoryStore) UpsertArcSummary(_ context.Context, in domain.UpsertArcSummaryInput) (domain.ArcSummary, error) {
	return domain.ArcSummary{
		ID:       21,
		BookID:   in.BookID,
		Level:    in.Level,
		VolumeID: in.VolumeID,
		StartSeq: in.StartSeq,
		EndSeq:   in.EndSeq,
		Summary:  in.Summary,
	}, nil
}

func (f *fakeMemoryStore) GetPendingDelta(_ context.Context, _ int64) (*domain.ChapterDelta, error) {
	return f.pendingDelta, nil
}

func (f *fakeMemoryStore) ListPendingDeltas(_ context.Context, _ int64) ([]domain.ChapterDelta, error) {
	return f.pendingDeltas, nil
}

func (f *fakeMemoryStore) ListDirtyChapters(_ context.Context, _ int64) ([]domain.DirtyChapterItem, error) {
	return f.dirtyChapters, nil
}

func (f *fakeMemoryStore) ClearDirtyChapters(_ context.Context, _ int64, _ []int64) (domain.ClearDirtyChaptersResult, error) {
	return f.clearDirtyRes, nil
}

func (f *fakeMemoryStore) ListConsistencyReports(_ context.Context, _ int64) ([]domain.BookConsistencyReport, error) {
	return f.reports, nil
}

func (f *fakeMemoryStore) UpdateConsistencyFindingStatus(_ context.Context, reportID int64, findingID string, status domain.ConsistencyFindingStatus) (domain.BookConsistencyReport, error) {
	for i := range f.reports {
		if f.reports[i].ID == reportID {
			for j := range f.reports[i].Findings {
				if f.reports[i].Findings[j].ID == findingID {
					f.reports[i].Findings[j].Status = status
					return f.reports[i], nil
				}
			}
		}
	}
	return domain.BookConsistencyReport{}, domain.ErrNotFound
}

func (f *fakeMemoryStore) DeleteConsistencyReport(_ context.Context, _ int64) error {
	return nil
}

func (f *fakeMemoryStore) ApplyDeltaDecisions(_ context.Context, _ domain.ApplyDeltaInput) (domain.ApplyDeltaResult, error) {
	return f.applyResult, nil
}

func (f *fakeMemoryStore) SearchMemory(_ context.Context, _ int64, query string, _ int) ([]domain.MemorySearchHit, error) {
	f.lastSearchQuery = query
	return f.searchHits, nil
}

type fakeDeltaExtractor struct {
	delta   domain.ChapterDelta
	blockCh chan struct{}
	started chan struct{}
}

func (e *fakeDeltaExtractor) ExtractChapterDelta(ctx context.Context, chapterID int64) (domain.ChapterDelta, error) {
	if e.started != nil {
		close(e.started)
	}
	if e.blockCh != nil {
		select {
		case <-ctx.Done():
			return domain.ChapterDelta{}, ctx.Err()
		case <-e.blockCh:
		}
	}
	d := e.delta
	d.ChapterID = chapterID
	return d, nil
}

func TestMemoryServiceNilRuntimeGuard(t *testing.T) {
	ctx := context.Background()
	svc := NewMemoryService(NewRuntime())

	if _, err := svc.SnapshotAt(ctx, 1, 10, false); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("SnapshotAt err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.ListStateEvents(ctx, 1, 0, 0, false); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("ListStateEvents err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.ListForeshadowings(ctx, 1, ""); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("ListForeshadowings err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.ExtractChapterDelta(ctx, 1); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("ExtractChapterDelta err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.GetPendingDelta(ctx, 1); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("GetPendingDelta err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{DeltaID: 1}); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("ApplyDeltaDecisions err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.SearchMemory(ctx, 1, "林渊", 0); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("SearchMemory err = %v, want ErrRuntimeNotReady", err)
	}
}

func TestMemoryServiceValidationAndBehavior(t *testing.T) {
	ctx := context.Background()
	ms := &fakeMemoryStore{
		snapshots: []domain.EntitySnapshot{{Entity: domain.Entity{ID: 1, Name: "林渊"}}},
		searchHits: []domain.MemorySearchHit{
			{Source: domain.MemorySearchSourceChapter, ChapterID: 42, ChapterSeq: 42, Title: "破障反杀", Snippet: "林渊反杀", Score: 1.5},
		},
		applyResult: domain.ApplyDeltaResult{
			Delta:                 domain.ChapterDelta{ID: 9, ChapterID: 42, ReviewStatus: domain.DeltaReviewApplied},
			CommittedEvents:       2,
			UpdatedForeshadowings: 1,
		},
	}
	ext := &fakeDeltaExtractor{
		delta: domain.ChapterDelta{
			ID:            9,
			CausalSummary: "林渊反杀夺丹。",
			ReviewStatus:  domain.DeltaReviewPending,
		},
	}

	rt := NewRuntime()
	rt.Init(RuntimeDeps{
		MemoryStore:    ms,
		DeltaExtractor: ext,
	})
	svc := NewMemoryService(rt)

	// 校验非法参数
	if _, err := svc.SnapshotAt(ctx, 0, 1, false); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bookId=0, got %v", err)
	}
	if _, err := svc.SnapshotAt(ctx, 1, -1, false); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for chapterSeq=-1, got %v", err)
	}
	if _, err := svc.ListForeshadowings(ctx, 1, "not_a_status"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bad foreshadowing status, got %v", err)
	}
	if _, err := svc.ListArcSummaries(ctx, 1, "bad_level"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bad arc summary level, got %v", err)
	}
	if err := svc.DeleteForeshadowing(ctx, 0); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for id=0, got %v", err)
	}
	if _, err := svc.SearchMemory(ctx, 1, "林渊", -2); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for maxChapterSeq=-2, got %v", err)
	}

	// SnapshotAt 保证 EffectiveAttributes 非 nil
	snaps, err := svc.SnapshotAt(ctx, 1, 42, true)
	if err != nil || len(snaps) != 1 || snaps[0].EffectiveAttributes == nil {
		t.Fatalf("SnapshotAt = (%+v, %v)", snaps, err)
	}

	// UpsertForeshadowing 自动补默认 status="planted" 且切片非 nil
	fs, err := svc.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
		BookID:            1,
		Title:             "血玉令牌密库",
		PlantedChapterSeq: 42,
		TargetResolveSeq:  75,
	})
	if err != nil || fs.Status != domain.ForeshadowingPlanted || fs.InvolvedEntityIDs == nil {
		t.Fatalf("UpsertForeshadowing = (%+v, %v)", fs, err)
	}
	if _, err := svc.ListForeshadowings(ctx, 1, "planted"); err != nil || ms.lastFSStatus != domain.ForeshadowingPlanted {
		t.Fatalf("ListForeshadowings status filter mismatch: err=%v got=%q", err, ms.lastFSStatus)
	}
	if err := svc.DeleteForeshadowing(ctx, fs.ID); err != nil || ms.deletedForeshadow != fs.ID {
		t.Fatalf("DeleteForeshadowing err=%v deleted=%d", err, ms.deletedForeshadow)
	}

	// ArcSummaries
	if _, err := svc.UpsertArcSummary(ctx, domain.UpsertArcSummaryInput{
		BookID:   1,
		Level:    domain.ArcSummaryLevelArc10,
		StartSeq: 1,
		EndSeq:   10,
		Summary:  "前十章总结",
	}); err != nil {
		t.Fatalf("UpsertArcSummary failed: %v", err)
	}
	if _, err := svc.ListArcSummaries(ctx, 1, "arc_10"); err != nil || ms.lastArcLevel != domain.ArcSummaryLevelArc10 {
		t.Fatalf("ListArcSummaries err=%v level=%q", err, ms.lastArcLevel)
	}

	// Empty query short-circuits without hitting store
	emptyHits, err := svc.SearchMemory(ctx, 1, "   ", 0)
	if err != nil || len(emptyHits) != 0 || ms.lastSearchQuery != "" {
		t.Fatalf("expected empty query short-circuit, got hits=%v err=%v lastQuery=%q", emptyHits, err, ms.lastSearchQuery)
	}
	hits, err := svc.SearchMemory(ctx, 1, " 林渊 ", 50)
	if err != nil || len(hits) != 1 || ms.lastSearchQuery != "林渊" {
		t.Fatalf("SearchMemory = (%+v, %v) lastQuery=%q", hits, err, ms.lastSearchQuery)
	}

	// GetPendingDelta returns nil cleanly when absent, normalized struct when present
	if d, err := svc.GetPendingDelta(ctx, 42); err != nil || d != nil {
		t.Fatalf("expected nil pending delta, got (%+v, %v)", d, err)
	}
	ms.pendingDelta = &domain.ChapterDelta{ID: 9, ChapterID: 42, ReviewStatus: domain.DeltaReviewPending}
	gotDelta, err := svc.GetPendingDelta(ctx, 42)
	if err != nil || gotDelta == nil || gotDelta.StatePatches == nil || gotDelta.NewEntities == nil || gotDelta.Foreshadows == nil {
		t.Fatalf("GetPendingDelta did not normalize slices: %+v err=%v", gotDelta, err)
	}

	// ExtractChapterDelta & ApplyDeltaDecisions normalize nil slices
	extracted, err := svc.ExtractChapterDelta(ctx, 42)
	if err != nil || extracted.ChapterID != 42 || extracted.StatePatches == nil {
		t.Fatalf("ExtractChapterDelta = (%+v, %v)", extracted, err)
	}
	applied, err := svc.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{
		DeltaID:            9,
		ApplyCausalSummary: true,
		Decisions:          []domain.DeltaDecision{{Kind: domain.DeltaDecisionKindStatePatch, ItemID: "p1", Accept: true}},
	})
	if err != nil || applied.CommittedEvents != 2 || applied.MarkedDirtyChapterIDs == nil {
		t.Fatalf("ApplyDeltaDecisions = (%+v, %v)", applied, err)
	}
}

func TestRuntimeCloseCancelsInFlightDeltaExtraction(t *testing.T) {
	started := make(chan struct{})
	ext := &fakeDeltaExtractor{
		started: started,
		blockCh: make(chan struct{}),
	}
	rt := NewRuntime()
	rt.Init(RuntimeDeps{DeltaExtractor: ext})
	svc := NewMemoryService(rt)

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.ExtractChapterDelta(context.Background(), 42)
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("extractor did not start in time")
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("rt.Close failed: %v", err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled on rt.Close(), got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ExtractChapterDelta did not exit after rt.Close()")
	}
}

func TestMemoryServiceConsistencyAndDirtyRPCs(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "体检测试", Genre: domain.GenreXianxia})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	ch, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第一章"})
	ch.Status = domain.ChapterStatusUpstreamDirty
	st.chapters[ch.ID] = ch

	ms := &fakeMemoryStore{
		dirtyChapters: []domain.DirtyChapterItem{{ChapterID: ch.ID, GlobalSeq: 1, Title: "第一章"}},
		clearDirtyRes: domain.ClearDirtyChaptersResult{ClearedCount: 1, ClearedChapterIDs: []int64{ch.ID}},
		reports: []domain.BookConsistencyReport{{
			ID:        7,
			BookID:    book.ID,
			ScopeType: domain.ConsistencyScopeBook,
			Findings:  []domain.AIConsistencyFinding{{ID: "ai-1", Title: "战力冲突", Status: domain.ConsistencyFindingOpen}},
		}},
	}
	rt := NewRuntime()
	rt.Init(RuntimeDeps{Store: st, MemoryStore: ms})
	svc := NewMemoryService(rt)

	if _, err := svc.RunLocalConsistencyAudit(ctx, 0, domain.LocalAuditOptions{}); err == nil {
		t.Fatalf("expected validation error for bookId=0")
	}
	localRep, err := svc.RunLocalConsistencyAudit(ctx, book.ID, domain.LocalAuditOptions{})
	if err != nil {
		t.Fatalf("RunLocalConsistencyAudit: %v", err)
	}
	if localRep.Summary.UpstreamDirtyCount != 1 || len(localRep.Findings) != 1 {
		t.Fatalf("expected 1 upstream_dirty finding, got %+v", localRep)
	}

	dirtyList, err := svc.ListDirtyChapters(ctx, book.ID)
	if err != nil || len(dirtyList) != 1 {
		t.Fatalf("ListDirtyChapters = (%+v, %v)", dirtyList, err)
	}
	cleared, err := svc.ClearDirtyChapters(ctx, book.ID, []int64{ch.ID})
	if err != nil || cleared.ClearedCount != 1 {
		t.Fatalf("ClearDirtyChapters = (%+v, %v)", cleared, err)
	}

	if _, err := svc.UpdateConsistencyFindingStatus(ctx, 7, "ai-1", "invalid"); err == nil {
		t.Fatalf("expected validation error for invalid finding status")
	}
	updatedRep, err := svc.UpdateConsistencyFindingStatus(ctx, 7, "ai-1", "resolved")
	if err != nil || updatedRep.Findings[0].Status != domain.ConsistencyFindingResolved {
		t.Fatalf("UpdateConsistencyFindingStatus = (%+v, %v)", updatedRep, err)
	}
}
