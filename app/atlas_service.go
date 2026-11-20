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
