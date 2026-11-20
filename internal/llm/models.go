package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"nib/internal/domain"
)

const (
	maxModelsBodyBytes     = 2 * 1024 * 1024
	defaultListModelsLimit = 15 * time.Second
)

// ListModels 向供应商模型列表端点发起查询，返回去重且排序后的模型 ID 列表。
// 支持 openai_compat（GET /models）、anthropic（GET /v1/models）与 gemini（GET /v1beta/models）。
func (c *Client) ListModels(ctx context.Context, provider domain.Provider) ([]string, error) {
	if !provider.Protocol.Valid() {
		return nil, fmt.Errorf("%w: unsupported protocol %q", domain.ErrValidation, provider.Protocol)
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return nil, fmt.Errorf("%w: empty baseUrl", domain.ErrValidation)
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		callCtx, cancel = context.WithTimeout(ctx, defaultListModelsLimit)
		defer cancel()
	}

	httpReq, err := buildListModelsRequest(callCtx, provider)
	if err != nil {
		return nil, &APIError{
			Protocol:  provider.Protocol,
			Message:   sanitizeErrorText(err.Error(), provider.APIKey),
			Retryable: false,
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, classifyTransportError(callCtx, provider.Protocol, err, provider.APIKey)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxModelsBodyBytes))
	if readErr != nil {
		return nil, classifyTransportError(callCtx, provider.Protocol, readErr, provider.APIKey)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyHTTPError(provider.Protocol, resp.StatusCode, body, provider.APIKey)
	}

	trimmed := strings.TrimSpace(string(body))
	if looksLikeHTML(trimmed) {
		return nil, &APIError{
			Protocol:   provider.Protocol,
			StatusCode: resp.StatusCode,
			Message:    "上游返回了 HTML 页面而非模型列表 JSON，请检查 Base URL",
			Retryable:  false,
		}
	}

	switch provider.Protocol {
	case domain.ProtocolOpenAICompat:
		return parseOpenAIModels(body, provider.APIKey)
	case domain.ProtocolAnthropic:
		return parseAnthropicModels(body, provider.APIKey)
	case domain.ProtocolGemini:
		return parseGeminiModels(body, provider.APIKey)
	default:
		return nil, fmt.Errorf("%w: unsupported protocol %q", domain.ErrValidation, provider.Protocol)
	}
}

func buildListModelsRequest(ctx context.Context, provider domain.Provider) (*http.Request, error) {
	base := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	var endpoint string

	switch provider.Protocol {
	case domain.ProtocolOpenAICompat:
		base = strings.TrimSuffix(base, "/chat/completions")
		base = strings.TrimRight(base, "/")
		if strings.HasSuffix(base, "/models") {
			endpoint = base
		} else {
			endpoint = base + "/models"
		}
	case domain.ProtocolAnthropic:
		base = strings.TrimSuffix(base, "/messages")
		base = strings.TrimRight(base, "/")
		switch {
		case strings.HasSuffix(base, "/v1/models"):
			endpoint = base
		case strings.HasSuffix(base, "/v1"):
			endpoint = base + "/models"
		default:
			endpoint = base + "/v1/models"
		}
	case domain.ProtocolGemini:
		switch {
		case strings.HasSuffix(base, "/v1beta/models") || strings.HasSuffix(base, "/v1/models"):
			endpoint = base
		case strings.HasSuffix(base, "/v1beta") || strings.HasSuffix(base, "/v1"):
			endpoint = base + "/models"
		default:
			endpoint = base + "/v1beta/models"
		}
	default:
		return nil, fmt.Errorf("%w: unsupported protocol %q", domain.ErrValidation, provider.Protocol)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build models request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	key := strings.TrimSpace(provider.APIKey)
	switch provider.Protocol {
	case domain.ProtocolOpenAICompat:
		if key != "" {
			httpReq.Header.Set("Authorization", "Bearer "+key)
		}
	case domain.ProtocolAnthropic:
		httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
		if key != "" {
			httpReq.Header.Set("x-api-key", key)
		}
	case domain.ProtocolGemini:
		if key != "" {
			httpReq.Header.Set("x-goog-api-key", key)
		}
	}
	return httpReq, nil
}

func parseOpenAIModels(body []byte, apiKey string) ([]string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &APIError{
			Protocol:  domain.ProtocolOpenAICompat,
			Message:   "解析模型列表 JSON 失败: " + sanitizeErrorText(err.Error(), apiKey),
			Retryable: false,
		}
	}
	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return nil, classifyStreamPayloadError(domain.ProtocolOpenAICompat, payload.Error.Type, "", payload.Error.Message, apiKey)
	}

	raw := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			raw = append(raw, id)
		}
	}
	if len(raw) == 0 {
		for _, item := range payload.Models {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				id = strings.TrimSpace(item.Name)
			}
			if id != "" {
				raw = append(raw, id)
			}
		}
	}
	return dedupeAndSortModels(raw), nil
}

func parseAnthropicModels(body []byte, apiKey string) ([]string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &APIError{
			Protocol:  domain.ProtocolAnthropic,
			Message:   "解析模型列表 JSON 失败: " + sanitizeErrorText(err.Error(), apiKey),
			Retryable: false,
		}
	}
	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return nil, classifyStreamPayloadError(domain.ProtocolAnthropic, payload.Error.Type, "", payload.Error.Message, apiKey)
	}

	raw := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			raw = append(raw, id)
		}
	}
	return dedupeAndSortModels(raw), nil
}

func parseGeminiModels(body []byte, apiKey string) ([]string, error) {
	var payload struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &APIError{
			Protocol:  domain.ProtocolGemini,
			Message:   "解析模型列表 JSON 失败: " + sanitizeErrorText(err.Error(), apiKey),
			Retryable: false,
		}
	}
	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return nil, classifyStreamPayloadError(domain.ProtocolGemini, payload.Error.Status, fmt.Sprint(payload.Error.Code), payload.Error.Message, apiKey)
	}

	preferred := make([]string, 0, len(payload.Models))
	all := make([]string, 0, len(payload.Models))
	for _, m := range payload.Models {
		id := strings.TrimPrefix(strings.TrimSpace(m.Name), "models/")
		if id == "" {
			continue
		}
		all = append(all, id)
		for _, method := range m.SupportedGenerationMethods {
			if strings.EqualFold(strings.TrimSpace(method), "generateContent") {
				preferred = append(preferred, id)
				break
			}
		}
	}
	if len(preferred) > 0 {
		return dedupeAndSortModels(preferred), nil
	}
	return dedupeAndSortModels(all), nil
}

func dedupeAndSortModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		trimmed := strings.TrimSpace(m)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}
