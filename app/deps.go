package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/egoist/mygo"

	"nib/internal/domain"
)

// ErrRuntimeNotReady 表示桌面运行时依赖（SQLite / 引擎组件）尚未在 WhenReady 中完成初始化。
var ErrRuntimeNotReady = errors.New("app runtime not initialized")

// Store 定义 app 层依赖的 Phase 1 纯 Go 持久化接口（由 internal/store 实现）。
type Store interface {
	// Books
	ListBooks(ctx context.Context) ([]domain.Book, error)
	GetBook(ctx context.Context, id int64) (domain.Book, error)
	CreateBook(ctx context.Context, in domain.CreateBookInput) (domain.Book, error)
	UpdateBook(ctx context.Context, in domain.UpdateBookInput) (domain.Book, error)
	DeleteBook(ctx context.Context, id int64) error

	// Volumes
	ListVolumes(ctx context.Context, bookID int64) ([]domain.Volume, error)
	UpsertVolume(ctx context.Context, in domain.UpsertVolumeInput) (domain.Volume, error)
	DeleteVolume(ctx context.Context, id int64) error

	// Chapters & Beats
	ListChapters(ctx context.Context, volumeID int64) ([]domain.Chapter, error)
	ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error)
	GetChapter(ctx context.Context, id int64) (domain.Chapter, error)
	CreateChapter(ctx context.Context, in domain.CreateChapterInput) (domain.Chapter, error)
	InsertChapter(ctx context.Context, in domain.InsertChapterInput) (domain.Chapter, error)
	MoveChapter(ctx context.Context, in domain.MoveChapterInput) (domain.Chapter, error)
	DeleteChapter(ctx context.Context, chapterID int64) error
	ReplaceBookText(ctx context.Context, in domain.ReplaceBookTextInput) (domain.ReplaceBookTextResult, error)
	UpdateChapterMeta(ctx context.Context, in domain.UpdateChapterMetaInput) (domain.Chapter, error)
	SaveChapterDraft(ctx context.Context, in domain.SaveChapterDraftInput) (domain.Chapter, error)
	FinalizeChapter(ctx context.Context, chapterID int64) (domain.Chapter, error)
	ListBeats(ctx context.Context, chapterID int64) ([]domain.Beat, error)
	ReplaceBeats(ctx context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error)

	// Chapter Versions
	ListChapterVersions(ctx context.Context, chapterID int64) ([]domain.ChapterVersion, error)
	RestoreVersion(ctx context.Context, versionID int64) (domain.Chapter, error)

	// Entities
	ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error)
	UpsertEntity(ctx context.Context, in domain.UpsertEntityInput) (domain.Entity, error)
	DeleteEntity(ctx context.Context, id int64) error
	ListEntityRelations(ctx context.Context, bookID int64) ([]domain.EntityRelation, error)
	UpsertEntityRelation(ctx context.Context, in domain.UpsertEntityRelationInput) (domain.EntityRelation, error)
	DeleteEntityRelation(ctx context.Context, id int64) error

	// Director History
	ListDirectorMessages(ctx context.Context, bookID int64) ([]domain.DirectorMessage, error)
	ClearDirectorMessages(ctx context.Context, bookID int64) error

	// Database Backup
	BackupToFile(ctx context.Context, targetPath string) error

	// Providers & Model Routes
	ListProviders(ctx context.Context) ([]domain.Provider, error)
	GetProvider(ctx context.Context, id int64) (domain.Provider, error)
	UpsertProvider(ctx context.Context, in domain.UpsertProviderInput) (domain.Provider, error)
	DeleteProvider(ctx context.Context, id int64) error
	ListModelRoutes(ctx context.Context) ([]domain.ModelRoute, error)
	SaveModelRoute(ctx context.Context, route domain.ModelRoute) (domain.ModelRoute, error)
	ListModelPricing(ctx context.Context) ([]domain.ModelPricing, error)
	UpsertModelPricing(ctx context.Context, in domain.ModelPricing) (domain.ModelPricing, error)
	DeleteModelPricing(ctx context.Context, providerName, modelName string) error

	Close() error
}

// Exporter 定义单向 Markdown / TXT / EPUB 导出接口（由 internal/exporter 实现）。
type Exporter interface {
	ExportBook(ctx context.Context, bookID int64) (domain.ExportResult, error)
	ExportChapter(ctx context.Context, chapterID int64) (string, error)
	ExportBookTXT(ctx context.Context, bookID int64, targetPath string) (string, error)
	ExportBookEPUB(ctx context.Context, bookID int64, targetPath string) (string, error)
}

// LLMClient 定义多协议供应商连通性探测与模型列表拉取接口（由 internal/llm 实现）。
type LLMClient interface {
	TestConnection(ctx context.Context, provider domain.Provider) (domain.ProviderTestResult, error)
	ListModels(ctx context.Context, provider domain.Provider) ([]string, error)
}

// BriefingCompiler 定义上下文作战简报编译接口（由 internal/compiler 实现）。
type BriefingCompiler interface {
	Compile(ctx context.Context, chapterID int64, opts domain.BriefingOptions) (domain.Briefing, error)
}

// PipelineRunner 定义章节流式出稿管线接口（由 internal/pipeline 实现，零 MyGo 依赖）。
type PipelineRunner interface {
	GenerateChapter(
		ctx context.Context,
		chapterID int64,
		opts domain.GenerateChapterOptions,
		emit func(domain.GenChunk) error,
	) (domain.Chapter, error)
	RewriteSelection(
		ctx context.Context,
		req domain.RewriteSelectionRequest,
		emit func(domain.GenChunk) error,
	) (domain.RewriteSelectionResult, error)
}

// MemoryStore 定义 Phase 2 记忆引擎所需的持久化与检索接口（由 internal/store 实现）。
type MemoryStore interface {
	SnapshotAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntitySnapshot, error)
	ListStateEvents(ctx context.Context, bookID int64, entityID int64, upToSeq int, includePending bool) ([]domain.EntityStateEvent, error)
	ListForeshadowings(ctx context.Context, bookID int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error)
	UpsertForeshadowing(ctx context.Context, in domain.UpsertForeshadowingInput) (domain.Foreshadowing, error)
	DeleteForeshadowing(ctx context.Context, id int64) error
	ListArcSummaries(ctx context.Context, bookID int64, level domain.ArcSummaryLevel) ([]domain.ArcSummary, error)
	UpsertArcSummary(ctx context.Context, in domain.UpsertArcSummaryInput) (domain.ArcSummary, error)
	GetPendingDelta(ctx context.Context, chapterID int64) (*domain.ChapterDelta, error)
	ApplyDeltaDecisions(ctx context.Context, in domain.ApplyDeltaInput) (domain.ApplyDeltaResult, error)
	SearchMemory(ctx context.Context, bookID int64, query string, maxChapterSeq int) ([]domain.MemorySearchHit, error)
}

// DeltaExtractor 定义完章记忆增量抽取接口（由 internal/memory 实现，零 MyGo 依赖）。
type DeltaExtractor interface {
	ExtractChapterDelta(ctx context.Context, chapterID int64) (domain.ChapterDelta, error)
}

// StyleStore 定义 Phase 3 文风指纹、样章锚点、去 AI 味规则与质检报告的持久化接口（由 internal/store 实现）。
type StyleStore interface {
	GetStyleProfile(ctx context.Context, bookID int64) (domain.StyleProfile, error)
	SaveStyleProfile(ctx context.Context, in domain.UpsertStyleProfileInput) (domain.StyleProfile, error)
	ListStyleSamples(ctx context.Context, bookID int64, sceneType domain.SceneType) ([]domain.StyleSample, error)
	UpsertStyleSample(ctx context.Context, in domain.UpsertStyleSampleInput) (domain.StyleSample, error)
	DeleteStyleSample(ctx context.Context, id int64) error
	ListDeaiRules(ctx context.Context, bookID int64) ([]domain.DeaiRule, error)
	UpsertDeaiRule(ctx context.Context, in domain.UpsertDeaiRuleInput) (domain.DeaiRule, error)
	DeleteDeaiRule(ctx context.Context, id int64) error
	GetChapterAudit(ctx context.Context, chapterID int64) (domain.ChapterAudit, error)
}

// StyleEngine 定义 Phase 3 样章文风抽取、0 成本本地 AI 味检测、Critic 盲审与手术式局部润色接口（零 MyGo 依赖）。
type StyleEngine interface {
	ExtractStyleProfile(ctx context.Context, bookID int64, sampleTexts []string) (domain.StyleProfile, error)
	LintChapterLocal(ctx context.Context, chapterID int64) (domain.ChapterAudit, error)
	RunCriticAudit(ctx context.Context, chapterID int64) (domain.ChapterAudit, error)
	GenerateSurgicalDiffs(ctx context.Context, chapterID int64, targetParagraphs []int) (domain.ChapterAudit, error)
	ApplySurgicalDiff(ctx context.Context, in domain.ApplySurgicalDiffInput) (domain.ApplySurgicalDiffResult, error)
}

// BatchStore 定义 Phase 4 微批运行记录与 Token 统计的只读查询接口（由 internal/store 实现）。
type BatchStore interface {
	GetBatchRun(ctx context.Context, batchID int64) (domain.BatchRun, error)
	GetLatestBatchRun(ctx context.Context, bookID int64) (*domain.BatchRun, error)
	GetUsageSummary(ctx context.Context, bookID int64) (domain.UsageSummary, error)
}

// BatchEngine 定义 Phase 4 批量章纲流式生成与 1~5 章串行微批队列调度接口（由 internal/pipeline 实现，零 MyGo 依赖）。
type BatchEngine interface {
	BatchGenerateOutlines(
		ctx context.Context,
		req domain.BatchOutlineRequest,
		emit func(domain.OutlineChunk) error,
	) ([]domain.Chapter, error)
	StartBatchRun(ctx context.Context, bookID int64, chapterIDs []int64, mode domain.BatchMode) (domain.BatchRun, error)
	CancelBatchRun(ctx context.Context, batchID int64) (domain.BatchRun, error)
	RetryBatchItem(ctx context.Context, itemID int64) (domain.BatchRun, error)
}

// GenesisEngine 定义「AI 一句话开书」企划生成与一键落库接口（由 internal/pipeline 实现，零 MyGo 依赖）。
type GenesisEngine interface {
	GenerateBookGenesis(
		ctx context.Context,
		req domain.BookGenesisRequest,
		emit func(domain.GenesisChunk) error,
	) (domain.BookGenesisPlan, error)
	ApplyBookGenesis(ctx context.Context, in domain.ApplyGenesisInput) (domain.ApplyGenesisResult, error)
}

// AutopilotEngine 定义全自动托管写作流水线接口（由 internal/pipeline 实现，零 MyGo 依赖）。
type AutopilotEngine interface {
	StartAutopilot(ctx context.Context, req domain.AutopilotRequest) (domain.AutopilotState, error)
	ResumeAutopilot(ctx context.Context, bookID int64) (domain.AutopilotState, error)
	GetAutopilotState(bookID int64) domain.AutopilotState
	StopAutopilot(bookID int64) (domain.AutopilotState, error)
	ResolveAutopilotGate(bookID int64, action domain.AutopilotDecisionAction) (domain.AutopilotState, error)
}

// ArcSummaryGenerator 定义 AI 自动生成 10 章弧线摘要与卷级摘要的接口（由 *pipeline.Runner 实现）。
type ArcSummaryGenerator interface {
	GenerateArcSummary(ctx context.Context, req domain.GenerateArcSummaryRequest) (domain.ArcSummary, error)
}

// DirectorEngine 定义「全局 AI 导演」多步工具操控全书接口（由 internal/pipeline 实现，零 MyGo 依赖）。
type DirectorEngine interface {
	RunDirectorChat(
		ctx context.Context,
		req domain.DirectorChatRequest,
		emit func(domain.DirectorEvent) error,
	) (domain.DirectorReply, error)
}

// DirPickerFunc 封装原生目录选择弹窗，默认使用 mygo.Dialog.Open，可在单测中替换。
type DirPickerFunc func(ctx context.Context) (string, error)

// RuntimeDeps 汇总 WhenReady 阶段注入到 Runtime 的全部核心依赖。
type RuntimeDeps struct {
	Store           Store
	Exporter        Exporter
	LLM             LLMClient
	Compiler        BriefingCompiler
	Pipeline        PipelineRunner
	MemoryStore     MemoryStore
	DeltaExtractor  DeltaExtractor
	StyleStore      StyleStore
	StyleEngine     StyleEngine
	BatchStore      BatchStore
	BatchEngine     BatchEngine
	GenesisEngine   GenesisEngine
	AutopilotEngine AutopilotEngine
	DirectorEngine  DirectorEngine
	DirPicker       DirPickerFunc
}

// Runtime 是线程安全的延迟初始化容器。
// main() 顶层仅创建空 Runtime 并绑定 Service，确保 `mygo generate` 绝不触碰 SQLite；
// 真正打开数据库与注入依赖全部发生在 mygo.App.WhenReady 回调中。
type Runtime struct {
	mu         sync.RWMutex
	deps       RuntimeDeps
	baseCtx    context.Context
	baseCancel context.CancelFunc
	activeWG   sync.WaitGroup
	ready      bool
	closed     bool
}

// NewRuntime 创建一个尚未注入依赖的空 Runtime 容器。
func NewRuntime() *Runtime {
	return &Runtime{}
}

// Init 在 WhenReady 中注入已就绪的底层引擎与仓储实现。
func (r *Runtime) Init(deps RuntimeDeps) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.baseCancel != nil {
		r.baseCancel()
	}
	r.deps = deps
	r.baseCtx, r.baseCancel = context.WithCancel(context.Background())
	r.ready = true
	r.closed = false
}

// Ready 返回 Runtime 是否已初始化且尚未关闭。
func (r *Runtime) Ready() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ready && !r.closed
}

// Close 同步取消运行中的流式任务、等待正在落库的事务结束，再关闭 SQLite 连接（幂等）。
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.ready = false
	cancel := r.baseCancel
	st := r.deps.Store
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// 等待所有在途的流水线（含中断保底落库事务）安全退出后再关库。
	r.activeWG.Wait()

	if st != nil {
		return st.Close()
	}
	return nil
}

func (r *Runtime) store() (Store, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed || r.deps.Store == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.deps.Store, nil
}

func (r *Runtime) exporter() Exporter {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed {
		return nil
	}
	return r.deps.Exporter
}

func (r *Runtime) requireExporter() (Exporter, error) {
	exp := r.exporter()
	if exp == nil {
		return nil, ErrRuntimeNotReady
	}
	return exp, nil
}

func (r *Runtime) llm() (LLMClient, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed || r.deps.LLM == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.deps.LLM, nil
}

func (r *Runtime) compiler() (BriefingCompiler, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed || r.deps.Compiler == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.deps.Compiler, nil
}

func (r *Runtime) trackPipeline(callCtx context.Context) (PipelineRunner, context.Context, context.CancelFunc, func(), error) {
	if r == nil {
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	if !r.ready || r.closed || r.deps.Pipeline == nil {
		r.mu.RUnlock()
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	runner := r.deps.Pipeline
	baseCtx := r.baseCtx
	// 必须在持有 RLock 期间递增计数，防止与 Close() 中的 activeWG.Wait() 产生竞态。
	r.activeWG.Add(1)
	r.mu.RUnlock()

	genCtx, cancel := context.WithCancel(callCtx)
	var stopWatch func() bool
	if baseCtx != nil {
		stopWatch = context.AfterFunc(baseCtx, cancel)
	}
	release := func() {
		if stopWatch != nil {
			stopWatch()
		}
		cancel()
		r.activeWG.Done()
	}
	return runner, genCtx, cancel, release, nil
}

func (r *Runtime) memoryStore() (MemoryStore, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed || r.deps.MemoryStore == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.deps.MemoryStore, nil
}

func (r *Runtime) trackExtractor(callCtx context.Context) (DeltaExtractor, context.Context, func(), error) {
	if r == nil {
		return nil, nil, nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	if !r.ready || r.closed || r.deps.DeltaExtractor == nil {
		r.mu.RUnlock()
		return nil, nil, nil, ErrRuntimeNotReady
	}
	ext := r.deps.DeltaExtractor
	baseCtx := r.baseCtx
	r.activeWG.Add(1)
	r.mu.RUnlock()

	extCtx, cancel := context.WithCancel(callCtx)
	var stopWatch func() bool
	if baseCtx != nil {
		stopWatch = context.AfterFunc(baseCtx, cancel)
	}
	release := func() {
		if stopWatch != nil {
			stopWatch()
		}
		cancel()
		r.activeWG.Done()
	}
	return ext, extCtx, release, nil
}

func (r *Runtime) styleStore() (StyleStore, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed || r.deps.StyleStore == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.deps.StyleStore, nil
}

func (r *Runtime) styleEngine() (StyleEngine, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed || r.deps.StyleEngine == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.deps.StyleEngine, nil
}

func (r *Runtime) trackStyleEngine(callCtx context.Context) (StyleEngine, context.Context, func(), error) {
	if r == nil {
		return nil, nil, nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	if !r.ready || r.closed || r.deps.StyleEngine == nil {
		r.mu.RUnlock()
		return nil, nil, nil, ErrRuntimeNotReady
	}
	eng := r.deps.StyleEngine
	baseCtx := r.baseCtx
	r.activeWG.Add(1)
	r.mu.RUnlock()

	engCtx, cancel := context.WithCancel(callCtx)
	var stopWatch func() bool
	if baseCtx != nil {
		stopWatch = context.AfterFunc(baseCtx, cancel)
	}
	release := func() {
		if stopWatch != nil {
			stopWatch()
		}
		cancel()
		r.activeWG.Done()
	}
	return eng, engCtx, release, nil
}

func (r *Runtime) batchStore() (BatchStore, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed || r.deps.BatchStore == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.deps.BatchStore, nil
}

func (r *Runtime) batchEngine() (BatchEngine, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed || r.deps.BatchEngine == nil {
		return nil, ErrRuntimeNotReady
	}
	return r.deps.BatchEngine, nil
}

func (r *Runtime) trackBatchEngine(callCtx context.Context) (BatchEngine, context.Context, context.CancelFunc, func(), error) {
	if r == nil {
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	if !r.ready || r.closed || r.deps.BatchEngine == nil {
		r.mu.RUnlock()
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	eng := r.deps.BatchEngine
	baseCtx := r.baseCtx
	r.activeWG.Add(1)
	r.mu.RUnlock()

	engCtx, cancel := context.WithCancel(callCtx)
	var stopWatch func() bool
	if baseCtx != nil {
		stopWatch = context.AfterFunc(baseCtx, cancel)
	}
	release := func() {
		if stopWatch != nil {
			stopWatch()
		}
		cancel()
		r.activeWG.Done()
	}
	return eng, engCtx, cancel, release, nil
}

func (r *Runtime) trackGenesisEngine(callCtx context.Context) (GenesisEngine, context.Context, context.CancelFunc, func(), error) {
	if r == nil {
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	if !r.ready || r.closed {
		r.mu.RUnlock()
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	eng := r.deps.GenesisEngine
	if eng == nil {
		if ge, ok := r.deps.BatchEngine.(GenesisEngine); ok {
			eng = ge
		} else if ge, ok := r.deps.Pipeline.(GenesisEngine); ok {
			eng = ge
		}
	}
	if eng == nil {
		r.mu.RUnlock()
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	baseCtx := r.baseCtx
	r.activeWG.Add(1)
	r.mu.RUnlock()

	engCtx, cancel := context.WithCancel(callCtx)
	var stopWatch func() bool
	if baseCtx != nil {
		stopWatch = context.AfterFunc(baseCtx, cancel)
	}
	release := func() {
		if stopWatch != nil {
			stopWatch()
		}
		cancel()
		r.activeWG.Done()
	}
	return eng, engCtx, cancel, release, nil
}

func (r *Runtime) autopilotEngine() (AutopilotEngine, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed {
		return nil, ErrRuntimeNotReady
	}
	if r.deps.AutopilotEngine != nil {
		return r.deps.AutopilotEngine, nil
	}
	if ae, ok := r.deps.BatchEngine.(AutopilotEngine); ok {
		return ae, nil
	}
	if ae, ok := r.deps.Pipeline.(AutopilotEngine); ok {
		return ae, nil
	}
	return nil, ErrRuntimeNotReady
}

func (r *Runtime) arcSummaryGenerator() (ArcSummaryGenerator, error) {
	if r == nil {
		return nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.ready || r.closed {
		return nil, ErrRuntimeNotReady
	}
	if gen, ok := r.deps.Pipeline.(ArcSummaryGenerator); ok {
		return gen, nil
	}
	if gen, ok := r.deps.AutopilotEngine.(ArcSummaryGenerator); ok {
		return gen, nil
	}
	return nil, ErrRuntimeNotReady
}

func (r *Runtime) trackDirectorEngine(callCtx context.Context) (DirectorEngine, context.Context, context.CancelFunc, func(), error) {
	if r == nil {
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	if !r.ready || r.closed {
		r.mu.RUnlock()
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	eng := r.deps.DirectorEngine
	if eng == nil {
		if de, ok := r.deps.BatchEngine.(DirectorEngine); ok {
			eng = de
		} else if de, ok := r.deps.Pipeline.(DirectorEngine); ok {
			eng = de
		}
	}
	if eng == nil {
		r.mu.RUnlock()
		return nil, nil, nil, nil, ErrRuntimeNotReady
	}
	baseCtx := r.baseCtx
	r.activeWG.Add(1)
	r.mu.RUnlock()

	engCtx, cancel := context.WithCancel(callCtx)
	var stopWatch func() bool
	if baseCtx != nil {
		stopWatch = context.AfterFunc(baseCtx, cancel)
	}
	release := func() {
		if stopWatch != nil {
			stopWatch()
		}
		cancel()
		r.activeWG.Done()
	}
	return eng, engCtx, cancel, release, nil
}

func (r *Runtime) dirPicker() DirPickerFunc {
	if r != nil {
		r.mu.RLock()
		fn := r.deps.DirPicker
		r.mu.RUnlock()
		if fn != nil {
			return fn
		}
	}
	return defaultPickExportDir
}

func defaultPickExportDir(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            mygo.CallerWindow(ctx),
		Title:             "选择本地 Markdown 导出目录",
		Directory:         true,
		CreateDirectories: true,
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}

func validatePositiveID(field string, id int64) error {
	if id <= 0 {
		return &domain.ValidationError{
			Field:   field,
			Message: fmt.Sprintf("%s 必须大于 0", field),
		}
	}
	return nil
}
