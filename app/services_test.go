package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nib/internal/domain"
)

// fakeStore 是用于 app 层单元测试的纯内存仓储替身。
type fakeStore struct {
	books       map[int64]domain.Book
	volumes     map[int64]domain.Volume
	chapters    map[int64]domain.Chapter
	beats       map[int64][]domain.Beat
	versions    map[int64][]domain.ChapterVersion
	entities    map[int64]domain.Entity
	providers   map[int64]domain.Provider
	routes      map[domain.PipelineStage]domain.ModelRoute
	nextID      int64
	closedCount int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		books:     make(map[int64]domain.Book),
		volumes:   make(map[int64]domain.Volume),
		chapters:  make(map[int64]domain.Chapter),
		beats:     make(map[int64][]domain.Beat),
		versions:  make(map[int64][]domain.ChapterVersion),
		entities:  make(map[int64]domain.Entity),
		providers: make(map[int64]domain.Provider),
		routes:    make(map[domain.PipelineStage]domain.ModelRoute),
		nextID:    1,
	}
}

func (f *fakeStore) allocID() int64 {
	id := f.nextID
	f.nextID++
	return id
}

func (f *fakeStore) ListBooks(_ context.Context) ([]domain.Book, error) {
	out := make([]domain.Book, 0, len(f.books))
	for _, b := range f.books {
		out = append(out, b)
	}
	return out, nil
}

func (f *fakeStore) GetBook(_ context.Context, id int64) (domain.Book, error) {
	b, ok := f.books[id]
	if !ok {
		return domain.Book{}, domain.ErrNotFound
	}
	return b, nil
}

func (f *fakeStore) CreateBook(_ context.Context, in domain.CreateBookInput) (domain.Book, error) {
	b := domain.Book{
		ID:                 f.allocID(),
		Title:              in.Title,
		Genre:              in.Genre,
		Premise:            in.Premise,
		GoldenFinger:       in.GoldenFinger,
		TargetChapterWords: in.TargetChapterWords,
		ExportDir:          in.ExportDir,
	}
	f.books[b.ID] = b
	return b, nil
}

func (f *fakeStore) UpdateBook(_ context.Context, in domain.UpdateBookInput) (domain.Book, error) {
	b, ok := f.books[in.ID]
	if !ok {
		return domain.Book{}, domain.ErrNotFound
	}
	b.Title = in.Title
	b.Genre = in.Genre
	b.Premise = in.Premise
	b.GoldenFinger = in.GoldenFinger
	b.TargetChapterWords = in.TargetChapterWords
	b.ExportDir = in.ExportDir
	f.books[b.ID] = b
	return b, nil
}

func (f *fakeStore) DeleteBook(_ context.Context, id int64) error {
	delete(f.books, id)
	return nil
}

func (f *fakeStore) ListVolumes(_ context.Context, bookID int64) ([]domain.Volume, error) {
	var out []domain.Volume
	for _, v := range f.volumes {
		if v.BookID == bookID {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeStore) UpsertVolume(_ context.Context, in domain.UpsertVolumeInput) (domain.Volume, error) {
	id := in.ID
	if id == 0 {
		id = f.allocID()
	}
	v := domain.Volume{
		ID:         id,
		BookID:     in.BookID,
		Seq:        in.Seq,
		Title:      in.Title,
		Goal:       in.Goal,
		ClimaxPlan: in.ClimaxPlan,
	}
	f.volumes[id] = v
	return v, nil
}

func (f *fakeStore) DeleteVolume(_ context.Context, id int64) error {
	delete(f.volumes, id)
	return nil
}

func (f *fakeStore) ListChapters(_ context.Context, volumeID int64) ([]domain.Chapter, error) {
	var out []domain.Chapter
	for _, c := range f.chapters {
		if c.VolumeID == volumeID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) ListBookChapters(_ context.Context, bookID int64) ([]domain.Chapter, error) {
	var out []domain.Chapter
	for _, c := range f.chapters {
		if c.BookID == bookID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) GetChapter(_ context.Context, id int64) (domain.Chapter, error) {
	c, ok := f.chapters[id]
	if !ok {
		return domain.Chapter{}, domain.ErrNotFound
	}
	return c, nil
}

func (f *fakeStore) CreateChapter(_ context.Context, in domain.CreateChapterInput) (domain.Chapter, error) {
	c := domain.Chapter{
		ID:             f.allocID(),
		BookID:         in.BookID,
		VolumeID:       in.VolumeID,
		GlobalSeq:      len(f.chapters) + 1,
		VolSeq:         len(f.chapters) + 1,
		Title:          in.Title,
		Status:         domain.ChapterStatusPlanned,
		EmotionTag:     in.EmotionTag,
		HookType:       in.HookType,
		HookNote:       in.HookNote,
		OutlineSummary: in.OutlineSummary,
	}
	f.chapters[c.ID] = c
	return c, nil
}

func (f *fakeStore) UpdateChapterMeta(_ context.Context, in domain.UpdateChapterMetaInput) (domain.Chapter, error) {
	c, ok := f.chapters[in.ID]
	if !ok {
		return domain.Chapter{}, domain.ErrNotFound
	}
	c.Title = in.Title
	if in.Status != "" {
		c.Status = in.Status
	}
	if in.EmotionTag != "" {
		c.EmotionTag = in.EmotionTag
	}
	if in.HookType != "" {
		c.HookType = in.HookType
	}
	c.HookNote = in.HookNote
	c.OutlineSummary = in.OutlineSummary
	c.CausalSummary = in.CausalSummary
	f.chapters[c.ID] = c
	return c, nil
}

func (f *fakeStore) SaveChapterDraft(_ context.Context, in domain.SaveChapterDraftInput) (domain.Chapter, error) {
	c, ok := f.chapters[in.ChapterID]
	if !ok {
		return domain.Chapter{}, domain.ErrNotFound
	}
	if in.Title != "" {
		c.Title = in.Title
	}
	c.Content = in.Content
	c.WordCount = domain.CountWords(in.Content)
	if in.Status != "" {
		c.Status = in.Status
	}
	f.chapters[c.ID] = c
	return c, nil
}

func (f *fakeStore) FinalizeChapter(_ context.Context, chapterID int64) (domain.Chapter, error) {
	c, ok := f.chapters[chapterID]
	if !ok {
		return domain.Chapter{}, domain.ErrNotFound
	}
	c.Status = domain.ChapterStatusFinalized
	f.chapters[c.ID] = c
	return c, nil
}

func (f *fakeStore) ListBeats(_ context.Context, chapterID int64) ([]domain.Beat, error) {
	return f.beats[chapterID], nil
}

func (f *fakeStore) ReplaceBeats(_ context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error) {
	f.beats[chapterID] = beats
	return beats, nil
}

func (f *fakeStore) ListChapterVersions(_ context.Context, chapterID int64) ([]domain.ChapterVersion, error) {
	return f.versions[chapterID], nil
}

func (f *fakeStore) RestoreVersion(_ context.Context, versionID int64) (domain.Chapter, error) {
	for chapID, list := range f.versions {
		for _, v := range list {
			if v.ID == versionID {
				c := f.chapters[chapID]
				c.Content = v.Content
				c.WordCount = v.WordCount
				f.chapters[chapID] = c
				return c, nil
			}
		}
	}
	return domain.Chapter{}, domain.ErrNotFound
}

func (f *fakeStore) ListEntities(_ context.Context, bookID int64, _ domain.EntityFilter) ([]domain.Entity, error) {
	var out []domain.Entity
	for _, e := range f.entities {
		if e.BookID == bookID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeStore) UpsertEntity(_ context.Context, in domain.UpsertEntityInput) (domain.Entity, error) {
	id := in.ID
	if id == 0 {
		id = f.allocID()
	}
	e := domain.Entity{
		ID:          id,
		BookID:      in.BookID,
		Kind:        in.Kind,
		Name:        in.Name,
		Aliases:     in.Aliases,
		IsInvariant: in.IsInvariant,
		Summary:     in.Summary,
		Attributes:  in.Attributes,
	}
	f.entities[id] = e
	return e, nil
}

func (f *fakeStore) DeleteEntity(_ context.Context, id int64) error {
	delete(f.entities, id)
	return nil
}

func (f *fakeStore) ListProviders(_ context.Context) ([]domain.Provider, error) {
	out := make([]domain.Provider, 0, len(f.providers))
	for _, p := range f.providers {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeStore) GetProvider(_ context.Context, id int64) (domain.Provider, error) {
	p, ok := f.providers[id]
	if !ok {
		return domain.Provider{}, domain.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) UpsertProvider(_ context.Context, in domain.UpsertProviderInput) (domain.Provider, error) {
	id := in.ID
	if id == 0 {
		id = f.allocID()
	}
	key := in.APIKey
	if existing, ok := f.providers[id]; ok && key == "" {
		key = existing.APIKey
	}
	p := domain.Provider{
		ID:       id,
		Name:     in.Name,
		Protocol: in.Protocol,
		BaseURL:  in.BaseURL,
		APIKey:   key,
		Models:   in.Models,
		Enabled:  in.Enabled,
	}
	f.providers[id] = p
	return p, nil
}

func (f *fakeStore) DeleteProvider(_ context.Context, id int64) error {
	delete(f.providers, id)
	return nil
}

func (f *fakeStore) ListModelRoutes(_ context.Context) ([]domain.ModelRoute, error) {
	out := make([]domain.ModelRoute, 0, len(f.routes))
	for _, r := range f.routes {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeStore) SaveModelRoute(_ context.Context, route domain.ModelRoute) (domain.ModelRoute, error) {
	f.routes[route.Stage] = route
	return route, nil
}

func (f *fakeStore) Close() error {
	f.closedCount++
	return nil
}

type fakeExporter struct {
	exportedBooks    []int64
	exportedChapters []int64
}

func (e *fakeExporter) ExportBook(_ context.Context, bookID int64) (domain.ExportResult, error) {
	e.exportedBooks = append(e.exportedBooks, bookID)
	return domain.ExportResult{ExportDir: "D:/Export", BookDir: "D:/Export/Test", ChapterCount: 1}, nil
}

func (e *fakeExporter) ExportChapter(_ context.Context, chapterID int64) (string, error) {
	e.exportedChapters = append(e.exportedChapters, chapterID)
	return "D:/Export/Test/ch1.md", nil
}

type fakeLLM struct {
	lastProvider domain.Provider
	hadDeadline  bool
	models       []string
	listErr      error
}

func (l *fakeLLM) TestConnection(ctx context.Context, p domain.Provider) (domain.ProviderTestResult, error) {
	l.lastProvider = p
	_, l.hadDeadline = ctx.Deadline()
	return domain.ProviderTestResult{OK: true, LatencyMs: 42, ModelUsed: "deepseek-chat", Message: "ok"}, nil
}

func (l *fakeLLM) ListModels(ctx context.Context, p domain.Provider) ([]string, error) {
	l.lastProvider = p
	_, l.hadDeadline = ctx.Deadline()
	if l.listErr != nil {
		return nil, l.listErr
	}
	if len(l.models) > 0 {
		out := make([]string, len(l.models))
		copy(out, l.models)
		return out, nil
	}
	return []string{"deepseek-chat", "deepseek-reasoner"}, nil
}

type fakeCompiler struct{}

func (fakeCompiler) Compile(_ context.Context, chapterID int64, _ domain.BriefingOptions) (domain.Briefing, error) {
	return domain.Briefing{
		ChapterID:        chapterID,
		TotalTokenBudget: domain.DefaultBriefingTokenBudget,
		TotalTokenEst:    1280,
		Slots:            []domain.BriefingSlot{{Key: "p0_invariants", Priority: "P0", Required: true}},
	}, nil
}

type fakePipeline struct {
	chunks []domain.GenChunk
}

func (p *fakePipeline) GenerateChapter(_ context.Context, chapterID int64, _ domain.GenerateChapterOptions, emit func(domain.GenChunk) error) (domain.Chapter, error) {
	for _, c := range p.chunks {
		if err := emit(c); err != nil {
			return domain.Chapter{}, err
		}
	}
	return domain.Chapter{ID: chapterID, Content: "生成正文", WordCount: 4, Status: domain.ChapterStatusReviewing}, nil
}

func TestNilRuntimeGuard(t *testing.T) {
	ctx := context.Background()
	emptyRT := NewRuntime()

	lib := NewLibraryService(emptyRT)
	atlas := NewAtlasService(emptyRT)
	settings := NewSettingsService(emptyRT)
	studio := NewStudioService(emptyRT)

	// 静态题材包列表无需数据库也应可用
	if presets := lib.ListGenrePresets(); len(presets) != 4 {
		t.Fatalf("ListGenrePresets len = %d, want 4", len(presets))
	}

	if _, err := lib.ListBooks(ctx); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("LibraryService.ListBooks err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := atlas.ListEntities(ctx, 1, domain.EntityFilter{}); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("AtlasService.ListEntities err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := settings.ListProviders(ctx); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("SettingsService.ListProviders err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := studio.PreviewBriefing(ctx, 1, domain.BriefingOptions{}); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("StudioService.PreviewBriefing err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := studio.GenerateChapter(ctx, 1, domain.GenerateChapterOptions{}, nil); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("StudioService.GenerateChapter err = %v, want ErrRuntimeNotReady", err)
	}

	// nil Runtime 指针同样不应 panic
	var nilSvc LibraryService
	if _, err := nilSvc.ListBooks(ctx); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("nil-runtime ListBooks err = %v, want ErrRuntimeNotReady", err)
	}
}

func TestSettingsServiceRedactsKeysAndEnforcesTimeout(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	llm := &fakeLLM{}
	rt := NewRuntime()
	rt.Init(RuntimeDeps{
		Store: st,
		LLM:   llm,
		DirPicker: func(_ context.Context) (string, error) {
			return `E:\Novels\Export`, nil
		},
	})
	svc := NewSettingsService(rt)

	secret := "sk-live-secret-body-9988a8f2"
	saved, err := svc.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "DeepSeek",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   secret,
		Models:   []string{"deepseek-chat"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}
	if !saved.HasAPIKey || saved.APIKeyMasked != "sk-...a8f2" {
		t.Fatalf("unexpected masked key: has=%v masked=%q", saved.HasAPIKey, saved.APIKeyMasked)
	}
	if strings.Contains(saved.APIKeyMasked, "secret-body") {
		t.Fatalf("APIKeyMasked leaked plaintext secret")
	}

	list, err := svc.ListProviders(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListProviders err=%v len=%d", err, len(list))
	}
	if list[0].APIKeyMasked != "sk-...a8f2" || !list[0].HasAPIKey {
		t.Fatalf("ListProviders did not preserve mask: %+v", list[0])
	}

	// TestProvider 内部应读取完整密钥供 LLMClient 使用，并自动设置超时 Deadline
	res, err := svc.TestProvider(ctx, saved.ID)
	if err != nil || !res.OK {
		t.Fatalf("TestProvider err=%v res=%+v", err, res)
	}
	if llm.lastProvider.APIKey != secret {
		t.Fatalf("LLMClient did not receive stored key")
	}
	if !llm.hadDeadline {
		t.Fatalf("expected TestProvider to enforce a timeout deadline")
	}

	// PickExportDir 使用注入选择器
	dir, err := svc.PickExportDir(ctx)
	if err != nil || dir != `E:\Novels\Export` {
		t.Fatalf("PickExportDir = (%q, %v)", dir, err)
	}
}

func TestValidationErrorsAcrossServices(t *testing.T) {
	ctx := context.Background()
	rt := NewRuntime()
	rt.Init(RuntimeDeps{
		Store:    newFakeStore(),
		Exporter: &fakeExporter{},
		LLM:      &fakeLLM{},
		Compiler: fakeCompiler{},
		Pipeline: &fakePipeline{},
	})

	lib := NewLibraryService(rt)
	atlas := NewAtlasService(rt)
	settings := NewSettingsService(rt)
	studio := NewStudioService(rt)

	if _, err := lib.CreateBook(ctx, domain.CreateBookInput{Title: "", Genre: domain.GenreXianxia}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty book title, got %v", err)
	}
	if _, err := lib.GetChapter(ctx, 0); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for chapterID=0, got %v", err)
	}
	if _, err := lib.ReplaceBeats(ctx, 1, []domain.Beat{{Stage: "invalid", Summary: "x"}}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid beat stage, got %v", err)
	}
	if _, err := atlas.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: 1, Kind: domain.EntityKindCharacter, Name: ""}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty entity name, got %v", err)
	}
	if _, err := settings.UpsertProvider(ctx, domain.UpsertProviderInput{Name: "Bad", Protocol: domain.ProtocolAnthropic, BaseURL: "not-a-url"}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bad provider url, got %v", err)
	}
	if _, err := settings.SaveModelRoute(ctx, domain.ModelRoute{Stage: domain.StageDraft, Temperature: 9.9}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bad temperature, got %v", err)
	}
	if _, err := studio.PreviewBriefing(ctx, 1, domain.BriefingOptions{TokenBudget: -1}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for negative token budget, got %v", err)
	}
	if _, err := studio.RestoreVersion(ctx, 0); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for versionId=0, got %v", err)
	}
}

func TestSaveDraftAndFinalizeTriggerExporterOnlyWhenExportDirSet(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	exp := &fakeExporter{}
	rt := NewRuntime()
	rt.Init(RuntimeDeps{
		Store:    st,
		Exporter: exp,
		Compiler: fakeCompiler{},
		Pipeline: &fakePipeline{
			chunks: []domain.GenChunk{{Kind: domain.GenChunkDelta, Delta: "正文"}},
		},
	})
	lib := NewLibraryService(rt)
	studio := NewStudioService(rt)

	// 1) 未配置 ExportDir 的书籍：保存草稿与定稿不应触发导出，手动整书导出应返回 ErrExportDirNotSet
	bookNoDir, err := lib.CreateBook(ctx, domain.CreateBookInput{Title: "无导出目录", Genre: domain.GenreUrban})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}
	vol1, _ := lib.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: bookNoDir.ID, Seq: 1, Title: "第一卷"})
	ch1, _ := lib.CreateChapter(ctx, domain.CreateChapterInput{BookID: bookNoDir.ID, VolumeID: vol1.ID, Title: "第一章"})

	if _, err := lib.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{ChapterID: ch1.ID, Content: "初稿正文"}); err != nil {
		t.Fatalf("SaveChapterDraft failed: %v", err)
	}
	if _, err := lib.FinalizeChapter(ctx, ch1.ID); err != nil {
		t.Fatalf("FinalizeChapter failed: %v", err)
	}
	if len(exp.exportedChapters) != 0 {
		t.Fatalf("expected 0 exports when exportDir is empty, got %d", len(exp.exportedChapters))
	}
	if _, err := lib.ExportBookMarkdown(ctx, bookNoDir.ID); !errors.Is(err, domain.ErrExportDirNotSet) {
		t.Fatalf("ExportBookMarkdown err = %v, want ErrExportDirNotSet", err)
	}

	// 2) 配置了 ExportDir 的书籍：SaveChapterDraft 与 FinalizeChapter 均应触发单章导出
	bookWithDir, err := lib.CreateBook(ctx, domain.CreateBookInput{
		Title:     "太初剑渊",
		Genre:     domain.GenreXianxia,
		ExportDir: `E:\Exports`,
	})
	if err != nil {
		t.Fatalf("CreateBook with dir failed: %v", err)
	}
	vol2, _ := lib.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: bookWithDir.ID, Seq: 1, Title: "第一卷"})
	ch2, _ := lib.CreateChapter(ctx, domain.CreateChapterInput{BookID: bookWithDir.ID, VolumeID: vol2.ID, Title: "坊市暗流"})

	if _, err := lib.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{ChapterID: ch2.ID, Content: "残阵灵光骤灭。"}); err != nil {
		t.Fatalf("SaveChapterDraft failed: %v", err)
	}
	if _, err := lib.FinalizeChapter(ctx, ch2.ID); err != nil {
		t.Fatalf("FinalizeChapter failed: %v", err)
	}
	if len(exp.exportedChapters) != 2 || exp.exportedChapters[0] != ch2.ID || exp.exportedChapters[1] != ch2.ID {
		t.Fatalf("expected 2 chapter exports for chapter %d, got %v", ch2.ID, exp.exportedChapters)
	}

	// 手动整书导出应触发 ExportBook
	res, err := lib.ExportBookMarkdown(ctx, bookWithDir.ID)
	if err != nil || res.ChapterCount != 1 {
		t.Fatalf("ExportBookMarkdown = (%+v, %v)", res, err)
	}
	if len(exp.exportedBooks) != 1 || exp.exportedBooks[0] != bookWithDir.ID {
		t.Fatalf("expected ExportBook called for %d, got %v", bookWithDir.ID, exp.exportedBooks)
	}

	// StudioService 简报、生成与关闭校验
	briefing, err := studio.PreviewBriefing(ctx, ch2.ID, domain.BriefingOptions{})
	if err != nil || briefing.ChapterID != ch2.ID {
		t.Fatalf("PreviewBriefing = (%+v, %v)", briefing, err)
	}
	genChap, err := studio.GenerateChapter(ctx, ch2.ID, domain.GenerateChapterOptions{}, nil)
	if err != nil || genChap.Status != domain.ChapterStatusReviewing {
		t.Fatalf("GenerateChapter = (%+v, %v)", genChap, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close failed: %v", err)
	}
	if st.closedCount != 1 {
		t.Fatalf("Store.Close called %d times, want 1", st.closedCount)
	}
	if rt.Ready() {
		t.Fatalf("Runtime should not be ready after Close")
	}
}

func TestSettingsServiceFetchProviderModels(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	llmMock := &fakeLLM{models: []string{"deepseek-chat", "deepseek-reasoner"}}

	rt := NewRuntime()
	rt.Init(RuntimeDeps{
		Store: st,
		LLM:   llmMock,
	})
	settings := NewSettingsService(rt)

	saved, err := settings.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "DeepSeek",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-keep-me-1234567890",
		Models:   []string{},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}

	res, err := settings.FetchProviderModels(ctx, saved.ID)
	if err != nil {
		t.Fatalf("FetchProviderModels failed: %v", err)
	}
	if res.Count != 2 || len(res.Models) != 2 {
		t.Fatalf("unexpected FetchModelsResult: %+v", res)
	}
	if !llmMock.hadDeadline {
		t.Fatalf("expected FetchProviderModels to attach timeout deadline")
	}

	// 验证模型列表已持久化且原 API Key 未被清空
	stored, err := st.GetProvider(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetProvider failed: %v", err)
	}
	if stored.APIKey != "sk-keep-me-1234567890" {
		t.Fatalf("API key was overwritten: %q", stored.APIKey)
	}
	if len(stored.Models) != 2 || stored.Models[0] != "deepseek-chat" {
		t.Fatalf("stored models not updated: %v", stored.Models)
	}

	// 无窗口上下文下 SetWindowTheme 应安全返回 nil
	if err := settings.SetWindowTheme(ctx, "light"); err != nil {
		t.Fatalf("SetWindowTheme failed: %v", err)
	}
}
