package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"nib/internal/domain"
)

// trashPayload 是回收站条目的完整领域对象快照。
// 采用领域结构体而非裸表行，恢复时可复用同一套字段语义并重新分配主键，避免 ID 冲突。
type trashPayload struct {
	ItemType  domain.TrashItemType              `json:"itemType"`
	Book      *domain.Book                      `json:"book,omitempty"`
	Volumes   []domain.Volume                   `json:"volumes,omitempty"`
	Chapters  []domain.Chapter                  `json:"chapters,omitempty"`
	Beats     map[int64][]domain.Beat           `json:"beats,omitempty"`
	Versions  map[int64][]domain.ChapterVersion `json:"versions,omitempty"`
	Entities  []domain.Entity                   `json:"entities,omitempty"`
	Relations []domain.EntityRelation           `json:"relations,omitempty"`
}

// ListTrashItems 返回回收站条目（最近删除优先），并计算 30 天保留截止时间。
func (s *Store) ListTrashItems(ctx context.Context) ([]domain.TrashItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, item_type, book_id, title, summary, deleted_at
		FROM trash_items
		ORDER BY deleted_at DESC, id DESC
	`)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	out := make([]domain.TrashItem, 0)
	for rows.Next() {
		var item domain.TrashItem
		var itemType string
		if err := rows.Scan(&item.ID, &itemType, &item.BookID, &item.Title, &item.Summary, &item.DeletedAt); err != nil {
			return nil, mapSQLiteError(err)
		}
		item.ItemType = domain.TrashItemType(itemType)
		item.RetentionAt = domain.TrashRetentionAt(item.DeletedAt)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// RestoreTrashItem 将回收站条目恢复为新对象（自动分配新 ID 并重映射关联数据）。
func (s *Store) RestoreTrashItem(ctx context.Context, trashID int64) (domain.TrashRestoreResult, error) {
	if trashID <= 0 {
		return domain.TrashRestoreResult{}, &domain.ValidationError{Field: "id", Message: "回收站条目 ID 必须大于 0"}
	}

	var (
		title  string
		result domain.TrashRestoreResult
	)
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var (
			itemType string
			raw      string
		)
		if err := tx.QueryRowContext(ctx, `
			SELECT item_type, title, payload_json FROM trash_items WHERE id = ?
		`, trashID).Scan(&itemType, &title, &raw); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("trash item %d: %w", trashID, domain.ErrNotFound)
			}
			return mapSQLiteError(err)
		}
		var payload trashPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return fmt.Errorf("decode trash payload: %w", err)
		}
		var err error
		switch domain.TrashItemType(itemType) {
		case domain.TrashItemBook:
			result, err = restoreBookTx(ctx, tx, payload)
		case domain.TrashItemVolume:
			result, err = restoreVolumeTx(ctx, tx, payload)
		case domain.TrashItemChapter:
			result, err = restoreChapterTx(ctx, tx, payload)
		case domain.TrashItemEntity:
			result, err = restoreEntityTx(ctx, tx, payload)
		default:
			return &domain.ValidationError{Field: "itemType", Message: "不支持的回收站条目类型"}
		}
		if err != nil {
			return err
		}
		delRes, err := tx.ExecContext(ctx, `DELETE FROM trash_items WHERE id = ?`, trashID)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := delRes.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("trash item %d: %w", trashID, domain.ErrNotFound)
		}
		return nil
	})
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}
	if result.Title == "" {
		result.Title = title
	}
	return result, nil
}

// DeleteTrashItem 永久删除一条回收站记录。
func (s *Store) DeleteTrashItem(ctx context.Context, id int64) error {
	if id <= 0 {
		return &domain.ValidationError{Field: "id", Message: "回收站条目 ID 必须大于 0"}
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM trash_items WHERE id = ?`, id)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("trash item %d: %w", id, domain.ErrNotFound)
		}
		return nil
	})
}

// ClearTrash 清空回收站。
func (s *Store) ClearTrash(ctx context.Context) (int64, error) {
	var affected int64
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM trash_items`)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err = res.RowsAffected()
		return err
	})
	return affected, err
}

// CleanupExpiredTrash 清理超过 30 天的回收站条目。
func (s *Store) CleanupExpiredTrash(ctx context.Context) (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -domain.TrashRetentionDays).Format("2006-01-02T15:04:05.000Z")
	var affected int64
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM trash_items WHERE deleted_at < ?`, cutoff)
		if err != nil {
			return mapSQLiteError(err)
		}
		affected, err = res.RowsAffected()
		return err
	})
	return affected, err
}

// --- 快照 ---

func snapshotBookTrashTx(ctx context.Context, tx *sql.Tx, bookID int64) error {
	book, err := getBookRow(ctx, tx, bookID)
	if err != nil {
		return err
	}
	payload := trashPayload{ItemType: domain.TrashItemBook, Book: &book}

	volRows, err := tx.QueryContext(ctx, `
		SELECT id, book_id, seq, title, goal, climax_plan, created_at
		FROM volumes WHERE book_id = ? ORDER BY seq ASC, id ASC
	`, bookID)
	if err != nil {
		return mapSQLiteError(err)
	}
	for volRows.Next() {
		v, err := scanVolume(volRows)
		if err != nil {
			volRows.Close()
			return err
		}
		payload.Volumes = append(payload.Volumes, v)
	}
	if err := volRows.Err(); err != nil {
		volRows.Close()
		return err
	}
	volRows.Close()

	chapterRows, err := tx.QueryContext(ctx, `SELECT `+chapterColumns+` FROM chapters WHERE book_id = ? ORDER BY global_seq ASC, id ASC`, bookID)
	if err != nil {
		return mapSQLiteError(err)
	}
	chapters, err := scanChapterRows(chapterRows)
	chapterRows.Close()
	if err != nil {
		return err
	}
	payload.Chapters = chapters

	if err := fillTrashChapterDetailsTx(ctx, tx, &payload); err != nil {
		return err
	}

	entityRows, err := tx.QueryContext(ctx, `SELECT `+entityColumns+` FROM entities WHERE book_id = ? ORDER BY id ASC`, bookID)
	if err != nil {
		return mapSQLiteError(err)
	}
	entities, err := scanEntityRows(entityRows)
	entityRows.Close()
	if err != nil {
		return err
	}
	payload.Entities = entities

	relRows, err := tx.QueryContext(ctx, `
		SELECT id, book_id, source_entity_id, target_entity_id, relation_type, description, created_at, updated_at
		FROM entity_relations WHERE book_id = ? ORDER BY id ASC
	`, bookID)
	if err != nil {
		return mapSQLiteError(err)
	}
	for relRows.Next() {
		var rel domain.EntityRelation
		if err := relRows.Scan(&rel.ID, &rel.BookID, &rel.SourceEntityID, &rel.TargetEntityID, &rel.RelationType, &rel.Description, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
			relRows.Close()
			return mapSQLiteError(err)
		}
		payload.Relations = append(payload.Relations, rel)
	}
	if err := relRows.Err(); err != nil {
		relRows.Close()
		return err
	}
	relRows.Close()

	summary := domain.FormatTrashSummary(len(payload.Volumes), len(payload.Chapters), len(payload.Entities))
	return insertTrashItemTx(ctx, tx, domain.TrashItemBook, bookID, book.Title, summary, payload)
}

func snapshotVolumeTrashTx(ctx context.Context, tx *sql.Tx, volumeID int64) error {
	volume, err := getVolumeRow(ctx, tx, volumeID)
	if err != nil {
		return err
	}
	payload := trashPayload{ItemType: domain.TrashItemVolume, Volumes: []domain.Volume{volume}}

	chapterRows, err := tx.QueryContext(ctx, `SELECT `+chapterColumns+` FROM chapters WHERE volume_id = ? ORDER BY vol_seq ASC, id ASC`, volumeID)
	if err != nil {
		return mapSQLiteError(err)
	}
	chapters, err := scanChapterRows(chapterRows)
	chapterRows.Close()
	if err != nil {
		return err
	}
	payload.Chapters = chapters
	if err := fillTrashChapterDetailsTx(ctx, tx, &payload); err != nil {
		return err
	}

	summary := domain.FormatTrashSummary(1, len(payload.Chapters), 0)
	return insertTrashItemTx(ctx, tx, domain.TrashItemVolume, volume.BookID, volume.Title, summary, payload)
}

func snapshotChapterTrashTx(ctx context.Context, tx *sql.Tx, chapterID int64) error {
	chapter, err := getChapterRow(ctx, tx, chapterID)
	if err != nil {
		return err
	}
	payload := trashPayload{ItemType: domain.TrashItemChapter, Chapters: []domain.Chapter{chapter}}
	if err := fillTrashChapterDetailsTx(ctx, tx, &payload); err != nil {
		return err
	}
	summary := fmt.Sprintf("第 %d 章 · %d 字", chapter.GlobalSeq, chapter.WordCount)
	return insertTrashItemTx(ctx, tx, domain.TrashItemChapter, chapter.BookID, chapter.Title, summary, payload)
}

func snapshotEntityTrashTx(ctx context.Context, tx *sql.Tx, entityID int64) error {
	entity, err := getEntityRow(ctx, tx, entityID)
	if err != nil {
		return err
	}
	payload := trashPayload{ItemType: domain.TrashItemEntity, Entities: []domain.Entity{entity}}

	relRows, err := tx.QueryContext(ctx, `
		SELECT id, book_id, source_entity_id, target_entity_id, relation_type, description, created_at, updated_at
		FROM entity_relations
		WHERE source_entity_id = ? OR target_entity_id = ?
		ORDER BY id ASC
	`, entityID, entityID)
	if err != nil {
		return mapSQLiteError(err)
	}
	for relRows.Next() {
		var rel domain.EntityRelation
		if err := relRows.Scan(&rel.ID, &rel.BookID, &rel.SourceEntityID, &rel.TargetEntityID, &rel.RelationType, &rel.Description, &rel.CreatedAt, &rel.UpdatedAt); err != nil {
			relRows.Close()
			return mapSQLiteError(err)
		}
		payload.Relations = append(payload.Relations, rel)
	}
	if err := relRows.Err(); err != nil {
		relRows.Close()
		return err
	}
	relRows.Close()

	return insertTrashItemTx(ctx, tx, domain.TrashItemEntity, entity.BookID, entity.Name, string(entity.Kind), payload)
}

func fillTrashChapterDetailsTx(ctx context.Context, tx *sql.Tx, payload *trashPayload) error {
	payload.Beats = make(map[int64][]domain.Beat, len(payload.Chapters))
	payload.Versions = make(map[int64][]domain.ChapterVersion, len(payload.Chapters))
	for _, ch := range payload.Chapters {
		beats, err := listBeatsQuery(ctx, tx, ch.ID)
		if err != nil {
			return err
		}
		payload.Beats[ch.ID] = beats

		rows, err := tx.QueryContext(ctx, `
			SELECT id, chapter_id, source, model_name, content, word_count, created_at
			FROM chapter_versions WHERE chapter_id = ? ORDER BY id ASC
		`, ch.ID)
		if err != nil {
			return mapSQLiteError(err)
		}
		versions := make([]domain.ChapterVersion, 0)
		for rows.Next() {
			v, err := scanChapterVersion(rows)
			if err != nil {
				rows.Close()
				return err
			}
			versions = append(versions, v)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		payload.Versions[ch.ID] = versions
	}
	return nil
}

func insertTrashItemTx(ctx context.Context, tx *sql.Tx, itemType domain.TrashItemType, bookID int64, title, summary string, payload trashPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO trash_items (item_type, book_id, title, summary, payload_json)
		VALUES (?, ?, ?, ?, ?)
	`, string(itemType), bookID, title, summary, string(raw)); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

// --- 恢复 ---

func restoreBookTx(ctx context.Context, tx *sql.Tx, payload trashPayload) (domain.TrashRestoreResult, error) {
	if payload.Book == nil {
		return domain.TrashRestoreResult{}, errors.New("回收站数据缺少书籍信息")
	}
	book := *payload.Book
	res, err := tx.ExecContext(ctx, `
		INSERT INTO books (title, genre, premise, golden_finger, target_chapter_words, export_dir,
		                   pov_mode, pacing_style, writing_prompt, daily_word_goal)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, book.Title, string(book.Genre), book.Premise, book.GoldenFinger, book.TargetChapterWords, book.ExportDir,
		book.POVMode, book.PacingStyle, book.WritingPrompt, book.DailyWordGoal)
	if err != nil {
		return domain.TrashRestoreResult{}, mapSQLiteError(err)
	}
	newBookID, err := res.LastInsertId()
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}

	volumeMap := make(map[int64]int64, len(payload.Volumes))
	volumes := append([]domain.Volume(nil), payload.Volumes...)
	sort.Slice(volumes, func(i, j int) bool { return volumes[i].Seq < volumes[j].Seq })
	for i, volume := range volumes {
		volRes, err := tx.ExecContext(ctx, `
			INSERT INTO volumes (book_id, seq, title, goal, climax_plan) VALUES (?, ?, ?, ?, ?)
		`, newBookID, i+1, volume.Title, volume.Goal, volume.ClimaxPlan)
		if err != nil {
			return domain.TrashRestoreResult{}, mapSQLiteError(err)
		}
		newVolumeID, err := volRes.LastInsertId()
		if err != nil {
			return domain.TrashRestoreResult{}, err
		}
		volumeMap[volume.ID] = newVolumeID
	}

	entityMap, err := restoreEntitiesTx(ctx, tx, newBookID, payload.Entities)
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}
	chapterMap, err := restoreChaptersTx(ctx, tx, newBookID, volumeMap, payload.Chapters, payload.Beats, payload.Versions, entityMap)
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}
	if err := restoreRelationsTx(ctx, tx, newBookID, payload.Relations, entityMap); err != nil {
		return domain.TrashRestoreResult{}, err
	}
	_ = chapterMap

	return domain.TrashRestoreResult{
		ItemType: domain.TrashItemBook,
		ItemID:   newBookID,
		Title:    book.Title,
		Message:  fmt.Sprintf("已恢复《%s》，共 %d 卷 / %d 章", book.Title, len(volumeMap), len(payload.Chapters)),
	}, nil
}

func loadExistingEntityIdentityMapTx(ctx context.Context, tx *sql.Tx, bookID int64) (map[int64]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM entities WHERE book_id = ?`, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()
	out := make(map[int64]int64)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = id
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return out, nil
}

func restoreVolumeTx(ctx context.Context, tx *sql.Tx, payload trashPayload) (domain.TrashRestoreResult, error) {
	if len(payload.Volumes) == 0 {
		return domain.TrashRestoreResult{}, errors.New("回收站数据缺少分卷信息")
	}
	volume := payload.Volumes[0]
	var nextSeq int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM volumes WHERE book_id = ?`, volume.BookID).Scan(&nextSeq); err != nil {
		return domain.TrashRestoreResult{}, mapSQLiteError(err)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO volumes (book_id, seq, title, goal, climax_plan) VALUES (?, ?, ?, ?, ?)
	`, volume.BookID, nextSeq, volume.Title, volume.Goal, volume.ClimaxPlan)
	if err != nil {
		return domain.TrashRestoreResult{}, mapSQLiteError(err)
	}
	newVolumeID, err := res.LastInsertId()
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}
	volumeMap := map[int64]int64{volume.ID: newVolumeID}
	entityMap, err := loadExistingEntityIdentityMapTx(ctx, tx, volume.BookID)
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}
	if _, err := restoreChaptersTx(ctx, tx, volume.BookID, volumeMap, payload.Chapters, payload.Beats, payload.Versions, entityMap); err != nil {
		return domain.TrashRestoreResult{}, err
	}
	if err := resequenceBookChaptersTx(ctx, tx, volume.BookID); err != nil {
		return domain.TrashRestoreResult{}, err
	}
	return domain.TrashRestoreResult{
		ItemType: domain.TrashItemVolume,
		ItemID:   newVolumeID,
		Title:    volume.Title,
		Message:  fmt.Sprintf("已恢复分卷《%s》，共 %d 章", volume.Title, len(payload.Chapters)),
	}, nil
}

func restoreChapterTx(ctx context.Context, tx *sql.Tx, payload trashPayload) (domain.TrashRestoreResult, error) {
	if len(payload.Chapters) == 0 {
		return domain.TrashRestoreResult{}, errors.New("回收站数据缺少章节信息")
	}
	chapter := payload.Chapters[0]
	volumeID := chapter.VolumeID
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM volumes WHERE id = ? AND book_id = ?`, volumeID, chapter.BookID).Scan(&exists); err != nil {
		return domain.TrashRestoreResult{}, mapSQLiteError(err)
	}
	if exists == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT id FROM volumes WHERE book_id = ? ORDER BY seq ASC LIMIT 1`, chapter.BookID).Scan(&volumeID); err != nil {
			return domain.TrashRestoreResult{}, fmt.Errorf("原分卷已不存在，且书籍没有可用分卷: %w", domain.ErrNotFound)
		}
	}
	volumeMap := map[int64]int64{chapter.VolumeID: volumeID}
	entityMap, err := loadExistingEntityIdentityMapTx(ctx, tx, chapter.BookID)
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}
	chapterMap, err := restoreChaptersTx(ctx, tx, chapter.BookID, volumeMap, payload.Chapters, payload.Beats, payload.Versions, entityMap)
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}
	if err := resequenceBookChaptersTx(ctx, tx, chapter.BookID); err != nil {
		return domain.TrashRestoreResult{}, err
	}
	newID := chapterMap[chapter.ID]
	return domain.TrashRestoreResult{
		ItemType: domain.TrashItemChapter,
		ItemID:   newID,
		Title:    chapter.Title,
		Message:  fmt.Sprintf("已恢复章节《%s》", chapter.Title),
	}, nil
}

func restoreEntityTx(ctx context.Context, tx *sql.Tx, payload trashPayload) (domain.TrashRestoreResult, error) {
	if len(payload.Entities) == 0 {
		return domain.TrashRestoreResult{}, errors.New("回收站数据缺少设定实体")
	}
	entity := payload.Entities[0]
	entityMap, err := restoreEntitiesTx(ctx, tx, entity.BookID, payload.Entities)
	if err != nil {
		return domain.TrashRestoreResult{}, err
	}
	// 关系另一端若仍存在则保留，不存在则跳过，避免恢复出悬空关系。
	if err := restoreRelationsTx(ctx, tx, entity.BookID, payload.Relations, entityMap); err != nil {
		return domain.TrashRestoreResult{}, err
	}
	return domain.TrashRestoreResult{
		ItemType: domain.TrashItemEntity,
		ItemID:   entityMap[entity.ID],
		Title:    entity.Name,
		Message:  fmt.Sprintf("已恢复设定「%s」", entity.Name),
	}, nil
}

func restoreEntitiesTx(ctx context.Context, tx *sql.Tx, bookID int64, entities []domain.Entity) (map[int64]int64, error) {
	result := make(map[int64]int64, len(entities))
	for _, entity := range entities {
		aliases, err := json.Marshal(entity.Aliases)
		if err != nil {
			return nil, err
		}
		attrs, err := json.Marshal(entity.Attributes)
		if err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO entities (book_id, kind, name, aliases_json, is_invariant, summary, attributes_json)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, bookID, string(entity.Kind), entity.Name, string(aliases), boolToInt(entity.IsInvariant), entity.Summary, string(attrs))
		if err != nil {
			return nil, mapSQLiteError(err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		result[entity.ID] = newID
	}
	return result, nil
}

func restoreChaptersTx(
	ctx context.Context,
	tx *sql.Tx,
	bookID int64,
	volumeMap map[int64]int64,
	chapters []domain.Chapter,
	beatsMap map[int64][]domain.Beat,
	versionsMap map[int64][]domain.ChapterVersion,
	entityMap map[int64]int64,
) (map[int64]int64, error) {
	chapterMap := make(map[int64]int64, len(chapters))
	if len(chapters) == 0 {
		return chapterMap, nil
	}
	sorted := append([]domain.Chapter(nil), chapters...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].GlobalSeq == sorted[j].GlobalSeq {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].GlobalSeq < sorted[j].GlobalSeq
	})

	var nextGlobal int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(global_seq), 0) + 1 FROM chapters WHERE book_id = ?`, bookID).Scan(&nextGlobal); err != nil {
		return nil, mapSQLiteError(err)
	}
	volumeNextSeq := make(map[int64]int)
	for _, ch := range sorted {
		newVolumeID, ok := volumeMap[ch.VolumeID]
		if !ok {
			// 章节所属卷可能已不存在：退化到该书第一卷。
			if err := tx.QueryRowContext(ctx, `SELECT id FROM volumes WHERE book_id = ? ORDER BY seq ASC LIMIT 1`, bookID).Scan(&newVolumeID); err != nil {
				return nil, fmt.Errorf("章节《%s》没有可挂载的分卷: %w", ch.Title, domain.ErrNotFound)
			}
			volumeMap[ch.VolumeID] = newVolumeID
		}
		volSeq, ok := volumeNextSeq[newVolumeID]
		if !ok {
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(vol_seq), 0) + 1 FROM chapters WHERE volume_id = ?`, newVolumeID).Scan(&volSeq); err != nil {
				return nil, mapSQLiteError(err)
			}
		}
		volumeNextSeq[newVolumeID] = volSeq + 1

		res, err := tx.ExecContext(ctx, `
			INSERT INTO chapters (book_id, volume_id, global_seq, vol_seq, title, status, emotion_tag, hook_type,
			                      hook_note, outline_summary, causal_summary, content, word_count, last_briefing_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, bookID, newVolumeID, nextGlobal, volSeq, ch.Title, string(ch.Status), string(ch.EmotionTag), string(ch.HookType),
			ch.HookNote, ch.OutlineSummary, ch.CausalSummary, ch.Content, ch.WordCount, ch.LastBriefingJSON)
		if err != nil {
			return nil, mapSQLiteError(err)
		}
		newChapterID, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		chapterMap[ch.ID] = newChapterID
		nextGlobal++

		for _, beat := range beatsMap[ch.ID] {
			var pov any
			if beat.POVEntityID != nil {
				if mapped, ok := entityMap[*beat.POVEntityID]; ok {
					pov = mapped
				}
			}
			entityIDs := make([]int64, 0, len(beat.EntityIDs))
			for _, id := range beat.EntityIDs {
				if mapped, ok := entityMap[id]; ok {
					entityIDs = append(entityIDs, mapped)
				}
			}
			entityJSON, err := json.Marshal(entityIDs)
			if err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO chapter_beats (chapter_id, seq, stage, summary, conflict_note, pov_entity_id, entity_ids_json, target_words)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`, newChapterID, beat.Seq, string(beat.Stage), beat.Summary, beat.ConflictNote, pov, string(entityJSON), beat.TargetWords); err != nil {
				return nil, mapSQLiteError(err)
			}
		}

		for _, version := range versionsMap[ch.ID] {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO chapter_versions (chapter_id, source, model_name, content, word_count)
				VALUES (?, ?, ?, ?, ?)
			`, newChapterID, string(version.Source), version.ModelName, version.Content, version.WordCount); err != nil {
				return nil, mapSQLiteError(err)
			}
		}
	}
	return chapterMap, nil
}

func restoreRelationsTx(ctx context.Context, tx *sql.Tx, bookID int64, relations []domain.EntityRelation, entityMap map[int64]int64) error {
	for _, rel := range relations {
		sourceID, ok := entityMap[rel.SourceEntityID]
		if !ok {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE id = ? AND book_id = ?`, rel.SourceEntityID, bookID).Scan(&exists); err != nil {
				return mapSQLiteError(err)
			}
			if exists == 0 {
				continue
			}
			sourceID = rel.SourceEntityID
		}
		targetID, ok := entityMap[rel.TargetEntityID]
		if !ok {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE id = ? AND book_id = ?`, rel.TargetEntityID, bookID).Scan(&exists); err != nil {
				return mapSQLiteError(err)
			}
			if exists == 0 {
				continue
			}
			targetID = rel.TargetEntityID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO entity_relations (book_id, source_entity_id, target_entity_id, relation_type, description)
			VALUES (?, ?, ?, ?, ?)
		`, bookID, sourceID, targetID, rel.RelationType, rel.Description); err != nil {
			return mapSQLiteError(err)
		}
	}
	return nil
}

// ensure strings import is used even if future refactors drop other uses.
var _ = strings.TrimSpace
