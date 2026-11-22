package store

import (
	"context"
	"strings"

	"nib/internal/domain"
)

const stateEventColumns = `
	id, book_id, entity_id, chapter_id, chapter_seq, attr_key,
	prev_value, new_value, change_severity, reason, status, created_at
`

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
	return scanAll(rows, scanStateEvent)
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
	return scanAll(rows, scanStateEvent)
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
