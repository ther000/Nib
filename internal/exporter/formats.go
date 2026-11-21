package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"nib/internal/domain"
)

// ExportBookTXT 将整本书合并导出为纯净中文网文 TXT 文件（UTF-8，原子落盘）。
func ExportBookTXT(book domain.Book, volumes []domain.Volume, chapters []domain.Chapter, targetPath string) (string, error) {
	volByID := make(map[int64]domain.Volume, len(volumes))
	for _, v := range volumes {
		volByID[v.ID] = v
	}
	sorted := sortChaptersForExport(chapters)

	var b strings.Builder
	title := strings.TrimSpace(book.Title)
	if title == "" {
		title = fallbackFilename
	}
	fmt.Fprintf(&b, "《%s》\n", title)
	if p := strings.TrimSpace(book.Premise); p != "" {
		fmt.Fprintf(&b, "\n简介：\n%s\n", p)
	}
	b.WriteString("\n" + strings.Repeat("=", 36) + "\n")

	var lastVolID int64 = -1
	for _, ch := range sorted {
		if ch.VolumeID != lastVolID {
			if vol, ok := volByID[ch.VolumeID]; ok {
				fmt.Fprintf(&b, "\n\n【第%d卷 %s】\n", vol.Seq, strings.TrimSpace(vol.Title))
			}
			lastVolID = ch.VolumeID
		}
		seq := ch.GlobalSeq
		if seq <= 0 {
			seq = ch.VolSeq
		}
		fmt.Fprintf(&b, "\n\n第%d章 %s\n\n", seq, strings.TrimSpace(ch.Title))
		content := strings.ReplaceAll(strings.TrimSpace(ch.Content), "\r\n", "\n")
		if content != "" {
			b.WriteString(content)
			b.WriteByte('\n')
		}
	}

	if err := writeFileAtomic(targetPath, []byte(b.String())); err != nil {
		return "", fmt.Errorf("export TXT to %q: %w", targetPath, err)
	}
	return targetPath, nil
}

// ExportBookEPUB 使用纯 Go 标准库 archive/zip 生成合法的 EPUB 3 电子书（零 CGO/外部依赖）。
func ExportBookEPUB(book domain.Book, volumes []domain.Volume, chapters []domain.Chapter, targetPath string) (string, error) {
	sorted := sortChaptersForExport(chapters)
	title := strings.TrimSpace(book.Title)
	if title == "" {
		title = fallbackFilename
	}
	bookUID := fmt.Sprintf("urn:nib:book:%d:%d", book.ID, time.Now().Unix())
	modified := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 1. mimetype 必须是 ZIP 首项且不压缩（STORE），符合 EPUB 规范。
	mimeHeader := &zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	}
	mw, err := zw.CreateHeader(mimeHeader)
	if err != nil {
		return "", err
	}
	if _, err := mw.Write([]byte("application/epub+zip")); err != nil {
		return "", err
	}

	writeZipText := func(name, content string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(content))
		return err
	}

	// 2. META-INF/container.xml
	if err := writeZipText("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`); err != nil {
		return "", err
	}

	// 3. 基础中文排印样式表
	if err := writeZipText("OEBPS/style.css", `body { font-family: serif; line-height: 1.8; margin: 1.2em; }
h1 { font-size: 1.35em; margin-bottom: 1em; }
p { text-indent: 2em; margin: 0.6em 0; }
`); err != nil {
		return "", err
	}

	// 4. 分章 XHTML + 清单与导航项
	var manifestItems strings.Builder
	var spineItems strings.Builder
	var navItems strings.Builder

	for idx, ch := range sorted {
		itemID := fmt.Sprintf("ch%04d", idx+1)
		fileName := fmt.Sprintf("chapters/%s.xhtml", itemID)
		seq := ch.GlobalSeq
		if seq <= 0 {
			seq = ch.VolSeq
		}
		chapHeading := fmt.Sprintf("第%d章 %s", seq, strings.TrimSpace(ch.Title))

		var bodyHTML strings.Builder
		raw := strings.ReplaceAll(ch.Content, "\r\n", "\n")
		for _, line := range strings.Split(raw, "\n") {
			p := strings.TrimSpace(line)
			if p == "" {
				continue
			}
			fmt.Fprintf(&bodyHTML, "  <p>%s</p>\n", html.EscapeString(p))
		}
		if bodyHTML.Len() == 0 {
			bodyHTML.WriteString("  <p>（本章暂无正文）</p>\n")
		}

		chapXHTML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head>
  <meta charset="UTF-8"/>
  <title>%s</title>
  <link rel="stylesheet" type="text/css" href="../style.css"/>
</head>
<body>
  <h1>%s</h1>
%s</body>
</html>
`, html.EscapeString(chapHeading), html.EscapeString(chapHeading), bodyHTML.String())

		if err := writeZipText("OEBPS/"+fileName, chapXHTML); err != nil {
			return "", err
		}
		fmt.Fprintf(&manifestItems, "    <item id=%q href=%q media-type=\"application/xhtml+xml\"/>\n", itemID, fileName)
		fmt.Fprintf(&spineItems, "    <itemref idref=%q/>\n", itemID)
		fmt.Fprintf(&navItems, "      <li><a href=%q>%s</a></li>\n", fileName, html.EscapeString(chapHeading))
	}

	// 5. OEBPS/nav.xhtml
	navXHTML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh-CN">
<head>
  <meta charset="UTF-8"/>
  <title>目录</title>
</head>
<body>
  <nav epub:type="toc" id="toc">
    <h1>%s</h1>
    <ol>
%s    </ol>
  </nav>
</body>
</html>
`, html.EscapeString(title), navItems.String())
	if err := writeZipText("OEBPS/nav.xhtml", navXHTML); err != nil {
		return "", err
	}

	// 6. OEBPS/content.opf
	opf := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid" xml:lang="zh-CN">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">%s</dc:identifier>
    <dc:title>%s</dc:title>
    <dc:language>zh-CN</dc:language>
    <dc:description>%s</dc:description>
    <meta property="dcterms:modified">%s</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="css" href="style.css" media-type="text/css"/>
%s  </manifest>
  <spine>
%s  </spine>
</package>
`, html.EscapeString(bookUID), html.EscapeString(title), html.EscapeString(strings.TrimSpace(book.Premise)), modified, manifestItems.String(), spineItems.String())
	if err := writeZipText("OEBPS/content.opf", opf); err != nil {
		return "", err
	}

	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := writeFileAtomic(targetPath, buf.Bytes()); err != nil {
		return "", fmt.Errorf("export EPUB to %q: %w", targetPath, err)
	}
	return targetPath, nil
}

// ExportBookTXT 按 bookID 读取全书并导出合并 TXT。
func (e *StoreExporter) ExportBookTXT(ctx context.Context, bookID int64, targetPath string) (string, error) {
	book, volumes, chapters, err := e.loadBookTree(ctx, bookID)
	if err != nil {
		return "", err
	}
	return ExportBookTXT(book, volumes, chapters, targetPath)
}

// ExportBookEPUB 按 bookID 读取全书并导出标准 EPUB 电子书。
func (e *StoreExporter) ExportBookEPUB(ctx context.Context, bookID int64, targetPath string) (string, error) {
	book, volumes, chapters, err := e.loadBookTree(ctx, bookID)
	if err != nil {
		return "", err
	}
	return ExportBookEPUB(book, volumes, chapters, targetPath)
}

func (e *StoreExporter) loadBookTree(ctx context.Context, bookID int64) (domain.Book, []domain.Volume, []domain.Chapter, error) {
	if e == nil || e.src == nil {
		return domain.Book{}, nil, nil, fmt.Errorf("exporter data source is nil")
	}
	book, err := e.src.GetBook(ctx, bookID)
	if err != nil {
		return domain.Book{}, nil, nil, err
	}
	volumes, err := e.src.ListVolumes(ctx, bookID)
	if err != nil {
		return domain.Book{}, nil, nil, err
	}
	chapters, err := e.src.ListBookChapters(ctx, bookID)
	if err != nil {
		return domain.Book{}, nil, nil, err
	}
	return book, volumes, chapters, nil
}

func sortChaptersForExport(chapters []domain.Chapter) []domain.Chapter {
	sorted := make([]domain.Chapter, len(chapters))
	copy(sorted, chapters)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].GlobalSeq != sorted[j].GlobalSeq {
			return sorted[i].GlobalSeq < sorted[j].GlobalSeq
		}
		if sorted[i].VolSeq != sorted[j].VolSeq {
			return sorted[i].VolSeq < sorted[j].VolSeq
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}
