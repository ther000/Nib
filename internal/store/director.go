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
		SELECT id, role, content, tool_calls_json, undo_snapshot_id, created_at
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
			m      domain.DirectorMessage
			tcJSON string
		)
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &tcJSON, &m.UndoSnapshotID, &m.CreatedAt); err != nil {
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
			INSERT INTO director_messages (book_id, role, content, tool_calls_json, undo_snapshot_id)
			VALUES (?, ?, ?, ?, ?)
		`, bookID, role, msg.Content, tcJSON, msg.UndoSnapshotID)
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

const entityRelationSelect = `
	SELECT r.id, r.book_id, r.source_entity_id, s.name, r.target_entity_id, t.name,
	       r.relation_type, r.description, r.effective_chapter_seq, r.ended_chapter_seq,
	       r.status, r.created_at, r.updated_at
	FROM entity_relations r
	JOIN entities s ON s.id = r.source_entity_id
	JOIN entities t ON t.id = r.target_entity_id
`

func scanEntityRelation(sc rowScanner) (domain.EntityRelation, error) {
	var (
		rel      domain.EntityRelation
		endedSeq sql.NullInt64
		status   string
	)
	if err := sc.Scan(
		&rel.ID, &rel.BookID, &rel.SourceEntityID, &rel.SourceName,
		&rel.TargetEntityID, &rel.TargetName, &rel.RelationType,
		&rel.Description, &rel.EffectiveChapterSeq, &endedSeq,
		&status, &rel.CreatedAt, &rel.UpdatedAt,
	); err != nil {
		return domain.EntityRelation{}, mapSQLiteError(err)
	}
	if endedSeq.Valid {
		v := int(endedSeq.Int64)
		rel.EndedChapterSeq = &v
	}
	if status == "" {
		rel.Status = domain.StateEventCommitted
	} else {
		rel.Status = domain.StateEventStatus(status)
	}
	return rel, nil
}

// ListEntityRelations 返回指定书籍的全部实体关系（自动联查双方实体名称）。
func (s *Store) ListEntityRelations(ctx context.Context, bookID int64) ([]domain.EntityRelation, error) {
	if bookID <= 0 {
		return nil, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	rows, err := s.db.QueryContext(ctx, entityRelationSelect+`
		WHERE r.book_id = ?
		ORDER BY r.effective_chapter_seq ASC, r.id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	return scanAll(rows, scanEntityRelation)
}

// ListEntityRelationsAt 按章节时间轴切片查询截至 chapterSeq（含）仍生效的实体关系，绝不泄露未来关系演变。
func (s *Store) ListEntityRelationsAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntityRelation, error) {
	if bookID <= 0 {
		return nil, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	if chapterSeq < 0 {
		return nil, fmt.Errorf("%w: chapterSeq must be >= 0", domain.ErrValidation)
	}

	statusFilter := "r.status = 'committed'"
	if includePending {
		statusFilter = "r.status IN ('committed', 'pending')"
	}

	rows, err := s.db.QueryContext(ctx, entityRelationSelect+`
		WHERE r.book_id = ?
		  AND r.effective_chapter_seq <= ?
		  AND (r.ended_chapter_seq IS NULL OR r.ended_chapter_seq > ?)
		  AND `+statusFilter+`
		ORDER BY r.effective_chapter_seq ASC, r.id ASC
	`, bookID, chapterSeq, chapterSeq)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	return scanAll(rows, scanEntityRelation)
}

// UpsertEntityRelation 创建或更新一条实体关系（支持生效/结束章号区间）。
func (s *Store) UpsertEntityRelation(ctx context.Context, in domain.UpsertEntityRelationInput) (domain.EntityRelation, error) {
	if err := in.Validate(); err != nil {
		return domain.EntityRelation{}, err
	}
	relType := strings.TrimSpace(in.RelationType)
	desc := strings.TrimSpace(in.Description)
	status := in.Status
	if status == "" {
		status = domain.StateEventCommitted
	}
	var endedArg any
	if in.EndedChapterSeq != nil {
		endedArg = *in.EndedChapterSeq
	}

	var savedID int64
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO entity_relations (
					book_id, source_entity_id, target_entity_id, relation_type,
					description, effective_chapter_seq, ended_chapter_seq, status
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, in.BookID, in.SourceEntityID, in.TargetEntityID, relType, desc, in.EffectiveChapterSeq, endedArg, string(status))
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
			    effective_chapter_seq = ?,
			    ended_chapter_seq = ?,
			    status = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ? AND book_id = ?
		`, in.SourceEntityID, in.TargetEntityID, relType, desc, in.EffectiveChapterSeq, endedArg, string(status), in.ID, in.BookID)
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

	row := s.db.QueryRowContext(ctx, entityRelationSelect+` WHERE r.id = ?`, savedID)
	return scanEntityRelation(row)
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
