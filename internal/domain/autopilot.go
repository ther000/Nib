package domain

import "strings"

const (
	DefaultAutopilotChapters = 3
	MinAutopilotChapters     = 1
	MaxAutopilotChapters     = 20
)

// AutopilotStatus 表示全自动托管流水线的当前生命周期状态。
type AutopilotStatus string

const (
	AutopilotStatusIdle            AutopilotStatus = "idle"
	AutopilotStatusRunning         AutopilotStatus = "running"
	AutopilotStatusWaitingDecision AutopilotStatus = "waiting_decision"
	AutopilotStatusCompleted       AutopilotStatus = "completed"
	AutopilotStatusStopped         AutopilotStatus = "stopped"
	AutopilotStatusFailed          AutopilotStatus = "failed"
)

// AutopilotStep 表示托管推进到单章时的子阶段。
type AutopilotStep string

const (
	AutopilotStepPreparing       AutopilotStep = "preparing"
	AutopilotStepOutlining       AutopilotStep = "outlining"
	AutopilotStepDrafting        AutopilotStep = "drafting"
	AutopilotStepLinting         AutopilotStep = "linting"
	AutopilotStepCritiquing      AutopilotStep = "critiquing"
	AutopilotStepPolishing       AutopilotStep = "polishing"
	AutopilotStepExtracting      AutopilotStep = "extracting"
	AutopilotStepCommitting      AutopilotStep = "committing"
	AutopilotStepWaitingDecision AutopilotStep = "waiting_decision"
	AutopilotStepDone            AutopilotStep = "done"
)

// AutopilotDecisionAction 表示托管遇阻时作者在决策闸门中的选择。
type AutopilotDecisionAction string

const (
	AutopilotDecisionContinue     AutopilotDecisionAction = "continue"
	AutopilotDecisionRetryChapter AutopilotDecisionAction = "retry_chapter"
	AutopilotDecisionSkipChapter  AutopilotDecisionAction = "skip_chapter"
	AutopilotDecisionStop         AutopilotDecisionAction = "stop"
)

func (a AutopilotDecisionAction) Valid() bool {
	switch a {
	case AutopilotDecisionContinue,
		AutopilotDecisionRetryChapter,
		AutopilotDecisionSkipChapter,
		AutopilotDecisionStop:
		return true
	default:
		return false
	}
}

// AutopilotRequest 是启动全自动托管写作的入参。
type AutopilotRequest struct {
	BookID           int64  `json:"bookId"`
	VolumeID         int64  `json:"volumeId"`
	TargetChapters   int    `json:"targetChapters"`
	AutoOutline      bool   `json:"autoOutline"`
	AutoPolish       bool   `json:"autoPolish"`
	AutoCommitMemory bool   `json:"autoCommitMemory"`
	MaxTokensBudget  int    `json:"maxTokensBudget"`
	ArcGoal          string `json:"arcGoal"`
}

func (r AutopilotRequest) Validate() error {
	if r.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if r.VolumeID < 0 {
		return newValidationError("volumeId", "卷 ID 不能为负数")
	}
	if r.TargetChapters != 0 && (r.TargetChapters < MinAutopilotChapters || r.TargetChapters > MaxAutopilotChapters) {
		return newValidationError("targetChapters", "托管章数需在 1 到 20 章之间")
	}
	if r.MaxTokensBudget < 0 {
		return newValidationError("maxTokensBudget", "Token 上限不能为负数")
	}
	return nil
}

// AutopilotGateOption 描述决策闸门中的一个可选操作按钮。
type AutopilotGateOption struct {
	Action      AutopilotDecisionAction `json:"action"`
	Label       string                  `json:"label"`
	Description string                  `json:"description"`
}

// AutopilotGate 表示托管过程中需要作者拍板的挂起状态。
type AutopilotGate struct {
	Reason     string                `json:"reason"`
	Title      string                `json:"title"`
	Detail     string                `json:"detail"`
	ChapterID  int64                 `json:"chapterId"`
	ChapterSeq int                   `json:"chapterSeq"`
	Options    []AutopilotGateOption `json:"options"`
}

// AutopilotLogEntry 记录托管流水线的可读步骤轨迹。
type AutopilotLogEntry struct {
	Time       string        `json:"time"`
	ChapterSeq int           `json:"chapterSeq"`
	Step       AutopilotStep `json:"step"`
	Tone       string        `json:"tone"` // "info" | "ok" | "warn" | "danger"
	Message    string        `json:"message"`
}

// AutopilotState 是全自动托管流水线的实时状态快照（供前端状态条与控制台展示，同时持久化到 autopilot_runs）。
type AutopilotState struct {
	RunID               int64               `json:"runId,omitempty"`
	BookID              int64               `json:"bookId"`
	VolumeID            int64               `json:"volumeId,omitempty"`
	Status              AutopilotStatus     `json:"status"`
	Step                AutopilotStep       `json:"step"`
	TargetChapters      int                 `json:"targetChapters"`
	CompletedChapters   int                 `json:"completedChapters"`
	AutoOutline         bool                `json:"autoOutline"`
	AutoPolish          bool                `json:"autoPolish"`
	CurrentChapterID    int64               `json:"currentChapterId"`
	CurrentChapterSeq   int                 `json:"currentChapterSeq"`
	CurrentChapterTitle string              `json:"currentChapterTitle"`
	TokensUsed          int                 `json:"tokensUsed"`
	MaxTokensBudget     int                 `json:"maxTokensBudget"`
	AutoCommitMemory    bool                `json:"autoCommitMemory"`
	ArcGoal             string              `json:"arcGoal,omitempty"`
	Message             string              `json:"message"`
	Error               string              `json:"error,omitempty"`
	Gate                *AutopilotGate      `json:"gate,omitempty"`
	Logs                []AutopilotLogEntry `json:"logs"`
	StartedAt           string              `json:"startedAt,omitempty"`
	UpdatedAt           string              `json:"updatedAt,omitempty"`
}

func (s AutopilotState) Normalized() AutopilotState {
	if s.Logs == nil {
		s.Logs = []AutopilotLogEntry{}
	}
	if s.Status == "" {
		s.Status = AutopilotStatusIdle
	}
	if s.Gate != nil && s.Gate.Options == nil {
		s.Gate.Options = []AutopilotGateOption{}
	}
	s.Message = strings.TrimSpace(s.Message)
	return s
}
