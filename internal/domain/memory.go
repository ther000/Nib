package domain

import "strings"

// StateEventStatus 定义事件溯源状态账本记录的生命周期状态（对应 entity_state_events.status）。
type StateEventStatus string

const (
	StateEventPending   StateEventStatus = "pending"
	StateEventCommitted StateEventStatus = "committed"
	StateEventReverted  StateEventStatus = "reverted"
)

func (s StateEventStatus) Valid() bool {
	switch s {
	case StateEventPending, StateEventCommitted, StateEventReverted:
		return true
	default:
		return false
	}
}

// ChangeSeverity 定义实体属性变更的风险等级（对应 entity_state_events.change_severity）。
type ChangeSeverity string

const (
	ChangeSeverityNormal   ChangeSeverity = "normal"
	ChangeSeverityHighRisk ChangeSeverity = "high_risk"
)

func (s ChangeSeverity) Valid() bool {
	switch s {
	case ChangeSeverityNormal, ChangeSeverityHighRisk:
		return true
	default:
		return false
	}
}

// ForeshadowingStatus 定义伏笔生命周期状态（对应 foreshadowings.status）。
type ForeshadowingStatus string

const (
	ForeshadowingPendingPlant ForeshadowingStatus = "pending_plant"
	ForeshadowingPlanted      ForeshadowingStatus = "planted"
	ForeshadowingHinted       ForeshadowingStatus = "hinted"
	ForeshadowingResolved     ForeshadowingStatus = "resolved"
	ForeshadowingAbandoned    ForeshadowingStatus = "abandoned"
)

func (s ForeshadowingStatus) Valid() bool {
	switch s {
	case ForeshadowingPendingPlant,
		ForeshadowingPlanted,
		ForeshadowingHinted,
		ForeshadowingResolved,
		ForeshadowingAbandoned:
		return true
	default:
		return false
	}
}

func (s ForeshadowingStatus) Label() string {
	switch s {
	case ForeshadowingPendingPlant:
		return "待埋设"
	case ForeshadowingPlanted:
		return "已埋设"
	case ForeshadowingHinted:
		return "已暗示"
	case ForeshadowingResolved:
		return "已回收"
	case ForeshadowingAbandoned:
		return "已废弃"
	default:
		return string(s)
	}
}

// ArcSummaryLevel 定义分层摘要金字塔层级（对应 arc_summaries.level）。
type ArcSummaryLevel string

const (
	ArcSummaryLevelArc10  ArcSummaryLevel = "arc_10"
	ArcSummaryLevelVolume ArcSummaryLevel = "volume"
)

func (l ArcSummaryLevel) Valid() bool {
	switch l {
	case ArcSummaryLevelArc10, ArcSummaryLevelVolume:
		return true
	default:
		return false
	}
}

// DeltaReviewStatus 定义完章记忆变更单的审核状态（对应 chapter_deltas.review_status）。
type DeltaReviewStatus string

const (
	DeltaReviewPending   DeltaReviewStatus = "pending"
	DeltaReviewApplied   DeltaReviewStatus = "applied"
	DeltaReviewDismissed DeltaReviewStatus = "dismissed"
)

func (s DeltaReviewStatus) Valid() bool {
	switch s {
	case DeltaReviewPending, DeltaReviewApplied, DeltaReviewDismissed:
		return true
	default:
		return false
	}
}

// DeltaForeshadowAction 定义记忆变更单中的伏笔动作类型。
type DeltaForeshadowAction string

const (
	DeltaForeshadowPlant   DeltaForeshadowAction = "plant"
	DeltaForeshadowHint    DeltaForeshadowAction = "hint"
	DeltaForeshadowResolve DeltaForeshadowAction = "resolve"
	DeltaForeshadowAbandon DeltaForeshadowAction = "abandon"
)

func (a DeltaForeshadowAction) Valid() bool {
	switch a {
	case DeltaForeshadowPlant,
		DeltaForeshadowHint,
		DeltaForeshadowResolve,
		DeltaForeshadowAbandon:
		return true
	default:
		return false
	}
}

// DeltaDecision.Kind 与 MemorySearchHit.Source 的约定常量。
const (
	DeltaDecisionKindStatePatch = "state_patch"
	DeltaDecisionKindForeshadow = "foreshadow"
	DeltaDecisionKindNewEntity  = "new_entity"
	DeltaDecisionKindRelation   = "relation"
	DeltaDecisionKindDeadline   = "deadline"

	MemorySearchSourceChapter       = "chapter"
	MemorySearchSourceEntity        = "entity"
	MemorySearchSourceForeshadowing = "foreshadowing"
	MemorySearchSourceArcSummary    = "arc_summary"
)

// StoryDeadlineStatus 定义故事内关键期限/倒计时的生命周期状态（对应 story_deadlines.status）。
type StoryDeadlineStatus string

const (
	StoryDeadlineActive   StoryDeadlineStatus = "active"
	StoryDeadlineReached  StoryDeadlineStatus = "reached"
	StoryDeadlineExpired  StoryDeadlineStatus = "expired"
	StoryDeadlineCanceled StoryDeadlineStatus = "canceled"
)

func (s StoryDeadlineStatus) Valid() bool {
	switch s {
	case StoryDeadlineActive, StoryDeadlineReached, StoryDeadlineExpired, StoryDeadlineCanceled:
		return true
	default:
		return false
	}
}

func (s StoryDeadlineStatus) Label() string {
	switch s {
	case StoryDeadlineActive:
		return "倒计时中"
	case StoryDeadlineReached:
		return "已如期兑现"
	case StoryDeadlineExpired:
		return "已超期未果"
	case StoryDeadlineCanceled:
		return "已失效取消"
	default:
		return string(s)
	}
}

// StoryDeadline 表示书内关键期限或倒计时（如“秘境开启 Day 30”，对应 story_deadlines 表）。
type StoryDeadline struct {
	ID              int64               `json:"id"`
	BookID          int64               `json:"bookId"`
	Title           string              `json:"title"`
	StartChapterSeq int                 `json:"startChapterSeq"`
	TargetDay       int                 `json:"targetDay"`
	TargetLabel     string              `json:"targetLabel"`
	EntityID        *int64              `json:"entityId,omitempty"`
	ForeshadowingID *int64              `json:"foreshadowingId,omitempty"`
	Status          StoryDeadlineStatus `json:"status"`
	Note            string              `json:"note"`
	CreatedAt       string              `json:"createdAt"`
	UpdatedAt       string              `json:"updatedAt"`
}

// UpsertStoryDeadlineInput 是新建或编辑故事内期限倒计时的入参。
type UpsertStoryDeadlineInput struct {
	ID              int64               `json:"id"`
	BookID          int64               `json:"bookId"`
	Title           string              `json:"title"`
	StartChapterSeq int                 `json:"startChapterSeq"`
	TargetDay       int                 `json:"targetDay"`
	TargetLabel     string              `json:"targetLabel"`
	EntityID        *int64              `json:"entityId,omitempty"`
	ForeshadowingID *int64              `json:"foreshadowingId,omitempty"`
	Status          StoryDeadlineStatus `json:"status"`
	Note            string              `json:"note"`
}

func (in UpsertStoryDeadlineInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if strings.TrimSpace(in.Title) == "" {
		return newValidationError("title", "期限名称不能为空")
	}
	if in.StartChapterSeq < 0 {
		return newValidationError("startChapterSeq", "起始章号不能为负数")
	}
	if in.Status != "" && !in.Status.Valid() {
		return newValidationError("status", "无效的期限状态")
	}
	return nil
}

// EntityStateEvent 表示按章节号递增的事件溯源属性变更记录（对应 entity_state_events 表）。
type EntityStateEvent struct {
	ID             int64            `json:"id"`
	BookID         int64            `json:"bookId"`
	EntityID       int64            `json:"entityId"`
	ChapterID      int64            `json:"chapterId"`
	ChapterSeq     int              `json:"chapterSeq"`
	AttrKey        string           `json:"attrKey"`
	PrevValue      string           `json:"prevValue"`
	NewValue       string           `json:"newValue"`
	ChangeSeverity ChangeSeverity   `json:"changeSeverity"`
	Reason         string           `json:"reason"`
	Status         StateEventStatus `json:"status"`
	CreatedAt      string           `json:"createdAt"`
}

func (e EntityStateEvent) Validate() error {
	if e.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if e.EntityID <= 0 {
		return newValidationError("entityId", "实体 ID 必须大于 0")
	}
	if e.ChapterID <= 0 {
		return newValidationError("chapterId", "章节 ID 必须大于 0")
	}
	if e.ChapterSeq <= 0 {
		return newValidationError("chapterSeq", "章节序号必须大于 0")
	}
	if strings.TrimSpace(e.AttrKey) == "" {
		return newValidationError("attrKey", "属性键不能为空")
	}
	if e.ChangeSeverity != "" && !e.ChangeSeverity.Valid() {
		return newValidationError("changeSeverity", "无效的变更风险等级")
	}
	if e.Status != "" && !e.Status.Valid() {
		return newValidationError("status", "无效的状态事件状态")
	}
	return nil
}

// EntitySnapshot 表示截至某章节时间点折算后的设定实体状态快照（SnapshotAt 返回项）。
type EntitySnapshot struct {
	Entity              Entity            `json:"entity"`
	EffectiveAttributes map[string]string `json:"effectiveAttributes"`
	AppliedEventCount   int               `json:"appliedEventCount"`
	PendingEventCount   int               `json:"pendingEventCount"`
}

// Foreshadowing 表示长篇伏笔生命周期记录（对应 foreshadowings 表）。
type Foreshadowing struct {
	ID                 int64               `json:"id"`
	BookID             int64               `json:"bookId"`
	Title              string              `json:"title"`
	Description        string              `json:"description"`
	PlantedChapterSeq  int                 `json:"plantedChapterSeq"`
	LastHintSeq        int                 `json:"lastHintSeq"`
	TargetResolveSeq   int                 `json:"targetResolveSeq"`
	ResolvedChapterSeq int                 `json:"resolvedChapterSeq"`
	Status             ForeshadowingStatus `json:"status"`
	InvolvedEntityIDs  []int64             `json:"involvedEntityIds"`
	CreatedAt          string              `json:"createdAt"`
	UpdatedAt          string              `json:"updatedAt"`
}

// UpsertForeshadowingInput 是创建或编辑伏笔的入参。
type UpsertForeshadowingInput struct {
	ID                 int64               `json:"id"`
	BookID             int64               `json:"bookId"`
	Title              string              `json:"title"`
	Description        string              `json:"description"`
	PlantedChapterSeq  int                 `json:"plantedChapterSeq"`
	LastHintSeq        int                 `json:"lastHintSeq"`
	TargetResolveSeq   int                 `json:"targetResolveSeq"`
	ResolvedChapterSeq int                 `json:"resolvedChapterSeq"`
	Status             ForeshadowingStatus `json:"status"`
	InvolvedEntityIDs  []int64             `json:"involvedEntityIds"`
}

func (in UpsertForeshadowingInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if strings.TrimSpace(in.Title) == "" {
		return newValidationError("title", "伏笔标题不能为空")
	}
	if in.PlantedChapterSeq < 0 {
		return newValidationError("plantedChapterSeq", "埋设章节号不能为负数")
	}
	if in.LastHintSeq < 0 {
		return newValidationError("lastHintSeq", "最近暗示章节号不能为负数")
	}
	if in.TargetResolveSeq < 0 {
		return newValidationError("targetResolveSeq", "计划回收章节号不能为负数")
	}
	if in.ResolvedChapterSeq < 0 {
		return newValidationError("resolvedChapterSeq", "实际回收章节号不能为负数")
	}
	if in.Status != "" && !in.Status.Valid() {
		return newValidationError("status", "无效的伏笔状态")
	}
	return nil
}

// ArcSummary 表示 10 章弧线摘要或卷级摘要（对应 arc_summaries 表）。
type ArcSummary struct {
	ID        int64           `json:"id"`
	BookID    int64           `json:"bookId"`
	Level     ArcSummaryLevel `json:"level"`
	VolumeID  *int64          `json:"volumeId,omitempty"`
	StartSeq  int             `json:"startSeq"`
	EndSeq    int             `json:"endSeq"`
	Summary   string          `json:"summary"`
	CreatedAt string          `json:"createdAt"`
}

// UpsertArcSummaryInput 是新增或更新弧线/卷级摘要的入参。
type UpsertArcSummaryInput struct {
	ID       int64           `json:"id"`
	BookID   int64           `json:"bookId"`
	Level    ArcSummaryLevel `json:"level"`
	VolumeID *int64          `json:"volumeId,omitempty"`
	StartSeq int             `json:"startSeq"`
	EndSeq   int             `json:"endSeq"`
	Summary  string          `json:"summary"`
}

func (in UpsertArcSummaryInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if !in.Level.Valid() {
		return newValidationError("level", "无效的摘要层级")
	}
	if in.VolumeID != nil && *in.VolumeID <= 0 {
		return newValidationError("volumeId", "卷 ID 必须大于 0")
	}
	if in.StartSeq <= 0 {
		return newValidationError("startSeq", "起始章号必须大于 0")
	}
	if in.EndSeq < in.StartSeq {
		return newValidationError("endSeq", "结束章号不能小于起始章号")
	}
	if strings.TrimSpace(in.Summary) == "" {
		return newValidationError("summary", "摘要内容不能为空")
	}
	return nil
}

// GenerateArcSummaryRequest 表示由 AI 自动汇总 10 章弧线摘要或卷级摘要的入参。
type GenerateArcSummaryRequest struct {
	BookID   int64           `json:"bookId"`
	Level    ArcSummaryLevel `json:"level"`
	VolumeID *int64          `json:"volumeId,omitempty"`
	StartSeq int             `json:"startSeq"`
	EndSeq   int             `json:"endSeq"`
}

func (r GenerateArcSummaryRequest) Validate() error {
	if r.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if !r.Level.Valid() {
		return newValidationError("level", "无效的摘要层级")
	}
	if r.Level == ArcSummaryLevelArc10 {
		if r.StartSeq <= 0 || r.EndSeq < r.StartSeq {
			return newValidationError("startSeq", "请指定有效的起止章号范围")
		}
	} else if r.VolumeID == nil || *r.VolumeID <= 0 {
		if r.StartSeq <= 0 || r.EndSeq < r.StartSeq {
			return newValidationError("volumeId", "卷级摘要需指定分卷或有效章号范围")
		}
	}
	return nil
}

// DeltaNewEntity 表示完章抽取发现的新登场设定实体提案。
type DeltaNewEntity struct {
	ID          string            `json:"id"`
	Kind        EntityKind        `json:"kind"`
	Name        string            `json:"name"`
	Aliases     []string          `json:"aliases,omitempty"`
	IsInvariant bool              `json:"isInvariant,omitempty"`
	Summary     string            `json:"summary"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	Reason      string            `json:"reason,omitempty"`
}

// DeltaStatePatch 表示完章抽取出的单条实体状态变更提案。
type DeltaStatePatch struct {
	ID         string         `json:"id"`
	EntityID   int64          `json:"entityId"`
	EntityName string         `json:"entityName"`
	AttrKey    string         `json:"attrKey"`
	PrevValue  string         `json:"prevValue"`
	NewValue   string         `json:"newValue"`
	Severity   ChangeSeverity `json:"severity"`
	Reason     string         `json:"reason"`
}

// DeltaForeshadowUpdate 表示完章抽取出的单条伏笔变更提案（埋设/暗示/回收/废弃）。
type DeltaForeshadowUpdate struct {
	ID                string                `json:"id"`
	Action            DeltaForeshadowAction `json:"action"`
	ForeshadowingID   int64                 `json:"foreshadowingId"`
	Title             string                `json:"title"`
	Description       string                `json:"description"`
	TargetResolveSeq  int                   `json:"targetResolveSeq"`
	InvolvedEntityIDs []int64               `json:"involvedEntityIds"`
}

// DeltaRelationChange 表示完章抽取出的角色/势力关系变更提案（upsert 新增/变更 或 end 终止旧关系）。
type DeltaRelationChange struct {
	ID             string `json:"id"`
	Action         string `json:"action"` // "upsert" | "end"
	RelationID     int64  `json:"relationId,omitempty"`
	SourceEntityID int64  `json:"sourceEntityId,omitempty"`
	SourceName     string `json:"sourceName"`
	TargetEntityID int64  `json:"targetEntityId,omitempty"`
	TargetName     string `json:"targetName"`
	RelationType   string `json:"relationType"`
	Description    string `json:"description"`
	Reason         string `json:"reason,omitempty"`
}

// DeltaDeadlineChange 表示完章抽取出的故事内期限/倒计时新建或状态变更提案。
type DeltaDeadlineChange struct {
	ID          string `json:"id"`
	Action      string `json:"action"` // "create" | "update" | "reach" | "expire" | "cancel"
	DeadlineID  int64  `json:"deadlineId,omitempty"`
	Title       string `json:"title"`
	TargetDay   int    `json:"targetDay"`
	TargetLabel string `json:"targetLabel"`
	Note        string `json:"note"`
}

// ChapterDelta 表示完章记忆变更单（对应 chapter_deltas 表）。
// 抽取结果永不静默入库，必须由作者在审改台确认后才转为 committed。
type ChapterDelta struct {
	ID              int64                   `json:"id"`
	ChapterID       int64                   `json:"chapterId"`
	CausalSummary   string                  `json:"causalSummary"`
	StoryTimeLabel  string                  `json:"storyTimeLabel"`
	StoryDay        *int                    `json:"storyDay,omitempty"`
	NewEntities     []DeltaNewEntity        `json:"newEntities"`
	StatePatches    []DeltaStatePatch       `json:"statePatches"`
	Foreshadows     []DeltaForeshadowUpdate `json:"foreshadows"`
	RelationChanges []DeltaRelationChange   `json:"relationChanges"`
	DeadlineChanges []DeltaDeadlineChange   `json:"deadlineChanges"`
	HighRiskFlags   int                     `json:"highRiskFlags"`
	ReviewStatus    DeltaReviewStatus       `json:"reviewStatus"`
	CreatedAt       string                  `json:"createdAt"`
	ReviewedAt      string                  `json:"reviewedAt"`
}

// DeltaDecision 表示作者对记忆变更单中某一条提案的审核结论。
type DeltaDecision struct {
	Kind             string `json:"kind"` // "state_patch" | "foreshadow" | "new_entity" | "relation" | "deadline"
	ItemID           string `json:"itemId"`
	Accept           bool   `json:"accept"`
	OverrideNewValue string `json:"overrideNewValue,omitempty"`
}

func (d DeltaDecision) Validate() error {
	switch d.Kind {
	case DeltaDecisionKindStatePatch,
		DeltaDecisionKindForeshadow,
		DeltaDecisionKindNewEntity,
		DeltaDecisionKindRelation,
		DeltaDecisionKindDeadline:
	default:
		return newValidationError("kind", "无效的变更决策类型")
	}
	if strings.TrimSpace(d.ItemID) == "" {
		return newValidationError("itemId", "变更项 ID 不能为空")
	}
	return nil
}

// ApplyDeltaInput 是作者提交记忆变更单审核结论的入参。
type ApplyDeltaInput struct {
	DeltaID                int64           `json:"deltaId"`
	ApplyCausalSummary     bool            `json:"applyCausalSummary"`
	CausalSummaryOverride  string          `json:"causalSummaryOverride,omitempty"`
	ApplyStoryTimeline     bool            `json:"applyStoryTimeline,omitempty"`
	StoryTimeLabelOverride string          `json:"storyTimeLabelOverride,omitempty"`
	StoryDayOverride       *int            `json:"storyDayOverride,omitempty"`
	Decisions              []DeltaDecision `json:"decisions"`
}

func (in ApplyDeltaInput) Validate() error {
	if in.DeltaID <= 0 {
		return newValidationError("deltaId", "变更单 ID 必须大于 0")
	}
	for _, d := range in.Decisions {
		if err := d.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ApplyDeltaResult 汇总记忆变更单提交后的落库与下游脏标结果。
type ApplyDeltaResult struct {
	Delta                 ChapterDelta `json:"delta"`
	CommittedEvents       int          `json:"committedEvents"`
	UpdatedForeshadowings int          `json:"updatedForeshadowings"`
	UpdatedRelations      int          `json:"updatedRelations"`
	UpdatedDeadlines      int          `json:"updatedDeadlines"`
	MarkedDirtyChapterIDs []int64      `json:"markedDirtyChapterIds"`
}

// MemorySearchHit 表示 FTS5 + 短词 LIKE 混合检索返回的命中片段。
type MemorySearchHit struct {
	Source          string  `json:"source"` // "chapter" | "entity" | "foreshadowing" | "arc_summary"
	ChapterID       int64   `json:"chapterId"`
	ChapterSeq      int     `json:"chapterSeq"`
	EntityID        int64   `json:"entityId"`
	ForeshadowingID int64   `json:"foreshadowingId,omitempty"`
	ArcSummaryID    int64   `json:"arcSummaryId,omitempty"`
	Title           string  `json:"title"`
	Snippet         string  `json:"snippet"`
	Score           float64 `json:"score"`
}
