package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nib/internal/domain"
	"nib/internal/llm"
)

const (
	defaultSaveTimeout    = 5 * time.Second
	defaultDraftMaxTokens = 4096
)

// Store 定义单章生成流水线所需的最小持久化接口（与 internal/store.Store 方法签名直接对齐）。
type Store interface {
	GetBook(ctx context.Context, id int64) (domain.Book, error)
	GetVolume(ctx context.Context, id int64) (domain.Volume, error)
	GetChapter(ctx context.Context, id int64) (domain.Chapter, error)
	GetModelRoute(ctx context.Context, stage domain.PipelineStage) (domain.ModelRoute, error)
	GetProvider(ctx context.Context, id int64) (domain.Provider, error)
	SaveChapterContent(
		ctx context.Context,
		chapterID int64,
		content string,
		status domain.ChapterStatus,
		source domain.VersionSource,
		modelName string,
		briefingJSON string,
	) (domain.Chapter, error)
}

// Compiler 定义上下文编译接口（与 internal/compiler.Compiler 对齐）。
type Compiler interface {
	Compile(ctx context.Context, chapterID int64, opts domain.BriefingOptions) (domain.Briefing, error)
}

// LLMStreamer 定义流式模型调用接口（与 internal/llm.Client 对齐）。
type LLMStreamer interface {
	Stream(
		ctx context.Context,
		provider domain.Provider,
		req llm.ChatRequest,
		onEvent func(llm.StreamEvent) error,
	) (domain.TokenUsage, error)
}

// Exporter 定义单向 Markdown 章节导出接口（与 internal/exporter 对齐）。
type Exporter interface {
	ExportChapter(book domain.Book, vol domain.Volume, chap domain.Chapter) (string, error)
}

// ExporterFunc 允许将包级函数（如 exporter.ExportChapter）直接适配为 Exporter 接口。
type ExporterFunc func(book domain.Book, vol domain.Volume, chap domain.Chapter) (string, error)

func (f ExporterFunc) ExportChapter(book domain.Book, vol domain.Volume, chap domain.Chapter) (string, error) {
	return f(book, vol, chap)
}

// Option 用于定制 Runner 行为（如缩短中断落库超时）。
type Option func(*Runner)

// WithSaveTimeout 配置中断保底保存时 detached context 的超时时间。
func WithSaveTimeout(d time.Duration) Option {
	return func(r *Runner) {
		if d > 0 {
			r.saveTimeout = d
		}
	}
}

// WithStyleStore 显式注入 Phase 3 文风与质检仓储接口（若 store 本身实现 StyleStore，New 会自动挂载）。
func WithStyleStore(ss StyleStore) Option {
	return func(r *Runner) {
		if ss != nil {
			r.styleStore = ss
		}
	}
}

// WithBatchStore 显式注入 Phase 4 微批调度与 Token 统计仓储接口（若 store 本身实现 BatchStore，New 会自动挂载）。
func WithBatchStore(bs BatchStore) Option {
	return func(r *Runner) {
		if bs != nil {
			r.batchStore = bs
		}
	}
}

// WithDeltaExtractor 注入完章记忆增量抽取器，供 full_pipeline 串行微批接力使用。
func WithDeltaExtractor(ext DeltaExtractor) Option {
	return func(r *Runner) {
		if ext != nil {
			r.extractor = ext
		}
	}
}

// WithProgressListener 注入串行微批实时进度监听回调（生产环境接入 app.EmitBatchProgress）。
func WithProgressListener(fn func(domain.BatchProgress)) Option {
	return func(r *Runner) {
		if fn != nil {
			r.progressListener = fn
		}
	}
}

// Runner 编排单章流式出稿、异常/中止保底落库、单向 Markdown 导出、Phase 3 审校润色，以及 Phase 4 批量章纲与串行微批调度。
type Runner struct {
	store            Store
	styleStore       StyleStore
	batchStore       BatchStore
	outlineStore     OutlineStore
	extractor        DeltaExtractor
	compiler         Compiler
	llm               LLMStreamer
	exporter          Exporter
	progressListener  func(domain.BatchProgress)
	autopilotListener func(domain.AutopilotState)
	saveTimeout       time.Duration

	batchMu       sync.Mutex
	closed        bool
	batchCancels  map[int64]activeBatchHandle
	batchTokenSeq uint64
	batchWG       sync.WaitGroup

	autopilotMu sync.Mutex
	autopilots  map[int64]*autopilotSession
}

// New 创建流水线执行器。若 store 同时实现 StyleStore / BatchStore / OutlineStore，将自动挂载。
func New(store Store, comp Compiler, streamer LLMStreamer, exp Exporter, opts ...Option) *Runner {
	r := &Runner{
		store:        store,
		compiler:     comp,
		llm:          streamer,
		exporter:     exp,
		saveTimeout:  defaultSaveTimeout,
		batchCancels: make(map[int64]activeBatchHandle),
		autopilots:   make(map[int64]*autopilotSession),
	}
	if ss, ok := store.(StyleStore); ok {
		r.styleStore = ss
	}
	if bs, ok := store.(BatchStore); ok {
		r.batchStore = bs
	}
	if os, ok := store.(OutlineStore); ok {
		r.outlineStore = os
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// GenerateChapter 执行 Phase 1 单章流式出稿主链路：
// 1. 解析 draft 模型路由与启用供应商；
// 2. 编译作战简报并推送 started 分片；
// 3. 调用统一 LLM 客户端逐 token 推流；
// 4. 正常完成时先快照再写库（status=reviewing）并按需触发单向 Markdown 导出；
// 5. 若中途收到 context.Canceled 或 onChunk 要求停止，使用 detached 短超时上下文保底保存已产出正文（status=partial），避免丢稿。
func (r *Runner) GenerateChapter(
	ctx context.Context,
	chapterID int64,
	opts domain.GenerateChapterOptions,
	onChunk func(domain.GenChunk) error,
) (domain.Chapter, error) {
	if r == nil || r.store == nil || r.compiler == nil || r.llm == nil {
		return domain.Chapter{}, errors.New("pipeline runner dependencies not initialized")
	}
	if chapterID <= 0 {
		return domain.Chapter{}, fmt.Errorf("%w: chapterId must be > 0", domain.ErrValidation)
	}
	if err := opts.Validate(); err != nil {
		return domain.Chapter{}, err
	}

	emit := func(chunk domain.GenChunk) error {
		if onChunk == nil {
			return nil
		}
		return onChunk(chunk)
	}

	// 1. 加载章节、书卷，并解析 draft 路由与供应商。
	chap, err := r.store.GetChapter(ctx, chapterID)
	if err != nil {
		return domain.Chapter{}, fmt.Errorf("load chapter %d: %w", chapterID, err)
	}
	book, err := r.store.GetBook(ctx, chap.BookID)
	if err != nil {
		return domain.Chapter{}, fmt.Errorf("load book %d: %w", chap.BookID, err)
	}
	vol, err := r.store.GetVolume(ctx, chap.VolumeID)
	if err != nil {
		return domain.Chapter{}, fmt.Errorf("load volume %d: %w", chap.VolumeID, err)
	}

	route, provider, err := r.resolveStageRouteAndProvider(ctx, domain.StageDraft)
	if err != nil {
		return domain.Chapter{}, err
	}

	// 2. 编译上下文作战简报并序列化快照。
	briefingOpts := mergeBriefingOptions(opts)
	briefing, err := r.compiler.Compile(ctx, chapterID, briefingOpts)
	if err != nil {
		return domain.Chapter{}, fmt.Errorf("compile briefing: %w", err)
	}
	briefingBytes, err := json.Marshal(briefing)
	if err != nil {
		return domain.Chapter{}, fmt.Errorf("marshal briefing json: %w", err)
	}
	briefingJSON := string(briefingBytes)

	if err := emit(domain.GenChunk{
		Kind:         domain.GenChunkStarted,
		ChapterID:    chapterID,
		Status:       domain.ChapterStatusDrafting,
		ModelName:    route.ModelName,
		ProviderName: provider.Name,
	}); err != nil {
		return domain.Chapter{}, domain.ErrGenerationCanceled
	}

	// 3. 构造模型请求并流式推块。
	isContinue := strings.EqualFold(strings.TrimSpace(opts.Mode), "continue") && strings.TrimSpace(chap.Content) != ""
	userPrompt := briefing.UserPrompt
	basePrefix := ""
	if isContinue {
		basePrefix = strings.TrimRight(chap.Content, " \t")
		tail := tailRunes(basePrefix, 1800)
		userPrompt = userPrompt + "\n\n【续写模式指令（必须严格遵守）】\n" +
			"本章已由作者撰写前半部分，请紧接下方「当前正文末尾」的最后一句无缝往下续写，补完剩余节拍与章末钩子。\n" +
			"- 严禁重复或改写已写内容，直接输出续写的新增正文；\n" +
			"- 保持人称、语气、节奏与前文完全一致。\n\n" +
			"【当前正文末尾】\n" + tail
	}

	temperature := route.Temperature
	if opts.Temperature != nil {
		temperature = *opts.Temperature
	}
	maxTokens := route.MaxTokens
	if opts.MaxTokens != nil {
		maxTokens = *opts.MaxTokens
	}
	if maxTokens <= 0 {
		maxTokens = defaultDraftMaxTokens
	}

	chatReq := llm.ChatRequest{
		Model:       route.ModelName,
		System:      briefing.SystemPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userPrompt}},
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	var contentBuf strings.Builder
	var finalUsage domain.TokenUsage
	var finishReason string
	var stoppedByCallback bool
	draftStart := time.Now()
	activeModel := route.ModelName
	activeProviderName := provider.Name
	basePrefixWords := 0
	if isContinue {
		basePrefixWords = domain.CountWords(basePrefix)
	}
	runningDeltaWords := 0

	usedProv, usedModel, streamUsage, streamErr := r.streamWithFallbackNotify(
		ctx,
		route,
		provider,
		chatReq,
		func(ev llm.StreamEvent) error {
			if ev.FinishReason != "" {
				finishReason = ev.FinishReason
			}
			if ev.ReasoningDelta != "" {
				if err := emit(domain.GenChunk{
					Kind:           domain.GenChunkThinking,
					ChapterID:      chapterID,
					ReasoningDelta: ev.ReasoningDelta,
					Status:         domain.ChapterStatusDrafting,
					ModelName:      activeModel,
					ProviderName:   activeProviderName,
				}); err != nil {
					stoppedByCallback = true
					return err
				}
			}
			if ev.Delta != "" {
				contentBuf.WriteString(ev.Delta)
				runningDeltaWords += domain.CountWords(ev.Delta)
				if err := emit(domain.GenChunk{
					Kind:         domain.GenChunkDelta,
					ChapterID:    chapterID,
					Delta:        ev.Delta,
					WordCount:    basePrefixWords + runningDeltaWords,
					Status:       domain.ChapterStatusDrafting,
					ModelName:    activeModel,
					ProviderName: activeProviderName,
				}); err != nil {
					stoppedByCallback = true
					return err
				}
			}
			if ev.Usage != nil {
				finalUsage = *ev.Usage
				uCopy := *ev.Usage
				if err := emit(domain.GenChunk{
					Kind:         domain.GenChunkUsage,
					ChapterID:    chapterID,
					Usage:        &uCopy,
					FinishReason: finishReason,
					ModelName:    activeModel,
					ProviderName: activeProviderName,
				}); err != nil {
					stoppedByCallback = true
					return err
				}
			}
			return nil
		},
		func(fbProv domain.Provider, fbModel string) {
			activeModel = fbModel
			activeProviderName = fbProv.Name
		},
	)
	if usedModel != "" {
		route.ModelName = usedModel
	}
	if usedProv.Name != "" {
		provider = usedProv
	}

	if hasUsage(streamUsage) {
		finalUsage = streamUsage
	}
	if hasUsage(finalUsage) {
		r.recordTokenUsage(ctx, chap.BookID, chap.ID, domain.StageDraft, usedProv.Name, usedModel, finalUsage, time.Since(draftStart).Milliseconds())
	}

	generated := contentBuf.String()
	finalContent := generated
	if isContinue && strings.TrimSpace(generated) != "" {
		finalContent = joinContinuation(basePrefix, generated)
	}

	// 4. 处理中止或流式错误：只要已流出正文，必须用 detached 上下文保底落库为 partial。
	if streamErr != nil {
		isCanceled := stoppedByCallback ||
			errors.Is(streamErr, domain.ErrGenerationCanceled) ||
			errors.Is(streamErr, context.Canceled) ||
			errors.Is(ctx.Err(), context.Canceled)

		if isCanceled {
			if strings.TrimSpace(generated) != "" {
				savedChap, saveErr := r.savePartialDetached(ctx, book, vol, chapterID, finalContent, route.ModelName, briefingJSON)
				if saveErr != nil {
					return domain.Chapter{}, fmt.Errorf("%w: failed to preserve partial draft: %v", domain.ErrGenerationCanceled, saveErr)
				}
				_ = emit(domain.GenChunk{
					Kind:         domain.GenChunkPartial,
					ChapterID:    chapterID,
					WordCount:    savedChap.WordCount,
					Status:       domain.ChapterStatusPartial,
					ModelName:    route.ModelName,
					ProviderName: provider.Name,
					Usage:        usagePtrOrNil(finalUsage),
					FinishReason: finishReason,
				})
				return savedChap, domain.ErrGenerationCanceled
			}
			_ = emit(domain.GenChunk{
				Kind:         domain.GenChunkPartial,
				ChapterID:    chapterID,
				Status:       chap.Status,
				ModelName:    route.ModelName,
				ProviderName: provider.Name,
				Usage:        usagePtrOrNil(finalUsage),
			})
			return domain.Chapter{}, domain.ErrGenerationCanceled
		}

		// 非取消类错误：若已有部分文本产出，同样保底保存为 partial 防止丢稿。
		var savedChap domain.Chapter
		errStatus := chap.Status
		errWordCount := chap.WordCount
		if strings.TrimSpace(generated) != "" {
			if saved, saveErr := r.savePartialDetached(ctx, book, vol, chapterID, finalContent, route.ModelName, briefingJSON); saveErr == nil {
				savedChap = saved
				errStatus = saved.Status
				errWordCount = saved.WordCount
			}
		}
		_ = emit(domain.GenChunk{
			Kind:         domain.GenChunkError,
			ChapterID:    chapterID,
			WordCount:    errWordCount,
			Status:       errStatus,
			ModelName:    route.ModelName,
			ProviderName: provider.Name,
			Usage:        usagePtrOrNil(finalUsage),
			Error:        streamErr.Error(),
			Retryable:    llm.IsRetryable(streamErr),
		})
		return savedChap, streamErr
	}

	// 5. 空响应保护：严禁将空正文覆盖已有草稿或将章节标为 reviewing。
	if strings.TrimSpace(generated) == "" {
		emptyErr := fmt.Errorf("%w: 模型未生成任何正文内容", domain.ErrUpstreamUnavailable)
		_ = emit(domain.GenChunk{
			Kind:         domain.GenChunkError,
			ChapterID:    chapterID,
			WordCount:    chap.WordCount,
			Status:       chap.Status,
			ModelName:    route.ModelName,
			ProviderName: provider.Name,
			Usage:        usagePtrOrNil(finalUsage),
			FinishReason: finishReason,
			Error:        emptyErr.Error(),
			Retryable:    true,
		})
		return domain.Chapter{}, emptyErr
	}

	// 6. 正常完成：落库（由 Store 事务保证先归档旧正文至 chapter_versions）、同步单向导出、发 completed 事件。
	savedChap, err := r.store.SaveChapterContent(
		ctx,
		chapterID,
		finalContent,
		domain.ChapterStatusReviewing,
		domain.VersionSourceAIDraft,
		route.ModelName,
		briefingJSON,
	)
	if err != nil {
		return domain.Chapter{}, fmt.Errorf("save generated chapter: %w", err)
	}

	var exportWarning string
	if err := r.tryExportChapter(book, vol, savedChap); err != nil {
		exportWarning = fmt.Sprintf("正文已保存，但单向导出失败: %v", err)
	}

	// Phase 3：完章落库后自动触发 0 成本本地 AI 味扫描并落库质检报告（不阻塞主出稿返回）。
	if r.styleStore != nil {
		_, _ = r.LintChapterLocal(ctx, chapterID)
	}

	_ = emit(domain.GenChunk{
		Kind:         domain.GenChunkCompleted,
		ChapterID:    chapterID,
		WordCount:    savedChap.WordCount,
		Status:       savedChap.Status,
		ModelName:    route.ModelName,
		ProviderName: provider.Name,
		Usage:        usagePtrOrNil(finalUsage),
		FinishReason: finishReason,
		Error:        exportWarning,
	})
	return savedChap, nil
}

func (r *Runner) savePartialDetached(
	ctx context.Context,
	book domain.Book,
	vol domain.Volume,
	chapterID int64,
	content string,
	modelName string,
	briefingJSON string,
) (domain.Chapter, error) {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.saveTimeout)
	defer cancel()

	savedChap, err := r.store.SaveChapterContent(
		saveCtx,
		chapterID,
		content,
		domain.ChapterStatusPartial,
		domain.VersionSourceAIDraft,
		modelName,
		briefingJSON,
	)
	if err != nil {
		return domain.Chapter{}, err
	}
	// 中断场景下尽力同步单向导出，不影响数据库已安全落库的结果。
	_ = r.tryExportChapter(book, vol, savedChap)
	return savedChap, nil
}

func (r *Runner) tryExportChapter(book domain.Book, vol domain.Volume, chap domain.Chapter) error {
	if r.exporter == nil || strings.TrimSpace(book.ExportDir) == "" {
		return nil
	}
	_, err := r.exporter.ExportChapter(book, vol, chap)
	if errors.Is(err, domain.ErrExportDirNotSet) {
		return nil
	}
	return err
}

func mergeBriefingOptions(opts domain.GenerateChapterOptions) domain.BriefingOptions {
	bo := opts.Briefing
	extraTop := strings.TrimSpace(opts.ExtraInstruction)
	extraInner := strings.TrimSpace(bo.ExtraInstruction)
	switch {
	case extraTop == "":
		bo.ExtraInstruction = extraInner
	case extraInner == "" || extraInner == extraTop:
		bo.ExtraInstruction = extraTop
	default:
		bo.ExtraInstruction = extraInner + "\n" + extraTop
	}
	return bo
}

func hasUsage(u domain.TokenUsage) bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0
}

func usagePtrOrNil(u domain.TokenUsage) *domain.TokenUsage {
	if !hasUsage(u) {
		return nil
	}
	cp := u
	return &cp
}

func tailRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[len(r)-maxRunes:])
}

func joinContinuation(prefix, addition string) string {
	p := strings.TrimRight(prefix, " \t")
	a := strings.TrimLeft(addition, "\r\n")
	if p == "" {
		return addition
	}
	if a == "" {
		return p
	}
	if strings.HasSuffix(p, "\n") {
		return p + a
	}
	// 如果前文结尾已是完整句末标点，默认另起一段；否则直接衔接句子。
	lastRune := []rune(p)[len([]rune(p))-1]
	switch lastRune {
	case '。', '！', '？', '”', '」', '』', '…', '!', '?':
		return p + "\n\n" + a
	default:
		return p + a
	}
}

// RewriteSelection 对编辑器选中的局部段落执行流式 AI 微调（不直接覆写数据库，由前端预览后采纳）。
func (r *Runner) RewriteSelection(
	ctx context.Context,
	req domain.RewriteSelectionRequest,
	onChunk func(domain.GenChunk) error,
) (domain.RewriteSelectionResult, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.RewriteSelectionResult{}, errors.New("pipeline runner dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.RewriteSelectionResult{}, err
	}

	emit := func(chunk domain.GenChunk) error {
		if onChunk == nil {
			return nil
		}
		return onChunk(chunk)
	}

	chap, err := r.store.GetChapter(ctx, req.ChapterID)
	if err != nil {
		return domain.RewriteSelectionResult{}, fmt.Errorf("load chapter %d: %w", req.ChapterID, err)
	}
	book, err := r.store.GetBook(ctx, chap.BookID)
	if err != nil {
		return domain.RewriteSelectionResult{}, fmt.Errorf("load book %d: %w", chap.BookID, err)
	}

	// 优先使用 polish 路由，未配置时回退 draft 路由。
	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StagePolish, domain.StageDraft)
	if err != nil {
		return domain.RewriteSelectionResult{}, err
	}

	actionGuide := selectionActionGuide(req.Action, req.CustomInstruction)
	sysPrompt := fmt.Sprintf(
		"你是中文长篇网文精修编辑，正在协助作者局部修改《%s》第 %d 章《%s》的选中片段。\n"+
			"输出铁律：\n"+
			"1. 只输出改写后的目标片段纯文本，严禁输出解释、前缀、引号包裹或 Markdown 代码块；\n"+
			"2. 必须与上文和下文自然衔接，严禁更改未提及的人名、境界、道具或剧情走向；\n"+
			"3. 杜绝翻译腔、说教总结句和 AI 套话（如“不由得”“宛如”“空气仿佛凝固”）。",
		book.Title, chap.GlobalSeq, chap.Title,
	)

	var userBuf strings.Builder
	fmt.Fprintf(&userBuf, "【改写要求】\n%s\n\n", actionGuide)
	if before := strings.TrimSpace(tailRunes(req.BeforeContext, 320)); before != "" {
		fmt.Fprintf(&userBuf, "【上文参考（仅供衔接，不要重复输出）】\n%s\n\n", before)
	}
	fmt.Fprintf(&userBuf, "【需要改写的选中原文】\n%s\n", req.SelectedText)
	if after := strings.TrimSpace(headRunes(req.AfterContext, 320)); after != "" {
		fmt.Fprintf(&userBuf, "\n【下文参考（仅供衔接，不要重复输出）】\n%s\n", after)
	}

	if err := emit(domain.GenChunk{
		Kind:         domain.GenChunkStarted,
		ChapterID:    req.ChapterID,
		ModelName:    route.ModelName,
		ProviderName: provider.Name,
	}); err != nil {
		return domain.RewriteSelectionResult{}, domain.ErrGenerationCanceled
	}

	maxTokens := route.MaxTokens
	if maxTokens <= 0 || maxTokens > 4096 {
		maxTokens = 2048
	}
	chatReq := llm.ChatRequest{
		Model:       route.ModelName,
		System:      sysPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userBuf.String()}},
		Temperature: 0.65,
		MaxTokens:   maxTokens,
	}

	var outBuf strings.Builder
	var finalUsage domain.TokenUsage
	start := time.Now()
	activeModel := route.ModelName
	activeProviderName := provider.Name
	runningWords := 0

	usedProv, usedModel, streamUsage, streamErr := r.streamWithFallbackNotify(
		ctx,
		route,
		provider,
		chatReq,
		func(ev llm.StreamEvent) error {
			if ev.ReasoningDelta != "" {
				return emit(domain.GenChunk{
					Kind:           domain.GenChunkThinking,
					ChapterID:      req.ChapterID,
					ReasoningDelta: ev.ReasoningDelta,
					ModelName:      activeModel,
					ProviderName:   activeProviderName,
				})
			}
			if ev.Delta != "" {
				outBuf.WriteString(ev.Delta)
				runningWords += domain.CountWords(ev.Delta)
				return emit(domain.GenChunk{
					Kind:         domain.GenChunkDelta,
					ChapterID:    req.ChapterID,
					Delta:        ev.Delta,
					WordCount:    runningWords,
					ModelName:    activeModel,
					ProviderName: activeProviderName,
				})
			}
			if ev.Usage != nil {
				finalUsage = *ev.Usage
			}
			return nil
		},
		func(fbProv domain.Provider, fbModel string) {
			activeModel = fbModel
			activeProviderName = fbProv.Name
		},
	)
	if hasUsage(streamUsage) {
		finalUsage = streamUsage
	}
	if hasUsage(finalUsage) {
		r.recordTokenUsage(ctx, chap.BookID, chap.ID, domain.StagePolish, usedProv.Name, usedModel, finalUsage, time.Since(start).Milliseconds())
	}
	if streamErr != nil {
		if errors.Is(streamErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return domain.RewriteSelectionResult{}, domain.ErrGenerationCanceled
		}
		return domain.RewriteSelectionResult{}, streamErr
	}

	rewritten := strings.TrimSpace(outBuf.String())
	if rewritten == "" {
		return domain.RewriteSelectionResult{}, fmt.Errorf("%w: 模型未返回改写内容", domain.ErrUpstreamUnavailable)
	}
	_ = emit(domain.GenChunk{
		Kind:         domain.GenChunkCompleted,
		ChapterID:    req.ChapterID,
		WordCount:    domain.CountWords(rewritten),
		ModelName:    usedModel,
		ProviderName: usedProv.Name,
		Usage:        usagePtrOrNil(finalUsage),
	})
	return domain.RewriteSelectionResult{
		RewrittenText: rewritten,
		ModelName:     usedModel,
		ProviderName:  usedProv.Name,
		Usage:         usagePtrOrNil(finalUsage),
	}, nil
}

func headRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}

func selectionActionGuide(action, custom string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "expand":
		return "扩写细节：补充动作、感官与环境互动，增强画面感与爽点张力，字数扩充到原文的 1.5~2 倍，不要注水。"
	case "condense":
		return "精简紧凑：删去啰嗦修饰、重复心理活动与拖沓过渡，保留核心冲突和信息量，让节奏更凌厉。"
	case "dialogue":
		return "优化对话：让人物台词更符合身份与潜台词，减少书面语和播音腔，穿插简短神态与动作。"
	case "vivid":
		return "增强画面与动作感：把抽象概括改成具象动作和感官细节，提升代入感与节奏起伏。"
	case "deai":
		return "去 AI 味：彻底清除高频套话、三段排比、刻意升华和抽象情绪总结，改成自然地道的中文网文笔法。"
	case "custom":
		return strings.TrimSpace(custom)
	default:
		if strings.TrimSpace(custom) != "" {
			return strings.TrimSpace(custom)
		}
		return "润色该片段，使其行文更自然紧凑、有代入感。"
	}
}
