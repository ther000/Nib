package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"nib/internal/domain"
)

const (
	stateEventColumns = `
		id, book_id, entity_id, chapter_id, chapter_seq, attr_key,
		prev_value, new_value, change_severity, reason, status, created_at
	`
	foreshadowingColumns = `
		id, book_id, title, description, planted_chapter_seq, last_hint_seq,
		target_resolve_seq, resolved_chapter_seq, status, involved_entity_ids_json,
		created_at, updated_at
	`
	arcSummaryColumns = `
		id, book_id, level, volume_id, start_seq, end_seq, summary, created_at
	`
	chapterDeltaColumns = `
		id, chapter_id, causal_summary, new_entities_json, state_patch_json,
		foreshadow_json, high_risk_flags, review_status, created_at, reviewed_at
	`
)

// SnapshotAt 按时间轴折算截至 chapterSeq（含）时的全书实体状态快照。
// 严格禁止读取 chapter_seq > chapterSeq 的未来章节事件；仅当 includePending=true 时才合并 pending 事件。
func (s *Store) SnapshotAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntitySnapshot, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if chapterSeq < 0 {
		return nil, &domain.ValidationError{Field: "chapterSeq", Message: "章节序号不能为负数"}
	}

	entities, err := s.ListEntities(ctx, bookID, domain.EntityFilter{})
	if err != nil {
		return nil, err
	}
	if len(entities) == 0 {
		return []domain.EntitySnapshot{}, nil
	}

	snaps := make([]domain.EntitySnapshot, len(entities))
	idxByID := make(map[int64]int, len(entities))
	for i, ent := range entities {
		eff := make(map[string]string, len(ent.Attributes))
		for k, v := range ent.Attributes {
			eff[k] = v
		}
		snaps[i] = domain.EntitySnapshot{
			Entity:              ent,
			EffectiveAttributes: eff,
		}
		idxByID[ent.ID] = i
	}

	if chapterSeq == 0 {
		return snaps, nil
	}

	statusClause := `status = 'committed'`
	if includePending {
		statusClause = `status IN ('committed', 'pending')`
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+stateEventColumns+`
		FROM entity_state_events
		WHERE book_id = ? AND chapter_seq <= ? AND `+statusClause+`
		ORDER BY chapter_seq ASC, id ASC
	`, bookID, chapterSeq)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	for rows.Next() {
		ev, err := scanStateEvent(rows)
		if err != nil {
			return nil, err
		}
		idx, ok := idxByID[ev.EntityID]
		if !ok {
			continue
		}
		snaps[idx].EffectiveAttributes[ev.AttrKey] = ev.NewValue
		switch ev.Status {
		case domain.StateEventCommitted:
			snaps[idx].AppliedEventCount++
		case domain.StateEventPending:
			snaps[idx].PendingEventCount++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return snaps, nil
}

// ListStateEvents 查询实体状态变更事件流水；entityID=0 表示全书，upToSeq=0 表示不限截止章号。
func (s *Store) ListStateEvents(ctx context.Context, bookID int64, entityID int64, upToSeq int, includePending bool) ([]domain.EntityStateEvent, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if entityID < 0 {
		return nil, &domain.ValidationError{Field: "entityId", Message: "实体 ID 不能为负数"}
	}
	if upToSeq < 0 {
		return nil, &domain.ValidationError{Field: "upToSeq", Message: "截止章节号不能为负数"}
	}

	conds := []string{"book_id = ?"}
	args := []any{bookID}
	if entityID > 0 {
		conds = append(conds, "entity_id = ?")
		args = append(args, entityID)
	}
	if upToSeq > 0 {
		conds = append(conds, "chapter_seq <= ?")
		args = append(args, upToSeq)
	}
	if includePending {
		conds = append(conds, "status IN ('committed', 'pending')")
	} else {
		conds = append(conds, "status = 'committed'")
	}

	query := `SELECT ` + stateEventColumns + ` FROM entity_state_events WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY chapter_seq ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.EntityStateEvent, 0)
	for rows.Next() {
		ev, err := scanStateEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

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
	return scanForeshadowingRows(rows)
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
	return scanForeshadowingRows(rows)
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

	out := make([]domain.ArcSummary, 0)
	for rows.Next() {
		a, err := scanArcSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
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

// SavePendingDelta 在单个事务内保存完章抽取出的待审核记忆变更单（review_status='pending'）：
// 1. 回退该章此前尚未审核的 pending 状态事件；
// 2. 写入/覆写 chapter_deltas 暂存变更单；
// 3. 对匹配到既有实体的 state_patches 写入 status='pending' 的 entity_state_events，
//    仅允许下游微批通过 includePending=true 读取，绝不静默提交为 committed。
func (s *Store) SavePendingDelta(ctx context.Context, delta domain.ChapterDelta) (domain.ChapterDelta, error) {
	if delta.ChapterID <= 0 {
		return domain.ChapterDelta{}, &domain.ValidationError{Field: "chapterId", Message: "章节 ID 必须大于 0"}
	}

	var result domain.ChapterDelta
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		ch, err := getChapterRow(ctx, tx, delta.ChapterID)
		if err != nil {
			return err
		}

		entityIndex, err := buildBookEntityLookup(ctx, tx, ch.BookID)
		if err != nil {
			return err
		}

		newEntities := make([]domain.DeltaNewEntity, len(delta.NewEntities))
		for i, ne := range delta.NewEntities {
			if strings.TrimSpace(ne.ID) == "" {
				ne.ID = fmt.Sprintf("ent-%d", i+1)
			}
			newEntities[i] = ne
		}

		patches := make([]domain.DeltaStatePatch, len(delta.StatePatches))
		highRiskCount := 0
		for i, p := range delta.StatePatches {
			if strings.TrimSpace(p.ID) == "" {
				p.ID = fmt.Sprintf("patch-%d", i+1)
			}
			if p.Severity == "" {
				p.Severity = domain.ChangeSeverityNormal
			}
			if !p.Severity.Valid() {
				return &domain.ValidationError{Field: "statePatches.severity", Message: "无效的变更风险等级"}
			}
			if p.Severity == domain.ChangeSeverityHighRisk {
				highRiskCount++
			}

			// 若 EntityID 已传入则验证归属本书，否则尝试按名称/别名匹配既有实体。
			if p.EntityID > 0 {
				if !entityIndex.hasID(p.EntityID) {
					p.EntityID = 0
				}
			}
			if p.EntityID <= 0 {
				if matchedID, ok := entityIndex.lookup(p.EntityName); ok {
					p.EntityID = matchedID
				}
			}
			patches[i] = p
		}

		foreshadows := make([]domain.DeltaForeshadowUpdate, len(delta.Foreshadows))
		for i, fs := range delta.Foreshadows {
			if strings.TrimSpace(fs.ID) == "" {
				fs.ID = fmt.Sprintf("fs-%d", i+1)
			}
			if fs.Action != "" && !fs.Action.Valid() {
				return &domain.ValidationError{Field: "foreshadows.action", Message: "无效的伏笔变更动作"}
			}
			foreshadows[i] = fs
		}

		highRiskFlags := delta.HighRiskFlags
		if highRiskCount > highRiskFlags {
			highRiskFlags = highRiskCount
		}

		newEntitiesJSON, err := json.Marshal(newEntities)
		if err != nil {
			return fmt.Errorf("marshal newEntities: %w", err)
		}
		patchesJSON, err := json.Marshal(patches)
		if err != nil {
			return fmt.Errorf("marshal statePatches: %w", err)
		}
		foreshadowsJSON, err := json.Marshal(foreshadows)
		if err != nil {
			return fmt.Errorf("marshal foreshadows: %w", err)
		}

		// 先将本章此前残留的 pending 状态事件置为 reverted，避免多次重抽堆叠脏数据。
		if _, err := tx.ExecContext(ctx, `
			UPDATE entity_state_events
			SET status = 'reverted'
			WHERE chapter_id = ? AND status = 'pending'
		`, ch.ID); err != nil {
			return mapSQLiteError(err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chapter_deltas (
				chapter_id, causal_summary, new_entities_json, state_patch_json,
				foreshadow_json, high_risk_flags, review_status, reviewed_at
			)
			VALUES (?, ?, ?, ?, ?, ?, 'pending', '')
			ON CONFLICT(chapter_id) DO UPDATE SET
				causal_summary    = excluded.causal_summary,
				new_entities_json = excluded.new_entities_json,
				state_patch_json  = excluded.state_patch_json,
				foreshadow_json   = excluded.foreshadow_json,
				high_risk_flags   = excluded.high_risk_flags,
				review_status     = 'pending',
				created_at        = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
				reviewed_at       = ''
		`, ch.ID, strings.TrimSpace(delta.CausalSummary), string(newEntitiesJSON), string(patchesJSON), string(foreshadowsJSON), highRiskFlags); err != nil {
			return mapSQLiteError(err)
		}

		// 对已关联到实体的属性变更写入 pending 状态事件，供微批 includePending=true 使用。
		for _, p := range patches {
			attrKey := strings.TrimSpace(p.AttrKey)
			if p.EntityID <= 0 || attrKey == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO entity_state_events (
					book_id, entity_id, chapter_id, chapter_seq, attr_key,
					prev_value, new_value, change_severity, reason, status
				)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')
			`, ch.BookID, p.EntityID, ch.ID, ch.GlobalSeq, attrKey,
				p.PrevValue, p.NewValue, string(p.Severity), p.Reason); err != nil {
				return mapSQLiteError(err)
			}
		}

		row := tx.QueryRowContext(ctx, `SELECT `+chapterDeltaColumns+` FROM chapter_deltas WHERE chapter_id = ?`, ch.ID)
		result, err = scanChapterDelta(row)
		return err
	})
	if err != nil {
		return domain.ChapterDelta{}, err
	}
	return result, nil
}

// GetPendingDelta 返回指定章节当前待审核的记忆变更单；若不存在待审核单返回 (nil, nil)。
func (s *Store) GetPendingDelta(ctx context.Context, chapterID int64) (*domain.ChapterDelta, error) {
	if chapterID <= 0 {
		return nil, &domain.ValidationError{Field: "chapterId", Message: "章节 ID 必须大于 0"}
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT `+chapterDeltaColumns+`
		FROM chapter_deltas
		WHERE chapter_id = ? AND review_status = 'pending'
	`, chapterID)
	delta, err := scanChapterDelta(row)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &delta, nil
}

// ApplyDeltaDecisions 在单个 SQL 事务内提交作者对记忆变更单的审核结论：
// - 可选写入 chapters.causal_summary；
// - 创建采纳的新实体并立即可被同批次 state_patches 引用；
// - 采纳的 state_patches 转为 committed（支持 OverrideNewValue），驳回的转为 reverted；
// - 执行采纳的伏笔 plant/hint/resolve/abandon 变更；
// - 更新 chapter_deltas.review_status（applied 或 dismissed）与 reviewed_at；
// - 若发生任何提交变更，将后续未定稿章节标记为 upstream_dirty。
func (s *Store) ApplyDeltaDecisions(ctx context.Context, in domain.ApplyDeltaInput) (domain.ApplyDeltaResult, error) {
	if err := in.Validate(); err != nil {
		return domain.ApplyDeltaResult{}, err
	}

	decisions := make(map[string]map[string]domain.DeltaDecision, 3)
	for _, d := range in.Decisions {
		if decisions[d.Kind] == nil {
			decisions[d.Kind] = make(map[string]domain.DeltaDecision)
		}
		decisions[d.Kind][d.ItemID] = d
	}

	var out domain.ApplyDeltaResult
	out.MarkedDirtyChapterIDs = make([]int64, 0)

	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT `+chapterDeltaColumns+` FROM chapter_deltas WHERE id = ?`, in.DeltaID)
		delta, err := scanChapterDelta(row)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fmt.Errorf("chapter delta %d: %w", in.DeltaID, domain.ErrNotFound)
			}
			return err
		}
		if delta.ReviewStatus != domain.DeltaReviewPending {
			return fmt.Errorf("chapter delta %d already reviewed (%s): %w", delta.ID, delta.ReviewStatus, domain.ErrConflict)
		}

		ch, err := getChapterRow(ctx, tx, delta.ChapterID)
		if err != nil {
			return err
		}

		entityIndex, err := buildBookEntityLookup(ctx, tx, ch.BookID)
		if err != nil {
			return err
		}

		anyCommitted := false

		// 1. 因果摘要
		if in.ApplyCausalSummary {
			summary := strings.TrimSpace(in.CausalSummaryOverride)
			if summary == "" {
				summary = strings.TrimSpace(delta.CausalSummary)
			}
			if summary != "" {
				if _, err := tx.ExecContext(ctx, `
					UPDATE chapters
					SET causal_summary = ?,
					    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
					WHERE id = ?
				`, summary, ch.ID); err != nil {
					return mapSQLiteError(err)
				}
				anyCommitted = true
			}
		}

		// 2. 新实体提案（必须先于 state_patch 创建，使后续补丁能映射到新建实体 ID）
		for _, ne := range delta.NewEntities {
			dec, ok := decisions[domain.DeltaDecisionKindNewEntity][ne.ID]
			if !ok || !dec.Accept {
				continue
			}
			name := strings.TrimSpace(ne.Name)
			if name == "" {
				continue
			}
			if existingID, exists := entityIndex.lookup(name); exists {
				entityIndex.register(existingID, name, ne.Aliases)
				continue
			}
			kind := ne.Kind
			if !kind.Valid() {
				kind = domain.EntityKindCharacter
			}
			aliasesJSON, err := marshalStringSlice(ne.Aliases)
			if err != nil {
				return fmt.Errorf("marshal new entity aliases: %w", err)
			}
			attrsJSON, err := marshalAttributes(ne.Attributes)
			if err != nil {
				return fmt.Errorf("marshal new entity attributes: %w", err)
			}
			res, err := tx.ExecContext(ctx, `
				INSERT INTO entities (book_id, kind, name, aliases_json, is_invariant, summary, attributes_json)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, ch.BookID, string(kind), name, aliasesJSON, boolToInt(ne.IsInvariant), ne.Summary, attrsJSON)
			if err != nil {
				return mapSQLiteError(err)
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			entityIndex.register(newID, name, ne.Aliases)
			anyCommitted = true
		}

		// 3. 实体状态变更补丁（先读取本章所有 pending 事件，采纳则转 committed / 补插，驳回或未选则全部 reverted）
		pendingEvents, err := listChapterPendingEvents(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		usedPending := make(map[int64]bool, len(pendingEvents))

		for _, p := range delta.StatePatches {
			dec, ok := decisions[domain.DeltaDecisionKindStatePatch][p.ID]
			if !ok || !dec.Accept {
				continue
			}
			attrKey := strings.TrimSpace(p.AttrKey)
			if attrKey == "" {
				continue
			}

			entityID := p.EntityID
			if entityID > 0 && !entityIndex.hasID(entityID) {
				entityID = 0
			}
			if entityID <= 0 {
				if matchedID, found := entityIndex.lookup(p.EntityName); found {
					entityID = matchedID
				}
			}
			if entityID <= 0 {
				continue
			}

			finalValue := p.NewValue
			if strings.TrimSpace(dec.OverrideNewValue) != "" {
				finalValue = dec.OverrideNewValue
			}
			sev := p.Severity
			if !sev.Valid() {
				sev = domain.ChangeSeverityNormal
			}

			// 优先复用 SavePendingDelta 已落库的 pending 事件行，保持事件 ID 连续。
			var matchedEventID int64
			for _, ev := range pendingEvents {
				if usedPending[ev.ID] {
					continue
				}
				if ev.EntityID == entityID && ev.AttrKey == attrKey {
					matchedEventID = ev.ID
					usedPending[ev.ID] = true
					break
				}
			}

			if matchedEventID > 0 {
				if _, err := tx.ExecContext(ctx, `
					UPDATE entity_state_events
					SET new_value = ?,
					    change_severity = ?,
					    status = 'committed'
					WHERE id = ?
				`, finalValue, string(sev), matchedEventID); err != nil {
					return mapSQLiteError(err)
				}
			} else {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO entity_state_events (
						book_id, entity_id, chapter_id, chapter_seq, attr_key,
						prev_value, new_value, change_severity, reason, status
					)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'committed')
				`, ch.BookID, entityID, ch.ID, ch.GlobalSeq, attrKey,
					p.PrevValue, finalValue, string(sev), p.Reason); err != nil {
					return mapSQLiteError(err)
				}
			}

			out.CommittedEvents++
			anyCommitted = true
		}

		// 所有未被采纳的本章 pending 状态事件统一置为 reverted。
		if _, err := tx.ExecContext(ctx, `
			UPDATE entity_state_events
			SET status = 'reverted'
			WHERE chapter_id = ? AND status = 'pending'
		`, ch.ID); err != nil {
			return mapSQLiteError(err)
		}

		// 4. 伏笔变更提案
		for _, fs := range delta.Foreshadows {
			dec, ok := decisions[domain.DeltaDecisionKindForeshadow][fs.ID]
			if !ok || !dec.Accept {
				continue
			}

			targetID := fs.ForeshadowingID
			if targetID <= 0 && fs.Action != domain.DeltaForeshadowPlant {
				targetID, err = lookupForeshadowingByTitle(ctx, tx, ch.BookID, fs.Title)
				if err != nil {
					return err
				}
			}

			switch fs.Action {
			case domain.DeltaForeshadowPlant:
				title := strings.TrimSpace(fs.Title)
				if title == "" {
					continue
				}
				involvedJSON, err := marshalInt64Slice(fs.InvolvedEntityIDs)
				if err != nil {
					return fmt.Errorf("marshal foreshadow involvedEntityIds: %w", err)
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO foreshadowings (
						book_id, title, description, planted_chapter_seq, last_hint_seq,
						target_resolve_seq, resolved_chapter_seq, status, involved_entity_ids_json
					)
					VALUES (?, ?, ?, ?, 0, ?, 0, 'planted', ?)
				`, ch.BookID, title, fs.Description, ch.GlobalSeq, fs.TargetResolveSeq, involvedJSON); err != nil {
					return mapSQLiteError(err)
				}
				out.UpdatedForeshadowings++
				anyCommitted = true

			case domain.DeltaForeshadowHint:
				if targetID <= 0 {
					continue
				}
				res, err := tx.ExecContext(ctx, `
					UPDATE foreshadowings
					SET last_hint_seq = ?,
					    status = CASE WHEN status IN ('resolved', 'abandoned') THEN status ELSE 'hinted' END,
					    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
					WHERE id = ? AND book_id = ?
				`, ch.GlobalSeq, targetID, ch.BookID)
				if err != nil {
					return mapSQLiteError(err)
				}
				if n, _ := res.RowsAffected(); n > 0 {
					out.UpdatedForeshadowings++
					anyCommitted = true
				}

			case domain.DeltaForeshadowResolve:
				if targetID <= 0 {
					continue
				}
				res, err := tx.ExecContext(ctx, `
					UPDATE foreshadowings
					SET resolved_chapter_seq = ?,
					    status = 'resolved',
					    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
					WHERE id = ? AND book_id = ?
				`, ch.GlobalSeq, targetID, ch.BookID)
				if err != nil {
					return mapSQLiteError(err)
				}
				if n, _ := res.RowsAffected(); n > 0 {
					out.UpdatedForeshadowings++
					anyCommitted = true
				}

			case domain.DeltaForeshadowAbandon:
				if targetID <= 0 {
					continue
				}
				res, err := tx.ExecContext(ctx, `
					UPDATE foreshadowings
					SET status = 'abandoned',
					    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
					WHERE id = ? AND book_id = ?
				`, targetID, ch.BookID)
				if err != nil {
					return mapSQLiteError(err)
				}
				if n, _ := res.RowsAffected(); n > 0 {
					out.UpdatedForeshadowings++
					anyCommitted = true
				}
			}
		}

		// 5. 更新变更单审定状态
		reviewStatus := domain.DeltaReviewDismissed
		if anyCommitted {
			reviewStatus = domain.DeltaReviewApplied
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE chapter_deltas
			SET review_status = ?,
			    reviewed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE id = ?
		`, string(reviewStatus), delta.ID); err != nil {
			return mapSQLiteError(err)
		}

		// 6. 若发生记忆提交，将后续未定稿章节标记为 upstream_dirty
		if anyCommitted {
			dirtyRows, err := tx.QueryContext(ctx, `
				SELECT id
				FROM chapters
				WHERE book_id = ?
				  AND global_seq > ?
				  AND status IN ('outlined', 'drafting', 'partial', 'reviewing')
				ORDER BY global_seq ASC, id ASC
			`, ch.BookID, ch.GlobalSeq)
			if err != nil {
				return mapSQLiteError(err)
			}
			for dirtyRows.Next() {
				var id int64
				if err := dirtyRows.Scan(&id); err != nil {
					_ = dirtyRows.Close()
					return mapSQLiteError(err)
				}
				out.MarkedDirtyChapterIDs = append(out.MarkedDirtyChapterIDs, id)
			}
			if err := dirtyRows.Err(); err != nil {
				_ = dirtyRows.Close()
				return err
			}
			_ = dirtyRows.Close()

			if len(out.MarkedDirtyChapterIDs) > 0 {
				if _, err := tx.ExecContext(ctx, `
					UPDATE chapters
					SET status = 'upstream_dirty',
					    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
					WHERE book_id = ?
					  AND global_seq > ?
					  AND status IN ('outlined', 'drafting', 'partial', 'reviewing')
				`, ch.BookID, ch.GlobalSeq); err != nil {
					return mapSQLiteError(err)
				}
			}
		}

		updatedRow := tx.QueryRowContext(ctx, `SELECT `+chapterDeltaColumns+` FROM chapter_deltas WHERE id = ?`, delta.ID)
		out.Delta, err = scanChapterDelta(updatedRow)
		return err
	})
	if err != nil {
		return domain.ApplyDeltaResult{}, err
	}
	return out, nil
}

// SearchMemory 执行 FTS5 trigram + 双字短词 LIKE 混合检索（遵循 AGENTS.md §3.4）。
// - 检索词长度 >= 3 runes：使用 chapters_fts 与 entities_fts（trigram）精确检索；
// - 检索词长度 < 3 runes：自动回退 LIKE '%query%'，避免 trigram 对双字人名（如“林渊”）漏召回；
// - maxChapterSeq > 0 时严格限制 chapters.global_seq <= maxChapterSeq，杜绝未来章节剧透。
func (s *Store) SearchMemory(ctx context.Context, bookID int64, query string, maxChapterSeq int) ([]domain.MemorySearchHit, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if maxChapterSeq < 0 {
		return nil, &domain.ValidationError{Field: "maxChapterSeq", Message: "最大章节号不能为负数"}
	}

	q := strings.TrimSpace(query)
	if q == "" {
		return []domain.MemorySearchHit{}, nil
	}

	var hits []domain.MemorySearchHit
	var err error
	if utf8.RuneCountInString(q) >= 3 {
		hits, err = s.searchMemoryFTS5(ctx, bookID, q, maxChapterSeq)
	} else {
		hits, err = s.searchMemoryLike(ctx, bookID, q, maxChapterSeq)
	}
	if err != nil {
		return nil, err
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Source != hits[j].Source {
			return hits[i].Source < hits[j].Source
		}
		if hits[i].ChapterSeq != hits[j].ChapterSeq {
			return hits[i].ChapterSeq > hits[j].ChapterSeq
		}
		return hits[i].EntityID < hits[j].EntityID
	})
	if len(hits) > 30 {
		hits = hits[:30]
	}
	return hits, nil
}

func (s *Store) searchMemoryFTS5(ctx context.Context, bookID int64, q string, maxChapterSeq int) ([]domain.MemorySearchHit, error) {
	matchExpr := formatFTS5Phrase(q)
	hits := make([]domain.MemorySearchHit, 0)

	// 1. 章节 FTS5 检索
	chapRows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.global_seq, c.title,
		       snippet(chapters_fts, 1, '【', '】', '…', 24) AS snip,
		       c.content,
		       bm25(chapters_fts) AS rank
		FROM chapters_fts
		JOIN chapters c ON c.id = chapters_fts.rowid
		WHERE chapters_fts MATCH ?
		  AND c.book_id = ?
		  AND (? <= 0 OR c.global_seq <= ?)
		ORDER BY rank ASC, c.global_seq DESC
		LIMIT 20
	`, matchExpr, bookID, maxChapterSeq, maxChapterSeq)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer chapRows.Close()

	for chapRows.Next() {
		var (
			id, seq              int64
			title, snip, content string
			rank                 float64
		)
		if err := chapRows.Scan(&id, &seq, &title, &snip, &content, &rank); err != nil {
			return nil, mapSQLiteError(err)
		}
		if strings.TrimSpace(snip) == "" || !strings.Contains(snip, "【") {
			snip = buildContextSnippet(content, q, title)
		}
		hits = append(hits, domain.MemorySearchHit{
			Source:     domain.MemorySearchSourceChapter,
			ChapterID:  id,
			ChapterSeq: int(seq),
			Title:      fmt.Sprintf("第%d章 %s", seq, title),
			Snippet:    snip,
			Score:      bm25ToPositiveScore(rank),
		})
	}
	if err := chapRows.Err(); err != nil {
		return nil, err
	}

	// 2. 实体 FTS5 检索
	entRows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.name, e.aliases_json, e.summary,
		       bm25(entities_fts) AS rank
		FROM entities_fts
		JOIN entities e ON e.id = entities_fts.rowid
		WHERE entities_fts MATCH ?
		  AND e.book_id = ?
		ORDER BY rank ASC, e.is_invariant DESC, e.id ASC
		LIMIT 20
	`, matchExpr, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer entRows.Close()

	for entRows.Next() {
		var (
			id                        int64
			name, aliasesJSON, summary string
			rank                      float64
		)
		if err := entRows.Scan(&id, &name, &aliasesJSON, &summary, &rank); err != nil {
			return nil, mapSQLiteError(err)
		}
		combined := formatEntitySearchText(name, unmarshalStringSlice(aliasesJSON), summary, nil)
		hits = append(hits, domain.MemorySearchHit{
			Source:   domain.MemorySearchSourceEntity,
			EntityID: id,
			Title:    name,
			Snippet:  buildContextSnippet(combined, q, name),
			Score:    bm25ToPositiveScore(rank),
		})
	}
	if err := entRows.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}

func (s *Store) searchMemoryLike(ctx context.Context, bookID int64, q string, maxChapterSeq int) ([]domain.MemorySearchHit, error) {
	pattern := "%" + escapeLikePattern(q) + "%"
	hits := make([]domain.MemorySearchHit, 0)

	// 1. 章节短词 LIKE 回退检索
	chapRows, err := s.db.QueryContext(ctx, `
		SELECT id, global_seq, title, content
		FROM chapters
		WHERE book_id = ?
		  AND (? <= 0 OR global_seq <= ?)
		  AND (title LIKE ? ESCAPE '\' OR content LIKE ? ESCAPE '\')
		ORDER BY global_seq DESC, id DESC
		LIMIT 20
	`, bookID, maxChapterSeq, maxChapterSeq, pattern, pattern)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer chapRows.Close()

	for chapRows.Next() {
		var (
			id, seq        int64
			title, content string
		)
		if err := chapRows.Scan(&id, &seq, &title, &content); err != nil {
			return nil, mapSQLiteError(err)
		}
		score := 1.0
		if strings.Contains(title, q) {
			score += 2.0
		}
		if cnt := strings.Count(content, q); cnt > 0 {
			score += float64(min(cnt, 10)) * 0.25
		}
		hits = append(hits, domain.MemorySearchHit{
			Source:     domain.MemorySearchSourceChapter,
			ChapterID:  id,
			ChapterSeq: int(seq),
			Title:      fmt.Sprintf("第%d章 %s", seq, title),
			Snippet:    buildContextSnippet(content, q, title),
			Score:      score,
		})
	}
	if err := chapRows.Err(); err != nil {
		return nil, err
	}

	// 2. 实体短词 LIKE 回退检索
	entRows, err := s.db.QueryContext(ctx, `
		SELECT id, name, aliases_json, summary, attributes_json
		FROM entities
		WHERE book_id = ?
		  AND (
		      name LIKE ? ESCAPE '\'
		      OR aliases_json LIKE ? ESCAPE '\'
		      OR summary LIKE ? ESCAPE '\'
		      OR attributes_json LIKE ? ESCAPE '\'
		  )
		ORDER BY is_invariant DESC, id ASC
		LIMIT 20
	`, bookID, pattern, pattern, pattern, pattern)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer entRows.Close()

	for entRows.Next() {
		var (
			id                                   int64
			name, aliasesJSON, summary, attrsJSON string
		)
		if err := entRows.Scan(&id, &name, &aliasesJSON, &summary, &attrsJSON); err != nil {
			return nil, mapSQLiteError(err)
		}
		aliases := unmarshalStringSlice(aliasesJSON)
		attrs := unmarshalAttributes(attrsJSON)
		score := 1.0
		if strings.EqualFold(strings.TrimSpace(name), q) {
			score += 4.0
		} else if strings.Contains(name, q) {
			score += 2.5
		}
		combined := formatEntitySearchText(name, aliases, summary, attrs)
		hits = append(hits, domain.MemorySearchHit{
			Source:   domain.MemorySearchSourceEntity,
			EntityID: id,
			Title:    name,
			Snippet:  buildContextSnippet(combined, q, name),
			Score:    score,
		})
	}
	if err := entRows.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}

// --- Internal helpers for memory.go ---

type bookEntityLookup struct {
	ids     map[int64]bool
	byName  map[string]int64
	byAlias map[string]int64
}

func buildBookEntityLookup(ctx context.Context, q querier, bookID int64) (*bookEntityLookup, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name, aliases_json FROM entities WHERE book_id = ? ORDER BY id ASC`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	idx := &bookEntityLookup{
		ids:     make(map[int64]bool),
		byName:  make(map[string]int64),
		byAlias: make(map[string]int64),
	}
	for rows.Next() {
		var (
			id          int64
			name        string
			aliasesJSON string
		)
		if err := rows.Scan(&id, &name, &aliasesJSON); err != nil {
			return nil, mapSQLiteError(err)
		}
		idx.register(id, name, unmarshalStringSlice(aliasesJSON))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return idx, nil
}

func (l *bookEntityLookup) register(id int64, name string, aliases []string) {
	l.ids[id] = true
	if key := normalizeLookupKey(name); key != "" {
		if _, exists := l.byName[key]; !exists {
			l.byName[key] = id
		}
	}
	for _, a := range aliases {
		if key := normalizeLookupKey(a); key != "" {
			if _, exists := l.byAlias[key]; !exists {
				l.byAlias[key] = id
			}
		}
	}
}

func (l *bookEntityLookup) hasID(id int64) bool {
	return l.ids[id]
}

func (l *bookEntityLookup) lookup(name string) (int64, bool) {
	key := normalizeLookupKey(name)
	if key == "" {
		return 0, false
	}
	if id, ok := l.byName[key]; ok {
		return id, true
	}
	if id, ok := l.byAlias[key]; ok {
		return id, true
	}
	return 0, false
}

func normalizeLookupKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func listChapterPendingEvents(ctx context.Context, q querier, chapterID int64) ([]domain.EntityStateEvent, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT `+stateEventColumns+`
		FROM entity_state_events
		WHERE chapter_id = ? AND status = 'pending'
		ORDER BY id ASC
	`, chapterID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.EntityStateEvent, 0)
	for rows.Next() {
		ev, err := scanStateEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
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

func scanStateEvent(sc rowScanner) (domain.EntityStateEvent, error) {
	var (
		ev       domain.EntityStateEvent
		severity string
		status   string
	)
	if err := sc.Scan(
		&ev.ID, &ev.BookID, &ev.EntityID, &ev.ChapterID, &ev.ChapterSeq,
		&ev.AttrKey, &ev.PrevValue, &ev.NewValue, &severity, &ev.Reason,
		&status, &ev.CreatedAt,
	); err != nil {
		return domain.EntityStateEvent{}, mapSQLiteError(err)
	}
	ev.ChangeSeverity = domain.ChangeSeverity(severity)
	ev.Status = domain.StateEventStatus(status)
	return ev, nil
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
	f.InvolvedEntityIDs = unmarshalInt64Slice(involvedJSON)
	return f, nil
}

func scanForeshadowingRows(rows *sql.Rows) ([]domain.Foreshadowing, error) {
	out := make([]domain.Foreshadowing, 0)
	for rows.Next() {
		f, err := scanForeshadowing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
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

func scanChapterDelta(sc rowScanner) (domain.ChapterDelta, error) {
	var (
		d               domain.ChapterDelta
		newEntitiesJSON string
		patchesJSON     string
		foreshadowJSON  string
		reviewStatus    string
	)
	if err := sc.Scan(
		&d.ID, &d.ChapterID, &d.CausalSummary,
		&newEntitiesJSON, &patchesJSON, &foreshadowJSON,
		&d.HighRiskFlags, &reviewStatus, &d.CreatedAt, &d.ReviewedAt,
	); err != nil {
		return domain.ChapterDelta{}, mapSQLiteError(err)
	}
	d.ReviewStatus = domain.DeltaReviewStatus(reviewStatus)
	d.NewEntities = unmarshalDeltaNewEntities(newEntitiesJSON)
	d.StatePatches = unmarshalDeltaStatePatches(patchesJSON)
	d.Foreshadows = unmarshalDeltaForeshadows(foreshadowJSON)
	return d, nil
}

func unmarshalDeltaNewEntities(raw string) []domain.DeltaNewEntity {
	if strings.TrimSpace(raw) == "" {
		return []domain.DeltaNewEntity{}
	}
	var out []domain.DeltaNewEntity
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []domain.DeltaNewEntity{}
	}
	return out
}

func unmarshalDeltaStatePatches(raw string) []domain.DeltaStatePatch {
	if strings.TrimSpace(raw) == "" {
		return []domain.DeltaStatePatch{}
	}
	var out []domain.DeltaStatePatch
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []domain.DeltaStatePatch{}
	}
	return out
}

func unmarshalDeltaForeshadows(raw string) []domain.DeltaForeshadowUpdate {
	if strings.TrimSpace(raw) == "" {
		return []domain.DeltaForeshadowUpdate{}
	}
	var out []domain.DeltaForeshadowUpdate
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []domain.DeltaForeshadowUpdate{}
	}
	return out
}

func formatFTS5Phrase(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}

func bm25ToPositiveScore(rank float64) float64 {
	// SQLite FTS5 bm25() 越小（越负）表示相关性越高，统一转为正数越大越好。
	if rank < 0 {
		return 1.0 - rank
	}
	return 1.0 / (1.0 + rank)
}

func formatEntitySearchText(name string, aliases []string, summary string, attrs map[string]string) string {
	parts := make([]string, 0, 4)
	if len(aliases) > 0 {
		parts = append(parts, "别名："+strings.Join(aliases, "、"))
	}
	if s := strings.TrimSpace(summary); s != "" {
		parts = append(parts, s)
	}
	if len(attrs) > 0 {
		keys := make([]string, 0, len(attrs))
		for k := range attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, attrs[k]))
		}
		parts = append(parts, strings.Join(pairs, "；"))
	}
	if len(parts) == 0 {
		return name
	}
	return strings.Join(parts, " | ")
}

func buildContextSnippet(text, query, fallback string) string {
	clean := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", " "), "\n", " "))
	if clean == "" {
		return fallback
	}
	runes := []rune(clean)
	qRunes := []rune(query)

	matchIdx := -1
	if len(qRunes) > 0 {
		lowerClean := []rune(strings.ToLower(clean))
		lowerQ := []rune(strings.ToLower(query))
		for i := 0; i <= len(lowerClean)-len(lowerQ); i++ {
			matched := true
			for j := range lowerQ {
				if lowerClean[i+j] != lowerQ[j] {
					matched = false
					break
				}
			}
			if matched {
				matchIdx = i
				break
			}
		}
	}

	const radius = 36
	if matchIdx < 0 {
		if len(runes) <= radius*2 {
			return clean
		}
		return string(runes[:radius*2]) + "…"
	}

	start := max(0, matchIdx-radius)
	end := min(len(runes), matchIdx+len(qRunes)+radius)

	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	b.WriteString(string(runes[start:matchIdx]))
	b.WriteString("【")
	b.WriteString(string(runes[matchIdx : matchIdx+len(qRunes)]))
	b.WriteString("】")
	b.WriteString(string(runes[matchIdx+len(qRunes) : end]))
	if end < len(runes) {
		b.WriteString("…")
	}
	return b.String()
}
