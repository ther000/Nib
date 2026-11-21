package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nib/internal/domain"
	"nib/internal/store"
)

func TestParseAndImportNovelFile(t *testing.T) {
	raw := "\uFEFF第一章 陨落天才\n\n林渊握紧断剑，冷眼看着长老。\n\n第2章：逆转乾坤\n\n古碑亮起金芒，经脉重塑。\n\n### 第3章 宗门大比\n\n擂台之上，一剑惊四座。"
	parsed := ParseNovelText(raw)
	if len(parsed) != 3 {
		t.Fatalf("ParseNovelText got %d chapters, want 3: %+v", len(parsed), parsed)
	}
	if parsed[0].Title != "陨落天才" || parsed[1].Title != "逆转乾坤" || parsed[2].Title != "宗门大比" {
		t.Fatalf("unexpected titles: %+v", parsed)
	}

	tmp := t.TempDir()
	novelPath := filepath.Join(tmp, "剑道独尊.txt")
	if err := os.WriteFile(novelPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write temp novel: %v", err)
	}

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	res, err := ImportBookFromFile(context.Background(), st, novelPath, domain.GenreXuanhuan)
	if err != nil {
		t.Fatalf("ImportBookFromFile failed: %v", err)
	}
	if res.Book.Title != "剑道独尊" || res.ChapterCount != 3 || res.TotalWords == 0 {
		t.Fatalf("unexpected import result: %+v", res)
	}
	chaps, err := st.ListBookChapters(context.Background(), res.Book.ID)
	if err != nil || len(chaps) != 3 {
		t.Fatalf("ListBookChapters = %d, err=%v", len(chaps), err)
	}
}
