package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nib/internal/domain"
)

const (
	autopilotMaxLogEntries = 80
	autopilotRetryDelay    = 1200 * time.Millisecond
)

// AutopilotRunStore 定义全自动托管持久化接口（由 *store.Store 自动满足）。
type AutopilotRunStore interface {
	CreateAutopilotRun(ctx context.Context, state domain.AutopilotState) (int64, error)
	UpdateAutopilotRun(ctx context.Context, state domain.AutopilotState) error
	GetLatestAutopilotRun(ctx context.Context, bookID int64) (*domain.AutopilotState, error)
}

// AutopilotExtraStore 聚合托管流水线所需的卷、实体与记忆变更落库接口（由 *store.Store 自动满足）。
type AutopilotExtraStore interface {
	ListVolumes(ctx context.Context, bookID int64) ([]domain.Volume, error)
	UpsertVolume(ctx context.Context, in domain.UpsertVolumeInput) (domain.Volume, error)
	ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error)
	ListBeats(ctx context.Context, chapterID int64) ([]domain.Beat, error)
	ApplyDeltaDecisions(ctx context.Context, in domain.ApplyDeltaInput) (domain.ApplyDeltaResult, error)
}

type autopilotSession struct {
	mu       sync.Mutex
	state    domain.AutopilotState
	cancel   context.CancelFunc
	gateCh   chan domain.AutopilotDecisionAction
	active   bool
}

// WithAutopilotListener 注入全自动托管实时状态推送回调（生产环境接入 app.EmitAutopilotProgress）。
func WithAutopilotListener(fn func(domain.AutopilotState)) Option {
	return func(r *Runner) {
		if fn != nil {
			r.autopilotListener = fn
		}
	}
}

// GetAutopilotState 返回指定书籍当前或最近一次托管流水线的状态快照（内存中不存在时自动从 SQLite 恢复）。
func (r *Runner) GetAutopilotState(bookID int64) domain.AutopilotState {
	if r == nil || bookID <= 0 {
		return domain.AutopilotState{BookID: bookID, Status: domain.AutopilotStatusIdle}.Normalized()
	}
	r.autopilotMu.Lock()
	sess := r.autopilots[bookID]
	r.autopilotMu.Unlock()
	if sess != nil {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return cloneAutopilotState(sess.state)
	}
	if rs, ok := r.store.(AutopilotRunStore); ok {
		if persisted, err := rs.GetLatestAutopilotRun(context.Background(), bookID); err == nil && persisted != nil {
			return persisted.Normalized()
		}
	}
	return domain.AutopilotState{BookID: bookID, Status: domain.AutopilotStatusIdle}.Normalized()
}

// ResumeAutopilot 从最近一次中断或停止的托管记录继续执行剩余章节。
func (r *Runner) ResumeAutopilot(ctx context.Context, bookID int64) (domain.AutopilotState, error) {
	if r == nil || bookID <= 0 {
		return domain.AutopilotState{}, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	prev := r.GetAutopilotState(bookID)
	if prev.Status == domain.AutopilotStatusRunning || prev.Status == domain.AutopilotStatusWaitingDecision {
		return prev, fmt.Errorf("%w: 该书托管任务正在运行中", domain.ErrConflict)
	}
	remaining := prev.TargetChapters - prev.CompletedChapters
	if remaining <= 0 {
		return domain.AutopilotState{}, fmt.Errorf("%w: 最近一次托管已完成全部目标章节，请开启新任务", domain.ErrValidation)
	}
	remainingBudget := 0
	if prev.MaxTokensBudget > 0 {
		remainingBudget = prev.MaxTokensBudget - prev.TokensUsed
		if remainingBudget <= 0 {
			return domain.AutopilotState{}, fmt.Errorf("%w: 原托管任务的 Token 预算已用尽，请调整预算后开启新任务", domain.ErrValidation)
		}
	}
	return r.StartAutopilot(ctx, domain.AutopilotRequest{
		BookID:           bookID,
		VolumeID:         prev.VolumeID,
		TargetChapters:   remaining,
		AutoOutline:      prev.AutoOutline,
		AutoPolish:       prev.AutoPolish,
		AutoCommitMemory: prev.AutoCommitMemory,
		MaxTokensBudget:  remainingBudget,
		ArcGoal:          prev.ArcGoal,
	})
}

// StartAutopilot 为指定书籍启动全自动托管流水线（自动补纲 → 写稿 → 质检润色 → 自动记忆入库 → 下一章）。
func (r *Runner) StartAutopilot(ctx context.Context, req domain.AutopilotRequest) (domain.AutopilotState, error) {
	if r == nil || r.store == nil || r.compiler == nil || r.llm == nil {
		return domain.AutopilotState{}, errors.New("pipeline autopilot dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.AutopilotState{}, err
	}
	if req.TargetChapters <= 0 {
		req.TargetChapters = domain.DefaultAutopilotChapters
	}
	// 提前校验书籍与 draft 路由存在，尽早向用户提示配置问题。
	if _, err := r.store.GetBook(ctx, req.BookID); err != nil {
		return domain.AutopilotState{}, fmt.Errorf("load book %d: %w", req.BookID, err)
	}
	if _, _, err := r.resolveDraftRouteAndProvider(ctx); err != nil {
		return domain.AutopilotState{}, err
	}

	r.autopilotMu.Lock()
	if existing, ok := r.autopilots[req.BookID]; ok {
		existing.mu.Lock()
		isRunning := existing.active
		existing.mu.Unlock()
		if isRunning {
			r.autopilotMu.Unlock()
			return domain.AutopilotState{}, fmt.Errorf("%w: 该书已有运行中的托管任务", domain.ErrConflict)
		}
	}

	runCtx, cancel := context.WithCancel(context.Background())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sess := &autopilotSession{
		cancel: cancel,
		gateCh: make(chan domain.AutopilotDecisionAction, 1),
		active: true,
		state: domain.AutopilotState{
			BookID:           req.BookID,
			VolumeID:         req.VolumeID,
			Status:           domain.AutopilotStatusRunning,
			Step:             domain.AutopilotStepPreparing,
			TargetChapters:   req.TargetChapters,
			AutoOutline:      req.AutoOutline,
			AutoPolish:       req.AutoPolish,
			MaxTokensBudget:  req.MaxTokensBudget,
			AutoCommitMemory: req.AutoCommitMemory,
			ArcGoal:          strings.TrimSpace(req.ArcGoal),
			Message:          fmt.Sprintf("已启动托管，计划自动推进 %d 章", req.TargetChapters),
			Logs: []domain.AutopilotLogEntry{
				{
					Time:    now,
					Step:    domain.AutopilotStepPreparing,
					Tone:    "info",
					Message: fmt.Sprintf("开始全自动托管（目标 %d 章）", req.TargetChapters),
				},
			},
			StartedAt: now,
			UpdatedAt: now,
		},
	}
	if rs, ok := r.store.(AutopilotRunStore); ok {
		if runID, cErr := rs.CreateAutopilotRun(context.Background(), sess.state); cErr == nil && runID > 0 {
			sess.state.RunID = runID
		}
	}
	r.autopilots[req.BookID] = sess
	r.batchWG.Add(1)
	r.autopilotMu.Unlock()

	initial := cloneAutopilotState(sess.state)
	r.emitAutopilotProgress(initial)

	go func() {
		defer func() {
			sess.mu.Lock()
			sess.active = false
			sess.mu.Unlock()
			cancel()
			r.batchWG.Done()
		}()
		r.runAutopilotLoop(runCtx, sess, req)
	}()

	return initial, nil
}

// StopAutopilot 请求平滑中止指定书籍正在运行或等待决策的托管任务。
func (r *Runner) StopAutopilot(bookID int64) (domain.AutopilotState, error) {
	if r == nil || bookID <= 0 {
		return domain.AutopilotState{}, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	r.autopilotMu.Lock()
	sess := r.autopilots[bookID]
	r.autopilotMu.Unlock()
	if sess == nil {
		return domain.AutopilotState{BookID: bookID, Status: domain.AutopilotStatusIdle}.Normalized(), nil
	}
	sess.mu.Lock()
	cancel := sess.cancel
	wasActive := sess.active
	if wasActive {
		sess.state.Message = "正在停止托管（当前章已写内容会保底保存）…"
		sess.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	snap := cloneAutopilotState(sess.state)
	sess.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if wasActive {
		select {
		case sess.gateCh <- domain.AutopilotDecisionStop:
		default:
		}
		r.emitAutopilotProgress(snap)
	}
	return snap, nil
}

// ResolveAutopilotGate 在托管暂停于决策闸门时接收作者选择并恢复推进。
func (r *Runner) ResolveAutopilotGate(bookID int64, action domain.AutopilotDecisionAction) (domain.AutopilotState, error) {
	if !action.Valid() {
		return domain.AutopilotState{}, fmt.Errorf("%w: 无效的决策操作", domain.ErrValidation)
	}
	r.autopilotMu.Lock()
	sess := r.autopilots[bookID]
	r.autopilotMu.Unlock()
	if sess == nil {
		return domain.AutopilotState{}, fmt.Errorf("%w: 当前没有等待决策的托管任务", domain.ErrNotFound)
	}
	sess.mu.Lock()
	if !sess.active || sess.state.Status != domain.AutopilotStatusWaitingDecision {
		sess.mu.Unlock()
		return domain.AutopilotState{}, fmt.Errorf("%w: 当前托管任务未处于等待决策状态", domain.ErrConflict)
	}
	sess.state.Gate = nil
	sess.state.Status = domain.AutopilotStatusRunning
	sess.state.Message = "已收到指令，恢复推进…"
	sess.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	snap := cloneAutopilotState(sess.state)
	sess.mu.Unlock()

	select {
	case sess.gateCh <- action:
	default:
	}
	r.emitAutopilotProgress(snap)
	return snap, nil
}

func (r *Runner) runAutopilotLoop(ctx context.Context, sess *autopilotSession, req domain.AutopilotRequest) {
	extraStore, _ := r.store.(AutopilotExtraStore)

	runStore, _ := r.store.(AutopilotRunStore)

	update := func(fn func(*domain.AutopilotState)) {
		sess.mu.Lock()
		fn(&sess.state)
		sess.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		snap := cloneAutopilotState(sess.state)
		sess.mu.Unlock()
		if runStore != nil && snap.RunID > 0 {
			_ = runStore.UpdateAutopilotRun(context.Background(), snap)
		}
		r.emitAutopilotProgress(snap)
	}
	addLog := func(seq int, step domain.AutopilotStep, tone, msg string) {
		update(func(s *domain.AutopilotState) {
			s.Step = step
			s.Message = msg
			s.Logs = append(s.Logs, domain.AutopilotLogEntry{
				Time:       time.Now().UTC().Format(time.RFC3339Nano),
				ChapterSeq: seq,
				Step:       step,
				Tone:       tone,
				Message:    msg,
			})
			if len(s.Logs) > autopilotMaxLogEntries {
				s.Logs = s.Logs[len(s.Logs)-autopilotMaxLogEntries:]
			}
		})
	}
	waitGate := func(gate domain.AutopilotGate) domain.AutopilotDecisionAction {
		update(func(s *domain.AutopilotState) {
			s.Status = domain.AutopilotStatusWaitingDecision
			s.Step = domain.AutopilotStepWaitingDecision
			gCopy := gate
			s.Gate = &gCopy
			s.Message = gate.Title
			s.Logs = append(s.Logs, domain.AutopilotLogEntry{
				Time:       time.Now().UTC().Format(time.RFC3339Nano),
				ChapterSeq: gate.ChapterSeq,
				Step:       domain.AutopilotStepWaitingDecision,
				Tone:       "warn",
				Message:    gate.Title + "：" + gate.Detail,
			})
		})
		select {
		case <-ctx.Done():
			return domain.AutopilotDecisionStop
		case act := <-sess.gateCh:
			return act
		}
	}

	// 1. 解析目标卷（若全书尚无卷，自动创建第一卷）。
	targetVol, err := r.ensureAutopilotVolume(ctx, req.BookID, req.VolumeID, extraStore)
	if err != nil {
		update(func(s *domain.AutopilotState) {
			s.Status = domain.AutopilotStatusFailed
			s.Error = err.Error()
			s.Message = "准备卷结构失败：" + err.Error()
		})
		return
	}

	book, err := r.store.GetBook(ctx, req.BookID)
	if err != nil {
		update(func(s *domain.AutopilotState) {
			s.Status = domain.AutopilotStatusFailed
			s.Error = err.Error()
			s.Message = "读取书籍失败：" + err.Error()
		})
		return
	}

	completed := 0
	skippedIDs := make(map[int64]bool)

	for completed < req.TargetChapters {
		if ctx.Err() != nil {
			update(func(s *domain.AutopilotState) {
				s.Status = domain.AutopilotStatusStopped
				s.Gate = nil
				s.Message = fmt.Sprintf("已停止托管（已完成 %d / %d 章）", completed, req.TargetChapters)
			})
			return
		}

		// 检查 Token 预算上限
		if req.MaxTokensBudget > 0 {
			sess.mu.Lock()
			used := sess.state.TokensUsed
			sess.mu.Unlock()
			if used >= req.MaxTokensBudget {
				addLog(0, domain.AutopilotStepDone, "warn", fmt.Sprintf("已达本次 Token 上限（%d），自动停止托管", req.MaxTokensBudget))
				update(func(s *domain.AutopilotState) {
					s.Status = domain.AutopilotStatusCompleted
					s.Step = domain.AutopilotStepDone
				})
				return
			}
		}

		// 2. 寻找下一章待写章节；不足时自动续写章纲。
		chap, err := r.findOrOutlineNextChapter(ctx, req, targetVol, req.TargetChapters-completed, skippedIDs, extraStore, addLog)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrGenerationCanceled) || ctx.Err() != nil {
				update(func(s *domain.AutopilotState) {
					s.Status = domain.AutopilotStatusStopped
					s.Gate = nil
					s.Message = fmt.Sprintf("已停止托管（已完成 %d / %d 章）", completed, req.TargetChapters)
				})
				return
			}
			update(func(s *domain.AutopilotState) {
				s.Status = domain.AutopilotStatusFailed
				s.Error = err.Error()
				s.Message = "准备下一章失败：" + err.Error()
			})
			return
		}
		if chap == nil {
			addLog(0, domain.AutopilotStepDone, "info", "当前卷已无更多待写章节，且未开启自动续写章纲")
			update(func(s *domain.AutopilotState) {
				s.Status = domain.AutopilotStatusCompleted
				s.Step = domain.AutopilotStepDone
			})
			return
		}

		update(func(s *domain.AutopilotState) {
			s.CurrentChapterID = chap.ID
			s.CurrentChapterSeq = chap.GlobalSeq
			s.CurrentChapterTitle = chap.Title
		})

		// 3. 单章执行（支持在决策闸门选择重试本章）
		chapterDone := false
		for !chapterDone {
			if ctx.Err() != nil {
				update(func(s *domain.AutopilotState) {
					s.Status = domain.AutopilotStatusStopped
					s.Gate = nil
					s.Message = fmt.Sprintf("已停止托管（已完成 %d / %d 章）", completed, req.TargetChapters)
				})
				return
			}

			addLog(chap.GlobalSeq, domain.AutopilotStepDrafting, "info", fmt.Sprintf("第 %d 章《%s》正在写正文…", chap.GlobalSeq, chap.Title))
			var chapterTokens int
			savedChap, draftErr := r.draftChapterWithSelfHeal(ctx, chap.ID, func(usage domain.TokenUsage) {
				chapterTokens = usage.TotalTokens
			})
			if chapterTokens > 0 {
				update(func(s *domain.AutopilotState) {
					s.TokensUsed += chapterTokens
				})
			}

			if draftErr != nil {
				if errors.Is(draftErr, domain.ErrGenerationCanceled) || errors.Is(draftErr, context.Canceled) || ctx.Err() != nil {
					update(func(s *domain.AutopilotState) {
						s.Status = domain.AutopilotStatusStopped
						s.Gate = nil
						s.Message = fmt.Sprintf("已停止托管，第 %d 章已保底保存", chap.GlobalSeq)
					})
					return
				}
				decision := waitGate(domain.AutopilotGate{
					Reason:     "draft_error",
					Title:      fmt.Sprintf("第 %d 章写稿遇到错误", chap.GlobalSeq),
					Detail:     draftErr.Error(),
					ChapterID:  chap.ID,
					ChapterSeq: chap.GlobalSeq,
					Options: []domain.AutopilotGateOption{
						{Action: domain.AutopilotDecisionRetryChapter, Label: "重试本章", Description: "重新生成第 " + fmt.Sprintf("%d", chap.GlobalSeq) + " 章"},
						{Action: domain.AutopilotDecisionSkipChapter, Label: "跳过本章", Description: "保留现状并继续下一章"},
						{Action: domain.AutopilotDecisionStop, Label: "停止托管", Description: "暂停后续任务，由你手动接管"},
					},
				})
				switch decision {
				case domain.AutopilotDecisionRetryChapter:
					continue
				case domain.AutopilotDecisionSkipChapter:
					skippedIDs[chap.ID] = true
					chapterDone = true
					continue
				default:
					update(func(s *domain.AutopilotState) {
						s.Status = domain.AutopilotStatusStopped
						s.Gate = nil
						s.Message = "已停止托管"
					})
					return
				}
			}

			// 字数异常截断检测（不足目标字数 35% 且目标字数 >= 1000 时触发决策闸门）
			if book.TargetChapterWords >= 1000 && savedChap.WordCount > 0 && savedChap.WordCount*100 < book.TargetChapterWords*35 {
				decision := waitGate(domain.AutopilotGate{
					Reason:     "word_count_low",
					Title:      fmt.Sprintf("第 %d 章字数明显偏短（%d / %d 字）", chap.GlobalSeq, savedChap.WordCount, book.TargetChapterWords),
					Detail:     "模型可能提前截断，你可以选择重写本章，或保留当前正文继续后续润色与下一章。",
					ChapterID:  chap.ID,
					ChapterSeq: chap.GlobalSeq,
					Options: []domain.AutopilotGateOption{
						{Action: domain.AutopilotDecisionRetryChapter, Label: "重写本章", Description: "存档当前版本后重新生成"},
						{Action: domain.AutopilotDecisionContinue, Label: "保留并继续", Description: "继续润色、更新设定并推进下一章"},
						{Action: domain.AutopilotDecisionStop, Label: "停止托管", Description: "停在这里由你手动修改"},
					},
				})
				switch decision {
				case domain.AutopilotDecisionRetryChapter:
					continue
				case domain.AutopilotDecisionStop:
					update(func(s *domain.AutopilotState) {
						s.Status = domain.AutopilotStatusStopped
						s.Gate = nil
						s.Message = "已停止托管"
					})
					return
				}
			}

			addLog(chap.GlobalSeq, domain.AutopilotStepLinting, "ok", fmt.Sprintf("第 %d 章成稿 %d 字", chap.GlobalSeq, savedChap.WordCount))

			// 4. 自动质检与手术式局部润色
			if req.AutoPolish && r.styleStore != nil && ctx.Err() == nil {
				var latestAudit domain.ChapterAudit
				if audit, lintErr := r.LintChapterLocal(ctx, chap.ID); lintErr == nil {
					latestAudit = audit
				}
				if _, _, routeErr := r.resolveStageRouteAndProvider(ctx, domain.StageCritic); routeErr == nil && ctx.Err() == nil {
					addLog(chap.GlobalSeq, domain.AutopilotStepCritiquing, "info", fmt.Sprintf("第 %d 章正在进行 AI 审校…", chap.GlobalSeq))
					if criticAudit, cErr := r.RunCriticAudit(ctx, chap.ID); cErr == nil {
						latestAudit = criticAudit
					} else if ctx.Err() == nil {
						addLog(chap.GlobalSeq, domain.AutopilotStepCritiquing, "warn", "AI 审校跳过："+cErr.Error())
					}
				}
				if (len(latestAudit.LintIssues) > 0 || len(latestAudit.CriticFindings) > 0) && ctx.Err() == nil {
					if _, _, pErr := r.resolveStageRouteAndProvider(ctx, domain.StagePolish); pErr == nil {
						addLog(chap.GlobalSeq, domain.AutopilotStepPolishing, "info", fmt.Sprintf("第 %d 章正在局部润色…", chap.GlobalSeq))
						if diffAudit, dErr := r.GenerateSurgicalDiffs(ctx, chap.ID, nil); dErr == nil {
							applied := 0
							for _, d := range diffAudit.Diffs {
								if d.Status == domain.DiffStatusPending {
									if _, aErr := r.ApplySurgicalDiff(ctx, domain.ApplySurgicalDiffInput{
										ChapterID: chap.ID,
										DiffID:    d.ID,
										Accept:    true,
									}); aErr == nil {
										applied++
									}
								}
							}
							if applied > 0 {
								addLog(chap.GlobalSeq, domain.AutopilotStepPolishing, "ok", fmt.Sprintf("第 %d 章已自动润色 %d 处段落（已存修改前快照）", chap.GlobalSeq, applied))
							}
						} else if ctx.Err() == nil {
							addLog(chap.GlobalSeq, domain.AutopilotStepPolishing, "warn", "局部润色跳过："+dErr.Error())
						}
					}
				}
			}

			// 5. 自动记忆抽取 + 1.A 自动入库生效（保留完整审计记录）
			if r.extractor != nil && ctx.Err() == nil {
				if _, _, extRouteErr := r.resolveStageRouteAndProvider(ctx, domain.StageExtract); extRouteErr == nil {
					addLog(chap.GlobalSeq, domain.AutopilotStepExtracting, "info", fmt.Sprintf("第 %d 章正在抽取设定与伏笔更新…", chap.GlobalSeq))
					delta, extErr := r.extractor.ExtractChapterDelta(ctx, chap.ID)
					if extErr != nil {
						if ctx.Err() == nil {
							addLog(chap.GlobalSeq, domain.AutopilotStepExtracting, "warn", "记忆抽取跳过："+extErr.Error())
						}
					} else if req.AutoCommitMemory && extraStore != nil {
						addLog(chap.GlobalSeq, domain.AutopilotStepCommitting, "info", fmt.Sprintf("第 %d 章自动同步设定与伏笔…", chap.GlobalSeq))
						if applyRes, appErr := extraStore.ApplyDeltaDecisions(ctx, buildAutoAcceptDeltaInput(delta)); appErr == nil {
							addLog(chap.GlobalSeq, domain.AutopilotStepCommitting, "ok", fmt.Sprintf("第 %d 章已同步记忆（%d 条状态变化，%d 条伏笔更新）", chap.GlobalSeq, applyRes.CommittedEvents, applyRes.UpdatedForeshadowings))
						} else {
							addLog(chap.GlobalSeq, domain.AutopilotStepCommitting, "warn", "自动同步记忆失败（保留在待审列表）："+appErr.Error())
						}
					} else {
						addLog(chap.GlobalSeq, domain.AutopilotStepExtracting, "ok", fmt.Sprintf("第 %d 章记忆变更已写入待审列表", chap.GlobalSeq))
					}
				}
			}

			// 6. 长篇记忆金字塔自动滚动：每满 10 章自动生成一次 10 章弧线摘要，防止早期剧情滑出 P5 窗口。
			if ctx.Err() == nil && chap.GlobalSeq > 0 && chap.GlobalSeq%10 == 0 {
				if generatedArc, arcErr := r.maybeAutoRollArcSummary(ctx, req.BookID, chap.GlobalSeq); arcErr == nil && generatedArc {
					addLog(chap.GlobalSeq, domain.AutopilotStepCommitting, "ok", fmt.Sprintf("已自动归档第 %d~%d 章 10 章弧线摘要", chap.GlobalSeq-9, chap.GlobalSeq))
				}
			}

			completed++
			chapterDone = true
			update(func(s *domain.AutopilotState) {
				s.CompletedChapters = completed
			})
		}
	}

	addLog(0, domain.AutopilotStepDone, "ok", fmt.Sprintf("托管完成：已连续推进 %d 章", completed))
	update(func(s *domain.AutopilotState) {
		s.Status = domain.AutopilotStatusCompleted
		s.Step = domain.AutopilotStepDone
		s.Gate = nil
	})
}

func (r *Runner) draftChapterWithSelfHeal(
	ctx context.Context,
	chapterID int64,
	onUsage func(domain.TokenUsage),
) (domain.Chapter, error) {
	opts := domain.GenerateChapterOptions{
		Briefing: domain.BriefingOptions{
			IncludePending: true,
		},
	}
	var lastUsage domain.TokenUsage
	chunkCb := func(chunk domain.GenChunk) error {
		if chunk.Usage != nil {
			lastUsage = *chunk.Usage
		}
		return nil
	}

	chap, err := r.GenerateChapter(ctx, chapterID, opts, chunkCb)
	if err == nil {
		if onUsage != nil {
			onUsage(lastUsage)
		}
		return chap, nil
	}
	if errors.Is(err, domain.ErrGenerationCanceled) || errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrValidation) || ctx.Err() != nil {
		return chap, err
	}

	// 瞬时错误自动退避重试 1 次
	select {
	case <-ctx.Done():
		return chap, domain.ErrGenerationCanceled
	case <-time.After(autopilotRetryDelay):
	}
	chap, err = r.GenerateChapter(ctx, chapterID, opts, chunkCb)
	if onUsage != nil && lastUsage.TotalTokens > 0 {
		onUsage(lastUsage)
	}
	return chap, err
}

func (r *Runner) ensureAutopilotVolume(
	ctx context.Context,
	bookID int64,
	volumeID int64,
	extra AutopilotExtraStore,
) (domain.Volume, error) {
	if volumeID > 0 {
		return r.store.GetVolume(ctx, volumeID)
	}
	if extra == nil {
		return domain.Volume{}, errors.New("store does not support listing volumes")
	}
	vols, err := extra.ListVolumes(ctx, bookID)
	if err != nil {
		return domain.Volume{}, err
	}
	if len(vols) > 0 {
		return vols[len(vols)-1], nil
	}
	return extra.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: bookID,
		Seq:    1,
		Title:  "第一卷 初露锋芒",
	})
}

func (r *Runner) findOrOutlineNextChapter(
	ctx context.Context,
	req domain.AutopilotRequest,
	vol domain.Volume,
	remaining int,
	skipped map[int64]bool,
	extra AutopilotExtraStore,
	addLog func(seq int, step domain.AutopilotStep, tone, msg string),
) (*domain.Chapter, error) {
	if r.outlineStore == nil {
		return nil, errors.New("outline store not initialized")
	}
	chaps, err := r.outlineStore.ListChapters(ctx, vol.ID)
	if err != nil {
		return nil, err
	}
	for i := range chaps {
		ch := chaps[i]
		if skipped[ch.ID] {
			continue
		}
		if strings.TrimSpace(ch.Content) == "" ||
			ch.Status == domain.ChapterStatusPlanned ||
			ch.Status == domain.ChapterStatusOutlined ||
			ch.Status == domain.ChapterStatusPartial {
			return &ch, nil
		}
	}

	if !req.AutoOutline {
		return nil, nil
	}

	// 自动批量续写接下来的章纲（单次最多补 5 章）
	genCount := min(remaining, 5)
	if genCount < 1 {
		genCount = 1
	}
	addLog(0, domain.AutopilotStepOutlining, "info", fmt.Sprintf("待写章节不足，正在自动续写后续 %d 章大纲…", genCount))
	created, err := r.BatchGenerateOutlines(ctx, domain.BatchOutlineRequest{
		BookID:   req.BookID,
		VolumeID: vol.ID,
		Count:    genCount,
		ArcGoal:  strings.TrimSpace(req.ArcGoal),
	}, nil)
	if err != nil {
		return nil, err
	}
	if len(created) == 0 {
		return nil, errors.New("自动续写章纲未返回新章节")
	}

	// 自动给新生成章纲的节拍关联已存在的角色/道具实体，避免写稿上下文缺人。
	if extra != nil {
		if entities, eErr := extra.ListEntities(ctx, req.BookID, domain.EntityFilter{}); eErr == nil && len(entities) > 0 {
			for _, ch := range created {
				beats, bErr := extra.ListBeats(ctx, ch.ID)
				if bErr != nil || len(beats) == 0 {
					continue
				}
				changed := false
				for bi := range beats {
					if len(beats[bi].EntityIDs) == 0 {
						matched := matchMentionedEntityIDs(beats[bi].Summary+" "+beats[bi].ConflictNote+" "+ch.OutlineSummary, entities)
						if len(matched) > 0 {
							beats[bi].EntityIDs = matched
							changed = true
						}
					}
				}
				if changed {
					_, _ = r.outlineStore.ReplaceBeats(ctx, ch.ID, beats)
				}
			}
		}
	}

	addLog(created[0].GlobalSeq, domain.AutopilotStepOutlining, "ok", fmt.Sprintf("已自动续写 %d 章大纲（第 %d~%d 章）", len(created), created[0].GlobalSeq, created[len(created)-1].GlobalSeq))
	first := created[0]
	return &first, nil
}

func buildAutoAcceptDeltaInput(delta domain.ChapterDelta) domain.ApplyDeltaInput {
	decisions := make([]domain.DeltaDecision, 0, len(delta.NewEntities)+len(delta.StatePatches)+len(delta.Foreshadows))
	for _, ne := range delta.NewEntities {
		if ne.ID != "" {
			decisions = append(decisions, domain.DeltaDecision{
				Kind:   domain.DeltaDecisionKindNewEntity,
				ItemID: ne.ID,
				Accept: true,
			})
		}
	}
	for _, sp := range delta.StatePatches {
		if sp.ID != "" {
			decisions = append(decisions, domain.DeltaDecision{
				Kind:   domain.DeltaDecisionKindStatePatch,
				ItemID: sp.ID,
				Accept: true,
			})
		}
	}
	for _, fs := range delta.Foreshadows {
		if fs.ID != "" {
			decisions = append(decisions, domain.DeltaDecision{
				Kind:   domain.DeltaDecisionKindForeshadow,
				ItemID: fs.ID,
				Accept: true,
			})
		}
	}
	return domain.ApplyDeltaInput{
		DeltaID:            delta.ID,
		ApplyCausalSummary: strings.TrimSpace(delta.CausalSummary) != "",
		Decisions:          decisions,
	}
}

func (r *Runner) emitAutopilotProgress(state domain.AutopilotState) {
	if r != nil && r.autopilotListener != nil {
		r.autopilotListener(state.Normalized())
	}
}

func cloneAutopilotState(src domain.AutopilotState) domain.AutopilotState {
	dst := src
	dst.Logs = make([]domain.AutopilotLogEntry, len(src.Logs))
	copy(dst.Logs, src.Logs)
	if src.Gate != nil {
		g := *src.Gate
		g.Options = make([]domain.AutopilotGateOption, len(src.Gate.Options))
		copy(g.Options, src.Gate.Options)
		dst.Gate = &g
	}
	return dst.Normalized()
}
