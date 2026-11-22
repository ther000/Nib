package store

import (
	"context"
	"testing"

	"nib/internal/domain"
)

// createLegacyBook 直接按 Phase 1 的最小 books 列插入数据，供旧版本迁移测试使用。
// Store 的新版 CRUD 默认假设 migration 008 的书籍写作规范列已存在，因此旧版本库不能走 CreateBook。
func createLegacyBook(t *testing.T, st *Store, title string, genre domain.GenreID) domain.Book {
	t.Helper()
	ctx := context.Background()
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO books (title, genre, premise, golden_finger, target_chapter_words, export_dir)
		VALUES (?, ?, '', '', ?, '')
	`, title, string(genre), domain.DefaultTargetChapterWords)
	if err != nil {
		t.Fatalf("insert legacy book: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("legacy book id: %v", err)
	}
	return domain.Book{ID: id, Title: title, Genre: genre, TargetChapterWords: domain.DefaultTargetChapterWords}
}

// createLegacyChapter 直接按 Phase 1 的最小 chapters 列插入章节，供旧版本迁移测试使用。
// Store 的新版 CRUD 默认假设 migration 012 的故事时间线列已存在，因此旧版本库不能走 CreateChapter/SaveChapterContent。
func createLegacyChapter(
	t *testing.T,
	st *Store,
	bookID, volumeID int64,
	seq int,
	title, content string,
	status domain.ChapterStatus,
) domain.Chapter {
	t.Helper()
	ctx := context.Background()
	if status == "" {
		status = domain.ChapterStatusPlanned
	}
	words := domain.CountWords(content)
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO chapters (book_id, volume_id, global_seq, vol_seq, title, status, content, word_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, bookID, volumeID, seq, seq, title, string(status), content, words)
	if err != nil {
		t.Fatalf("insert legacy chapter: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("legacy chapter id: %v", err)
	}
	return domain.Chapter{
		ID:        id,
		BookID:    bookID,
		VolumeID:  volumeID,
		GlobalSeq: seq,
		VolSeq:    seq,
		Title:     title,
		Status:    status,
		Content:   content,
		WordCount: words,
	}
}
