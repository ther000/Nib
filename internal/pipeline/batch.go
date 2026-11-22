package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nib/internal/domain"
	"nib/internal/genres"
	"nib/internal/llm"
)

const defaultOutlineMaxTokens = 4096

type activeBatchHandle struct {
	cancel context.CancelFunc
	token  uint64
}

// BatchStore 定义 Phase 4 串行微批运行记录与 Token 统计所需的持久化接口（直接由 *store.Store 满足）。
type BatchStore interface {
	CreateBatchRun(ctx context.Context, bookID int64, chapterIDs []int64, mode domain.BatchMode) (domain.BatchRun, error)
	GetBatchRun(ctx context.Context, batchID int64) (domain.BatchRun, error)
	GetLatestBatchRun(ctx context.Context, bookID int64) (*domain.BatchRun, error)
	GetBatchRunItem(ctx context.Context, itemID int64) (domain.BatchRunItem, error)
	UpdateBatchItemState(ctx context.Context, itemID int64, state domain.BatchItemState, inputHash, errorMsg string) error
	UpdateBatchRunStatus(ctx context.Context, batchID int64, status domain.BatchRunStatus, doneItems int) error
	RecordTokenUsage(ctx context.Context, entry domain.TokenUsageLog) error
	GetUsageSummary(ctx context.Context, bookID int64) (domain.UsageSummary, error)
}

// OutlineStore 定义批量章纲生成落库所需的章节与节拍写入接口（直接由 *store.Store 满足）。
type OutlineStore interface {
	ListChapters(ctx context.Context, volumeID int64) ([]domain.Chapter, error)
	ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error)
	CreateChapter(ctx context.Context, in domain.CreateChapterInput) (domain.Chapter, error)
	UpdateChapterMeta(ctx context.Context, in domain.UpdateChapterMetaInput) (domain.Chapter, error)
	ReplaceBeats(ctx context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error)
}

// DeltaExtractor 定义完章记忆增量抽取接口（直接由 *memory.Extractor 满足）。
type DeltaExtractor interface {
	ExtractChapterDelta(ctx context.Context, chapterID int64) (domain.ChapterDelta, error)
}

// Close 取消所有运行中的后台微批队列并等待在途保底保存安全结束（应在关闭 SQLite 前调用）。
func (r *Runner) Close() {
	if r == nil {
		return
	}
	r.batchMu.Lock()
	r.closed = true
	cancels := make([]context.CancelFunc, 0, len(r.batchCancels))
	for _, h := range r.batchCancels {
		if h.cancel != nil {
			cancels = append(cancels, h.cancel)
		}
	}
	r.batchMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}

	r.autopilotMu.Lock()
	autoCancels := make([]context.CancelFunc, 0, len(r.autopilots))
	for _, sess := range r.autopilots {
		sess.mu.Lock()
		if sess.active && sess.cancel != nil {
			autoCancels = append(autoCancels, sess.cancel)
		}
		sess.mu.Unlock()
	}
	r.autopilotMu.Unlock()
	for _, cancel := range autoCancels {
		cancel()
	}

	r.batchWG.Wait()
}

// WaitAllBatches 等待所有后台微批任务退出（供单测与优雅关机使用）。
func (r *Runner) WaitAllBatches() {
	if r == nil {
		return
	}
	r.batchWG.Wait()
}

func (r *Runner) recordTokenUsage(
	ctx context.Context,
	bookID int64,
	chapterID int64,
	stage domain.PipelineStage,
	providerName string,
	modelName string,
	usage domain.TokenUsage,
	latencyMs int64,
) {
	domain.NotifyUsageObserver(ctx, usage)

	if r == nil || r.batchStore == nil || bookID <= 0 {
		return
	}
	if !stage.ValidUsageStage() || strings.TrimSpace(providerName) == "" || strings.TrimSpace(modelName) == "" {
		return
	}
	if latencyMs < 0 {
		latencyMs = 0
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.saveTimeout)
	defer cancel()
	_ = r.batchStore.RecordTokenUsage(saveCtx, domain.TokenUsageLog{
		BookID:           bookID,
		ChapterID:        chapterID,
		Stage:            stage,
		ProviderName:     providerName,
		ModelName:        modelName,
		PromptTokens:     max(0, usage.PromptTokens),
		CompletionTokens: max(0, usage.CompletionTokens),
		LatencyMs:        latencyMs,
	})
}

// BatchGenerateOutlines 使用 outline 路由一次性流式生成 1~10 章结构化章纲与节拍卡，落库为 outlined 状态并返回已保存章节列表。
func (r *Runner) BatchGenerateOutlines(
	ctx context.Context,
	req domain.BatchOutlineRequest,
	emit func(domain.OutlineChunk) error,
) ([]domain.Chapter, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return nil, errors.New("pipeline dependencies not initialized")
	}
	if r.outlineStore == nil {
		return nil, errors.New("pipeline outline store not initialized")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	count := req.Count
	if count <= 0 {
		count = domain.DefaultBatchOutlineCount
	}

	send := func(chunk domain.OutlineChunk) error {
		if emit == nil {
			return nil
		}
		return emit(chunk)
	}

	book, err := r.store.GetBook(ctx, req.BookID)
	if err != nil {
		return nil, fmt.Errorf("load book %d: %w", req.BookID, err)
	}
	vol, err := r.store.GetVolume(ctx, req.VolumeID)
	if err != nil {
		return nil, fmt.Errorf("load volume %d: %w", req.VolumeID, err)
	}
	existingChaps, err := r.outlineStore.ListBookChapters(ctx, book.ID)
	if err != nil {
		return nil, fmt.Errorf("list book chapters: %w", err)
	}

	startSeq := req.StartSeq
	if startSeq <= 0 {
		maxSeq := 0
		for _, ch := range existingChaps {
			if ch.GlobalSeq > maxSeq {
				maxSeq = ch.GlobalSeq
			}
		}
		startSeq = maxSeq + 1
	}

	route, provider, err := r.resolveStageRouteAndProvider(ctx, domain.StageOutline)
	if err != nil {
		return nil, err
	}

	if err := send(domain.OutlineChunk{Kind: domain.GenChunkStarted}); err != nil {
		return nil, domain.ErrGenerationCanceled
	}

	sysPrompt, userPrompt := buildBatchOutlinePrompts(book, vol, existingChaps, startSeq, count, req.ArcGoal)
	maxTokens := route.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultOutlineMaxTokens
	}
	chatReq := llm.ChatRequest{
		Model:       route.ModelName,
		System:      sysPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userPrompt}},
		Temperature: route.Temperature,
		MaxTokens:   maxTokens,
	}

	start := time.Now()
	var buf strings.Builder
	var finalUsage domain.TokenUsage
	usedProv, usedModel, streamUsage, streamErr := r.streamWithFallback(ctx, route, provider, chatReq, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
			if err := send(domain.OutlineChunk{Kind: domain.GenChunkDelta, Delta: ev.Delta}); err != nil {
				return err
			}
		}
		if ev.Usage != nil {
			finalUsage = *ev.Usage
		}
		return nil
	})
	if hasUsage(streamUsage) {
		finalUsage = streamUsage
	}
	if streamErr != nil {
		_ = send(domain.OutlineChunk{Kind: domain.GenChunkError, Error: streamErr.Error()})
		return nil, streamErr
	}
	r.recordTokenUsage(ctx, book.ID, 0, domain.StageOutline, usedProv.Name, usedModel, finalUsage, time.Since(start).Milliseconds())

	outlines, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		book.ID,
		0,
		domain.StageOutline,
		usedProv,
		route,
		defaultOutlineMaxTokens,
		`批量章纲 JSON（需包含 "outlines" 数组，每章含 title, emotion_tag, hook_type, hook_note, outline_summary, beats）`,
		buf.String(),
		func(raw string) ([]domain.GeneratedChapterOutline, error) {
			return parseBatchOutlinesPayload(raw, count)
		},
	)
	if parseErr != nil {
		if !errors.Is(parseErr, domain.ErrGenerationCanceled) {
			_ = send(domain.OutlineChunk{Kind: domain.GenChunkError, Error: parseErr.Error()})
		}
		return nil, parseErr
	}

	// 将生成的结构化章纲与节拍卡落库至指定卷。
	volChaps, err := r.outlineStore.ListChapters(ctx, vol.ID)
	if err != nil {
		return nil, fmt.Errorf("list volume chapters: %w", err)
	}
	volChapBySeq := make(map[int]domain.Chapter, len(volChaps))
	for _, ch := range volChaps {
		volChapBySeq[ch.GlobalSeq] = ch
	}

	savedChapters := make([]domain.Chapter, 0, len(outlines))
	for idx, out := range outlines {
		targetSeq := startSeq + idx
		var chap domain.Chapter
		if existing, ok := volChapBySeq[targetSeq]; ok &&
			strings.TrimSpace(existing.Content) == "" &&
			(existing.Status == domain.ChapterStatusPlanned || existing.Status == domain.ChapterStatusOutlined) {
			chap, err = r.outlineStore.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
				ID:             existing.ID,
				Title:          out.Title,
				Status:         domain.ChapterStatusOutlined,
				EmotionTag:     out.EmotionTag,
				HookType:       out.HookType,
				HookNote:       out.HookNote,
				OutlineSummary: out.OutlineSummary,
				CausalSummary:  existing.CausalSummary,
			})
			if err != nil {
				return nil, fmt.Errorf("update chapter %d outline: %w", existing.ID, err)
			}
		} else {
			created, cErr := r.outlineStore.CreateChapter(ctx, domain.CreateChapterInput{
				BookID:         book.ID,
				VolumeID:       vol.ID,
				Title:          out.Title,
				EmotionTag:     out.EmotionTag,
				HookType:       out.HookType,
				HookNote:       out.HookNote,
				OutlineSummary: out.OutlineSummary,
			})
			if cErr != nil {
				return nil, fmt.Errorf("create chapter outline #%d: %w", idx+1, cErr)
			}
			chap, err = r.outlineStore.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
				ID:             created.ID,
				Title:          created.Title,
				Status:         domain.ChapterStatusOutlined,
				EmotionTag:     out.EmotionTag,
				HookType:       out.HookType,
				HookNote:       out.HookNote,
				OutlineSummary: out.OutlineSummary,
			})
			if err != nil {
				return nil, fmt.Errorf("mark chapter %d outlined: %w", created.ID, err)
			}
		}

		if len(out.Beats) > 0 {
			if _, bErr := r.outlineStore.ReplaceBeats(ctx, chap.ID, out.Beats); bErr != nil {
				return nil, fmt.Errorf("save beats for chapter %d: %w", chap.ID, bErr)
			}
		}
		savedChapters = append(savedChapters, chap)
	}

	_ = send(domain.OutlineChunk{
		Kind:     domain.GenChunkCompleted,
		Outlines: outlines,
		Chapters: savedChapters,
		Usage:    usagePtrOrNil(finalUsage),
	})
	return savedChapters, nil
}

// StartBatchRun 创建并异步启动 1~5 章串行接力微批调度任务，后台逐章执行并实时推送 BatchProgress。
func (r *Runner) StartBatchRun(
	ctx context.Context,
	bookID int64,
	chapterIDs []int64,
	mode domain.BatchMode,
) (domain.BatchRun, error) {
	if r == nil || r.batchStore == nil || r.store == nil || r.compiler == nil || r.llm == nil {
		return domain.BatchRun{}, errors.New("pipeline batch dependencies not initialized")
	}
	if bookID <= 0 {
		return domain.BatchRun{}, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	if len(chapterIDs) == 0 || len(chapterIDs) > 5 {
		return domain.BatchRun{}, fmt.Errorf("%w: batch size must be 1..5", domain.ErrValidation)
	}
	if !mode.Valid() {
		return domain.BatchRun{}, fmt.Errorf("%w: invalid batch mode %q", domain.ErrValidation, mode)
	}
	// 预先校验 draft 路由配置，尽早向作者反馈未配置模型。
	if _, _, err := r.resolveStageRouteAndProvider(ctx, domain.StageDraft); err != nil {
		return domain.BatchRun{}, err
	}

	run, err := r.batchStore.CreateBatchRun(ctx, bookID, chapterIDs, mode)
	if err != nil {
		return domain.BatchRun{}, err
	}

	if err := r.launchBatchExecution(run.ID, 0); err != nil {
		return domain.BatchRun{}, err
	}
	return run, nil
}

// CancelBatchRun 中止正在运行的串行微批队列，返回最新状态快照。
func (r *Runner) CancelBatchRun(ctx context.Context, batchID int64) (domain.BatchRun, error) {
	if r == nil || r.batchStore == nil {
		return domain.BatchRun{}, errors.New("pipeline batch store not initialized")
	}
	r.batchMu.Lock()
	handle, active := r.batchCancels[batchID]
	r.batchMu.Unlock()
	if active && handle.cancel != nil {
		handle.cancel()
	}

	run, err := r.batchStore.GetBatchRun(ctx, batchID)
	if err != nil {
		return domain.BatchRun{}, err
	}
	if !active && (run.Status == domain.BatchRunRunning || run.Status == domain.BatchRunPaused) {
		for _, it := range run.Items {
			if it.State != domain.BatchItemAwaitingReview && it.State != domain.BatchItemFailed && it.State != domain.BatchItemCanceled {
				_ = r.batchStore.UpdateBatchItemState(ctx, it.ID, domain.BatchItemCanceled, "", "用户中止微批队列")
			}
		}
		_ = r.batchStore.UpdateBatchRunStatus(ctx, batchID, domain.BatchRunCanceled, countDoneItems(run.Items))
		run, err = r.batchStore.GetBatchRun(ctx, batchID)
		if err != nil {
			return domain.BatchRun{}, err
		}
		r.emitBatchProgress(domain.BatchProgress{
			BatchRunID: run.ID,
			BookID:     run.BookID,
			Status:     run.Status,
			TotalItems: run.TotalItems,
			DoneItems:  run.DoneItems,
			Message:    "已中止串行微批队列",
			UpdatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	return run, nil
}

// RetryBatchItem 对失败（failed）、已取消（canceled）或恢复待执行（queued）的单个微批章节项发起重跑，并继续顺延执行后续排队章节。
func (r *Runner) RetryBatchItem(ctx context.Context, itemID int64) (domain.BatchRun, error) {
	if r == nil || r.batchStore == nil {
		return domain.BatchRun{}, errors.New("pipeline batch store not initialized")
	}
	item, err := r.batchStore.GetBatchRunItem(ctx, itemID)
	if err != nil {
		return domain.BatchRun{}, err
	}
	if item.State != domain.BatchItemFailed && item.State != domain.BatchItemCanceled && item.State != domain.BatchItemQueued {
		return domain.BatchRun{}, fmt.Errorf("%w: 仅失败、已取消或排队中的章节支持重跑", domain.ErrValidation)
	}

	// 并发保护：若所属微批队列仍在后台运行中，拒绝并发重试，避免旧/新协程互相覆盖状态或误删取消句柄。
	r.batchMu.Lock()
	_, active := r.batchCancels[item.BatchRunID]
	r.batchMu.Unlock()
	if active {
		return domain.BatchRun{}, fmt.Errorf("%w: 该微批任务仍在运行中，请先停止当前队列再发起重跑", domain.ErrConflict)
	}

	if err := r.batchStore.UpdateBatchItemState(ctx, item.ID, domain.BatchItemQueued, "", ""); err != nil {
		return domain.BatchRun{}, err
	}
	run, err := r.batchStore.GetBatchRun(ctx, item.BatchRunID)
	if err != nil {
		return domain.BatchRun{}, err
	}
	if err := r.batchStore.UpdateBatchRunStatus(ctx, run.ID, domain.BatchRunRunning, countDoneItems(run.Items)); err != nil {
		return domain.BatchRun{}, err
	}

	if err := r.launchBatchExecution(run.ID, item.ID); err != nil {
		return domain.BatchRun{}, err
	}
	return r.batchStore.GetBatchRun(ctx, run.ID)
}

func (r *Runner) launchBatchExecution(batchID int64, targetItemID int64) error {
	r.batchMu.Lock()
	if r.closed {
		r.batchMu.Unlock()
		return errors.New("pipeline runner is closed")
	}
	if _, exists := r.batchCancels[batchID]; exists {
		r.batchMu.Unlock()
		return fmt.Errorf("%w: 微批任务 #%d 仍在运行中", domain.ErrConflict, batchID)
	}
	batchCtx, cancel := context.WithCancel(context.Background())
	r.batchTokenSeq++
	tok := r.batchTokenSeq
	r.batchCancels[batchID] = activeBatchHandle{
		cancel: cancel,
		token:  tok,
	}
	r.batchWG.Add(1)
	r.batchMu.Unlock()

	go func() {
		defer func() {
			r.batchMu.Lock()
			if cur, ok := r.batchCancels[batchID]; ok && cur.token == tok {
				delete(r.batchCancels, batchID)
			}
			r.batchMu.Unlock()
			cancel()
			r.batchWG.Done()
		}()
		r.executeBatchRun(batchCtx, batchID, targetItemID)
	}()
	return nil
}

func (r *Runner) executeBatchRun(ctx context.Context, batchID int64, retryItemID int64) {
	dbCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.WithoutCancel(ctx), r.saveTimeout)
	}

	sCtx, cancelLoad := dbCtx()
	run, err := r.batchStore.GetBatchRun(sCtx, batchID)
	cancelLoad()
	if err != nil {
		return
	}

	doneCount := countDoneItems(run.Items)
	emitState := func(status domain.BatchRunStatus, it domain.BatchRunItem, state domain.BatchItemState, msg string) {
		r.emitBatchProgress(domain.BatchProgress{
			BatchRunID:        run.ID,
			BookID:            run.BookID,
			Status:            status,
			TotalItems:        run.TotalItems,
			DoneItems:         doneCount,
			CurrentItemID:     it.ID,
			CurrentChapterID:  it.ChapterID,
			CurrentChapterSeq: it.ChapterGlobalSeq,
			CurrentState:      state,
			Message:           msg,
			UpdatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	updateItem := func(it domain.BatchRunItem, state domain.BatchItemState, hash, errMsg, msg string) {
		uCtx, cancel := dbCtx()
		_ = r.batchStore.UpdateBatchItemState(uCtx, it.ID, state, hash, errMsg)
		cancel()
		emitState(domain.BatchRunRunning, it, state, msg)
	}

	for idx, it := range run.Items {
		if it.State == domain.BatchItemAwaitingReview {
			continue
		}
		if retryItemID > 0 && it.ID != retryItemID && it.State != domain.BatchItemQueued {
			continue
		}

		// 检查是否已在当前章开始前被取消。
		if ctx.Err() != nil {
			r.markRemainingCanceled(run, idx, doneCount)
			return
		}

		// 1. compiling（验证编译并计算输入指纹；启用 IncludePending=true 承接前章 pending 状态）
		inputHash := r.computeChapterInputHash(ctx, it.ChapterID)
		updateItem(it, domain.BatchItemCompiling, inputHash, "", fmt.Sprintf("第%d章《%s》编译作战简报…", it.ChapterGlobalSeq, it.ChapterTitle))

		// 2. drafting（串行微批核心：IncludePending=true 让第 N+1 章立刻读取第 N 章刚抽出的 pending 状态）
		updateItem(it, domain.BatchItemDrafting, inputHash, "", fmt.Sprintf("第%d章《%s》流式出稿中…", it.ChapterGlobalSeq, it.ChapterTitle))
		_, genErr := r.GenerateChapter(ctx, it.ChapterID, domain.GenerateChapterOptions{
			Briefing: domain.BriefingOptions{
				IncludePending: true,
			},
		}, nil)
		if genErr != nil {
			if errors.Is(genErr, domain.ErrGenerationCanceled) || ctx.Err() != nil {
				uCtx, cancel := dbCtx()
				_ = r.batchStore.UpdateBatchItemState(uCtx, it.ID, domain.BatchItemCanceled, inputHash, "用户中止生成（已保底保存 partial）")
				cancel()
				r.markRemainingCanceled(run, idx+1, doneCount)
				return
			}
			uCtx, cancel := dbCtx()
			_ = r.batchStore.UpdateBatchItemState(uCtx, it.ID, domain.BatchItemFailed, inputHash, genErr.Error())
			_ = r.batchStore.UpdateBatchRunStatus(uCtx, run.ID, domain.BatchRunFailed, doneCount)
			cancel()
			emitState(domain.BatchRunFailed, it, domain.BatchItemFailed, fmt.Sprintf("第%d章出稿失败：%v", it.ChapterGlobalSeq, genErr))
			return
		}

		// 3~4. 本地扫描 + (draft_and_polish / full_pipeline: Critic 盲审与手术式局部润色)
		if r.styleStore != nil {
			if run.Mode == domain.BatchModeDraftAndPolish || run.Mode == domain.BatchModeFullPipeline {
				_, passErr := r.runPolishPass(ctx, it.ChapterID, true, func(step domain.BatchItemState, msg string) {
					updateItem(it, step, inputHash, "", fmt.Sprintf("第%d章《%s》%s", it.ChapterGlobalSeq, it.ChapterTitle, msg))
				})
				if passErr != nil {
					if errors.Is(passErr, domain.ErrGenerationCanceled) || ctx.Err() != nil {
						r.markRemainingCanceled(run, idx, doneCount)
						return
					}
					uCtx, cancel := dbCtx()
					_ = r.batchStore.UpdateBatchItemState(uCtx, it.ID, domain.BatchItemFailed, inputHash, passErr.Error())
					_ = r.batchStore.UpdateBatchRunStatus(uCtx, run.ID, domain.BatchRunFailed, doneCount)
					cancel()
					emitState(domain.BatchRunFailed, it, domain.BatchItemFailed, fmt.Sprintf("第%d章质检润色失败：%v", it.ChapterGlobalSeq, passErr))
					return
				}
			} else {
				updateItem(it, domain.BatchItemLinting, inputHash, "", fmt.Sprintf("第%d章《%s》本地 AI 味扫描…", it.ChapterGlobalSeq, it.ChapterTitle))
				_, _ = r.LintChapterLocal(ctx, it.ChapterID)
			}
		}

		// 5. full_pipeline：完章抽取 pending 增量变更单（永不静默提交，但允许同批下一章以 IncludePending=true 读取！）
		if run.Mode == domain.BatchModeFullPipeline && r.extractor != nil {
			if ctx.Err() != nil {
				r.markRemainingCanceled(run, idx, doneCount)
				return
			}
			if _, _, extRouteErr := r.resolveStageRouteAndProvider(ctx, domain.StageExtract); extRouteErr == nil {
				updateItem(it, domain.BatchItemExtracting, inputHash, "", fmt.Sprintf("第%d章《%s》抽取待审记忆变更单…", it.ChapterGlobalSeq, it.ChapterTitle))
				if _, extErr := r.extractor.ExtractChapterDelta(ctx, it.ChapterID); extErr != nil {
					if errors.Is(extErr, domain.ErrGenerationCanceled) || ctx.Err() != nil {
						r.markRemainingCanceled(run, idx, doneCount)
						return
					}
					// 按 docs/05 §5 风险缓解策略：抽取解析失败时不损毁已生成的正文，标记错误或记录提醒继续进入待审。
					uCtx, cancel := dbCtx()
					_ = r.batchStore.UpdateBatchItemState(uCtx, it.ID, domain.BatchItemAwaitingReview, inputHash, fmt.Sprintf("正文已生成，记忆抽取失败待手动补充：%v", extErr))
					doneCount++
					_ = r.batchStore.UpdateBatchRunStatus(uCtx, run.ID, domain.BatchRunRunning, doneCount)
					cancel()
					emitState(domain.BatchRunRunning, it, domain.BatchItemAwaitingReview, fmt.Sprintf("第%d章进入待审（记忆抽取失败待手动补充）", it.ChapterGlobalSeq))
					continue
				}
			}
		}

		// 6. awaiting_review
		doneCount++
		uCtx, cancel := dbCtx()
		_ = r.batchStore.UpdateBatchItemState(uCtx, it.ID, domain.BatchItemAwaitingReview, inputHash, "")
		_ = r.batchStore.UpdateBatchRunStatus(uCtx, run.ID, domain.BatchRunRunning, doneCount)
		cancel()
		emitState(domain.BatchRunRunning, it, domain.BatchItemAwaitingReview, fmt.Sprintf("第%d章《%s》已就绪待审（%d/%d）", it.ChapterGlobalSeq, it.ChapterTitle, doneCount, run.TotalItems))
	}

	// 汇总全部子项状态确定最终队列状态。
	fCtx, cancelFinal := dbCtx()
	latestRun, err := r.batchStore.GetBatchRun(fCtx, run.ID)
	cancelFinal()
	if err != nil {
		return
	}
	finalDone := countDoneItems(latestRun.Items)
	finalStatus := domain.BatchRunCompleted
	for _, it := range latestRun.Items {
		if it.State == domain.BatchItemFailed {
			finalStatus = domain.BatchRunFailed
			break
		}
		if it.State == domain.BatchItemCanceled {
			finalStatus = domain.BatchRunCanceled
		}
	}
	doneCount = finalDone

	uCtx, cancelUpdate := dbCtx()
	_ = r.batchStore.UpdateBatchRunStatus(uCtx, run.ID, finalStatus, finalDone)
	cancelUpdate()

	var lastItem domain.BatchRunItem
	if len(latestRun.Items) > 0 {
		lastItem = latestRun.Items[len(latestRun.Items)-1]
	}
	emitState(finalStatus, lastItem, lastItem.State, fmt.Sprintf("串行微批任务结束：%d/%d 章待审", finalDone, latestRun.TotalItems))
}

func (r *Runner) markRemainingCanceled(run domain.BatchRun, fromIdx int, doneCount int) {
	uCtx, cancel := context.WithTimeout(context.Background(), r.saveTimeout)
	defer cancel()
	for i := fromIdx; i < len(run.Items); i++ {
		if run.Items[i].State != domain.BatchItemAwaitingReview && run.Items[i].State != domain.BatchItemFailed {
			_ = r.batchStore.UpdateBatchItemState(uCtx, run.Items[i].ID, domain.BatchItemCanceled, "", "用户中止微批队列")
		}
	}
	_ = r.batchStore.UpdateBatchRunStatus(uCtx, run.ID, domain.BatchRunCanceled, doneCount)
	var curItem domain.BatchRunItem
	if fromIdx < len(run.Items) {
		curItem = run.Items[fromIdx]
	}
	r.emitBatchProgress(domain.BatchProgress{
		BatchRunID:        run.ID,
		BookID:            run.BookID,
		Status:            domain.BatchRunCanceled,
		TotalItems:        run.TotalItems,
		DoneItems:         doneCount,
		CurrentItemID:     curItem.ID,
		CurrentChapterID:  curItem.ChapterID,
		CurrentChapterSeq: curItem.ChapterGlobalSeq,
		CurrentState:      domain.BatchItemCanceled,
		Message:           "微批队列已安全中止",
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (r *Runner) emitBatchProgress(p domain.BatchProgress) {
	if r != nil && r.progressListener != nil {
		r.progressListener(p)
	}
}

func (r *Runner) computeChapterInputHash(ctx context.Context, chapterID int64) string {
	ch, err := r.store.GetChapter(ctx, chapterID)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s|%s|%s", ch.ID, ch.Title, ch.EmotionTag, ch.HookType, ch.OutlineSummary, ch.UpdatedAt)))
	return hex.EncodeToString(sum[:8])
}

func countDoneItems(items []domain.BatchRunItem) int {
	n := 0
	for _, it := range items {
		if it.State == domain.BatchItemAwaitingReview {
			n++
		}
	}
	return n
}

func buildBatchOutlinePrompts(
	book domain.Book,
	vol domain.Volume,
	existingChapters []domain.Chapter,
	startSeq int,
	count int,
	arcGoal string,
) (string, string) {
	preset := genres.GetOrDefault(book.Genre)

	sys := `你是男频长篇网文资深大纲与节奏编排引擎。请严格遵循男频“立靶—蓄压—反转—爆发—落袋留钩”循环节奏，避免连续 3 章纯压抑（suppress）或连续 5 章无高潮爆发（burst），一次性输出指定数量的结构化章纲与节拍卡。
严格只输出一个合法 JSON 对象（禁止输出 Markdown 围栏或多余说明）：
{
  "outlines": [
    {
      "title": "精炼网文章节名（不含第X章前缀）",
      "emotion_tag": "setup|suppress|turn|burst|harvest",
      "hook_type": "mystery|crisis|counterattack_eve|reward_pending|new_arc",
      "hook_note": "章末200字具体悬念落点",
      "outline_summary": "80~150字本章主线推进摘要",
      "beats": [
        {
          "stage": "setup|target|suppress|turn|burst|harvest|hook",
          "summary": "该节拍具体发生的情节与推进",
          "conflict_note": "冲突筹码或信息差要点",
          "target_words": 500
        }
      ]
    }
  ]
}`

	var user strings.Builder
	fmt.Fprintf(&user, "【书籍设定】《%s》｜题材：%s（%s）\n", book.Title, preset.Name, preset.LoopCycleChapters)
	if strings.TrimSpace(book.Premise) != "" {
		fmt.Fprintf(&user, "核心卖点：%s\n", strings.TrimSpace(book.Premise))
	}
	if strings.TrimSpace(book.GoldenFinger) != "" {
		fmt.Fprintf(&user, "金手指：%s\n", strings.TrimSpace(book.GoldenFinger))
	}
	fmt.Fprintf(&user, "节奏侧重：%s\n", preset.PacingFocus)
	fmt.Fprintf(&user, "当前卷：第%d卷《%s》｜卷目标：%s｜卷高潮规划：%s\n", vol.Seq, vol.Title, vol.Goal, vol.ClimaxPlan)

	// 附带最近 10 章前文因果摘要避免剧情断档。
	var recent []string
	for _, ch := range existingChapters {
		if ch.GlobalSeq < startSeq && strings.TrimSpace(ch.CausalSummary) != "" {
			recent = append(recent, fmt.Sprintf("- 第%d章《%s》：%s", ch.GlobalSeq, ch.Title, strings.TrimSpace(ch.CausalSummary)))
		}
	}
	if len(recent) > 10 {
		recent = recent[len(recent)-10:]
	}
	if len(recent) > 0 {
		user.WriteString("\n【前文近章因果衔接】\n")
		user.WriteString(strings.Join(recent, "\n"))
		user.WriteString("\n")
	}

	fmt.Fprintf(&user, "\n【本轮批量编排任务】\n请从第 %d 章起连续规划 %d 章（第 %d~%d 章），每章包含 3~5 张节拍卡。\n", startSeq, count, startSeq, startSeq+count-1)
	if strings.TrimSpace(arcGoal) != "" {
		fmt.Fprintf(&user, "本单元小高潮与走势要求：%s\n", strings.TrimSpace(arcGoal))
	}
	return sys, user.String()
}

func parseBatchOutlinesPayload(raw string, expectedCount int) ([]domain.GeneratedChapterOutline, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty batch outline response")
	}

	type rawBeatDTO struct {
		Stage             string `json:"stage"`
		Summary           string `json:"summary"`
		ConflictNote      string `json:"conflict_note"`
		ConflictNoteCamel string `json:"conflictNote"`
		TargetWords       int    `json:"target_words"`
		TargetWordsCamel  int    `json:"targetWords"`
	}
	type rawOutlineDTO struct {
		Title          string       `json:"title"`
		EmotionTag     string       `json:"emotion_tag"`
		EmotionCamel   string       `json:"emotionTag"`
		HookType       string       `json:"hook_type"`
		HookTypeCamel  string       `json:"hookType"`
		HookNote       string       `json:"hook_note"`
		HookNoteCamel  string       `json:"hookNote"`
		OutlineSummary string       `json:"outline_summary"`
		OutlineCamel   string       `json:"outlineSummary"`
		Beats          []rawBeatDTO `json:"beats"`
	}

	var rawList []rawOutlineDTO

	if jsonStr, err := llm.ExtractJSONObject(trimmed); err == nil {
		var wrap struct {
			Outlines []rawOutlineDTO `json:"outlines"`
			Chapters []rawOutlineDTO `json:"chapters"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &wrap); err != nil {
			return nil, fmt.Errorf("unmarshal batch outlines object: %w", err)
		}
		rawList = wrap.Outlines
		if len(rawList) == 0 {
			rawList = wrap.Chapters
		}
	} else if arrStr, arrErr := llm.ExtractJSONArray(trimmed); arrErr == nil {
		// 兼容模型直接返回顶层 JSON 数组 [...]
		if err := json.Unmarshal([]byte(arrStr), &rawList); err != nil {
			return nil, fmt.Errorf("unmarshal batch outlines array: %w", err)
		}
	}

	if len(rawList) == 0 {
		return nil, errors.New("JSON missing non-empty outlines array")
	}
	if expectedCount > 0 && len(rawList) > expectedCount {
		rawList = rawList[:expectedCount]
	}

	out := make([]domain.GeneratedChapterOutline, 0, len(rawList))
	for i, item := range rawList {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = fmt.Sprintf("待定章节 %d", i+1)
		}
		emotion := domain.EmotionTag(strings.ToLower(strings.TrimSpace(item.EmotionTag)))
		if !emotion.Valid() {
			emotion = domain.EmotionTag(strings.ToLower(strings.TrimSpace(item.EmotionCamel)))
		}
		if !emotion.Valid() {
			emotion = domain.EmotionSetup
		}
		hookType := domain.HookType(strings.ToLower(strings.TrimSpace(item.HookType)))
		if !hookType.Valid() {
			hookType = domain.HookType(strings.ToLower(strings.TrimSpace(item.HookTypeCamel)))
		}
		if !hookType.Valid() {
			hookType = domain.HookMystery
		}
		hookNote := strings.TrimSpace(item.HookNote)
		if hookNote == "" {
			hookNote = strings.TrimSpace(item.HookNoteCamel)
		}
		summary := strings.TrimSpace(item.OutlineSummary)
		if summary == "" {
			summary = strings.TrimSpace(item.OutlineCamel)
		}

		beats := make([]domain.Beat, 0, len(item.Beats))
		for _, b := range item.Beats {
			bSum := strings.TrimSpace(b.Summary)
			if bSum == "" {
				continue
			}
			st := domain.BeatStage(strings.ToLower(strings.TrimSpace(b.Stage)))
			if !st.Valid() {
				st = domain.BeatStageSetup
			}
			conflict := strings.TrimSpace(b.ConflictNote)
			if conflict == "" {
				conflict = strings.TrimSpace(b.ConflictNoteCamel)
			}
			tw := b.TargetWords
			if tw <= 0 {
				tw = b.TargetWordsCamel
			}
			if tw <= 0 {
				tw = domain.DefaultBeatTargetWords
			}
			beats = append(beats, domain.Beat{
				Seq:          len(beats) + 1,
				Stage:        st,
				Summary:      bSum,
				ConflictNote: conflict,
				EntityIDs:    []int64{},
				TargetWords:  tw,
			})
		}

		out = append(out, domain.GeneratedChapterOutline{
			Title:          title,
			EmotionTag:     emotion,
			HookType:       hookType,
			HookNote:       hookNote,
			OutlineSummary: summary,
			Beats:          beats,
		})
	}
	return out, nil
}
