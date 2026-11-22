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

const consistencyReportColumns = `
	id, book_id, scope_type, scope_ref_id, start_seq, end_seq,
	model_name, findings_json, summary, created_at
`

// SaveConsistencyReport 持久化一次第二层 AI 深度语义一致性审计报告。
func (s *Store) SaveConsistencyReport(ctx context.Context, rep domain.BookConsistencyReport) (domain.BookConsistencyReport, error) {
	if rep.BookID <= 0 {
		return domain.BookConsistencyReport{}, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if !rep.ScopeType.Valid() {
		return domain.BookConsistencyReport{}, &domain.ValidationError{Field: "scopeType", Message: "无效的语义审计范围类型"}
	}

	rep.Normalize()
	findingsBytes, err := json.Marshal(rep.Findings)
	if err != nil {
		return domain.BookConsistencyReport{}, fmt.Errorf("marshal consistency findings: %w", err)
	}

	var saved domain.BookConsistencyReport
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, rep.BookID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("book %d: %w", rep.BookID, domain.ErrNotFound)
			}
			return mapSQLiteError(err)
		}

		res, err := tx.ExecContext(ctx, `
			INSERT INTO book_consistency_reports (
				book_id, scope_type, scope_ref_id, start_seq, end_seq,
				model_name, findings_json, summary
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, rep.BookID, string(rep.ScopeType), rep.ScopeRefID, rep.StartSeq, rep.EndSeq,
			strings.TrimSpace(rep.ModelName), string(findingsBytes), strings.TrimSpace(rep.Summary))
		if err != nil {
			return mapSQLiteError(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		row := tx.QueryRowContext(ctx, `SELECT `+consistencyReportColumns+` FROM book_consistency_reports WHERE id = ?`, id)
		saved, err = scanConsistencyReport(row)
		return err
	})
	if err != nil {
		return domain.BookConsistencyReport{}, err
	}
	return saved, nil
}

// ListConsistencyReports 返回指定书籍的全部 AI 深度语义审计历史报告（最新在前）。
func (s *Store) ListConsistencyReports(ctx context.Context, bookID int64) ([]domain.BookConsistencyReport, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+consistencyReportColumns+`
		FROM book_consistency_reports
		WHERE book_id = ?
		ORDER BY id DESC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanAll(rows, scanConsistencyReport)
}

// UpdateConsistencyFindingStatus 更新单条 AI 深度审计问题的处理状态（open / resolved / ignored）。
func (s *Store) UpdateConsistencyFindingStatus(
	ctx context.Context,
	reportID int64,
	findingID string,
	status domain.ConsistencyFindingStatus,
) (domain.BookConsistencyReport, error) {
	if reportID <= 0 {
		return domain.BookConsistencyReport{}, &domain.ValidationError{Field: "reportId", Message: "报告 ID 必须大于 0"}
	}
	trimmedID := strings.TrimSpace(findingID)
	if trimmedID == "" {
		return domain.BookConsistencyReport{}, &domain.ValidationError{Field: "findingId", Message: "问题 ID 不能为空"}
	}
	if !status.Valid() {
		return domain.BookConsistencyReport{}, &domain.ValidationError{Field: "status", Message: "无效的处理状态"}
	}

	var updated domain.BookConsistencyReport
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT `+consistencyReportColumns+` FROM book_consistency_reports WHERE id = ?`, reportID)
		rep, err := scanConsistencyReport(row)
		if err != nil {
			return err
		}

		matched := false
		for i := range rep.Findings {
			if rep.Findings[i].ID == trimmedID {
				rep.Findings[i].Status = status
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("finding %q in report %d: %w", trimmedID, reportID, domain.ErrNotFound)
		}

		raw, err := json.Marshal(rep.Findings)
		if err != nil {
			return fmt.Errorf("marshal updated consistency findings: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE book_consistency_reports
			SET findings_json = ?
			WHERE id = ?
		`, string(raw), reportID); err != nil {
			return mapSQLiteError(err)
		}
		updated = rep
		updated.Normalize()
		return nil
	})
	if err != nil {
		return domain.BookConsistencyReport{}, err
	}
	return updated, nil
}

// DeleteConsistencyReport 删除指定 AI 深度审计历史报告。
func (s *Store) DeleteConsistencyReport(ctx context.Context, reportID int64) error {
	if reportID <= 0 {
		return &domain.ValidationError{Field: "reportId", Message: "报告 ID 必须大于 0"}
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM book_consistency_reports WHERE id = ?`, reportID)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("consistency report %d: %w", reportID, domain.ErrNotFound)
		}
		return nil
	})
}

func scanConsistencyReport(sc rowScanner) (domain.BookConsistencyReport, error) {
	var (
		rep          domain.BookConsistencyReport
		scopeType    string
		findingsJSON string
	)
	if err := sc.Scan(
		&rep.ID, &rep.BookID, &scopeType, &rep.ScopeRefID, &rep.StartSeq, &rep.EndSeq,
		&rep.ModelName, &findingsJSON, &rep.Summary, &rep.CreatedAt,
	); err != nil {
		return domain.BookConsistencyReport{}, mapSQLiteError(err)
	}
	rep.ScopeType = domain.ConsistencyAuditScopeType(scopeType)
	rep.Findings = unmarshalJSONSlice[domain.AIConsistencyFinding](findingsJSON)
	rep.Normalize()
	return rep, nil
}
