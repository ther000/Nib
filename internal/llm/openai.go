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

type openAIChatRequest struct {
	Model         string        `json:"model"`
	Messages      []ChatMessage `json:"messages"`
	Temperature   float64       `json:"temperature"`
	MaxTokens     int           `json:"max_tokens,omitempty"`
	Stream        bool          `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func buildOpenAIRequest(ctx context.Context, provider domain.Provider, req ChatRequest) (*http.Request, error) {
	base := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	endpoint := base + "/chat/completions"
	if strings.HasSuffix(base, "/chat/completions") {
		endpoint = base
	}

	msgs := make([]ChatMessage, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		msgs = append(msgs, ChatMessage{Role: "system", Content: req.System})
	}
	msgs = append(msgs, req.Messages...)

	payload := openAIChatRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
		StreamOptions: &struct {
			IncludeUsage bool `json:"include_usage"`
		}{IncludeUsage: true},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	return httpReq, nil
}

func parseOpenAIStream(
	body io.Reader,
	apiKey string,
	emit func(StreamEvent) error,
) (domain.TokenUsage, error) {
	var usage domain.TokenUsage
	var finishReason string
	var seenValidEvent bool
	var seenDelta bool
	var errDone = errStreamCompleted

	err := readSSE(body, func(ev sseEvent) error {
		data := strings.TrimSpace(ev.Data)
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			return errDone
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// 容忍部分中转站混入的非 JSON 状态文本或空心跳帧。
			return nil
		}

		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			codeStr := ""
			if chunk.Error.Code != nil {
				codeStr = fmt.Sprint(chunk.Error.Code)
			}
			return classifyStreamPayloadError(domain.ProtocolOpenAICompat, chunk.Error.Type, codeStr, chunk.Error.Message, apiKey)
		}

		for _, ch := range chunk.Choices {
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				finishReason = *ch.FinishReason
				seenValidEvent = true
			}
			if ch.Delta.Content != "" {
				seenValidEvent = true
				seenDelta = true
				if err := emit(StreamEvent{
					Delta:        ch.Delta.Content,
					FinishReason: finishReason,
				}); err != nil {
					return err
				}
			}
		}

		if chunk.Usage != nil {
			seenValidEvent = true
			u := domain.TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
			if u.TotalTokens == 0 {
				u.TotalTokens = u.PromptTokens + u.CompletionTokens
			}
			usage = u
			uCopy := u
			if err := emit(StreamEvent{
				Usage:        &uCopy,
				FinishReason: finishReason,
			}); err != nil {
				return err
			}
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
			Protocol:  domain.ProtocolOpenAICompat,
			Message:   "上游流式响应为空，未收到有效事件或正文分片",
			Retryable: true,
			Err:       domain.ErrUpstreamUnavailable,
		}
	}
	return usage, nil
}
