package exporter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"nib/internal/domain"
)

func TestSanitizeFilename(t *testing.T) {
	longCJK := strings.Repeat("剑道独尊破苍穹", 12) // 84 runes

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strip windows illegal characters",
			in:   `血战:天渊/宗*秘境?"禁地"<一>|终章`,
			want: "血战天渊宗秘境禁地一终章",
		},
		{
			name: "strip control chars and trailing dots spaces",
			in:   "  \t寒夜\r\n试剑...   ",
			want: "寒夜试剑",
		},
		{
			name: "only illegal chars falls back to default",
			in:   `\/:*?"<>|...  `,
			want: "未命名",
		},
		{
			name: "empty falls back to default",
			in:   "   ",
			want: "未命名",
		},
		{
			name: "cap long CJK name at 60 runes",
			in:   longCJK,
			want: string([]rune(longCJK)[:60]),
		},
		{
			name: "truncate then trim trailing dot",
			in:   strings.Repeat("甲", 59) + ".乙丙",
			want: strings.Repeat("甲", 59),
		},
		{
			name: "windows reserved device name",
			in:   "CON",
			want: "CON_",
		},
		{
			name: "windows reserved device name case insensitive with ext",
			in:   "nul.txt",
			want: "nul.txt_",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeFilename(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if utf8.RuneCountInString(got) > maxFilenameRunes {
				t.Fatalf("rune count %d exceeds %d", utf8.RuneCountInString(got), maxFilenameRunes)
			}
		})
	}
}

func TestExportDirNotSetError(t *testing.T) {
	book := domain.Book{ID: 1, Title: "无导出目录", Genre: domain.GenreXuanhuan, ExportDir: "   "}
	vol := domain.Volume{ID: 1, BookID: 1, Seq: 1, Title: "第一卷"}
	chap := domain.Chapter{ID: 1, BookID: 1, VolumeID: 1, GlobalSeq: 1, VolSeq: 1, Title: "开篇"}

	if _, err := ExportChapter(book, vol, chap); !errors.Is(err, domain.ErrExportDirNotSet) {
		t.Fatalf("ExportChapter err = %v, want ErrExportDirNotSet", err)
	}
	if _, err := ExportBook(book, []domain.Volume{vol}, []domain.Chapter{chap}, nil); !errors.Is(err, domain.ErrExportDirNotSet) {
		t.Fatalf("ExportBook err = %v, want ErrExportDirNotSet", err)
	}
}

func TestExportChapterAtomicAndPreserveUnrelatedFiles(t *testing.T) {
	root := t.TempDir()
	book := domain.Book{
		ID:        1,
		Title:     `渊海:剑主?`,
		Genre:     domain.GenreXuanhuan,
		ExportDir: root,
	}
	vol := domain.Volume{ID: 10, BookID: 1, Seq: 1, Title: `北境/寒霜*`}
	chap := domain.Chapter{
		ID:        100,
		BookID:    1,
		VolumeID:  10,
		GlobalSeq: 1,
		VolSeq:    1,
		Title:     `寒夜<拾碑>..`,
		Content:   "第一段正文。\r\n\r\n第二段正文。",
	}

	// 预先在导出根目录和书籍目录放入用户自己的无关文件，验证导出绝不会误删或篡改。
	unrelatedRoot := filepath.Join(root, "作者大纲笔记.txt")
	if err := os.WriteFile(unrelatedRoot, []byte("keep-root"), 0o644); err != nil {
		t.Fatalf("write unrelatedRoot: %v", err)
	}
	bookDir := filepath.Join(root, "渊海剑主")
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		t.Fatalf("mkdir bookDir: %v", err)
	}
	unrelatedInBook := filepath.Join(bookDir, "自定义备忘.md")
	if err := os.WriteFile(unrelatedInBook, []byte("keep-book"), 0o644); err != nil {
		t.Fatalf("write unrelatedInBook: %v", err)
	}

	writtenPath, err := ExportChapter(book, vol, chap)
	if err != nil {
		t.Fatalf("ExportChapter: %v", err)
	}

	wantPath := filepath.Join(root, "渊海剑主", "第01卷_北境寒霜", "第001章_寒夜拾碑.md")
	if writtenPath != wantPath {
		t.Fatalf("writtenPath = %q, want %q", writtenPath, wantPath)
	}

	raw, err := os.ReadFile(writtenPath)
	if err != nil {
		t.Fatalf("ReadFile exported chapter: %v", err)
	}
	wantMD := "# 第001章 寒夜<拾碑>..\n\n第一段正文。\n\n第二段正文。\n"
	if string(raw) != wantMD {
		t.Fatalf("chapter markdown =\n%q\nwant:\n%q", string(raw), wantMD)
	}

	// 再次原子覆写，验证无残留临时文件且内容正确更新。
	chap.Content = "修订后的正文内容。"
	if _, err := ExportChapter(book, vol, chap); err != nil {
		t.Fatalf("second ExportChapter: %v", err)
	}
	raw2, err := os.ReadFile(writtenPath)
	if err != nil {
		t.Fatalf("ReadFile updated chapter: %v", err)
	}
	if !strings.Contains(string(raw2), "修订后的正文内容。") {
		t.Fatalf("atomic overwrite did not update content: %q", string(raw2))
	}

	// 检查同目录下无残留 .tmp 文件
	entries, err := os.ReadDir(filepath.Dir(writtenPath))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover temp file found: %s", e.Name())
		}
	}

	// 验证无关文件原封不动。
	gotRoot, err := os.ReadFile(unrelatedRoot)
	if err != nil || string(gotRoot) != "keep-root" {
		t.Fatalf("unrelatedRoot altered: %q err=%v", string(gotRoot), err)
	}
	gotBook, err := os.ReadFile(unrelatedInBook)
	if err != nil || string(gotBook) != "keep-book" {
		t.Fatalf("unrelatedInBook altered: %q err=%v", string(gotBook), err)
	}
}

func TestExportBookAndAtlasSummary(t *testing.T) {
	root := t.TempDir()
	book := domain.Book{
		ID:                 1,
		Title:              "青云问仙",
		Genre:              domain.GenreXianxia,
		Premise:            "凡人杂役靠残阵破局",
		GoldenFinger:       "古镜映阵",
		TargetChapterWords: 2800,
		ExportDir:          root,
	}
	vols := []domain.Volume{
		{ID: 1, BookID: 1, Seq: 1, Title: "外门蛰伏"},
		{ID: 2, BookID: 1, Seq: 2, Title: "坊市风云"},
	}
	chaps := []domain.Chapter{
		{ID: 2, BookID: 1, VolumeID: 2, GlobalSeq: 2, VolSeq: 1, Title: "坊市夜交易", Content: "第二章正文。"},
		{ID: 1, BookID: 1, VolumeID: 1, GlobalSeq: 1, VolSeq: 1, Title: "杂役峰晨钟", Content: "第一章正文。"},
	}
	entities := []domain.Entity{
		{
			ID:          1,
			BookID:      1,
			Kind:        domain.EntityKindRule,
			Name:        "寿元硬约束",
			IsInvariant: true,
			Summary:     "炼气寿元止于百岁，绝不可凭空续命。",
		},
		{
			ID:         2,
			BookID:     1,
			Kind:       domain.EntityKindCharacter,
			Name:       "林渊",
			Aliases:    []string{"林执事"},
			Summary:    "稳健行事的外门修士",
			Attributes: map[string]string{"cultivation": "筑基初期", "weapon": "青冥剑"},
		},
	}

	res, err := New().ExportBook(book, vols, chaps, entities)
	if err != nil {
		t.Fatalf("ExportBook: %v", err)
	}
	if res.ChapterCount != 2 {
		t.Fatalf("ChapterCount = %d, want 2", res.ChapterCount)
	}
	if len(res.FilesWritten) != 3 {
		t.Fatalf("len(FilesWritten) = %d, want 3: %v", len(res.FilesWritten), res.FilesWritten)
	}
	if res.BookDir != filepath.Join(root, "青云问仙") {
		t.Fatalf("BookDir = %q", res.BookDir)
	}

	atlasPath := filepath.Join(res.BookDir, atlasSummaryFilename)
	atlasRaw, err := os.ReadFile(atlasPath)
	if err != nil {
		t.Fatalf("ReadFile atlas summary: %v", err)
	}
	atlasText := string(atlasRaw)
	for _, want := range []string{
		"# 青云问仙 — 设定与铁律汇总",
		"题材：仙侠（xianxia）",
		"古镜映阵",
		"## 世界铁律",
		"寿元硬约束",
		"### 角色",
		"林渊（别名：林执事）",
		"- **cultivation**：筑基初期",
	} {
		if !strings.Contains(atlasText, want) {
			t.Fatalf("atlas summary missing %q:\n%s", want, atlasText)
		}
	}

	ch1Path := filepath.Join(res.BookDir, "第01卷_外门蛰伏", "第001章_杂役峰晨钟.md")
	ch2Path := filepath.Join(res.BookDir, "第02卷_坊市风云", "第002章_坊市夜交易.md")
	for _, p := range []string{ch1Path, ch2Path} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected exported chapter file %q: %v", p, err)
		}
	}
}

type fakeDataSource struct {
	book     domain.Book
	volumes  []domain.Volume
	chapters []domain.Chapter
	entities []domain.Entity
}

func (f fakeDataSource) GetBook(_ context.Context, id int64) (domain.Book, error) {
	if f.book.ID != id {
		return domain.Book{}, domain.ErrNotFound
	}
	return f.book, nil
}
func (f fakeDataSource) GetVolume(_ context.Context, id int64) (domain.Volume, error) {
	for _, v := range f.volumes {
		if v.ID == id {
			return v, nil
		}
	}
	return domain.Volume{}, domain.ErrNotFound
}
func (f fakeDataSource) GetChapter(_ context.Context, id int64) (domain.Chapter, error) {
	for _, c := range f.chapters {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Chapter{}, domain.ErrNotFound
}
func (f fakeDataSource) ListVolumes(_ context.Context, _ int64) ([]domain.Volume, error) {
	return f.volumes, nil
}
func (f fakeDataSource) ListBookChapters(_ context.Context, _ int64) ([]domain.Chapter, error) {
	return f.chapters, nil
}
func (f fakeDataSource) ListEntities(_ context.Context, _ int64, _ domain.EntityFilter) ([]domain.Entity, error) {
	return f.entities, nil
}

func TestStoreExporterByID(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src := fakeDataSource{
		book:     domain.Book{ID: 1, Title: "玄镜录", Genre: domain.GenreGeneral, ExportDir: root},
		volumes:  []domain.Volume{{ID: 11, BookID: 1, Seq: 1, Title: "序幕"}},
		chapters: []domain.Chapter{{ID: 101, BookID: 1, VolumeID: 11, GlobalSeq: 1, VolSeq: 1, Title: "初见", Content: "正文"}},
	}
	se := NewStoreExporter(src)

	chPath, err := se.ExportChapter(ctx, 101)
	if err != nil {
		t.Fatalf("StoreExporter.ExportChapter: %v", err)
	}
	if _, err := os.Stat(chPath); err != nil {
		t.Fatalf("exported chapter missing: %v", err)
	}

	res, err := se.ExportBook(ctx, 1)
	if err != nil {
		t.Fatalf("StoreExporter.ExportBook: %v", err)
	}
	if res.ChapterCount != 1 || len(res.FilesWritten) != 2 {
		t.Fatalf("StoreExporter.ExportBook unexpected result: %+v", res)
	}
}
