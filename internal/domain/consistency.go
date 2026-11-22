package domain

import "strings"

// LocalAuditFindingCategory 定义第一层 0 成本本地全书秒扫的诊断类别。
type LocalAuditFindingCategory string

const (
	LocalAuditDeadReappear      LocalAuditFindingCategory = "dead_reappear"
	LocalAuditAbsentEntity      LocalAuditFindingCategory = "absent_entity"
	LocalAuditOverdueForeshadow LocalAuditFindingCategory = "overdue_foreshadow"
	LocalAuditTimelineAnomaly   LocalAuditFindingCategory = "timeline_anomaly"
	LocalAuditPendingDelta      LocalAuditFindingCategory = "pending_delta"
	LocalAuditUpstreamDirty     LocalAuditFindingCategory = "upstream_dirty"
)

// LocalAuditSeverity 定义一致性体检条目的严重程度。
type LocalAuditSeverity string

const (
	LocalAuditSeverityError LocalAuditSeverity = "error"
	LocalAuditSeverityWarn  LocalAuditSeverity = "warn"
	LocalAuditSeverityInfo  LocalAuditSeverity = "info"
)

// LocalAuditOptions 配置本地秒扫阈值参数。
type LocalAuditOptions struct {
	AbsentThresholdChapters int `json:"absentThresholdChapters"`
	MaxStoryDayJump         int `json:"maxStoryDayJump"`
}

// LocalAuditFinding 表示单条 0 成本本地体检问题与精准跳转位置。
type LocalAuditFinding struct {
	ID              string                    `json:"id"`
	Category        LocalAuditFindingCategory `json:"category"`
	Severity        LocalAuditSeverity        `json:"severity"`
	Title           string                    `json:"title"`
	Detail          string                    `json:"detail"`
	Suggestion      string                    `json:"suggestion"`
	ChapterID       int64                     `json:"chapterId"`
	ChapterSeq      int                       `json:"chapterSeq"`
	ChapterTitle    string                    `json:"chapterTitle"`
	EntityID        int64                     `json:"entityId"`
	EntityName      string                    `json:"entityName"`
	ForeshadowingID int64                     `json:"foreshadowingId"`
	DeadlineID      int64                     `json:"deadlineId"`
	JumpTarget      string                    `json:"jumpTarget"` // "studio" | "architect" | "atlas_entity" | "atlas_foreshadow" | "dirty_queue"
}

// LocalAuditSummary 汇总本地秒扫各类别命中统计。
type LocalAuditSummary struct {
	TotalFindings          int `json:"totalFindings"`
	ErrorCount             int `json:"errorCount"`
	WarnCount              int `json:"warnCount"`
	InfoCount              int `json:"infoCount"`
	DeadReappearCount      int `json:"deadReappearCount"`
	AbsentEntityCount      int `json:"absentEntityCount"`
	OverdueForeshadowCount int `json:"overdueForeshadowCount"`
	TimelineAnomalyCount   int `json:"timelineAnomalyCount"`
	PendingDeltaCount      int `json:"pendingDeltaCount"`
	UpstreamDirtyCount     int `json:"upstreamDirtyCount"`
}

// LocalAuditReport 是第一层 0 成本本地全书秒扫的完整报告。
type LocalAuditReport struct {
	BookID           int64               `json:"bookId"`
	LatestChapterSeq int                 `json:"latestChapterSeq"`
	ScannedChapters  int                 `json:"scannedChapters"`
	ScannedEntities  int                 `json:"scannedEntities"`
	ElapsedMs        int64               `json:"elapsedMs"`
	Summary          LocalAuditSummary   `json:"summary"`
	Findings         []LocalAuditFinding `json:"findings"`
}

// ConsistencyAuditScopeType 定义第二层 AI 深度语义审计的覆盖范围。
type ConsistencyAuditScopeType string

const (
	ConsistencyScopeVolume ConsistencyAuditScopeType = "volume"
	ConsistencyScopeRange  ConsistencyAuditScopeType = "range"
	ConsistencyScopeBook   ConsistencyAuditScopeType = "book"
)

func (s ConsistencyAuditScopeType) Valid() bool {
	switch s {
	case ConsistencyScopeVolume, ConsistencyScopeRange, ConsistencyScopeBook:
		return true
	default:
		return false
	}
}

// ConsistencyFindingStatus 定义 AI 深度语义审计条目的处理状态。
type ConsistencyFindingStatus string

const (
	ConsistencyFindingOpen     ConsistencyFindingStatus = "open"
	ConsistencyFindingResolved ConsistencyFindingStatus = "resolved"
	ConsistencyFindingIgnored  ConsistencyFindingStatus = "ignored"
)

func (s ConsistencyFindingStatus) Valid() bool {
	switch s {
	case ConsistencyFindingOpen, ConsistencyFindingResolved, ConsistencyFindingIgnored:
		return true
	default:
		return false
	}
}

// AIConsistencyFinding 表示 AI 深度语义审计识别到的跨章深层逻辑矛盾。
type AIConsistencyFinding struct {
	ID          string                   `json:"id"`
	Category    string                   `json:"category"` // "power_collapse" | "motivation_conflict" | "rule_contradiction" | "timeline_logic" | "other"
	Severity    LocalAuditSeverity       `json:"severity"`
	Title       string                   `json:"title"`
	Description string                   `json:"description"`
	Evidence    string                   `json:"evidence"`
	Suggestion  string                   `json:"suggestion"`
	ChapterSeqs []int                    `json:"chapterSeqs"`
	EntityNames []string                 `json:"entityNames"`
	Status      ConsistencyFindingStatus `json:"status"`
}

// BookConsistencyReport 对应持久化表 book_consistency_reports。
type BookConsistencyReport struct {
	ID         int64                     `json:"id"`
	BookID     int64                     `json:"bookId"`
	ScopeType  ConsistencyAuditScopeType `json:"scopeType"`
	ScopeRefID int64                     `json:"scopeRefId"`
	StartSeq   int                       `json:"startSeq"`
	EndSeq     int                       `json:"endSeq"`
	ModelName  string                    `json:"modelName"`
	Summary    string                    `json:"summary"`
	Findings   []AIConsistencyFinding    `json:"findings"`
	CreatedAt  string                    `json:"createdAt"`
}

// RunAIConsistencyAuditRequest 是触发第二层 AI 深度语义审计的入参。
type RunAIConsistencyAuditRequest struct {
	BookID    int64                     `json:"bookId"`
	ScopeType ConsistencyAuditScopeType `json:"scopeType"`
	VolumeID  int64                     `json:"volumeId"`
	StartSeq  int                       `json:"startSeq"`
	EndSeq    int                       `json:"endSeq"`
}

func (r RunAIConsistencyAuditRequest) Validate() error {
	if r.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if !r.ScopeType.Valid() {
		return newValidationError("scopeType", "无效的语义审计范围类型")
	}
	if r.ScopeType == ConsistencyScopeVolume && r.VolumeID <= 0 {
		return newValidationError("volumeId", "按卷审计时必须选择分卷")
	}
	if r.ScopeType == ConsistencyScopeRange {
		if r.StartSeq <= 0 {
			return newValidationError("startSeq", "按区间审计时起始章号必须大于 0")
		}
		if r.EndSeq < r.StartSeq {
			return newValidationError("endSeq", "结束章号不能早于起始章号")
		}
	}
	return nil
}

// DirtyChapterItem 描述待串联修复的单条 upstream_dirty 章节及其上游变更原因。
type DirtyChapterItem struct {
	ChapterID         int64  `json:"chapterId"`
	VolumeID          int64  `json:"volumeId"`
	GlobalSeq         int    `json:"globalSeq"`
	Title             string `json:"title"`
	WordCount         int    `json:"wordCount"`
	HasContent        bool   `json:"hasContent"`
	HasPendingDelta   bool   `json:"hasPendingDelta"`
	LastAuditFindings int    `json:"lastAuditFindings"`
	UpstreamSummary   string `json:"upstreamSummary"`
	UpstreamSourceSeq int    `json:"upstreamSourceSeq"`
	UpdatedAt         string `json:"updatedAt"`
}

// ClearDirtyChaptersResult 汇总批量/单章消标结果。
type ClearDirtyChaptersResult struct {
	ClearedCount      int     `json:"clearedCount"`
	ClearedChapterIDs []int64 `json:"clearedChapterIds"`
}

// Normalize 确保切片字段非 nil，避免 JSON 序列化为 null。
func (r *BookConsistencyReport) Normalize() {
	if r == nil {
		return
	}
	if r.Findings == nil {
		r.Findings = []AIConsistencyFinding{}
	}
	for i := range r.Findings {
		if r.Findings[i].ChapterSeqs == nil {
			r.Findings[i].ChapterSeqs = []int{}
		}
		if r.Findings[i].EntityNames == nil {
			r.Findings[i].EntityNames = []string{}
		}
		if strings.TrimSpace(string(r.Findings[i].Status)) == "" {
			r.Findings[i].Status = ConsistencyFindingOpen
		}
	}
}
