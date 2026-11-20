package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nib/internal/domain"
)

const (
	anthropicAPIVersion     = "2023-06-01"
	anthropicDefaultMaxToks = 4096
)

type anthropicRequest struct {
	Model       string        `json:"model"`
	System      string        `json:"system,omitempty"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type anthropicSSEPayload struct {
	Type    string `json:"type"`
	Message *struct {
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Delta *struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		Thinking   string `json:"thinking"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func buildAnthropicRequest(ctx context.Context, provider domain.Provider, req ChatRequest) (*http.Request, error) {
	base := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	var endpoint string
	switch {
	case strings.HasSuffix(base, "/v1/messages"):
		endpoint = base
	case strings.HasSuffix(base, "/v1"):
		endpoint = base + "/messages"
	default:
		endpoint = base + "/v1/messages"
	}

	var sysParts []string
	if strings.TrimSpace(req.System) != "" {
		sysParts = append(sysParts, req.System)
	}
	msgs := make([]ChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if strings.EqualFold(m.Role, "system") {
			if strings.TrimSpace(m.Content) != "" {
				sysParts = append(sysParts, m.Content)
			}
			continue
		}
		msgs = append(msgs, m)
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxToks
	}

	payload := anthropicRequest{
		Model:       req.Model,
		System:      strings.Join(sysParts, "\n\n"),
		Messages:    msgs,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build anthropic request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		httpReq.Header.Set("x-api-key", key)
	}
	return httpReq, nil
}

func parseAnthropicStream(
	body io.Reader,
	apiKey string,
	emit func(StreamEvent) error,
) (domain.TokenUsage, error) {
	var usage domain.TokenUsage
	var finishReason string
	var seenValidEvent bool
	var seenDelta bool
	errDone := errStreamCompleted

	err := readSSE(body, func(ev sseEvent) error {
		data := strings.TrimSpace(ev.Data)
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			return errDone
		}

		var payload anthropicSSEPayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}

		eventType := payload.Type
		if eventType == "" {
			eventType = ev.Event
		}

		switch eventType {
		case "ping":
			return nil
		case "error":
			if payload.Error != nil {
				return classifyStreamPayloadError(domain.ProtocolAnthropic, payload.Error.Type, "", payload.Error.Message, apiKey)
			}
			return classifyStreamPayloadError(domain.ProtocolAnthropic, "stream_error", "", data, apiKey)
		case "message_start":
			if payload.Message != nil && payload.Message.Usage != nil {
				u := payload.Message.Usage
				usage.PromptTokens = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
				if u.OutputTokens > 0 {
					usage.CompletionTokens = u.OutputTokens
				}
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
		case "content_block_delta":
			if payload.Delta != nil && payload.Delta.Type == "thinking_delta" && payload.Delta.Thinking != "" {
				seenValidEvent = true
				if err := emit(StreamEvent{ReasoningDelta: payload.Delta.Thinking}); err != nil {
					return err
				}
			} else if payload.Delta != nil && payload.Delta.Type == "text_delta" && payload.Delta.Text != "" {
				seenValidEvent = true
				seenDelta = true
				if err := emit(StreamEvent{Delta: payload.Delta.Text}); err != nil {
					return err
				}
			}
		case "message_delta":
			if payload.Delta != nil && payload.Delta.StopReason != "" {
				finishReason = payload.Delta.StopReason
				seenValidEvent = true
			}
			if payload.Usage != nil {
				if payload.Usage.InputTokens > 0 {
					usage.PromptTokens = payload.Usage.InputTokens
				}
				if payload.Usage.OutputTokens > 0 {
					usage.CompletionTokens = payload.Usage.OutputTokens
					seenValidEvent = true
				}
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
			uCopy := usage
			if err := emit(StreamEvent{
				Usage:        &uCopy,
				FinishReason: finishReason,
			}); err != nil {
				return err
			}
		case "message_stop":
			return errDone
		}
		return nil
	})

	if err == errDone {
		err = nil
	}
	if err != nil {
		return usage, err
	}
	if !seenDelta && !seenValidEvent {
		return usage, &APIError{
			Protocol:  domain.ProtocolAnthropic,
			Message:   "上游流式响应为空，未收到有效事件或正文分片",
			Retryable: true,
			Err:       domain.ErrUpstreamUnavailable,
		}
	}
	return usage, nil
}
