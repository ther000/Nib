package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"nib/internal/domain"
)

// ListDirectorMessages 返回指定书籍的 AI 导演持久化对话记录（按时间顺序）。
func (s *Store) ListDirectorMessages(ctx context.Context, bookID int64) ([]domain.DirectorMessage, error) {
	if bookID <= 0 {
		return nil, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, role, content, tool_calls_json, created_at
		FROM director_messages
		WHERE book_id = ?
		ORDER BY id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.DirectorMessage, 0)
	for rows.Next() {
		var (
			m        domain.DirectorMessage
			tcJSON   string
		)
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &tcJSON, &m.CreatedAt); err != nil {
			return nil, mapSQLiteError(err)
		}
		if strings.TrimSpace(tcJSON) != "" && tcJSON != "[]" {
			var tcs []domain.DirectorToolCall
			if err := json.Unmarshal([]byte(tcJSON), &tcs); err == nil && len(tcs) > 0 {
				m.ToolCalls = tcs
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendDirectorMessage 追加一条 AI 导演对话消息到 SQLite。
func (s *Store) AppendDirectorMessage(ctx context.Context, bookID int64, msg domain.DirectorMessage) (domain.DirectorMessage, error) {
	if bookID <= 0 {
		return domain.DirectorMessage{}, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	if role != "user" && role != "assistant" {
		role = "assistant"
	}
	tcJSON := "[]"
	if len(msg.ToolCalls) > 0 {
		if raw, err := json.Marshal(msg.ToolCalls); err == nil {
			tcJSON = string(raw)
		}
	}
	var saved domain.DirectorMessage
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO director_messages (book_id, role, content, tool_calls_json)
			VALUES (?, ?, ?, ?)
		`, bookID, role, msg.Content, tcJSON)
		if err != nil {
			return mapSQLiteError(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		var createdAt string
		_ = tx.QueryRowContext(ctx, `SELECT created_at FROM director_messages WHERE id = ?`, id).Scan(&createdAt)
		saved = msg
		saved.ID = id
		saved.Role = role
		saved.CreatedAt = createdAt
		return nil
	})
	return saved, err
}

// ClearDirectorMessages 清空指定书籍的 AI 导演对话历史。
func (s *Store) ClearDirectorMessages(ctx context.Context, bookID int64) error {
	if bookID <= 0 {
		return fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM director_messages WHERE book_id = ?`, bookID)
		return mapSQLiteError(err)
	})
}

// ListEntityRelations 返回指定书籍的全部实体关系（自动联查双方实体名称）。
func (s *Store) ListEntityRelations(ctx context.Context, bookID int64) ([]domain.EntityRelation, error) {
	if bookID <= 0 {
		return nil, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.book_id, r.source_entity_id, s.name, r.target_entity_id, t.name,
		       r.relation_type, r.description, r.created_at, r.updated_at
		FROM entity_relations r
		JOIN entities s ON s.id = r.source_entity_id
		JOIN entities t ON t.id = r.target_entity_id
		WHERE r.book_id = ?
		ORDER BY r.id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.EntityRelation, 0)
	for rows.Next() {
		var rel domain.EntityRelation
		if err := rows.Scan(
			&rel.ID, &rel.BookID, &rel.SourceEntityID, &rel.SourceName,
			&rel.TargetEntityID, &rel.TargetName, &rel.RelationType,
			&rel.Description, &rel.CreatedAt, &rel.UpdatedAt,
		); err != nil {
			return nil, mapSQLiteError(err)
		}
		out = append(out, rel)
	}
	return out, rows.Err()
}

// UpsertEntityRelation 创建或更新一条实体关系。
func (s *Store) UpsertEntityRelation(ctx context.Context, in domain.UpsertEntityRelationInput) (domain.EntityRelation, error) {
	if err := in.Validate(); err != nil {
		return domain.EntityRelation{}, err
	}
	relType := strings.TrimSpace(in.RelationType)
	desc := strings.TrimSpace(in.Description)

	var savedID int64
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO entity_relations (book_id, source_entity_id, target_entity_id, relation_type, description)
				VALUES (?, ?, ?, ?, ?)
			`, in.BookID, in.SourceEntityID, in.TargetEntityID, relType, desc)
			if err != nil {
				return mapSQLiteError(err)
			}
			savedID, err = res.LastInsertId()
			return err
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE entity_relations
			SET source_entity_id = ?,
			    target_entity_id = ?,
			    relation_type = ?,
			    description = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ? AND book_id = ?
		`, in.SourceEntityID, in.TargetEntityID, relType, desc, in.ID, in.BookID)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return domain.ErrNotFound
		}
		savedID = in.ID
		return nil
	})
	if err != nil {
		return domain.EntityRelation{}, err
	}

	var rel domain.EntityRelation
	err = s.db.QueryRowContext(ctx, `
		SELECT r.id, r.book_id, r.source_entity_id, s.name, r.target_entity_id, t.name,
		       r.relation_type, r.description, r.created_at, r.updated_at
		FROM entity_relations r
		JOIN entities s ON s.id = r.source_entity_id
		JOIN entities t ON t.id = r.target_entity_id
		WHERE r.id = ?
	`, savedID).Scan(
		&rel.ID, &rel.BookID, &rel.SourceEntityID, &rel.SourceName,
		&rel.TargetEntityID, &rel.TargetName, &rel.RelationType,
		&rel.Description, &rel.CreatedAt, &rel.UpdatedAt,
	)
	if err != nil {
		return domain.EntityRelation{}, mapSQLiteError(err)
	}
	return rel, nil
}

// DeleteEntityRelation 删除指定实体关系。
func (s *Store) DeleteEntityRelation(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: id must be > 0", domain.ErrValidation)
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM entity_relations WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}
