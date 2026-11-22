package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"nib/internal/domain"
)

func TestScratchNotesCRUD(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)
	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "便签测试", Genre: domain.GenreUrban})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	note, err := st.UpsertScratchNote(ctx, domain.UpsertScratchNoteInput{
		BookID:  book.ID,
		Title:   "巷口伏笔",
		Content: "下一章让老陈在巷口捡到半张车票。",
		Tags:    []string{"伏笔", "都市"},
		Pinned:  true,
	})
	if err != nil {
		t.Fatalf("UpsertScratchNote: %v", err)
	}
	if note.ID == 0 || note.Title != "巷口伏笔" || !note.Pinned || len(note.Tags) != 2 {
		t.Fatalf("unexpected note: %+v", note)
	}

	list, err := st.ListScratchNotes(ctx, book.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListScratchNotes: %+v err=%v", list, err)
	}

	note.Title = "巷口伏笔（改）"
	updated, err := st.UpsertScratchNote(ctx, domain.UpsertScratchNoteInput{
		ID: note.ID, BookID: book.ID, Title: note.Title, Content: note.Content, Tags: note.Tags, Pinned: false,
	})
	if err != nil || updated.Title != note.Title || updated.Pinned {
		t.Fatalf("update note: %+v err=%v", updated, err)
	}

	if err := st.DeleteScratchNote(ctx, note.ID); err != nil {
		t.Fatalf("DeleteScratchNote: %v", err)
	}
	list, _ = st.ListScratchNotes(ctx, book.ID)
	if len(list) != 0 {
		t.Fatalf("expected empty notes, got %+v", list)
	}
}

func TestTrashChapterEntityAndBookRestore(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)
	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "回收站测试", Genre: domain.GenreXuanhuan})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	vol, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	if err != nil {
		t.Fatalf("UpsertVolume: %v", err)
	}
	ch, err := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "初入宗门"})
	if err != nil {
		t.Fatalf("CreateChapter: %v", err)
	}
	content := "山门在雾里显出一角，少年攥紧了手里的木牌。"
	if _, err := st.SaveChapterContent(ctx, ch.ID, content, domain.ChapterStatusFinalized, domain.VersionSourceManualBackup, "", ""); err != nil {
		t.Fatalf("SaveChapterContent: %v", err)
	}
	if _, err := st.ReplaceBeats(ctx, ch.ID, []domain.Beat{{Seq: 1, Stage: domain.BeatStageSetup, Summary: "入山门", TargetWords: 500}}); err != nil {
		t.Fatalf("ReplaceBeats: %v", err)
	}
	entity, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "沈青"})
	if err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	// 章节删除 -> 回收 -> 恢复
	if err := st.DeleteChapter(ctx, ch.ID); err != nil {
		t.Fatalf("DeleteChapter: %v", err)
	}
	trash, err := st.ListTrashItems(ctx)
	if err != nil || len(trash) != 1 || trash[0].ItemType != domain.TrashItemChapter {
		t.Fatalf("chapter trash: %+v err=%v", trash, err)
	}
	restored, err := st.RestoreTrashItem(ctx, trash[0].ID)
	if err != nil {
		t.Fatalf("RestoreTrashItem chapter: %v", err)
	}
	if restored.ItemType != domain.TrashItemChapter || restored.ItemID == 0 {
		t.Fatalf("unexpected chapter restore: %+v", restored)
	}
	restoredChapter, err := st.GetChapter(ctx, restored.ItemID)
	if err != nil || restoredChapter.Content != content || restoredChapter.WordCount != domain.CountWords(content) {
		t.Fatalf("restored chapter content mismatch: %+v err=%v", restoredChapter, err)
	}
	beats, _ := st.ListBeats(ctx, restored.ItemID)
	if len(beats) != 1 || beats[0].Summary != "入山门" {
		t.Fatalf("restored beats mismatch: %+v", beats)
	}

	// 设定删除 -> 回收 -> 恢复
	if err := st.DeleteEntity(ctx, entity.ID); err != nil {
		t.Fatalf("DeleteEntity: %v", err)
	}
	trash, _ = st.ListTrashItems(ctx)
	var entityTrashID int64
	for _, item := range trash {
		if item.ItemType == domain.TrashItemEntity {
			entityTrashID = item.ID
		}
	}
	if entityTrashID == 0 {
		t.Fatalf("entity trash missing: %+v", trash)
	}
	entityRestore, err := st.RestoreTrashItem(ctx, entityTrashID)
	if err != nil {
		t.Fatalf("RestoreTrashItem entity: %v", err)
	}
	entities, _ := st.ListEntities(ctx, book.ID, domain.EntityFilter{})
	found := false
	for _, item := range entities {
		if item.ID == entityRestore.ItemID && item.Name == "沈青" {
			found = true
		}
	}
	if !found {
		t.Fatalf("restored entity not found: %+v", entities)
	}

	// 整书删除 -> 回收 -> 恢复（保留卷、章节、正文）
	if err := st.DeleteBook(ctx, book.ID); err != nil {
		t.Fatalf("DeleteBook: %v", err)
	}
	trash, _ = st.ListTrashItems(ctx)
	var bookTrashID int64
	for _, item := range trash {
		if item.ItemType == domain.TrashItemBook {
			bookTrashID = item.ID
		}
	}
	if bookTrashID == 0 {
		t.Fatalf("book trash missing: %+v", trash)
	}
	bookRestore, err := st.RestoreTrashItem(ctx, bookTrashID)
	if err != nil {
		t.Fatalf("RestoreTrashItem book: %v", err)
	}
	restoredBook, err := st.GetBook(ctx, bookRestore.ItemID)
	if err != nil || restoredBook.Title != "回收站测试" || restoredBook.ChapterCount != 1 {
		t.Fatalf("restored book mismatch: %+v err=%v", restoredBook, err)
	}
	chapters, _ := st.ListBookChapters(ctx, restoredBook.ID)
	if len(chapters) != 1 || chapters[0].Content != content {
		t.Fatalf("restored book chapters mismatch: %+v", chapters)
	}
}

func TestBatchChapterOperationsAndSameVolumeReorder(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)
	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "批量章节测试", Genre: domain.GenreUrban})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	vol, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	if err != nil {
		t.Fatalf("UpsertVolume: %v", err)
	}
	ids := make([]int64, 0, 3)
	for i := 1; i <= 3; i++ {
		ch, err := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "章节" + string(rune('0'+i))})
		if err != nil {
			t.Fatalf("CreateChapter: %v", err)
		}
		ids = append(ids, ch.ID)
	}

	// 同卷拖拽：把第 3 章移到卷首。
	if _, err := st.MoveChapter(ctx, domain.MoveChapterInput{ChapterID: ids[2], TargetVolumeID: vol.ID, TargetVolSeq: 1}); err != nil {
		t.Fatalf("same-volume reorder: %v", err)
	}
	list, _ := st.ListChapters(ctx, vol.ID)
	if len(list) != 3 || list[0].ID != ids[2] {
		t.Fatalf("unexpected order after reorder: %+v", list)
	}

	if count, err := st.BatchFinalizeChapters(ctx, []int64{ids[0], ids[1]}); err != nil || count != 2 {
		t.Fatalf("BatchFinalizeChapters: count=%d err=%v", count, err)
	}
	first, _ := st.GetChapter(ctx, ids[0])
	second, _ := st.GetChapter(ctx, ids[1])
	if first.Status != domain.ChapterStatusFinalized || second.Status != domain.ChapterStatusFinalized {
		t.Fatalf("batch finalize failed: %+v %+v", first, second)
	}

	if count, err := st.BatchDeleteChapters(ctx, []int64{ids[0], ids[1]}); err != nil || count != 2 {
		t.Fatalf("BatchDeleteChapters: count=%d err=%v", count, err)
	}
	remaining, _ := st.ListBookChapters(ctx, book.ID)
	if len(remaining) != 1 || remaining[0].ID != ids[2] || remaining[0].GlobalSeq != 1 {
		t.Fatalf("unexpected remaining chapters: %+v", remaining)
	}
	trash, _ := st.ListTrashItems(ctx)
	if len(trash) != 2 {
		t.Fatalf("expected 2 trash entries, got %+v", trash)
	}
}

func TestPhase7ToLatestUpgradeAddsNotesAndBookProfile(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "upgrade-v7.db")
	allMigs, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("loadEmbeddedMigrations: %v", err)
	}
	if len(allMigs) < 9 {
		t.Fatalf("expected at least 9 migrations, got %d", len(allMigs))
	}
	old, err := openWithMigrations(dbPath, allMigs[:7])
	if err != nil {
		t.Fatalf("open v7: %v", err)
	}
	book := createLegacyBook(t, old, "旧库升级测试", domain.GenreUrban)
	if err := old.Close(); err != nil {
		t.Fatalf("close v7: %v", err)
	}

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("upgrade to latest: %v", err)
	}
	defer st.Close()
	loaded, err := st.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetBook after upgrade: %v", err)
	}
	if loaded.POVMode != "third_limited" || loaded.DailyWordGoal != domain.DefaultDailyWordGoal {
		t.Fatalf("unexpected upgraded book profile: %+v", loaded)
	}
	note, err := st.UpsertScratchNote(ctx, domain.UpsertScratchNoteInput{BookID: book.ID, Title: "升级后便签", Content: "可用"})
	if err != nil || note.ID == 0 {
		t.Fatalf("scratch note after upgrade: %+v err=%v", note, err)
	}
	if _, err := st.ListTrashItems(ctx); err != nil {
		t.Fatalf("trash table after upgrade: %v", err)
	}
}

func TestAutoBackupAndRestoreDataFromFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nib.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "自动备份测试", Genre: domain.GenreXianxia})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	backupDir := filepath.Join(dir, "backups")
	backupPath, err := st.CreateAutoBackup(ctx, backupDir, 7)
	if err != nil {
		t.Fatalf("CreateAutoBackup: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("auto backup missing: %v", err)
	}

	if err := st.DeleteBook(ctx, book.ID); err != nil {
		t.Fatalf("DeleteBook: %v", err)
	}
	if _, err := st.GetBook(ctx, book.ID); err == nil {
		t.Fatalf("book should be deleted before restore")
	}

	preRestore, err := st.RestoreDataFromFile(ctx, backupPath)
	if err != nil {
		t.Fatalf("RestoreDataFromFile: %v", err)
	}
	if _, err := os.Stat(preRestore); err != nil {
		t.Fatalf("pre-restore backup missing: %v", err)
	}
	restored, err := st.GetBook(ctx, book.ID)
	if err != nil || restored.Title != "自动备份测试" {
		t.Fatalf("restored book mismatch: %+v err=%v", restored, err)
	}
	// 验证 staging 目录已清理，不残留 .nib-restore-stage-* 或 .bak.* 文件
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if len(e.Name()) > 18 && e.Name()[:18] == ".nib-restore-stage" {
			t.Fatalf("leftover restore staging artifact: %s", e.Name())
		}
	}
}

func TestRestoreChapterPreservesExistingEntityLinksAndPreventsDoubleRestore(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "节拍关联恢复", Genre: domain.GenreXuanhuan})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Seq: 1, Title: "第一卷"})
	hero, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "林渊"})
	sword, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindItem, Name: "玄铁剑"})
	chap, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "试剑"})
	_, err := st.ReplaceBeats(ctx, chap.ID, []domain.Beat{{
		ChapterID:   chap.ID,
		Seq:         1,
		Stage:       domain.BeatStageSetup,
		Summary:     "林渊持剑入场",
		POVEntityID: &hero.ID,
		EntityIDs:   []int64{hero.ID, sword.ID},
		TargetWords: 500,
	}})
	if err != nil {
		t.Fatalf("ReplaceBeats: %v", err)
	}

	if err := st.DeleteChapter(ctx, chap.ID); err != nil {
		t.Fatalf("DeleteChapter: %v", err)
	}
	trash, err := st.ListTrashItems(ctx)
	if err != nil || len(trash) != 1 {
		t.Fatalf("expected 1 trash item, got %+v err=%v", trash, err)
	}

	// 并发两次 RestoreTrashItem，只能有一次成功
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	var restoredChapID int64
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, rErr := st.RestoreTrashItem(ctx, trash[0].ID)
			if rErr == nil {
				mu.Lock()
				successes++
				restoredChapID = res.ItemID
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("expected exactly 1 concurrent restore success, got %d", successes)
	}

	beats, err := st.ListBeats(ctx, restoredChapID)
	if err != nil || len(beats) != 1 {
		t.Fatalf("ListBeats after restore: %+v err=%v", beats, err)
	}
	if beats[0].POVEntityID == nil || *beats[0].POVEntityID != hero.ID {
		t.Fatalf("expected POVEntityID=%d preserved, got %v", hero.ID, beats[0].POVEntityID)
	}
	if len(beats[0].EntityIDs) != 2 || beats[0].EntityIDs[0] != hero.ID || beats[0].EntityIDs[1] != sword.ID {
		t.Fatalf("expected EntityIDs=[%d,%d] preserved, got %+v", hero.ID, sword.ID, beats[0].EntityIDs)
	}
}
