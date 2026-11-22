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

const chapterColumns = `
	id, book_id, volume_id, global_seq, vol_seq, title, status,
	emotion_tag, hook_type, hook_note, outline_summary, causal_summary,
	content, word_count, last_briefing_json, story_time_label, story_day,
	created_at, updated_at
`

// ListChapters 返回指定卷下的全部章节（按卷内章序 vol_seq 升序）。
func (s *Store) ListChapters(ctx context.Context, volumeID int64) ([]domain.Chapter, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+chapterColumns+`
		FROM chapters
		WHERE volume_id = ?
		ORDER BY vol_seq ASC, global_seq ASC, id ASC
	`, volumeID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanChapterRows(rows)
}

// ListBookChapters 返回整本书的全部章节（按全局章序 global_seq 升序）。
func (s *Store) ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+chapterColumns+`
		FROM chapters
		WHERE book_id = ?
		ORDER BY global_seq ASC, id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanChapterRows(rows)
}

// GetChapter 按 ID 查询单章完整信息。
func (s *Store) GetChapter(ctx context.Context, id int64) (domain.Chapter, error) {
	return getChapterRow(ctx, s.db, id)
}

// GetPreviousChapters 返回指定全局章序之前的最多 limit 章（按 global_seq 倒序，索引 0 为紧邻上一章 N-1）。
func (s *Store) GetPreviousChapters(ctx context.Context, bookID int64, beforeGlobalSeq int, limit int) ([]domain.Chapter, error) {
	if limit <= 0 {
		return []domain.Chapter{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+chapterColumns+`
		FROM chapters
		WHERE book_id = ? AND global_seq < ?
		ORDER BY global_seq DESC
		LIMIT ?
	`, bookID, beforeGlobalSeq, limit)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanChapterRows(rows)
}

// CreateChapter 在指定卷内新建章节，自动顺延该书全局章序 global_seq 与卷内章序 vol_seq。
func (s *Store) CreateChapter(ctx context.Context, in domain.CreateChapterInput) (domain.Chapter, error) {
	if err := in.Validate(); err != nil {
		return domain.Chapter{}, err
	}

	emotion := in.EmotionTag
	if emotion == "" {
		emotion = domain.EmotionSetup
	}
	hookType := in.HookType
	if hookType == "" {
		hookType = domain.HookMystery
	}

	var result domain.Chapter
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var volBookID int64
		if err := tx.QueryRowContext(ctx, `SELECT book_id FROM volumes WHERE id = ?`, in.VolumeID).Scan(&volBookID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("volume %d: %w", in.VolumeID, domain.ErrNotFound)
			}
			return mapSQLiteError(err)
		}
		if volBookID != in.BookID {
			return &domain.ValidationError{Field: "volumeId", Message: "卷不属于指定书籍"}
		}

		var nextGlobalSeq, nextVolSeq int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(global_seq), 0) + 1 FROM chapters WHERE book_id = ?`, in.BookID).Scan(&nextGlobalSeq); err != nil {
			return mapSQLiteError(err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(vol_seq), 0) + 1 FROM chapters WHERE volume_id = ?`, in.VolumeID).Scan(&nextVolSeq); err != nil {
			return mapSQLiteError(err)
		}

		res, err := tx.ExecContext(ctx, `
			INSERT INTO chapters (
				book_id, volume_id, global_seq, vol_seq, title, status,
				emotion_tag, hook_type, hook_note, outline_summary
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, in.BookID, in.VolumeID, nextGlobalSeq, nextVolSeq, strings.TrimSpace(in.Title),
			string(domain.ChapterStatusPlanned), string(emotion), string(hookType), in.HookNote, in.OutlineSummary)
		if err != nil {
			return mapSQLiteError(err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		result, err = getChapterRow(ctx, tx, newID)
		return err
	})
	if err != nil {
		return domain.Chapter{}, err
	}
	return result, nil
}

// UpdateChapterMeta 更新章节标题、状态、情绪极性、钩子与大纲元信息（不修改正文内容）。
func (s *Store) UpdateChapterMeta(ctx context.Context, in domain.UpdateChapterMetaInput) (domain.Chapter, error) {
	if err := in.Validate(); err != nil {
		return domain.Chapter{}, err
	}

	var result domain.Chapter
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		existing, err := getChapterRow(ctx, tx, in.ID)
		if err != nil {
			return err
		}

		status := in.Status
		if status == "" {
			status = existing.Status
		}
		emotion := in.EmotionTag
		if emotion == "" {
			emotion = existing.EmotionTag
		}
		hookType := in.HookType
		if hookType == "" {
			hookType = existing.HookType
		}

		storyLabel := existing.StoryTimeLabel
		storyDay := existing.StoryDay
		if in.UpdateStoryTimeline {
			storyLabel = strings.TrimSpace(in.StoryTimeLabel)
			storyDay = in.StoryDay
		}
		var storyDayArg any
		if storyDay != nil {
			storyDayArg = *storyDay
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE chapters
			SET title = ?,
			    status = ?,
			    emotion_tag = ?,
			    hook_type = ?,
			    hook_note = ?,
			    outline_summary = ?,
			    causal_summary = ?,
			    story_time_label = ?,
			    story_day = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, strings.TrimSpace(in.Title), string(status), string(emotion), string(hookType),
			in.HookNote, in.OutlineSummary, in.CausalSummary, storyLabel, storyDayArg, in.ID); err != nil {
			return mapSQLiteError(err)
		}

		result, err = getChapterRow(ctx, tx, in.ID)
		return err
	})
	if err != nil {
		return domain.Chapter{}, err
	}
	return result, nil
}

// SaveChapterContent 在单个 SQL 事务内更新章节正文与字数统计。
// 遵循 AGENTS.md §4.1 数据安全红线：若原正文非空且与新内容不同，
// 必然先将原正文归档至 chapter_versions，再覆写 chapters.content。
func (s *Store) SaveChapterContent(
	ctx context.Context,
	chapterID int64,
	content string,
	status domain.ChapterStatus,
	source domain.VersionSource,
	modelName string,
	briefingJSON string,
) (domain.Chapter, error) {
	if chapterID <= 0 {
		return domain.Chapter{}, &domain.ValidationError{Field: "chapterId", Message: "章节 ID 必须大于 0"}
	}
	if status != "" && !status.Valid() {
		return domain.Chapter{}, &domain.ValidationError{Field: "status", Message: "无效的章节状态"}
	}
	if source != "" && !source.Valid() {
		return domain.Chapter{}, &domain.ValidationError{Field: "source", Message: "无效的快照来源类型"}
	}
	if source == "" {
		source = domain.VersionSourceManualBackup
	}

	var result domain.Chapter
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		existing, err := getChapterRow(ctx, tx, chapterID)
		if err != nil {
			return err
		}

		// 红线：旧正文非空且有变化时，强制先写入历史版本快照再覆写主表。
		if existing.Content != "" && existing.Content != content {
			oldWordCount := domain.CountWords(existing.Content)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO chapter_versions (chapter_id, source, model_name, content, word_count)
				VALUES (?, ?, ?, ?, ?)
			`, chapterID, string(source), strings.TrimSpace(modelName), existing.Content, oldWordCount); err != nil {
				return mapSQLiteError(err)
			}
		}

		nextStatus := status
		if nextStatus == "" {
			nextStatus = existing.Status
		}
		nextBriefing := existing.LastBriefingJSON
		if strings.TrimSpace(briefingJSON) != "" {
			nextBriefing = briefingJSON
		}
		newWordCount := domain.CountWords(content)

		if _, err := tx.ExecContext(ctx, `
			UPDATE chapters
			SET content = ?,
			    word_count = ?,
			    status = ?,
			    last_briefing_json = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, content, newWordCount, string(nextStatus), nextBriefing, chapterID); err != nil {
			return mapSQLiteError(err)
		}
		if err := recordDailyWritingDeltaTx(ctx, tx, existing.BookID, newWordCount-existing.WordCount, s.schemaVersion >= 8); err != nil {
			return err
		}

		result, err = getChapterRow(ctx, tx, chapterID)
		return err
	})
	if err != nil {
		return domain.Chapter{}, err
	}
	return result, nil
}

// SaveChapterDraft 面向 app 层手动保存草稿的便捷事务接口（可选同时更新标题），
// 与 SaveChapterContent 一样强制先归档旧正文到 chapter_versions 再覆写。
func (s *Store) SaveChapterDraft(ctx context.Context, in domain.SaveChapterDraftInput) (domain.Chapter, error) {
	if err := in.Validate(); err != nil {
		return domain.Chapter{}, err
	}

	source := in.BackupSource
	if source == "" {
		source = domain.VersionSourceManualBackup
	}

	var result domain.Chapter
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		existing, err := getChapterRow(ctx, tx, in.ChapterID)
		if err != nil {
			return err
		}

		// 红线：旧正文非空且有变化时，强制先写入历史版本快照再覆写主表。
		if existing.Content != "" && existing.Content != in.Content {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO chapter_versions (chapter_id, source, model_name, content, word_count)
				VALUES (?, ?, ?, ?, ?)
			`, in.ChapterID, string(source), strings.TrimSpace(in.ModelName), existing.Content, domain.CountWords(existing.Content)); err != nil {
				return mapSQLiteError(err)
			}
		}

		nextTitle := existing.Title
		if trimmed := strings.TrimSpace(in.Title); trimmed != "" {
			nextTitle = trimmed
		}
		nextStatus := existing.Status
		if in.Status != "" {
			nextStatus = in.Status
		}
		newWordCount := domain.CountWords(in.Content)

		if _, err := tx.ExecContext(ctx, `
			UPDATE chapters
			SET title = ?,
			    content = ?,
			    word_count = ?,
			    status = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, nextTitle, in.Content, newWordCount, string(nextStatus), in.ChapterID); err != nil {
			return mapSQLiteError(err)
		}
		if err := recordDailyWritingDeltaTx(ctx, tx, existing.BookID, newWordCount-existing.WordCount, s.schemaVersion >= 8); err != nil {
			return err
		}

		result, err = getChapterRow(ctx, tx, in.ChapterID)
		return err
	})
	if err != nil {
		return domain.Chapter{}, err
	}
	return result, nil
}

// FinalizeChapter 将章节状态置为 finalized（已定稿）。
func (s *Store) FinalizeChapter(ctx context.Context, chapterID int64) (domain.Chapter, error) {
	if chapterID <= 0 {
		return domain.Chapter{}, &domain.ValidationError{Field: "chapterId", Message: "章节 ID 必须大于 0"}
	}

	var result domain.Chapter
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE chapters
			SET status = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, string(domain.ChapterStatusFinalized), chapterID)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("chapter %d: %w", chapterID, domain.ErrNotFound)
		}
		result, err = getChapterRow(ctx, tx, chapterID)
		return err
	})
	if err != nil {
		return domain.Chapter{}, err
	}
	return result, nil
}

// DeleteChapter 在单个事务内删除指定章节，并顺滑重排全书 global_seq 与卷内 vol_seq。
func (s *Store) DeleteChapter(ctx context.Context, chapterID int64) error {
	if chapterID <= 0 {
		return &domain.ValidationError{Field: "chapterId", Message: "章节 ID 必须大于 0"}
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		existing, err := getChapterRow(ctx, tx, chapterID)
		if err != nil {
			return err
		}
		if err := snapshotChapterTrashTx(ctx, tx, chapterID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM chapters WHERE id = ?`, chapterID); err != nil {
			return mapSQLiteError(err)
		}
		return resequenceBookChaptersTx(ctx, tx, existing.BookID)
	})
}

// BatchFinalizeChapters 批量把章节状态改为 finalized，返回实际更新章数。
func (s *Store) BatchFinalizeChapters(ctx context.Context, chapterIDs []int64) (int, error) {
	ids := uniquePositiveIDs(chapterIDs)
	if len(ids) == 0 {
		return 0, &domain.ValidationError{Field: "chapterIds", Message: "请选择至少一章"}
	}
	var updated int
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			res, err := tx.ExecContext(ctx, `
				UPDATE chapters
				SET status = 'finalized', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ?
			`, id)
			if err != nil {
				return mapSQLiteError(err)
			}
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			updated += int(affected)
		}
		return nil
	})
	return updated, err
}

// BatchDeleteChapters 批量删除章节（逐章写入回收站快照）并重排受影响书籍的章序。
func (s *Store) BatchDeleteChapters(ctx context.Context, chapterIDs []int64) (int, error) {
	ids := uniquePositiveIDs(chapterIDs)
	if len(ids) == 0 {
		return 0, &domain.ValidationError{Field: "chapterIds", Message: "请选择至少一章"}
	}
	var deleted int
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		books := make(map[int64]struct{})
		for _, id := range ids {
			chapter, err := getChapterRow(ctx, tx, id)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					continue
				}
				return err
			}
			if err := snapshotChapterTrashTx(ctx, tx, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM chapters WHERE id = ?`, id); err != nil {
				return mapSQLiteError(err)
			}
			books[chapter.BookID] = struct{}{}
			deleted++
		}
		for bookID := range books {
			if err := resequenceBookChaptersTx(ctx, tx, bookID); err != nil {
				return err
			}
		}
		return nil
	})
	return deleted, err
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// InsertChapter 在指定卷内某章之后（afterChapterId=0 表示卷首）插入新章，并重排全书与卷内章序。
func (s *Store) InsertChapter(ctx context.Context, in domain.InsertChapterInput) (domain.Chapter, error) {
	if err := in.Validate(); err != nil {
		return domain.Chapter{}, err
	}

	emotion := in.EmotionTag
	if emotion == "" {
		emotion = domain.EmotionSetup
	}
	hookType := in.HookType
	if hookType == "" {
		hookType = domain.HookMystery
	}

	var result domain.Chapter
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var volBookID int64
		if err := tx.QueryRowContext(ctx, `SELECT book_id FROM volumes WHERE id = ?`, in.VolumeID).Scan(&volBookID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("volume %d: %w", in.VolumeID, domain.ErrNotFound)
			}
			return mapSQLiteError(err)
		}
		if volBookID != in.BookID {
			return &domain.ValidationError{Field: "volumeId", Message: "卷不属于指定书籍"}
		}

		volIDs, err := listVolumeChapterIDsTx(ctx, tx, in.VolumeID)
		if err != nil {
			return err
		}
		insertIdx := 0
		if in.AfterChapterID > 0 {
			found := false
			for i, cid := range volIDs {
				if cid == in.AfterChapterID {
					insertIdx = i + 1
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("after chapter %d not in volume %d: %w", in.AfterChapterID, in.VolumeID, domain.ErrNotFound)
			}
		}

		// 先用临时不冲突的 global_seq 插入，随后统一按卷序与卷内序重排。
		var maxGlobal int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(global_seq), 0) FROM chapters WHERE book_id = ?`, in.BookID).Scan(&maxGlobal); err != nil {
			return mapSQLiteError(err)
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO chapters (
				book_id, volume_id, global_seq, vol_seq, title, status,
				emotion_tag, hook_type, hook_note, outline_summary
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, in.BookID, in.VolumeID, maxGlobal+1000, len(volIDs)+1, strings.TrimSpace(in.Title),
			string(domain.ChapterStatusPlanned), string(emotion), string(hookType), in.HookNote, in.OutlineSummary)
		if err != nil {
			return mapSQLiteError(err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		// 构造新卷内顺序并写回 vol_seq，再全书重排 global_seq。
		ordered := make([]int64, 0, len(volIDs)+1)
		ordered = append(ordered, volIDs[:insertIdx]...)
		ordered = append(ordered, newID)
		ordered = append(ordered, volIDs[insertIdx:]...)
		for i, cid := range ordered {
			if _, err := tx.ExecContext(ctx, `UPDATE chapters SET vol_seq = ? WHERE id = ?`, i+1, cid); err != nil {
				return mapSQLiteError(err)
			}
		}

		if err := resequenceBookChaptersTx(ctx, tx, in.BookID); err != nil {
			return err
		}
		result, err = getChapterRow(ctx, tx, newID)
		return err
	})
	if err != nil {
		return domain.Chapter{}, err
	}
	return result, nil
}

// MoveChapter 调整章节在卷内的顺序（up / down）或跨卷移动，并保持全书 global_seq 与卷内 vol_seq 连续唯一。
func (s *Store) MoveChapter(ctx context.Context, in domain.MoveChapterInput) (domain.Chapter, error) {
	if err := in.Validate(); err != nil {
		return domain.Chapter{}, err
	}

	var result domain.Chapter
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		chap, err := getChapterRow(ctx, tx, in.ChapterID)
		if err != nil {
			return err
		}

		dir := strings.ToLower(strings.TrimSpace(in.Direction))
		if in.TargetVolumeID > 0 && in.TargetVolumeID == chap.VolumeID && in.TargetVolSeq > 0 {
			// 同卷内按目标序号重排（拖拽排序）。
			volIDs, err := listVolumeChapterIDsTx(ctx, tx, chap.VolumeID)
			if err != nil {
				return err
			}
			ordered := make([]int64, 0, len(volIDs))
			for _, cid := range volIDs {
				if cid != chap.ID {
					ordered = append(ordered, cid)
				}
			}
			insertIdx := in.TargetVolSeq - 1
			if insertIdx < 0 {
				insertIdx = 0
			}
			if insertIdx > len(ordered) {
				insertIdx = len(ordered)
			}
			ordered = append(ordered[:insertIdx], append([]int64{chap.ID}, ordered[insertIdx:]...)...)
			for i, cid := range ordered {
				if _, err := tx.ExecContext(ctx, `UPDATE chapters SET vol_seq = ? WHERE id = ?`, i+1, cid); err != nil {
					return mapSQLiteError(err)
				}
			}
		} else if in.TargetVolumeID > 0 && in.TargetVolumeID != chap.VolumeID {
			var targetBookID int64
			if err := tx.QueryRowContext(ctx, `SELECT book_id FROM volumes WHERE id = ?`, in.TargetVolumeID).Scan(&targetBookID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("volume %d: %w", in.TargetVolumeID, domain.ErrNotFound)
				}
				return mapSQLiteError(err)
			}
			if targetBookID != chap.BookID {
				return &domain.ValidationError{Field: "targetVolumeId", Message: "目标分卷不属于当前书籍"}
			}
			targetIDs, err := listVolumeChapterIDsTx(ctx, tx, in.TargetVolumeID)
			if err != nil {
				return err
			}
			insertIdx := len(targetIDs)
			if in.TargetVolSeq > 0 && in.TargetVolSeq-1 < len(targetIDs) {
				insertIdx = in.TargetVolSeq - 1
			}
			if _, err := tx.ExecContext(ctx, `UPDATE chapters SET volume_id = ? WHERE id = ?`, in.TargetVolumeID, chap.ID); err != nil {
				return mapSQLiteError(err)
			}
			ordered := make([]int64, 0, len(targetIDs)+1)
			ordered = append(ordered, targetIDs[:insertIdx]...)
			ordered = append(ordered, chap.ID)
			ordered = append(ordered, targetIDs[insertIdx:]...)
			for i, cid := range ordered {
				if _, err := tx.ExecContext(ctx, `UPDATE chapters SET vol_seq = ? WHERE id = ?`, i+1, cid); err != nil {
					return mapSQLiteError(err)
				}
			}
		} else if dir == "up" || dir == "down" {
			volIDs, err := listVolumeChapterIDsTx(ctx, tx, chap.VolumeID)
			if err != nil {
				return err
			}
			idx := -1
			for i, cid := range volIDs {
				if cid == chap.ID {
					idx = i
					break
				}
			}
			if idx < 0 {
				return fmt.Errorf("chapter %d: %w", chap.ID, domain.ErrNotFound)
			}
			swapIdx := idx - 1
			if dir == "down" {
				swapIdx = idx + 1
			}
			if swapIdx >= 0 && swapIdx < len(volIDs) {
				volIDs[idx], volIDs[swapIdx] = volIDs[swapIdx], volIDs[idx]
				for i, cid := range volIDs {
					if _, err := tx.ExecContext(ctx, `UPDATE chapters SET vol_seq = ? WHERE id = ?`, i+1, cid); err != nil {
						return mapSQLiteError(err)
					}
				}
			}
		}

		if err := resequenceBookChaptersTx(ctx, tx, chap.BookID); err != nil {
			return err
		}
		result, err = getChapterRow(ctx, tx, chap.ID)
		return err
	})
	if err != nil {
		return domain.Chapter{}, err
	}
	return result, nil
}

// ReplaceBookText 在整本书范围内查找并替换文本（如全书改角色/招式名）。
// 严格遵守 AGENTS.md §4.1 数据安全红线：每章正文变动前强制先归档至 chapter_versions。
func (s *Store) ReplaceBookText(ctx context.Context, in domain.ReplaceBookTextInput) (domain.ReplaceBookTextResult, error) {
	if err := in.Validate(); err != nil {
		return domain.ReplaceBookTextResult{}, err
	}

	var res domain.ReplaceBookTextResult
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+chapterColumns+` FROM chapters WHERE book_id = ? ORDER BY global_seq ASC, id ASC`, in.BookID)
		if err != nil {
			return mapSQLiteError(err)
		}
		chaps, err := scanChapterRows(rows)
		_ = rows.Close()
		if err != nil {
			return err
		}

		for _, ch := range chaps {
			contentHits := strings.Count(ch.Content, in.Find)
			titleHits := 0
			if in.IncludeTitles {
				titleHits = strings.Count(ch.Title, in.Find)
			}
			outlineHits := 0
			if in.IncludeOutlines {
				outlineHits = strings.Count(ch.OutlineSummary, in.Find)
			}
			chapHits := contentHits + titleHits + outlineHits
			if chapHits == 0 {
				continue
			}

			res.AffectedChapters++
			res.TotalMatches += chapHits
			if in.DryRun {
				continue
			}

			newContent := ch.Content
			if contentHits > 0 {
				// 红线：覆写正文前先归档旧版本快照。
				if ch.Content != "" {
					if _, err := tx.ExecContext(ctx, `
						INSERT INTO chapter_versions (chapter_id, source, model_name, content, word_count)
						VALUES (?, ?, '', ?, ?)
					`, ch.ID, string(domain.VersionSourceManualBackup), ch.Content, domain.CountWords(ch.Content)); err != nil {
						return mapSQLiteError(err)
					}
				}
				newContent = strings.ReplaceAll(ch.Content, in.Find, in.Replace)
			}

			newTitle := ch.Title
			if titleHits > 0 {
				replacedTitle := strings.TrimSpace(strings.ReplaceAll(ch.Title, in.Find, in.Replace))
				if replacedTitle != "" {
					newTitle = replacedTitle
				}
			}
			newOutline := ch.OutlineSummary
			if outlineHits > 0 {
				newOutline = strings.ReplaceAll(ch.OutlineSummary, in.Find, in.Replace)
			}

			if _, err := tx.ExecContext(ctx, `
				UPDATE chapters
				SET title = ?,
				    outline_summary = ?,
				    content = ?,
				    word_count = ?,
				    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ?
			`, newTitle, newOutline, newContent, domain.CountWords(newContent), ch.ID); err != nil {
				return mapSQLiteError(err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.ReplaceBookTextResult{}, err
	}
	return res, nil
}

func listVolumeChapterIDsTx(ctx context.Context, tx *sql.Tx, volumeID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM chapters
		WHERE volume_id = ?
		ORDER BY vol_seq ASC, global_seq ASC, id ASC
	`, volumeID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, mapSQLiteError(err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// resequenceBookChaptersTx 按卷序（volumes.seq ASC）与卷内章序（chapters.vol_seq ASC）重排全书 global_seq 与各卷 vol_seq。
// 先使用负数临时值规避 UNIQUE(book_id, global_seq) 约束碰撞，再写回连续的 1..N。
func resequenceBookChaptersTx(ctx context.Context, tx *sql.Tx, bookID int64) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT c.id, c.volume_id
		FROM chapters c
		JOIN volumes v ON v.id = c.volume_id
		WHERE c.book_id = ?
		ORDER BY v.seq ASC, v.id ASC, c.vol_seq ASC, c.global_seq ASC, c.id ASC
	`, bookID)
	if err != nil {
		return mapSQLiteError(err)
	}
	type item struct {
		id    int64
		volID int64
	}
	ordered := make([]item, 0)
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.volID); err != nil {
			_ = rows.Close()
			return mapSQLiteError(err)
		}
		ordered = append(ordered, it)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return mapSQLiteError(err)
	}
	_ = rows.Close()

	if len(ordered) == 0 {
		return nil
	}

	// Phase 1: 写入负数临时 global_seq，消除交换顺序时的唯一索引冲突。
	for i, it := range ordered {
		if _, err := tx.ExecContext(ctx, `UPDATE chapters SET global_seq = ? WHERE id = ?`, -(i + 1), it.id); err != nil {
			return mapSQLiteError(err)
		}
	}

	// Phase 2: 写入连续 1..N 的 global_seq 与各卷内连续 1..M 的 vol_seq。
	volCounters := make(map[int64]int)
	for i, it := range ordered {
		volCounters[it.volID]++
		if _, err := tx.ExecContext(ctx, `
			UPDATE chapters SET global_seq = ?, vol_seq = ? WHERE id = ?
		`, i+1, volCounters[it.volID], it.id); err != nil {
			return mapSQLiteError(err)
		}
	}
	// 同步更新事件溯源状态账本中的 chapter_seq，避免重排后 SnapshotAt 时间轴漂移。
	if _, err := tx.ExecContext(ctx, `
		UPDATE entity_state_events
		SET chapter_seq = (SELECT global_seq FROM chapters WHERE chapters.id = entity_state_events.chapter_id)
		WHERE book_id = ?
		  AND EXISTS (SELECT 1 FROM chapters WHERE chapters.id = entity_state_events.chapter_id)
	`, bookID); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

// ListBeats 返回指定章节的全部节拍卡（按 seq 升序）。
func (s *Store) ListBeats(ctx context.Context, chapterID int64) ([]domain.Beat, error) {
	return listBeatsQuery(ctx, s.db, chapterID)
}

// ListBookBeats 一次性按 chapter_id 分组返回整本书的全部节拍卡（按全局章序与节拍序号升序）。
func (s *Store) ListBookBeats(ctx context.Context, bookID int64) (map[int64][]domain.Beat, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.chapter_id, b.seq, b.stage, b.summary, b.conflict_note,
		       b.pov_entity_id, b.entity_ids_json, b.target_words
		FROM chapter_beats b
		JOIN chapters c ON c.id = b.chapter_id
		WHERE c.book_id = ?
		ORDER BY c.global_seq ASC, b.seq ASC, b.id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	grouped := make(map[int64][]domain.Beat)
	for rows.Next() {
		b, err := scanBeat(rows)
		if err != nil {
			return nil, err
		}
		grouped[b.ChapterID] = append(grouped[b.ChapterID], b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return grouped, nil
}

// ReplaceBeats 在单个事务内清空并按 1..N 顺序重写章节的全部节拍卡。
func (s *Store) ReplaceBeats(ctx context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error) {
	if chapterID <= 0 {
		return nil, &domain.ValidationError{Field: "chapterId", Message: "章节 ID 必须大于 0"}
	}
	for i, b := range beats {
		if err := b.Validate(); err != nil {
			return nil, fmt.Errorf("beat[%d]: %w", i, err)
		}
	}

	var result []domain.Beat
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM chapters WHERE id = ?`, chapterID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("chapter %d: %w", chapterID, domain.ErrNotFound)
			}
			return mapSQLiteError(err)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM chapter_beats WHERE chapter_id = ?`, chapterID); err != nil {
			return mapSQLiteError(err)
		}

		for i, b := range beats {
			seq := i + 1
			targetWords := b.TargetWords
			if targetWords == 0 {
				targetWords = domain.DefaultBeatTargetWords
			}
			var pov sql.NullInt64
			if b.POVEntityID != nil {
				pov = sql.NullInt64{Int64: *b.POVEntityID, Valid: true}
			}
			entityIDsJSON, err := marshalInt64Slice(b.EntityIDs)
			if err != nil {
				return fmt.Errorf("marshal beat[%d] entityIds: %w", i, err)
			}

			if _, err := tx.ExecContext(ctx, `
				INSERT INTO chapter_beats (
					chapter_id, seq, stage, summary, conflict_note,
					pov_entity_id, entity_ids_json, target_words
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, chapterID, seq, string(b.Stage), strings.TrimSpace(b.Summary), b.ConflictNote, pov, entityIDsJSON, targetWords); err != nil {
				return mapSQLiteError(err)
			}
		}

		var err error
		result, err = listBeatsQuery(ctx, tx, chapterID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListChapterVersions 返回指定章节的全部历史版本快照（按最新在前 id DESC 排序）。
func (s *Store) ListChapterVersions(ctx context.Context, chapterID int64) ([]domain.ChapterVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, chapter_id, source, model_name, content, word_count, created_at
		FROM chapter_versions
		WHERE chapter_id = ?
		ORDER BY id DESC
	`, chapterID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.ChapterVersion, 0)
	for rows.Next() {
		v, err := scanChapterVersion(rows)
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

// RestoreChapterVersion 在单个事务内先将当前章节正文以 source='restore_backup' 归档，
// 再将章节正文回滚为目标快照内容。
func (s *Store) RestoreChapterVersion(ctx context.Context, versionID int64) (domain.Chapter, error) {
	if versionID <= 0 {
		return domain.Chapter{}, &domain.ValidationError{Field: "versionId", Message: "版本 ID 必须大于 0"}
	}

	var result domain.Chapter
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `
			SELECT id, chapter_id, source, model_name, content, word_count, created_at
			FROM chapter_versions
			WHERE id = ?
		`, versionID)
		target, err := scanChapterVersion(row)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("chapter version %d: %w", versionID, domain.ErrNotFound)
			}
			return err
		}

		current, err := getChapterRow(ctx, tx, target.ChapterID)
		if err != nil {
			return err
		}

		// 红线：回滚前必须先将当前正文归档为 restore_backup。
		if current.Content != "" {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO chapter_versions (chapter_id, source, model_name, content, word_count)
				VALUES (?, ?, '', ?, ?)
			`, current.ID, string(domain.VersionSourceRestoreBackup), current.Content, domain.CountWords(current.Content)); err != nil {
				return mapSQLiteError(err)
			}
		}

		restoredWords := domain.CountWords(target.Content)
		if _, err := tx.ExecContext(ctx, `
			UPDATE chapters
			SET content = ?,
			    word_count = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, target.Content, restoredWords, current.ID); err != nil {
			return mapSQLiteError(err)
		}

		result, err = getChapterRow(ctx, tx, current.ID)
		return err
	})
	if err != nil {
		return domain.Chapter{}, err
	}
	return result, nil
}

// RestoreVersion 是 RestoreChapterVersion 的语义别名，直接匹配 app.Store 接口。
func (s *Store) RestoreVersion(ctx context.Context, versionID int64) (domain.Chapter, error) {
	return s.RestoreChapterVersion(ctx, versionID)
}

func scanChapter(sc rowScanner) (domain.Chapter, error) {
	var (
		c          domain.Chapter
		status     string
		emotionTag string
		hookType   string
		storyDay   sql.NullInt64
	)
	if err := sc.Scan(
		&c.ID, &c.BookID, &c.VolumeID, &c.GlobalSeq, &c.VolSeq, &c.Title, &status,
		&emotionTag, &hookType, &c.HookNote, &c.OutlineSummary, &c.CausalSummary,
		&c.Content, &c.WordCount, &c.LastBriefingJSON, &c.StoryTimeLabel, &storyDay,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return domain.Chapter{}, mapSQLiteError(err)
	}
	c.Status = domain.ChapterStatus(status)
	c.EmotionTag = domain.EmotionTag(emotionTag)
	c.HookType = domain.HookType(hookType)
	if storyDay.Valid {
		d := int(storyDay.Int64)
		c.StoryDay = &d
	}
	return c, nil
}

func scanChapterRows(rows *sql.Rows) ([]domain.Chapter, error) {
	out := make([]domain.Chapter, 0)
	for rows.Next() {
		c, err := scanChapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func getChapterRow(ctx context.Context, q queryRower, id int64) (domain.Chapter, error) {
	row := q.QueryRowContext(ctx, `SELECT `+chapterColumns+` FROM chapters WHERE id = ?`, id)
	c, err := scanChapter(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Chapter{}, fmt.Errorf("chapter %d: %w", id, domain.ErrNotFound)
		}
		return domain.Chapter{}, err
	}
	return c, nil
}

func listBeatsQuery(ctx context.Context, q querier, chapterID int64) ([]domain.Beat, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, chapter_id, seq, stage, summary, conflict_note, pov_entity_id, entity_ids_json, target_words
		FROM chapter_beats
		WHERE chapter_id = ?
		ORDER BY seq ASC, id ASC
	`, chapterID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.Beat, 0)
	for rows.Next() {
		b, err := scanBeat(rows)
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

func scanBeat(sc rowScanner) (domain.Beat, error) {
	var (
		b             domain.Beat
		stage         string
		pov           sql.NullInt64
		entityIDsJSON string
	)
	if err := sc.Scan(&b.ID, &b.ChapterID, &b.Seq, &stage, &b.Summary, &b.ConflictNote, &pov, &entityIDsJSON, &b.TargetWords); err != nil {
		return domain.Beat{}, mapSQLiteError(err)
	}
	b.Stage = domain.BeatStage(stage)
	if pov.Valid {
		id := pov.Int64
		b.POVEntityID = &id
	}
	b.EntityIDs = unmarshalInt64Slice(entityIDsJSON)
	return b, nil
}

func scanChapterVersion(sc rowScanner) (domain.ChapterVersion, error) {
	var (
		v      domain.ChapterVersion
		source string
	)
	if err := sc.Scan(&v.ID, &v.ChapterID, &source, &v.ModelName, &v.Content, &v.WordCount, &v.CreatedAt); err != nil {
		return domain.ChapterVersion{}, mapSQLiteError(err)
	}
	v.Source = domain.VersionSource(source)
	return v, nil
}

func marshalInt64Slice(ids []int64) (string, error) {
	clean := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalInt64Slice(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return []int64{}
	}
	var out []int64
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []int64{}
	}
	return out
}
