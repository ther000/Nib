package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"nib/internal/domain"
)

const bookSelectWithStats = `
	SELECT
		b.id, b.title, b.genre, b.premise, b.golden_finger,
		b.target_chapter_words, b.export_dir,
		b.pov_mode, b.pacing_style, b.writing_prompt, b.daily_word_goal,
		COALESCE(c.chap_count, 0),
		COALESCE(c.finalized_count, 0),
		COALESCE(c.total_words, 0),
		b.created_at, b.updated_at
	FROM books b
	LEFT JOIN (
		SELECT
			book_id,
			COUNT(*) AS chap_count,
			SUM(CASE WHEN status = 'finalized' THEN 1 ELSE 0 END) AS finalized_count,
			COALESCE(SUM(word_count), 0) AS total_words
		FROM chapters
		GROUP BY book_id
	) c ON c.book_id = b.id
`

// ListBooks 返回全部书籍列表（含聚合章数与总字数，按创建时间倒序、ID 倒序）。
func (s *Store) ListBooks(ctx context.Context) ([]domain.Book, error) {
	rows, err := s.db.QueryContext(ctx, bookSelectWithStats+`
		ORDER BY b.created_at DESC, b.id DESC
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
	povMode := strings.TrimSpace(in.POVMode)
	if povMode == "" {
		povMode = "third_limited"
	}
	dailyGoal := in.DailyWordGoal
	if dailyGoal <= 0 {
		dailyGoal = domain.DefaultDailyWordGoal
	}

	var result domain.Book
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO books (
				title, genre, premise, golden_finger, target_chapter_words, export_dir,
				pov_mode, pacing_style, writing_prompt, daily_word_goal
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, strings.TrimSpace(in.Title), string(in.Genre), in.Premise, in.GoldenFinger, targetWords, strings.TrimSpace(in.ExportDir),
			povMode, strings.TrimSpace(in.PacingStyle), strings.TrimSpace(in.WritingPrompt), dailyGoal)
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

// UpdateBook 更新书籍核心设定与写作规范。
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
		existing, err := getBookRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		povMode := strings.TrimSpace(in.POVMode)
		if povMode == "" {
			povMode = existing.POVMode
			if povMode == "" {
				povMode = "third_limited"
			}
		}
		dailyGoal := in.DailyWordGoal
		if dailyGoal <= 0 {
			dailyGoal = existing.DailyWordGoal
			if dailyGoal <= 0 {
				dailyGoal = domain.DefaultDailyWordGoal
			}
		}

		res, err := tx.ExecContext(ctx, `
			UPDATE books
			SET title = ?,
			    genre = ?,
			    premise = ?,
			    golden_finger = ?,
			    target_chapter_words = ?,
			    export_dir = ?,
			    pov_mode = ?,
			    pacing_style = ?,
			    writing_prompt = ?,
			    daily_word_goal = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, strings.TrimSpace(in.Title), string(in.Genre), in.Premise, in.GoldenFinger, targetWords, strings.TrimSpace(in.ExportDir),
			povMode, strings.TrimSpace(in.PacingStyle), strings.TrimSpace(in.WritingPrompt), dailyGoal, in.ID)
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
		if err := snapshotBookTrashTx(ctx, tx, id); err != nil {
			return err
		}
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

// DeleteVolume 按 ID 删除卷（级联删除卷内章节、节拍与版本，并重排全书章节序号）。
func (s *Store) DeleteVolume(ctx context.Context, id int64) error {
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var bookID int64
		if err := tx.QueryRowContext(ctx, `SELECT book_id FROM volumes WHERE id = ?`, id).Scan(&bookID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("volume %d: %w", id, domain.ErrNotFound)
			}
			return mapSQLiteError(err)
		}
		if err := snapshotVolumeTrashTx(ctx, tx, id); err != nil {
			return err
		}
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
		return resequenceBookChaptersTx(ctx, tx, bookID)
	})
}

func scanBook(sc rowScanner) (domain.Book, error) {
	var (
		b     domain.Book
		genre string
	)
	if err := sc.Scan(
		&b.ID, &b.Title, &genre, &b.Premise, &b.GoldenFinger,
		&b.TargetChapterWords, &b.ExportDir,
		&b.POVMode, &b.PacingStyle, &b.WritingPrompt, &b.DailyWordGoal,
		&b.ChapterCount, &b.FinalizedCount, &b.TotalWords,
		&b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return domain.Book{}, mapSQLiteError(err)
	}
	b.Genre = domain.GenreID(genre)
	if b.POVMode == "" {
		b.POVMode = "third_limited"
	}
	if b.DailyWordGoal <= 0 {
		b.DailyWordGoal = domain.DefaultDailyWordGoal
	}
	return b, nil
}

func getBookRow(ctx context.Context, q queryRower, id int64) (domain.Book, error) {
	row := q.QueryRowContext(ctx, bookSelectWithStats+`
		WHERE b.id = ?
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

// recordDailyWritingDeltaTx 在章节保存/出稿事务内记录当日码字净增量。
// 旧版本库（迁移 008 之前）没有 writing_stats_daily 表，enabled=false 时直接跳过。
func recordDailyWritingDeltaTx(ctx context.Context, tx *sql.Tx, bookID int64, delta int, enabled bool) error {
	if !enabled || bookID <= 0 || delta == 0 {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	initialWords := delta
	if initialWords < 0 {
		initialWords = 0
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO writing_stats_daily (stat_date, book_id, words_delta, chapters_touched)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(stat_date, book_id) DO UPDATE SET
			words_delta = MAX(0, writing_stats_daily.words_delta + excluded.words_delta),
			chapters_touched = writing_stats_daily.chapters_touched + 1
	`, today, bookID, initialWords)
	if err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

// GetWritingStats 汇总单书（bookID > 0）或全部作品（bookID == 0）的近 N 天码字产出与日更进度。
func (s *Store) GetWritingStats(ctx context.Context, bookID int64, days int) (domain.WritingStatsSummary, error) {
	if days <= 0 {
		days = 14
	}
	if days > 90 {
		days = 90
	}

	summary := domain.WritingStatsSummary{
		BookID:    bookID,
		DailyGoal: domain.DefaultDailyWordGoal,
		Daily:     make([]domain.DailyWordStat, 0, days),
	}

	// 1. 统计当前范围总字数、总章数、定稿章数和日更目标
	if bookID > 0 {
		book, err := s.GetBook(ctx, bookID)
		if err != nil {
			return domain.WritingStatsSummary{}, err
		}
		summary.DailyGoal = book.DailyWordGoal
		summary.TotalWords = book.TotalWords
		summary.TotalChapters = book.ChapterCount
		summary.FinalizedChapters = book.FinalizedCount
	} else {
		var goalSum int
		if err := s.db.QueryRowContext(ctx, `
			SELECT
				COALESCE((SELECT SUM(word_count) FROM chapters), 0),
				COALESCE((SELECT COUNT(*) FROM chapters), 0),
				COALESCE((SELECT COUNT(*) FROM chapters WHERE status = 'finalized'), 0),
				COALESCE((SELECT MAX(daily_word_goal) FROM books), 0)
		`).Scan(&summary.TotalWords, &summary.TotalChapters, &summary.FinalizedChapters, &goalSum); err != nil {
			return domain.WritingStatsSummary{}, mapSQLiteError(err)
		}
		if goalSum > 0 {
			summary.DailyGoal = goalSum
		}
	}

	// 2. 拉取近 90 天内每日汇总（同时支持计算连续更新天数）
	now := time.Now()
	todayStr := now.Format("2006-01-02")
	sinceStr := now.AddDate(0, 0, -89).Format("2006-01-02")

	byDate := make(map[string]domain.DailyWordStat)
	var (
		rows *sql.Rows
		err  error
	)
	if bookID > 0 {
		rows, err = s.db.QueryContext(ctx, `
			SELECT stat_date, words_delta, chapters_touched
			FROM writing_stats_daily
			WHERE book_id = ? AND stat_date >= ?
			ORDER BY stat_date ASC
		`, bookID, sinceStr)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT stat_date, COALESCE(SUM(words_delta), 0), COALESCE(SUM(chapters_touched), 0)
			FROM writing_stats_daily
			WHERE stat_date >= ?
			GROUP BY stat_date
			ORDER BY stat_date ASC
		`, sinceStr)
	}
	if err != nil {
		return domain.WritingStatsSummary{}, mapSQLiteError(err)
	}
	defer rows.Close()

	for rows.Next() {
		var d domain.DailyWordStat
		if err := rows.Scan(&d.Date, &d.WordsDelta, &d.ChaptersTouched); err != nil {
			return domain.WritingStatsSummary{}, mapSQLiteError(err)
		}
		byDate[d.Date] = d
	}
	if err := rows.Err(); err != nil {
		return domain.WritingStatsSummary{}, err
	}

	// 3. 补齐连续 days 天序列（从早到晚）
	for i := days - 1; i >= 0; i-- {
		dt := now.AddDate(0, 0, -i).Format("2006-01-02")
		if item, ok := byDate[dt]; ok {
			summary.Daily = append(summary.Daily, item)
		} else {
			summary.Daily = append(summary.Daily, domain.DailyWordStat{Date: dt})
		}
	}

	if todayItem, ok := byDate[todayStr]; ok {
		summary.TodayWords = todayItem.WordsDelta
	}

	// 4. 计算连续码字天数（今日有产出从今日起算，否则若昨日有产出从昨日起算）
	streak := 0
	startOffset := 0
	if summary.TodayWords <= 0 {
		startOffset = 1
	}
	for i := startOffset; i < 90; i++ {
		dt := now.AddDate(0, 0, -i).Format("2006-01-02")
		if item, ok := byDate[dt]; ok && item.WordsDelta > 0 {
			streak++
		} else {
			break
		}
	}
	summary.StreakDays = streak
	return summary, nil
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
