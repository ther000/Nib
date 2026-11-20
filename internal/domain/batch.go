package domain

import (
	"fmt"
	"slices"
	"strings"
)

const (
	DefaultBatchOutlineCount = 5
	MinBatchOutlineCount     = 1
	MaxBatchOutlineCount     = 10

	// StageStyle 用于 token_usage_logs.stage 记录样章文风抽取的 Token 消耗。
	StageStyle PipelineStage = "style"
)

// ValidUsageStage 判定 stage 是否为合法的 Token 统计环节（5 个路由环节 + style）。
func (s PipelineStage) ValidUsageStage() bool {
	return s.Valid() || s == StageStyle
}

// BatchMode 定义串行微批生成队列的执行深度档位（对应 batch_runs.mode）。
type BatchMode string

const (
	BatchModeDraftOnly      BatchMode = "draft_only"
	BatchModeDraftAndPolish BatchMode = "draft_and_polish"
	BatchModeFullPipeline   BatchMode = "full_pipeline"
)

func (m BatchMode) Valid() bool {
	switch m {
	case BatchModeDraftOnly, BatchModeDraftAndPolish, BatchModeFullPipeline:
		return true
	default:
		return false
	}
}

func (m BatchMode) Label() string {
	switch m {
	case BatchModeDraftOnly:
		return "仅出草稿 + 本地 Lint"
	case BatchModeDraftAndPolish:
		return "草稿 + Critic 审校 + 手术润色"
	case BatchModeFullPipeline:
		return "全流程（含完章记忆抽取）"
	default:
		return string(m)
	}
}

// BatchRunStatus 定义串行微批任务的整体运行状态（对应 batch_runs.status）。
type BatchRunStatus string

const (
	BatchRunRunning   BatchRunStatus = "running"
	BatchRunPaused    BatchRunStatus = "paused"
	BatchRunCompleted BatchRunStatus = "completed"
	BatchRunCanceled  BatchRunStatus = "canceled"
	BatchRunFailed    BatchRunStatus = "failed"
)

func (s BatchRunStatus) Valid() bool {
	switch s {
	case BatchRunRunning, BatchRunPaused, BatchRunCompleted, BatchRunCanceled, BatchRunFailed:
		return true
	default:
		return false
	}
}

// BatchItemState 定义微批队列中单章的执行子状态机（对应 batch_run_items.state）。
type BatchItemState string

const (
	BatchItemQueued         BatchItemState = "queued"
	BatchItemCompiling      BatchItemState = "compiling"
	BatchItemDrafting       BatchItemState = "drafting"
	BatchItemLinting        BatchItemState = "linting"
	BatchItemCritiquing     BatchItemState = "critiquing"
	BatchItemPolishing      BatchItemState = "polishing"
	BatchItemExtracting     BatchItemState = "extracting"
	BatchItemAwaitingReview BatchItemState = "awaiting_review"
	BatchItemFailed         BatchItemState = "failed"
	BatchItemCanceled       BatchItemState = "canceled"
)

func (s BatchItemState) Valid() bool {
	switch s {
	case BatchItemQueued,
		BatchItemCompiling,
		BatchItemDrafting,
		BatchItemLinting,
		BatchItemCritiquing,
		BatchItemPolishing,
		BatchItemExtracting,
		BatchItemAwaitingReview,
		BatchItemFailed,
		BatchItemCanceled:
		return true
	default:
		return false
	}
}

// PacingAlertKind 定义卷级情绪节奏诊断的 3 类告警。
type PacingAlertKind string

const (
	PacingAlertSuppressOverload  PacingAlertKind = "suppress_overload"
	PacingAlertMissingBurst      PacingAlertKind = "missing_burst"
	PacingAlertOverdueForeshadow PacingAlertKind = "overdue_foreshadowing"
)

func (k PacingAlertKind) Valid() bool {
	switch k {
	case PacingAlertSuppressOverload, PacingAlertMissingBurst, PacingAlertOverdueForeshadow:
		return true
	default:
		return false
	}
}

// PacingPoint 表示卷情绪折线图上的单个章节节点。
type PacingPoint struct {
	ChapterID  int64         `json:"chapterId"`
	GlobalSeq  int           `json:"globalSeq"`
	VolSeq     int           `json:"volSeq"`
	Title      string        `json:"title"`
	EmotionTag EmotionTag    `json:"emotionTag"`
	Weight     int           `json:"weight"`
	HookType   HookType      `json:"hookType"`
	Status     ChapterStatus `json:"status"`
}

// PacingAlert 表示卷级情绪曲线自动诊断出的一条节奏或伏笔超期预警。
type PacingAlert struct {
	Kind            PacingAlertKind `json:"kind"`
	Severity        LintSeverity    `json:"severity"`
	ChapterIDs      []int64         `json:"chapterIds"`
	StartSeq        int             `json:"startSeq"`
	EndSeq          int             `json:"endSeq"`
	Message         string          `json:"message"`
	ForeshadowingID int64           `json:"foreshadowingId,omitempty"`
}

// VolumePacingReport 汇总单卷的章节情绪折线数据与诊断告警列表。
type VolumePacingReport struct {
	VolumeID int64         `json:"volumeId"`
	Points   []PacingPoint `json:"points"`
	Alerts   []PacingAlert `json:"alerts"`
}

// DiagnoseVolumePacing 按 docs/03-core-engines.md §2.1 执行确定性卷级节奏诊断：
// 1. suppress_overload：连续 >= 3 章处于 suppress 且无 turn/burst；
// 2. missing_burst：连续 >= 5 章峰值权重 < 4（未出现 burst）；
// 3. overdue_foreshadowing：处于 planted/hinted 的伏笔满足 target_resolve_seq > 0 且 maxGlobalSeq >= target_resolve_seq。
func DiagnoseVolumePacing(volumeID int64, chapters []Chapter, openForeshadowings []Foreshadowing) VolumePacingReport {
	sorted := slices.Clone(chapters)
	slices.SortFunc(sorted, func(a, b Chapter) int {
		if a.GlobalSeq != b.GlobalSeq {
			return a.GlobalSeq - b.GlobalSeq
		}
		if a.VolSeq != b.VolSeq {
			return a.VolSeq - b.VolSeq
		}
		return int(a.ID - b.ID)
	})

	points := make([]PacingPoint, 0, len(sorted))
	alerts := make([]PacingAlert, 0)

	var (
		suppressRun []Chapter
		noBurstRun  []Chapter
		maxGlobal   int
	)

	flushSuppressRun := func() {
		if len(suppressRun) >= 3 {
			ids := make([]int64, len(suppressRun))
			for i, ch := range suppressRun {
				ids[i] = ch.ID
			}
			startSeq := suppressRun[0].GlobalSeq
			endSeq := suppressRun[len(suppressRun)-1].GlobalSeq
			alerts = append(alerts, PacingAlert{
				Kind:       PacingAlertSuppressOverload,
				Severity:   LintSeverityWarn,
				ChapterIDs: ids,
				StartSeq:   startSeq,
				EndSeq:     endSeq,
				Message:    fmt.Sprintf("第 %d~%d 章连续 %d 章蓄压受阻（suppress），建议插入小反转或利益预支以防劝退读者", startSeq, endSeq, len(suppressRun)),
			})
		}
		suppressRun = nil
	}

	flushNoBurstRun := func() {
		if len(noBurstRun) >= 5 {
			ids := make([]int64, len(noBurstRun))
			for i, ch := range noBurstRun {
				ids[i] = ch.ID
			}
			startSeq := noBurstRun[0].GlobalSeq
			endSeq := noBurstRun[len(noBurstRun)-1].GlobalSeq
			alerts = append(alerts, PacingAlert{
				Kind:       PacingAlertMissingBurst,
				Severity:   LintSeverityWarn,
				ChapterIDs: ids,
				StartSeq:   startSeq,
				EndSeq:     endSeq,
				Message:    fmt.Sprintf("第 %d~%d 章连续 %d 章缺少高潮爆发（burst），本单元缺少阶段爽点峰值", startSeq, endSeq, len(noBurstRun)),
			})
		}
		noBurstRun = nil
	}

	for _, ch := range sorted {
		if ch.GlobalSeq > maxGlobal {
			maxGlobal = ch.GlobalSeq
		}
		w := ch.EmotionTag.Weight()
		points = append(points, PacingPoint{
			ChapterID:  ch.ID,
			GlobalSeq:  ch.GlobalSeq,
			VolSeq:     ch.VolSeq,
			Title:      ch.Title,
			EmotionTag: ch.EmotionTag,
			Weight:     w,
			HookType:   ch.HookType,
			Status:     ch.Status,
		})

		if ch.EmotionTag == EmotionSuppress {
			suppressRun = append(suppressRun, ch)
		} else {
			flushSuppressRun()
		}

		if w < EmotionBurst.Weight() {
			noBurstRun = append(noBurstRun, ch)
		} else {
			flushNoBurstRun()
		}
	}
	flushSuppressRun()
	flushNoBurstRun()

	if maxGlobal > 0 {
		lastChapID := sorted[len(sorted)-1].ID
		for _, fs := range openForeshadowings {
			if fs.Status != ForeshadowingPlanted && fs.Status != ForeshadowingHinted {
				continue
			}
			if fs.TargetResolveSeq > 0 && maxGlobal >= fs.TargetResolveSeq {
				sev := LintSeverityWarn
				if maxGlobal > fs.TargetResolveSeq {
					sev = LintSeverityError
				}
				alerts = append(alerts, PacingAlert{
					Kind:            PacingAlertOverdueForeshadow,
					Severity:        sev,
					ChapterIDs:      []int64{lastChapID},
					StartSeq:        fs.TargetResolveSeq,
					EndSeq:          maxGlobal,
					Message:         fmt.Sprintf("伏笔「%s」计划在第 %d 章回收，当前已推进至第 %d 章，请安排收网或推进暗示", fs.Title, fs.TargetResolveSeq, maxGlobal),
					ForeshadowingID: fs.ID,
				})
			}
		}
	}

	return VolumePacingReport{
		VolumeID: volumeID,
		Points:   points,
		Alerts:   alerts,
	}
}

// BatchOutlineRequest 是批量生成 1~10 章结构化章纲与节拍卡的入参。
type BatchOutlineRequest struct {
	BookID   int64  `json:"bookId"`
	VolumeID int64  `json:"volumeId"`
	StartSeq int    `json:"startSeq"`
	Count    int    `json:"count"`
	ArcGoal  string `json:"arcGoal"`
}

func (r BatchOutlineRequest) Validate() error {
	if r.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if r.VolumeID <= 0 {
		return newValidationError("volumeId", "卷 ID 必须大于 0")
	}
	if r.StartSeq < 0 {
		return newValidationError("startSeq", "起始章号不能为负数")
	}
	if r.Count != 0 && (r.Count < MinBatchOutlineCount || r.Count > MaxBatchOutlineCount) {
		return newValidationError("count", "单次批量生成章纲数量必须在 1 到 10 章之间")
	}
	return nil
}

// GeneratedChapterOutline 表示批量大纲生成出的单章结构化章纲与节拍草案。
type GeneratedChapterOutline struct {
	Title          string     `json:"title"`
	EmotionTag     EmotionTag `json:"emotionTag"`
	HookType       HookType   `json:"hookType"`
	HookNote       string     `json:"hookNote"`
	OutlineSummary string     `json:"outlineSummary"`
	Beats          []Beat     `json:"beats"`
}

// OutlineChunk 是通过 *mygo.Channel[OutlineChunk] 流式推送给编排台的批量章纲生成分片。
type OutlineChunk struct {
	Kind     GenChunkKind              `json:"kind"`
	Delta    string                    `json:"delta,omitempty"`
	Outlines []GeneratedChapterOutline `json:"outlines,omitempty"`
	Chapters []Chapter                 `json:"chapters,omitempty"`
	Usage    *TokenUsage               `json:"usage,omitempty"`
	Error    string                    `json:"error,omitempty"`
}

// BatchRunItem 表示串行微批队列中的单章排队与执行状态明细（对应 batch_run_items 表）。
type BatchRunItem struct {
	ID               int64          `json:"id"`
	BatchRunID       int64          `json:"batchRunId"`
	ChapterID        int64          `json:"chapterId"`
	ChapterGlobalSeq int            `json:"chapterGlobalSeq"`
	ChapterTitle     string         `json:"chapterTitle"`
	Seq              int            `json:"seq"`
	State            BatchItemState `json:"state"`
	InputHash        string         `json:"inputHash"`
	ErrorMsg         string         `json:"errorMsg"`
	StartedAt        string         `json:"startedAt"`
	FinishedAt       string         `json:"finishedAt"`
}

// BatchRun 表示一次 1~5 章串行接力微批调度任务容器（对应 batch_runs 表）。
type BatchRun struct {
	ID         int64          `json:"id"`
	BookID     int64          `json:"bookId"`
	Mode       BatchMode      `json:"mode"`
	Status     BatchRunStatus `json:"status"`
	TotalItems int            `json:"totalItems"`
	DoneItems  int            `json:"doneItems"`
	Items      []BatchRunItem `json:"items"`
	CreatedAt  string         `json:"createdAt"`
	FinishedAt string         `json:"finishedAt"`
}

// BatchProgress 是通过 mygo.NewEvent[BatchProgress]("batch:progress") 实时广播给前端的微批进度载荷。
type BatchProgress struct {
	BatchRunID        int64          `json:"batchRunId"`
	BookID            int64          `json:"bookId"`
	Status            BatchRunStatus `json:"status"`
	TotalItems        int            `json:"totalItems"`
	DoneItems         int            `json:"doneItems"`
	CurrentItemID     int64          `json:"currentItemId"`
	CurrentChapterID  int64          `json:"currentChapterId"`
	CurrentChapterSeq int            `json:"currentChapterSeq"`
	CurrentState      BatchItemState `json:"currentState"`
	Message           string         `json:"message"`
	UpdatedAt         string         `json:"updatedAt"`
}

// TokenUsageLog 表示单次模型调用的 Token 与耗时流水（对应 token_usage_logs 表）。
type TokenUsageLog struct {
	ID               int64         `json:"id"`
	BookID           int64         `json:"bookId"`
	ChapterID        int64         `json:"chapterId"`
	Stage            PipelineStage `json:"stage"`
	ProviderName     string        `json:"providerName"`
	ModelName        string        `json:"modelName"`
	PromptTokens     int           `json:"promptTokens"`
	CompletionTokens int           `json:"completionTokens"`
	LatencyMs        int64         `json:"latencyMs"`
	CreatedAt        string        `json:"createdAt"`
}

func (l TokenUsageLog) Validate() error {
	if l.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if !l.Stage.ValidUsageStage() {
		return newValidationError("stage", "无效的流水线统计阶段")
	}
	if strings.TrimSpace(l.ProviderName) == "" {
		return newValidationError("providerName", "供应商名称不能为空")
	}
	if strings.TrimSpace(l.ModelName) == "" {
		return newValidationError("modelName", "模型名称不能为空")
	}
	if l.PromptTokens < 0 || l.CompletionTokens < 0 || l.LatencyMs < 0 {
		return newValidationError("tokens", "Token 数与耗时不能为负数")
	}
	return nil
}

// StageUsageStat 按创作环节（outline/draft/critic/polish/extract/style）聚合 Token 消耗与平均延迟。
type StageUsageStat struct {
	Stage            string `json:"stage"`
	Calls            int    `json:"calls"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	TotalTokens      int    `json:"totalTokens"`
	AvgLatencyMs     int64  `json:"avgLatencyMs"`
}

// ModelUsageStat 按供应商与模型名称聚合 Token 消耗。
type ModelUsageStat struct {
	ProviderName     string `json:"providerName"`
	ModelName        string `json:"modelName"`
	Calls            int    `json:"calls"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	TotalTokens      int    `json:"totalTokens"`
}

// UsageSummary 汇总整本书（或全局）的 Token 消耗统计面板数据。
type UsageSummary struct {
	BookID                int64            `json:"bookId"`
	TotalCalls            int              `json:"totalCalls"`
	TotalPromptTokens     int              `json:"totalPromptTokens"`
	TotalCompletionTokens int              `json:"totalCompletionTokens"`
	TotalTokens           int              `json:"totalTokens"`
	ByStage               []StageUsageStat `json:"byStage"`
	ByModel               []ModelUsageStat `json:"byModel"`
}
