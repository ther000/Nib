package app

import (
	"context"
	"strings"

	"nib/internal/domain"
)

// StyleService 提供 Phase 3 文风指纹（Style DNA）、分场景样章、0 成本本地 AI 味检测、Critic 盲审与手术式局部润色的桌面 RPC 绑定。
type StyleService struct {
	rt *Runtime
}

// NewStyleService 构造绑定到共享 Runtime 的 StyleService。
func NewStyleService(rt *Runtime) *StyleService {
	return &StyleService{rt: rt}
}

// GetStyleProfile 获取指定书籍的量化文风指纹档案。
func (s *StyleService) GetStyleProfile(ctx context.Context, bookID int64) (domain.StyleProfile, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.StyleProfile{}, err
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return domain.StyleProfile{}, err
	}
	return ss.GetStyleProfile(ctx, bookID)
}

// SaveStyleProfile 校验并保存指定书籍的量化文风指纹档案。
func (s *StyleService) SaveStyleProfile(ctx context.Context, in domain.UpsertStyleProfileInput) (domain.StyleProfile, error) {
	if err := in.Validate(); err != nil {
		return domain.StyleProfile{}, err
	}
	if in.AvgSentenceChars == 0 {
		in.AvgSentenceChars = domain.DefaultAvgSentenceChars
	}
	if in.MaxParagraphChars == 0 {
		in.MaxParagraphChars = domain.DefaultMaxParagraphChars
	}
	if in.DialogueRatioMin == 0 && in.DialogueRatioMax == 0 {
		in.DialogueRatioMin = domain.DefaultDialogueRatioMin
		in.DialogueRatioMax = domain.DefaultDialogueRatioMax
	}
	if in.PovRule == "" {
		in.PovRule = domain.PovThirdLimited
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return domain.StyleProfile{}, err
	}
	return ss.SaveStyleProfile(ctx, in)
}

// ExtractStyleProfile 从导入的 1~N 段样章文本中提取量化文风指纹并持久化。
func (s *StyleService) ExtractStyleProfile(ctx context.Context, bookID int64, sampleTexts []string) (domain.StyleProfile, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return domain.StyleProfile{}, err
	}
	var cleaned []string
	for _, txt := range sampleTexts {
		if trimmed := strings.TrimSpace(txt); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return domain.StyleProfile{}, &domain.ValidationError{
			Field:   "sampleTexts",
			Message: "请至少提供一段非空样章文本",
		}
	}
	eng, engCtx, release, err := s.rt.trackStyleEngine(ctx)
	if err != nil {
		return domain.StyleProfile{}, err
	}
	defer release()
	return eng.ExtractStyleProfile(engCtx, bookID, cleaned)
}

// ListStyleSamples 按书籍与可选场景类型（"" / "combat" / "dialogue" / "daily" / "suspense"）查询分场景样章库。
func (s *StyleService) ListStyleSamples(ctx context.Context, bookID int64, sceneType string) ([]domain.StyleSample, error) {
	if err := validatePositiveID("bookId", bookID); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(sceneType)
	var sceneFilter domain.SceneType
	if trimmed != "" {
		sceneFilter = domain.SceneType(trimmed)
		if !sceneFilter.Valid() {
			return nil, &domain.ValidationError{Field: "sceneType", Message: "无效的场景类型"}
		}
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return nil, err
	}
	samples, err := ss.ListStyleSamples(ctx, bookID, sceneFilter)
	if err != nil {
		return nil, err
	}
	if samples == nil {
		return []domain.StyleSample{}, nil
	}
	return samples, nil
}

// UpsertStyleSample 新增或编辑单条分场景样章锚点。
func (s *StyleService) UpsertStyleSample(ctx context.Context, in domain.UpsertStyleSampleInput) (domain.StyleSample, error) {
	if err := in.Validate(); err != nil {
		return domain.StyleSample{}, err
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return domain.StyleSample{}, err
	}
	return ss.UpsertStyleSample(ctx, in)
}

// DeleteStyleSample 删除指定的分场景样章锚点。
func (s *StyleService) DeleteStyleSample(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return err
	}
	return ss.DeleteStyleSample(ctx, id)
}

// ListDeaiRules 查询去 AI 味黑名单与结构规则（bookID=0 返回全局内置规则，bookID>0 返回合并后的有效规则列表）。
func (s *StyleService) ListDeaiRules(ctx context.Context, bookID int64) ([]domain.DeaiRule, error) {
	if bookID < 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 不能为负数"}
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return nil, err
	}
	rules, err := ss.ListDeaiRules(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if rules == nil {
		return []domain.DeaiRule{}, nil
	}
	return rules, nil
}

// UpsertDeaiRule 新增或更新去 AI 味规则（支持开关内置规则或添加自定义词条/正则）。
func (s *StyleService) UpsertDeaiRule(ctx context.Context, in domain.UpsertDeaiRuleInput) (domain.DeaiRule, error) {
	if err := in.Validate(); err != nil {
		return domain.DeaiRule{}, err
	}
	if in.Severity == "" {
		in.Severity = domain.LintSeverityWarn
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return domain.DeaiRule{}, err
	}
	return ss.UpsertDeaiRule(ctx, in)
}

// DeleteDeaiRule 删除指定的去 AI 味规则。
func (s *StyleService) DeleteDeaiRule(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return err
	}
	return ss.DeleteDeaiRule(ctx, id)
}

// ListRewritePresets 查询划词微调常用指令预设（bookID=0 返回全局，bookID>0 返回全局+本书预设）。
func (s *StyleService) ListRewritePresets(ctx context.Context, bookID int64) ([]domain.RewritePreset, error) {
	if bookID < 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 不能为负数"}
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return nil, err
	}
	presets, err := ss.ListRewritePresets(ctx, bookID)
	if err != nil {
		return nil, err
	}
	if presets == nil {
		return []domain.RewritePreset{}, nil
	}
	return presets, nil
}

// UpsertRewritePreset 新增或更新划词微调常用指令预设。
func (s *StyleService) UpsertRewritePreset(ctx context.Context, in domain.UpsertRewritePresetInput) (domain.RewritePreset, error) {
	if err := in.Validate(); err != nil {
		return domain.RewritePreset{}, err
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return domain.RewritePreset{}, err
	}
	return ss.UpsertRewritePreset(ctx, in)
}

// DeleteRewritePreset 删除指定的划词微调常用指令预设。
func (s *StyleService) DeleteRewritePreset(ctx context.Context, id int64) error {
	if err := validatePositiveID("id", id); err != nil {
		return err
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return err
	}
	return ss.DeleteRewritePreset(ctx, id)
}

// GetChapterAudit 读取指定章节最近一次持久化的质检与手术润色报告。
func (s *StyleService) GetChapterAudit(ctx context.Context, chapterID int64) (domain.ChapterAudit, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return domain.ChapterAudit{}, err
	}
	ss, err := s.rt.styleStore()
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	audit, err := ss.GetChapterAudit(ctx, chapterID)
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	normalizeChapterAudit(&audit)
	return audit, nil
}

// LintChapterLocal 对指定章节执行 0 成本本地确定性 AI 味扫描（<20ms 返回定位告警与去 AI 味得分）。
func (s *StyleService) LintChapterLocal(ctx context.Context, chapterID int64) (domain.ChapterAudit, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return domain.ChapterAudit{}, err
	}
	eng, err := s.rt.styleEngine()
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	audit, err := eng.LintChapterLocal(ctx, chapterID)
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	normalizeChapterAudit(&audit)
	return audit, nil
}

// RunCriticAudit 调用 critic 模型路由对照作战简报与章节正文执行 4 维独立盲审（一致性、男频毒点、节拍落点、章末钩子）。
func (s *StyleService) RunCriticAudit(ctx context.Context, chapterID int64) (domain.ChapterAudit, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return domain.ChapterAudit{}, err
	}
	eng, engCtx, release, err := s.rt.trackStyleEngine(ctx)
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	defer release()

	audit, err := eng.RunCriticAudit(engCtx, chapterID)
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	normalizeChapterAudit(&audit)
	return audit, nil
}

// GenerateSurgicalDiffs 仅针对指定的病灶段落（或为空时自动选取本地 Lint + Critic 点名段落）连同上下文 [prev, target, next] 生成手术式局部替换建议。
func (s *StyleService) GenerateSurgicalDiffs(ctx context.Context, chapterID int64, targetParagraphs []int) (domain.ChapterAudit, error) {
	if err := validatePositiveID("chapterId", chapterID); err != nil {
		return domain.ChapterAudit{}, err
	}
	for _, idx := range targetParagraphs {
		if idx < 0 {
			return domain.ChapterAudit{}, &domain.ValidationError{
				Field:   "targetParagraphs",
				Message: "段落索引不能为负数",
			}
		}
	}
	eng, engCtx, release, err := s.rt.trackStyleEngine(ctx)
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	defer release()

	audit, err := eng.GenerateSurgicalDiffs(engCtx, chapterID, targetParagraphs)
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	normalizeChapterAudit(&audit)
	return audit, nil
}

// ApplySurgicalDiff 采纳或忽略单条手术式局部润色建议（采纳时先落 surgical_polish 版本快照再原位替换段落，并同步单向 Markdown 导出）。
func (s *StyleService) ApplySurgicalDiff(ctx context.Context, in domain.ApplySurgicalDiffInput) (domain.ApplySurgicalDiffResult, error) {
	if err := in.Validate(); err != nil {
		return domain.ApplySurgicalDiffResult{}, err
	}
	eng, err := s.rt.styleEngine()
	if err != nil {
		return domain.ApplySurgicalDiffResult{}, err
	}
	res, err := eng.ApplySurgicalDiff(ctx, in)
	if err != nil {
		return domain.ApplySurgicalDiffResult{}, err
	}
	normalizeChapterAudit(&res.Audit)

	// 若采纳了润色替换且配置了导出器与书籍导出目录，同步刷新单向 Markdown 镜像。
	if in.Accept {
		if expErr := s.rt.syncChapterMarkdownIfConfigured(ctx, res.Chapter); expErr != nil {
			return res, expErr
		}
	}
	return res, nil
}

func normalizeChapterAudit(a *domain.ChapterAudit) {
	if a.LintIssues == nil {
		a.LintIssues = []domain.LintIssue{}
	}
	if a.CriticFindings == nil {
		a.CriticFindings = []domain.CriticFinding{}
	}
	if a.Diffs == nil {
		a.Diffs = []domain.SurgicalDiff{}
	}
}
