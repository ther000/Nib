package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"nib/internal/domain"
)

const (
	maxErrorBodyBytes     = 64 * 1024
	defaultTestConnTimout = 10 * time.Second
)

var (
	defaultBackoffSchedule = []time.Duration{
		1 * time.Second,
		3 * time.Second,
		8 * time.Second,
	}
	errStreamCompleted = errors.New("stream completed")
)

// Option 用于自定义 Client 的 HTTP 客户端、退避序列与休眠函数（便于单测注入快速退避）。
type Option func(*Client)

// WithHTTPClient 注入自定义 *http.Client。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithBackoff 注入指数退避序列（切片长度即最大重试次数）。
func WithBackoff(schedule []time.Duration) Option {
	return func(c *Client) {
		cp := make([]time.Duration, len(schedule))
		copy(cp, schedule)
		c.backoff = cp
	}
}

// WithSleep 注入可取消的休眠实现，单元测试可注入零延迟桩函数。
func WithSleep(fn func(ctx context.Context, d time.Duration) error) Option {
	return func(c *Client) {
		if fn != nil {
			c.sleepFn = fn
		}
	}
}

// Client 是多协议统一的流式 LLM 客户端（支持 openai_compat / anthropic / gemini）。
type Client struct {
	httpClient *http.Client
	backoff    []time.Duration
	sleepFn    func(ctx context.Context, d time.Duration) error
}

// NewClient 创建默认配置的统一流式 LLM 客户端。
func NewClient(opts ...Option) *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 90 * time.Second,
	}
	c := &Client{
		httpClient: &http.Client{Transport: transport},
		backoff:    append([]time.Duration(nil), defaultBackoffSchedule...),
		sleepFn:    defaultSleep,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// Stream 发起统一流式生成请求，通过 onEvent 逐片推送增量文本与 Token 用量。
// 仅在尚未产出任何正文分片前遇到可重试错误（429 / 502 / 503 / 504 / 传输断连）时执行指数退避重试。
func (c *Client) Stream(
	ctx context.Context,
	provider domain.Provider,
	req ChatRequest,
	onEvent func(StreamEvent) error,
) (domain.TokenUsage, error) {
	if err := validateProviderAndReq(provider, req); err != nil {
		return domain.TokenUsage{}, err
	}

	var lastUsage domain.TokenUsage
	var lastErr error

	maxAttempts := len(c.backoff) + 1
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return lastUsage, classifyContextError(ctx.Err())
		}

		usage, hasDelta, cbErr, err := c.streamOnce(ctx, provider, req, onEvent)
		lastUsage = usage
		if err == nil {
			return usage, nil
		}
		lastErr = err

		// 回调主动中止、上下文取消、或已经输出正文分片后严禁重试（避免正文重复拼接）。
		if cbErr != nil || ctx.Err() != nil || hasDelta || !IsRetryable(err) {
			return usage, err
		}

		if attempt >= len(c.backoff) {
			break
		}
		if sleepErr := c.sleepFn(ctx, c.backoff[attempt]); sleepErr != nil {
			return usage, classifyContextError(sleepErr)
		}
	}

	return lastUsage, lastErr
}

// TestConnection 对指定供应商执行轻量流式探活与测速。
// 上游鉴权/模型/限流/网络失败以 ProviderTestResult{OK:false, Message:...} 返回且不泄露 API Key；
// 仅当入参配置非法或调用方主动取消 ctx 时返回非空 error。
func (c *Client) TestConnection(ctx context.Context, provider domain.Provider) (domain.ProviderTestResult, error) {
	if !provider.Protocol.Valid() {
		return domain.ProviderTestResult{OK: false, Message: "不支持的协议类型"}, fmt.Errorf("%w: unsupported protocol %q", domain.ErrValidation, provider.Protocol)
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return domain.ProviderTestResult{OK: false, Message: "Base URL 不能为空"}, fmt.Errorf("%w: empty baseUrl", domain.ErrValidation)
	}

	model := firstNonEmptyModel(provider.Models)
	if model == "" {
		model = firstNonEmptyModel(provider.FetchedModels)
	}
	if model == "" {
		return domain.ProviderTestResult{
			OK:      false,
			Message: "供应商未配置可用模型，请先拉取或添加至少一个模型",
		}, nil
	}

	probeCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		probeCtx, cancel = context.WithTimeout(ctx, defaultTestConnTimout)
		defer cancel()
	}

	start := time.Now()
	req := ChatRequest{
		Model:       model,
		Messages:    []ChatMessage{{Role: "user", Content: "请只回复 OK"}},
		Temperature: 0,
		MaxTokens:   128,
	}

	_, _, _, err := c.streamOnce(probeCtx, provider, req, nil)
	elapsedMs := time.Since(start).Milliseconds()

	if err == nil {
		return domain.ProviderTestResult{
			OK:        true,
			LatencyMs: elapsedMs,
			ModelUsed: model,
			Message:   "连接成功",
		}, nil
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return domain.ProviderTestResult{
			OK:        false,
			LatencyMs: elapsedMs,
			ModelUsed: model,
			Message:   "已取消连通性测试",
		}, domain.ErrGenerationCanceled
	}

	return domain.ProviderTestResult{
		OK:        false,
		LatencyMs: elapsedMs,
		ModelUsed: model,
		Message:   sanitizeErrorText(err.Error(), provider.APIKey),
	}, nil
}

func (c *Client) streamOnce(
	ctx context.Context,
	provider domain.Provider,
	req ChatRequest,
	onEvent func(StreamEvent) error,
) (usage domain.TokenUsage, hasDelta bool, callbackErr error, retErr error) {
	httpReq, err := buildProtocolRequest(ctx, provider, req)
	if err != nil {
		return domain.TokenUsage{}, false, nil, &APIError{
			Protocol:  provider.Protocol,
			Message:   sanitizeErrorText(err.Error(), provider.APIKey),
			Retryable: false,
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return domain.TokenUsage{}, false, nil, classifyTransportError(ctx, provider.Protocol, err, provider.APIKey)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return domain.TokenUsage{}, false, nil, classifyHTTPError(provider.Protocol, resp.StatusCode, body, provider.APIKey)
	}

	if mediaType := parseMediaType(resp.Header.Get("Content-Type")); isNonSSEMediaType(mediaType) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return domain.TokenUsage{}, false, nil, classifyNonSSEBody(provider.Protocol, resp.StatusCode, mediaType, body, provider.APIKey)
	}

	emit := func(ev StreamEvent) error {
		if ctx.Err() != nil {
			return classifyContextError(ctx.Err())
		}
		if ev.Delta != "" {
			hasDelta = true
		}
		if onEvent == nil {
			return nil
		}
		if err := onEvent(ev); err != nil {
			callbackErr = err
			return err
		}
		return nil
	}

	var parseErr error
	switch provider.Protocol {
	case domain.ProtocolOpenAICompat:
		usage, parseErr = parseOpenAIStream(resp.Body, provider.APIKey, emit)
	case domain.ProtocolAnthropic:
		usage, parseErr = parseAnthropicStream(resp.Body, provider.APIKey, emit)
	case domain.ProtocolGemini:
		usage, parseErr = parseGeminiStream(resp.Body, provider.APIKey, emit)
	default:
		return domain.TokenUsage{}, false, nil, fmt.Errorf("%w: unsupported protocol %q", domain.ErrValidation, provider.Protocol)
	}

	if parseErr == nil {
		return usage, hasDelta, nil, nil
	}
	if callbackErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return usage, hasDelta, callbackErr, domain.ErrGenerationCanceled
		}
		return usage, hasDelta, callbackErr, callbackErr
	}
	var apiErr *APIError
	if errors.As(parseErr, &apiErr) {
		return usage, hasDelta, nil, parseErr
	}
	return usage, hasDelta, nil, classifyTransportError(ctx, provider.Protocol, parseErr, provider.APIKey)
}

func buildProtocolRequest(ctx context.Context, provider domain.Provider, req ChatRequest) (*http.Request, error) {
	switch provider.Protocol {
	case domain.ProtocolOpenAICompat:
		return buildOpenAIRequest(ctx, provider, req)
	case domain.ProtocolAnthropic:
		return buildAnthropicRequest(ctx, provider, req)
	case domain.ProtocolGemini:
		return buildGeminiRequest(ctx, provider, req)
	default:
		return nil, fmt.Errorf("%w: unsupported protocol %q", domain.ErrValidation, provider.Protocol)
	}
}

func validateProviderAndReq(provider domain.Provider, req ChatRequest) error {
	if !provider.Protocol.Valid() {
		return fmt.Errorf("%w: unsupported protocol %q", domain.ErrValidation, provider.Protocol)
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return fmt.Errorf("%w: empty baseUrl", domain.ErrValidation)
	}
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("%w: empty model", domain.ErrValidation)
	}
	return nil
}

func firstNonEmptyModel(models []string) string {
	for _, m := range models {
		if trimmed := strings.TrimSpace(m); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseMediaType(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	mt, _, err := mime.ParseMediaType(trimmed)
	if err != nil {
		// 容错回退：截去分号参数并小写。
		if idx := strings.IndexByte(trimmed, ';'); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		return strings.ToLower(strings.TrimSpace(trimmed))
	}
	return strings.ToLower(strings.TrimSpace(mt))
}

func isNonSSEMediaType(mediaType string) bool {
	if mediaType == "" || mediaType == "text/event-stream" {
		return false
	}
	switch mediaType {
	case "text/plain", "application/octet-stream":
		// 极少数中转站流式 SSE 未设标准 text/event-stream，交由协议解析器校验有效事件数。
		return false
	default:
		return true
	}
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
