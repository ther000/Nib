package domain

import "strings"

// Entity 表示设定实体或世界铁律档案（对应 entities 表）。
type Entity struct {
	ID          int64             `json:"id"`
	BookID      int64             `json:"bookId"`
	Kind        EntityKind        `json:"kind"`
	Name        string            `json:"name"`
	Aliases     []string          `json:"aliases"`
	IsInvariant bool              `json:"isInvariant"`
	Summary     string            `json:"summary"`
	Attributes  map[string]string `json:"attributes"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
}

// EntityFilter 是查询设定实体的筛选条件。
type EntityFilter struct {
	Kind          EntityKind `json:"kind,omitempty"`
	InvariantOnly bool       `json:"invariantOnly,omitempty"`
	Query         string     `json:"query,omitempty"`
}

func (f EntityFilter) Validate() error {
	if f.Kind != "" && !f.Kind.Valid() {
		return newValidationError("kind", "无效的实体分类")
	}
	return nil
}

// UpsertEntityInput 是新增或编辑设定实体的入参。
type UpsertEntityInput struct {
	ID          int64             `json:"id"`
	BookID      int64             `json:"bookId"`
	Kind        EntityKind        `json:"kind"`
	Name        string            `json:"name"`
	Aliases     []string          `json:"aliases"`
	IsInvariant bool              `json:"isInvariant"`
	Summary     string            `json:"summary"`
	Attributes  map[string]string `json:"attributes"`
}

func (in UpsertEntityInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if !in.Kind.Valid() {
		return newValidationError("kind", "无效的实体分类")
	}
	if strings.TrimSpace(in.Name) == "" {
		return newValidationError("name", "实体名称不能为空")
	}
	return nil
}

// EntityRelation 表示两个设定实体之间的有向关系（如 师徒 / 死敌 / 同盟 / 隶属），支持按章节区间演变。
type EntityRelation struct {
	ID                  int64            `json:"id"`
	BookID              int64            `json:"bookId"`
	SourceEntityID      int64            `json:"sourceEntityId"`
	SourceName          string           `json:"sourceName,omitempty"`
	TargetEntityID      int64            `json:"targetEntityId"`
	TargetName          string           `json:"targetName,omitempty"`
	RelationType        string           `json:"relationType"`
	Description         string           `json:"description"`
	EffectiveChapterSeq int              `json:"effectiveChapterSeq"`
	EndedChapterSeq     *int             `json:"endedChapterSeq,omitempty"`
	Status              StateEventStatus `json:"status"`
	CreatedAt           string           `json:"createdAt,omitempty"`
	UpdatedAt           string           `json:"updatedAt,omitempty"`
}

// UpsertEntityRelationInput 是新建或更新实体关系的入参。
type UpsertEntityRelationInput struct {
	ID                  int64            `json:"id"`
	BookID              int64            `json:"bookId"`
	SourceEntityID      int64            `json:"sourceEntityId"`
	TargetEntityID      int64            `json:"targetEntityId"`
	RelationType        string           `json:"relationType"`
	Description         string           `json:"description"`
	EffectiveChapterSeq int              `json:"effectiveChapterSeq"`
	EndedChapterSeq     *int             `json:"endedChapterSeq,omitempty"`
	Status              StateEventStatus `json:"status,omitempty"`
}

func (in UpsertEntityRelationInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if in.SourceEntityID <= 0 || in.TargetEntityID <= 0 {
		return newValidationError("entityId", "请选择关系双方实体")
	}
	if in.SourceEntityID == in.TargetEntityID {
		return newValidationError("targetEntityId", "关系双方不能是同一个实体")
	}
	if strings.TrimSpace(in.RelationType) == "" {
		return newValidationError("relationType", "关系类型不能为空")
	}
	if in.EffectiveChapterSeq < 0 {
		return newValidationError("effectiveChapterSeq", "生效章号不能为负数")
	}
	if in.EndedChapterSeq != nil && *in.EndedChapterSeq < in.EffectiveChapterSeq {
		return newValidationError("endedChapterSeq", "结束章号不能早于生效章号")
	}
	if in.Status != "" && !in.Status.Valid() {
		return newValidationError("status", "无效的关系记录状态")
	}
	return nil
}

// EntityPresenceStat 记录单个实体在指定章节范围内的出场频次与掉线统计。
type EntityPresenceStat struct {
	EntityID             int64      `json:"entityId"`
	EntityName           string     `json:"entityName"`
	EntityKind           EntityKind `json:"entityKind"`
	IsInvariant          bool       `json:"isInvariant"`
	AppearanceChapters   int        `json:"appearanceChapters"`
	MentionCount         int        `json:"mentionCount"`
	BeatAppearances      int        `json:"beatAppearances"`
	FirstSeenSeq         int        `json:"firstSeenSeq"`
	LastSeenSeq          int        `json:"lastSeenSeq"`
	LastSeenChapterID    int64      `json:"lastSeenChapterId"`
	LastSeenChapterTitle string     `json:"lastSeenChapterTitle"`
	AbsentChapters       int        `json:"absentChapters"`
	NeverAppeared        bool       `json:"neverAppeared"`
	AppearedChapterSeqs  []int      `json:"appearedChapterSeqs"`
}

// EntityPresenceReport 汇总全书实体的出场频次与掉线预警雷达结果。
type EntityPresenceReport struct {
	BookID           int64                `json:"bookId"`
	ReferenceSeq     int                  `json:"referenceSeq"`
	LatestChapterSeq int                  `json:"latestChapterSeq"`
	Stats            []EntityPresenceStat `json:"stats"`
}
