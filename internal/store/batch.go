package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
)

// CreateBatchRun 在单个事务内创建微批调度主记录与按序排队的单章明细。
func (s *Store) CreateBatchRun(ctx context.Context, bookID int64, chapterIDs []int64, mode domain.BatchMode) (domain.BatchRun, error) {
	if bookID <= 0 {
		return domain.BatchRun{}, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	if len(chapterIDs) == 0 {
		return domain.BatchRun{}, fmt.Errorf("%w: chapterIds cannot be empty", domain.ErrValidation)
	}
	if !mode.Valid() {
		return domain.BatchRun{}, fmt.Errorf("%w: invalid batch mode %q", domain.ErrValidation, mode)
	}

	var batchID int64
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO batch_runs (book_id, mode, status, total_items, done_items)
			VALUES (?, ?, 'running', ?, 0)
		`, bookID, string(mode), len(chapterIDs))
		if err != nil {
			return mapSQLiteError(err)
		}
		batchID, err = res.LastInsertId()
		if err != nil {
			return err
		}

		for i, chID := range chapterIDs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO batch_run_items (batch_run_id, chapter_id, seq, state)
				VALUES (?, ?, ?, 'queued')
			`, batchID, chID, i+1); err != nil {
				return mapSQLiteError(err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.BatchRun{}, err
	}
	return s.GetBatchRun(ctx, batchID)
}

// GetBatchRun 查询指定微批任务及按 seq 排序的单章排队明细（含章节序号与标题）。
func (s *Store) GetBatchRun(ctx context.Context, batchID int64) (domain.BatchRun, error) {
	var run domain.BatchRun
	var modeStr, statusStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, book_id, mode, status, total_items, done_items, created_at, finished_at
		FROM batch_runs
		WHERE id = ?
	`, batchID).Scan(
		&run.ID,
		&run.BookID,
		&modeStr,
		&statusStr,
		&run.TotalItems,
		&run.DoneItems,
		&run.CreatedAt,
		&run.FinishedAt,
	)
	if err != nil {
		return domain.BatchRun{}, mapSQLiteError(err)
	}
	run.Mode = domain.BatchMode(modeStr)
	run.Status = domain.BatchRunStatus(statusStr)

	items, err := s.listBatchRunItems(ctx, run.ID)
	if err != nil {
		return domain.BatchRun{}, err
	}
	run.Items = items
	return run, nil
}

// GetLatestBatchRun 返回指定书籍最近一次微批任务；若尚未运行过则返回 (nil, nil)。
func (s *Store) GetLatestBatchRun(ctx context.Context, bookID int64) (*domain.BatchRun, error) {
	var batchID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM batch_runs
		WHERE book_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, bookID).Scan(&batchID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, mapSQLiteError(err)
	}
	run, err := s.GetBatchRun(ctx, batchID)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// GetBatchRunItem 按明细 ID 查询单条微批排队项。
func (s *Store) GetBatchRunItem(ctx context.Context, itemID int64) (domain.BatchRunItem, error) {
	var it domain.BatchRunItem
	var stateStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT i.id, i.batch_run_id, i.chapter_id, c.global_seq, c.title,
		       i.seq, i.state, i.input_hash, i.error_msg, i.started_at, i.finished_at
		FROM batch_run_items i
		JOIN chapters c ON c.id = i.chapter_id
		WHERE i.id = ?
	`, itemID).Scan(
		&it.ID,
		&it.BatchRunID,
		&it.ChapterID,
		&it.ChapterGlobalSeq,
		&it.ChapterTitle,
		&it.Seq,
		&stateStr,
		&it.InputHash,
		&it.ErrorMsg,
		&it.StartedAt,
		&it.FinishedAt,
	)
	if err != nil {
		return domain.BatchRunItem{}, mapSQLiteError(err)
	}
	it.State = domain.BatchItemState(stateStr)
	return it, nil
}

func (s *Store) listBatchRunItems(ctx context.Context, batchRunID int64) ([]domain.BatchRunItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.batch_run_id, i.chapter_id, c.global_seq, c.title,
		       i.seq, i.state, i.input_hash, i.error_msg, i.started_at, i.finished_at
		FROM batch_run_items i
		JOIN chapters c ON c.id = i.chapter_id
		WHERE i.batch_run_id = ?
		ORDER BY i.seq ASC, i.id ASC
	`, batchRunID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	items := make([]domain.BatchRunItem, 0)
	for rows.Next() {
		var it domain.BatchRunItem
		var stateStr string
		if err := rows.Scan(
			&it.ID,
			&it.BatchRunID,
			&it.ChapterID,
			&it.ChapterGlobalSeq,
			&it.ChapterTitle,
			&it.Seq,
			&stateStr,
			&it.InputHash,
			&it.ErrorMsg,
			&it.StartedAt,
			&it.FinishedAt,
		); err != nil {
			return nil, mapSQLiteError(err)
		}
		it.State = domain.BatchItemState(stateStr)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return items, nil
}

// UpdateBatchItemState 更新单章微批明细状态、输入哈希与错误信息，并自动维护 started_at / finished_at 时间戳。
func (s *Store) UpdateBatchItemState(ctx context.Context, itemID int64, state domain.BatchItemState, inputHash, errorMsg string) error {
	if !state.Valid() {
		return fmt.Errorf("%w: invalid batch item state %q", domain.ErrValidation, state)
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var res sql.Result
		var err error
		switch state {
		case domain.BatchItemQueued:
			res, err = tx.ExecContext(ctx, `
				UPDATE batch_run_items
				SET state = ?,
				    input_hash = CASE WHEN ? <> '' THEN ? ELSE input_hash END,
				    error_msg = ?,
				    finished_at = ''
				WHERE id = ?
			`, string(state), inputHash, inputHash, errorMsg, itemID)
		case domain.BatchItemAwaitingReview, domain.BatchItemFailed, domain.BatchItemCanceled:
			res, err = tx.ExecContext(ctx, `
				UPDATE batch_run_items
				SET state = ?,
				    input_hash = CASE WHEN ? <> '' THEN ? ELSE input_hash END,
				    error_msg = ?,
				    started_at = CASE WHEN started_at = '' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') ELSE started_at END,
				    finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ?
			`, string(state), inputHash, inputHash, errorMsg, itemID)
		default:
			res, err = tx.ExecContext(ctx, `
				UPDATE batch_run_items
				SET state = ?,
				    input_hash = CASE WHEN ? <> '' THEN ? ELSE input_hash END,
				    error_msg = ?,
				    started_at = CASE WHEN started_at = '' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now') ELSE started_at END,
				    finished_at = ''
				WHERE id = ?
			`, string(state), inputHash, inputHash, errorMsg, itemID)
		}
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// UpdateBatchRunStatus 更新微批任务主状态与已完成章数。
func (s *Store) UpdateBatchRunStatus(ctx context.Context, batchID int64, status domain.BatchRunStatus, doneItems int) error {
	if !status.Valid() {
		return fmt.Errorf("%w: invalid batch run status %q", domain.ErrValidation, status)
	}
	if doneItems < 0 {
		doneItems = 0
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var res sql.Result
		var err error
		switch status {
		case domain.BatchRunCompleted, domain.BatchRunCanceled, domain.BatchRunFailed:
			res, err = tx.ExecContext(ctx, `
				UPDATE batch_runs
				SET status = ?, done_items = ?, finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ?
			`, string(status), doneItems, batchID)
		default:
			res, err = tx.ExecContext(ctx, `
				UPDATE batch_runs
				SET status = ?, done_items = ?, finished_at = ''
				WHERE id = ?
			`, string(status), doneItems, batchID)
		}
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// RecoverInterruptedBatchRuns 在应用启动时将上次异常退出残留的 running 微批置为 paused，
// 并将执行中的在途子项重置为 queued，防止队列永久卡死。
func (s *Store) RecoverInterruptedBatchRuns(ctx context.Context) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE batch_run_items
			SET state = 'queued', error_msg = '上次运行异常中断，已恢复为待执行'
			WHERE batch_run_id IN (SELECT id FROM batch_runs WHERE status = 'running')
			  AND state IN ('compiling', 'drafting', 'linting', 'critiquing', 'polishing', 'extracting')
		`); err != nil {
			return mapSQLiteError(err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE batch_runs
			SET status = 'paused'
			WHERE status = 'running'
		`); err != nil {
			return mapSQLiteError(err)
		}
		return nil
	})
}

// RecordTokenUsage 追加一条模型 Token 消耗与耗时流水。
func (s *Store) RecordTokenUsage(ctx context.Context, entry domain.TokenUsageLog) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var chapID sql.NullInt64
		if entry.ChapterID > 0 {
			chapID = sql.NullInt64{Int64: entry.ChapterID, Valid: true}
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO token_usage_logs (
				book_id, chapter_id, stage, provider_name, model_name,
				prompt_tokens, completion_tokens, latency_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
			entry.BookID,
			chapID,
			string(entry.Stage),
			strings.TrimSpace(entry.ProviderName),
			strings.TrimSpace(entry.ModelName),
			entry.PromptTokens,
			entry.CompletionTokens,
			entry.LatencyMs,
		)
		return mapSQLiteError(err)
	})
}

// GetUsageSummary 聚合指定书籍（bookID > 0）或全库（bookID == 0）的 Token 消耗统计（按环节与按供应商/模型）。
func (s *Store) GetUsageSummary(ctx context.Context, bookID int64) (domain.UsageSummary, error) {
	summary := domain.UsageSummary{
		BookID:  bookID,
		ByStage: []domain.StageUsageStat{},
		ByModel: []domain.ModelUsageStat{},
	}

	whereClause := ""
	var args []any
	if bookID > 0 {
		whereClause = "WHERE book_id = ?"
		args = append(args, bookID)
	}

	// 1. 按环节聚合
	stageRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT stage,
		       COUNT(*),
		       COALESCE(SUM(prompt_tokens), 0),
		       COALESCE(SUM(completion_tokens), 0),
		       COALESCE(CAST(ROUND(AVG(latency_ms)) AS INTEGER), 0)
		FROM token_usage_logs
		%s
		GROUP BY stage
		ORDER BY stage ASC
	`, whereClause), args...)
	if err != nil {
		return domain.UsageSummary{}, mapSQLiteError(err)
	}
	defer stageRows.Close()

	for stageRows.Next() {
		var st domain.StageUsageStat
		if err := stageRows.Scan(
			&st.Stage,
			&st.Calls,
			&st.PromptTokens,
			&st.CompletionTokens,
			&st.AvgLatencyMs,
		); err != nil {
			return domain.UsageSummary{}, mapSQLiteError(err)
		}
		st.TotalTokens = st.PromptTokens + st.CompletionTokens
		summary.ByStage = append(summary.ByStage, st)
		summary.TotalCalls += st.Calls
		summary.TotalPromptTokens += st.PromptTokens
		summary.TotalCompletionTokens += st.CompletionTokens
		summary.TotalTokens += st.TotalTokens
	}
	if err := stageRows.Err(); err != nil {
		return domain.UsageSummary{}, mapSQLiteError(err)
	}

	// 2. 按供应商 + 模型聚合
	modelRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT provider_name,
		       model_name,
		       COUNT(*),
		       COALESCE(SUM(prompt_tokens), 0),
		       COALESCE(SUM(completion_tokens), 0)
		FROM token_usage_logs
		%s
		GROUP BY provider_name, model_name
		ORDER BY (COALESCE(SUM(prompt_tokens), 0) + COALESCE(SUM(completion_tokens), 0)) DESC, provider_name ASC, model_name ASC
	`, whereClause), args...)
	if err != nil {
		return domain.UsageSummary{}, mapSQLiteError(err)
	}
	defer modelRows.Close()

	for modelRows.Next() {
		var ms domain.ModelUsageStat
		if err := modelRows.Scan(
			&ms.ProviderName,
			&ms.ModelName,
			&ms.Calls,
			&ms.PromptTokens,
			&ms.CompletionTokens,
		); err != nil {
			return domain.UsageSummary{}, mapSQLiteError(err)
		}
		ms.TotalTokens = ms.PromptTokens + ms.CompletionTokens
		summary.ByModel = append(summary.ByModel, ms)
	}
	if err := modelRows.Err(); err != nil {
		return domain.UsageSummary{}, mapSQLiteError(err)
	}

	return summary, nil
}
