package exporter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"nib/internal/domain"
	"nib/internal/genres"
)

const (
	maxFilenameRunes     = 60
	fallbackFilename     = "未命名"
	atlasSummaryFilename = "00_设定与铁律汇总.md"
)

// DataSource 定义基于 ID 导出时所需的最小只读数据源接口（与 internal/store 解耦）。
type DataSource interface {
	GetBook(ctx context.Context, id int64) (domain.Book, error)
	GetVolume(ctx context.Context, id int64) (domain.Volume, error)
	GetChapter(ctx context.Context, id int64) (domain.Chapter, error)
	ListVolumes(ctx context.Context, bookID int64) ([]domain.Volume, error)
	ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error)
	ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error)
}

// Exporter 实现单向本地 Markdown 镜像导出（面向已持有领域对象的调用方，如 internal/pipeline）。
type Exporter struct{}

// New 创建一个新的 Exporter 实例。
func New() *Exporter {
	return &Exporter{}
}

// ExportChapter 委托至包级 ExportChapter。
func (e *Exporter) ExportChapter(book domain.Book, vol domain.Volume, chap domain.Chapter) (string, error) {
	return ExportChapter(book, vol, chap)
}

// ExportBook 委托至包级 ExportBook。
func (e *Exporter) ExportBook(book domain.Book, volumes []domain.Volume, chapters []domain.Chapter, entities []domain.Entity) (domain.ExportResult, error) {
	return ExportBook(book, volumes, chapters, entities)
}

// StoreExporter 封装基于 DataSource 的按 ID 单向导出，直接满足 app.Exporter 接口。
type StoreExporter struct {
	src DataSource
}

// NewStoreExporter 创建绑定只读数据源的按 ID 导出器。
func NewStoreExporter(src DataSource) *StoreExporter {
	return &StoreExporter{src: src}
}

// ExportChapter 按 chapterID 读取所属书卷并单向导出为 Markdown 文件。
func (e *StoreExporter) ExportChapter(ctx context.Context, chapterID int64) (string, error) {
	if e == nil || e.src == nil {
		return "", fmt.Errorf("exporter data source is nil")
	}
	chap, err := e.src.GetChapter(ctx, chapterID)
	if err != nil {
		return "", err
	}
	book, err := e.src.GetBook(ctx, chap.BookID)
	if err != nil {
		return "", err
	}
	vol, err := e.src.GetVolume(ctx, chap.VolumeID)
	if err != nil {
		return "", err
	}
	return ExportChapter(book, vol, chap)
}

// ExportBook 按 bookID 读取全书卷章与设定实体并单向镜像导出。
func (e *StoreExporter) ExportBook(ctx context.Context, bookID int64) (domain.ExportResult, error) {
	if e == nil || e.src == nil {
		return domain.ExportResult{}, fmt.Errorf("exporter data source is nil")
	}
	book, err := e.src.GetBook(ctx, bookID)
	if err != nil {
		return domain.ExportResult{}, err
	}
	volumes, err := e.src.ListVolumes(ctx, bookID)
	if err != nil {
		return domain.ExportResult{}, err
	}
	chapters, err := e.src.ListBookChapters(ctx, bookID)
	if err != nil {
		return domain.ExportResult{}, err
	}
	entities, err := e.src.ListEntities(ctx, bookID, domain.EntityFilter{})
	if err != nil {
		return domain.ExportResult{}, err
	}
	return ExportBook(book, volumes, chapters, entities)
}

// SanitizeFilename 将任意书名/卷名/章名清洗为 Windows 与跨平台合法文件/目录名：
// - 剔除 Windows 非法字符 \/:*?"<>| 与全部控制字符；
// - 去除首尾空白及尾随句点/空格；
// - 截断至最多 60 runes（截断后再次清理尾随句点/空格）；
// - 规避 Windows 保留设备名（CON/PRN/AUX/NUL/COM1..9/LPT1..9）；
// - 结果为空时回退为 "未命名"。
func SanitizeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if isIllegalFilenameRune(r) {
			continue
		}
		b.WriteRune(r)
	}

	cleaned := trimFilenameEdges(b.String())
	if cleaned == "" {
		return fallbackFilename
	}

	runes := []rune(cleaned)
	if len(runes) > maxFilenameRunes {
		cleaned = trimFilenameEdges(string(runes[:maxFilenameRunes]))
	}
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return fallbackFilename
	}

	if isWindowsReservedName(cleaned) {
		if len([]rune(cleaned)) >= maxFilenameRunes {
			r := []rune(cleaned)
			cleaned = string(r[:maxFilenameRunes-1]) + "_"
		} else {
			cleaned += "_"
		}
	}

	return cleaned
}

// ExportChapter 将单章以纯净正文 Markdown 原子写入：
// <export_dir>/<合法书名>/第01卷_<合法卷名>/第001章_<合法章名>.md
// 绝不触碰导出目录中任何无关文件。
func ExportChapter(book domain.Book, vol domain.Volume, chap domain.Chapter) (string, error) {
	exportDir := strings.TrimSpace(book.ExportDir)
	if exportDir == "" {
		return "", domain.ErrExportDirNotSet
	}

	bookDir := filepath.Join(filepath.Clean(exportDir), SanitizeFilename(book.Title))
	volDirName := formatVolumeDirName(vol)
	chapFileName := formatChapterFileName(chap)
	targetPath := filepath.Join(bookDir, volDirName, chapFileName)

	markdown := renderChapterMarkdown(chap)
	if err := writeFileAtomic(targetPath, []byte(markdown)); err != nil {
		return "", fmt.Errorf("export chapter %d to %q: %w", chap.ID, targetPath, err)
	}
	return targetPath, nil
}

// ExportBook 单向镜像导出整本书的全部章节以及根目录下的 00_设定与铁律汇总.md。
func ExportBook(book domain.Book, volumes []domain.Volume, chapters []domain.Chapter, entities []domain.Entity) (domain.ExportResult, error) {
	exportDir := strings.TrimSpace(book.ExportDir)
	if exportDir == "" {
		return domain.ExportResult{}, domain.ErrExportDirNotSet
	}
	cleanExportDir := filepath.Clean(exportDir)
	bookDir := filepath.Join(cleanExportDir, SanitizeFilename(book.Title))

	volByID := make(map[int64]domain.Volume, len(volumes))
	for _, v := range volumes {
		volByID[v.ID] = v
	}

	sortedChapters := make([]domain.Chapter, len(chapters))
	copy(sortedChapters, chapters)
	sort.Slice(sortedChapters, func(i, j int) bool {
		if sortedChapters[i].GlobalSeq != sortedChapters[j].GlobalSeq {
			return sortedChapters[i].GlobalSeq < sortedChapters[j].GlobalSeq
		}
		if sortedChapters[i].VolSeq != sortedChapters[j].VolSeq {
			return sortedChapters[i].VolSeq < sortedChapters[j].VolSeq
		}
		return sortedChapters[i].ID < sortedChapters[j].ID
	})

	filesWritten := make([]string, 0, len(sortedChapters)+1)

	atlasPath := filepath.Join(bookDir, atlasSummaryFilename)
	atlasContent := renderAtlasSummaryMarkdown(book, entities)
	if err := writeFileAtomic(atlasPath, []byte(atlasContent)); err != nil {
		return domain.ExportResult{}, fmt.Errorf("export atlas summary to %q: %w", atlasPath, err)
	}
	filesWritten = append(filesWritten, atlasPath)

	for _, chap := range sortedChapters {
		vol, ok := volByID[chap.VolumeID]
		if !ok {
			vol = domain.Volume{ID: chap.VolumeID, Seq: 0, Title: "未分卷"}
		}
		writtenPath, err := ExportChapter(book, vol, chap)
		if err != nil {
			return domain.ExportResult{}, err
		}
		filesWritten = append(filesWritten, writtenPath)
	}

	return domain.ExportResult{
		ExportDir:    cleanExportDir,
		BookDir:      bookDir,
		FilesWritten: filesWritten,
		ChapterCount: len(sortedChapters),
	}, nil
}

func formatVolumeDirName(vol domain.Volume) string {
	seq := vol.Seq
	if seq < 0 {
		seq = 0
	}
	return fmt.Sprintf("第%02d卷_%s", seq, SanitizeFilename(vol.Title))
}

func formatChapterFileName(chap domain.Chapter) string {
	seq := chap.GlobalSeq
	if seq <= 0 {
		seq = chap.VolSeq
	}
	if seq < 0 {
		seq = 0
	}
	return fmt.Sprintf("第%03d章_%s.md", seq, SanitizeFilename(chap.Title))
}

func renderChapterMarkdown(chap domain.Chapter) string {
	seq := chap.GlobalSeq
	if seq <= 0 {
		seq = chap.VolSeq
	}
	title := strings.TrimSpace(chap.Title)
	if title == "" {
		title = fallbackFilename
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# 第%03d章 %s\n", seq, title)

	content := strings.ReplaceAll(chap.Content, "\r\n", "\n")
	if strings.TrimSpace(content) != "" {
		b.WriteByte('\n')
		b.WriteString(strings.TrimRight(content, "\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderAtlasSummaryMarkdown(book domain.Book, entities []domain.Entity) string {
	preset := genres.GetOrDefault(book.Genre)

	var b strings.Builder
	title := strings.TrimSpace(book.Title)
	if title == "" {
		title = fallbackFilename
	}
	fmt.Fprintf(&b, "# %s — 设定与铁律汇总\n\n", title)
	fmt.Fprintf(&b, "- 题材：%s（%s）\n", preset.Name, book.Genre)
	if book.TargetChapterWords > 0 {
		fmt.Fprintf(&b, "- 目标章字数：%d 字\n", book.TargetChapterWords)
	}
	if p := strings.TrimSpace(book.Premise); p != "" {
		fmt.Fprintf(&b, "- 核心卖点：%s\n", p)
	}
	if gf := strings.TrimSpace(book.GoldenFinger); gf != "" {
		fmt.Fprintf(&b, "- 金手指 / 核心机制：%s\n", gf)
	}

	sorted := make([]domain.Entity, len(entities))
	copy(sorted, entities)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ID != sorted[j].ID {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Name < sorted[j].Name
	})

	var invariants []domain.Entity
	byKind := make(map[domain.EntityKind][]domain.Entity)
	for _, e := range sorted {
		if e.IsInvariant {
			invariants = append(invariants, e)
		} else {
			byKind[e.Kind] = append(byKind[e.Kind], e)
		}
	}

	b.WriteString("\n## 世界铁律\n\n")
	if len(invariants) == 0 {
		b.WriteString("（暂无世界铁律）\n")
	} else {
		for _, e := range invariants {
			writeEntityMarkdown(&b, e)
		}
	}

	kindOrder := []domain.EntityKind{
		domain.EntityKindCharacter,
		domain.EntityKindFaction,
		domain.EntityKindLocation,
		domain.EntityKindItem,
		domain.EntityKindSystem,
		domain.EntityKindRule,
	}

	hasNonInvariant := false
	for _, k := range kindOrder {
		if len(byKind[k]) > 0 {
			hasNonInvariant = true
			break
		}
	}
	if hasNonInvariant {
		b.WriteString("\n## 设定实体档案\n")
		for _, k := range kindOrder {
			list := byKind[k]
			if len(list) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\n### %s\n\n", k.Label())
			for _, e := range list {
				writeEntityMarkdown(&b, e)
			}
		}
	}

	return b.String()
}

func writeEntityMarkdown(b *strings.Builder, e domain.Entity) {
	fmt.Fprintf(b, "#### %s", strings.TrimSpace(e.Name))
	if len(e.Aliases) > 0 {
		fmt.Fprintf(b, "（别名：%s）", strings.Join(e.Aliases, "、"))
	}
	b.WriteString("\n\n")

	if s := strings.TrimSpace(e.Summary); s != "" {
		fmt.Fprintf(b, "%s\n\n", s)
	}
	if len(e.Attributes) > 0 {
		keys := make([]string, 0, len(e.Attributes))
		for k := range e.Attributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "- **%s**：%s\n", k, e.Attributes[k])
		}
		b.WriteByte('\n')
	}
}

func isIllegalFilenameRune(r rune) bool {
	if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
		return true
	}
	switch r {
	case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
		return true
	default:
		return false
	}
}

func trimFilenameEdges(s string) string {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	s = strings.TrimRightFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	})
	return s
}

func isWindowsReservedName(name string) bool {
	base := name
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	upper := strings.ToUpper(base)
	switch upper {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

// writeFileAtomic 通过同目录临时文件 + Sync + Rename 实现原子落盘，
// 避免写半截损坏文件且绝不影响同目录其他用户文件。
func writeFileAtomic(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".nib-export-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}
