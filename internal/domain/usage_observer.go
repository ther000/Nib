package domain

import "context"

type usageObserverCtxKey struct{}

// UsageObserver 用于在多阶段流水线执行期间实时汇总每一次模型调用的 Token 消耗。
type UsageObserver func(TokenUsage)

// WithUsageObserver 返回附加了 Token 消耗观察回调的子上下文。
func WithUsageObserver(ctx context.Context, fn UsageObserver) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, usageObserverCtxKey{}, fn)
}

// NotifyUsageObserver 向当前上下文注册的观察者汇报本次调用的 Token 消耗。
func NotifyUsageObserver(ctx context.Context, usage TokenUsage) {
	if ctx == nil {
		return
	}
	fn, _ := ctx.Value(usageObserverCtxKey{}).(UsageObserver)
	if fn == nil {
		return
	}
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = max(0, usage.PromptTokens) + max(0, usage.CompletionTokens)
	}
	if usage.TotalTokens > 0 {
		fn(usage)
	}
}
