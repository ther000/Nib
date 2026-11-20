package domain

import "strings"

const (
	DefaultAvgSentenceChars  = 18.0
	DefaultMaxParagraphChars = 140
	DefaultDialogueRatioMin  = 0.25
	DefaultDialogueRatioMax  = 0.55
	DefaultDeaiScore         = 100
)

// SceneType 定义分场景样章锚点的场景分类（对应 style_samples.scene_type）。
type SceneType string

const (
	SceneCombat   SceneType = "combat"
	SceneDialogue SceneType = "dialogue"
	SceneDaily    SceneType = "daily"
	SceneSuspense SceneType = "suspense"
)

func (s SceneType) Valid() bool {
	switch s {
	case SceneCombat, SceneDialogue, SceneDaily, SceneSuspense:
		return true
	default:
		return false
	}
}

func (s SceneType) Label() string {
	switch s {
	case SceneCombat:
		return "战斗交锋"
	case SceneDialogue:
		return "博弈对话"
	case SceneDaily:
		return "过渡日常"
	case SceneSuspense:
		return "探秘悬念"
	default:
		return string(s)
	}
}

func AllSceneTypes() []SceneType {
	return []SceneType{SceneCombat, SceneDialogue, SceneDaily, SceneSuspense}
}

// DeaiCategory 定义去 AI 味规则所属分类（对应 deai_rules.category）。
type DeaiCategory string

const (
	DeaiCategoryCliche     DeaiCategory = "cliche"
	DeaiCategoryStructure  DeaiCategory = "structure"
	DeaiCategoryFormatting DeaiCategory = "formatting"
)

func (c DeaiCategory) Valid() bool {
	switch c {
	case DeaiCategoryCliche, DeaiCategoryStructure, DeaiCategoryFormatting:
		return true
	default:
		return false
	}
}

func (c DeaiCategory) Label() string {
	switch c {
	case DeaiCategoryCliche:
		return "高频套话"
	case DeaiCategoryStructure:
		return "句式结构病"
	case DeaiCategoryFormatting:
		return "排版与推进病"
	default:
		return string(c)
	}
}

// LintSeverity 定义本地去 AI 味告警与审校问题的严重级别（对应 deai_rules.severity）。
type LintSeverity string

const (
	LintSeverityInfo  LintSeverity = "info"
	LintSeverityWarn  LintSeverity = "warn"
	LintSeverityError LintSeverity = "error"
)

func (s LintSeverity) Valid() bool {
	switch s {
	case LintSeverityInfo, LintSeverityWarn, LintSeverityError:
		return true
	default:
		return false
	}
}

func (s LintSeverity) Label() string {
	switch s {
	case LintSeverityInfo:
		return "提示"
	case LintSeverityWarn:
		return "警告"
	case LintSeverityError:
		return "严重"
	default:
		return string(s)
	}
}

// PovRule 定义叙事视角约束（对应 style_profiles.pov_rule）。
type PovRule string

const (
	PovThirdLimited    PovRule = "third_limited"
	PovFirstPerson     PovRule = "first_person"
	PovThirdOmniscient PovRule = "third_omniscient"
)

func (p PovRule) Valid() bool {
	switch p {
	case PovThirdLimited, PovFirstPerson, PovThirdOmniscient:
		return true
	default:
		return false
	}
}

func (p PovRule) Label() string {
	switch p {
	case PovThirdLimited:
		return "第三人称限知"
	case PovFirstPerson:
		return "第一人称"
	case PovThirdOmniscient:
		return "第三人称全知"
	default:
		return string(p)
	}
}

// CriticDimension 定义独立 Critic 盲审的 4 个核验维度。
type CriticDimension string

const (
	CriticDimConsistency  CriticDimension = "consistency"
	CriticDimPoisonPoint  CriticDimension = "poison_point"
	CriticDimBeatCoverage CriticDimension = "beat_coverage"
	CriticDimHook         CriticDimension = "hook"
)

func (d CriticDimension) Valid() bool {
	switch d {
	case CriticDimConsistency, CriticDimPoisonPoint, CriticDimBeatCoverage, CriticDimHook:
		return true
	default:
		return false
	}
}

func (d CriticDimension) Label() string {
	switch d {
	case CriticDimConsistency:
		return "防吃书一致性"
	case CriticDimPoisonPoint:
		return "男频毒点排查"
	case CriticDimBeatCoverage:
		return "节拍落点核验"
	case CriticDimHook:
		return "章末钩子强检"
	default:
		return string(d)
	}
}

// DiffStatus 定义手术式局部润色建议的采纳状态。
type DiffStatus string

const (
	DiffStatusPending  DiffStatus = "pending"
	DiffStatusAccepted DiffStatus = "accepted"
	DiffStatusIgnored  DiffStatus = "ignored"
)

func (s DiffStatus) Valid() bool {
	switch s {
	case DiffStatusPending, DiffStatusAccepted, DiffStatusIgnored:
		return true
	default:
		return false
	}
}

// StyleProfile 表示单本书的量化文风指纹档案（对应 style_profiles 表）。
type StyleProfile struct {
	ID                  int64   `json:"id"`
	BookID              int64   `json:"bookId"`
	AvgSentenceChars    float64 `json:"avgSentenceChars"`
	MaxParagraphChars   int     `json:"maxParagraphChars"`
	DialogueRatioMin    float64 `json:"dialogueRatioMin"`
	DialogueRatioMax    float64 `json:"dialogueRatioMax"`
	PovRule             PovRule `json:"povRule"`
	ToneDirectives      string  `json:"toneDirectives"`
	ForbiddenTropesNote string  `json:"forbiddenTropesNote"`
	UpdatedAt           string  `json:"updatedAt"`
}

// DefaultStyleProfile 返回指定书籍的推荐默认文风指纹参数。
func DefaultStyleProfile(bookID int64) StyleProfile {
	return StyleProfile{
		BookID:            bookID,
		AvgSentenceChars:  DefaultAvgSentenceChars,
		MaxParagraphChars: DefaultMaxParagraphChars,
		DialogueRatioMin:  DefaultDialogueRatioMin,
		DialogueRatioMax:  DefaultDialogueRatioMax,
		PovRule:           PovThirdLimited,
	}
}

// UpsertStyleProfileInput 是保存书籍文风指纹的入参。
type UpsertStyleProfileInput struct {
	BookID              int64   `json:"bookId"`
	AvgSentenceChars    float64 `json:"avgSentenceChars"`
	MaxParagraphChars   int     `json:"maxParagraphChars"`
	DialogueRatioMin    float64 `json:"dialogueRatioMin"`
	DialogueRatioMax    float64 `json:"dialogueRatioMax"`
	PovRule             PovRule `json:"povRule"`
	ToneDirectives      string  `json:"toneDirectives"`
	ForbiddenTropesNote string  `json:"forbiddenTropesNote"`
}

func (in UpsertStyleProfileInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if in.AvgSentenceChars < 0 || in.AvgSentenceChars > 200 {
		return newValidationError("avgSentenceChars", "平均句长必须在 0 到 200 字之间")
	}
	if in.MaxParagraphChars < 0 || in.MaxParagraphChars > 2000 {
		return newValidationError("maxParagraphChars", "单段字数上限必须在 0 到 2000 字之间")
	}
	if in.DialogueRatioMin < 0 || in.DialogueRatioMin > 1.0 {
		return newValidationError("dialogueRatioMin", "对话占比下限必须在 0.0 到 1.0 之间")
	}
	if in.DialogueRatioMax < 0 || in.DialogueRatioMax > 1.0 {
		return newValidationError("dialogueRatioMax", "对话占比上限必须在 0.0 到 1.0 之间")
	}
	if in.DialogueRatioMax > 0 && in.DialogueRatioMin > in.DialogueRatioMax {
		return newValidationError("dialogueRatioMin", "对话占比下限不能大于上限")
	}
	if in.PovRule != "" && !in.PovRule.Valid() {
		return newValidationError("povRule", "无效的叙事视角规则")
	}
	return nil
}

// StyleSample 表示分场景样章锚点片段（对应 style_samples 表）。
type StyleSample struct {
	ID        int64     `json:"id"`
	BookID    int64     `json:"bookId"`
	SceneType SceneType `json:"sceneType"`
	Title     string    `json:"title"`
	Excerpt   string    `json:"excerpt"`
	CharCount int       `json:"charCount"`
	CreatedAt string    `json:"createdAt"`
}

// UpsertStyleSampleInput 是新增或编辑样章片段的入参。
type UpsertStyleSampleInput struct {
	ID        int64     `json:"id"`
	BookID    int64     `json:"bookId"`
	SceneType SceneType `json:"sceneType"`
	Title     string    `json:"title"`
	Excerpt   string    `json:"excerpt"`
}

func (in UpsertStyleSampleInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if !in.SceneType.Valid() {
		return newValidationError("sceneType", "无效的场景类型")
	}
	if strings.TrimSpace(in.Title) == "" {
		return newValidationError("title", "样章标题不能为空")
	}
	if strings.TrimSpace(in.Excerpt) == "" {
		return newValidationError("excerpt", "样章片段内容不能为空")
	}
	return nil
}

// DeaiRule 表示去 AI 味黑名单或结构规则（对应 deai_rules 表；BookID=0 表示全局默认规则）。
type DeaiRule struct {
	ID            int64        `json:"id"`
	BookID        int64        `json:"bookId"`
	Category      DeaiCategory `json:"category"`
	Pattern       string       `json:"pattern"`
	IsRegex       bool         `json:"isRegex"`
	Severity      LintSeverity `json:"severity"`
	MaxPerChapter int          `json:"maxPerChapter"`
	Hint          string       `json:"hint"`
	Enabled       bool         `json:"enabled"`
}

// UpsertDeaiRuleInput 是新增或更新去 AI 味规则的入参（BookID=0 表示全局规则，>0 表示书籍专属规则）。
type UpsertDeaiRuleInput struct {
	ID            int64        `json:"id"`
	BookID        int64        `json:"bookId"`
	Category      DeaiCategory `json:"category"`
	Pattern       string       `json:"pattern"`
	IsRegex       bool         `json:"isRegex"`
	Severity      LintSeverity `json:"severity"`
	MaxPerChapter int          `json:"maxPerChapter"`
	Hint          string       `json:"hint"`
	Enabled       bool         `json:"enabled"`
}

func (in UpsertDeaiRuleInput) Validate() error {
	if in.BookID < 0 {
		return newValidationError("bookId", "书籍 ID 不能为负数")
	}
	if !in.Category.Valid() {
		return newValidationError("category", "无效的规则分类")
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return newValidationError("pattern", "匹配模式不能为空")
	}
	if in.Severity != "" && !in.Severity.Valid() {
		return newValidationError("severity", "无效的告警级别")
	}
	if in.MaxPerChapter < 0 {
		return newValidationError("maxPerChapter", "单章允许上限不能为负数")
	}
	return nil
}

// LintIssue 表示 0 成本本地确定性扫描器检出的单处 AI 味或排版告警。
type LintIssue struct {
	RuleID         string       `json:"ruleId"`
	Category       DeaiCategory `json:"category"`
	Severity       LintSeverity `json:"severity"`
	ParagraphIndex int          `json:"paragraphIndex"`
	StartRune      int          `json:"startRune"`
	EndRune        int          `json:"endRune"`
	MatchedText    string       `json:"matchedText"`
	Hint           string       `json:"hint"`
}

// CriticFinding 表示独立 Critic 模型盲审发现的吃书冲突、毒点、节拍遗漏或弱钩子问题。
type CriticFinding struct {
	ID             string          `json:"id"`
	Dimension      CriticDimension `json:"dimension"`
	Severity       LintSeverity    `json:"severity"`
	ParagraphIndex int             `json:"paragraphIndex"`
	Title          string          `json:"title"`
	Detail         string          `json:"detail"`
	Suggestion     string          `json:"suggestion"`
}

// SurgicalDiff 表示针对单段病灶输出的最小手术式局部替换建议。
type SurgicalDiff struct {
	ID             string     `json:"id"`
	ParagraphIndex int        `json:"paragraphIndex"`
	Reason         string     `json:"reason"`
	Original       string     `json:"original"`
	Replacement    string     `json:"replacement"`
	Status         DiffStatus `json:"status"`
}

// ChapterAudit 表示章节质检与手术润色完整报告（对应 chapter_audits 表）。
type ChapterAudit struct {
	ID             int64           `json:"id"`
	ChapterID      int64           `json:"chapterId"`
	LintIssues     []LintIssue     `json:"lintIssues"`
	CriticFindings []CriticFinding `json:"criticFindings"`
	Diffs          []SurgicalDiff  `json:"diffs"`
	HookPassed     bool            `json:"hookPassed"`
	HookVerdict    string          `json:"hookVerdict"`
	DeaiScore      int             `json:"deaiScore"`
	UpdatedAt      string          `json:"updatedAt"`
}

// ApplySurgicalDiffInput 是作者在审改台采纳或忽略单条手术式局部润色的入参。
type ApplySurgicalDiffInput struct {
	ChapterID         int64  `json:"chapterId"`
	DiffID            string `json:"diffId"`
	Accept            bool   `json:"accept"`
	CustomReplacement string `json:"customReplacement,omitempty"`
}

func (in ApplySurgicalDiffInput) Validate() error {
	if in.ChapterID <= 0 {
		return newValidationError("chapterId", "章节 ID 必须大于 0")
	}
	if strings.TrimSpace(in.DiffID) == "" {
		return newValidationError("diffId", "手术润色项 ID 不能为空")
	}
	return nil
}

// ApplySurgicalDiffResult 返回采纳/忽略手术润色后的最新章节正文与质检报告。
type ApplySurgicalDiffResult struct {
	Chapter Chapter      `json:"chapter"`
	Audit   ChapterAudit `json:"audit"`
}
