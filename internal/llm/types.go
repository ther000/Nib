package llm

import "nib/internal/domain"

// ToolDefinition 定义发往 OpenAI / Anthropic / Gemini 原生 Function Calling 的工具规范。
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall 表示模型发出的单次原生工具调用。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatMessage 表示对话请求中的单条消息（兼容普通对话与原生 Tool Calling 回传）。
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ChatRequest 表示发往统一 LLM 适配层的标准聊天补全请求。
type ChatRequest struct {
	Model       string           `json:"model"`
	System      string           `json:"system,omitempty"`
	Messages    []ChatMessage    `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"maxTokens"`
}

// StreamEvent 表示统一流式解析器输出的单个事件分片。
type StreamEvent struct {
	Delta          string
	ReasoningDelta string
	ToolCalls      []ToolCall
	Usage          *domain.TokenUsage
	FinishReason   string
	Err            error
}
