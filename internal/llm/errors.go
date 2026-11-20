package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"nib/internal/domain"
)

const maxSanitizedErrRunes = 300

var (
	reBearerToken  = regexp.MustCompile(`(?i)\bBearer\s+[^\s"',;}\]]+`)
	reHeaderSecret = regexp.MustCompile(`(?i)(x-api-key|x-goog-api-key|api[_-]?key|authorization)\s*[:=]\s*[^\s"',;}\]]+`)
	reQuerySecret  = regexp.MustCompile(`(?i)([?&](?:key|api_key|apikey|token)=)[^&\s"',;}\]]+`)
)

// APIError 表示上游 LLM 接口或传输层返回的脱敏分类错误。
type APIError struct {
	Protocol   domain.ProviderProtocol
	StatusCode int
	Message    string
	Retryable  bool
	Err        error
}

func (e *APIError) Error() string {
	if e.StatusCode > 0 {
		if e.Message != "" {
			return fmt.Sprintf("llm(%s) HTTP %d: %s", e.Protocol, e.StatusCode, e.Message)
		}
		return fmt.Sprintf("llm(%s) HTTP %d", e.Protocol, e.StatusCode)
	}
	if e.Message != "" {
		return fmt.Sprintf("llm(%s): %s", e.Protocol, e.Message)
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "llm request failed"
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// IsRetryable 判断一个错误是否属于允许退避重试的上游瞬态故障。
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, domain.ErrGenerationCanceled) || errors.Is(err, context.Canceled) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable
	}
	return errors.Is(err, domain.ErrUpstreamRateLimit) || errors.Is(err, domain.ErrUpstreamUnavailable)
}

func classifyContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return domain.ErrGenerationCanceled
	}
	return err
}

func classifyTransportError(ctx context.Context, protocol domain.ProviderProtocol, err error, apiKey string) error {
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return domain.ErrGenerationCanceled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &APIError{
			Protocol:  protocol,
			Message:   "请求超时",
			Retryable: false,
			Err:       context.DeadlineExceeded,
		}
	}
	return &APIError{
		Protocol:  protocol,
		Message:   sanitizeErrorText(err.Error(), apiKey),
		Retryable: true,
		Err:       domain.ErrUpstreamUnavailable,
	}
}

func classifyHTTPError(protocol domain.ProviderProtocol, status int, body []byte, apiKey string) error {
	rawMsg := extractUpstreamErrorMessage(body)
	cleanMsg := sanitizeErrorText(rawMsg, apiKey)

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		if cleanMsg == "" {
			cleanMsg = "API Key 无效或无权限"
		}
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    cleanMsg,
			Retryable:  false,
			Err:        domain.ErrInvalidAPIKey,
		}
	case http.StatusNotFound:
		if cleanMsg == "" {
			cleanMsg = "模型不存在或 Base URL 路径无效"
		}
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    cleanMsg,
			Retryable:  false,
		}
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		if isContextOverflowMessage(rawMsg) || isContextOverflowMessage(string(body)) {
			if cleanMsg == "" {
				cleanMsg = "上下文长度超出模型窗口上限"
			}
			return &APIError{
				Protocol:   protocol,
				StatusCode: status,
				Message:    cleanMsg,
				Retryable:  false,
				Err:        domain.ErrContextOverflow,
			}
		}
		if cleanMsg == "" {
			cleanMsg = "上游拒绝了当前请求参数"
		}
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    cleanMsg,
			Retryable:  false,
		}
	case http.StatusTooManyRequests:
		if cleanMsg == "" {
			cleanMsg = "上游触发限流，请稍后重试"
		}
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    cleanMsg,
			Retryable:  true,
			Err:        domain.ErrUpstreamRateLimit,
		}
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		if cleanMsg == "" {
			cleanMsg = "上游服务暂时不可用"
		}
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    cleanMsg,
			Retryable:  true,
			Err:        domain.ErrUpstreamUnavailable,
		}
	default:
		retryable := status >= 502 && status <= 504
		var sentinel error
		if status >= 500 {
			sentinel = domain.ErrUpstreamUnavailable
		}
		if cleanMsg == "" {
			cleanMsg = http.StatusText(status)
		}
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    cleanMsg,
			Retryable:  retryable,
			Err:        sentinel,
		}
	}
}

func classifyStreamPayloadError(protocol domain.ProviderProtocol, errType, errCode, errMsg, apiKey string) error {
	combined := strings.TrimSpace(strings.Join([]string{errType, errCode, errMsg}, " "))
	cleanMsg := sanitizeErrorText(errMsg, apiKey)
	if cleanMsg == "" {
		cleanMsg = sanitizeErrorText(combined, apiKey)
	}
	lower := strings.ToLower(combined)

	switch {
	case strings.Contains(lower, "authentication") || strings.Contains(lower, "invalid_api_key") || strings.Contains(lower, "permission"):
		return &APIError{Protocol: protocol, Message: cleanMsg, Retryable: false, Err: domain.ErrInvalidAPIKey}
	case isContextOverflowMessage(combined):
		return &APIError{Protocol: protocol, Message: cleanMsg, Retryable: false, Err: domain.ErrContextOverflow}
	case strings.Contains(lower, "rate_limit") || strings.Contains(lower, "resource_exhausted") || strings.Contains(lower, "quota"):
		return &APIError{Protocol: protocol, Message: cleanMsg, Retryable: true, Err: domain.ErrUpstreamRateLimit}
	case strings.Contains(lower, "overloaded") || strings.Contains(lower, "unavailable") || strings.Contains(lower, "timeout"):
		return &APIError{Protocol: protocol, Message: cleanMsg, Retryable: true, Err: domain.ErrUpstreamUnavailable}
	default:
		return &APIError{Protocol: protocol, Message: cleanMsg, Retryable: false}
	}
}

func isContextOverflowMessage(text string) bool {
	lower := strings.ToLower(text)
	patterns := []string{
		"context_length_exceeded",
		"context length",
		"maximum context",
		"context window",
		"too many tokens",
		"token limit",
		"prompt is too long",
		"input is too long",
		"exceeds the maximum number of tokens",
		"exceeds the context window",
		"request too large",
		"payload too large",
		"max_prompt_tokens",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func extractUpstreamErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var env struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
			Status  string `json:"status"`
		} `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		if env.Error != nil && strings.TrimSpace(env.Error.Message) != "" {
			if env.Error.Type != "" {
				return env.Error.Type + ": " + env.Error.Message
			}
			if env.Error.Status != "" {
				return env.Error.Status + ": " + env.Error.Message
			}
			return env.Error.Message
		}
		if strings.TrimSpace(env.Message) != "" {
			return env.Message
		}
		if strings.TrimSpace(env.Detail) != "" {
			return env.Detail
		}
	}
	return trimmed
}

func classifyNonSSEBody(protocol domain.ProviderProtocol, status int, mediaType string, body []byte, apiKey string) error {
	trimmed := strings.TrimSpace(string(body))

	// 兼容中转站在 HTTP 200 下返回 application/json 错误体的情况。
	if strings.Contains(mediaType, "json") || (len(trimmed) > 0 && trimmed[0] == '{') {
		var env struct {
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    any    `json:"code"`
				Status  string `json:"status"`
			} `json:"error"`
			Code    any    `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}
		if err := json.Unmarshal(body, &env); err == nil {
			if env.Error != nil && (strings.TrimSpace(env.Error.Message) != "" || env.Error.Type != "" || env.Error.Status != "") {
				codeStr := ""
				if env.Error.Code != nil {
					codeStr = fmt.Sprint(env.Error.Code)
				}
				errType := env.Error.Type
				if errType == "" {
					errType = env.Error.Status
				}
				return classifyStreamPayloadError(protocol, errType, codeStr, env.Error.Message, apiKey)
			}
			msg := strings.TrimSpace(env.Message)
			if msg == "" {
				msg = strings.TrimSpace(env.Detail)
			}
			if msg != "" {
				codeStr := ""
				if env.Code != nil {
					codeStr = fmt.Sprint(env.Code)
				}
				return classifyStreamPayloadError(protocol, env.Type, codeStr, msg, apiKey)
			}
		}
		clean := sanitizeErrorText(trimmed, apiKey)
		if clean == "" {
			clean = "上游返回了空 JSON 响应而非 SSE 流"
		}
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    "上游返回非流式 JSON 响应: " + clean,
			Retryable:  false,
		}
	}

	if strings.Contains(mediaType, "html") || looksLikeHTML(trimmed) {
		clean := sanitizeErrorText(trimmed, apiKey)
		msg := "上游返回了 HTML 页面而非流式响应，请检查 Base URL 或代理配置"
		if clean != "" {
			msg += ": " + clean
		}
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    msg,
			Retryable:  false,
		}
	}

	clean := sanitizeErrorText(trimmed, apiKey)
	if clean == "" {
		return &APIError{
			Protocol:   protocol,
			StatusCode: status,
			Message:    "上游返回了不支持的非流式响应类型: " + mediaType,
			Retryable:  false,
		}
	}
	return &APIError{
		Protocol:   protocol,
		StatusCode: status,
		Message:    fmt.Sprintf("上游返回非流式响应(%s): %s", mediaType, clean),
		Retryable:  false,
	}
}

func looksLikeHTML(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "<!doctype html") ||
		strings.HasPrefix(lower, "<html") ||
		strings.Contains(lower, "<head>") ||
		strings.Contains(lower, "<body>")
}

// sanitizeErrorText 彻底抹除错误信息中的 API Key、认证头与过长原始内容（遵循 AGENTS.md §4.3）。
func sanitizeErrorText(raw string, secrets ...string) string {
	s := raw
	for _, secret := range secrets {
		trimmed := strings.TrimSpace(secret)
		if trimmed == "" {
			continue
		}
		s = strings.ReplaceAll(s, trimmed, "[REDACTED]")
		if escaped := url.QueryEscape(trimmed); escaped != "" && escaped != trimmed {
			s = strings.ReplaceAll(s, escaped, "[REDACTED]")
		}
	}
	s = reBearerToken.ReplaceAllString(s, "Bearer [REDACTED]")
	s = reHeaderSecret.ReplaceAllString(s, "$1=[REDACTED]")
	s = reQuerySecret.ReplaceAllString(s, "${1}[REDACTED]")

	// 压缩连续空白并截断超长 HTML/错误栈，避免污染 UI 展示。
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > maxSanitizedErrRunes {
		return string(runes[:maxSanitizedErrRunes]) + "..."
	}
	return s
}
