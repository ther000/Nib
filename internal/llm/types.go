package llm

import "nib/internal/domain"

// ChatMessage 表示对话请求中的单条消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 表示发往统一 LLM 适配层的标准聊天补全请求。
type ChatRequest struct {
	Model       string        `json:"model"`
	System      string        `json:"system,omitempty"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"maxTokens"`
}

// StreamEvent 表示统一流式解析器输出的单个事件分片。
type StreamEvent struct {
	Delta          string
	ReasoningDelta string
	Usage          *domain.TokenUsage
	FinishReason   string
	Err            error
}
