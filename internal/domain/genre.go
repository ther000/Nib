package domain

// GenreFieldSpec 描述某题材包下实体的快捷属性字段定义。
type GenreFieldSpec struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	EntityKind  EntityKind `json:"entityKind"`
	Placeholder string     `json:"placeholder"`
	Required    bool       `json:"required,omitempty"`
	HighRisk    bool       `json:"highRisk,omitempty"`
}

// BeatTemplate 描述某题材包预置的单章节拍卡模板项。
type BeatTemplate struct {
	Stage       BeatStage `json:"stage"`
	Title       string    `json:"title"`
	PromptHint  string    `json:"promptHint"`
	TargetWords int       `json:"targetWords"`
}

// GenrePreset 是男频题材包的完整静态规格，供建书引导、设定面板和编译器 P0 槽位使用。
type GenrePreset struct {
	ID                GenreID          `json:"id"`
	Name              string           `json:"name"`
	Tagline           string           `json:"tagline"`
	LoopCycleChapters string           `json:"loopCycleChapters"`
	PacingFocus       string           `json:"pacingFocus"`
	EntityFields      []GenreFieldSpec `json:"entityFields"`
	DefaultBeats      []BeatTemplate   `json:"defaultBeats"`
	AntiAIDirectives  []string         `json:"antiAiDirectives"`
	AntiTropeRules    []string         `json:"antiTropeRules"`
	SystemDirective   string           `json:"systemDirective"`
}
