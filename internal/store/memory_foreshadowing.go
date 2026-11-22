package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
)

const (
	foreshadowingColumns = `
		id, book_id, title, description, planted_chapter_seq, last_hint_seq,
		target_resolve_seq, resolved_chapter_seq, status, involved_entity_ids_json,
		created_at, updated_at
	`
	arcSummaryColumns = `
		id, book_id, level, volume_id, start_seq, end_seq, summary, created_at
	`
)

// ListForeshadowings 按书籍与可选状态筛选伏笔列表（status 为空返回全部；未关闭及计划回收临近的优先）。
func (s *Store) ListForeshadowings(ctx context.Context, bookID int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if status != "" && !status.Valid() {
		return nil, &domain.ValidationError{Field: "status", Message: "无效的伏笔状态"}
	}

	conds := []string{"book_id = ?"}
	args := []any{bookID}
	if status != "" {
		conds = append(conds, "status = ?")
		args = append(args, string(status))
	}

	query := `
		SELECT ` + foreshadowingColumns + `
		FROM foreshadowings
		WHERE ` + strings.Join(conds, " AND ") + `
		ORDER BY
			CASE status
				WHEN 'planted' THEN 1
				WHEN 'hinted' THEN 2
				WHEN 'pending_plant' THEN 3
				WHEN 'resolved' THEN 4
				WHEN 'abandoned' THEN 5
				ELSE 9
			END,
			CASE WHEN target_resolve_seq > 0 THEN 0 ELSE 1 END,
			target_resolve_seq ASC,
			planted_chapter_seq ASC,
			id ASC
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanAll(rows, scanForeshadowing)
}

// ListActiveForeshadowings 返回当前章号（含）之前已埋设且尚未关闭（planted/hinted）的伏笔，
// 按超期 / 最临近 target_resolve_seq 升序排列，供编译器 P3 槽位直接注入。
func (s *Store) ListActiveForeshadowings(ctx context.Context, bookID int64, currentSeq int) ([]domain.Foreshadowing, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if currentSeq < 0 {
		return nil, &domain.ValidationError{Field: "currentSeq", Message: "当前章节号不能为负数"}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+foreshadowingColumns+`
		FROM foreshadowings
		WHERE book_id = ?
		  AND status IN ('planted', 'hinted')
		  AND (? <= 0 OR planted_chapter_seq <= ?)
		ORDER BY
			CASE WHEN target_resolve_seq > 0 AND ? > 0 AND target_resolve_seq <= ? THEN 0
			     WHEN target_resolve_seq > 0 THEN 1
			     ELSE 2
			END,
			target_resolve_seq ASC,
			planted_chapter_seq ASC,
			id ASC
	`, bookID, currentSeq, currentSeq, currentSeq, currentSeq)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanAll(rows, scanForeshadowing)
}

// UpsertForeshadowing 新增或更新单条伏笔记录。
func (s *Store) UpsertForeshadowing(ctx context.Context, in domain.UpsertForeshadowingInput) (domain.Foreshadowing, error) {
	if err := in.Validate(); err != nil {
		return domain.Foreshadowing{}, err
	}

	title := strings.TrimSpace(in.Title)
	involvedJSON, err := marshalInt64Slice(in.InvolvedEntityIDs)
	if err != nil {
		return domain.Foreshadowing{}, fmt.Errorf("marshal involvedEntityIds: %w", err)
	}

	var result domain.Foreshadowing
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, in.BookID).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("book %d: %w", in.BookID, domain.ErrNotFound)
				}
				return mapSQLiteError(err)
			}

			st := in.Status
			if st == "" {
				st = domain.ForeshadowingPlanted
			}

			res, err := tx.ExecContext(ctx, `
				INSERT INTO foreshadowings (
					book_id, title, description, planted_chapter_seq, last_hint_seq,
					target_resolve_seq, resolved_chapter_seq, status, involved_entity_ids_json
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, in.BookID, title, in.Description, in.PlantedChapterSeq, in.LastHintSeq,
				in.TargetResolveSeq, in.ResolvedChapterSeq, string(st), involvedJSON)
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getForeshadowingRow(ctx, tx, newID)
			return err
		}

		existing, err := getForeshadowingRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if existing.BookID != in.BookID {
			return &domain.ValidationError{Field: "bookId", Message: "伏笔不属于指定书籍"}
		}

		st := in.Status
		if st == "" {
			st = existing.Status
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE foreshadowings
			SET title = ?,
			    description = ?,
			    planted_chapter_seq = ?,
			    last_hint_seq = ?,
			    target_resolve_seq = ?,
			    resolved_chapter_seq = ?,
			    status = ?,
			    involved_entity_ids_json = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, title, in.Description, in.PlantedChapterSeq, in.LastHintSeq,
			in.TargetResolveSeq, in.ResolvedChapterSeq, string(st), involvedJSON, in.ID); err != nil {
			return mapSQLiteError(err)
		}
		result, err = getForeshadowingRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.Foreshadowing{}, err
	}
	return result, nil
}

// DeleteForeshadowing 按 ID 删除伏笔记录。
func (s *Store) DeleteForeshadowing(ctx context.Context, id int64) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM foreshadowings WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("foreshadowing %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

// ListArcSummaries 查询 10 章弧线摘要与卷摘要（level 留空返回全部层级）。
func (s *Store) ListArcSummaries(ctx context.Context, bookID int64, level domain.ArcSummaryLevel) ([]domain.ArcSummary, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if level != "" && !level.Valid() {
		return nil, &domain.ValidationError{Field: "level", Message: "无效的摘要层级"}
	}

	conds := []string{"book_id = ?"}
	args := []any{bookID}
	if level != "" {
		conds = append(conds, "level = ?")
		args = append(args, string(level))
	}

	query := `
		SELECT ` + arcSummaryColumns + `
		FROM arc_summaries
		WHERE ` + strings.Join(conds, " AND ") + `
		ORDER BY level ASC, start_seq ASC, end_seq ASC, id ASC
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanAll(rows, scanArcSummary)
}

// UpsertArcSummary 新增或更新 10 章弧线摘要或卷摘要。
func (s *Store) UpsertArcSummary(ctx context.Context, in domain.UpsertArcSummaryInput) (domain.ArcSummary, error) {
	if err := in.Validate(); err != nil {
		return domain.ArcSummary{}, err
	}

	var volID sql.NullInt64
	if in.VolumeID != nil {
		volID = sql.NullInt64{Int64: *in.VolumeID, Valid: true}
	}

	var result domain.ArcSummary
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if volID.Valid {
			var volBookID int64
			if err := tx.QueryRowContext(ctx, `SELECT book_id FROM volumes WHERE id = ?`, volID.Int64).Scan(&volBookID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("volume %d: %w", volID.Int64, domain.ErrNotFound)
				}
				return mapSQLiteError(err)
			}
			if volBookID != in.BookID {
				return &domain.ValidationError{Field: "volumeId", Message: "卷不属于指定书籍"}
			}
		}

		if in.ID <= 0 {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, in.BookID).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("book %d: %w", in.BookID, domain.ErrNotFound)
				}
				return mapSQLiteError(err)
			}

			res, err := tx.ExecContext(ctx, `
				INSERT INTO arc_summaries (book_id, level, volume_id, start_seq, end_seq, summary)
				VALUES (?, ?, ?, ?, ?, ?)
			`, in.BookID, string(in.Level), volID, in.StartSeq, in.EndSeq, strings.TrimSpace(in.Summary))
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getArcSummaryRow(ctx, tx, newID)
			return err
		}

		existing, err := getArcSummaryRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if existing.BookID != in.BookID {
			return &domain.ValidationError{Field: "bookId", Message: "摘要不属于指定书籍"}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE arc_summaries
			SET level = ?, volume_id = ?, start_seq = ?, end_seq = ?, summary = ?
			WHERE id = ?
		`, string(in.Level), volID, in.StartSeq, in.EndSeq, strings.TrimSpace(in.Summary), in.ID); err != nil {
			return mapSQLiteError(err)
		}
		result, err = getArcSummaryRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.ArcSummary{}, err
	}
	return result, nil
}

func lookupForeshadowingByTitle(ctx context.Context, q querier, bookID int64, title string) (int64, error) {
	target := normalizeLookupKey(title)
	if target == "" {
		return 0, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id, title FROM foreshadowings WHERE book_id = ? ORDER BY id ASC`, bookID)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id int64
			t  string
		)
		if err := rows.Scan(&id, &t); err != nil {
			return 0, mapSQLiteError(err)
		}
		if normalizeLookupKey(t) == target {
			return id, nil
		}
	}
	return 0, rows.Err()
}

func scanForeshadowing(sc rowScanner) (domain.Foreshadowing, error) {
	var (
		f            domain.Foreshadowing
		status       string
		involvedJSON string
	)
	if err := sc.Scan(
		&f.ID, &f.BookID, &f.Title, &f.Description,
		&f.PlantedChapterSeq, &f.LastHintSeq, &f.TargetResolveSeq, &f.ResolvedChapterSeq,
		&status, &involvedJSON, &f.CreatedAt, &f.UpdatedAt,
	); err != nil {
		return domain.Foreshadowing{}, mapSQLiteError(err)
	}
	f.Status = domain.ForeshadowingStatus(status)
	f.InvolvedEntityIDs = unmarshalJSONSlice[int64](involvedJSON)
	return f, nil
}

func getForeshadowingRow(ctx context.Context, q queryRower, id int64) (domain.Foreshadowing, error) {
	row := q.QueryRowContext(ctx, `SELECT `+foreshadowingColumns+` FROM foreshadowings WHERE id = ?`, id)
	f, err := scanForeshadowing(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Foreshadowing{}, fmt.Errorf("foreshadowing %d: %w", id, domain.ErrNotFound)
		}
		return domain.Foreshadowing{}, err
	}
	return f, nil
}

func scanArcSummary(sc rowScanner) (domain.ArcSummary, error) {
	var (
		a     domain.ArcSummary
		level string
		volID sql.NullInt64
	)
	if err := sc.Scan(&a.ID, &a.BookID, &level, &volID, &a.StartSeq, &a.EndSeq, &a.Summary, &a.CreatedAt); err != nil {
		return domain.ArcSummary{}, mapSQLiteError(err)
	}
	a.Level = domain.ArcSummaryLevel(level)
	if volID.Valid {
		v := volID.Int64
		a.VolumeID = &v
	}
	return a, nil
}

func getArcSummaryRow(ctx context.Context, q queryRower, id int64) (domain.ArcSummary, error) {
	row := q.QueryRowContext(ctx, `SELECT `+arcSummaryColumns+` FROM arc_summaries WHERE id = ?`, id)
	a, err := scanArcSummary(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ArcSummary{}, fmt.Errorf("arc summary %d: %w", id, domain.ErrNotFound)
		}
		return domain.ArcSummary{}, err
	}
	return a, nil
}
