package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"nib/internal/domain"
)

const (
	styleProfileColumns = `
		id, book_id, avg_sentence_chars, max_paragraph_chars,
		dialogue_ratio_min, dialogue_ratio_max, pov_rule,
		tone_directives, forbidden_tropes_note, updated_at
	`
	styleSampleColumns = `
		id, book_id, scene_type, title, excerpt, char_count, created_at
	`
	deaiRuleColumns = `
		id, book_id, category, pattern, is_regex, severity, max_per_chapter, hint, replacement, enabled
	`
	rewritePresetColumns = `
		id, book_id, label, instruction, sort_order, created_at
	`
	chapterAuditColumns = `
		id, chapter_id, lint_json, critic_json, diffs_json,
		hook_passed, hook_verdict, deai_score, updated_at
	`
)

// GetStyleProfile 查询指定书籍的文风指纹档案；
// 若尚未持久化自定义记录，校验书籍存在后返回 domain.DefaultStyleProfile(bookID)。
func (s *Store) GetStyleProfile(ctx context.Context, bookID int64) (domain.StyleProfile, error) {
	if bookID <= 0 {
		return domain.StyleProfile{}, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}

	row := s.db.QueryRowContext(ctx, `SELECT `+styleProfileColumns+` FROM style_profiles WHERE book_id = ?`, bookID)
	prof, err := scanStyleProfile(row)
	if err == nil {
		return prof, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.StyleProfile{}, err
	}

	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, bookID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.StyleProfile{}, fmt.Errorf("book %d: %w", bookID, domain.ErrNotFound)
		}
		return domain.StyleProfile{}, mapSQLiteError(err)
	}
	return domain.DefaultStyleProfile(bookID), nil
}

// SaveStyleProfile 在事务内新增或更新书籍的量化文风指纹档案。
func (s *Store) SaveStyleProfile(ctx context.Context, in domain.UpsertStyleProfileInput) (domain.StyleProfile, error) {
	if err := in.Validate(); err != nil {
		return domain.StyleProfile{}, err
	}

	avgSentence := in.AvgSentenceChars
	if avgSentence == 0 {
		avgSentence = domain.DefaultAvgSentenceChars
	}
	maxPara := in.MaxParagraphChars
	if maxPara == 0 {
		maxPara = domain.DefaultMaxParagraphChars
	}
	dMin := in.DialogueRatioMin
	dMax := in.DialogueRatioMax
	if dMin == 0 && dMax == 0 {
		dMin = domain.DefaultDialogueRatioMin
		dMax = domain.DefaultDialogueRatioMax
	}
	pov := in.PovRule
	if pov == "" {
		pov = domain.PovThirdLimited
	}

	var result domain.StyleProfile
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, in.BookID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("book %d: %w", in.BookID, domain.ErrNotFound)
			}
			return mapSQLiteError(err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO style_profiles (
				book_id, avg_sentence_chars, max_paragraph_chars,
				dialogue_ratio_min, dialogue_ratio_max, pov_rule,
				tone_directives, forbidden_tropes_note, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(book_id) DO UPDATE SET
				avg_sentence_chars    = excluded.avg_sentence_chars,
				max_paragraph_chars   = excluded.max_paragraph_chars,
				dialogue_ratio_min    = excluded.dialogue_ratio_min,
				dialogue_ratio_max    = excluded.dialogue_ratio_max,
				pov_rule              = excluded.pov_rule,
				tone_directives       = excluded.tone_directives,
				forbidden_tropes_note = excluded.forbidden_tropes_note,
				updated_at            = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		`, in.BookID, avgSentence, maxPara, dMin, dMax, string(pov),
			strings.TrimSpace(in.ToneDirectives), strings.TrimSpace(in.ForbiddenTropesNote)); err != nil {
			return mapSQLiteError(err)
		}

		row := tx.QueryRowContext(ctx, `SELECT `+styleProfileColumns+` FROM style_profiles WHERE book_id = ?`, in.BookID)
		var err error
		result, err = scanStyleProfile(row)
		return err
	})
	if err != nil {
		return domain.StyleProfile{}, err
	}
	return result, nil
}

// ListStyleSamples 查询指定书籍的分场景样章锚点（sceneType 为空返回全部场景）。
func (s *Store) ListStyleSamples(ctx context.Context, bookID int64, sceneType domain.SceneType) ([]domain.StyleSample, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if sceneType != "" && !sceneType.Valid() {
		return nil, &domain.ValidationError{Field: "sceneType", Message: "无效的场景类型"}
	}

	conds := []string{"book_id = ?"}
	args := []any{bookID}
	if sceneType != "" {
		conds = append(conds, "scene_type = ?")
		args = append(args, string(sceneType))
	}

	query := `SELECT ` + styleSampleColumns + ` FROM style_samples WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.StyleSample, 0)
	for rows.Next() {
		sm, err := scanStyleSample(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertStyleSample 新增或编辑分场景样章片段，自动统计非空白字符数 char_count。
func (s *Store) UpsertStyleSample(ctx context.Context, in domain.UpsertStyleSampleInput) (domain.StyleSample, error) {
	if err := in.Validate(); err != nil {
		return domain.StyleSample{}, err
	}

	title := strings.TrimSpace(in.Title)
	excerpt := strings.TrimSpace(in.Excerpt)
	charCount := domain.CountWords(excerpt)

	var result domain.StyleSample
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
				INSERT INTO style_samples (book_id, scene_type, title, excerpt, char_count)
				VALUES (?, ?, ?, ?, ?)
			`, in.BookID, string(in.SceneType), title, excerpt, charCount)
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getStyleSampleRow(ctx, tx, newID)
			return err
		}

		existing, err := getStyleSampleRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if existing.BookID != in.BookID {
			return &domain.ValidationError{Field: "bookId", Message: "样章不属于指定书籍"}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE style_samples
			SET scene_type = ?, title = ?, excerpt = ?, char_count = ?
			WHERE id = ?
		`, string(in.SceneType), title, excerpt, charCount, in.ID); err != nil {
			return mapSQLiteError(err)
		}
		result, err = getStyleSampleRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.StyleSample{}, err
	}
	return result, nil
}

// DeleteStyleSample 按 ID 删除样章锚点。
func (s *Store) DeleteStyleSample(ctx context.Context, id int64) error {
	if id <= 0 {
		return &domain.ValidationError{Field: "id", Message: "样章 ID 必须大于 0"}
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM style_samples WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("style sample %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

// ListDeaiRules 返回全局内置规则（book_id = 0）以及指定书籍专属规则（book_id = bookID），
// 按分类（cliche -> structure -> formatting）与 ID 稳定排序。
func (s *Store) ListDeaiRules(ctx context.Context, bookID int64) ([]domain.DeaiRule, error) {
	if bookID < 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 不能为负数"}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+deaiRuleColumns+`
		FROM deai_rules
		WHERE book_id = 0 OR (? > 0 AND book_id = ?)
		ORDER BY
			CASE category
				WHEN 'cliche'     THEN 1
				WHEN 'structure'  THEN 2
				WHEN 'formatting' THEN 3
				WHEN 'sensitive'  THEN 4
				ELSE 9
			END,
			book_id ASC,
			id ASC
	`, bookID, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.DeaiRule, 0)
	for rows.Next() {
		r, err := scanDeaiRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertDeaiRule 新增或更新去 AI 味规则（支持修改全局内置规则的启用开关/严重级别/阈值，或增改书籍专属规则）。
func (s *Store) UpsertDeaiRule(ctx context.Context, in domain.UpsertDeaiRuleInput) (domain.DeaiRule, error) {
	if err := in.Validate(); err != nil {
		return domain.DeaiRule{}, err
	}

	pattern := strings.TrimSpace(in.Pattern)
	if in.IsRegex {
		if _, err := regexp.Compile(pattern); err != nil {
			return domain.DeaiRule{}, &domain.ValidationError{Field: "pattern", Message: "无效的正则表达式"}
		}
	}
	severity := in.Severity
	if severity == "" {
		severity = domain.LintSeverityWarn
	}

	var result domain.DeaiRule
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			if in.BookID > 0 {
				var exists int
				if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, in.BookID).Scan(&exists); err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return fmt.Errorf("book %d: %w", in.BookID, domain.ErrNotFound)
					}
					return mapSQLiteError(err)
				}
			}

			res, err := tx.ExecContext(ctx, `
				INSERT INTO deai_rules (
					book_id, category, pattern, is_regex,
					severity, max_per_chapter, hint, replacement, enabled
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, in.BookID, string(in.Category), pattern, boolToInt(in.IsRegex),
				string(severity), in.MaxPerChapter, strings.TrimSpace(in.Hint), strings.TrimSpace(in.Replacement), boolToInt(in.Enabled))
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getDeaiRuleRow(ctx, tx, newID)
			return err
		}

		existing, err := getDeaiRuleRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if existing.BookID > 0 && in.BookID > 0 && existing.BookID != in.BookID {
			return &domain.ValidationError{Field: "bookId", Message: "规则不属于指定书籍"}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE deai_rules
			SET category = ?,
			    pattern = ?,
			    is_regex = ?,
			    severity = ?,
			    max_per_chapter = ?,
			    hint = ?,
			    replacement = ?,
			    enabled = ?
			WHERE id = ?
		`, string(in.Category), pattern, boolToInt(in.IsRegex),
			string(severity), in.MaxPerChapter, strings.TrimSpace(in.Hint), strings.TrimSpace(in.Replacement), boolToInt(in.Enabled), in.ID); err != nil {
			return mapSQLiteError(err)
		}
		result, err = getDeaiRuleRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.DeaiRule{}, err
	}
	return result, nil
}

// DeleteDeaiRule 按 ID 删除自定义去 AI 味规则；
// 全局默认规则（book_id = 0）禁止删除（返回 domain.ErrValidation），只允许通过 UpsertDeaiRule 禁用。
func (s *Store) DeleteDeaiRule(ctx context.Context, id int64) error {
	if id <= 0 {
		return &domain.ValidationError{Field: "id", Message: "规则 ID 必须大于 0"}
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		existing, err := getDeaiRuleRow(ctx, tx, id)
		if err != nil {
			return err
		}
		if existing.BookID == 0 {
			return &domain.ValidationError{Field: "id", Message: "全局内置去 AI 味规则不可删除，请通过启用开关禁用"}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM deai_rules WHERE id = ?`, id); err != nil {
			return mapSQLiteError(err)
		}
		return nil
	})
}

// GetChapterAudit 读取指定章节的质检与手术润色报告；
// 若尚未生成过报告，校验章节存在后返回带 DeaiScore=100 与空切片的默认报告。
func (s *Store) GetChapterAudit(ctx context.Context, chapterID int64) (domain.ChapterAudit, error) {
	if chapterID <= 0 {
		return domain.ChapterAudit{}, &domain.ValidationError{Field: "chapterId", Message: "章节 ID 必须大于 0"}
	}

	row := s.db.QueryRowContext(ctx, `SELECT `+chapterAuditColumns+` FROM chapter_audits WHERE chapter_id = ?`, chapterID)
	audit, err := scanChapterAudit(row)
	if err == nil {
		return audit, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.ChapterAudit{}, err
	}

	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM chapters WHERE id = ?`, chapterID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ChapterAudit{}, fmt.Errorf("chapter %d: %w", chapterID, domain.ErrNotFound)
		}
		return domain.ChapterAudit{}, mapSQLiteError(err)
	}

	return domain.ChapterAudit{
		ChapterID:      chapterID,
		LintIssues:     []domain.LintIssue{},
		CriticFindings: []domain.CriticFinding{},
		Diffs:          []domain.SurgicalDiff{},
		DeaiScore:      domain.DefaultDeaiScore,
	}, nil
}

// SaveChapterAudit 在事务内新增或更新章节质检与手术润色报告。
func (s *Store) SaveChapterAudit(ctx context.Context, audit domain.ChapterAudit) (domain.ChapterAudit, error) {
	if audit.ChapterID <= 0 {
		return domain.ChapterAudit{}, &domain.ValidationError{Field: "chapterId", Message: "章节 ID 必须大于 0"}
	}

	lintIssues := audit.LintIssues
	if lintIssues == nil {
		lintIssues = []domain.LintIssue{}
	}
	criticFindings := audit.CriticFindings
	if criticFindings == nil {
		criticFindings = []domain.CriticFinding{}
	}
	diffs := audit.Diffs
	if diffs == nil {
		diffs = []domain.SurgicalDiff{}
	}

	lintJSON, err := json.Marshal(lintIssues)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("marshal lintIssues: %w", err)
	}
	criticJSON, err := json.Marshal(criticFindings)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("marshal criticFindings: %w", err)
	}
	diffsJSON, err := json.Marshal(diffs)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("marshal diffs: %w", err)
	}

	score := audit.DeaiScore
	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	var result domain.ChapterAudit
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM chapters WHERE id = ?`, audit.ChapterID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("chapter %d: %w", audit.ChapterID, domain.ErrNotFound)
			}
			return mapSQLiteError(err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chapter_audits (
				chapter_id, lint_json, critic_json, diffs_json,
				hook_passed, hook_verdict, deai_score, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(chapter_id) DO UPDATE SET
				lint_json    = excluded.lint_json,
				critic_json  = excluded.critic_json,
				diffs_json   = excluded.diffs_json,
				hook_passed  = excluded.hook_passed,
				hook_verdict = excluded.hook_verdict,
				deai_score   = excluded.deai_score,
				updated_at   = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		`, audit.ChapterID, string(lintJSON), string(criticJSON), string(diffsJSON),
			boolToInt(audit.HookPassed), strings.TrimSpace(audit.HookVerdict), score); err != nil {
			return mapSQLiteError(err)
		}

		row := tx.QueryRowContext(ctx, `SELECT `+chapterAuditColumns+` FROM chapter_audits WHERE chapter_id = ?`, audit.ChapterID)
		var err error
		result, err = scanChapterAudit(row)
		return err
	})
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	return result, nil
}

func scanStyleProfile(sc rowScanner) (domain.StyleProfile, error) {
	var (
		p       domain.StyleProfile
		povRule string
	)
	if err := sc.Scan(
		&p.ID, &p.BookID, &p.AvgSentenceChars, &p.MaxParagraphChars,
		&p.DialogueRatioMin, &p.DialogueRatioMax, &povRule,
		&p.ToneDirectives, &p.ForbiddenTropesNote, &p.UpdatedAt,
	); err != nil {
		return domain.StyleProfile{}, mapSQLiteError(err)
	}
	p.PovRule = domain.PovRule(povRule)
	return p, nil
}

func scanStyleSample(sc rowScanner) (domain.StyleSample, error) {
	var (
		sm        domain.StyleSample
		sceneType string
	)
	if err := sc.Scan(&sm.ID, &sm.BookID, &sceneType, &sm.Title, &sm.Excerpt, &sm.CharCount, &sm.CreatedAt); err != nil {
		return domain.StyleSample{}, mapSQLiteError(err)
	}
	sm.SceneType = domain.SceneType(sceneType)
	return sm, nil
}

func getStyleSampleRow(ctx context.Context, q queryRower, id int64) (domain.StyleSample, error) {
	row := q.QueryRowContext(ctx, `SELECT `+styleSampleColumns+` FROM style_samples WHERE id = ?`, id)
	sm, err := scanStyleSample(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.StyleSample{}, fmt.Errorf("style sample %d: %w", id, domain.ErrNotFound)
		}
		return domain.StyleSample{}, err
	}
	return sm, nil
}

func scanDeaiRule(sc rowScanner) (domain.DeaiRule, error) {
	var (
		r        domain.DeaiRule
		category string
		isRegex  int
		severity string
		enabled  int
	)
	if err := sc.Scan(
		&r.ID, &r.BookID, &category, &r.Pattern, &isRegex,
		&severity, &r.MaxPerChapter, &r.Hint, &r.Replacement, &enabled,
	); err != nil {
		return domain.DeaiRule{}, mapSQLiteError(err)
	}
	r.Category = domain.DeaiCategory(category)
	r.IsRegex = isRegex != 0
	r.Severity = domain.LintSeverity(severity)
	r.Enabled = enabled != 0
	return r, nil
}

// ListRewritePresets 返回全局默认指令与指定书籍专属的划词改写常用指令（按 sort_order ASC, id ASC）。
func (s *Store) ListRewritePresets(ctx context.Context, bookID int64) ([]domain.RewritePreset, error) {
	if bookID < 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 不能为负数"}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+rewritePresetColumns+`
		FROM rewrite_presets
		WHERE book_id = 0 OR (? > 0 AND book_id = ?)
		ORDER BY sort_order ASC, id ASC
	`, bookID, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.RewritePreset, 0)
	for rows.Next() {
		p, err := scanRewritePreset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertRewritePreset 新增或更新划词微调常用指令预设。
func (s *Store) UpsertRewritePreset(ctx context.Context, in domain.UpsertRewritePresetInput) (domain.RewritePreset, error) {
	if err := in.Validate(); err != nil {
		return domain.RewritePreset{}, err
	}
	label := strings.TrimSpace(in.Label)
	instruction := strings.TrimSpace(in.Instruction)

	var result domain.RewritePreset
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if in.ID <= 0 {
			if in.BookID > 0 {
				var exists int
				if err := tx.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id = ?`, in.BookID).Scan(&exists); err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return fmt.Errorf("book %d: %w", in.BookID, domain.ErrNotFound)
					}
					return mapSQLiteError(err)
				}
			}
			res, err := tx.ExecContext(ctx, `
				INSERT INTO rewrite_presets (book_id, label, instruction, sort_order)
				VALUES (?, ?, ?, ?)
			`, in.BookID, label, instruction, in.SortOrder)
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			result, err = getRewritePresetRow(ctx, tx, newID)
			return err
		}

		existing, err := getRewritePresetRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if existing.BookID > 0 && in.BookID > 0 && existing.BookID != in.BookID {
			return &domain.ValidationError{Field: "bookId", Message: "预设指令不属于指定书籍"}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE rewrite_presets
			SET label = ?, instruction = ?, sort_order = ?
			WHERE id = ?
		`, label, instruction, in.SortOrder, in.ID); err != nil {
			return mapSQLiteError(err)
		}
		result, err = getRewritePresetRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.RewritePreset{}, err
	}
	return result, nil
}

// DeleteRewritePreset 按 ID 删除自定义划词常用指令预设。
func (s *Store) DeleteRewritePreset(ctx context.Context, id int64) error {
	if id <= 0 {
		return &domain.ValidationError{Field: "id", Message: "预设指令 ID 必须大于 0"}
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM rewrite_presets WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("rewrite preset %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

func scanRewritePreset(sc rowScanner) (domain.RewritePreset, error) {
	var p domain.RewritePreset
	if err := sc.Scan(&p.ID, &p.BookID, &p.Label, &p.Instruction, &p.SortOrder, &p.CreatedAt); err != nil {
		return domain.RewritePreset{}, mapSQLiteError(err)
	}
	return p, nil
}

func getRewritePresetRow(ctx context.Context, q queryRower, id int64) (domain.RewritePreset, error) {
	row := q.QueryRowContext(ctx, `SELECT `+rewritePresetColumns+` FROM rewrite_presets WHERE id = ?`, id)
	p, err := scanRewritePreset(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.RewritePreset{}, fmt.Errorf("rewrite preset %d: %w", id, domain.ErrNotFound)
		}
		return domain.RewritePreset{}, err
	}
	return p, nil
}

func getDeaiRuleRow(ctx context.Context, q queryRower, id int64) (domain.DeaiRule, error) {
	row := q.QueryRowContext(ctx, `SELECT `+deaiRuleColumns+` FROM deai_rules WHERE id = ?`, id)
	r, err := scanDeaiRule(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.DeaiRule{}, fmt.Errorf("deai rule %d: %w", id, domain.ErrNotFound)
		}
		return domain.DeaiRule{}, err
	}
	return r, nil
}

func scanChapterAudit(sc rowScanner) (domain.ChapterAudit, error) {
	var (
		a          domain.ChapterAudit
		lintJSON   string
		criticJSON string
		diffsJSON  string
		hookPassed int
	)
	if err := sc.Scan(
		&a.ID, &a.ChapterID, &lintJSON, &criticJSON, &diffsJSON,
		&hookPassed, &a.HookVerdict, &a.DeaiScore, &a.UpdatedAt,
	); err != nil {
		return domain.ChapterAudit{}, mapSQLiteError(err)
	}
	a.HookPassed = hookPassed != 0
	a.LintIssues = unmarshalLintIssues(lintJSON)
	a.CriticFindings = unmarshalCriticFindings(criticJSON)
	a.Diffs = unmarshalSurgicalDiffs(diffsJSON)
	return a, nil
}

func unmarshalLintIssues(raw string) []domain.LintIssue {
	if strings.TrimSpace(raw) == "" {
		return []domain.LintIssue{}
	}
	var out []domain.LintIssue
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []domain.LintIssue{}
	}
	return out
}

func unmarshalCriticFindings(raw string) []domain.CriticFinding {
	if strings.TrimSpace(raw) == "" {
		return []domain.CriticFinding{}
	}
	var out []domain.CriticFinding
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []domain.CriticFinding{}
	}
	return out
}

func unmarshalSurgicalDiffs(raw string) []domain.SurgicalDiff {
	if strings.TrimSpace(raw) == "" {
		return []domain.SurgicalDiff{}
	}
	var out []domain.SurgicalDiff
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []domain.SurgicalDiff{}
	}
	return out
}
