package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"nib/internal/domain"
	"nib/internal/llm"
)

func (r *Runner) resolveStageRouteAndProvider(
	ctx context.Context,
	stage domain.PipelineStage,
) (domain.ModelRoute, domain.Provider, error) {
	if r == nil || r.store == nil {
		return domain.ModelRoute{}, domain.Provider{}, errors.New("pipeline store not initialized")
	}
	route, err := r.store.GetModelRoute(ctx, stage)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ModelRoute{}, domain.Provider{}, domain.ErrRouteNotConfigured
		}
		return domain.ModelRoute{}, domain.Provider{}, fmt.Errorf("load %s model route: %w", stage, err)
	}
	if route.ProviderID == nil || *route.ProviderID <= 0 || strings.TrimSpace(route.ModelName) == "" {
		return domain.ModelRoute{}, domain.Provider{}, domain.ErrRouteNotConfigured
	}

	provider, err := r.store.GetProvider(ctx, *route.ProviderID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ModelRoute{}, domain.Provider{}, domain.ErrProviderNotConfigured
		}
		return domain.ModelRoute{}, domain.Provider{}, fmt.Errorf("load provider %d: %w", *route.ProviderID, err)
	}
	if !provider.Enabled || strings.TrimSpace(provider.BaseURL) == "" {
		return domain.ModelRoute{}, domain.Provider{}, domain.ErrProviderNotConfigured
	}
	return route, provider, nil
}

// resolveFirstAvailableStageRoute 按优先级依次查找第一个已配置且启用的阶段路由。
func (r *Runner) resolveFirstAvailableStageRoute(
	ctx context.Context,
	stages ...domain.PipelineStage,
) (domain.ModelRoute, domain.Provider, error) {
	var lastErr error = domain.ErrRouteNotConfigured
	for _, st := range stages {
		route, prov, err := r.resolveStageRouteAndProvider(ctx, st)
		if err == nil {
			return route, prov, nil
		}
		lastErr = err
	}
	return domain.ModelRoute{}, domain.Provider{}, lastErr
}

func (r *Runner) streamWithFallback(
	ctx context.Context,
	route domain.ModelRoute,
	primaryProvider domain.Provider,
	req llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
) (usedProvider domain.Provider, usedModel string, usage domain.TokenUsage, err error) {
	return r.streamWithFallbackNotify(ctx, route, primaryProvider, req, onEvent, nil)
}

// streamWithFallbackNotify 使用指定路由的主模型发起流式调用；
// 若主模型在尚未吐出任何有效输出前因非取消错误失败，且配置了有效的启用备用模型，
// 则通过 onFallback 通知调用方更新当前活跃模型后切换备用模型重试一次；若两者皆败则保留主/备双端错误上下文。
func (r *Runner) streamWithFallbackNotify(
	ctx context.Context,
	route domain.ModelRoute,
	primaryProvider domain.Provider,
	req llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
	onFallback func(fbProv domain.Provider, fbModel string),
) (usedProvider domain.Provider, usedModel string, usage domain.TokenUsage, err error) {
	usedProvider = primaryProvider
	usedModel = req.Model

	var emittedOutput bool
	wrappedEmit := func(ev llm.StreamEvent) error {
		if ev.Delta != "" || ev.ReasoningDelta != "" || len(ev.ToolCalls) > 0 {
			emittedOutput = true
		}
		if onEvent != nil {
			return onEvent(ev)
		}
		return nil
	}

	usage, primaryErr := r.llm.Stream(ctx, primaryProvider, req, wrappedEmit)
	if primaryErr == nil {
		return usedProvider, usedModel, usage, nil
	}
	if emittedOutput || ctx.Err() != nil || errors.Is(primaryErr, context.Canceled) || errors.Is(primaryErr, domain.ErrGenerationCanceled) {
		return usedProvider, usedModel, usage, primaryErr
	}

	fbProv, fbModel, ok := r.resolveFallbackProviderAndModel(ctx, route, primaryProvider.ID, req.Model)
	if !ok {
		return usedProvider, usedModel, usage, primaryErr
	}

	if onFallback != nil {
		onFallback(fbProv, fbModel)
	}
	fbReq := req
	fbReq.Model = fbModel
	fbUsage, fbErr := r.llm.Stream(ctx, fbProv, fbReq, onEvent)
	if fbErr != nil {
		if ctx.Err() != nil || errors.Is(fbErr, context.Canceled) || errors.Is(fbErr, domain.ErrGenerationCanceled) {
			return fbProv, fbModel, fbUsage, fbErr
		}
		return fbProv, fbModel, fbUsage, fmt.Errorf(
			"主模型 %s/%s 失败（%w）；备用模型 %s/%s 同样失败: %w",
			primaryProvider.Name, req.Model, primaryErr,
			fbProv.Name, fbModel, fbErr,
		)
	}
	return fbProv, fbModel, fbUsage, nil
}

func (r *Runner) resolveFallbackProviderAndModel(
	ctx context.Context,
	route domain.ModelRoute,
	primaryProviderID int64,
	primaryModel string,
) (domain.Provider, string, bool) {
	if r == nil || r.store == nil || route.FallbackProviderID == nil || *route.FallbackProviderID <= 0 {
		return domain.Provider{}, "", false
	}
	fbModel := strings.TrimSpace(route.FallbackModelName)
	if fbModel == "" {
		return domain.Provider{}, "", false
	}
	if *route.FallbackProviderID == primaryProviderID && strings.EqualFold(fbModel, strings.TrimSpace(primaryModel)) {
		return domain.Provider{}, "", false
	}
	fbProv, err := r.store.GetProvider(ctx, *route.FallbackProviderID)
	if err != nil || !fbProv.Enabled || strings.TrimSpace(fbProv.BaseURL) == "" {
		return domain.Provider{}, "", false
	}
	return fbProv, fbModel, true
}

func (r *Runner) collectStageLLMResponse(
	ctx context.Context,
	bookID int64,
	chapterID int64,
	stage domain.PipelineStage,
	provider domain.Provider,
	route domain.ModelRoute,
	defaultMaxToks int,
	sysPrompt string,
	userPrompt string,
) (string, error) {
	maxTokens := route.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxToks
	}
	req := llm.ChatRequest{
		Model:       route.ModelName,
		System:      sysPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userPrompt}},
		Temperature: route.Temperature,
		MaxTokens:   maxTokens,
	}
	start := time.Now()
	var buf strings.Builder
	var lastUsage domain.TokenUsage
	usedProv, usedModel, usage, err := r.streamWithFallback(ctx, route, provider, req, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
		}
		if ev.Usage != nil {
			lastUsage = *ev.Usage
		}
		return nil
	})
	if hasUsage(usage) {
		lastUsage = usage
	}
	if hasUsage(lastUsage) {
		r.recordTokenUsage(ctx, bookID, chapterID, stage, usedProv.Name, usedModel, lastUsage, time.Since(start).Milliseconds())
	}
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// parseOrRepairStageJSON 解析模型返回的 JSON 文本；若首轮解析失败且未取消上下文，自动执行 1 次纠错重问。
func parseOrRepairStageJSON[T any](
	ctx context.Context,
	r *Runner,
	bookID int64,
	chapterID int64,
	stage domain.PipelineStage,
	provider domain.Provider,
	route domain.ModelRoute,
	defaultMaxToks int,
	schemaDesc string,
	raw string,
	parseFn func(string) (T, error),
) (T, error) {
	parsed, parseErr := parseFn(raw)
	if parseErr == nil {
		return parsed, nil
	}
	var zero T
	if ctx.Err() != nil {
		return zero, domain.ErrGenerationCanceled
	}
	repSys, repUser := buildStyleJSONRepairPrompts(schemaDesc, raw, parseErr)
	repResp, repErr := r.collectStageLLMResponse(ctx, bookID, chapterID, stage, provider, route, defaultMaxToks, repSys, repUser)
	if repErr != nil {
		return zero, fmt.Errorf("%s JSON 纠错请求失败: %w", schemaDesc, repErr)
	}
	parsed, parseErr = parseFn(repResp)
	if parseErr != nil {
		return zero, fmt.Errorf("%s JSON 解析失败（自动纠错重试 1 次后仍无效）: %w", schemaDesc, parseErr)
	}
	return parsed, nil
}

type polishPassResult struct {
	Audit        domain.ChapterAudit
	AppliedDiffs int
}

// runPolishPass 统一执行单章质检与手术式局部润色闭环：
// LintChapterLocal -> (若配 Critic) RunCriticAudit -> (若有病灶且配 Polish) GenerateSurgicalDiffs + ApplySurgicalDiff。
// strictErrors 为 true 时（微批队列），Critic/Polish 失败会向上返回错误；为 false 时（托管/AI 导演），非取消错误降级继续。
func (r *Runner) runPolishPass(
	ctx context.Context,
	chapterID int64,
	strictErrors bool,
	onStep func(step domain.BatchItemState, msg string),
) (polishPassResult, error) {
	var res polishPassResult
	if r == nil || r.styleStore == nil {
		return res, nil
	}

	if onStep != nil {
		onStep(domain.BatchItemLinting, "本地 AI 味扫描…")
	}
	if audit, lintErr := r.LintChapterLocal(ctx, chapterID); lintErr == nil {
		res.Audit = audit
	}

	if ctx.Err() != nil {
		return res, domain.ErrGenerationCanceled
	}
	if _, _, routeErr := r.resolveStageRouteAndProvider(ctx, domain.StageCritic); routeErr == nil {
		if onStep != nil {
			onStep(domain.BatchItemCritiquing, "Critic 独立盲审…")
		}
		criticAudit, criticErr := r.RunCriticAudit(ctx, chapterID)
		if criticErr != nil {
			if errors.Is(criticErr, domain.ErrGenerationCanceled) || ctx.Err() != nil {
				return res, domain.ErrGenerationCanceled
			}
			if strictErrors {
				return res, criticErr
			}
			if onStep != nil {
				onStep(domain.BatchItemCritiquing, "AI 审校跳过："+criticErr.Error())
			}
		} else {
			res.Audit = criticAudit
		}
	}

	if ctx.Err() != nil {
		return res, domain.ErrGenerationCanceled
	}
	if len(res.Audit.LintIssues) > 0 || len(res.Audit.CriticFindings) > 0 {
		if _, _, polishErr := r.resolveStageRouteAndProvider(ctx, domain.StagePolish); polishErr == nil {
			if onStep != nil {
				onStep(domain.BatchItemPolishing, "手术式局部润色…")
			}
			diffAudit, diffErr := r.GenerateSurgicalDiffs(ctx, chapterID, nil)
			if diffErr != nil {
				if errors.Is(diffErr, domain.ErrGenerationCanceled) || ctx.Err() != nil {
					return res, domain.ErrGenerationCanceled
				}
				if strictErrors {
					return res, diffErr
				}
				if onStep != nil {
					onStep(domain.BatchItemPolishing, "局部润色跳过："+diffErr.Error())
				}
				return res, nil
			}
			res.Audit = diffAudit
			for _, d := range diffAudit.Diffs {
				if d.Status != domain.DiffStatusPending {
					continue
				}
				appliedRes, appErr := r.ApplySurgicalDiff(ctx, domain.ApplySurgicalDiffInput{
					ChapterID: chapterID,
					DiffID:    d.ID,
					Accept:    true,
				})
				if appErr == nil {
					res.AppliedDiffs++
					res.Audit = appliedRes.Audit
				}
			}
		}
	}

	return res, nil
}
