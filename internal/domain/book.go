package domain

import (
	"strings"
	"unicode"
)

const (
	DefaultTargetChapterWords = 2500
	MinTargetChapterWords     = 300
	MaxTargetChapterWords     = 50000
	DefaultBeatTargetWords    = 500
	DefaultDailyWordGoal      = 4000
)

// Book 表示单部长篇作品根容器（对应 books 表）。
type Book struct {
	ID                 int64   `json:"id"`
	Title              string  `json:"title"`
	Genre              GenreID `json:"genre"`
	Premise            string  `json:"premise"`
	GoldenFinger       string  `json:"goldenFinger"`
	TargetChapterWords int     `json:"targetChapterWords"`
	ExportDir          string  `json:"exportDir"`
	POVMode            string  `json:"povMode"`
	PacingStyle        string  `json:"pacingStyle"`
	WritingPrompt      string  `json:"writingPrompt"`
	DailyWordGoal      int     `json:"dailyWordGoal"`
	ChapterCount       int     `json:"chapterCount"`
	FinalizedCount     int     `json:"finalizedCount"`
	TotalWords         int     `json:"totalWords"`
	CreatedAt          string  `json:"createdAt"`
	UpdatedAt          string  `json:"updatedAt"`
}

// CreateBookInput 是创建新书的入参。
type CreateBookInput struct {
	Title              string  `json:"title"`
	Genre              GenreID `json:"genre"`
	Premise            string  `json:"premise"`
	GoldenFinger       string  `json:"goldenFinger"`
	TargetChapterWords int     `json:"targetChapterWords"`
	ExportDir          string  `json:"exportDir"`
	POVMode            string  `json:"povMode,omitempty"`
	PacingStyle        string  `json:"pacingStyle,omitempty"`
	WritingPrompt      string  `json:"writingPrompt,omitempty"`
	DailyWordGoal      int     `json:"dailyWordGoal,omitempty"`
}

func (in CreateBookInput) Validate() error {
	if strings.TrimSpace(in.Title) == "" {
		return newValidationError("title", "书名不能为空")
	}
	if !in.Genre.Valid() {
		return newValidationError("genre", "不支持的题材包 ID")
	}
	if in.TargetChapterWords != 0 && (in.TargetChapterWords < MinTargetChapterWords || in.TargetChapterWords > MaxTargetChapterWords) {
		return newValidationError("targetChapterWords", "目标章字数需在 300 到 50000 之间")
	}
	if in.DailyWordGoal < 0 || in.DailyWordGoal > 200000 {
		return newValidationError("dailyWordGoal", "日更目标字数需在 0 到 200000 之间")
	}
	return nil
}

// UpdateBookInput 是修改书籍设定的入参。
type UpdateBookInput struct {
	ID                 int64   `json:"id"`
	Title              string  `json:"title"`
	Genre              GenreID `json:"genre"`
	Premise            string  `json:"premise"`
	GoldenFinger       string  `json:"goldenFinger"`
	TargetChapterWords int     `json:"targetChapterWords"`
	ExportDir          string  `json:"exportDir"`
	POVMode            string  `json:"povMode,omitempty"`
	PacingStyle        string  `json:"pacingStyle,omitempty"`
	WritingPrompt      string  `json:"writingPrompt,omitempty"`
	DailyWordGoal      int     `json:"dailyWordGoal,omitempty"`
}

func (in UpdateBookInput) Validate() error {
	if in.ID <= 0 {
		return newValidationError("id", "书籍 ID 必须大于 0")
	}
	return CreateBookInput{
		Title:              in.Title,
		Genre:              in.Genre,
		Premise:            in.Premise,
		GoldenFinger:       in.GoldenFinger,
		TargetChapterWords: in.TargetChapterWords,
		ExportDir:          in.ExportDir,
		POVMode:            in.POVMode,
		PacingStyle:        in.PacingStyle,
		WritingPrompt:      in.WritingPrompt,
		DailyWordGoal:      in.DailyWordGoal,
	}.Validate()
}

// DailyWordStat 表示单日码字产出统计。
type DailyWordStat struct {
	Date            string `json:"date"`
	WordsDelta      int    `json:"wordsDelta"`
	ChaptersTouched int    `json:"chaptersTouched"`
}

// WritingStatsSummary 表示首页与单书的写作产出总览。
type WritingStatsSummary struct {
	BookID            int64           `json:"bookId"`
	TodayWords        int             `json:"todayWords"`
	DailyGoal         int             `json:"dailyGoal"`
	StreakDays        int             `json:"streakDays"`
	TotalWords        int             `json:"totalWords"`
	TotalChapters     int             `json:"totalChapters"`
	FinalizedChapters int             `json:"finalizedChapters"`
	Daily             []DailyWordStat `json:"daily"`
}

// Volume 表示书籍的一级大弧线卷单元（对应 volumes 表）。
type Volume struct {
	ID         int64  `json:"id"`
	BookID     int64  `json:"bookId"`
	Seq        int    `json:"seq"`
	Title      string `json:"title"`
	Goal       string `json:"goal"`
	ClimaxPlan string `json:"climaxPlan"`
	CreatedAt  string `json:"createdAt"`
}

// UpsertVolumeInput 是创建或更新卷的入参。Seq 为 0 时由仓储自动顺延序号。
type UpsertVolumeInput struct {
	ID         int64  `json:"id"`
	BookID     int64  `json:"bookId"`
	Seq        int    `json:"seq"`
	Title      string `json:"title"`
	Goal       string `json:"goal"`
	ClimaxPlan string `json:"climaxPlan"`
}

func (in UpsertVolumeInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if in.Seq < 0 {
		return newValidationError("seq", "卷序号不能为负数")
	}
	if strings.TrimSpace(in.Title) == "" {
		return newValidationError("title", "卷名不能为空")
	}
	return nil
}

// Chapter 表示单章交付单元（对应 chapters 表）。
type Chapter struct {
	ID               int64         `json:"id"`
	BookID           int64         `json:"bookId"`
	VolumeID         int64         `json:"volumeId"`
	GlobalSeq        int           `json:"globalSeq"`
	VolSeq           int           `json:"volSeq"`
	Title            string        `json:"title"`
	Status           ChapterStatus `json:"status"`
	EmotionTag       EmotionTag    `json:"emotionTag"`
	HookType         HookType      `json:"hookType"`
	HookNote         string        `json:"hookNote"`
	OutlineSummary   string        `json:"outlineSummary"`
	CausalSummary    string        `json:"causalSummary"`
	Content          string        `json:"content"`
	WordCount        int           `json:"wordCount"`
	LastBriefingJSON string        `json:"lastBriefingJson"`
	StoryTimeLabel   string        `json:"storyTimeLabel"`
	StoryDay         *int          `json:"storyDay,omitempty"`
	CreatedAt        string        `json:"createdAt"`
	UpdatedAt        string        `json:"updatedAt"`
}

// CreateChapterInput 是新建章节的入参。
type CreateChapterInput struct {
	BookID         int64      `json:"bookId"`
	VolumeID       int64      `json:"volumeId"`
	Title          string     `json:"title"`
	EmotionTag     EmotionTag `json:"emotionTag"`
	HookType       HookType   `json:"hookType"`
	HookNote       string     `json:"hookNote"`
	OutlineSummary string     `json:"outlineSummary"`
}

func (in CreateChapterInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if in.VolumeID <= 0 {
		return newValidationError("volumeId", "卷 ID 必须大于 0")
	}
	if strings.TrimSpace(in.Title) == "" {
		return newValidationError("title", "章节标题不能为空")
	}
	if in.EmotionTag != "" && !in.EmotionTag.Valid() {
		return newValidationError("emotionTag", "无效的情绪极性标签")
	}
	if in.HookType != "" && !in.HookType.Valid() {
		return newValidationError("hookType", "无效的章末钩子类型")
	}
	return nil
}

// UpdateChapterMetaInput 是更新章节大纲元信息（标题/情绪/钩子/状态/故事内时间）的入参。
type UpdateChapterMetaInput struct {
	ID                  int64         `json:"id"`
	Title               string        `json:"title"`
	Status              ChapterStatus `json:"status"`
	EmotionTag          EmotionTag    `json:"emotionTag"`
	HookType            HookType      `json:"hookType"`
	HookNote            string        `json:"hookNote"`
	OutlineSummary      string        `json:"outlineSummary"`
	CausalSummary       string        `json:"causalSummary"`
	StoryTimeLabel      string        `json:"storyTimeLabel,omitempty"`
	StoryDay            *int          `json:"storyDay,omitempty"`
	UpdateStoryTimeline bool          `json:"updateStoryTimeline,omitempty"`
}

func (in UpdateChapterMetaInput) Validate() error {
	if in.ID <= 0 {
		return newValidationError("id", "章节 ID 必须大于 0")
	}
	if strings.TrimSpace(in.Title) == "" {
		return newValidationError("title", "章节标题不能为空")
	}
	if in.Status != "" && !in.Status.Valid() {
		return newValidationError("status", "无效的章节状态")
	}
	if in.EmotionTag != "" && !in.EmotionTag.Valid() {
		return newValidationError("emotionTag", "无效的情绪极性标签")
	}
	if in.HookType != "" && !in.HookType.Valid() {
		return newValidationError("hookType", "无效的章末钩子类型")
	}
	return nil
}

// SaveChapterDraftInput 是保存正文草稿的入参。
type SaveChapterDraftInput struct {
	ChapterID    int64         `json:"chapterId"`
	Title        string        `json:"title,omitempty"`
	Content      string        `json:"content"`
	Status       ChapterStatus `json:"status,omitempty"`
	BackupSource VersionSource `json:"backupSource,omitempty"`
	ModelName    string        `json:"modelName,omitempty"`
}

func (in SaveChapterDraftInput) Validate() error {
	if in.ChapterID <= 0 {
		return newValidationError("chapterId", "章节 ID 必须大于 0")
	}
	if in.Status != "" && !in.Status.Valid() {
		return newValidationError("status", "无效的章节状态")
	}
	if in.BackupSource != "" && !in.BackupSource.Valid() {
		return newValidationError("backupSource", "无效的快照来源类型")
	}
	return nil
}

// Beat 表示章节内部的一张剧情节拍卡（对应 chapter_beats 表）。
type Beat struct {
	ID           int64     `json:"id"`
	ChapterID    int64     `json:"chapterId"`
	Seq          int       `json:"seq"`
	Stage        BeatStage `json:"stage"`
	Summary      string    `json:"summary"`
	ConflictNote string    `json:"conflictNote"`
	POVEntityID  *int64    `json:"povEntityId,omitempty"`
	EntityIDs    []int64   `json:"entityIds"`
	TargetWords  int       `json:"targetWords"`
}

func (b Beat) Validate() error {
	if !b.Stage.Valid() {
		return newValidationError("stage", "无效的节拍阶段")
	}
	if strings.TrimSpace(b.Summary) == "" {
		return newValidationError("summary", "节拍摘要不能为空")
	}
	if b.TargetWords < 0 {
		return newValidationError("targetWords", "节拍目标字数不能为负数")
	}
	if b.POVEntityID != nil && *b.POVEntityID <= 0 {
		return newValidationError("povEntityId", "POV 实体 ID 必须大于 0")
	}
	return nil
}

// ChapterVersion 表示章节正文的历史版本快照（对应 chapter_versions 表）。
type ChapterVersion struct {
	ID        int64         `json:"id"`
	ChapterID int64         `json:"chapterId"`
	Source    VersionSource `json:"source"`
	ModelName string        `json:"modelName"`
	Content   string        `json:"content"`
	WordCount int           `json:"wordCount"`
	CreatedAt string        `json:"createdAt"`
}

// ExportResult 表示单向 Markdown 镜像导出结果。
type ExportResult struct {
	ExportDir    string   `json:"exportDir"`
	BookDir      string   `json:"bookDir"`
	FilesWritten []string `json:"filesWritten"`
	ChapterCount int      `json:"chapterCount"`
}

// InsertChapterInput 表示在指定卷中某章之后（或卷首）插入新章节的入参。
type InsertChapterInput struct {
	BookID         int64      `json:"bookId"`
	VolumeID       int64      `json:"volumeId"`
	AfterChapterID int64      `json:"afterChapterId,omitempty"`
	Title          string     `json:"title"`
	EmotionTag     EmotionTag `json:"emotionTag"`
	HookType       HookType   `json:"hookType"`
	HookNote       string     `json:"hookNote"`
	OutlineSummary string     `json:"outlineSummary"`
}

func (in InsertChapterInput) Validate() error {
	return CreateChapterInput{
		BookID:         in.BookID,
		VolumeID:       in.VolumeID,
		Title:          in.Title,
		EmotionTag:     in.EmotionTag,
		HookType:       in.HookType,
		HookNote:       in.HookNote,
		OutlineSummary: in.OutlineSummary,
	}.Validate()
}

// MoveChapterInput 表示调整章节顺序或跨卷移动的入参。
// Direction 支持 "up" / "down"；若指定 TargetVolumeID > 0，则将章节移动到目标卷（可选 TargetVolSeq，<=0 表示卷尾）。
type MoveChapterInput struct {
	ChapterID      int64  `json:"chapterId"`
	Direction      string `json:"direction,omitempty"`
	TargetVolumeID int64  `json:"targetVolumeId,omitempty"`
	TargetVolSeq   int    `json:"targetVolSeq,omitempty"`
}

func (in MoveChapterInput) Validate() error {
	if in.ChapterID <= 0 {
		return newValidationError("chapterId", "章节 ID 必须大于 0")
	}
	dir := strings.ToLower(strings.TrimSpace(in.Direction))
	if dir != "" && dir != "up" && dir != "down" {
		return newValidationError("direction", "移动方向仅支持 up 或 down")
	}
	if dir == "" && in.TargetVolumeID <= 0 {
		return newValidationError("direction", "请指定移动方向或目标分卷")
	}
	return nil
}

// ReplaceBookTextInput 表示全书范围查找并替换文本的入参（覆写每章正文前强制归档快照）。
type ReplaceBookTextInput struct {
	BookID          int64  `json:"bookId"`
	Find            string `json:"find"`
	Replace         string `json:"replace"`
	IncludeTitles   bool   `json:"includeTitles,omitempty"`
	IncludeOutlines bool   `json:"includeOutlines,omitempty"`
	DryRun          bool   `json:"dryRun,omitempty"`
}

func (in ReplaceBookTextInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if in.Find == "" {
		return newValidationError("find", "查找内容不能为空")
	}
	return nil
}

// ReplaceBookTextResult 表示全书查找替换结果。
type ReplaceBookTextResult struct {
	AffectedChapters int `json:"affectedChapters"`
	TotalMatches     int `json:"totalMatches"`
}

// ImportBookResult 表示从本地 TXT/Markdown 文件拆章导入书籍的结果。
type ImportBookResult struct {
	Book          Book   `json:"book"`
	Volume        Volume `json:"volume"`
	ChapterCount  int    `json:"chapterCount"`
	TotalWords    int    `json:"totalWords"`
	SourcePath    string `json:"sourcePath"`
}

// CountWords 按中文网文习惯统计正文字数（剔除空白字符后的 Unicode 字符数）。
func CountWords(text string) int {
	n := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
