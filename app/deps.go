package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	GetWritingStats(ctx context.Context, bookID int64, days int) (domain.WritingStatsSummary, error)

	// Scratch Notes
	ListScratchNotes(ctx context.Context, bookID int64) ([]domain.ScratchNote, error)
	UpsertScratchNote(ctx context.Context, in domain.UpsertScratchNoteInput) (domain.ScratchNote, error)
	DeleteScratchNote(ctx context.Context, id int64) error

	// Trash & Restore
	ListTrashItems(ctx context.Context) ([]domain.TrashItem, error)
	RestoreTrashItem(ctx context.Context, trashID int64) (domain.TrashRestoreResult, error)
	DeleteTrashItem(ctx context.Context, trashID int64) error
	ClearTrash(ctx context.Context) (int64, error)
	RestoreDataFromFile(ctx context.Context, backupPath string) (string, error)

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
	BatchFinalizeChapters(ctx context.Context, chapterIDs []int64) (int, error)
	BatchDeleteChapters(ctx context.Context, chapterIDs []int64) (int, error)
	ReplaceBookText(ctx context.Context, in domain.ReplaceBookTextInput) (domain.ReplaceBookTextResult, error)
	UpdateChapterMeta(ctx context.Context, in domain.UpdateChapterMetaInput) (domain.Chapter, error)
	SaveChapterDraft(ctx context.Context, in domain.SaveChapterDraftInput) (domain.Chapter, error)
	FinalizeChapter(ctx context.Context, chapterID int64) (domain.Chapter, error)
	ListBeats(ctx context.Context, chapterID int64) ([]domain.Beat, error)
	ListBookBeats(ctx context.Context, bookID int64) (map[int64][]domain.Beat, error)
	ReplaceBeats(ctx context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error)

	// Chapter Versions
	ListChapterVersions(ctx context.Context, chapterID int64) ([]domain.ChapterVersion, error)
	RestoreVersion(ctx context.Context, versionID int64) (domain.Chapter, error)

	// Entities
	ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error)
	UpsertEntity(ctx context.Context, in domain.UpsertEntityInput) (domain.Entity, error)
	DeleteEntity(ctx context.Context, id int64) error
	ListEntityRelations(ctx context.Context, bookID int64) ([]domain.EntityRelation, error)
	ListEntityRelationsAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntityRelation, error)
	UpsertEntityRelation(ctx context.Context, in domain.UpsertEntityRelationInput) (domain.EntityRelation, error)
	DeleteEntityRelation(ctx context.Context, id int64) error
	PreviewEntityMerge(ctx context.Context, in domain.EntityMergePreviewInput) (domain.EntityMergePreview, error)
	ExecuteEntityMerge(ctx context.Context, in domain.EntityMergeExecuteInput) (domain.EntityMergeResult, error)
	PreviewBookRename(ctx context.Context, in domain.BookRenamePreviewInput) (domain.BookRenamePreview, error)
	ExecuteBookRename(ctx context.Context, in domain.BookRenameExecuteInput) (domain.BookRenameResult, error)
	ListSafetySnapshots(ctx context.Context, bookID int64) ([]domain.SafetySnapshot, error)
	RestoreSafetySnapshot(ctx context.Context, snapshotID int64) (string, error)

	// Director History
	ListDirectorMessages(ctx context.Context, bookID int64) ([]domain.DirectorMessage, error)
	ClearDirectorMessages(ctx context.Context, bookID int64) error

	// Database Backup
	BackupToFile(ctx context.Context, targetPath string) error
	Path() string

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
	ListStoryDeadlines(ctx context.Context, bookID int64, status domain.StoryDeadlineStatus) ([]domain.StoryDeadline, error)
	UpsertStoryDeadline(ctx context.Context, in domain.UpsertStoryDeadlineInput) (domain.StoryDeadline, error)
	DeleteStoryDeadline(ctx context.Context, id int64) error
	ListArcSummaries(ctx context.Context, bookID int64, level domain.ArcSummaryLevel) ([]domain.ArcSummary, error)
	UpsertArcSummary(ctx context.Context, in domain.UpsertArcSummaryInput) (domain.ArcSummary, error)
	GetPendingDelta(ctx context.Context, chapterID int64) (*domain.ChapterDelta, error)
	ListPendingDeltas(ctx context.Context, bookID int64) ([]domain.ChapterDelta, error)
	ListDirtyChapters(ctx context.Context, bookID int64) ([]domain.DirtyChapterItem, error)
	ClearDirtyChapters(ctx context.Context, bookID int64, chapterIDs []int64) (domain.ClearDirtyChaptersResult, error)
	ListConsistencyReports(ctx context.Context, bookID int64) ([]domain.BookConsistencyReport, error)
	UpdateConsistencyFindingStatus(ctx context.Context, reportID int64, findingID string, status domain.ConsistencyFindingStatus) (domain.BookConsistencyReport, error)
	DeleteConsistencyReport(ctx context.Context, reportID int64) error
	ApplyDeltaDecisions(ctx context.Context, in domain.ApplyDeltaInput) (domain.ApplyDeltaResult, error)
	SearchMemory(ctx context.Context, bookID int64, query string, maxChapterSeq int) ([]domain.MemorySearchHit, error)
}

// ConsistencyAuditEngine 定义第二层 AI 深度语义一致性审计接口（由 *pipeline.Runner 实现）。
type ConsistencyAuditEngine interface {
	RunAIConsistencyAudit(ctx context.Context, req domain.RunAIConsistencyAuditRequest) (domain.BookConsistencyReport, error)
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
	ListRewritePresets(ctx context.Context, bookID int64) ([]domain.RewritePreset, error)
	UpsertRewritePreset(ctx context.Context, in domain.UpsertRewritePresetInput) (domain.RewritePreset, error)
	DeleteRewritePreset(ctx context.Context, id int64) error
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

// ReverseExtractEngine 定义旧稿导入后的 AI 设定反推与确认落库接口（由 internal/pipeline 实现）。
type ReverseExtractEngine interface {
	ReverseExtractBook(
		ctx context.Context,
		req domain.ReverseExtractRequest,
		emit func(domain.ReverseExtractChunk) error,
	) (domain.ReverseExtractPreview, error)
	ApplyReverseExtract(ctx context.Context, in domain.ApplyReverseExtractInput) (domain.ApplyReverseExtractResult, error)
}

// GenesisEngine 定义 AI 开书（一步直出与两阶段协同）企划生成与一键落库接口（由 internal/pipeline 实现，零 MyGo 依赖）。
type GenesisEngine interface {
	GenerateBookGenesis(
		ctx context.Context,
		req domain.BookGenesisRequest,
		emit func(domain.GenesisChunk) error,
	) (domain.BookGenesisPlan, error)
	GenerateBookConcept(
		ctx context.Context,
		req domain.BookConceptRequest,
		emit func(domain.ConceptChunk) error,
	) (domain.BookConcept, error)
	GenerateBookBlueprint(
		ctx context.Context,
		req domain.BookBlueprintRequest,
		emit func(domain.BlueprintChunk) error,
	) (domain.BookBlueprint, error)
	ApplyBookGenesis(ctx context.Context, in domain.ApplyGenesisInput) (domain.ApplyGenesisResult, error)
}

// RadarAnalyzer 定义扫榜 AI 风向拆解接口（由 *pipeline.Runner 实现，零 MyGo 依赖）。
type RadarAnalyzer interface {
	AnalyzeRadarTrend(
		ctx context.Context,
		req domain.RadarAnalyzeRequest,
		emit func(domain.RadarAnalyzeChunk) error,
	) (domain.RadarAnalysis, error)
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
	initErr    error
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

// SetInitError 记录 WhenReady 阶段初始化失败原因，便于服务层向前端返回精确诊断信息。
func (r *Runtime) SetInitError(err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initErr = err
}

// RunBackground 启动受 Runtime 生命周期托管的后台维护任务（关库前会等待其结束）。
func (r *Runtime) RunBackground(fn func(ctx context.Context)) {
	if r == nil || fn == nil {
		return
	}
	r.mu.RLock()
	if !r.ready || r.closed || r.baseCtx == nil {
		r.mu.RUnlock()
		return
	}
	ctx := r.baseCtx
	r.activeWG.Add(1)
	r.mu.RUnlock()

	go func() {
		defer r.activeWG.Done()
		fn(ctx)
	}()
}

// Init 在 WhenReady 中注入已就绪的底层引擎与仓储实现。
func (r *Runtime) Init(deps RuntimeDeps) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.baseCancel != nil {
		r.baseCancel()
	}
	r.deps = deps
	r.initErr = nil
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

func requireDep[T any](r *Runtime, pick func(*RuntimeDeps) (T, bool)) (T, error) {
	var zero T
	if r == nil {
		return zero, ErrRuntimeNotReady
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.initErr != nil {
		return zero, fmt.Errorf("%w: %v", ErrRuntimeNotReady, r.initErr)
	}
	if !r.ready || r.closed {
		return zero, ErrRuntimeNotReady
	}
	dep, ok := pick(&r.deps)
	if !ok {
		return zero, ErrRuntimeNotReady
	}
	return dep, nil
}

func trackDep[T any](r *Runtime, callCtx context.Context, pick func(*RuntimeDeps) (T, bool)) (T, context.Context, context.CancelFunc, func(), error) {
	var zero T
	if r == nil {
		return zero, nil, nil, nil, ErrRuntimeNotReady
	}
	r.mu.RLock()
	if r.initErr != nil {
		r.mu.RUnlock()
		return zero, nil, nil, nil, fmt.Errorf("%w: %v", ErrRuntimeNotReady, r.initErr)
	}
	if !r.ready || r.closed {
		r.mu.RUnlock()
		return zero, nil, nil, nil, ErrRuntimeNotReady
	}
	dep, ok := pick(&r.deps)
	if !ok {
		r.mu.RUnlock()
		return zero, nil, nil, nil, ErrRuntimeNotReady
	}
	baseCtx := r.baseCtx
	// 必须在持有 RLock 期间递增计数，防止与 Close() 中的 activeWG.Wait() 产生竞态。
	r.activeWG.Add(1)
	r.mu.RUnlock()

	depCtx, cancel := context.WithCancel(callCtx)
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
	return dep, depCtx, cancel, release, nil
}

func channelEmitter[T any](ctx context.Context, cancel context.CancelFunc, ch *mygo.Channel[T]) func(T) error {
	return func(ev T) error {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if ch == nil {
			return nil
		}
		if sendErr := ch.Send(ev); sendErr != nil {
			if cancel != nil {
				cancel()
			}
			return sendErr
		}
		return nil
	}
}

func (r *Runtime) syncChapterMarkdownIfConfigured(ctx context.Context, chap domain.Chapter) error {
	exp := r.exporter()
	if exp == nil {
		return nil
	}
	st, err := r.store()
	if err != nil {
		return err
	}
	book, err := st.GetBook(ctx, chap.BookID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(book.ExportDir) == "" {
		return nil
	}
	_, err = exp.ExportChapter(ctx, chap.ID)
	return err
}

func (r *Runtime) store() (Store, error) {
	return requireDep(r, func(d *RuntimeDeps) (Store, bool) {
		return d.Store, d.Store != nil
	})
}

func (r *Runtime) exporter() Exporter {
	exp, err := r.requireExporter()
	if err != nil {
		return nil
	}
	return exp
}

func (r *Runtime) requireExporter() (Exporter, error) {
	return requireDep(r, func(d *RuntimeDeps) (Exporter, bool) {
		return d.Exporter, d.Exporter != nil
	})
}

func (r *Runtime) llm() (LLMClient, error) {
	return requireDep(r, func(d *RuntimeDeps) (LLMClient, bool) {
		return d.LLM, d.LLM != nil
	})
}

func (r *Runtime) compiler() (BriefingCompiler, error) {
	return requireDep(r, func(d *RuntimeDeps) (BriefingCompiler, bool) {
		return d.Compiler, d.Compiler != nil
	})
}

func (r *Runtime) trackPipeline(callCtx context.Context) (PipelineRunner, context.Context, context.CancelFunc, func(), error) {
	return trackDep(r, callCtx, func(d *RuntimeDeps) (PipelineRunner, bool) {
		return d.Pipeline, d.Pipeline != nil
	})
}

func (r *Runtime) memoryStore() (MemoryStore, error) {
	return requireDep(r, func(d *RuntimeDeps) (MemoryStore, bool) {
		return d.MemoryStore, d.MemoryStore != nil
	})
}

func (r *Runtime) trackExtractor(callCtx context.Context) (DeltaExtractor, context.Context, func(), error) {
	ext, ctx, _, release, err := trackDep(r, callCtx, func(d *RuntimeDeps) (DeltaExtractor, bool) {
		return d.DeltaExtractor, d.DeltaExtractor != nil
	})
	return ext, ctx, release, err
}

func (r *Runtime) styleStore() (StyleStore, error) {
	return requireDep(r, func(d *RuntimeDeps) (StyleStore, bool) {
		return d.StyleStore, d.StyleStore != nil
	})
}

func (r *Runtime) styleEngine() (StyleEngine, error) {
	return requireDep(r, func(d *RuntimeDeps) (StyleEngine, bool) {
		return d.StyleEngine, d.StyleEngine != nil
	})
}

func (r *Runtime) trackStyleEngine(callCtx context.Context) (StyleEngine, context.Context, func(), error) {
	eng, ctx, _, release, err := trackDep(r, callCtx, func(d *RuntimeDeps) (StyleEngine, bool) {
		return d.StyleEngine, d.StyleEngine != nil
	})
	return eng, ctx, release, err
}

func (r *Runtime) batchStore() (BatchStore, error) {
	return requireDep(r, func(d *RuntimeDeps) (BatchStore, bool) {
		return d.BatchStore, d.BatchStore != nil
	})
}

func (r *Runtime) batchEngine() (BatchEngine, error) {
	return requireDep(r, func(d *RuntimeDeps) (BatchEngine, bool) {
		return d.BatchEngine, d.BatchEngine != nil
	})
}

func (r *Runtime) trackBatchEngine(callCtx context.Context) (BatchEngine, context.Context, context.CancelFunc, func(), error) {
	return trackDep(r, callCtx, func(d *RuntimeDeps) (BatchEngine, bool) {
		return d.BatchEngine, d.BatchEngine != nil
	})
}

func (r *Runtime) trackRadarAnalyzer(callCtx context.Context) (RadarAnalyzer, context.Context, context.CancelFunc, func(), error) {
	return trackDep(r, callCtx, func(d *RuntimeDeps) (RadarAnalyzer, bool) {
		if ra, ok := d.GenesisEngine.(RadarAnalyzer); ok {
			return ra, true
		}
		if ra, ok := d.BatchEngine.(RadarAnalyzer); ok {
			return ra, true
		}
		if ra, ok := d.Pipeline.(RadarAnalyzer); ok {
			return ra, true
		}
		return nil, false
	})
}

func (r *Runtime) trackGenesisEngine(callCtx context.Context) (GenesisEngine, context.Context, context.CancelFunc, func(), error) {
	return trackDep(r, callCtx, func(d *RuntimeDeps) (GenesisEngine, bool) {
		if d.GenesisEngine != nil {
			return d.GenesisEngine, true
		}
		if ge, ok := d.BatchEngine.(GenesisEngine); ok {
			return ge, true
		}
		if ge, ok := d.Pipeline.(GenesisEngine); ok {
			return ge, true
		}
		return nil, false
	})
}

func (r *Runtime) autopilotEngine() (AutopilotEngine, error) {
	return requireDep(r, func(d *RuntimeDeps) (AutopilotEngine, bool) {
		if d.AutopilotEngine != nil {
			return d.AutopilotEngine, true
		}
		if ae, ok := d.BatchEngine.(AutopilotEngine); ok {
			return ae, true
		}
		if ae, ok := d.Pipeline.(AutopilotEngine); ok {
			return ae, true
		}
		return nil, false
	})
}

func (r *Runtime) reverseExtractEngine() (ReverseExtractEngine, error) {
	return requireDep(r, func(d *RuntimeDeps) (ReverseExtractEngine, bool) {
		eng, ok := d.Pipeline.(ReverseExtractEngine)
		return eng, ok && eng != nil
	})
}

func (r *Runtime) trackReverseExtractEngine(callCtx context.Context) (ReverseExtractEngine, context.Context, context.CancelFunc, func(), error) {
	return trackDep(r, callCtx, func(d *RuntimeDeps) (ReverseExtractEngine, bool) {
		eng, ok := d.Pipeline.(ReverseExtractEngine)
		return eng, ok && eng != nil
	})
}

func (r *Runtime) arcSummaryGenerator() (ArcSummaryGenerator, error) {
	return requireDep(r, func(d *RuntimeDeps) (ArcSummaryGenerator, bool) {
		if gen, ok := d.Pipeline.(ArcSummaryGenerator); ok {
			return gen, true
		}
		if gen, ok := d.AutopilotEngine.(ArcSummaryGenerator); ok {
			return gen, true
		}
		return nil, false
	})
}

func (r *Runtime) trackDirectorEngine(callCtx context.Context) (DirectorEngine, context.Context, context.CancelFunc, func(), error) {
	return trackDep(r, callCtx, func(d *RuntimeDeps) (DirectorEngine, bool) {
		if d.DirectorEngine != nil {
			return d.DirectorEngine, true
		}
		if de, ok := d.BatchEngine.(DirectorEngine); ok {
			return de, true
		}
		if de, ok := d.Pipeline.(DirectorEngine); ok {
			return de, true
		}
		return nil, false
	})
}

func (r *Runtime) trackConsistencyAuditEngine(callCtx context.Context) (ConsistencyAuditEngine, context.Context, context.CancelFunc, func(), error) {
	return trackDep(r, callCtx, func(d *RuntimeDeps) (ConsistencyAuditEngine, bool) {
		if ce, ok := d.Pipeline.(ConsistencyAuditEngine); ok {
			return ce, true
		}
		if ce, ok := d.AutopilotEngine.(ConsistencyAuditEngine); ok {
			return ce, true
		}
		return nil, false
	})
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
