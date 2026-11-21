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

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	Stream      bool               `json:"stream"`
}

type anthropicSSEPayload struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock *struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content_block"`
	Message *struct {
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
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
	msgs := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role == "system" {
			if strings.TrimSpace(m.Content) != "" {
				sysParts = append(sysParts, m.Content)
			}
			continue
		}
		if role == "tool" && m.ToolCallID != "" {
			msgs = append(msgs, anthropicMessage{
				Role: "user",
				Content: []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				}},
			})
			continue
		}
		if len(m.ToolCalls) > 0 {
			blocks := make([]map[string]any, 0, len(m.ToolCalls)+1)
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var inputObj any = map[string]any{}
				if strings.TrimSpace(tc.Arguments) != "" {
					_ = json.Unmarshal([]byte(tc.Arguments), &inputObj)
				}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": inputObj,
				})
			}
			msgs = append(msgs, anthropicMessage{Role: "assistant", Content: blocks})
			continue
		}
		msgs = append(msgs, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	var tools []anthropicTool
	if len(req.Tools) > 0 {
		tools = make([]anthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Parameters
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, anthropicTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: schema,
			})
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxToks
	}

	payload := anthropicRequest{
		Model:       req.Model,
		System:      strings.Join(sysParts, "\n\n"),
		Messages:    msgs,
		Tools:       tools,
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

	type antToolAcc struct {
		id   string
		name string
		args strings.Builder
	}
	toolBlocks := make(map[int]*antToolAcc)
	maxBlockIdx := -1

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
		case "content_block_start":
			if payload.ContentBlock != nil && payload.ContentBlock.Type == "tool_use" {
				seenValidEvent = true
				acc := &antToolAcc{id: payload.ContentBlock.ID, name: payload.ContentBlock.Name}
				if len(payload.ContentBlock.Input) > 0 && string(payload.ContentBlock.Input) != "{}" {
					acc.args.Write(payload.ContentBlock.Input)
				}
				toolBlocks[payload.Index] = acc
				if payload.Index > maxBlockIdx {
					maxBlockIdx = payload.Index
				}
			}
		case "content_block_delta":
			if payload.Delta != nil && payload.Delta.Type == "input_json_delta" {
				if acc, ok := toolBlocks[payload.Index]; ok {
					seenValidEvent = true
					acc.args.WriteString(payload.Delta.PartialJSON)
				}
			} else if payload.Delta != nil && payload.Delta.Type == "thinking_delta" && payload.Delta.Thinking != "" {
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
	if len(toolBlocks) > 0 {
		calls := make([]ToolCall, 0, len(toolBlocks))
		for i := 0; i <= maxBlockIdx; i++ {
			if acc, ok := toolBlocks[i]; ok && strings.TrimSpace(acc.name) != "" {
				args := strings.TrimSpace(acc.args.String())
				if args == "" {
					args = "{}"
				}
				calls = append(calls, ToolCall{ID: acc.id, Name: acc.name, Arguments: args})
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
			Protocol:  domain.ProtocolAnthropic,
			Message:   "上游流式响应为空，未收到有效事件或正文分片",
			Retryable: true,
			Err:       domain.ErrUpstreamUnavailable,
		}
	}
	return usage, nil
}
