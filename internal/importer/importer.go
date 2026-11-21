package importer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nib/internal/domain"
)

// ParsedChapter 表示从旧稿文本拆分出的单章标题与正文。
type ParsedChapter struct {
	Title   string
	Content string
}

// Store 定义导入旧稿建书所需的最小仓储契约（由 *store.Store 自动满足）。
type Store interface {
	CreateBook(ctx context.Context, in domain.CreateBookInput) (domain.Book, error)
	UpsertVolume(ctx context.Context, in domain.UpsertVolumeInput) (domain.Volume, error)
	CreateChapter(ctx context.Context, in domain.CreateChapterInput) (domain.Chapter, error)
	SaveChapterDraft(ctx context.Context, in domain.SaveChapterDraftInput) (domain.Chapter, error)
}

var (
	// 匹配常见中文网文章节标题（如 "第1章 少年"、"第一百二十三章：破局"、"### 第003章 暗涌"）
	reChineseChapterHeading = regexp.MustCompile(`^(?:#{1,4}\s*)?第[零一二三四五六七八九十百千万两0-9]+[章节回卷集话](?:[\s：:·\-—]+(.*))?$`)
	// 匹配纯 Markdown 一级/二级标题（如 "# 开篇" / "## 暗流"）
	reMarkdownHeading = regexp.MustCompile(`^#{1,2}\s+(.+)$`)
)

// ParseNovelText 将整本 TXT/Markdown 存稿智能拆分为多章；若未识别到章节标题则整体作为第一章。
func ParseNovelText(raw string) []ParsedChapter {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.TrimPrefix(normalized, "\uFEFF") // 去除 UTF-8 BOM
	lines := strings.Split(normalized, "\n")

	chapters := make([]ParsedChapter, 0)
	var curTitle string
	var curLines []string

	flush := func() {
		body := strings.TrimSpace(strings.Join(curLines, "\n"))
		if curTitle == "" && body == "" {
			return
		}
		title := strings.TrimSpace(curTitle)
		if title == "" {
			title = fmt.Sprintf("第%d章", len(chapters)+1)
		}
		chapters = append(chapters, ParsedChapter{
			Title:   title,
			Content: body,
		})
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if heading, ok := matchChapterHeading(trimmed); ok {
			if curTitle != "" || strings.TrimSpace(strings.Join(curLines, "\n")) != "" {
				flush()
			}
			curTitle = heading
			curLines = nil
			continue
		}
		curLines = append(curLines, line)
	}
	flush()

	if len(chapters) == 0 && strings.TrimSpace(normalized) != "" {
		chapters = append(chapters, ParsedChapter{
			Title:   "第一章",
			Content: strings.TrimSpace(normalized),
		})
	}
	return chapters
}

func matchChapterHeading(line string) (string, bool) {
	if line == "" || len([]rune(line)) > 60 {
		return "", false
	}
	if m := reChineseChapterHeading.FindStringSubmatch(line); m != nil {
		sub := strings.TrimSpace(m[1])
		if sub != "" {
			return sub, true
		}
		return strings.TrimLeft(line, "# "), true
	}
	if m := reMarkdownHeading.FindStringSubmatch(line); m != nil {
		title := strings.TrimSpace(m[1])
		if title != "" {
			return title, true
		}
	}
	return "", false
}

// ImportBookFromFile 读取本地 .txt / .md 文件，按章节自动拆分并一键建书入库。
func ImportBookFromFile(ctx context.Context, st Store, filePath string, genre domain.GenreID) (domain.ImportBookResult, error) {
	if st == nil {
		return domain.ImportBookResult{}, fmt.Errorf("importer store is nil")
	}
	cleanPath := filepath.Clean(strings.TrimSpace(filePath))
	if cleanPath == "" || cleanPath == "." {
		return domain.ImportBookResult{}, fmt.Errorf("%w: 请选择要导入的文件", domain.ErrValidation)
	}
	rawBytes, err := os.ReadFile(cleanPath)
	if err != nil {
		return domain.ImportBookResult{}, fmt.Errorf("读取导入文件失败: %w", err)
	}

	parsed := ParseNovelText(string(rawBytes))
	if len(parsed) == 0 {
		return domain.ImportBookResult{}, fmt.Errorf("%w: 文件内容为空，无法导入", domain.ErrValidation)
	}

	if !genre.Valid() {
		genre = domain.GenreGeneral
	}
	baseName := strings.TrimSuffix(filepath.Base(cleanPath), filepath.Ext(cleanPath))
	bookTitle := strings.TrimSpace(baseName)
	if bookTitle == "" {
		bookTitle = "导入作品"
	}

	book, err := st.CreateBook(ctx, domain.CreateBookInput{
		Title:              bookTitle,
		Genre:              genre,
		Premise:            fmt.Sprintf("从本地文件《%s》导入（共 %d 章）", filepath.Base(cleanPath), len(parsed)),
		TargetChapterWords: domain.DefaultTargetChapterWords,
	})
	if err != nil {
		return domain.ImportBookResult{}, err
	}

	vol, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: book.ID,
		Seq:    1,
		Title:  "第一卷 正文",
	})
	if err != nil {
		return domain.ImportBookResult{}, err
	}

	totalWords := 0
	for _, pc := range parsed {
		ch, cErr := st.CreateChapter(ctx, domain.CreateChapterInput{
			BookID:   book.ID,
			VolumeID: vol.ID,
			Title:    pc.Title,
		})
		if cErr != nil {
			return domain.ImportBookResult{}, cErr
		}
		saved, sErr := st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
			ChapterID: ch.ID,
			Content:   pc.Content,
			Status:    domain.ChapterStatusReviewing,
		})
		if sErr != nil {
			return domain.ImportBookResult{}, sErr
		}
		totalWords += saved.WordCount
	}

	return domain.ImportBookResult{
		Book:         book,
		Volume:       vol,
		ChapterCount: len(parsed),
		TotalWords:   totalWords,
		SourcePath:   cleanPath,
	}, nil
}
