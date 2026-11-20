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
