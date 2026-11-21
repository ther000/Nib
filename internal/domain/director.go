package domain

import "strings"

// DirectorToolCall 记录 AI 导演在单轮任务中调用的工具与执行结果。
type DirectorToolCall struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Label         string `json:"label"`
	ArgsJSON      string `json:"argsJson,omitempty"`
	Status        string `json:"status"` // "running" | "ok" | "error"
	ResultSummary string `json:"resultSummary,omitempty"`
	UndoChapterID int64  `json:"undoChapterId,omitempty"`
	UndoVersionID int64  `json:"undoVersionId,omitempty"`
}

// DirectorMessage 表示作者与 AI 导演之间的一条持久化对话消息。
type DirectorMessage struct {
	ID        int64              `json:"id,omitempty"`
	Role      string             `json:"role"` // "user" | "assistant"
	Content   string             `json:"content"`
	ToolCalls []DirectorToolCall `json:"toolCalls,omitempty"`
	CreatedAt string             `json:"createdAt,omitempty"`
}

// DirectorChatRequest 是向 AI 导演下达自然语言指令的入参。
type DirectorChatRequest struct {
	BookID          int64             `json:"bookId"`
	ActiveChapterID int64             `json:"activeChapterId"`
	Instruction     string            `json:"instruction"`
	History         []DirectorMessage `json:"history"`
}

func (r DirectorChatRequest) Validate() error {
	if r.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if strings.TrimSpace(r.Instruction) == "" {
		return newValidationError("instruction", "请输入给 AI 导演的指令")
	}
	return nil
}

// DirectorEventKind 标识 AI 导演流式推送事件类型。
type DirectorEventKind string

const (
	DirectorEventStarted     DirectorEventKind = "started"
	DirectorEventThinking    DirectorEventKind = "thinking"
	DirectorEventToolStart   DirectorEventKind = "tool_start"
	DirectorEventToolResult  DirectorEventKind = "tool_result"
	DirectorEventAnswerDelta DirectorEventKind = "answer_delta"
	DirectorEventCompleted   DirectorEventKind = "completed"
	DirectorEventError       DirectorEventKind = "error"
)

// DirectorEvent 是 AI 导演执行过程中的实时推送分片。
type DirectorEvent struct {
	Kind      DirectorEventKind `json:"kind"`
	Step      int               `json:"step,omitempty"`
	Message   string            `json:"message,omitempty"`
	ToolCall  *DirectorToolCall `json:"toolCall,omitempty"`
	Delta     string            `json:"delta,omitempty"`
	Reply     string            `json:"reply,omitempty"`
	ToolCalls []DirectorToolCall `json:"toolCalls,omitempty"`
	Mutated   bool              `json:"mutated,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// DirectorReply 是 AI 导演完成本轮指令后的最终返回结果。
type DirectorReply struct {
	Reply     string             `json:"reply"`
	ToolCalls []DirectorToolCall `json:"toolCalls"`
	Mutated   bool               `json:"mutated"`
}
