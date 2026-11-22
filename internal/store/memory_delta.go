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

const chapterDeltaColumns = `
	id, chapter_id, causal_summary, story_time_label, story_day,
	new_entities_json, state_patch_json, foreshadow_json,
	relation_changes_json, deadline_changes_json,
	high_risk_flags, review_status, created_at, reviewed_at
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

		relChanges := make([]domain.DeltaRelationChange, len(delta.RelationChanges))
		for i, rc := range delta.RelationChanges {
			if strings.TrimSpace(rc.ID) == "" {
				rc.ID = fmt.Sprintf("rel-%d", i+1)
			}
			rc.Action = strings.ToLower(strings.TrimSpace(rc.Action))
			if rc.Action == "" {
				rc.Action = "upsert"
			}
			if rc.SourceEntityID <= 0 {
				if sid, ok := entityIndex.lookup(rc.SourceName); ok {
					rc.SourceEntityID = sid
				}
			}
			if rc.TargetEntityID <= 0 {
				if tid, ok := entityIndex.lookup(rc.TargetName); ok {
					rc.TargetEntityID = tid
				}
			}
			relChanges[i] = rc
		}

		dlChanges := make([]domain.DeltaDeadlineChange, len(delta.DeadlineChanges))
		for i, dc := range delta.DeadlineChanges {
			if strings.TrimSpace(dc.ID) == "" {
				dc.ID = fmt.Sprintf("dl-%d", i+1)
			}
			dc.Action = strings.ToLower(strings.TrimSpace(dc.Action))
			if dc.Action == "" {
				dc.Action = "create"
			}
			dlChanges[i] = dc
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
		relChangesJSON, err := marshalJSONSlice(relChanges)
		if err != nil {
			return fmt.Errorf("marshal relationChanges: %w", err)
		}
		dlChangesJSON, err := marshalJSONSlice(dlChanges)
		if err != nil {
			return fmt.Errorf("marshal deadlineChanges: %w", err)
		}

		// 先将本章此前残留的 pending 状态事件置为 reverted，避免多次重抽堆叠脏数据。
		if _, err := tx.ExecContext(ctx, `
			UPDATE entity_state_events
			SET status = 'reverted'
			WHERE chapter_id = ? AND status = 'pending'
		`, ch.ID); err != nil {
			return mapSQLiteError(err)
		}

		var deltaStoryDayArg any
		if delta.StoryDay != nil {
			deltaStoryDayArg = *delta.StoryDay
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chapter_deltas (
				chapter_id, causal_summary, story_time_label, story_day,
				new_entities_json, state_patch_json, foreshadow_json,
				relation_changes_json, deadline_changes_json,
				high_risk_flags, review_status, reviewed_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', '')
			ON CONFLICT(chapter_id) DO UPDATE SET
				causal_summary        = excluded.causal_summary,
				story_time_label      = excluded.story_time_label,
				story_day             = excluded.story_day,
				new_entities_json     = excluded.new_entities_json,
				state_patch_json      = excluded.state_patch_json,
				foreshadow_json       = excluded.foreshadow_json,
				relation_changes_json = excluded.relation_changes_json,
				deadline_changes_json = excluded.deadline_changes_json,
				high_risk_flags       = excluded.high_risk_flags,
				review_status         = 'pending',
				created_at            = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
				reviewed_at           = ''
		`, ch.ID, strings.TrimSpace(delta.CausalSummary), strings.TrimSpace(delta.StoryTimeLabel), deltaStoryDayArg,
			newEntitiesJSON, patchesJSON, foreshadowsJSON, relChangesJSON, dlChangesJSON, highRiskFlags); err != nil {
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

		// 1.5 故事内时间坐标
		if in.ApplyStoryTimeline {
			timeLabel := strings.TrimSpace(in.StoryTimeLabelOverride)
			if timeLabel == "" {
				timeLabel = strings.TrimSpace(delta.StoryTimeLabel)
			}
			storyDay := in.StoryDayOverride
			if storyDay == nil {
				storyDay = delta.StoryDay
			}
			if timeLabel != "" || storyDay != nil {
				var dayArg any
				if storyDay != nil {
					dayArg = *storyDay
				}
				if _, err := tx.ExecContext(ctx, `
					UPDATE chapters
					SET story_time_label = ?,
					    story_day = ?,
					    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
					WHERE id = ?
				`, timeLabel, dayArg, ch.ID); err != nil {
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

		// 4.5 实体关系章节演变提案
		for _, rc := range delta.RelationChanges {
			dec, ok := decisions[domain.DeltaDecisionKindRelation][rc.ID]
			if !ok || !dec.Accept {
				continue
			}
			srcID := rc.SourceEntityID
			if srcID <= 0 || !entityIndex.hasID(srcID) {
				if matched, ok := entityIndex.lookup(rc.SourceName); ok {
					srcID = matched
				} else {
					srcID = 0
				}
			}
			dstID := rc.TargetEntityID
			if dstID <= 0 || !entityIndex.hasID(dstID) {
				if matched, ok := entityIndex.lookup(rc.TargetName); ok {
					dstID = matched
				} else {
					dstID = 0
				}
			}

			action := strings.ToLower(strings.TrimSpace(rc.Action))
			if action == "end" {
				if rc.RelationID > 0 {
					res, err := tx.ExecContext(ctx, `
						UPDATE entity_relations
						SET ended_chapter_seq = ?,
						    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
						WHERE id = ? AND book_id = ?
					`, ch.GlobalSeq, rc.RelationID, ch.BookID)
					if err != nil {
						return mapSQLiteError(err)
					}
					if n, _ := res.RowsAffected(); n > 0 {
						out.UpdatedRelations++
						anyCommitted = true
					}
					continue
				}
				if srcID > 0 && dstID > 0 {
					q := `
						UPDATE entity_relations
						SET ended_chapter_seq = ?,
						    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
						WHERE book_id = ? AND source_entity_id = ? AND target_entity_id = ?
						  AND (ended_chapter_seq IS NULL OR ended_chapter_seq > ?)
					`
					args := []any{ch.GlobalSeq, ch.BookID, srcID, dstID, ch.GlobalSeq}
					if rt := strings.TrimSpace(rc.RelationType); rt != "" {
						q += ` AND relation_type = ?`
						args = append(args, rt)
					}
					res, err := tx.ExecContext(ctx, q, args...)
					if err != nil {
						return mapSQLiteError(err)
					}
					if n, _ := res.RowsAffected(); n > 0 {
						out.UpdatedRelations++
						anyCommitted = true
					}
				}
				continue
			}

			// 默认 upsert：创建在本章正式生效的关系
			if srcID <= 0 || dstID <= 0 || srcID == dstID {
				continue
			}
			relType := strings.TrimSpace(rc.RelationType)
			if relType == "" {
				continue
			}
			desc := strings.TrimSpace(rc.Description)
			if strings.TrimSpace(dec.OverrideNewValue) != "" {
				relType = strings.TrimSpace(dec.OverrideNewValue)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO entity_relations (
					book_id, source_entity_id, target_entity_id, relation_type,
					description, effective_chapter_seq, ended_chapter_seq, status
				)
				VALUES (?, ?, ?, ?, ?, ?, NULL, 'committed')
			`, ch.BookID, srcID, dstID, relType, desc, ch.GlobalSeq); err != nil {
				return mapSQLiteError(err)
			}
			out.UpdatedRelations++
			anyCommitted = true
		}

		// 4.6 故事内期限倒计时提案
		for _, dc := range delta.DeadlineChanges {
			dec, ok := decisions[domain.DeltaDecisionKindDeadline][dc.ID]
			if !ok || !dec.Accept {
				continue
			}
			action := strings.ToLower(strings.TrimSpace(dc.Action))
			switch action {
			case "reach", "expire", "cancel", "update":
				newStatus := domain.StoryDeadlineActive
				switch action {
				case "reach":
					newStatus = domain.StoryDeadlineReached
				case "expire":
					newStatus = domain.StoryDeadlineExpired
				case "cancel":
					newStatus = domain.StoryDeadlineCanceled
				}
				dlID := dc.DeadlineID
				if dlID <= 0 && strings.TrimSpace(dc.Title) != "" {
					_ = tx.QueryRowContext(ctx, `
						SELECT id FROM story_deadlines
						WHERE book_id = ? AND lower(trim(title)) = lower(trim(?))
						ORDER BY id DESC LIMIT 1
					`, ch.BookID, dc.Title).Scan(&dlID)
				}
				if dlID <= 0 {
					continue
				}
				var res sql.Result
				if action == "update" {
					res, err = tx.ExecContext(ctx, `
						UPDATE story_deadlines
						SET target_day = CASE WHEN ? > 0 THEN ? ELSE target_day END,
						    target_label = CASE WHEN ? <> '' THEN ? ELSE target_label END,
						    note = CASE WHEN ? <> '' THEN ? ELSE note END,
						    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
						WHERE id = ? AND book_id = ?
					`, dc.TargetDay, dc.TargetDay, strings.TrimSpace(dc.TargetLabel), strings.TrimSpace(dc.TargetLabel),
						strings.TrimSpace(dc.Note), strings.TrimSpace(dc.Note), dlID, ch.BookID)
				} else {
					res, err = tx.ExecContext(ctx, `
						UPDATE story_deadlines
						SET status = ?,
						    note = CASE WHEN ? <> '' THEN ? ELSE note END,
						    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
						WHERE id = ? AND book_id = ?
					`, string(newStatus), strings.TrimSpace(dc.Note), strings.TrimSpace(dc.Note), dlID, ch.BookID)
				}
				if err != nil {
					return mapSQLiteError(err)
				}
				if n, _ := res.RowsAffected(); n > 0 {
					out.UpdatedDeadlines++
					anyCommitted = true
				}
			default: // create
				title := strings.TrimSpace(dc.Title)
				if title == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO story_deadlines (
						book_id, title, start_chapter_seq, target_day, target_label, status, note
					)
					VALUES (?, ?, ?, ?, ?, 'active', ?)
				`, ch.BookID, title, ch.GlobalSeq, dc.TargetDay, strings.TrimSpace(dc.TargetLabel), strings.TrimSpace(dc.Note)); err != nil {
					return mapSQLiteError(err)
				}
				out.UpdatedDeadlines++
				anyCommitted = true
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
		storyDay        sql.NullInt64
		newEntitiesJSON string
		patchesJSON     string
		foreshadowJSON  string
		relChangesJSON  string
		dlChangesJSON   string
		reviewStatus    string
	)
	if err := sc.Scan(
		&d.ID, &d.ChapterID, &d.CausalSummary, &d.StoryTimeLabel, &storyDay,
		&newEntitiesJSON, &patchesJSON, &foreshadowJSON,
		&relChangesJSON, &dlChangesJSON,
		&d.HighRiskFlags, &reviewStatus, &d.CreatedAt, &d.ReviewedAt,
	); err != nil {
		return domain.ChapterDelta{}, mapSQLiteError(err)
	}
	if storyDay.Valid {
		v := int(storyDay.Int64)
		d.StoryDay = &v
	}
	d.ReviewStatus = domain.DeltaReviewStatus(reviewStatus)
	d.NewEntities = unmarshalJSONSlice[domain.DeltaNewEntity](newEntitiesJSON)
	d.StatePatches = unmarshalJSONSlice[domain.DeltaStatePatch](patchesJSON)
	d.Foreshadows = unmarshalJSONSlice[domain.DeltaForeshadowUpdate](foreshadowJSON)
	d.RelationChanges = unmarshalJSONSlice[domain.DeltaRelationChange](relChangesJSON)
	d.DeadlineChanges = unmarshalJSONSlice[domain.DeltaDeadlineChange](dlChangesJSON)
	return d, nil
}

// ListPendingDeltas 返回指定书籍全部尚未审定（review_status='pending'）的记忆变更单，按章序升序排列。
func (s *Store) ListPendingDeltas(ctx context.Context, bookID int64) ([]domain.ChapterDelta, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.chapter_id, d.causal_summary, d.story_time_label, d.story_day,
		       d.new_entities_json, d.state_patch_json, d.foreshadow_json,
		       d.relation_changes_json, d.deadline_changes_json,
		       d.high_risk_flags, d.review_status, d.created_at, d.reviewed_at
		FROM chapter_deltas d
		JOIN chapters c ON c.id = d.chapter_id
		WHERE c.book_id = ? AND d.review_status = 'pending'
		ORDER BY c.global_seq ASC, d.id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	return scanAll(rows, scanChapterDelta)
}

// ListDirtyChapters 查询指定书籍全部标记为 upstream_dirty 的章节及关联修复信息，按章序升序返回。
func (s *Store) ListDirtyChapters(ctx context.Context, bookID int64) ([]domain.DirtyChapterItem, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.volume_id, c.global_seq, c.title, c.word_count,
		       c.content, c.updated_at,
		       EXISTS(
		           SELECT 1 FROM chapter_deltas pd
		           WHERE pd.chapter_id = c.id AND pd.review_status = 'pending'
		       ) AS has_pending_delta,
		       COALESCE((
		           SELECT ca.critic_json FROM chapter_audits ca
		           WHERE ca.chapter_id = c.id
		       ), '[]') AS critic_json
		FROM chapters c
		WHERE c.book_id = ? AND c.status = 'upstream_dirty'
		ORDER BY c.global_seq ASC, c.id ASC
	`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.DirtyChapterItem, 0)
	for rows.Next() {
		var (
			item       domain.DirtyChapterItem
			content    string
			hasPending int
			criticJSON string
		)
		if err := rows.Scan(
			&item.ChapterID, &item.VolumeID, &item.GlobalSeq, &item.Title,
			&item.WordCount, &content, &item.UpdatedAt, &hasPending, &criticJSON,
		); err != nil {
			return nil, mapSQLiteError(err)
		}
		item.HasContent = strings.TrimSpace(content) != ""
		item.HasPendingDelta = hasPending != 0
		var rawFindings []json.RawMessage
		if err := json.Unmarshal([]byte(criticJSON), &rawFindings); err == nil {
			item.LastAuditFindings = len(rawFindings)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 为每条脏标章附带早于它的最近一条已入库变更单来源，帮助作者定位上游变动原因。
	for i := range out {
		var (
			srcSeq        int
			causalSummary string
		)
		err := s.db.QueryRowContext(ctx, `
			SELECT uc.global_seq, ud.causal_summary
			FROM chapter_deltas ud
			JOIN chapters uc ON uc.id = ud.chapter_id
			WHERE uc.book_id = ? AND uc.global_seq < ? AND ud.review_status = 'applied'
			ORDER BY ud.reviewed_at DESC, uc.global_seq DESC
			LIMIT 1
		`, bookID, out[i].GlobalSeq).Scan(&srcSeq, &causalSummary)
		if err == nil {
			out[i].UpstreamSourceSeq = srcSeq
			summaryTrimmed := strings.TrimSpace(causalSummary)
			if summaryTrimmed != "" {
				out[i].UpstreamSummary = fmt.Sprintf("第 %d 章记忆已变更：%s", srcSeq, summaryTrimmed)
			} else {
				out[i].UpstreamSummary = fmt.Sprintf("第 %d 章已提交记忆变更单", srcSeq)
			}
		} else {
			out[i].UpstreamSummary = "上游章节记忆或关键设定发生更新，建议复核本章衔接"
		}
	}
	return out, nil
}

// ClearDirtyChapters 在单事务内清除指定章节（chapterIDs 为空则清除全书所有 upstream_dirty 章）的脏标：
// 有正文恢复为 reviewing，无正文但有大纲或节拍恢复为 outlined，否则恢复为 planned。
func (s *Store) ClearDirtyChapters(ctx context.Context, bookID int64, chapterIDs []int64) (domain.ClearDirtyChaptersResult, error) {
	if bookID <= 0 {
		return domain.ClearDirtyChaptersResult{}, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}

	cleanIDs := uniquePositiveIDs(chapterIDs)
	res := domain.ClearDirtyChaptersResult{ClearedChapterIDs: make([]int64, 0)}

	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		conds := []string{"book_id = ?", "status = 'upstream_dirty'"}
		args := []any{bookID}
		if len(cleanIDs) > 0 {
			placeholders := make([]string, len(cleanIDs))
			for i, id := range cleanIDs {
				placeholders[i] = "?"
				args = append(args, id)
			}
			conds = append(conds, "id IN ("+strings.Join(placeholders, ",")+")")
		}

		rows, err := tx.QueryContext(ctx, `
			SELECT id, content, outline_summary,
			       EXISTS(SELECT 1 FROM chapter_beats WHERE chapter_id = chapters.id)
			FROM chapters
			WHERE `+strings.Join(conds, " AND ")+`
			ORDER BY global_seq ASC, id ASC
		`, args...)
		if err != nil {
			return mapSQLiteError(err)
		}
		type targetRow struct {
			id         int64
			nextStatus domain.ChapterStatus
		}
		targets := make([]targetRow, 0)
		for rows.Next() {
			var (
				id       int64
				content  string
				outline  string
				hasBeats int
			)
			if err := rows.Scan(&id, &content, &outline, &hasBeats); err != nil {
				_ = rows.Close()
				return mapSQLiteError(err)
			}
			nextStatus := domain.ChapterStatusPlanned
			if strings.TrimSpace(content) != "" {
				nextStatus = domain.ChapterStatusReviewing
			} else if strings.TrimSpace(outline) != "" || hasBeats != 0 {
				nextStatus = domain.ChapterStatusOutlined
			}
			targets = append(targets, targetRow{id: id, nextStatus: nextStatus})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()

		for _, t := range targets {
			if _, err := tx.ExecContext(ctx, `
				UPDATE chapters
				SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ?
			`, string(t.nextStatus), t.id); err != nil {
				return mapSQLiteError(err)
			}
			res.ClearedChapterIDs = append(res.ClearedChapterIDs, t.id)
		}
		res.ClearedCount = len(res.ClearedChapterIDs)
		return nil
	})
	if err != nil {
		return domain.ClearDirtyChaptersResult{}, err
	}
	return res, nil
}
