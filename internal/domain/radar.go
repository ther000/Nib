package domain

import "strings"

// RadarPlatform 定义扫榜支持的网文平台。
type RadarPlatform string

const (
	RadarPlatformQidian RadarPlatform = "qidian"
	RadarPlatformFanqie RadarPlatform = "fanqie"
)

func (p RadarPlatform) Valid() bool {
	return p == RadarPlatformQidian || p == RadarPlatformFanqie
}

// RadarBoardSpec 描述一个可抓取的榜单规格。
type RadarBoardSpec struct {
	Platform RadarPlatform `json:"platform"`
	Key      string        `json:"key"`
	Label    string        `json:"label"`
	Category string        `json:"category,omitempty"`
}

// RadarItem 表示榜单中的单部作品条目。
type RadarItem struct {
	Rank        int    `json:"rank"`
	BookID      string `json:"bookId"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Category    string `json:"category"`
	SubCategory string `json:"subCategory,omitempty"`
	WordCount   string `json:"wordCount,omitempty"`
	HeatText    string `json:"heatText,omitempty"`
	StatusText  string `json:"statusText,omitempty"`
	Summary     string `json:"summary"`
	DetailURL   string `json:"detailUrl"`
}

// RadarBoard 表示一次扫榜抓取返回的完整榜单结果。
type RadarBoard struct {
	Platform      RadarPlatform `json:"platform"`
	BoardKey      string        `json:"boardKey"`
	BoardLabel    string        `json:"boardLabel"`
	SourceURL     string        `json:"sourceUrl"`
	FetchedAt     string        `json:"fetchedAt"`
	Cached        bool          `json:"cached"`
	FontProtected bool          `json:"fontProtected"`
	FontCSS       string        `json:"fontCss,omitempty"`
	FontFamily    string        `json:"fontFamily,omitempty"`
	Items         []RadarItem   `json:"items"`
}

// RadarAnalyzeRequest 是 AI 拆榜分析的入参，支持勾选明文榜单项或粘贴任意榜单文本。
type RadarAnalyzeRequest struct {
	Platform   RadarPlatform `json:"platform,omitempty"`
	BoardLabel string        `json:"boardLabel,omitempty"`
	Items      []RadarItem   `json:"items,omitempty"`
	RawText    string        `json:"rawText,omitempty"`
	Focus      string        `json:"focus,omitempty"`
}

func (r RadarAnalyzeRequest) Validate() error {
	if len(r.Items) == 0 && strings.TrimSpace(r.RawText) == "" {
		return newValidationError("items", "请先勾选至少一本上榜作品，或粘贴榜单文本")
	}
	return nil
}

// RadarIdeaAngle 表示 AI 基于榜单风向提炼出的一个差异化开书切入点。
type RadarIdeaAngle struct {
	Title      string   `json:"title"`
	Genre      GenreID  `json:"genre"`
	StyleTags  []string `json:"styleTags"`
	Idea       string   `json:"idea"`
	WhyItWorks string   `json:"whyItWorks"`
}

// RadarAnalysis 表示 AI 拆榜分析的结构化报告。
type RadarAnalysis struct {
	Summary      string           `json:"summary"`
	HotElements  []string         `json:"hotElements"`
	HookPatterns []string         `json:"hookPatterns"`
	Angles       []RadarIdeaAngle `json:"angles"`
}

// RadarAnalyzeChunk 是流式 AI 拆榜分析的进度与结果分片。
type RadarAnalyzeChunk struct {
	Kind     GenChunkKind   `json:"kind"`
	Delta    string         `json:"delta,omitempty"`
	Analysis *RadarAnalysis `json:"analysis,omitempty"`
	Usage    *TokenUsage    `json:"usage,omitempty"`
	Error    string         `json:"error,omitempty"`
}
