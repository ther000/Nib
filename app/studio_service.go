package app

import (
	"context"

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
	runner, genCtx, cancel, release, err := s.rt.trackPipeline(ctx)
	if err != nil {
		return domain.Chapter{}, err
	}
	defer release()

	return runner.GenerateChapter(genCtx, chapterID, opts, channelEmitter(genCtx, cancel, ch))
}

// RewriteSelection 对编辑器选中的局部段落执行流式 AI 微调，返回改写预览文本（不直接改库）。
func (s *StudioService) RewriteSelection(
	ctx context.Context,
	req domain.RewriteSelectionRequest,
	ch *mygo.Channel[domain.GenChunk],
) (domain.RewriteSelectionResult, error) {
	if err := req.Validate(); err != nil {
		return domain.RewriteSelectionResult{}, err
	}
	runner, genCtx, cancel, release, err := s.rt.trackPipeline(ctx)
	if err != nil {
		return domain.RewriteSelectionResult{}, err
	}
	defer release()

	return runner.RewriteSelection(genCtx, req, channelEmitter(genCtx, cancel, ch))
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
	if err := s.rt.syncChapterMarkdownIfConfigured(ctx, chap); err != nil {
		return chap, err
	}
	return chap, nil
}
