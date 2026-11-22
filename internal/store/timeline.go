package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
)

const storyDeadlineColumns = `
	id, book_id, title, start_chapter_seq, target_day, target_label,
	entity_id, foreshadowing_id, status, note, created_at, updated_at
`

// ListStoryDeadlines 返回指定书籍的故事内关键期限倒计时（status 为空返回全部）。
func (s *Store) ListStoryDeadlines(ctx context.Context, bookID int64, status domain.StoryDeadlineStatus) ([]domain.StoryDeadline, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if status != "" && !status.Valid() {
		return nil, &domain.ValidationError{Field: "status", Message: "无效的期限状态"}
	}

	conds := []string{"book_id = ?"}
	args := []any{bookID}
	if status != "" {
		conds = append(conds, "status = ?")
		args = append(args, string(status))
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+storyDeadlineColumns+`
		FROM story_deadlines
		WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, target_day ASC, id ASC
	`, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	return scanAll(rows, scanStoryDeadline)
}

// UpsertStoryDeadline 创建或更新一条故事内关键期限倒计时。
func (s *Store) UpsertStoryDeadline(ctx context.Context, in domain.UpsertStoryDeadlineInput) (domain.StoryDeadline, error) {
	if err := in.Validate(); err != nil {
		return domain.StoryDeadline{}, err
	}
	status := in.Status
	if status == "" {
		status = domain.StoryDeadlineActive
	}
	title := strings.TrimSpace(in.Title)
	targetLabel := strings.TrimSpace(in.TargetLabel)
	note := strings.TrimSpace(in.Note)

	var entArg any
	if in.EntityID != nil && *in.EntityID > 0 {
		entArg = *in.EntityID
	}
	var fsArg any
	if in.ForeshadowingID != nil && *in.ForeshadowingID > 0 {
		fsArg = *in.ForeshadowingID
	}

	var result domain.StoryDeadline
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, in.BookID).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("book %d: %w", in.BookID, domain.ErrNotFound)
				}
				return mapSQLiteError(err)
			}
			res, err := tx.ExecContext(ctx, `
				INSERT INTO story_deadlines (
					book_id, title, start_chapter_seq, target_day, target_label,
					entity_id, foreshadowing_id, status, note
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, in.BookID, title, in.StartChapterSeq, in.TargetDay, targetLabel, entArg, fsArg, string(status), note)
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getStoryDeadlineRow(ctx, tx, newID)
			return err
		}

		existing, err := getStoryDeadlineRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if existing.BookID != in.BookID {
			return &domain.ValidationError{Field: "bookId", Message: "期限记录不属于指定书籍"}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE story_deadlines
			SET title = ?,
			    start_chapter_seq = ?,
			    target_day = ?,
			    target_label = ?,
			    entity_id = ?,
			    foreshadowing_id = ?,
			    status = ?,
			    note = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, title, in.StartChapterSeq, in.TargetDay, targetLabel, entArg, fsArg, string(status), note, in.ID); err != nil {
			return mapSQLiteError(err)
		}
		result, err = getStoryDeadlineRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.StoryDeadline{}, err
	}
	return result, nil
}

// DeleteStoryDeadline 删除指定故事内期限记录。
func (s *Store) DeleteStoryDeadline(ctx context.Context, id int64) error {
	if id <= 0 {
		return &domain.ValidationError{Field: "id", Message: "期限 ID 必须大于 0"}
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM story_deadlines WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("story deadline %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

func scanStoryDeadline(sc rowScanner) (domain.StoryDeadline, error) {
	var (
		d      domain.StoryDeadline
		entID  sql.NullInt64
		fsID   sql.NullInt64
		status string
	)
	if err := sc.Scan(
		&d.ID, &d.BookID, &d.Title, &d.StartChapterSeq, &d.TargetDay, &d.TargetLabel,
		&entID, &fsID, &status, &d.Note, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return domain.StoryDeadline{}, mapSQLiteError(err)
	}
	if entID.Valid {
		v := entID.Int64
		d.EntityID = &v
	}
	if fsID.Valid {
		v := fsID.Int64
		d.ForeshadowingID = &v
	}
	d.Status = domain.StoryDeadlineStatus(status)
	return d, nil
}

func getStoryDeadlineRow(ctx context.Context, q queryRower, id int64) (domain.StoryDeadline, error) {
	row := q.QueryRowContext(ctx, `SELECT `+storyDeadlineColumns+` FROM story_deadlines WHERE id = ?`, id)
	d, err := scanStoryDeadline(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.StoryDeadline{}, fmt.Errorf("story deadline %d: %w", id, domain.ErrNotFound)
		}
		return domain.StoryDeadline{}, err
	}
	return d, nil
}
