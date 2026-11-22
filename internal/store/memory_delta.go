package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
)

const chapterDeltaColumns = `
	id, chapter_id, causal_summary, new_entities_json, state_patch_json,
	foreshadow_json, high_risk_flags, review_status, created_at, reviewed_at
`

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
			if p.EntityID > 0 && !entityIndex.hasID(p.EntityID) {
				p.EntityID = 0
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

		newEntitiesJSON, err := marshalJSONSlice(newEntities)
		if err != nil {
			return fmt.Errorf("marshal newEntities: %w", err)
		}
		patchesJSON, err := marshalJSONSlice(patches)
		if err != nil {
			return fmt.Errorf("marshal statePatches: %w", err)
		}
		foreshadowsJSON, err := marshalJSONSlice(foreshadows)
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
		`, ch.ID, strings.TrimSpace(delta.CausalSummary), newEntitiesJSON, patchesJSON, foreshadowsJSON, highRiskFlags); err != nil {
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
		idx.register(id, name, unmarshalJSONSlice[string](aliasesJSON))
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
	d.NewEntities = unmarshalJSONSlice[domain.DeltaNewEntity](newEntitiesJSON)
	d.StatePatches = unmarshalJSONSlice[domain.DeltaStatePatch](patchesJSON)
	d.Foreshadows = unmarshalJSONSlice[domain.DeltaForeshadowUpdate](foreshadowJSON)
	return d, nil
}
