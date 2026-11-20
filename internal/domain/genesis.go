package domain

import "strings"

const (
	DefaultGenesisChapterCount = 3
	MinGenesisChapterCount     = 1
	MaxGenesisChapterCount     = 5
)

// BookGenesisRequest 是「AI 一句话开书」的生成入参。
type BookGenesisRequest struct {
	Idea               string  `json:"idea"`
	Genre              GenreID `json:"genre"`
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
	if r.ChapterCount != 0 && (r.ChapterCount < MinGenesisChapterCount || r.ChapterCount > MaxGenesisChapterCount) {
		return newValidationError("chapterCount", "初始章纲数量需在 1 到 5 章之间")
	}
	if r.TargetChapterWords != 0 && (r.TargetChapterWords < MinTargetChapterWords || r.TargetChapterWords > MaxTargetChapterWords) {
		return newValidationError("targetChapterWords", "每章目标字数需在 300 到 50000 之间")
	}
	return nil
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
