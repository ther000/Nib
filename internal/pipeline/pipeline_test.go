package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/llm"
)

type fakeStore struct {
	mu           sync.Mutex
	book         domain.Book
	volume       domain.Volume
	chapter      domain.Chapter
	beats        []domain.Beat
	invariants   []domain.Entity
	entities     map[int64]domain.Entity
	prevChapters []domain.Chapter
	routes       map[domain.PipelineStage]domain.ModelRoute
	providers    map[int64]domain.Provider

	versions          []domain.ChapterVersion
	saveCtxErrAtCall  error
	lastSavedSource   domain.VersionSource
	lastSavedModel    string
	lastSavedBriefing string
}

func (s *fakeStore) GetBook(_ context.Context, id int64) (domain.Book, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.book.ID != id {
		return domain.Book{}, domain.ErrNotFound
	}
	return s.book, nil
}

func (s *fakeStore) GetVolume(_ context.Context, id int64) (domain.Volume, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.volume.ID != id {
		return domain.Volume{}, domain.ErrNotFound
	}
	return s.volume, nil
}

func (s *fakeStore) GetChapter(_ context.Context, id int64) (domain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chapter.ID != id {
		return domain.Chapter{}, domain.ErrNotFound
	}
	return s.chapter, nil
}

func (s *fakeStore) ListBeats(_ context.Context, _ int64) ([]domain.Beat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beats, nil
}

func (s *fakeStore) ListInvariants(_ context.Context, _ int64) ([]domain.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.invariants, nil
}

func (s *fakeStore) GetEntitiesByIDs(_ context.Context, ids []int64) ([]domain.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Entity, 0, len(ids))
	for _, id := range ids {
		if e, ok := s.entities[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *fakeStore) GetPreviousChapters(_ context.Context, _ int64, _ int, _ int) ([]domain.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prevChapters, nil
}

func (s *fakeStore) GetModelRoute(_ context.Context, stage domain.PipelineStage) (domain.ModelRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.routes[stage]
	if !ok {
		return domain.ModelRoute{}, domain.ErrNotFound
	}
	return r, nil
}

func (s *fakeStore) GetProvider(_ context.Context, id int64) (domain.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.providers[id]
	if !ok {
		return domain.Provider{}, domain.ErrNotFound
	}
	return p, nil
}

func (s *fakeStore) SaveChapterContent(
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
	s.saveCtxErrAtCall = ctx.Err()
	if ctx.Err() != nil {
		return domain.Chapter{}, ctx.Err()
	}
	if s.chapter.ID != chapterID {
		return domain.Chapter{}, domain.ErrNotFound
	}
	// 模拟 AGENTS.md §4.1：覆写前自动归档旧正文至 chapter_versions。
	if s.chapter.Content != "" && s.chapter.Content != content {
		s.versions = append(s.versions, domain.ChapterVersion{
			ID:        int64(len(s.versions) + 1),
			ChapterID: chapterID,
			Source:    source,
			ModelName: modelName,
			Content:   s.chapter.Content,
			WordCount: domain.CountWords(s.chapter.Content),
		})
	}
	s.chapter.Content = content
	s.chapter.WordCount = domain.CountWords(content)
	s.chapter.Status = status
	s.chapter.LastBriefingJSON = briefingJSON
	s.lastSavedSource = source
	s.lastSavedModel = modelName
	s.lastSavedBriefing = briefingJSON
	return s.chapter, nil
}

type fakeLLM struct {
	fn func(ctx context.Context, provider domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error)
}

func (f *fakeLLM) Stream(
	ctx context.Context,
	provider domain.Provider,
	req llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
) (domain.TokenUsage, error) {
	return f.fn(ctx, provider, req, onEvent)
}

type fakeExporter struct {
	mu      sync.Mutex
	calls   int
	lastCh  domain.Chapter
	retErr  error
	retPath string
}

func (e *fakeExporter) ExportChapter(_ domain.Book, _ domain.Volume, chap domain.Chapter) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.lastCh = chap
	return e.retPath, e.retErr
}

func newBaseStore(exportDir string) *fakeStore {
	providerID := int64(7)
	povID := int64(50)
	return &fakeStore{
		book: domain.Book{
			ID:                 1,
			Title:              "青萍剑主",
			Genre:              domain.GenreXianxia,
			Premise:            "凡人杂役以算筹推演剑阵",
			TargetChapterWords: 2500,
			ExportDir:          exportDir,
		},
		volume: domain.Volume{
			ID:     2,
			BookID: 1,
			Seq:    1,
			Title:  "外门问道",
		},
		chapter: domain.Chapter{
			ID:         3,
			BookID:     1,
			VolumeID:   2,
			GlobalSeq:  5,
			VolSeq:     5,
			Title:      "雨夜破阵",
			Status:     domain.ChapterStatusOutlined,
			EmotionTag: domain.EmotionTurn,
			HookType:   domain.HookCrisis,
			HookNote:   "阵眼崩裂瞬间，宗门警钟长鸣",
			Content:    "旧版初稿内容，用于验证覆写前必归档快照。",
			WordCount:  20,
		},
		beats: []domain.Beat{
			{
				ID:          1,
				ChapterID:   3,
				Seq:         1,
				Stage:       domain.BeatStageTurn,
				Summary:     "主角识破阵纹缺口",
				POVEntityID: &povID,
				EntityIDs:   []int64{50},
				TargetWords: 800,
			},
		},
		invariants: []domain.Entity{
			{
				ID:          11,
				BookID:      1,
				Kind:        domain.EntityKindRule,
				Name:        "灵石守恒",
				IsInvariant: true,
				Summary:     "一切法阵催动皆需实打实灵石消耗",
			},
		},
		entities: map[int64]domain.Entity{
			50: {
				ID:      50,
				BookID:  1,
				Kind:    domain.EntityKindCharacter,
				Name:    "陈青",
				Summary: "杂役峰弟子",
			},
		},
		routes: map[domain.PipelineStage]domain.ModelRoute{
			domain.StageDraft: {
				Stage:       domain.StageDraft,
				ProviderID:  &providerID,
				ModelName:   "deepseek-chat",
				Temperature: 0.8,
				MaxTokens:   4096,
			},
		},
		providers: map[int64]domain.Provider{
			providerID: {
				ID:       providerID,
				Name:     "DeepSeek",
				Protocol: domain.ProtocolOpenAICompat,
				BaseURL:  "https://api.example.com/v1",
				APIKey:   "sk-pipeline-test-1234",
				Models:   []string{"deepseek-chat"},
				Enabled:  true,
			},
		},
	}
}

func TestGenerateChapterHappyPathAndExport(t *testing.T) {
	store := newBaseStore(`E:\Exports\Nib`)
	comp := compiler.New(store)
	exp := &fakeExporter{retPath: `E:\Exports\Nib\青萍剑主\第01卷_外门问道\第005章_雨夜破阵.md`}

	var capturedReq llm.ChatRequest
	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			capturedReq = req
			for _, part := range []string{"夜雨砸在青石阶上。", "陈青袖中灵石尽碎，阵纹随之倒转。"} {
				if err := onEvent(llm.StreamEvent{Delta: part}); err != nil {
					return domain.TokenUsage{}, err
				}
			}
			u := domain.TokenUsage{PromptTokens: 410, CompletionTokens: 92, TotalTokens: 502}
			if err := onEvent(llm.StreamEvent{Usage: &u, FinishReason: "stop"}); err != nil {
				return u, err
			}
			return u, nil
		},
	}

	temp := 0.9
	maxToks := 3200
	runner := New(store, comp, streamer, exp)

	var chunks []domain.GenChunk
	chap, err := runner.GenerateChapter(context.Background(), 3, domain.GenerateChapterOptions{
		ExtraInstruction: "多用短句强调雨夜压迫感",
		Temperature:      &temp,
		MaxTokens:        &maxToks,
	}, func(ch domain.GenChunk) error {
		chunks = append(chunks, ch)
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateChapter failed: %v", err)
	}

	if chap.Status != domain.ChapterStatusReviewing {
		t.Fatalf("expected status=reviewing, got %q", chap.Status)
	}
	if chap.Content != "夜雨砸在青石阶上。陈青袖中灵石尽碎，阵纹随之倒转。" {
		t.Fatalf("unexpected saved content: %q", chap.Content)
	}
	if store.lastSavedSource != domain.VersionSourceAIDraft || store.lastSavedModel != "deepseek-chat" {
		t.Fatalf("unexpected save metadata: source=%q model=%q", store.lastSavedSource, store.lastSavedModel)
	}
	if len(store.versions) != 1 || store.versions[0].Content != "旧版初稿内容，用于验证覆写前必归档快照。" {
		t.Fatalf("expected old content archived before overwrite, got versions=%+v", store.versions)
	}

	// 验证简报 JSON 可反序列化且包含附加指令。
	var savedBrief domain.Briefing
	if err := json.Unmarshal([]byte(chap.LastBriefingJSON), &savedBrief); err != nil {
		t.Fatalf("invalid saved briefing JSON: %v", err)
	}
	if !strings.Contains(capturedReq.Messages[0].Content, "多用短句强调雨夜压迫感") {
		t.Fatalf("expected ExtraInstruction in UserPrompt")
	}
	if capturedReq.Temperature != 0.9 || capturedReq.MaxTokens != 3200 {
		t.Fatalf("expected temperature/maxTokens overrides applied, got temp=%v max=%d", capturedReq.Temperature, capturedReq.MaxTokens)
	}

	// 验证单向导出被触发。
	if exp.calls != 1 || exp.lastCh.Status != domain.ChapterStatusReviewing {
		t.Fatalf("expected exporter called once with reviewing chapter, calls=%d ch=%+v", exp.calls, exp.lastCh)
	}

	// 验证流式事件序列：started -> delta(2) -> usage -> completed。
	if len(chunks) != 5 {
		t.Fatalf("expected 5 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Kind != domain.GenChunkStarted ||
		chunks[1].Kind != domain.GenChunkDelta ||
		chunks[2].Kind != domain.GenChunkDelta ||
		chunks[3].Kind != domain.GenChunkUsage ||
		chunks[4].Kind != domain.GenChunkCompleted {
		t.Fatalf("unexpected chunk kind order: %+v", chunks)
	}
	if chunks[4].Usage == nil || chunks[4].Usage.TotalTokens != 502 {
		t.Fatalf("expected completed chunk with usage=502, got %+v", chunks[4].Usage)
	}
}

func TestGenerateChapterMidStreamCancelPreservesPartial(t *testing.T) {
	store := newBaseStore(`E:\Exports\Nib`)
	comp := compiler.New(store)
	exp := &fakeExporter{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamer := &fakeLLM{
		fn: func(sCtx context.Context, _ domain.Provider, _ llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			if err := onEvent(llm.StreamEvent{Delta: "半章正文已流出，"}); err != nil {
				return domain.TokenUsage{}, err
			}
			if err := onEvent(llm.StreamEvent{Delta: "绝不能因点击停止而丢失。"}); err != nil {
				return domain.TokenUsage{}, err
			}
			cancel()
			<-sCtx.Done()
			return domain.TokenUsage{}, domain.ErrGenerationCanceled
		},
	}

	runner := New(store, comp, streamer, exp, WithSaveTimeout(2*time.Second))

	var sawPartialChunk bool
	savedChap, err := runner.GenerateChapter(ctx, 3, domain.GenerateChapterOptions{}, func(ch domain.GenChunk) error {
		if ch.Kind == domain.GenChunkPartial {
			sawPartialChunk = true
		}
		return nil
	})

	if !errors.Is(err, domain.ErrGenerationCanceled) {
		t.Fatalf("expected ErrGenerationCanceled, got %v", err)
	}
	if store.saveCtxErrAtCall != nil {
		t.Fatalf("expected detached save context with nil Err, got %v", store.saveCtxErrAtCall)
	}
	if savedChap.Status != domain.ChapterStatusPartial {
		t.Fatalf("expected partial status, got %q", savedChap.Status)
	}
	if savedChap.Content != "半章正文已流出，绝不能因点击停止而丢失。" {
		t.Fatalf("expected partial prose preserved, got %q", savedChap.Content)
	}
	if len(store.versions) != 1 {
		t.Fatalf("expected previous content archived to chapter_versions before saving partial, got %d", len(store.versions))
	}
	if exp.calls != 1 || exp.lastCh.Status != domain.ChapterStatusPartial {
		t.Fatalf("expected partial chapter exported when ExportDir is set, calls=%d", exp.calls)
	}
	if !sawPartialChunk {
		t.Fatalf("expected GenChunkPartial emitted on cancel")
	}
}

func TestGenerateChapterCallbackStopPreservesPartial(t *testing.T) {
	store := newBaseStore("")
	comp := compiler.New(store)
	exp := &fakeExporter{}

	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, _ llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			if err := onEvent(llm.StreamEvent{Delta: "第一段正文。"}); err != nil {
				return domain.TokenUsage{}, err
			}
			if err := onEvent(llm.StreamEvent{Delta: "第二段正文。"}); err != nil {
				return domain.TokenUsage{}, err
			}
			return domain.TokenUsage{}, nil
		},
	}

	runner := New(store, comp, streamer, exp)
	deltaCount := 0
	savedChap, err := runner.GenerateChapter(context.Background(), 3, domain.GenerateChapterOptions{}, func(ch domain.GenChunk) error {
		if ch.Kind == domain.GenChunkDelta {
			deltaCount++
			if deltaCount == 2 {
				return errors.New("channel closed by ui")
			}
		}
		return nil
	})
	if !errors.Is(err, domain.ErrGenerationCanceled) {
		t.Fatalf("expected ErrGenerationCanceled when onChunk stops stream, got %v", err)
	}
	if savedChap.Status != domain.ChapterStatusPartial || savedChap.Content != "第一段正文。第二段正文。" {
		t.Fatalf("expected partial prose persisted, got status=%q content=%q", savedChap.Status, savedChap.Content)
	}
	// ExportDir 为空时不调用 exporter。
	if exp.calls != 0 {
		t.Fatalf("expected exporter not called when ExportDir is empty, got %d", exp.calls)
	}
}

func TestGenerateChapterMissingRouteAndProvider(t *testing.T) {
	t.Run("missing route row", func(t *testing.T) {
		store := newBaseStore("")
		delete(store.routes, domain.StageDraft)
		runner := New(store, compiler.New(store), &fakeLLM{}, nil)

		_, err := runner.GenerateChapter(context.Background(), 3, domain.GenerateChapterOptions{}, nil)
		if !errors.Is(err, domain.ErrRouteNotConfigured) {
			t.Fatalf("expected ErrRouteNotConfigured, got %v", err)
		}
	})

	t.Run("route without model name", func(t *testing.T) {
		store := newBaseStore("")
		r := store.routes[domain.StageDraft]
		r.ModelName = ""
		store.routes[domain.StageDraft] = r
		runner := New(store, compiler.New(store), &fakeLLM{}, nil)

		_, err := runner.GenerateChapter(context.Background(), 3, domain.GenerateChapterOptions{}, nil)
		if !errors.Is(err, domain.ErrRouteNotConfigured) {
			t.Fatalf("expected ErrRouteNotConfigured, got %v", err)
		}
	})

	t.Run("disabled provider", func(t *testing.T) {
		store := newBaseStore("")
		p := store.providers[7]
		p.Enabled = false
		store.providers[7] = p
		runner := New(store, compiler.New(store), &fakeLLM{}, nil)

		_, err := runner.GenerateChapter(context.Background(), 3, domain.GenerateChapterOptions{}, nil)
		if !errors.Is(err, domain.ErrProviderNotConfigured) {
			t.Fatalf("expected ErrProviderNotConfigured, got %v", err)
		}
	})

	t.Run("missing provider", func(t *testing.T) {
		store := newBaseStore("")
		delete(store.providers, 7)
		runner := New(store, compiler.New(store), &fakeLLM{}, nil)

		_, err := runner.GenerateChapter(context.Background(), 3, domain.GenerateChapterOptions{}, nil)
		if !errors.Is(err, domain.ErrProviderNotConfigured) {
			t.Fatalf("expected ErrProviderNotConfigured, got %v", err)
		}
	})
}

func TestGenerateChapterEmptyStreamNeverOverwritesExistingDraft(t *testing.T) {
	store := newBaseStore(`E:\Exports\Nib`)
	originalContent := store.chapter.Content
	originalStatus := store.chapter.Status
	comp := compiler.New(store)
	exp := &fakeExporter{}

	// 模拟 LLM 正常返回 nil 但未产出任何有效正文（或仅空白字符）。
	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, _ llm.ChatRequest, _ func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			return domain.TokenUsage{PromptTokens: 100, CompletionTokens: 0, TotalTokens: 100}, nil
		},
	}

	runner := New(store, comp, streamer, exp)
	var sawErrorChunk bool
	_, err := runner.GenerateChapter(context.Background(), 3, domain.GenerateChapterOptions{}, func(ch domain.GenChunk) error {
		if ch.Kind == domain.GenChunkError {
			sawErrorChunk = true
		}
		return nil
	})
	if !errors.Is(err, domain.ErrUpstreamUnavailable) {
		t.Fatalf("expected ErrUpstreamUnavailable on empty generated output, got %v", err)
	}
	if !sawErrorChunk {
		t.Fatalf("expected GenChunkError emitted on empty generated output")
	}
	if store.chapter.Content != originalContent {
		t.Fatalf("existing chapter content was overwritten! got=%q want=%q", store.chapter.Content, originalContent)
	}
	if store.chapter.Status != originalStatus {
		t.Fatalf("existing chapter status changed! got=%q want=%q", store.chapter.Status, originalStatus)
	}
	if len(store.versions) != 0 {
		t.Fatalf("expected no version archive written when generation failed empty, got %d", len(store.versions))
	}
	if exp.calls != 0 {
		t.Fatalf("expected exporter not called on empty generation, got %d", exp.calls)
	}
}
