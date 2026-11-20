package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"nib/internal/domain"
)

type geminiPart struct {
	Text    string `json:"text,omitempty"`
	Thought bool   `json:"thought,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiStreamChunk struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

func buildGeminiRequest(ctx context.Context, provider domain.Provider, req ChatRequest) (*http.Request, error) {
	base := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	model := strings.TrimPrefix(strings.TrimSpace(req.Model), "models/")
	escapedModel := url.PathEscape(model)

	var endpoint string
	if strings.HasSuffix(base, "/v1beta") {
		endpoint = fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", base, escapedModel)
	} else {
		endpoint = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", base, escapedModel)
	}

	var sysParts []string
	if strings.TrimSpace(req.System) != "" {
		sysParts = append(sysParts, req.System)
	}

	contents := make([]geminiContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch strings.ToLower(strings.TrimSpace(m.Role)) {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				sysParts = append(sysParts, m.Content)
			}
		case "assistant", "model":
			contents = append(contents, geminiContent{
				Role:  "model",
				Parts: []geminiPart{{Text: m.Content}},
			})
		default:
			contents = append(contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: m.Content}},
			})
		}
	}

	payload := geminiRequest{
		Contents: contents,
		GenerationConfig: &geminiGenerationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		},
	}
	if len(sysParts) > 0 {
		payload.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: strings.Join(sysParts, "\n\n")}},
		}
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		httpReq.Header.Set("x-goog-api-key", key)
	}
	return httpReq, nil
}

func parseGeminiStream(
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

		var chunk geminiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil
		}

		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return classifyStreamPayloadError(
				domain.ProtocolGemini,
				chunk.Error.Status,
				fmt.Sprint(chunk.Error.Code),
				chunk.Error.Message,
				apiKey,
			)
		}

		if len(chunk.Candidates) > 0 {
			cand := chunk.Candidates[0]
			if cand.FinishReason != "" {
				finishReason = cand.FinishReason
				seenValidEvent = true
			}
			for _, part := range cand.Content.Parts {
				if part.Text == "" {
					continue
				}
				seenValidEvent = true
				if part.Thought {
					if err := emit(StreamEvent{
						ReasoningDelta: part.Text,
						FinishReason:   finishReason,
					}); err != nil {
						return err
					}
					continue
				}
				seenDelta = true
				if err := emit(StreamEvent{
					Delta:        part.Text,
					FinishReason: finishReason,
				}); err != nil {
					return err
				}
			}
		}

		if chunk.UsageMetadata != nil {
			seenValidEvent = true
			u := domain.TokenUsage{
				PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
				CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
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
			Protocol:  domain.ProtocolGemini,
			Message:   "上游流式响应为空，未收到有效事件或正文分片",
			Retryable: true,
			Err:       domain.ErrUpstreamUnavailable,
		}
	}
	return usage, nil
}
