package exporter

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nib/internal/domain"
)

func TestExportBookTXTAndEPUB(t *testing.T) {
	tmp := t.TempDir()
	book := domain.Book{ID: 1, Title: "青冥剑录", Premise: "少年持剑破苍穹。"}
	vols := []domain.Volume{{ID: 10, BookID: 1, Seq: 1, Title: "初入江湖"}}
	chaps := []domain.Chapter{
		{ID: 100, BookID: 1, VolumeID: 10, GlobalSeq: 1, VolSeq: 1, Title: "雨夜出剑", Content: "雨落石阶。\n\n剑鸣破空。"},
		{ID: 101, BookID: 1, VolumeID: 10, GlobalSeq: 2, VolSeq: 2, Title: "黑市暗涌", Content: "灯火阑珊处，杀机暗藏。"},
	}

	txtPath := filepath.Join(tmp, "book.txt")
	if _, err := ExportBookTXT(book, vols, chaps, txtPath); err != nil {
		t.Fatalf("ExportBookTXT failed: %v", err)
	}
	rawTxt, err := os.ReadFile(txtPath)
	if err != nil {
		t.Fatalf("read txt: %v", err)
	}
	if !strings.Contains(string(rawTxt), "第1章 雨夜出剑") || !strings.Contains(string(rawTxt), "剑鸣破空。") {
		t.Fatalf("unexpected txt content: %s", string(rawTxt))
	}

	epubPath := filepath.Join(tmp, "book.epub")
	if _, err := ExportBookEPUB(book, vols, chaps, epubPath); err != nil {
		t.Fatalf("ExportBookEPUB failed: %v", err)
	}
	zr, err := zip.OpenReader(epubPath)
	if err != nil {
		t.Fatalf("open epub zip: %v", err)
	}
	defer zr.Close()
	if len(zr.File) < 5 || zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Fatalf("invalid epub mimetype entry: %+v", zr.File[0])
	}
	seen := make(map[string]bool)
	for _, f := range zr.File {
		seen[f.Name] = true
	}
	for _, req := range []string{"META-INF/container.xml", "OEBPS/content.opf", "OEBPS/nav.xhtml", "OEBPS/chapters/ch0001.xhtml", "OEBPS/chapters/ch0002.xhtml"} {
		if !seen[req] {
			t.Fatalf("missing required epub file %q", req)
		}
	}
}
