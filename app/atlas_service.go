package app

import (
	"context"

	"nib/internal/domain"
)

// AtlasService 提供设定实体与世界铁律档案的桌面 RPC 绑定。
type AtlasService struct {
	rt *Runtime
}

// NewAtlasService 构造绑定到共享 Runtime 的 AtlasService。
func NewAtlasService(rt *Runtime) *AtlasService {
	return &AtlasService{rt: rt}
}

// ListEntities 按书籍与筛选条件列出设定实体（含世界铁律）。
func (s *AtlasService) ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	entities, err := st.ListEntities(ctx, bookID, filter)
	if err != nil {
		return nil, err
	}
	if entities == nil {
		return []domain.Entity{}, nil
	}
	return entities, nil
}

// UpsertEntity 创建或更新设定实体 / 世界铁律。
func (s *AtlasService) UpsertEntity(ctx context.Context, input domain.UpsertEntityInput) (domain.Entity, error) {
	if err := input.Validate(); err != nil {
		return domain.Entity{}, err
	}
	if input.Aliases == nil {
		input.Aliases = []string{}
	}
	if input.Attributes == nil {
		input.Attributes = map[string]string{}
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Entity{}, err
	}
	return st.UpsertEntity(ctx, input)
}

// DeleteEntity 删除指定设定实体。
func (s *AtlasService) DeleteEntity(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	st, err := s.rt.store()
	if err != nil {
		return err
	}
	return st.DeleteEntity(ctx, id)
}

// ListEntityRelations 返回指定书籍的全部角色/势力关系。
func (s *AtlasService) ListEntityRelations(ctx context.Context, bookID int64) ([]domain.EntityRelation, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	rels, err := st.ListEntityRelations(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if rels == nil {
		return []domain.EntityRelation{}, nil
	}
	return rels, nil
}

// UpsertEntityRelation 创建或更新实体间关系。
func (s *AtlasService) UpsertEntityRelation(ctx context.Context, input domain.UpsertEntityRelationInput) (domain.EntityRelation, error) {
	if err := input.Validate(); err != nil {
		return domain.EntityRelation{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.EntityRelation{}, err
	}
	return st.UpsertEntityRelation(ctx, input)
}

// DeleteEntityRelation 删除指定实体关系。
func (s *AtlasService) DeleteEntityRelation(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	st, err := s.rt.store()
	if err != nil {
		return err
	}
	return st.DeleteEntityRelation(ctx, id)
}

// ListEntityRelationsAt 按章节时间轴切片查询截至 chapterSeq（含）生效的实体关系。
func (s *AtlasService) ListEntityRelationsAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntityRelation, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	if chapterSeq < 0 {
		return nil, &domain.ValidationError{Field: "chapterSeq", Message: "章节号不能为负数"}
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	rels, err := st.ListEntityRelationsAt(ctx, bookID, chapterSeq, includePending)
	if err != nil {
		return nil, err
	}
	if rels == nil {
		return []domain.EntityRelation{}, nil
	}
	return rels, nil
}

// PreviewEntityMerge 预览将重复实体合并进目标实体的受影响范围。
func (s *AtlasService) PreviewEntityMerge(ctx context.Context, in domain.EntityMergePreviewInput) (domain.EntityMergePreview, error) {
	if err := in.Validate(); err != nil {
		return domain.EntityMergePreview{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.EntityMergePreview{}, err
	}
	prev, err := st.PreviewEntityMerge(ctx, in)
	if err != nil {
		return domain.EntityMergePreview{}, err
	}
	if prev.TextHits == nil {
		prev.TextHits = []domain.TextImpactHit{}
	}
	return prev, nil
}

// ExecuteEntityMerge 在单事务内合并重复实体并保存安全撤销点与正文快照。
func (s *AtlasService) ExecuteEntityMerge(ctx context.Context, in domain.EntityMergeExecuteInput) (domain.EntityMergeResult, error) {
	if err := in.Validate(); err != nil {
		return domain.EntityMergeResult{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.EntityMergeResult{}, err
	}
	return st.ExecuteEntityMerge(ctx, in)
}

// PreviewBookRename 分模块预览全书安全改名的命中清单与粘连词风险提醒。
func (s *AtlasService) PreviewBookRename(ctx context.Context, in domain.BookRenamePreviewInput) (domain.BookRenamePreview, error) {
	if err := in.Validate(); err != nil {
		return domain.BookRenamePreview{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.BookRenamePreview{}, err
	}
	prev, err := st.PreviewBookRename(ctx, in)
	if err != nil {
		return domain.BookRenamePreview{}, err
	}
	if prev.Hits == nil {
		prev.Hits = []domain.TextImpactHit{}
	}
	return prev, nil
}

// ExecuteBookRename 按预览勾选清单执行全书安全改名（改动正文必先落 chapter_versions 快照且前置创建安全撤销点）。
func (s *AtlasService) ExecuteBookRename(ctx context.Context, in domain.BookRenameExecuteInput) (domain.BookRenameResult, error) {
	if err := in.Validate(); err != nil {
		return domain.BookRenameResult{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.BookRenameResult{}, err
	}
	return st.ExecuteBookRename(ctx, in)
}
