package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"nib/internal/domain"
)

// CreateAutopilotRun 创建一条持久化的全自动托管运行记录。
func (s *Store) CreateAutopilotRun(ctx context.Context, state domain.AutopilotState) (int64, error) {
	norm := state.Normalized()
	logsJSON, err := json.Marshal(norm.Logs)
	if err != nil {
		logsJSON = []byte("[]")
	}

	var runID int64
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO autopilot_runs (
				book_id, volume_id, status, step,
				target_chapters, completed_chapters,
				auto_outline, auto_polish, auto_commit_memory,
				max_tokens_budget, tokens_used, arc_goal,
				current_chapter_id, current_chapter_seq, current_chapter_title,
				message, error_msg, logs_json, started_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			norm.BookID, norm.VolumeID, string(norm.Status), string(norm.Step),
			norm.TargetChapters, norm.CompletedChapters,
			boolToInt(norm.AutoOutline), boolToInt(norm.AutoPolish), boolToInt(norm.AutoCommitMemory),
			norm.MaxTokensBudget, norm.TokensUsed, norm.ArcGoal,
			norm.CurrentChapterID, norm.CurrentChapterSeq, norm.CurrentChapterTitle,
			norm.Message, norm.Error, string(logsJSON), norm.StartedAt, norm.UpdatedAt,
		)
		if err != nil {
			return mapSQLiteError(err)
		}
		runID, err = res.LastInsertId()
		return err
	})
	return runID, err
}

// UpdateAutopilotRun 持久化托管任务的实时进度、状态与日志。
func (s *Store) UpdateAutopilotRun(ctx context.Context, state domain.AutopilotState) error {
	if state.RunID <= 0 {
		return nil
	}
	norm := state.Normalized()
	logsJSON, err := json.Marshal(norm.Logs)
	if err != nil {
		logsJSON = []byte("[]")
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE autopilot_runs
			SET volume_id = ?,
			    status = ?,
			    step = ?,
			    target_chapters = ?,
			    completed_chapters = ?,
			    tokens_used = ?,
			    current_chapter_id = ?,
			    current_chapter_seq = ?,
			    current_chapter_title = ?,
			    message = ?,
			    error_msg = ?,
			    logs_json = ?,
			    updated_at = ?
			WHERE id = ?
		`,
			norm.VolumeID, string(norm.Status), string(norm.Step),
			norm.TargetChapters, norm.CompletedChapters, norm.TokensUsed,
			norm.CurrentChapterID, norm.CurrentChapterSeq, norm.CurrentChapterTitle,
			norm.Message, norm.Error, string(logsJSON), norm.UpdatedAt, norm.RunID,
		)
		return mapSQLiteError(err)
	})
}

// GetLatestAutopilotRun 查询指定书籍最近一次持久化的托管记录；若不存在返回 (nil, nil)。
func (s *Store) GetLatestAutopilotRun(ctx context.Context, bookID int64) (*domain.AutopilotState, error) {
	if bookID <= 0 {
		return nil, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, book_id, volume_id, status, step,
		       target_chapters, completed_chapters,
		       auto_outline, auto_polish, auto_commit_memory,
		       max_tokens_budget, tokens_used, arc_goal,
		       current_chapter_id, current_chapter_seq, current_chapter_title,
		       message, error_msg, logs_json, started_at, updated_at
		FROM autopilot_runs
		WHERE book_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, bookID)
	st, err := scanAutopilotRun(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &st, nil
}

// RecoverInterruptedAutopilotRuns 在应用重启时将异常中断的 running / waiting_decision 托管置为 stopped，支持断点续跑。
func (s *Store) RecoverInterruptedAutopilotRuns(ctx context.Context) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE autopilot_runs
			SET status = 'stopped',
			    message = '上次托管因应用关闭中断，可点击继续托管从断点接续',
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE status IN ('running', 'waiting_decision')
		`)
		return mapSQLiteError(err)
	})
}

func scanAutopilotRun(sc rowScanner) (domain.AutopilotState, error) {
	var (
		st           domain.AutopilotState
		statusStr    string
		stepStr      string
		autoOutline  int
		autoPolish   int
		autoCommit   int
		logsJSON     string
	)
	if err := sc.Scan(
		&st.RunID, &st.BookID, &st.VolumeID, &statusStr, &stepStr,
		&st.TargetChapters, &st.CompletedChapters,
		&autoOutline, &autoPolish, &autoCommit,
		&st.MaxTokensBudget, &st.TokensUsed, &st.ArcGoal,
		&st.CurrentChapterID, &st.CurrentChapterSeq, &st.CurrentChapterTitle,
		&st.Message, &st.Error, &logsJSON, &st.StartedAt, &st.UpdatedAt,
	); err != nil {
		return domain.AutopilotState{}, mapSQLiteError(err)
	}
	st.Status = domain.AutopilotStatus(statusStr)
	st.Step = domain.AutopilotStep(stepStr)
	st.AutoOutline = autoOutline != 0
	st.AutoPolish = autoPolish != 0
	st.AutoCommitMemory = autoCommit != 0
	if err := json.Unmarshal([]byte(logsJSON), &st.Logs); err != nil {
		st.Logs = []domain.AutopilotLogEntry{}
	}
	return st.Normalized(), nil
}
