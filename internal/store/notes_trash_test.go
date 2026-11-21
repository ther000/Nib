package store

import (
	"context"
	"os"
	"path/filepath"
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
}
