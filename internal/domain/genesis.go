package domain

import "strings"

const (
	DefaultGenesisChapterCount = 3
	MinGenesisChapterCount     = 1
	MaxGenesisChapterCount     = 5
)

// BookGenesisRequest 是「AI 一句话开书（一步直出）」的生成入参。
type BookGenesisRequest struct {
	Idea               string  `json:"idea"`
	Genre              GenreID `json:"genre"`
	POVMode            string  `json:"povMode,omitempty"`
	PacingStyle        string  `json:"pacingStyle,omitempty"`
	ChapterCount       int     `json:"chapterCount"`
	TargetChapterWords int     `json:"targetChapterWords"`
}

func (r BookGenesisRequest) Validate() error {
	if strings.TrimSpace(r.Idea) == "" {
		return newValidationError("idea", "请输入一句话故事灵感")
	}
	if r.Genre != "" && !r.Genre.Valid() {
		return newValidationError("genre", "不支持的题材风格")
	}
	if r.ChapterCount < 0 || r.ChapterCount > MaxGenesisChapterCount {
		return newValidationError("chapterCount", "初始章纲数量需在 0 到 5 章之间")
	}
	if r.TargetChapterWords != 0 && (r.TargetChapterWords < MinTargetChapterWords || r.TargetChapterWords > MaxTargetChapterWords) {
		return newValidationError("targetChapterWords", "每章目标字数需在 300 到 50000 之间")
	}
	return nil
}

// BookConcept 是两阶段开书第一步产出的「核心立意」草案（书名候选、简介、金手指与核心冲突）。
type BookConcept struct {
	Title           string   `json:"title"`
	TitleCandidates []string `json:"titleCandidates"`
	Genre           GenreID  `json:"genre"`
	POVMode         string   `json:"povMode,omitempty"`
	PacingStyle     string   `json:"pacingStyle,omitempty"`
	Premise         string   `json:"premise"`
	GoldenFinger    string   `json:"goldenFinger"`
	CoreConflict    string   `json:"coreConflict"`
}

func (c BookConcept) Validate() error {
	if strings.TrimSpace(c.Title) == "" {
		return newValidationError("title", "书名不能为空")
	}
	if c.Genre != "" && !c.Genre.Valid() {
		return newValidationError("genre", "不支持的题材风格")
	}
	return nil
}

// BookConceptRequest 是两阶段开书第一步（生成/微调核心立意）的入参。
type BookConceptRequest struct {
	Idea           string       `json:"idea"`
	Genre          GenreID      `json:"genre"`
	POVMode        string       `json:"povMode,omitempty"`
	PacingStyle    string       `json:"pacingStyle,omitempty"`
	Feedback       string       `json:"feedback,omitempty"`
	CurrentConcept *BookConcept `json:"currentConcept,omitempty"`
}

func (r BookConceptRequest) Validate() error {
	if strings.TrimSpace(r.Idea) == "" && r.CurrentConcept == nil {
		return newValidationError("idea", "请输入故事灵感")
	}
	if r.Genre != "" && !r.Genre.Valid() {
		return newValidationError("genre", "不支持的题材风格")
	}
	return nil
}

// ConceptChunk 是流式生成核心立意时的进度与结果分片。
type ConceptChunk struct {
	Kind    GenChunkKind `json:"kind"`
	Delta   string       `json:"delta,omitempty"`
	Concept *BookConcept `json:"concept,omitempty"`
	Usage   *TokenUsage  `json:"usage,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// BookBlueprintRequest 是两阶段开书第二步（基于拍板立意生成设定、第一卷与初始章纲）的入参。
// Section 支持 "" / "all"（全量）、"entities"（仅重生成设定）、"outlines"（仅重生成第一卷与章纲）。
type BookBlueprintRequest struct {
	Concept            BookConcept     `json:"concept"`
	ChapterCount       int             `json:"chapterCount"`
	TargetChapterWords int             `json:"targetChapterWords"`
	Section            string          `json:"section,omitempty"`
	ExistingEntities   []GenesisEntity `json:"existingEntities,omitempty"`
}

func (r BookBlueprintRequest) Validate() error {
	if err := r.Concept.Validate(); err != nil {
		return err
	}
	if r.ChapterCount < 0 || r.ChapterCount > MaxGenesisChapterCount {
		return newValidationError("chapterCount", "初始章纲数量需在 0 到 5 章之间")
	}
	if r.TargetChapterWords != 0 && (r.TargetChapterWords < MinTargetChapterWords || r.TargetChapterWords > MaxTargetChapterWords) {
		return newValidationError("targetChapterWords", "每章目标字数需在 300 到 50000 之间")
	}
	sec := strings.ToLower(strings.TrimSpace(r.Section))
	if sec != "" && sec != "all" && sec != "entities" && sec != "outlines" {
		return newValidationError("section", "不支持的蓝图生成范围")
	}
	return nil
}

// BookBlueprint 是两阶段开书第二步产出的蓝图草案（第一卷、初始设定与初始章纲）。
type BookBlueprint struct {
	FirstVolume     GenesisVolume             `json:"firstVolume"`
	Entities        []GenesisEntity           `json:"entities"`
	InitialOutlines []GeneratedChapterOutline `json:"initialOutlines"`
}

// BlueprintChunk 是流式生成开书蓝图时的进度与结果分片。
type BlueprintChunk struct {
	Kind      GenChunkKind   `json:"kind"`
	Delta     string         `json:"delta,omitempty"`
	Blueprint *BookBlueprint `json:"blueprint,omitempty"`
	Usage     *TokenUsage    `json:"usage,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// GenesisEntity 表示开书企划中的初始设定（角色、势力、地点、道具或世界铁律）。
type GenesisEntity struct {
	Kind        EntityKind        `json:"kind"`
	Name        string            `json:"name"`
	Aliases     []string          `json:"aliases"`
	IsInvariant bool              `json:"isInvariant"`
	Summary     string            `json:"summary"`
	Attributes  map[string]string `json:"attributes"`
}

// GenesisVolume 表示开书企划中的第一卷规划。
type GenesisVolume struct {
	Title      string `json:"title"`
	Goal       string `json:"goal"`
	ClimaxPlan string `json:"climaxPlan"`
}

// BookGenesisPlan 是 AI 生成的完整开书企划草案，支持作者确认微调后一次性落库。
type BookGenesisPlan struct {
	Title              string                    `json:"title"`
	Genre              GenreID                   `json:"genre"`
	POVMode            string                    `json:"povMode,omitempty"`
	PacingStyle        string                    `json:"pacingStyle,omitempty"`
	Premise            string                    `json:"premise"`
	GoldenFinger       string                    `json:"goldenFinger"`
	TargetChapterWords int                       `json:"targetChapterWords"`
	Entities           []GenesisEntity           `json:"entities"`
	FirstVolume        GenesisVolume             `json:"firstVolume"`
	InitialOutlines    []GeneratedChapterOutline `json:"initialOutlines"`
}

func (p BookGenesisPlan) Validate() error {
	if strings.TrimSpace(p.Title) == "" {
		return newValidationError("title", "书名不能为空")
	}
	if !p.Genre.Valid() {
		return newValidationError("genre", "不支持的题材风格")
	}
	if p.TargetChapterWords != 0 && (p.TargetChapterWords < MinTargetChapterWords || p.TargetChapterWords > MaxTargetChapterWords) {
		return newValidationError("targetChapterWords", "每章目标字数需在 300 到 50000 之间")
	}
	return nil
}

// GenesisChunk 是流式生成开书企划时的进度与结果分片。
type GenesisChunk struct {
	Kind  GenChunkKind     `json:"kind"`
	Delta string           `json:"delta,omitempty"`
	Plan  *BookGenesisPlan `json:"plan,omitempty"`
	Usage *TokenUsage      `json:"usage,omitempty"`
	Error string           `json:"error,omitempty"`
}

// ApplyGenesisInput 是将确认后的开书企划一次性写入书库的入参。
type ApplyGenesisInput struct {
	Plan      BookGenesisPlan `json:"plan"`
	ExportDir string          `json:"exportDir"`
}

func (in ApplyGenesisInput) Validate() error {
	return in.Plan.Validate()
}

// ApplyGenesisResult 返回开书企划落库后的全部核心记录。
type ApplyGenesisResult struct {
	Book     Book      `json:"book"`
	Volume   Volume    `json:"volume"`
	Entities []Entity  `json:"entities"`
	Chapters []Chapter `json:"chapters"`
}
