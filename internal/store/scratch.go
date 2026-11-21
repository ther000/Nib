package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
)

const scratchNoteColumns = `id, book_id, title, content, tags_json, pinned, created_at, updated_at`

// ListScratchNotes 返回一本书的全部灵感便签（置顶优先，其次按更新时间倒序）。
func (s *Store) ListScratchNotes(ctx context.Context, bookID int64) ([]domain.ScratchNote, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+scratchNoteColumns+`
		FROM scratch_notes
		WHERE book_id = ?
		ORDER BY pinned DESC, updated_at DESC, id DESC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.ScratchNote, 0)
	for rows.Next() {
		note, err := scanScratchNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertScratchNote 新建或更新一条灵感便签。
func (s *Store) UpsertScratchNote(ctx context.Context, in domain.UpsertScratchNoteInput) (domain.ScratchNote, error) {
	if err := in.Validate(); err != nil {
		return domain.ScratchNote{}, err
	}
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	cleanTags := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		value := strings.TrimSpace(tag)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleanTags = append(cleanTags, value)
	}
	tagsJSON, err := json.Marshal(cleanTags)
	if err != nil {
		return domain.ScratchNote{}, err
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		runes := []rune(strings.TrimSpace(in.Content))
		if len(runes) > 24 {
			runes = runes[:24]
		}
		title = string(runes)
	}

	var result domain.ScratchNote
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID > 0 {
			res, err := tx.ExecContext(ctx, `
				UPDATE scratch_notes
				SET title = ?, content = ?, tags_json = ?, pinned = ?,
				    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ? AND book_id = ?
			`, title, in.Content, string(tagsJSON), boolToInt(in.Pinned), in.ID, in.BookID)
			if err != nil {
				return mapSQLiteError(err)
			}
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				return fmt.Errorf("scratch note %d: %w", in.ID, domain.ErrNotFound)
			}
			result, err = getScratchNoteRow(ctx, tx, in.ID)
			return err
		}

		res, err := tx.ExecContext(ctx, `
			INSERT INTO scratch_notes (book_id, title, content, tags_json, pinned)
			VALUES (?, ?, ?, ?, ?)
		`, in.BookID, title, in.Content, string(tagsJSON), boolToInt(in.Pinned))
		if err != nil {
			return mapSQLiteError(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		result, err = getScratchNoteRow(ctx, tx, id)
		return err
	})
	if err != nil {
		return domain.ScratchNote{}, err
	}
	return result, nil
}

// DeleteScratchNote 永久删除一条灵感便签。
func (s *Store) DeleteScratchNote(ctx context.Context, id int64) error {
	if id <= 0 {
		return &domain.ValidationError{Field: "id", Message: "便签 ID 必须大于 0"}
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM scratch_notes WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("scratch note %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

func getScratchNoteRow(ctx context.Context, q queryRower, id int64) (domain.ScratchNote, error) {
	row := q.QueryRowContext(ctx, `SELECT `+scratchNoteColumns+` FROM scratch_notes WHERE id = ?`, id)
	note, err := scanScratchNote(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ScratchNote{}, fmt.Errorf("scratch note %d: %w", id, domain.ErrNotFound)
		}
		return domain.ScratchNote{}, err
	}
	return note, nil
}

func scanScratchNote(sc rowScanner) (domain.ScratchNote, error) {
	var (
		note     domain.ScratchNote
		tagsJSON string
		pinned   int
	)
	if err := sc.Scan(&note.ID, &note.BookID, &note.Title, &note.Content, &tagsJSON, &pinned, &note.CreatedAt, &note.UpdatedAt); err != nil {
		return domain.ScratchNote{}, mapSQLiteError(err)
	}
	note.Tags = unmarshalStringSlice(tagsJSON)
	note.Pinned = pinned != 0
	return note, nil
}
