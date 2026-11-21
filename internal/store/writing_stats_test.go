package store

import (
	"context"
	"testing"

	"nib/internal/domain"
)

func TestBookProfileAndWritingStats(t *testing.T) {
	ctx := context.Background()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	book, err := st.CreateBook(ctx, domain.CreateBookInput{
		Title:         "统计测试书",
		Genre:         domain.GenreXuanhuan,
		POVMode:       "first_person",
		PacingStyle:   "番茄快节奏",
		WritingPrompt: "每章必须有一次信息差反转",
		DailyWordGoal: 5000,
	})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	if book.POVMode != "first_person" || book.PacingStyle != "番茄快节奏" || book.DailyWordGoal != 5000 {
		t.Fatalf("unexpected book profile fields: %+v", book)
	}

	vol, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: book.ID,
		Title:  "第一卷",
	})
	if err != nil {
		t.Fatalf("UpsertVolume: %v", err)
	}
	ch, err := st.CreateChapter(ctx, domain.CreateChapterInput{
		BookID:   book.ID,
		VolumeID: vol.ID,
		Title:    "开局",
	})
	if err != nil {
		t.Fatalf("CreateChapter: %v", err)
	}

	if _, err := st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID: ch.ID,
		Content:   "风起云涌，少年拔剑出鞘。",
	}); err != nil {
		t.Fatalf("SaveChapterDraft: %v", err)
	}
	if _, err := st.FinalizeChapter(ctx, ch.ID); err != nil {
		t.Fatalf("FinalizeChapter: %v", err)
	}

	loaded, err := st.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if loaded.ChapterCount != 1 || loaded.FinalizedCount != 1 || loaded.TotalWords != domain.CountWords("风起云涌，少年拔剑出鞘。") {
		t.Fatalf("unexpected aggregated book stats: %+v", loaded)
	}

	stats, err := st.GetWritingStats(ctx, book.ID, 14)
	if err != nil {
		t.Fatalf("GetWritingStats: %v", err)
	}
	if stats.TodayWords != loaded.TotalWords || stats.DailyGoal != 5000 || stats.StreakDays != 1 || len(stats.Daily) != 14 {
		t.Fatalf("unexpected writing stats: %+v", stats)
	}

	allStats, err := st.GetWritingStats(ctx, 0, 7)
	if err != nil {
		t.Fatalf("GetWritingStats all: %v", err)
	}
	if allStats.TotalChapters != 1 || allStats.FinalizedChapters != 1 || allStats.TotalWords != loaded.TotalWords || len(allStats.Daily) != 7 {
		t.Fatalf("unexpected all-books writing stats: %+v", allStats)
	}

	updated, err := st.UpdateBook(ctx, domain.UpdateBookInput{
		ID:                 book.ID,
		Title:              book.Title,
		Genre:              book.Genre,
		TargetChapterWords: 2600,
		POVMode:            "third_omniscient",
		PacingStyle:        "起点铺垫流",
		WritingPrompt:      "主角不降智。",
		DailyWordGoal:      6000,
	})
	if err != nil {
		t.Fatalf("UpdateBook profile: %v", err)
	}
	if updated.POVMode != "third_omniscient" || updated.PacingStyle != "起点铺垫流" || updated.WritingPrompt != "主角不降智。" || updated.DailyWordGoal != 6000 {
		t.Fatalf("unexpected updated book profile: %+v", updated)
	}
}
