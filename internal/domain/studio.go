package domain

const DefaultBriefingTokenBudget = 12000

// BriefingOptions 控制上下文编译器的槽位配额与人工微调选项。
type BriefingOptions struct {
	TokenBudget       int     `json:"tokenBudget,omitempty"`
	PinnedEntityIDs   []int64 `json:"pinnedEntityIds,omitempty"`
	ExcludedEntityIDs []int64 `json:"excludedEntityIds,omitempty"`
	ExtraInstruction  string  `json:"extraInstruction,omitempty"`
	IncludePendingMem bool    `json:"includePendingMem,omitempty"`
}

// BriefingSlot 表示作战简报中的单个优先级槽位（P0~P7）。
type BriefingSlot struct {
	Key         string `json:"key"`
	Priority    string `json:"priority"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	TokenBudget int    `json:"tokenBudget"`
	TokenEst    int    `json:"tokenEst"`
	Truncated   bool   `json:"truncated"`
	Required    bool   `json:"required"`
}

// Briefing 表示编译完成且可持久化至 chapters.last_briefing_json 的透明作战简报。
type Briefing struct {
	ChapterID        int64          `json:"chapterId"`
	ChapterSeq       int            `json:"chapterSeq"`
	BookTitle        string         `json:"bookTitle"`
	Genre            GenreID        `json:"genre"`
	TotalTokenBudget int            `json:"totalTokenBudget"`
	TotalTokenEst    int            `json:"totalTokenEst"`
	SystemPrompt     string         `json:"systemPrompt"`
	UserPrompt       string         `json:"userPrompt"`
	Slots            []BriefingSlot `json:"slots"`
	IncludedEntities []int64        `json:"includedEntities"`
	Warnings         []string       `json:"warnings,omitempty"`
	CompiledAt       string         `json:"compiledAt"`
}

// GenerateChapterOptions 控制单章流式出稿的临时覆盖参数。
type GenerateChapterOptions struct {
	Briefing         BriefingOptions `json:"briefing"`
	ExtraInstruction string          `json:"extraInstruction,omitempty"`
	Temperature      *float64        `json:"temperature,omitempty"`
	MaxTokens        *int            `json:"maxTokens,omitempty"`
}

func (o GenerateChapterOptions) Validate() error {
	if o.Briefing.TokenBudget < 0 {
		return newValidationError("briefing.tokenBudget", "上下文配额不能为负数")
	}
	if o.Temperature != nil && (*o.Temperature < 0 || *o.Temperature > 2.0) {
		return newValidationError("temperature", "温度参数必须在 0.0 到 2.0 之间")
	}
	if o.MaxTokens != nil && (*o.MaxTokens <= 0 || *o.MaxTokens > 200000) {
		return newValidationError("maxTokens", "最大输出 Token 范围无效")
	}
	return nil
}

// TokenUsage 记录一次模型调用的 Token 统计。
type TokenUsage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

// GenChunk 是通过 *mygo.Channel[GenChunk] 推送给前端审改台的流式分片事件。
type GenChunk struct {
	Kind         GenChunkKind  `json:"kind"`
	ChapterID    int64         `json:"chapterId"`
	Delta        string        `json:"delta,omitempty"`
	WordCount    int           `json:"wordCount,omitempty"`
	Status       ChapterStatus `json:"status,omitempty"`
	ModelName    string        `json:"modelName,omitempty"`
	ProviderName string        `json:"providerName,omitempty"`
	Usage        *TokenUsage   `json:"usage,omitempty"`
	FinishReason string        `json:"finishReason,omitempty"`
	Error        string        `json:"error,omitempty"`
	Retryable    bool          `json:"retryable,omitempty"`
	RetryAttempt int           `json:"retryAttempt,omitempty"`
}
