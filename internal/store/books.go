package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
)

// ListBooks 返回全部书籍列表（按创建时间倒序、ID 倒序）。
func (s *Store) ListBooks(ctx context.Context) ([]domain.Book, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, genre, premise, golden_finger, target_chapter_words, export_dir, created_at, updated_at
		FROM books
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.Book, 0)
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetBook 按 ID 查询单本书籍。
func (s *Store) GetBook(ctx context.Context, id int64) (domain.Book, error) {
	return getBookRow(ctx, s.db, id)
}

// CreateBook 创建新书并返回持久化后的完整记录。
func (s *Store) CreateBook(ctx context.Context, in domain.CreateBookInput) (domain.Book, error) {
	if err := in.Validate(); err != nil {
		return domain.Book{}, err
	}

	targetWords := in.TargetChapterWords
	if targetWords == 0 {
		targetWords = domain.DefaultTargetChapterWords
	}

	var result domain.Book
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO books (title, genre, premise, golden_finger, target_chapter_words, export_dir)
			VALUES (?, ?, ?, ?, ?, ?)
		`, strings.TrimSpace(in.Title), string(in.Genre), in.Premise, in.GoldenFinger, targetWords, strings.TrimSpace(in.ExportDir))
		if err != nil {
			return mapSQLiteError(err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		result, err = getBookRow(ctx, tx, newID)
		return err
	})
	if err != nil {
		return domain.Book{}, err
	}
	return result, nil
}

// UpdateBook 更新书籍核心设定。
func (s *Store) UpdateBook(ctx context.Context, in domain.UpdateBookInput) (domain.Book, error) {
	if err := in.Validate(); err != nil {
		return domain.Book{}, err
	}

	targetWords := in.TargetChapterWords
	if targetWords == 0 {
		targetWords = domain.DefaultTargetChapterWords
	}

	var result domain.Book
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE books
			SET title = ?,
			    genre = ?,
			    premise = ?,
			    golden_finger = ?,
			    target_chapter_words = ?,
			    export_dir = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, strings.TrimSpace(in.Title), string(in.Genre), in.Premise, in.GoldenFinger, targetWords, strings.TrimSpace(in.ExportDir), in.ID)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("book %d: %w", in.ID, domain.ErrNotFound)
		}
		result, err = getBookRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.Book{}, err
	}
	return result, nil
}

// DeleteBook 按 ID 删除书籍（级联删除全部卷、章、节拍、版本与设定实体）。
func (s *Store) DeleteBook(ctx context.Context, id int64) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM books WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("book %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

// ListVolumes 返回指定书籍下的全部卷（按 seq 升序）。
func (s *Store) ListVolumes(ctx context.Context, bookID int64) ([]domain.Volume, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, book_id, seq, title, goal, climax_plan, created_at
		FROM volumes
		WHERE book_id = ?
		ORDER BY seq ASC, id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.Volume, 0)
	for rows.Next() {
		v, err := scanVolume(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetVolume 按 ID 查询单个卷。
func (s *Store) GetVolume(ctx context.Context, id int64) (domain.Volume, error) {
	return getVolumeRow(ctx, s.db, id)
}

// UpsertVolume 创建或更新卷；新建且 Seq <= 0 时自动顺延该书下一个卷序号。
func (s *Store) UpsertVolume(ctx context.Context, in domain.UpsertVolumeInput) (domain.Volume, error) {
	if err := in.Validate(); err != nil {
		return domain.Volume{}, err
	}

	title := strings.TrimSpace(in.Title)
	var result domain.Volume
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, in.BookID).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("book %d: %w", in.BookID, domain.ErrNotFound)
				}
				return mapSQLiteError(err)
			}

			seq := in.Seq
			if seq <= 0 {
				if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM volumes WHERE book_id = ?`, in.BookID).Scan(&seq); err != nil {
					return mapSQLiteError(err)
				}
			}

			res, err := tx.ExecContext(ctx, `
				INSERT INTO volumes (book_id, seq, title, goal, climax_plan)
				VALUES (?, ?, ?, ?, ?)
			`, in.BookID, seq, title, in.Goal, in.ClimaxPlan)
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getVolumeRow(ctx, tx, newID)
			return err
		}

		existing, err := getVolumeRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if existing.BookID != in.BookID {
			return &domain.ValidationError{Field: "bookId", Message: "卷不属于指定书籍"}
		}

		seq := in.Seq
		if seq <= 0 {
			seq = existing.Seq
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE volumes
			SET seq = ?, title = ?, goal = ?, climax_plan = ?
			WHERE id = ?
		`, seq, title, in.Goal, in.ClimaxPlan, in.ID); err != nil {
			return mapSQLiteError(err)
		}

		result, err = getVolumeRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.Volume{}, err
	}
	return result, nil
}

// DeleteVolume 按 ID 删除卷（级联删除卷内章节、节拍与版本）。
func (s *Store) DeleteVolume(ctx context.Context, id int64) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM volumes WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("volume %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

func scanBook(sc rowScanner) (domain.Book, error) {
	var (
		b     domain.Book
		genre string
	)
	if err := sc.Scan(&b.ID, &b.Title, &genre, &b.Premise, &b.GoldenFinger, &b.TargetChapterWords, &b.ExportDir, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return domain.Book{}, mapSQLiteError(err)
	}
	b.Genre = domain.GenreID(genre)
	return b, nil
}

func getBookRow(ctx context.Context, q queryRower, id int64) (domain.Book, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, title, genre, premise, golden_finger, target_chapter_words, export_dir, created_at, updated_at
		FROM books
		WHERE id = ?
	`, id)
	b, err := scanBook(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Book{}, fmt.Errorf("book %d: %w", id, domain.ErrNotFound)
		}
		return domain.Book{}, err
	}
	return b, nil
}

func scanVolume(sc rowScanner) (domain.Volume, error) {
	var v domain.Volume
	if err := sc.Scan(&v.ID, &v.BookID, &v.Seq, &v.Title, &v.Goal, &v.ClimaxPlan, &v.CreatedAt); err != nil {
		return domain.Volume{}, mapSQLiteError(err)
	}
	return v, nil
}

func getVolumeRow(ctx context.Context, q queryRower, id int64) (domain.Volume, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, book_id, seq, title, goal, climax_plan, created_at
		FROM volumes
		WHERE id = ?
	`, id)
	v, err := scanVolume(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Volume{}, fmt.Errorf("volume %d: %w", id, domain.ErrNotFound)
		}
		return domain.Volume{}, err
	}
	return v, nil
}
