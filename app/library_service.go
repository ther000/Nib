package app

import (
	"context"
	"strings"

	"nib/internal/domain"
	"nib/internal/genres"
)

// LibraryService 提供书、卷、章、节拍卡及单向 Markdown 导出的桌面 RPC 绑定。
type LibraryService struct {
	rt *Runtime
}

// NewLibraryService 构造绑定到共享 Runtime 的 LibraryService。
func NewLibraryService(rt *Runtime) *LibraryService {
	return &LibraryService{rt: rt}
}

// ListGenrePresets 返回全部内置男频题材包规格（xuanhuan / xianxia / urban / general）。
func (s *LibraryService) ListGenrePresets() []domain.GenrePreset {
	return genres.List()
}

// ListBooks 返回书架上的全部书籍列表。
func (s *LibraryService) ListBooks(ctx context.Context) ([]domain.Book, error) {
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	books, err := st.ListBooks(ctx)
	if err != nil {
		return nil, err
	}
	if books == nil {
		return []domain.Book{}, nil
	}
	return books, nil
}

// CreateBook 校验参数并创建一本新书。
func (s *LibraryService) CreateBook(ctx context.Context, input domain.CreateBookInput) (domain.Book, error) {
	if err := input.Validate(); err != nil {
		return domain.Book{}, err
	}
	if input.TargetChapterWords == 0 {
		input.TargetChapterWords = domain.DefaultTargetChapterWords
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Book{}, err
	}
	return st.CreateBook(ctx, input)
}

// UpdateBook 校验参数并更新书籍基础设定与导出目录。
func (s *LibraryService) UpdateBook(ctx context.Context, input domain.UpdateBookInput) (domain.Book, error) {
	if err := input.Validate(); err != nil {
		return domain.Book{}, err
	}
	if input.TargetChapterWords == 0 {
		input.TargetChapterWords = domain.DefaultTargetChapterWords
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Book{}, err
	}
	return st.UpdateBook(ctx, input)
}

// DeleteBook 删除指定书籍及其级联数据。
func (s *LibraryService) DeleteBook(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	st, err := s.rt.store()
	if err != nil {
		return err
	}
	return st.DeleteBook(ctx, id)
}

// ListVolumes 返回指定书籍下的全部卷（按 seq 升序）。
func (s *LibraryService) ListVolumes(ctx context.Context, bookID int64) ([]domain.Volume, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	vols, err := st.ListVolumes(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if vols == nil {
		return []domain.Volume{}, nil
	}
	return vols, nil
}

// UpsertVolume 创建或更新卷信息。
func (s *LibraryService) UpsertVolume(ctx context.Context, input domain.UpsertVolumeInput) (domain.Volume, error) {
	if err := input.Validate(); err != nil {
		return domain.Volume{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Volume{}, err
	}
	return st.UpsertVolume(ctx, input)
}

// DeleteVolume 删除指定卷及其下属章节。
func (s *LibraryService) DeleteVolume(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	st, err := s.rt.store()
	if err != nil {
		return err
	}
	return st.DeleteVolume(ctx, id)
}

// ListChapters 返回指定卷下的全部章节。
func (s *LibraryService) ListChapters(ctx context.Context, volumeID int64) ([]domain.Chapter, error) {
	if err := validatePositiveID("volumeId", volumeID); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	chaps, err := st.ListChapters(ctx, volumeID)
	if err != nil {
		return nil, err
	}
	if chaps == nil {
		return []domain.Chapter{}, nil
	}
	return chaps, nil
}

// ListBookChapters 返回全书所有章节（按 global_seq 升序）。
func (s *LibraryService) ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	chaps, err := st.ListBookChapters(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if chaps == nil {
		return []domain.Chapter{}, nil
	}
	return chaps, nil
}

// GetChapter 读取单章详情（含正文与最近作战简报）。
func (s *LibraryService) GetChapter(ctx context.Context, id int64) (domain.Chapter, error) {
	if err := validatePositiveID("id", id); err != nil {
		return domain.Chapter{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Chapter{}, err
	}
	return st.GetChapter(ctx, id)
}

// CreateChapter 在指定卷中创建新章节。
func (s *LibraryService) CreateChapter(ctx context.Context, input domain.CreateChapterInput) (domain.Chapter, error) {
	if err := input.Validate(); err != nil {
		return domain.Chapter{}, err
	}
	if input.EmotionTag == "" {
		input.EmotionTag = domain.EmotionSetup
	}
	if input.HookType == "" {
		input.HookType = domain.HookMystery
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Chapter{}, err
	}
	return st.CreateChapter(ctx, input)
}

// UpdateChapterMeta 更新章节标题、状态、情绪标签、钩子与章纲摘要。
func (s *LibraryService) UpdateChapterMeta(ctx context.Context, input domain.UpdateChapterMetaInput) (domain.Chapter, error) {
	if err := input.Validate(); err != nil {
		return domain.Chapter{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Chapter{}, err
	}
	return st.UpdateChapterMeta(ctx, input)
}

// SaveChapterDraft 保存章节正文（由 Store 先归档历史快照），并在书籍配置了导出目录时触发单向 Markdown 同步。
func (s *LibraryService) SaveChapterDraft(ctx context.Context, input domain.SaveChapterDraftInput) (domain.Chapter, error) {
	if err := input.Validate(); err != nil {
		return domain.Chapter{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Chapter{}, err
	}
	chap, err := st.SaveChapterDraft(ctx, input)
	if err != nil {
		return domain.Chapter{}, err
	}
	if err := s.syncChapterMarkdownIfConfigured(ctx, st, chap); err != nil {
		return chap, err
	}
	return chap, nil
}

// FinalizeChapter 将章节状态置为 finalized，并在书籍配置了导出目录时触发单向 Markdown 同步。
func (s *LibraryService) FinalizeChapter(ctx context.Context, chapterID int64) (domain.Chapter, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return domain.Chapter{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Chapter{}, err
	}
	chap, err := st.FinalizeChapter(ctx, chapterID)
	if err != nil {
		return domain.Chapter{}, err
	}
	if err := s.syncChapterMarkdownIfConfigured(ctx, st, chap); err != nil {
		return chap, err
	}
	return chap, nil
}

// ListBeats 返回章节下的全部节拍卡（按 seq 升序）。
func (s *LibraryService) ListBeats(ctx context.Context, chapterID int64) ([]domain.Beat, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	beats, err := st.ListBeats(ctx, chapterID)
	if err != nil {
		return nil, err
	}
	if beats == nil {
		return []domain.Beat{}, nil
	}
	return beats, nil
}

// ReplaceBeats 整体校验并替换章节节拍卡序列。
func (s *LibraryService) ReplaceBeats(ctx context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return nil, err
	}
	normalized := make([]domain.Beat, len(beats))
	for i, b := range beats {
		if err := b.Validate(); err != nil {
			return nil, err
		}
		b.ChapterID = chapterID
		b.Seq = i + 1
		if b.TargetWords == 0 {
			b.TargetWords = domain.DefaultBeatTargetWords
		}
		if b.EntityIDs == nil {
			b.EntityIDs = []int64{}
		}
		normalized[i] = b
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	saved, err := st.ReplaceBeats(ctx, chapterID, normalized)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return []domain.Beat{}, nil
	}
	return saved, nil
}

// ExportBookMarkdown 手动触发全书卷章树与设定铁律单向 Markdown 镜像导出。
func (s *LibraryService) ExportBookMarkdown(ctx context.Context, bookID int64) (domain.ExportResult, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.ExportResult{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.ExportResult{}, err
	}
	exp, err := s.rt.requireExporter()
	if err != nil {
		return domain.ExportResult{}, err
	}
	book, err := st.GetBook(ctx, bookID)
	if err != nil {
		return domain.ExportResult{}, err
	}
	if strings.TrimSpace(book.ExportDir) == "" {
		return domain.ExportResult{}, domain.ErrExportDirNotSet
	}
	return exp.ExportBook(ctx, bookID)
}

func (s *LibraryService) syncChapterMarkdownIfConfigured(ctx context.Context, st Store, chap domain.Chapter) error {
	exp := s.rt.exporter()
	if exp == nil {
		return nil
	}
	book, err := st.GetBook(ctx, chap.BookID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(book.ExportDir) == "" {
		return nil
	}
	_, err = exp.ExportChapter(ctx, chap.ID)
	return err
}
