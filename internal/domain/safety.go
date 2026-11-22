package domain

import "strings"

// SafetySnapshot 表示一次高危写操作或 AI 导演轮次前自动生成的轻量数据库撤销点（对应 safety_snapshots 表）。
type SafetySnapshot struct {
	ID          int64  `json:"id"`
	BookID      int64  `json:"bookId"`
	Source      string `json:"source"` // "director_turn" | "entity_merge" | "book_rename"
	SourceRefID int64  `json:"sourceRefId"`
	Summary     string `json:"summary"`
	FilePath    string `json:"filePath"`
	CreatedAt   string `json:"createdAt"`
}

// TextImpactHit 描述合并或全书改名预览中命中替换的一处位置明细，支持前端逐条取消勾选。
type TextImpactHit struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"` // "chapter_content" | "chapter_meta" | "beat" | "volume" | "arc_summary" | "entity" | "relation" | "foreshadowing" | "state_event"
	TargetID    int64  `json:"targetId"`
	Label       string `json:"label"`
	MatchCount  int    `json:"matchCount"`
	Snippet     string `json:"snippet"`
	RiskWarning string `json:"riskWarning,omitempty"`
}

// EntityMergePreviewInput 请求预览将 SourceEntityID 合并进 TargetEntityID 的影响范围。
type EntityMergePreviewInput struct {
	BookID         int64 `json:"bookId"`
	SourceEntityID int64 `json:"sourceEntityId"`
	TargetEntityID int64 `json:"targetEntityId"`
	ReplaceInProse bool  `json:"replaceInProse"`
}

func (in EntityMergePreviewInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if in.SourceEntityID <= 0 || in.TargetEntityID <= 0 {
		return newValidationError("entityId", "请选择需要合并的源实体与目标实体")
	}
	if in.SourceEntityID == in.TargetEntityID {
		return newValidationError("targetEntityId", "不能将实体合并到自身")
	}
	return nil
}

// EntityMergeExecuteInput 提交执行实体合并并可选排除特定命中条目。
type EntityMergeExecuteInput struct {
	BookID         int64    `json:"bookId"`
	SourceEntityID int64    `json:"sourceEntityId"`
	TargetEntityID int64    `json:"targetEntityId"`
	ReplaceInProse bool     `json:"replaceInProse"`
	ExcludedHitIDs []string `json:"excludedHitIds,omitempty"`
}

func (in EntityMergeExecuteInput) Validate() error {
	return EntityMergePreviewInput{
		BookID:         in.BookID,
		SourceEntityID: in.SourceEntityID,
		TargetEntityID: in.TargetEntityID,
		ReplaceInProse: in.ReplaceInProse,
	}.Validate()
}

// EntityMergePreview 汇总实体合并前的所有受影响引用与可选正文命中清单。
type EntityMergePreview struct {
	SourceName      string          `json:"sourceName"`
	TargetName      string          `json:"targetName"`
	StateEventCount int             `json:"stateEventCount"`
	RelationCount   int             `json:"relationCount"`
	ForeshadowCount int             `json:"foreshadowCount"`
	BeatCount       int             `json:"beatCount"`
	TextHits        []TextImpactHit `json:"textHits"`
}

// EntityMergeResult 返回实体合并执行后的落库明细与安全快照 ID。
type EntityMergeResult struct {
	TargetEntity    Entity `json:"targetEntity"`
	MergedEvents    int    `json:"mergedEvents"`
	MergedRelations int    `json:"mergedRelations"`
	UpdatedChapters int    `json:"updatedChapters"`
	SnapshotID      int64  `json:"snapshotId"`
}

// BookRenamePreviewInput 请求预览全书级安全改名命中清单。
type BookRenamePreviewInput struct {
	BookID          int64  `json:"bookId"`
	OldName         string `json:"oldName"`
	NewName         string `json:"newName"`
	IncludeOutlines bool   `json:"includeOutlines"`
	IncludeMemory   bool   `json:"includeMemory"`
}

func (in BookRenamePreviewInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	oldTrim := strings.TrimSpace(in.OldName)
	newTrim := strings.TrimSpace(in.NewName)
	if oldTrim == "" {
		return newValidationError("oldName", "原名称不能为空")
	}
	if newTrim == "" {
		return newValidationError("newName", "新名称不能为空")
	}
	if oldTrim == newTrim {
		return newValidationError("newName", "新名称与原名称相同")
	}
	return nil
}

// BookRenameExecuteInput 提交全书级安全改名，支持逐条取消勾选命中项。
type BookRenameExecuteInput struct {
	BookID          int64    `json:"bookId"`
	OldName         string   `json:"oldName"`
	NewName         string   `json:"newName"`
	IncludeOutlines bool     `json:"includeOutlines"`
	IncludeMemory   bool     `json:"includeMemory"`
	ExcludedHitIDs  []string `json:"excludedHitIds,omitempty"`
}

func (in BookRenameExecuteInput) Validate() error {
	return BookRenamePreviewInput{
		BookID:          in.BookID,
		OldName:         in.OldName,
		NewName:         in.NewName,
		IncludeOutlines: in.IncludeOutlines,
		IncludeMemory:   in.IncludeMemory,
	}.Validate()
}

// BookRenamePreview 返回分模块命中清单与总匹配数。
type BookRenamePreview struct {
	OldName      string          `json:"oldName"`
	NewName      string          `json:"newName"`
	TotalMatches int             `json:"totalMatches"`
	Hits         []TextImpactHit `json:"hits"`
}

// BookRenameResult 返回全书安全改名执行结果与安全快照 ID。
type BookRenameResult struct {
	UpdatedChapters int   `json:"updatedChapters"`
	UpdatedRecords  int   `json:"updatedRecords"`
	TotalReplaced   int   `json:"totalReplaced"`
	SnapshotID      int64 `json:"snapshotId"`
}
