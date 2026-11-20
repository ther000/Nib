package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/llm"
)

type fakeBatchStore struct {
	*fakeStyleStore
	chaptersByID map[int64]domain.Chapter
	beatsByChap  map[int64][]domain.Beat
	runs         map[int64]*domain.BatchRun
	itemsByID    map[int64]*domain.BatchRunItem
	usageLogs    []domain.TokenUsageLog
	nextChapID   int64
	nextBatchID  int64
	nextItemID   int64
}

func newFakeBatchStore() *fakeBatchStore {
	base := newFakeStyleStore("")
	providerID := int64(7)
	base.routes[domain.StageExtract] = domain.ModelRoute{
		Stage:       domain.StageExtract,
		ProviderID:  &providerID,
		ModelName:   "deepseek-chat",
		Temperature: 0.1,
		MaxTokens:   2048,
	}
	ch1 := base.chapter
	ch1.ID = 101
	ch1.GlobalSeq = 1
	ch1.VolSeq = 1
	ch1.Title = "初入坊市"
	ch1.Content = ""
	ch1.Status = domain.ChapterStatusOutlined

	ch2 := domain.Chapter{
		ID:         102,
		BookID:     base.book.ID,
		VolumeID:   base.volume.ID,
		GlobalSeq:  2,
		VolSeq:     2,
		Title:      "黑市反杀",
		Status:     domain.ChapterStatusOutlined,
		EmotionTag: domain.EmotionBurst,
		HookType:   domain.HookCrisis,
	}

	return &fakeBatchStore{
		fakeStyleStore: base,
		chaptersByID: map[int64]domain.Chapter{
			ch1.ID: ch1,
			ch2.ID: ch2,
		},
		beatsByChap: map[int64][]domain.Beat{
			ch1.ID: base.beats,
			ch2.ID: base.beats,
		},
		runs:       map[int64]*domain.BatchRun{},
		itemsByID:  map[int64]*domain.BatchRunItem{},
		nextChapID: 200,
	}
}

func (s *fakeBatchStore) GetChapter(_ context.Context, id int64) (domain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.chaptersByID[id]
	if !ok {
		return domain.Chapter{}, domain.ErrNotFound
	}
	return ch, nil
}

func (s *fakeBatchStore) ListBeats(_ context.Context, chapterID int64) ([]domain.Beat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beatsByChap[chapterID], nil
}

func (s *fakeBatchStore) SaveChapterContent(
	ctx context.Context,
	chapterID int64,
	content string,
	status domain.ChapterStatus,
	source domain.VersionSource,
	modelName string,
	briefingJSON string,
) (domain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return domain.Chapter{}, ctx.Err()
	}
	ch, ok := s.chaptersByID[chapterID]
	if !ok {
		return domain.Chapter{}, domain.ErrNotFound
	}
	ch.Content = content
	ch.WordCount = domain.CountWords(content)
	ch.Status = status
	ch.LastBriefingJSON = briefingJSON
	s.chaptersByID[chapterID] = ch
	return ch, nil
}

func (s *fakeBatchStore) ListChapters(_ context.Context, volumeID int64) ([]domain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Chapter
	for _, ch := range s.chaptersByID {
		if ch.VolumeID == volumeID {
			out = append(out, ch)
		}
	}
	return out, nil
}

func (s *fakeBatchStore) ListBookChapters(_ context.Context, bookID int64) ([]domain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Chapter
	for _, ch := range s.chaptersByID {
		if ch.BookID == bookID {
			out = append(out, ch)
		}
	}
	return out, nil
}

func (s *fakeBatchStore) CreateChapter(_ context.Context, in domain.CreateChapterInput) (domain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextChapID++
	seq := len(s.chaptersByID) + 1
	ch := domain.Chapter{
		ID:             s.nextChapID,
		BookID:         in.BookID,
		VolumeID:       in.VolumeID,
		GlobalSeq:      seq,
		VolSeq:         seq,
		Title:          in.Title,
		Status:         domain.ChapterStatusPlanned,
		EmotionTag:     in.EmotionTag,
		HookType:       in.HookType,
		HookNote:       in.HookNote,
		OutlineSummary: in.OutlineSummary,
	}
	s.chaptersByID[ch.ID] = ch
	return ch, nil
}

func (s *fakeBatchStore) UpdateChapterMeta(_ context.Context, in domain.UpdateChapterMetaInput) (domain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.chaptersByID[in.ID]
	if !ok {
		return domain.Chapter{}, domain.ErrNotFound
	}
	ch.Title = in.Title
	if in.Status != "" {
		ch.Status = in.Status
	}
	if in.EmotionTag != "" {
		ch.EmotionTag = in.EmotionTag
	}
	if in.HookType != "" {
		ch.HookType = in.HookType
	}
	ch.HookNote = in.HookNote
	ch.OutlineSummary = in.OutlineSummary
	s.chaptersByID[ch.ID] = ch
	return ch, nil
}

func (s *fakeBatchStore) ReplaceBeats(_ context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.beatsByChap[chapterID] = append([]domain.Beat(nil), beats...)
	return s.beatsByChap[chapterID], nil
}

func (s *fakeBatchStore) CreateBatchRun(_ context.Context, bookID int64, chapterIDs []int64, mode domain.BatchMode) (domain.BatchRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextBatchID++
	run := &domain.BatchRun{
		ID:         s.nextBatchID,
		BookID:     bookID,
		Mode:       mode,
		Status:     domain.BatchRunRunning,
		TotalItems: len(chapterIDs),
		Items:      make([]domain.BatchRunItem, 0, len(chapterIDs)),
	}
	for i, chID := range chapterIDs {
		s.nextItemID++
		ch := s.chaptersByID[chID]
		it := domain.BatchRunItem{
			ID:               s.nextItemID,
			BatchRunID:       run.ID,
			ChapterID:        chID,
			ChapterGlobalSeq: ch.GlobalSeq,
			ChapterTitle:     ch.Title,
			Seq:              i + 1,
			State:            domain.BatchItemQueued,
		}
		itCopy := it
		s.itemsByID[it.ID] = &itCopy
		run.Items = append(run.Items, it)
	}
	s.runs[run.ID] = run
	return *run, nil
}

func (s *fakeBatchStore) GetBatchRun(_ context.Context, batchID int64) (domain.BatchRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[batchID]
	if !ok {
		return domain.BatchRun{}, domain.ErrNotFound
	}
	cp := *r
	cp.Items = make([]domain.BatchRunItem, len(r.Items))
	for i, it := range r.Items {
		cp.Items[i] = *s.itemsByID[it.ID]
	}
	return cp, nil
}

func (s *fakeBatchStore) GetLatestBatchRun(_ context.Context, bookID int64) (*domain.BatchRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latestID int64
	for id, r := range s.runs {
		if r.BookID == bookID && id > latestID {
			latestID = id
		}
	}
	if latestID == 0 {
		return nil, nil
	}
	cp := *s.runs[latestID]
	return &cp, nil
}

func (s *fakeBatchStore) GetBatchRunItem(_ context.Context, itemID int64) (domain.BatchRunItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.itemsByID[itemID]
	if !ok {
		return domain.BatchRunItem{}, domain.ErrNotFound
	}
	return *it, nil
}

func (s *fakeBatchStore) UpdateBatchItemState(_ context.Context, itemID int64, state domain.BatchItemState, inputHash, errorMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.itemsByID[itemID]
	if !ok {
		return domain.ErrNotFound
	}
	it.State = state
	if inputHash != "" {
		it.InputHash = inputHash
	}
	it.ErrorMsg = errorMsg
	return nil
}

func (s *fakeBatchStore) UpdateBatchRunStatus(_ context.Context, batchID int64, status domain.BatchRunStatus, doneItems int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[batchID]
	if !ok {
		return domain.ErrNotFound
	}
	r.Status = status
	r.DoneItems = doneItems
	return nil
}

func (s *fakeBatchStore) RecordTokenUsage(_ context.Context, entry domain.TokenUsageLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usageLogs = append(s.usageLogs, entry)
	return nil
}

func (s *fakeBatchStore) GetUsageSummary(_ context.Context, bookID int64) (domain.UsageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := domain.UsageSummary{BookID: bookID}
	for _, l := range s.usageLogs {
		if bookID > 0 && l.BookID != bookID {
			continue
		}
		sum.TotalCalls++
		sum.TotalPromptTokens += l.PromptTokens
		sum.TotalCompletionTokens += l.CompletionTokens
		sum.TotalTokens += l.PromptTokens + l.CompletionTokens
	}
	return sum, nil
}

type fakeDeltaExtractor struct {
	mu    sync.Mutex
	calls []int64
}

func (e *fakeDeltaExtractor) ExtractChapterDelta(_ context.Context, chapterID int64) (domain.ChapterDelta, error) {
	e.mu.Lock()
	e.calls = append(e.calls, chapterID)
	e.mu.Unlock()
	return domain.ChapterDelta{ID: chapterID, ChapterID: chapterID, ReviewStatus: domain.DeltaReviewPending}, nil
}

func TestBatchGenerateOutlinesWithRepairAndPersistence(t *testing.T) {
	store := newFakeBatchStore()
	calls := 0
	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, _ llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			calls++
			if calls == 1 {
				_ = onEvent(llm.StreamEvent{Delta: `{"outlines": broken`})
				return domain.TokenUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}, nil
			}
			payload := `{
				"outlines": [
					{
						"title": "夜探黑市",
						"emotion_tag": "suppress",
						"hook_type": "mystery",
						"hook_note": "黑市拍品竟刻着宗门禁纹",
						"outline_summary": "林渊乔装入市查探破障丹线索。",
						"beats": [
							{"stage": "setup", "summary": "进入黑市", "target_words": 500},
							{"stage": "hook", "summary": "发现古宗令牌", "target_words": 400}
						]
					},
					{
						"title": "雷阵反杀",
						"emotion_tag": "burst",
						"hook_type": "crisis",
						"hook_note": "长老神识掠过全城",
						"outline_summary": "借雷阵击溃王执事并夺丹脱身。",
						"beats": [
							{"stage": "burst", "summary": "引爆残阵反杀", "target_words": 700}
						]
					}
				]
			}`
			_ = onEvent(llm.StreamEvent{Delta: payload})
			return domain.TokenUsage{PromptTokens: 300, CompletionTokens: 180, TotalTokens: 480}, nil
		},
	}

	runner := New(store, compiler.New(store), streamer, nil)
	var sawCompleted bool
	chaps, err := runner.BatchGenerateOutlines(context.Background(), domain.BatchOutlineRequest{
		BookID:   store.book.ID,
		VolumeID: store.volume.ID,
		StartSeq: 3,
		Count:    2,
		ArcGoal:  "黑市夺宝并引出更高势力",
	}, func(ch domain.OutlineChunk) error {
		if ch.Kind == domain.GenChunkCompleted {
			sawCompleted = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("BatchGenerateOutlines failed: %v", err)
	}
	if calls != 2 || len(chaps) != 2 || !sawCompleted {
		t.Fatalf("unexpected outline generation result: calls=%d len=%d completed=%v", calls, len(chaps), sawCompleted)
	}
	if chaps[0].Status != domain.ChapterStatusOutlined || len(store.beatsByChap[chaps[0].ID]) != 2 {
		t.Fatalf("expected outlined chapter with 2 beats, got ch=%+v beats=%+v", chaps[0], store.beatsByChap[chaps[0].ID])
	}
	if len(store.usageLogs) == 0 || store.usageLogs[0].Stage != domain.StageOutline {
		t.Fatalf("expected outline token usage logged, got %+v", store.usageLogs)
	}
}

func TestStartBatchRunSerialExecutionAndRetry(t *testing.T) {
	store := newFakeBatchStore()
	extractor := &fakeDeltaExtractor{}

	var mu sync.Mutex
	var progressEvents []domain.BatchProgress

	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			sys := req.System
			switch {
			case strings.Contains(sys, "独立审校官"):
				_ = onEvent(llm.StreamEvent{Delta: `{"hook_passed": true, "hook_verdict": "钩子有力", "critic_findings": []}`})
			default:
				_ = onEvent(llm.StreamEvent{Delta: "铁剑出鞘，雷光劈开雨幕。\n\n陈青反手收起玉瓶，转身跃出巷口。"})
			}
			return domain.TokenUsage{PromptTokens: 200, CompletionTokens: 90, TotalTokens: 290}, nil
		},
	}

	runner := New(
		store,
		compiler.New(store),
		streamer,
		nil,
		WithDeltaExtractor(extractor),
		WithProgressListener(func(p domain.BatchProgress) {
			mu.Lock()
			progressEvents = append(progressEvents, p)
			mu.Unlock()
		}),
	)

	run, err := runner.StartBatchRun(context.Background(), store.book.ID, []int64{101, 102}, domain.BatchModeFullPipeline)
	if err != nil {
		t.Fatalf("StartBatchRun failed: %v", err)
	}
	runner.WaitAllBatches()

	finalRun, err := store.GetBatchRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetBatchRun failed: %v", err)
	}
	if finalRun.Status != domain.BatchRunCompleted || finalRun.DoneItems != 2 {
		t.Fatalf("expected completed batch run with 2 done items, got %+v", finalRun)
	}
	if len(extractor.calls) != 2 || extractor.calls[0] != 101 || extractor.calls[1] != 102 {
		t.Fatalf("expected serial delta extraction [101, 102], got %v", extractor.calls)
	}
	if len(progressEvents) < 6 {
		t.Fatalf("expected fine-grained progress events emitted, got %d", len(progressEvents))
	}
	if len(store.usageLogs) < 2 {
		t.Fatalf("expected token usage recorded across batch stages, got %d", len(store.usageLogs))
	}

	// 验证 RetryBatchItem：人为将第 2 项标为 failed 后发起单章重跑，应重新完成至 awaiting_review。
	_ = store.UpdateBatchItemState(context.Background(), finalRun.Items[1].ID, domain.BatchItemFailed, "", "mock fail")
	if _, err := runner.RetryBatchItem(context.Background(), finalRun.Items[1].ID); err != nil {
		t.Fatalf("RetryBatchItem failed: %v", err)
	}
	runner.WaitAllBatches()

	retried, _ := store.GetBatchRun(context.Background(), run.ID)
	if retried.Status != domain.BatchRunCompleted || retried.Items[1].State != domain.BatchItemAwaitingReview {
		t.Fatalf("expected retried item awaiting_review & run completed, got %+v", retried)
	}
}

func TestRetryBatchItemRejectsActiveBatchRunConflict(t *testing.T) {
	store := newFakeBatchStore()
	started := make(chan struct{})
	release := make(chan struct{})

	streamer := &fakeLLM{
		fn: func(ctx context.Context, _ domain.Provider, _ llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			select {
			case <-release:
				_ = onEvent(llm.StreamEvent{Delta: "林渊挥剑斩断阵旗。"})
				return domain.TokenUsage{PromptTokens: 100, CompletionTokens: 30, TotalTokens: 130}, nil
			case <-ctx.Done():
				return domain.TokenUsage{}, domain.ErrGenerationCanceled
			}
		},
	}

	runner := New(store, compiler.New(store), streamer, nil)
	run, err := runner.StartBatchRun(context.Background(), store.book.ID, []int64{101, 102}, domain.BatchModeDraftOnly)
	if err != nil {
		t.Fatalf("StartBatchRun failed: %v", err)
	}

	// 等第 1 章进入流式生成后，对第 2 章（仍为 queued）并发调用 RetryBatchItem，必须返回 ErrConflict 且不能破坏当前运行句柄。
	<-started
	_, retryErr := runner.RetryBatchItem(context.Background(), run.Items[1].ID)
	if !errors.Is(retryErr, domain.ErrConflict) {
		close(release)
		runner.WaitAllBatches()
		t.Fatalf("expected ErrConflict when retrying item of an active batch run, got %v", retryErr)
	}

	// 验证原运行句柄依然可正常取消。
	if _, cancelErr := runner.CancelBatchRun(context.Background(), run.ID); cancelErr != nil {
		close(release)
		runner.WaitAllBatches()
		t.Fatalf("CancelBatchRun failed: %v", cancelErr)
	}
	runner.WaitAllBatches()

	canceledRun, _ := store.GetBatchRun(context.Background(), run.ID)
	if canceledRun.Status != domain.BatchRunCanceled {
		t.Fatalf("expected canceled status after CancelBatchRun, got %q", canceledRun.Status)
	}
}
