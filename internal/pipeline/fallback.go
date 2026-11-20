package pipeline

import (
	"context"
	"errors"
	"strings"

	"nib/internal/domain"
	"nib/internal/llm"
)

// streamWithFallback 使用指定路由的主模型发起流式调用；
// 若主模型在尚未吐出任何正文分片前因非取消错误（429/5xx/超时/不可用等）失败，
// 且该路由配置了有效的启用备用供应商与模型，则无缝切换至备用模型重试一次。
func (r *Runner) streamWithFallback(
	ctx context.Context,
	route domain.ModelRoute,
	primaryProvider domain.Provider,
	req llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
) (usedProvider domain.Provider, usedModel string, usage domain.TokenUsage, err error) {
	usedProvider = primaryProvider
	usedModel = req.Model

	var emittedDelta bool
	wrappedEmit := func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			emittedDelta = true
		}
		if onEvent != nil {
			return onEvent(ev)
		}
		return nil
	}

	usage, err = r.llm.Stream(ctx, primaryProvider, req, wrappedEmit)
	if err == nil {
		return usedProvider, usedModel, usage, nil
	}
	if emittedDelta || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrGenerationCanceled) {
		return usedProvider, usedModel, usage, err
	}

	// 检查是否配置了备用供应商与模型
	fbProv, fbModel, ok := r.resolveFallbackProviderAndModel(ctx, route, primaryProvider.ID, req.Model)
	if !ok {
		return usedProvider, usedModel, usage, err
	}

	fbReq := req
	fbReq.Model = fbModel
	fbUsage, fbErr := r.llm.Stream(ctx, fbProv, fbReq, onEvent)
	return fbProv, fbModel, fbUsage, fbErr
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
	// 避免主备指向同一个供应商+模型死循环重试
	if *route.FallbackProviderID == primaryProviderID && strings.EqualFold(fbModel, strings.TrimSpace(primaryModel)) {
		return domain.Provider{}, "", false
	}
	fbProv, err := r.store.GetProvider(ctx, *route.FallbackProviderID)
	if err != nil || !fbProv.Enabled || strings.TrimSpace(fbProv.BaseURL) == "" {
		return domain.Provider{}, "", false
	}
	return fbProv, fbModel, true
}
