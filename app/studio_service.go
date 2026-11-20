package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo"

	"nib/internal/domain"
)

// StudioService 提供作战简报预览、流式出稿与章节版本快照回滚的桌面 RPC 绑定。
type StudioService struct {
	rt *Runtime
}

// NewStudioService 构造绑定到共享 Runtime 的 StudioService。
func NewStudioService(rt *Runtime) *StudioService {
	return &StudioService{rt: rt}
}

// PreviewBriefing 编译指定章节的透明作战简报，返回 P0~P7 槽位与 Token 分布估算。
func (s *StudioService) PreviewBriefing(ctx context.Context, chapterID int64, opts domain.BriefingOptions) (domain.Briefing, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return domain.Briefing{}, err
	}
	if opts.TokenBudget < 0 {
		return domain.Briefing{}, &domain.ValidationError{
			Field:   "tokenBudget",
			Message: "上下文配额不能为负数",
		}
	}
	comp, err := s.rt.compiler()
	if err != nil {
		return domain.Briefing{}, err
	}
	return comp.Compile(ctx, chapterID, opts)
}

// GenerateChapter 启动单章流式出稿，通过 *mygo.Channel[domain.GenChunk] 逐片推流。
// 前端取消 AbortSignal 或关闭通道时会立即取消下游生成上下文，保留 partial 文本并落快照。
func (s *StudioService) GenerateChapter(
	ctx context.Context,
	chapterID int64,
	opts domain.GenerateChapterOptions,
	ch *mygo.Channel[domain.GenChunk],
) (domain.Chapter, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return domain.Chapter{}, err
	}
	if err := opts.Validate(); err != nil {
		return domain.Chapter{}, err
	}
	runner, err := s.rt.pipeline()
	if err != nil {
		return domain.Chapter{}, err
	}

	genCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	emit := func(chunk domain.GenChunk) error {
		if ch == nil {
			return nil
		}
		if sendErr := ch.Send(chunk); sendErr != nil {
			cancel()
			return sendErr
		}
		return nil
	}

	return runner.GenerateChapter(genCtx, chapterID, opts, emit)
}

// ListChapterVersions 按时间倒序列出章节的历史正文快照。
func (s *StudioService) ListChapterVersions(ctx context.Context, chapterID int64) ([]domain.ChapterVersion, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return nil, err
	}
	st, err := s.rt.store()
	if err != nil {
		return nil, err
	}
	versions, err := st.ListChapterVersions(ctx, chapterID)
	if err != nil {
		return nil, err
	}
	if versions == nil {
		return []domain.ChapterVersion{}, nil
	}
	return versions, nil
}

// RestoreVersion 将章节正文回滚至指定历史版本快照（由 Store 先归档当前正文至 restore_backup），
// 并在书籍配置了导出目录时同步更新单向 Markdown 镜像。
func (s *StudioService) RestoreVersion(ctx context.Context, versionID int64) (domain.Chapter, error) {
	if err := validatePositiveID("versionId", versionID); err != nil {
		return domain.Chapter{}, err
	}
	st, err := s.rt.store()
	if err != nil {
		return domain.Chapter{}, err
	}
	chap, err := st.RestoreVersion(ctx, versionID)
	if err != nil {
		return domain.Chapter{}, err
	}
	if exp := s.rt.exporter(); exp != nil {
		book, bErr := st.GetBook(ctx, chap.BookID)
		if bErr != nil {
			return chap, bErr
		}
		if strings.TrimSpace(book.ExportDir) != "" {
			if _, expErr := exp.ExportChapter(ctx, chap.ID); expErr != nil {
				return chap, expErr
			}
		}
	}
	return chap, nil
}
