package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"nib/internal/domain"
)

const (
	maxSafetySnapshotsPerKeep = 20
	snippetContextRunes       = 14
)

// CreateSafetySnapshot 为磁盘库创建一次带元数据的轻量安全撤销点（内存库安全返回零值）。
func (s *Store) CreateSafetySnapshot(ctx context.Context, bookID int64, source string, sourceRefID int64, summary string) (domain.SafetySnapshot, error) {
	if s == nil || s.db == nil {
		return domain.SafetySnapshot{}, errors.New("database not initialized")
	}
	if isMemoryPath(s.path) {
		return domain.SafetySnapshot{}, nil
	}

	cleanSource := sanitizePathToken(source)
	if cleanSource == "" {
		cleanSource = "manual"
	}
	snapDir := filepath.Join(filepath.Dir(s.path), "safety_snapshots")
	fileName := fmt.Sprintf("snap-%s-%d.db", cleanSource, time.Now().UnixNano())
	targetPath := filepath.Join(snapDir, fileName)

	if err := s.BackupToFile(ctx, targetPath); err != nil {
		return domain.SafetySnapshot{}, err
	}

	var saved domain.SafetySnapshot
	var stalePaths []string
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO safety_snapshots (book_id, source, source_ref_id, summary, file_path)
			VALUES (?, ?, ?, ?, ?)
		`, bookID, strings.TrimSpace(source), sourceRefID, strings.TrimSpace(summary), targetPath)
		if err != nil {
			return mapSQLiteError(err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		var createdAt string
		_ = tx.QueryRowContext(ctx, `SELECT created_at FROM safety_snapshots WHERE id = ?`, newID).Scan(&createdAt)
		saved = domain.SafetySnapshot{
			ID:          newID,
			BookID:      bookID,
			Source:      strings.TrimSpace(source),
			SourceRefID: sourceRefID,
			Summary:     strings.TrimSpace(summary),
			FilePath:    targetPath,
			CreatedAt:   createdAt,
		}

		// 滚动清理超出保留上限的旧快照记录及磁盘文件
		rows, err := tx.QueryContext(ctx, `
			SELECT id, file_path
			FROM safety_snapshots
			ORDER BY id DESC
			LIMIT -1 OFFSET ?
		`, maxSafetySnapshotsPerKeep)
		if err == nil {
			var staleIDs []int64
			for rows.Next() {
				var (
					id int64
					fp string
				)
				if scanErr := rows.Scan(&id, &fp); scanErr == nil {
					staleIDs = append(staleIDs, id)
					if fp != "" {
						stalePaths = append(stalePaths, fp)
					}
				}
			}
			_ = rows.Close()
			for _, id := range staleIDs {
				_, _ = tx.ExecContext(ctx, `DELETE FROM safety_snapshots WHERE id = ?`, id)
			}
		}
		return nil
	})
	if err != nil {
		_ = os.Remove(targetPath)
		return domain.SafetySnapshot{}, err
	}

	for _, fp := range stalePaths {
		_ = os.Remove(fp)
	}
	return saved, nil
}

// ListSafetySnapshots 列出指定书籍（bookID<=0 时列出全部）的轻量撤销点记录（最新在前）。
func (s *Store) ListSafetySnapshots(ctx context.Context, bookID int64) ([]domain.SafetySnapshot, error) {
	q := `
		SELECT id, book_id, source, source_ref_id, summary, file_path, created_at
		FROM safety_snapshots
	`
	var args []any
	if bookID > 0 {
		q += ` WHERE book_id = ? OR book_id = 0`
		args = append(args, bookID)
	}
	q += ` ORDER BY id DESC LIMIT 30`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	return scanAll(rows, func(sc rowScanner) (domain.SafetySnapshot, error) {
		var item domain.SafetySnapshot
		if err := sc.Scan(&item.ID, &item.BookID, &item.Source, &item.SourceRefID, &item.Summary, &item.FilePath, &item.CreatedAt); err != nil {
			return domain.SafetySnapshot{}, mapSQLiteError(err)
		}
		return item, nil
	})
}

// RestoreSafetySnapshot 通过 safety_snapshots.id 原子恢复数据库至对应撤销点状态，并返回恢复前自动生成的热备路径。
func (s *Store) RestoreSafetySnapshot(ctx context.Context, snapshotID int64) (string, error) {
	if snapshotID <= 0 {
		return "", &domain.ValidationError{Field: "snapshotId", Message: "撤销点 ID 必须大于 0"}
	}
	var filePath string
	if err := s.db.QueryRowContext(ctx, `SELECT file_path FROM safety_snapshots WHERE id = ?`, snapshotID).Scan(&filePath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("safety snapshot %d: %w", snapshotID, domain.ErrNotFound)
		}
		return "", mapSQLiteError(err)
	}
	if strings.TrimSpace(filePath) == "" {
		return "", &domain.ValidationError{Field: "snapshotId", Message: "该撤销点无有效快照文件路径"}
	}
	return s.RestoreDataFromFile(ctx, filePath)
}

// PreviewEntityMerge 统计将源实体合并至目标实体的关联引用与可选正文命中清单。
func (s *Store) PreviewEntityMerge(ctx context.Context, in domain.EntityMergePreviewInput) (domain.EntityMergePreview, error) {
	if err := in.Validate(); err != nil {
		return domain.EntityMergePreview{}, err
	}

	src, err := getEntityRow(ctx, s.db, in.SourceEntityID)
	if err != nil {
		return domain.EntityMergePreview{}, err
	}
	dst, err := getEntityRow(ctx, s.db, in.TargetEntityID)
	if err != nil {
		return domain.EntityMergePreview{}, err
	}
	if src.BookID != in.BookID || dst.BookID != in.BookID {
		return domain.EntityMergePreview{}, &domain.ValidationError{Field: "entityId", Message: "实体不属于当前书籍"}
	}

	preview := domain.EntityMergePreview{
		SourceName: src.Name,
		TargetName: dst.Name,
		TextHits:   make([]domain.TextImpactHit, 0),
	}

	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entity_state_events WHERE book_id = ? AND entity_id = ?`, in.BookID, src.ID).Scan(&preview.StateEventCount)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entity_relations WHERE book_id = ? AND (source_entity_id = ? OR target_entity_id = ?)`, in.BookID, src.ID, src.ID).Scan(&preview.RelationCount)

	fsList, err := s.ListForeshadowings(ctx, in.BookID, "")
	if err == nil {
		for _, fs := range fsList {
			if slices.Contains(fs.InvolvedEntityIDs, src.ID) {
				preview.ForeshadowCount++
			}
		}
	}

	beatRows, err := s.db.QueryContext(ctx, `
		SELECT b.pov_entity_id, b.entity_ids_json
		FROM chapter_beats b
		JOIN chapters c ON c.id = b.chapter_id
		WHERE c.book_id = ?
	`, in.BookID)
	if err == nil {
		for beatRows.Next() {
			var (
				pov     sql.NullInt64
				idsJSON string
			)
			if scanErr := beatRows.Scan(&pov, &idsJSON); scanErr == nil {
				if (pov.Valid && pov.Int64 == src.ID) || slices.Contains(unmarshalInt64Slice(idsJSON), src.ID) {
					preview.BeatCount++
				}
			}
		}
		_ = beatRows.Close()
	}

	if in.ReplaceInProse && strings.TrimSpace(src.Name) != "" && strings.TrimSpace(src.Name) != strings.TrimSpace(dst.Name) {
		hits, err := s.scanChapterContentHits(ctx, in.BookID, src.Name)
		if err != nil {
			return domain.EntityMergePreview{}, err
		}
		preview.TextHits = hits
	}

	return preview, nil
}

// ExecuteEntityMerge 在单事务内合并重复实体（归并别名/属性/状态事件/关系/伏笔/节拍，并按选项安全替换正文与删除源实体）。
func (s *Store) ExecuteEntityMerge(ctx context.Context, in domain.EntityMergeExecuteInput) (domain.EntityMergeResult, error) {
	if err := in.Validate(); err != nil {
		return domain.EntityMergeResult{}, err
	}

	var snapshotID int64
	if snap, snapErr := s.CreateSafetySnapshot(ctx, in.BookID, "entity_merge", in.TargetEntityID, fmt.Sprintf("实体合并：%d -> %d", in.SourceEntityID, in.TargetEntityID)); snapErr != nil {
		return domain.EntityMergeResult{}, snapErr
	} else {
		snapshotID = snap.ID
	}

	excluded := toStringSet(in.ExcludedHitIDs)
	var res domain.EntityMergeResult
	res.SnapshotID = snapshotID

	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		src, err := getEntityRow(ctx, tx, in.SourceEntityID)
		if err != nil {
			return err
		}
		dst, err := getEntityRow(ctx, tx, in.TargetEntityID)
		if err != nil {
			return err
		}
		if src.BookID != in.BookID || dst.BookID != in.BookID {
			return &domain.ValidationError{Field: "entityId", Message: "实体不属于当前书籍"}
		}

		// 1. 将源实体名及别名归并进目标实体 aliases，并补齐缺失属性
		mergedAliases := append(append([]string(nil), dst.Aliases...), src.Name)
		mergedAliases = append(mergedAliases, src.Aliases...)
		filteredAliases := make([]string, 0, len(mergedAliases))
		targetTrim := strings.TrimSpace(dst.Name)
		for _, a := range mergedAliases {
			t := strings.TrimSpace(a)
			if t == "" || t == targetTrim {
				continue
			}
			filteredAliases = append(filteredAliases, t)
		}
		aliasesJSON, err := marshalStringSlice(filteredAliases)
		if err != nil {
			return err
		}

		mergedAttrs := make(map[string]string, len(dst.Attributes)+len(src.Attributes))
		for k, v := range src.Attributes {
			mergedAttrs[k] = v
		}
		for k, v := range dst.Attributes {
			mergedAttrs[k] = v
		}
		attrsJSON, err := marshalAttributes(mergedAttrs)
		if err != nil {
			return err
		}

		mergedSummary := strings.TrimSpace(dst.Summary)
		if mergedSummary == "" {
			mergedSummary = strings.TrimSpace(src.Summary)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE entities
			SET aliases_json = ?,
			    attributes_json = ?,
			    summary = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, aliasesJSON, attrsJSON, mergedSummary, dst.ID); err != nil {
			return mapSQLiteError(err)
		}

		// 2. 迁移事件溯源状态事件
		evRes, err := tx.ExecContext(ctx, `UPDATE entity_state_events SET entity_id = ? WHERE book_id = ? AND entity_id = ?`, dst.ID, in.BookID, src.ID)
		if err != nil {
			return mapSQLiteError(err)
		}
		if n, _ := evRes.RowsAffected(); n > 0 {
			res.MergedEvents = int(n)
		}

		// 3. 迁移故事内期限的关联实体外键
		if _, err := tx.ExecContext(ctx, `UPDATE story_deadlines SET entity_id = ? WHERE book_id = ? AND entity_id = ?`, dst.ID, in.BookID, src.ID); err != nil {
			return mapSQLiteError(err)
		}

		// 4. 迁移实体关系（剔除自环与完全重复边）
		mergedRels, err := migrateEntityRelationsTx(ctx, tx, in.BookID, src.ID, dst.ID)
		if err != nil {
			return err
		}
		res.MergedRelations = mergedRels

		// 5. 迁移伏笔 involved_entity_ids_json
		if err := replaceEntityIDInForeshadowingsTx(ctx, tx, in.BookID, src.ID, dst.ID); err != nil {
			return err
		}

		// 6. 迁移节拍卡 pov_entity_id 与 entity_ids_json
		if err := replaceEntityIDInBeatsTx(ctx, tx, in.BookID, src.ID, dst.ID); err != nil {
			return err
		}

		// 7. 可选替换历史章节正文（必先写 chapter_versions 快照）
		if in.ReplaceInProse && strings.TrimSpace(src.Name) != "" && strings.TrimSpace(src.Name) != targetTrim {
			updatedChaps, err := replaceChapterProseTx(ctx, tx, in.BookID, src.Name, targetTrim, excluded)
			if err != nil {
				return err
			}
			res.UpdatedChapters = updatedChaps
		}

		// 8. 删除已合并的源实体
		if _, err := tx.ExecContext(ctx, `DELETE FROM entities WHERE id = ? AND book_id = ?`, src.ID, in.BookID); err != nil {
			return mapSQLiteError(err)
		}

		res.TargetEntity, err = getEntityRow(ctx, tx, dst.ID)
		return err
	})
	if err != nil {
		return domain.EntityMergeResult{}, err
	}
	return res, nil
}

// PreviewBookRename 分模块预览全书安全改名命中清单，对粘连子串附带风险提示。
func (s *Store) PreviewBookRename(ctx context.Context, in domain.BookRenamePreviewInput) (domain.BookRenamePreview, error) {
	if err := in.Validate(); err != nil {
		return domain.BookRenamePreview{}, err
	}
	oldName := strings.TrimSpace(in.OldName)
	newName := strings.TrimSpace(in.NewName)

	preview := domain.BookRenamePreview{
		OldName: oldName,
		NewName: newName,
		Hits:    make([]domain.TextImpactHit, 0),
	}

	// 1. 章节正文命中
	proseHits, err := s.scanChapterContentHits(ctx, in.BookID, oldName)
	if err != nil {
		return domain.BookRenamePreview{}, err
	}
	preview.Hits = append(preview.Hits, proseHits...)

	// 2. 卷/章纲/节拍/摘要命中
	if in.IncludeOutlines {
		rows, err := s.db.QueryContext(ctx, `
			SELECT id, global_seq, title, outline_summary, causal_summary, hook_note
			FROM chapters WHERE book_id = ? ORDER BY global_seq ASC
		`, in.BookID)
		if err != nil {
			return domain.BookRenamePreview{}, mapSQLiteError(err)
		}
		for rows.Next() {
			var (
				id, seq                         int64
				title, outline, causal, hookNote string
			)
			if err := rows.Scan(&id, &seq, &title, &outline, &causal, &hookNote); err != nil {
				_ = rows.Close()
				return domain.BookRenamePreview{}, mapSQLiteError(err)
			}
			cnt := strings.Count(title, oldName) + strings.Count(outline, oldName) + strings.Count(causal, oldName) + strings.Count(hookNote, oldName)
			if cnt > 0 {
				sample := firstNonEmptyWithMatch(oldName, title, outline, causal, hookNote)
				preview.Hits = append(preview.Hits, buildTextImpactHit(
					fmt.Sprintf("chapter_meta:%d", id), "chapter_meta", id,
					fmt.Sprintf("第%d章元信息/章纲《%s》", seq, title), cnt, sample, oldName,
				))
			}
		}
		_ = rows.Close()

		beatRows, err := s.db.QueryContext(ctx, `
			SELECT b.id, c.global_seq, b.seq, b.summary, b.conflict_note
			FROM chapter_beats b
			JOIN chapters c ON c.id = b.chapter_id
			WHERE c.book_id = ?
			ORDER BY c.global_seq ASC, b.seq ASC
		`, in.BookID)
		if err != nil {
			return domain.BookRenamePreview{}, mapSQLiteError(err)
		}
		for beatRows.Next() {
			var (
				id, chapSeq int64
				beatSeq     int
				sum, conf   string
			)
			if err := beatRows.Scan(&id, &chapSeq, &beatSeq, &sum, &conf); err != nil {
				_ = beatRows.Close()
				return domain.BookRenamePreview{}, mapSQLiteError(err)
			}
			cnt := strings.Count(sum, oldName) + strings.Count(conf, oldName)
			if cnt > 0 {
				preview.Hits = append(preview.Hits, buildTextImpactHit(
					fmt.Sprintf("beat:%d", id), "beat", id,
					fmt.Sprintf("第%d章·第%d拍节拍卡", chapSeq, beatSeq), cnt, firstNonEmptyWithMatch(oldName, sum, conf), oldName,
				))
			}
		}
		_ = beatRows.Close()

		volRows, err := s.db.QueryContext(ctx, `SELECT id, seq, title, goal, climax_plan FROM volumes WHERE book_id = ? ORDER BY seq ASC`, in.BookID)
		if err != nil {
			return domain.BookRenamePreview{}, mapSQLiteError(err)
		}
		for volRows.Next() {
			var (
				id                   int64
				seq                  int
				title, goal, climax string
			)
			if err := volRows.Scan(&id, &seq, &title, &goal, &climax); err != nil {
				_ = volRows.Close()
				return domain.BookRenamePreview{}, mapSQLiteError(err)
			}
			cnt := strings.Count(title, oldName) + strings.Count(goal, oldName) + strings.Count(climax, oldName)
			if cnt > 0 {
				preview.Hits = append(preview.Hits, buildTextImpactHit(
					fmt.Sprintf("volume:%d", id), "volume", id,
					fmt.Sprintf("第%d卷《%s》卷纲", seq, title), cnt, firstNonEmptyWithMatch(oldName, title, goal, climax), oldName,
				))
			}
		}
		_ = volRows.Close()

		arcRows, err := s.db.QueryContext(ctx, `SELECT id, start_seq, end_seq, summary FROM arc_summaries WHERE book_id = ? ORDER BY start_seq ASC`, in.BookID)
		if err != nil {
			return domain.BookRenamePreview{}, mapSQLiteError(err)
		}
		for arcRows.Next() {
			var (
				id               int64
				startSeq, endSeq int
				summary          string
			)
			if err := arcRows.Scan(&id, &startSeq, &endSeq, &summary); err != nil {
				_ = arcRows.Close()
				return domain.BookRenamePreview{}, mapSQLiteError(err)
			}
			if cnt := strings.Count(summary, oldName); cnt > 0 {
				preview.Hits = append(preview.Hits, buildTextImpactHit(
					fmt.Sprintf("arc_summary:%d", id), "arc_summary", id,
					fmt.Sprintf("第%d–%d章弧线摘要", startSeq, endSeq), cnt, summary, oldName,
				))
			}
		}
		_ = arcRows.Close()
	}

	// 3. 设定档案/关系/伏笔/状态事件命中
	if in.IncludeMemory {
		entRows, err := s.db.QueryContext(ctx, `SELECT id, name, aliases_json, summary, attributes_json FROM entities WHERE book_id = ? ORDER BY id ASC`, in.BookID)
		if err != nil {
			return domain.BookRenamePreview{}, mapSQLiteError(err)
		}
		for entRows.Next() {
			var (
				id                                      int64
				name, aliasesJSON, summary, attrsJSON string
			)
			if err := entRows.Scan(&id, &name, &aliasesJSON, &summary, &attrsJSON); err != nil {
				_ = entRows.Close()
				return domain.BookRenamePreview{}, mapSQLiteError(err)
			}
			cnt := strings.Count(name, oldName) + strings.Count(summary, oldName) + countInJSONMapOrSlice(oldName, aliasesJSON, attrsJSON)
			if cnt > 0 {
				preview.Hits = append(preview.Hits, buildTextImpactHit(
					fmt.Sprintf("entity:%d", id), "entity", id,
					fmt.Sprintf("实体档案「%s」", name), cnt, firstNonEmptyWithMatch(oldName, name, summary, aliasesJSON, attrsJSON), oldName,
				))
			}
		}
		_ = entRows.Close()

		relRows, err := s.db.QueryContext(ctx, `SELECT id, relation_type, description FROM entity_relations WHERE book_id = ? ORDER BY id ASC`, in.BookID)
		if err != nil {
			return domain.BookRenamePreview{}, mapSQLiteError(err)
		}
		for relRows.Next() {
			var (
				id            int64
				relType, desc string
			)
			if err := relRows.Scan(&id, &relType, &desc); err != nil {
				_ = relRows.Close()
				return domain.BookRenamePreview{}, mapSQLiteError(err)
			}
			cnt := strings.Count(relType, oldName) + strings.Count(desc, oldName)
			if cnt > 0 {
				preview.Hits = append(preview.Hits, buildTextImpactHit(
					fmt.Sprintf("relation:%d", id), "relation", id,
					fmt.Sprintf("关系记录（%s）", relType), cnt, firstNonEmptyWithMatch(oldName, desc, relType), oldName,
				))
			}
		}
		_ = relRows.Close()

		fsRows, err := s.db.QueryContext(ctx, `SELECT id, title, description FROM foreshadowings WHERE book_id = ? ORDER BY id ASC`, in.BookID)
		if err != nil {
			return domain.BookRenamePreview{}, mapSQLiteError(err)
		}
		for fsRows.Next() {
			var (
				id          int64
				title, desc string
			)
			if err := fsRows.Scan(&id, &title, &desc); err != nil {
				_ = fsRows.Close()
				return domain.BookRenamePreview{}, mapSQLiteError(err)
			}
			cnt := strings.Count(title, oldName) + strings.Count(desc, oldName)
			if cnt > 0 {
				preview.Hits = append(preview.Hits, buildTextImpactHit(
					fmt.Sprintf("foreshadowing:%d", id), "foreshadowing", id,
					fmt.Sprintf("伏笔「%s」", title), cnt, firstNonEmptyWithMatch(oldName, title, desc), oldName,
				))
			}
		}
		_ = fsRows.Close()

		evRows, err := s.db.QueryContext(ctx, `SELECT id, chapter_seq, attr_key, prev_value, new_value, reason FROM entity_state_events WHERE book_id = ? ORDER BY id ASC`, in.BookID)
		if err != nil {
			return domain.BookRenamePreview{}, mapSQLiteError(err)
		}
		for evRows.Next() {
			var (
				id, seq                      int64
				key, prevVal, newVal, reason string
			)
			if err := evRows.Scan(&id, &seq, &key, &prevVal, &newVal, &reason); err != nil {
				_ = evRows.Close()
				return domain.BookRenamePreview{}, mapSQLiteError(err)
			}
			cnt := strings.Count(prevVal, oldName) + strings.Count(newVal, oldName) + strings.Count(reason, oldName)
			if cnt > 0 {
				preview.Hits = append(preview.Hits, buildTextImpactHit(
					fmt.Sprintf("state_event:%d", id), "state_event", id,
					fmt.Sprintf("第%d章状态事件（%s）", seq, key), cnt, firstNonEmptyWithMatch(oldName, newVal, prevVal, reason), oldName,
				))
			}
		}
		_ = evRows.Close()
	}

	for _, h := range preview.Hits {
		preview.TotalMatches += h.MatchCount
	}
	return preview, nil
}

// ExecuteBookRename 在事务内按作者勾选清单执行全书安全改名（改正文前必落 chapter_versions 快照且操作前创建统一撤销点）。
func (s *Store) ExecuteBookRename(ctx context.Context, in domain.BookRenameExecuteInput) (domain.BookRenameResult, error) {
	if err := in.Validate(); err != nil {
		return domain.BookRenameResult{}, err
	}
	oldName := strings.TrimSpace(in.OldName)
	newName := strings.TrimSpace(in.NewName)

	var snapshotID int64
	if snap, snapErr := s.CreateSafetySnapshot(ctx, in.BookID, "book_rename", in.BookID, fmt.Sprintf("全书改名：%s -> %s", oldName, newName)); snapErr != nil {
		return domain.BookRenameResult{}, snapErr
	} else {
		snapshotID = snap.ID
	}

	excluded := toStringSet(in.ExcludedHitIDs)
	var out domain.BookRenameResult
	out.SnapshotID = snapshotID

	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// 1. 替换章节正文（跳过 excluded 的 chapter_content:<id>）
		chapRows, err := tx.QueryContext(ctx, `SELECT id, content FROM chapters WHERE book_id = ?`, in.BookID)
		if err != nil {
			return mapSQLiteError(err)
		}
		type proseChange struct {
			id      int64
			oldText string
			newText string
			matches int
		}
		var proseChanges []proseChange
		for chapRows.Next() {
			var (
				id   int64
				text string
			)
			if err := chapRows.Scan(&id, &text); err != nil {
				_ = chapRows.Close()
				return mapSQLiteError(err)
			}
			hitID := fmt.Sprintf("chapter_content:%d", id)
			if excluded[hitID] {
				continue
			}
			cnt := strings.Count(text, oldName)
			if cnt > 0 {
				proseChanges = append(proseChanges, proseChange{
					id:      id,
					oldText: text,
					newText: strings.ReplaceAll(text, oldName, newName),
					matches: cnt,
				})
			}
		}
		if err := chapRows.Err(); err != nil {
			_ = chapRows.Close()
			return err
		}
		_ = chapRows.Close()

		for _, pc := range proseChanges {
			if pc.oldText != "" && pc.oldText != pc.newText {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO chapter_versions (chapter_id, source, model_name, content, word_count)
					VALUES (?, 'manual_backup', '', ?, ?)
				`, pc.id, pc.oldText, domain.CountWords(pc.oldText)); err != nil {
					return mapSQLiteError(err)
				}
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE chapters
				SET content = ?, word_count = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ?
			`, pc.newText, domain.CountWords(pc.newText), pc.id); err != nil {
				return mapSQLiteError(err)
			}
			out.UpdatedChapters++
			out.TotalReplaced += pc.matches
		}

		if in.IncludeOutlines {
			replaced, recs, err := executeOutlineRenameTx(ctx, tx, in.BookID, oldName, newName, excluded)
			if err != nil {
				return err
			}
			out.TotalReplaced += replaced
			out.UpdatedRecords += recs
		}

		if in.IncludeMemory {
			replaced, recs, err := executeMemoryRenameTx(ctx, tx, in.BookID, oldName, newName, excluded)
			if err != nil {
				return err
			}
			out.TotalReplaced += replaced
			out.UpdatedRecords += recs
		}
		return nil
	})
	if err != nil {
		return domain.BookRenameResult{}, err
	}
	return out, nil
}

func (s *Store) scanChapterContentHits(ctx context.Context, bookID int64, target string) ([]domain.TextImpactHit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, global_seq, title, content
		FROM chapters
		WHERE book_id = ?
		ORDER BY global_seq ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	hits := make([]domain.TextImpactHit, 0)
	for rows.Next() {
		var (
			id, seq        int64
			title, content string
		)
		if err := rows.Scan(&id, &seq, &title, &content); err != nil {
			return nil, mapSQLiteError(err)
		}
		if cnt := strings.Count(content, target); cnt > 0 {
			hits = append(hits, buildTextImpactHit(
				fmt.Sprintf("chapter_content:%d", id), "chapter_content", id,
				fmt.Sprintf("第%d章正文《%s》", seq, title), cnt, content, target,
			))
		}
	}
	return hits, rows.Err()
}

func replaceChapterProseTx(ctx context.Context, tx *sql.Tx, bookID int64, oldName, newName string, excluded map[string]bool) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, content FROM chapters WHERE book_id = ?`, bookID)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	type item struct {
		id      int64
		oldText string
		newText string
	}
	var list []item
	for rows.Next() {
		var (
			id   int64
			text string
		)
		if err := rows.Scan(&id, &text); err != nil {
			_ = rows.Close()
			return 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("chapter_content:%d", id)] {
			continue
		}
		if strings.Contains(text, oldName) {
			list = append(list, item{id: id, oldText: text, newText: strings.ReplaceAll(text, oldName, newName)})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	for _, it := range list {
		if it.oldText != "" && it.oldText != it.newText {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO chapter_versions (chapter_id, source, model_name, content, word_count)
				VALUES (?, 'manual_backup', '', ?, ?)
			`, it.id, it.oldText, domain.CountWords(it.oldText)); err != nil {
				return 0, mapSQLiteError(err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE chapters
			SET content = ?, word_count = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, it.newText, domain.CountWords(it.newText), it.id); err != nil {
			return 0, mapSQLiteError(err)
		}
	}
	return len(list), nil
}

func migrateEntityRelationsTx(ctx context.Context, tx *sql.Tx, bookID, srcID, dstID int64) (int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, source_entity_id, target_entity_id, relation_type, effective_chapter_seq
		FROM entity_relations
		WHERE book_id = ? AND (source_entity_id = ? OR target_entity_id = ?)
		ORDER BY id ASC
	`, bookID, srcID, srcID)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	type relRow struct {
		id     int64
		srcID  int64
		dstID  int64
		rType  string
		effSeq int
	}
	var impacted []relRow
	for rows.Next() {
		var rr relRow
		if err := rows.Scan(&rr.id, &rr.srcID, &rr.dstID, &rr.rType, &rr.effSeq); err != nil {
			_ = rows.Close()
			return 0, mapSQLiteError(err)
		}
		impacted = append(impacted, rr)
	}
	_ = rows.Close()

	migrated := 0
	for _, rr := range impacted {
		newSrc := rr.srcID
		if newSrc == srcID {
			newSrc = dstID
		}
		newDst := rr.dstID
		if newDst == srcID {
			newDst = dstID
		}
		if newSrc == newDst {
			if _, err := tx.ExecContext(ctx, `DELETE FROM entity_relations WHERE id = ?`, rr.id); err != nil {
				return 0, mapSQLiteError(err)
			}
			continue
		}
		var dupCount int
		_ = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM entity_relations
			WHERE book_id = ? AND id <> ? AND source_entity_id = ? AND target_entity_id = ? AND relation_type = ? AND effective_chapter_seq = ?
		`, bookID, rr.id, newSrc, newDst, rr.rType, rr.effSeq).Scan(&dupCount)
		if dupCount > 0 {
			if _, err := tx.ExecContext(ctx, `DELETE FROM entity_relations WHERE id = ?`, rr.id); err != nil {
				return 0, mapSQLiteError(err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE entity_relations
			SET source_entity_id = ?, target_entity_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, newSrc, newDst, rr.id); err != nil {
			return 0, mapSQLiteError(err)
		}
		migrated++
	}
	return migrated, nil
}

func replaceEntityIDInForeshadowingsTx(ctx context.Context, tx *sql.Tx, bookID, srcID, dstID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, involved_entity_ids_json FROM foreshadowings WHERE book_id = ?`, bookID)
	if err != nil {
		return mapSQLiteError(err)
	}
	type fsItem struct {
		id  int64
		ids []int64
	}
	var updates []fsItem
	for rows.Next() {
		var (
			id  int64
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return mapSQLiteError(err)
		}
		ids := unmarshalInt64Slice(raw)
		changed := false
		for i, v := range ids {
			if v == srcID {
				ids[i] = dstID
				changed = true
			}
		}
		if changed {
			updates = append(updates, fsItem{id: id, ids: ids})
		}
	}
	_ = rows.Close()

	for _, u := range updates {
		raw, err := marshalInt64Slice(u.ids)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE foreshadowings SET involved_entity_ids_json = ? WHERE id = ?`, raw, u.id); err != nil {
			return mapSQLiteError(err)
		}
	}
	return nil
}

func replaceEntityIDInBeatsTx(ctx context.Context, tx *sql.Tx, bookID, srcID, dstID int64) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE chapter_beats
		SET pov_entity_id = ?
		WHERE pov_entity_id = ?
		  AND chapter_id IN (SELECT id FROM chapters WHERE book_id = ?)
	`, dstID, srcID, bookID); err != nil {
		return mapSQLiteError(err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT b.id, b.entity_ids_json
		FROM chapter_beats b
		JOIN chapters c ON c.id = b.chapter_id
		WHERE c.book_id = ?
	`, bookID)
	if err != nil {
		return mapSQLiteError(err)
	}
	type beatItem struct {
		id  int64
		ids []int64
	}
	var updates []beatItem
	for rows.Next() {
		var (
			id  int64
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return mapSQLiteError(err)
		}
		ids := unmarshalInt64Slice(raw)
		changed := false
		for i, v := range ids {
			if v == srcID {
				ids[i] = dstID
				changed = true
			}
		}
		if changed {
			updates = append(updates, beatItem{id: id, ids: ids})
		}
	}
	_ = rows.Close()

	for _, u := range updates {
		raw, err := marshalInt64Slice(u.ids)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE chapter_beats SET entity_ids_json = ? WHERE id = ?`, raw, u.id); err != nil {
			return mapSQLiteError(err)
		}
	}
	return nil
}

func executeOutlineRenameTx(ctx context.Context, tx *sql.Tx, bookID int64, oldName, newName string, excluded map[string]bool) (int, int, error) {
	totalReplaced, updatedRecords := 0, 0

	// chapters meta
	cRows, err := tx.QueryContext(ctx, `SELECT id, title, outline_summary, causal_summary, hook_note FROM chapters WHERE book_id = ?`, bookID)
	if err != nil {
		return 0, 0, mapSQLiteError(err)
	}
	type cItem struct {
		id                        int64
		title, out, causal, hook string
		cnt                       int
	}
	var cUpdates []cItem
	for cRows.Next() {
		var it cItem
		if err := cRows.Scan(&it.id, &it.title, &it.out, &it.causal, &it.hook); err != nil {
			_ = cRows.Close()
			return 0, 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("chapter_meta:%d", it.id)] {
			continue
		}
		cnt := strings.Count(it.title, oldName) + strings.Count(it.out, oldName) + strings.Count(it.causal, oldName) + strings.Count(it.hook, oldName)
		if cnt > 0 {
			it.cnt = cnt
			cUpdates = append(cUpdates, it)
		}
	}
	_ = cRows.Close()
	for _, it := range cUpdates {
		if _, err := tx.ExecContext(ctx, `
			UPDATE chapters
			SET title = ?, outline_summary = ?, causal_summary = ?, hook_note = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, strings.ReplaceAll(it.title, oldName, newName),
			strings.ReplaceAll(it.out, oldName, newName),
			strings.ReplaceAll(it.causal, oldName, newName),
			strings.ReplaceAll(it.hook, oldName, newName), it.id); err != nil {
			return 0, 0, mapSQLiteError(err)
		}
		totalReplaced += it.cnt
		updatedRecords++
	}

	// beats
	bRows, err := tx.QueryContext(ctx, `
		SELECT b.id, b.summary, b.conflict_note
		FROM chapter_beats b JOIN chapters c ON c.id = b.chapter_id
		WHERE c.book_id = ?
	`, bookID)
	if err != nil {
		return 0, 0, mapSQLiteError(err)
	}
	type bItem struct {
		id        int64
		sum, conf string
		cnt       int
	}
	var bUpdates []bItem
	for bRows.Next() {
		var it bItem
		if err := bRows.Scan(&it.id, &it.sum, &it.conf); err != nil {
			_ = bRows.Close()
			return 0, 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("beat:%d", it.id)] {
			continue
		}
		cnt := strings.Count(it.sum, oldName) + strings.Count(it.conf, oldName)
		if cnt > 0 {
			it.cnt = cnt
			bUpdates = append(bUpdates, it)
		}
	}
	_ = bRows.Close()
	for _, it := range bUpdates {
		if _, err := tx.ExecContext(ctx, `UPDATE chapter_beats SET summary = ?, conflict_note = ? WHERE id = ?`,
			strings.ReplaceAll(it.sum, oldName, newName), strings.ReplaceAll(it.conf, oldName, newName), it.id); err != nil {
			return 0, 0, mapSQLiteError(err)
		}
		totalReplaced += it.cnt
		updatedRecords++
	}

	// volumes
	vRows, err := tx.QueryContext(ctx, `SELECT id, title, goal, climax_plan FROM volumes WHERE book_id = ?`, bookID)
	if err != nil {
		return 0, 0, mapSQLiteError(err)
	}
	type vItem struct {
		id                  int64
		title, goal, climax string
		cnt                 int
	}
	var vUpdates []vItem
	for vRows.Next() {
		var it vItem
		if err := vRows.Scan(&it.id, &it.title, &it.goal, &it.climax); err != nil {
			_ = vRows.Close()
			return 0, 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("volume:%d", it.id)] {
			continue
		}
		cnt := strings.Count(it.title, oldName) + strings.Count(it.goal, oldName) + strings.Count(it.climax, oldName)
		if cnt > 0 {
			it.cnt = cnt
			vUpdates = append(vUpdates, it)
		}
	}
	_ = vRows.Close()
	for _, it := range vUpdates {
		if _, err := tx.ExecContext(ctx, `UPDATE volumes SET title = ?, goal = ?, climax_plan = ? WHERE id = ?`,
			strings.ReplaceAll(it.title, oldName, newName),
			strings.ReplaceAll(it.goal, oldName, newName),
			strings.ReplaceAll(it.climax, oldName, newName), it.id); err != nil {
			return 0, 0, mapSQLiteError(err)
		}
		totalReplaced += it.cnt
		updatedRecords++
	}

	// arc_summaries
	aRows, err := tx.QueryContext(ctx, `SELECT id, summary FROM arc_summaries WHERE book_id = ?`, bookID)
	if err != nil {
		return 0, 0, mapSQLiteError(err)
	}
	type aItem struct {
		id  int64
		sum string
		cnt int
	}
	var aUpdates []aItem
	for aRows.Next() {
		var it aItem
		if err := aRows.Scan(&it.id, &it.sum); err != nil {
			_ = aRows.Close()
			return 0, 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("arc_summary:%d", it.id)] {
			continue
		}
		if cnt := strings.Count(it.sum, oldName); cnt > 0 {
			it.cnt = cnt
			aUpdates = append(aUpdates, it)
		}
	}
	_ = aRows.Close()
	for _, it := range aUpdates {
		if _, err := tx.ExecContext(ctx, `UPDATE arc_summaries SET summary = ? WHERE id = ?`,
			strings.ReplaceAll(it.sum, oldName, newName), it.id); err != nil {
			return 0, 0, mapSQLiteError(err)
		}
		totalReplaced += it.cnt
		updatedRecords++
	}

	return totalReplaced, updatedRecords, nil
}

func executeMemoryRenameTx(ctx context.Context, tx *sql.Tx, bookID int64, oldName, newName string, excluded map[string]bool) (int, int, error) {
	totalReplaced, updatedRecords := 0, 0

	// entities
	eRows, err := tx.QueryContext(ctx, `SELECT id, name, aliases_json, summary, attributes_json FROM entities WHERE book_id = ?`, bookID)
	if err != nil {
		return 0, 0, mapSQLiteError(err)
	}
	type eItem struct {
		id        int64
		name      string
		aliases   []string
		summary   string
		attrs     map[string]string
		cnt       int
	}
	var eUpdates []eItem
	for eRows.Next() {
		var (
			id                              int64
			name, aliasesJSON, summary, attrsJSON string
		)
		if err := eRows.Scan(&id, &name, &aliasesJSON, &summary, &attrsJSON); err != nil {
			_ = eRows.Close()
			return 0, 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("entity:%d", id)] {
			continue
		}
		aliases := unmarshalJSONSlice[string](aliasesJSON)
		attrs := unmarshalAttributes(attrsJSON)
		cnt := strings.Count(name, oldName) + strings.Count(summary, oldName)
		for _, a := range aliases {
			cnt += strings.Count(a, oldName)
		}
		for _, v := range attrs {
			cnt += strings.Count(v, oldName)
		}
		if cnt > 0 {
			eUpdates = append(eUpdates, eItem{id: id, name: name, aliases: aliases, summary: summary, attrs: attrs, cnt: cnt})
		}
	}
	_ = eRows.Close()
	for _, it := range eUpdates {
		newAliases := make([]string, len(it.aliases))
		for i, a := range it.aliases {
			newAliases[i] = strings.ReplaceAll(a, oldName, newName)
		}
		newAttrs := make(map[string]string, len(it.attrs))
		for k, v := range it.attrs {
			newAttrs[k] = strings.ReplaceAll(v, oldName, newName)
		}
		aliasesJSON, err := marshalStringSlice(newAliases)
		if err != nil {
			return 0, 0, err
		}
		attrsJSON, err := marshalAttributes(newAttrs)
		if err != nil {
			return 0, 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE entities
			SET name = ?, aliases_json = ?, summary = ?, attributes_json = ?,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, strings.ReplaceAll(it.name, oldName, newName), aliasesJSON, strings.ReplaceAll(it.summary, oldName, newName), attrsJSON, it.id); err != nil {
			return 0, 0, mapSQLiteError(err)
		}
		totalReplaced += it.cnt
		updatedRecords++
	}

	// entity_relations
	rRows, err := tx.QueryContext(ctx, `SELECT id, relation_type, description FROM entity_relations WHERE book_id = ?`, bookID)
	if err != nil {
		return 0, 0, mapSQLiteError(err)
	}
	type rItem struct {
		id      int64
		rt, desc string
		cnt     int
	}
	var rUpdates []rItem
	for rRows.Next() {
		var it rItem
		if err := rRows.Scan(&it.id, &it.rt, &it.desc); err != nil {
			_ = rRows.Close()
			return 0, 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("relation:%d", it.id)] {
			continue
		}
		cnt := strings.Count(it.rt, oldName) + strings.Count(it.desc, oldName)
		if cnt > 0 {
			it.cnt = cnt
			rUpdates = append(rUpdates, it)
		}
	}
	_ = rRows.Close()
	for _, it := range rUpdates {
		if _, err := tx.ExecContext(ctx, `UPDATE entity_relations SET relation_type = ?, description = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
			strings.ReplaceAll(it.rt, oldName, newName), strings.ReplaceAll(it.desc, oldName, newName), it.id); err != nil {
			return 0, 0, mapSQLiteError(err)
		}
		totalReplaced += it.cnt
		updatedRecords++
	}

	// foreshadowings
	fRows, err := tx.QueryContext(ctx, `SELECT id, title, description FROM foreshadowings WHERE book_id = ?`, bookID)
	if err != nil {
		return 0, 0, mapSQLiteError(err)
	}
	type fItem struct {
		id          int64
		title, desc string
		cnt         int
	}
	var fUpdates []fItem
	for fRows.Next() {
		var it fItem
		if err := fRows.Scan(&it.id, &it.title, &it.desc); err != nil {
			_ = fRows.Close()
			return 0, 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("foreshadowing:%d", it.id)] {
			continue
		}
		cnt := strings.Count(it.title, oldName) + strings.Count(it.desc, oldName)
		if cnt > 0 {
			it.cnt = cnt
			fUpdates = append(fUpdates, it)
		}
	}
	_ = fRows.Close()
	for _, it := range fUpdates {
		if _, err := tx.ExecContext(ctx, `UPDATE foreshadowings SET title = ?, description = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
			strings.ReplaceAll(it.title, oldName, newName), strings.ReplaceAll(it.desc, oldName, newName), it.id); err != nil {
			return 0, 0, mapSQLiteError(err)
		}
		totalReplaced += it.cnt
		updatedRecords++
	}

	// entity_state_events
	sRows, err := tx.QueryContext(ctx, `SELECT id, prev_value, new_value, reason FROM entity_state_events WHERE book_id = ?`, bookID)
	if err != nil {
		return 0, 0, mapSQLiteError(err)
	}
	type sItem struct {
		id               int64
		prevV, newV, rsn string
		cnt              int
	}
	var sUpdates []sItem
	for sRows.Next() {
		var it sItem
		if err := sRows.Scan(&it.id, &it.prevV, &it.newV, &it.rsn); err != nil {
			_ = sRows.Close()
			return 0, 0, mapSQLiteError(err)
		}
		if excluded[fmt.Sprintf("state_event:%d", it.id)] {
			continue
		}
		cnt := strings.Count(it.prevV, oldName) + strings.Count(it.newV, oldName) + strings.Count(it.rsn, oldName)
		if cnt > 0 {
			it.cnt = cnt
			sUpdates = append(sUpdates, it)
		}
	}
	_ = sRows.Close()
	for _, it := range sUpdates {
		if _, err := tx.ExecContext(ctx, `UPDATE entity_state_events SET prev_value = ?, new_value = ?, reason = ? WHERE id = ?`,
			strings.ReplaceAll(it.prevV, oldName, newName),
			strings.ReplaceAll(it.newV, oldName, newName),
			strings.ReplaceAll(it.rsn, oldName, newName), it.id); err != nil {
			return 0, 0, mapSQLiteError(err)
		}
		totalReplaced += it.cnt
		updatedRecords++
	}

	return totalReplaced, updatedRecords, nil
}

func buildTextImpactHit(id, scope string, targetID int64, label string, count int, text, query string) domain.TextImpactHit {
	hit := domain.TextImpactHit{
		ID:         id,
		Scope:      scope,
		TargetID:   targetID,
		Label:      label,
		MatchCount: count,
		Snippet:    extractMatchSnippet(text, query),
	}
	if hasCompoundSubstringRisk(text, query) {
		hit.RiskWarning = "检测到紧邻汉字/词组粘连（可能误伤长人名或复合词），请核对后再确认"
	}
	return hit
}

func extractMatchSnippet(text, query string) string {
	idx := strings.Index(text, query)
	if idx < 0 {
		return strings.TrimSpace(text)
	}
	prefix := []rune(text[:idx])
	if len(prefix) > snippetContextRunes {
		prefix = prefix[len(prefix)-snippetContextRunes:]
	}
	suffix := []rune(text[idx+len(query):])
	if len(suffix) > snippetContextRunes {
		suffix = suffix[:snippetContextRunes]
	}
	snippet := strings.ReplaceAll(string(prefix)+query+string(suffix), "\n", " ")
	return strings.TrimSpace(snippet)
}

// hasCompoundSubstringRisk 检查目标词是否在文本任意命中位置紧邻汉字或英文字母数字（例如「林风」出现在「林风云」中）。
func hasCompoundSubstringRisk(text, query string) bool {
	if text == "" || query == "" {
		return false
	}
	from := 0
	for {
		idx := strings.Index(text[from:], query)
		if idx < 0 {
			return false
		}
		start := from + idx
		end := start + len(query)
		if start > 0 {
			prevRune := lastRune(text[:start])
			if isWordAdjacentRune(prevRune) {
				return true
			}
		}
		if end < len(text) {
			nextRune := firstRune(text[end:])
			if isWordAdjacentRune(nextRune) {
				return true
			}
		}
		from = end
	}
}

func isWordAdjacentRune(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	var last rune
	for _, r := range s {
		last = r
	}
	return last
}

func firstNonEmptyWithMatch(query string, fields ...string) string {
	for _, f := range fields {
		if strings.Contains(f, query) {
			return f
		}
	}
	for _, f := range fields {
		if strings.TrimSpace(f) != "" {
			return f
		}
	}
	return ""
}

func countInJSONMapOrSlice(query string, aliasesJSON, attrsJSON string) int {
	cnt := 0
	for _, a := range unmarshalJSONSlice[string](aliasesJSON) {
		cnt += strings.Count(a, query)
	}
	for _, v := range unmarshalAttributes(attrsJSON) {
		cnt += strings.Count(v, query)
	}
	return cnt
}

func toStringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

func sanitizePathToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}
