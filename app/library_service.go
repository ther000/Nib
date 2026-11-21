package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo"

	"nib/internal/domain"
	"nib/internal/exporter"
	"nib/internal/genres"
	"nib/internal/importer"
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

// GetWritingStats 汇总单书（bookID > 0）或全部作品（bookID = 0）的近 N 天码字产出与日更进度。
func (s *LibraryService) GetWritingStats(ctx context.Context, bookID int64, days int) (domain.WritingStatsSummary, error) {
	if bookID < 0 {
		return domain.WritingStatsSummary{}, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 不能为负数"}
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.WritingStatsSummary{}, err
	}
	return st.GetWritingStats(ctx, bookID, days)
}

// ListScratchNotes 返回指定书籍的灵感便签/废稿池（置顶优先）。
func (s *LibraryService) ListScratchNotes(ctx context.Context, bookID int64) ([]domain.ScratchNote, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	notes, err := st.ListScratchNotes(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if notes == nil {
		return []domain.ScratchNote{}, nil
	}
	return notes, nil
}

// UpsertScratchNote 新建或更新一条灵感便签。
func (s *LibraryService) UpsertScratchNote(ctx context.Context, in domain.UpsertScratchNoteInput) (domain.ScratchNote, error) {
	st, err := s.rt.store()
	if err != nil {
		return domain.ScratchNote{}, err
	}
	return st.UpsertScratchNote(ctx, in)
}

// DeleteScratchNote 永久删除一条灵感便签。
func (s *LibraryService) DeleteScratchNote(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	st, err := s.rt.store()
	if err != nil {
		return err
	}
	return st.DeleteScratchNote(ctx, id)
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

// InsertChapter 在指定卷的某章之后（afterChapterId=0 表示卷首）插入新章节并自动重排章序。
func (s *LibraryService) InsertChapter(ctx context.Context, input domain.InsertChapterInput) (domain.Chapter, error) {
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
	return st.InsertChapter(ctx, input)
}

// MoveChapter 调整章节顺序（up / down）或跨卷移动并自动重排章序。
func (s *LibraryService) MoveChapter(ctx context.Context, input domain.MoveChapterInput) (domain.Chapter, error) {
	if err := input.Validate(); err != nil {
		return domain.Chapter{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Chapter{}, err
	}
	return st.MoveChapter(ctx, input)
}

// DeleteChapter 删除指定章节并自动重排剩余章节顺序。
func (s *LibraryService) DeleteChapter(ctx context.Context, chapterID int64) error {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return err
	}
	st, err := s.rt.store()
	if err != nil {
		return err
	}
	return st.DeleteChapter(ctx, chapterID)
}

// ReplaceBookText 在全书范围查找并替换文本（每章正文修改前自动保存历史快照）。
func (s *LibraryService) ReplaceBookText(ctx context.Context, input domain.ReplaceBookTextInput) (domain.ReplaceBookTextResult, error) {
	if err := input.Validate(); err != nil {
		return domain.ReplaceBookTextResult{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.ReplaceBookTextResult{}, err
	}
	return st.ReplaceBookText(ctx, input)
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

// ExportBookTXT 弹出原生保存对话框，将整本书合并导出为纯净中文 TXT 文件（用户取消时返回空字符串）。
func (s *LibraryService) ExportBookTXT(ctx context.Context, bookID int64) (string, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return "", err
	}
	st, err := s.rt.store()
	if err != nil {
		return "", err
	}
	exp, err := s.rt.requireExporter()
	if err != nil {
		return "", err
	}
	book, err := st.GetBook(ctx, bookID)
	if err != nil {
		return "", err
	}
	targetPath, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "导出整本 TXT",
		DefaultPath: exporter.SanitizeFilename(book.Title) + ".txt",
		Filters: []mygo.FileFilter{
			{Name: "纯文本小说 (*.txt)", Extensions: []string{"txt"}},
		},
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(targetPath) == "" {
		return "", nil
	}
	return exp.ExportBookTXT(ctx, bookID, targetPath)
}

// ExportBookEPUB 弹出原生保存对话框，将整本书导出为标准 EPUB 3 电子书（用户取消时返回空字符串）。
func (s *LibraryService) ExportBookEPUB(ctx context.Context, bookID int64) (string, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return "", err
	}
	st, err := s.rt.store()
	if err != nil {
		return "", err
	}
	exp, err := s.rt.requireExporter()
	if err != nil {
		return "", err
	}
	book, err := st.GetBook(ctx, bookID)
	if err != nil {
		return "", err
	}
	targetPath, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "导出 EPUB 电子书",
		DefaultPath: exporter.SanitizeFilename(book.Title) + ".epub",
		Filters: []mygo.FileFilter{
			{Name: "EPUB 电子书 (*.epub)", Extensions: []string{"epub"}},
		},
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(targetPath) == "" {
		return "", nil
	}
	return exp.ExportBookEPUB(ctx, bookID, targetPath)
}

// ImportBookFromFile 弹出原生文件选择对话框，读取本地 .txt / .md 旧稿并智能拆章建书；用户取消时返回 nil。
func (s *LibraryService) ImportBookFromFile(ctx context.Context, genre string) (*domain.ImportBookResult, error) {
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx),
		Title:  "选择要导入的小说旧稿（TXT / Markdown）",
		Filters: []mygo.FileFilter{
			{Name: "小说文稿 (*.txt, *.md)", Extensions: []string{"txt", "md", "markdown"}},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
		return nil, nil
	}
	res, err := importer.ImportBookFromFile(ctx, st, paths[0], domain.GenreID(strings.TrimSpace(genre)))
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// GetVolumePacing 计算指定卷的情绪极性折线数据，并自动诊断连续蓄压过载、缺高潮爆发与超期未收伏笔。
func (s *LibraryService) GetVolumePacing(ctx context.Context, volumeID int64) (domain.VolumePacingReport, error) {
	if err := validatePositiveID("volumeId", volumeID); err != nil {
		return domain.VolumePacingReport{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.VolumePacingReport{}, err
	}
	chapters, err := st.ListChapters(ctx, volumeID)
	if err != nil {
		return domain.VolumePacingReport{}, err
	}
	var openFS []domain.Foreshadowing
	if len(chapters) > 0 {
		if ms, msErr := s.rt.memoryStore(); msErr == nil {
			if fsList, fsErr := ms.ListForeshadowings(ctx, chapters[0].BookID, ""); fsErr == nil {
				openFS = fsList
			}
		}
	}
	return domain.DiagnoseVolumePacing(volumeID, chapters, openFS), nil
}

// BatchGenerateOutlines 调用 outline 模型路由流式批量生成 1~10 章结构化章纲与节拍卡并落库为 outlined 状态。
func (s *LibraryService) BatchGenerateOutlines(
	ctx context.Context,
	req domain.BatchOutlineRequest,
	ch *mygo.Channel[domain.OutlineChunk],
) ([]domain.Chapter, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if req.Count == 0 {
		req.Count = domain.DefaultBatchOutlineCount
	}
	eng, genCtx, cancel, release, err := s.rt.trackBatchEngine(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	emit := func(chunk domain.OutlineChunk) error {
		if ch == nil {
			return nil
		}
		if sendErr := ch.Send(chunk); sendErr != nil {
			cancel()
			return sendErr
		}
		return nil
	}

	chapters, err := eng.BatchGenerateOutlines(genCtx, req, emit)
	if err != nil {
		return nil, err
	}
	if chapters == nil {
		return []domain.Chapter{}, nil
	}
	return chapters, nil
}

// GenerateBookGenesis 根据一句话灵感流式生成完整开书企划（书名、简介、金手指、初始设定、第一卷与初始章纲）。
func (s *LibraryService) GenerateBookGenesis(
	ctx context.Context,
	req domain.BookGenesisRequest,
	ch *mygo.Channel[domain.GenesisChunk],
) (domain.BookGenesisPlan, error) {
	if err := req.Validate(); err != nil {
		return domain.BookGenesisPlan{}, err
	}
	eng, genCtx, cancel, release, err := s.rt.trackGenesisEngine(ctx)
	if err != nil {
		return domain.BookGenesisPlan{}, err
	}
	defer release()

	emit := func(chunk domain.GenesisChunk) error {
		if ch == nil {
			return nil
		}
		if sendErr := ch.Send(chunk); sendErr != nil {
			cancel()
			return sendErr
		}
		return nil
	}

	return eng.GenerateBookGenesis(genCtx, req, emit)
}

// ApplyBookGenesis 将作者确认后的开书企划一次性写入书库。
func (s *LibraryService) ApplyBookGenesis(ctx context.Context, in domain.ApplyGenesisInput) (domain.ApplyGenesisResult, error) {
	if err := in.Validate(); err != nil {
		return domain.ApplyGenesisResult{}, err
	}
	eng, genCtx, _, release, err := s.rt.trackGenesisEngine(ctx)
	if err != nil {
		return domain.ApplyGenesisResult{}, err
	}
	defer release()

	res, err := eng.ApplyBookGenesis(genCtx, in)
	if err != nil {
		return domain.ApplyGenesisResult{}, err
	}
	if res.Entities == nil {
		res.Entities = []domain.Entity{}
	}
	if res.Chapters == nil {
		res.Chapters = []domain.Chapter{}
	}
	return res, nil
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
