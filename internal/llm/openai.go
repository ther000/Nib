package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"nib/internal/domain"
)

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolSpec struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters,omitempty"`
	} `json:"function"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIChatRequest struct {
	Model         string           `json:"model"`
	Messages      []openAIMessage  `json:"messages"`
	Tools         []openAIToolSpec `json:"tools,omitempty"`
	Temperature   float64          `json:"temperature"`
	MaxTokens     int              `json:"max_tokens,omitempty"`
	Stream        bool             `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
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

	msgs := make([]openAIMessage, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		om := openAIMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			om.ToolCalls = make([]openAIToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				var otc openAIToolCall
				otc.ID = tc.ID
				otc.Type = "function"
				otc.Function.Name = tc.Name
				otc.Function.Arguments = tc.Arguments
				om.ToolCalls = append(om.ToolCalls, otc)
			}
			if strings.TrimSpace(m.Content) == "" {
				om.Content = nil
			}
		}
		msgs = append(msgs, om)
	}

	var tools []openAIToolSpec
	if len(req.Tools) > 0 {
		tools = make([]openAIToolSpec, 0, len(req.Tools))
		for _, t := range req.Tools {
			var spec openAIToolSpec
			spec.Type = "function"
			spec.Function.Name = t.Name
			spec.Function.Description = t.Description
			spec.Function.Parameters = t.Parameters
			tools = append(tools, spec)
		}
	}

	payload := openAIChatRequest{
		Model:       req.Model,
		Messages:    msgs,
		Tools:       tools,
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
	type toolAcc struct {
		id   string
		name string
		args strings.Builder
	}
	toolMap := make(map[int]*toolAcc)

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
			for _, tc := range ch.Delta.ToolCalls {
				seenValidEvent = true
				acc, ok := toolMap[tc.Index]
				if !ok {
					acc = &toolAcc{}
					toolMap[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name += tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.args.WriteString(tc.Function.Arguments)
				}
			}
			reasoning := ch.Delta.ReasoningContent
			if reasoning == "" {
				reasoning = ch.Delta.Reasoning
			}
			if reasoning != "" {
				seenValidEvent = true
				if err := emit(StreamEvent{
					ReasoningDelta: reasoning,
					FinishReason:   finishReason,
				}); err != nil {
					return err
				}
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
	if len(toolMap) > 0 {
		keys := make([]int, 0, len(toolMap))
		for k := range toolMap {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		calls := make([]ToolCall, 0, len(toolMap))
		for _, i := range keys {
			acc := toolMap[i]
			if acc != nil && strings.TrimSpace(acc.name) != "" {
				id := acc.id
				if id == "" {
					id = fmt.Sprintf("call_%d", i+1)
				}
				calls = append(calls, ToolCall{
					ID:        id,
					Name:      strings.TrimSpace(acc.name),
					Arguments: strings.TrimSpace(acc.args.String()),
				})
			}
		}
		if len(calls) > 0 {
			seenValidEvent = true
			if emitErr := emit(StreamEvent{ToolCalls: calls, FinishReason: finishReason}); emitErr != nil {
				return usage, emitErr
			}
		}
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
