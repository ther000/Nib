package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"nib/internal/domain"
	"nib/internal/genres"
	"nib/internal/linter"
	"nib/internal/llm"
)

const (
	defaultCriticMaxTokens   = 2048
	defaultPolishMaxTokens   = 2048
	maxSurgicalTargetParas   = 8
	hookVerifyTailRunes      = 200
	maxStyleRepairPreviewLen = 4000
)

// StyleStore 定义 Phase 3 文风指纹、样章锚点、去 AI 味规则与质检报告持久化契约（直接由 *store.Store 满足）。
type StyleStore interface {
	GetStyleProfile(ctx context.Context, bookID int64) (domain.StyleProfile, error)
	SaveStyleProfile(ctx context.Context, in domain.UpsertStyleProfileInput) (domain.StyleProfile, error)
	ListStyleSamples(ctx context.Context, bookID int64, sceneType domain.SceneType) ([]domain.StyleSample, error)
	UpsertStyleSample(ctx context.Context, in domain.UpsertStyleSampleInput) (domain.StyleSample, error)
	ListDeaiRules(ctx context.Context, bookID int64) ([]domain.DeaiRule, error)
	GetChapterAudit(ctx context.Context, chapterID int64) (domain.ChapterAudit, error)
	SaveChapterAudit(ctx context.Context, audit domain.ChapterAudit) (domain.ChapterAudit, error)
}

func (r *Runner) requireStyleStore() (StyleStore, error) {
	if r == nil || r.store == nil || r.styleStore == nil {
		return nil, errors.New("pipeline style store not initialized")
	}
	return r.styleStore, nil
}

// ExtractStyleProfile 从作者导入的 1~5 章样章中提取量化 Style DNA（句长/段长/对话占比）与分场景样章锚点；
// 若已配置可用 LLM 路由（outline / draft / critic），还会进一步提炼叙事语调指令与禁区说明；未配置路由时平滑回退至本地确定性提取。
func (r *Runner) ExtractStyleProfile(ctx context.Context, bookID int64, sampleTexts []string) (domain.StyleProfile, error) {
	ss, err := r.requireStyleStore()
	if err != nil {
		return domain.StyleProfile{}, err
	}
	if bookID <= 0 {
		return domain.StyleProfile{}, fmt.Errorf("%w: bookId must be > 0", domain.ErrValidation)
	}
	cleanSamples := make([]string, 0, len(sampleTexts))
	for _, s := range sampleTexts {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			cleanSamples = append(cleanSamples, trimmed)
		}
	}
	if len(cleanSamples) == 0 {
		return domain.StyleProfile{}, fmt.Errorf("%w: 请至少提供一段非空样章文本", domain.ErrValidation)
	}

	book, err := r.store.GetBook(ctx, bookID)
	if err != nil {
		return domain.StyleProfile{}, fmt.Errorf("load book %d: %w", bookID, err)
	}

	existing, err := ss.GetStyleProfile(ctx, bookID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.StyleProfile{}, fmt.Errorf("get existing style profile: %w", err)
	}

	avgSent, maxPara, dlgMin, dlgMax := linter.ExtractQuantitativeDNA(cleanSamples)
	pov := existing.PovRule
	if !pov.Valid() {
		pov = domain.PovThirdLimited
	}
	tone := strings.TrimSpace(existing.ToneDirectives)
	forbidden := strings.TrimSpace(existing.ForbiddenTropesNote)

	excerptsToSave := linter.ExtractSceneExcerpts(bookID, cleanSamples)

	// 若配置了任一可用模型路由，尝试调用 LLM 提炼定性语调指令与精选场景样章；失败或未配路由则优雅回退本地提取。
	if route, provider, routeErr := r.resolveFirstAvailableStageRoute(ctx, domain.StageOutline, domain.StageDraft, domain.StageCritic, domain.StagePolish); routeErr == nil && r.llm != nil {
		if llmRes, llmErr := r.extractQualitativeStyleWithLLM(ctx, provider, route, book, cleanSamples); llmErr == nil {
			if llmRes.PovRule.Valid() {
				pov = llmRes.PovRule
			}
			if strings.TrimSpace(llmRes.ToneDirectives) != "" {
				tone = strings.TrimSpace(llmRes.ToneDirectives)
			}
			if strings.TrimSpace(llmRes.ForbiddenTropesNote) != "" {
				forbidden = strings.TrimSpace(llmRes.ForbiddenTropesNote)
			}
			if len(llmRes.Samples) > 0 {
				excerptsToSave = llmRes.Samples
			}
		} else if errors.Is(ctx.Err(), context.Canceled) {
			return domain.StyleProfile{}, domain.ErrGenerationCanceled
		}
	}

	if tone == "" {
		tone = fmt.Sprintf("保持平均句长约 %.0f 字、单段不超过 %d 字的短促推进节奏，动作与对话优先于抽象心理描写。", avgSent, maxPara)
	}

	for _, sampleIn := range excerptsToSave {
		sampleIn.BookID = bookID
		if sampleIn.Validate() == nil {
			_, _ = ss.UpsertStyleSample(ctx, sampleIn)
		}
	}

	saved, err := ss.SaveStyleProfile(ctx, domain.UpsertStyleProfileInput{
		BookID:              bookID,
		AvgSentenceChars:    avgSent,
		MaxParagraphChars:   maxPara,
		DialogueRatioMin:    dlgMin,
		DialogueRatioMax:    dlgMax,
		PovRule:             pov,
		ToneDirectives:      tone,
		ForbiddenTropesNote: forbidden,
	})
	if err != nil {
		return domain.StyleProfile{}, fmt.Errorf("save style profile: %w", err)
	}
	return saved, nil
}

// LintChapterLocal 对指定章节执行 0 成本本地确定性 AI 味与排版检测（<20ms），保留既有 CriticFindings 与 Diffs 并持久化质检报告。
func (r *Runner) LintChapterLocal(ctx context.Context, chapterID int64) (domain.ChapterAudit, error) {
	ss, err := r.requireStyleStore()
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	if chapterID <= 0 {
		return domain.ChapterAudit{}, fmt.Errorf("%w: chapterId must be > 0", domain.ErrValidation)
	}

	chap, err := r.store.GetChapter(ctx, chapterID)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("load chapter %d: %w", chapterID, err)
	}

	profile, rules, err := r.loadStyleProfileAndRules(ctx, ss, chap.BookID)
	if err != nil {
		return domain.ChapterAudit{}, err
	}

	issues, score := linter.Lint(chap.Content, rules, profile)

	audit, err := ss.GetChapterAudit(ctx, chapterID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.ChapterAudit{}, fmt.Errorf("load chapter audit: %w", err)
	}
	audit.ChapterID = chapterID
	audit.LintIssues = issues
	audit.DeaiScore = score
	if audit.CriticFindings == nil {
		audit.CriticFindings = []domain.CriticFinding{}
	}
	if audit.Diffs == nil {
		audit.Diffs = []domain.SurgicalDiff{}
	}

	saved, err := ss.SaveChapterAudit(ctx, audit)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("save chapter audit: %w", err)
	}
	return saved, nil
}

// RunCriticAudit 调用独立 Critic 模型（低温度 0.20）对照作战简报与章节正文执行 4 维盲审：
// 防吃书一致性（consistency）、男频毒点（poison_point）、节拍落点（beat_coverage）与章末 200 字钩子强检（hook）。
func (r *Runner) RunCriticAudit(ctx context.Context, chapterID int64) (domain.ChapterAudit, error) {
	ss, err := r.requireStyleStore()
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	if r.llm == nil || r.compiler == nil {
		return domain.ChapterAudit{}, errors.New("pipeline llm or compiler not initialized")
	}
	if chapterID <= 0 {
		return domain.ChapterAudit{}, fmt.Errorf("%w: chapterId must be > 0", domain.ErrValidation)
	}

	chap, err := r.store.GetChapter(ctx, chapterID)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("load chapter %d: %w", chapterID, err)
	}
	if strings.TrimSpace(chap.Content) == "" {
		return domain.ChapterAudit{}, fmt.Errorf("%w: 章节正文为空，无法执行 Critic 审校", domain.ErrValidation)
	}
	book, err := r.store.GetBook(ctx, chap.BookID)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("load book %d: %w", chap.BookID, err)
	}

	route, provider, err := r.resolveStageRouteAndProvider(ctx, domain.StageCritic)
	if err != nil {
		return domain.ChapterAudit{}, err
	}

	// 先跑一遍本地 0 成本 Lint，确保 LintIssues 与 DeaiScore 同步刷新。
	profile, rules, err := r.loadStyleProfileAndRules(ctx, ss, chap.BookID)
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	lintIssues, deaiScore := linter.Lint(chap.Content, rules, profile)

	briefing, err := r.compiler.Compile(ctx, chapterID, domain.BriefingOptions{IncludePending: true})
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("compile briefing for critic: %w", err)
	}

	paragraphs := linter.SplitParagraphs(chap.Content)
	sysPrompt, userPrompt := buildCriticPrompts(book, chap, briefing, paragraphs)

	rawResp, err := r.collectStageLLMResponse(ctx, chap.BookID, chap.ID, domain.StageCritic, provider, route, defaultCriticMaxTokens, sysPrompt, userPrompt)
	if err != nil {
		return domain.ChapterAudit{}, err
	}

	parsed, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		chap.BookID,
		chap.ID,
		domain.StageCritic,
		provider,
		route,
		defaultCriticMaxTokens,
		"Critic 审校报告（需包含 hook_passed、hook_verdict、critic_findings）",
		rawResp,
		func(raw string) (parsedCriticResult, error) {
			return parseCriticPayload(raw, len(paragraphs))
		},
	)
	if parseErr != nil {
		return domain.ChapterAudit{}, parseErr
	}

	audit, err := ss.GetChapterAudit(ctx, chapterID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.ChapterAudit{}, fmt.Errorf("load existing chapter audit: %w", err)
	}
	audit.ChapterID = chapterID
	audit.LintIssues = lintIssues
	audit.DeaiScore = deaiScore
	audit.CriticFindings = parsed.findings
	audit.HookPassed = parsed.hookPassed
	audit.HookVerdict = parsed.hookVerdict
	if audit.Diffs == nil {
		audit.Diffs = []domain.SurgicalDiff{}
	}

	saved, err := ss.SaveChapterAudit(ctx, audit)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("save critic chapter audit: %w", err)
	}
	return saved, nil
}

// GenerateSurgicalDiffs 严格遵循 docs/03-core-engines.md §3.4 的“手术式局部润色”原则：
// 绝不整章重写，仅将病灶段落及其前后各 1 段（[prevPara, targetPara, nextPara]）连同病因送给 polish 模型，输出逐段可采纳的局部替换建议。
func (r *Runner) GenerateSurgicalDiffs(ctx context.Context, chapterID int64, targetParagraphs []int) (domain.ChapterAudit, error) {
	ss, err := r.requireStyleStore()
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	if r.llm == nil {
		return domain.ChapterAudit{}, errors.New("pipeline llm not initialized")
	}
	if chapterID <= 0 {
		return domain.ChapterAudit{}, fmt.Errorf("%w: chapterId must be > 0", domain.ErrValidation)
	}

	chap, err := r.store.GetChapter(ctx, chapterID)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("load chapter %d: %w", chapterID, err)
	}
	if strings.TrimSpace(chap.Content) == "" {
		return domain.ChapterAudit{}, fmt.Errorf("%w: 章节正文为空，无法生成手术润色建议", domain.ErrValidation)
	}
	book, err := r.store.GetBook(ctx, chap.BookID)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("load book %d: %w", chap.BookID, err)
	}

	route, provider, err := r.resolveStageRouteAndProvider(ctx, domain.StagePolish)
	if err != nil {
		return domain.ChapterAudit{}, err
	}

	paragraphs := linter.SplitParagraphs(chap.Content)
	if len(paragraphs) == 0 {
		return domain.ChapterAudit{}, fmt.Errorf("%w: 章节无有效段落", domain.ErrValidation)
	}

	profile, rules, err := r.loadStyleProfileAndRules(ctx, ss, chap.BookID)
	if err != nil {
		return domain.ChapterAudit{}, err
	}
	lintIssues, deaiScore := linter.Lint(chap.Content, rules, profile)

	audit, err := ss.GetChapterAudit(ctx, chapterID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.ChapterAudit{}, fmt.Errorf("load chapter audit: %w", err)
	}
	audit.ChapterID = chapterID
	audit.LintIssues = lintIssues
	audit.DeaiScore = deaiScore
	if audit.CriticFindings == nil {
		audit.CriticFindings = []domain.CriticFinding{}
	}

	targets, reasonsByPara := selectSurgicalTargets(len(paragraphs), targetParagraphs, audit.LintIssues, audit.CriticFindings)
	if len(targets) == 0 {
		if audit.Diffs == nil {
			audit.Diffs = []domain.SurgicalDiff{}
		}
		return ss.SaveChapterAudit(ctx, audit)
	}

	sysPrompt, userPrompt := buildSurgicalPolishPrompts(book, profile, rules, paragraphs, targets, reasonsByPara)

	rawResp, err := r.collectStageLLMResponse(ctx, chap.BookID, chap.ID, domain.StagePolish, provider, route, defaultPolishMaxTokens, sysPrompt, userPrompt)
	if err != nil {
		return domain.ChapterAudit{}, err
	}

	newDiffs, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		chap.BookID,
		chap.ID,
		domain.StagePolish,
		provider,
		route,
		defaultPolishMaxTokens,
		"手术式局部润色建议（需包含 diffs 数组：paragraph_index, reason, replacement）",
		rawResp,
		func(raw string) ([]domain.SurgicalDiff, error) {
			return parseSurgicalDiffsPayload(raw, paragraphs, targets, reasonsByPara)
		},
	)
	if parseErr != nil {
		return domain.ChapterAudit{}, parseErr
	}

	// 将新生成的目标段落 Diff 与既有其它段落的 Diff 合并（同段落覆写为最新 pending 建议）。
	updatedByPara := make(map[int]domain.SurgicalDiff, len(newDiffs))
	for _, d := range newDiffs {
		updatedByPara[d.ParagraphIndex] = d
	}
	mergedDiffs := make([]domain.SurgicalDiff, 0, len(audit.Diffs)+len(newDiffs))
	for _, existingDiff := range audit.Diffs {
		if _, replaced := updatedByPara[existingDiff.ParagraphIndex]; replaced {
			continue
		}
		mergedDiffs = append(mergedDiffs, existingDiff)
	}
	mergedDiffs = append(mergedDiffs, newDiffs...)
	slices.SortFunc(mergedDiffs, func(a, b domain.SurgicalDiff) int {
		return a.ParagraphIndex - b.ParagraphIndex
	})
	audit.Diffs = mergedDiffs

	saved, err := ss.SaveChapterAudit(ctx, audit)
	if err != nil {
		return domain.ChapterAudit{}, fmt.Errorf("save surgical diffs audit: %w", err)
	}
	return saved, nil
}

// ApplySurgicalDiff 采纳或忽略单条手术式局部润色建议。
// 采纳时严格遵循 AGENTS.md §4.1：通过 SaveChapterContent(source='surgical_polish') 先将当前正文归档至 chapter_versions，
// 再仅替换目标病灶段（绝不触碰其它好段落），并立即重跑本地 AI 味扫描更新评分。
func (r *Runner) ApplySurgicalDiff(ctx context.Context, in domain.ApplySurgicalDiffInput) (domain.ApplySurgicalDiffResult, error) {
	ss, err := r.requireStyleStore()
	if err != nil {
		return domain.ApplySurgicalDiffResult{}, err
	}
	if err := in.Validate(); err != nil {
		return domain.ApplySurgicalDiffResult{}, err
	}

	chap, err := r.store.GetChapter(ctx, in.ChapterID)
	if err != nil {
		return domain.ApplySurgicalDiffResult{}, fmt.Errorf("load chapter %d: %w", in.ChapterID, err)
	}
	audit, err := ss.GetChapterAudit(ctx, in.ChapterID)
	if err != nil {
		return domain.ApplySurgicalDiffResult{}, fmt.Errorf("load chapter audit %d: %w", in.ChapterID, err)
	}

	diffIdx := -1
	for i, d := range audit.Diffs {
		if d.ID == in.DiffID {
			diffIdx = i
			break
		}
	}
	if diffIdx < 0 {
		return domain.ApplySurgicalDiffResult{}, fmt.Errorf("%w: surgical diff %q not found", domain.ErrNotFound, in.DiffID)
	}

	targetDiff := audit.Diffs[diffIdx]

	if !in.Accept {
		audit.Diffs[diffIdx].Status = domain.DiffStatusIgnored
		savedAudit, saveErr := ss.SaveChapterAudit(ctx, audit)
		if saveErr != nil {
			return domain.ApplySurgicalDiffResult{}, fmt.Errorf("save ignored diff status: %w", saveErr)
		}
		return domain.ApplySurgicalDiffResult{
			Chapter: chap,
			Audit:   savedAudit,
		}, nil
	}

	replacement := strings.TrimSpace(in.CustomReplacement)
	if replacement == "" {
		replacement = strings.TrimSpace(targetDiff.Replacement)
	}
	if replacement == "" {
		return domain.ApplySurgicalDiffResult{}, fmt.Errorf("%w: 替换文本不能为空", domain.ErrValidation)
	}

	paragraphs := linter.SplitParagraphs(chap.Content)
	replaceParaIdx := resolveTargetParagraphIndex(paragraphs, targetDiff)
	if replaceParaIdx < 0 {
		return domain.ApplySurgicalDiffResult{}, fmt.Errorf("%w: 无法在当前正文中定位目标段落（P%d）", domain.ErrConflict, targetDiff.ParagraphIndex)
	}

	paragraphs[replaceParaIdx] = replacement
	newContent := linter.JoinParagraphs(paragraphs)

	savedChap, err := r.store.SaveChapterContent(
		ctx,
		chap.ID,
		newContent,
		chap.Status,
		domain.VersionSourceSurgicalPolish,
		"",
		chap.LastBriefingJSON,
	)
	if err != nil {
		return domain.ApplySurgicalDiffResult{}, fmt.Errorf("save surgical polish chapter content: %w", err)
	}

	// 若书籍配置了导出目录且 Runner 持有 exporter，尽力同步单向 Markdown 镜像。
	if r.exporter != nil {
		if book, bErr := r.store.GetBook(ctx, savedChap.BookID); bErr == nil && strings.TrimSpace(book.ExportDir) != "" {
			if vol, vErr := r.store.GetVolume(ctx, savedChap.VolumeID); vErr == nil {
				_ = r.tryExportChapter(book, vol, savedChap)
			}
		}
	}

	audit.Diffs[diffIdx].Replacement = replacement
	audit.Diffs[diffIdx].Status = domain.DiffStatusAccepted

	profile, rules, err := r.loadStyleProfileAndRules(ctx, ss, savedChap.BookID)
	if err == nil {
		issues, score := linter.Lint(savedChap.Content, rules, profile)
		audit.LintIssues = issues
		audit.DeaiScore = score
	}

	savedAudit, err := ss.SaveChapterAudit(ctx, audit)
	if err != nil {
		return domain.ApplySurgicalDiffResult{}, fmt.Errorf("save updated chapter audit: %w", err)
	}
	return domain.ApplySurgicalDiffResult{
		Chapter: savedChap,
		Audit:   savedAudit,
	}, nil
}

func resolveTargetParagraphIndex(paragraphs []string, diff domain.SurgicalDiff) int {
	origTrimmed := strings.TrimSpace(diff.Original)
	if diff.ParagraphIndex >= 0 && diff.ParagraphIndex < len(paragraphs) {
		if origTrimmed == "" || paragraphs[diff.ParagraphIndex] == origTrimmed {
			return diff.ParagraphIndex
		}
	}
	if origTrimmed != "" {
		for i, p := range paragraphs {
			if p == origTrimmed {
				return i
			}
		}
		// 原文不为空且在正文中找不到精确匹配时，绝不盲目覆盖原索引对应的新段落。
		return -1
	}
	return -1
}

func (r *Runner) loadStyleProfileAndRules(
	ctx context.Context,
	ss StyleStore,
	bookID int64,
) (domain.StyleProfile, []domain.DeaiRule, error) {
	profile, err := ss.GetStyleProfile(ctx, bookID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			profile = domain.DefaultStyleProfile(bookID)
		} else {
			return domain.StyleProfile{}, nil, fmt.Errorf("load style profile: %w", err)
		}
	}
	rules, err := ss.ListDeaiRules(ctx, bookID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.StyleProfile{}, nil, fmt.Errorf("load deai rules: %w", err)
	}
	if len(rules) == 0 {
		rules = linter.DefaultRules()
	}
	return profile, rules, nil
}

type qualitativeStyleLLMResult struct {
	PovRule             domain.PovRule
	ToneDirectives      string
	ForbiddenTropesNote string
	Samples             []domain.UpsertStyleSampleInput
}

func (r *Runner) extractQualitativeStyleWithLLM(
	ctx context.Context,
	provider domain.Provider,
	route domain.ModelRoute,
	book domain.Book,
	sampleTexts []string,
) (qualitativeStyleLLMResult, error) {
	sys := `你是男频网文文风指纹分析器。请根据作者提供的样章，提取叙事视角、语调风格规范、用词禁区，并从样章原文中摘取 1~4 段最具代表性的场景片段（combat/dialogue/daily/suspense）。
严格只输出合法 JSON 对象（禁止输出 Markdown 围栏或多余说明）：
{
  "pov_rule": "third_limited|first_person|third_omniscient",
  "tone_directives": "冷峻克制、动作先于心理等具体语感指令",
  "forbidden_tropes_note": "样章体现出的避雷规范",
  "samples": [
    {"scene_type": "combat|dialogue|daily|suspense", "title": "片段标题", "excerpt": "样章原文摘录"}
  ]
}`

	var user strings.Builder
	fmt.Fprintf(&user, "书名：《%s》（题材：%s）\n\n", book.Title, book.Genre)
	for i, s := range sampleTexts {
		runes := []rune(s)
		if len(runes) > 1500 {
			s = string(runes[:1500])
		}
		fmt.Fprintf(&user, "【样章 %d】\n%s\n\n", i+1, s)
	}

	raw, err := r.collectStageLLMResponse(ctx, book.ID, 0, domain.StageStyle, provider, route, defaultCriticMaxTokens, sys, user.String())
	if err != nil {
		return qualitativeStyleLLMResult{}, err
	}
	jsonStr, err := llm.ExtractJSONObject(raw)
	if err != nil {
		return qualitativeStyleLLMResult{}, err
	}
	var dto struct {
		PovRule             string `json:"pov_rule"`
		PovRuleCamel        string `json:"povRule"`
		ToneDirectives      string `json:"tone_directives"`
		ToneCamel           string `json:"toneDirectives"`
		ForbiddenTropesNote string `json:"forbidden_tropes_note"`
		ForbiddenCamel      string `json:"forbiddenTropesNote"`
		Samples             []struct {
			SceneType      string `json:"scene_type"`
			SceneTypeCamel string `json:"sceneType"`
			Title          string `json:"title"`
			Excerpt        string `json:"excerpt"`
		} `json:"samples"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return qualitativeStyleLLMResult{}, err
	}

	pov := domain.PovRule(strings.TrimSpace(dto.PovRule))
	if !pov.Valid() {
		pov = domain.PovRule(strings.TrimSpace(dto.PovRuleCamel))
	}
	tone := strings.TrimSpace(dto.ToneDirectives)
	if tone == "" {
		tone = strings.TrimSpace(dto.ToneCamel)
	}
	forbidden := strings.TrimSpace(dto.ForbiddenTropesNote)
	if forbidden == "" {
		forbidden = strings.TrimSpace(dto.ForbiddenCamel)
	}

	var samples []domain.UpsertStyleSampleInput
	for _, sp := range dto.Samples {
		sc := domain.SceneType(strings.TrimSpace(sp.SceneType))
		if !sc.Valid() {
			sc = domain.SceneType(strings.TrimSpace(sp.SceneTypeCamel))
		}
		if !sc.Valid() {
			sc = domain.SceneDaily
		}
		in := domain.UpsertStyleSampleInput{
			BookID:    book.ID,
			SceneType: sc,
			Title:     strings.TrimSpace(sp.Title),
			Excerpt:   strings.TrimSpace(sp.Excerpt),
		}
		if in.Title == "" {
			in.Title = fmt.Sprintf("样章示范·%s", sc.Label())
		}
		if in.Validate() == nil {
			samples = append(samples, in)
		}
	}

	return qualitativeStyleLLMResult{
		PovRule:             pov,
		ToneDirectives:      tone,
		ForbiddenTropesNote: forbidden,
		Samples:             samples,
	}, nil
}

func buildCriticPrompts(
	book domain.Book,
	chap domain.Chapter,
	briefing domain.Briefing,
	paragraphs []string,
) (string, string) {
	preset := genres.GetOrDefault(book.Genre)

	sys := `你是严苛的男频网文独立审校官（Critic）。请对照「本章作战简报」与「章节草稿（按段落编号 [P0]..[PN]）」，从以下 4 个维度进行盲审：
1. consistency（防吃书一致性）：核对人物境界、伤势、道具归属、资金数额与世界铁律是否前后冲突。
2. poison_point（男频毒点排查）：排查无动机降智送人头、主角圣母说教、信息差强行降智、数字量纲崩溃。
3. beat_coverage（节拍落点核验）：逐条核对节拍卡是否扎实写出，有无敷衍跳过核心反转。
4. hook（章末200字钩子强检）：单独核验最后 200 字是否真实兑现设定的章末钩子类型；若平淡收尾或鸡汤总结则判 hook_passed = false。

严格只输出一个合法 JSON 对象（禁止输出 Markdown 围栏或多余说明）：
{
  "hook_passed": true,
  "hook_verdict": "对章末200字钩子兑现情况的一句话判定",
  "critic_findings": [
    {
      "dimension": "consistency|poison_point|beat_coverage|hook",
      "severity": "info|warn|error",
      "paragraph_index": 0,
      "title": "问题简短标题",
      "detail": "具体冲突或毒点依据",
      "suggestion": "针对该段落的具体修改建议"
    }
  ]
}`

	var user strings.Builder
	fmt.Fprintf(&user, "【书籍与题材】《%s》｜题材：%s（%s）\n", book.Title, preset.Name, book.Genre)
	fmt.Fprintf(&user, "【本章钩子要求】类型：%s（%s）｜落点说明：%s\n\n", chap.HookType.Label(), chap.HookType, chap.HookNote)
	user.WriteString("【本章作战简报（设定快照与节拍要求）】\n")
	user.WriteString(briefing.SystemPrompt)
	user.WriteString("\n\n")
	user.WriteString(briefing.UserPrompt)
	user.WriteString("\n\n【待审校章节正文（按段落编号）】\n")
	for i, p := range paragraphs {
		fmt.Fprintf(&user, "[P%d] %s\n", i, p)
	}

	fullRunes := []rune(strings.TrimSpace(chap.Content))
	tailRunes := fullRunes
	if len(fullRunes) > hookVerifyTailRunes {
		tailRunes = fullRunes[len(fullRunes)-hookVerifyTailRunes:]
	}
	fmt.Fprintf(&user, "\n【章末最后 %d 字单独提取（供 hook 强检）】\n%s\n", len(tailRunes), string(tailRunes))
	return sys, user.String()
}

type parsedCriticResult struct {
	hookPassed  bool
	hookVerdict string
	findings    []domain.CriticFinding
}

func parseCriticPayload(raw string, paraCount int) (parsedCriticResult, error) {
	jsonStr, err := llm.ExtractJSONObject(raw)
	if err != nil {
		return parsedCriticResult{}, err
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &top); err != nil {
		return parsedCriticResult{}, err
	}
	_, hasFindings := top["critic_findings"]
	_, hasFindingsCamel := top["criticFindings"]
	_, hasHook := top["hook_passed"]
	_, hasHookCamel := top["hookPassed"]
	if !hasFindings && !hasFindingsCamel && !hasHook && !hasHookCamel {
		return parsedCriticResult{}, errors.New("JSON missing required critic fields (hook_passed / critic_findings)")
	}

	var dto struct {
		HookPassed     *bool `json:"hook_passed"`
		HookPassedAlt  *bool `json:"hookPassed"`
		HookVerdict    string `json:"hook_verdict"`
		HookVerdictAlt string `json:"hookVerdict"`
		Findings       []struct {
			Dimension      string `json:"dimension"`
			Severity       string `json:"severity"`
			ParagraphIndex int    `json:"paragraph_index"`
			ParaIdxCamel   *int   `json:"paragraphIndex"`
			Title          string `json:"title"`
			Detail         string `json:"detail"`
			Suggestion     string `json:"suggestion"`
		} `json:"critic_findings"`
		FindingsCamel []struct {
			Dimension      string `json:"dimension"`
			Severity       string `json:"severity"`
			ParagraphIndex int    `json:"paragraph_index"`
			ParaIdxCamel   *int   `json:"paragraphIndex"`
			Title          string `json:"title"`
			Detail         string `json:"detail"`
			Suggestion     string `json:"suggestion"`
		} `json:"criticFindings"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return parsedCriticResult{}, fmt.Errorf("unmarshal critic json: %w", err)
	}

	hookPassed := true
	if dto.HookPassed != nil {
		hookPassed = *dto.HookPassed
	} else if dto.HookPassedAlt != nil {
		hookPassed = *dto.HookPassedAlt
	}
	verdict := strings.TrimSpace(dto.HookVerdict)
	if verdict == "" {
		verdict = strings.TrimSpace(dto.HookVerdictAlt)
	}

	rawList := dto.Findings
	if len(rawList) == 0 {
		rawList = dto.FindingsCamel
	}

	findings := make([]domain.CriticFinding, 0, len(rawList))
	for _, item := range rawList {
		title := strings.TrimSpace(item.Title)
		detail := strings.TrimSpace(item.Detail)
		if title == "" && detail == "" {
			continue
		}
		if title == "" {
			title = detail
		}
		dim := domain.CriticDimension(strings.ToLower(strings.TrimSpace(item.Dimension)))
		if !dim.Valid() {
			dim = domain.CriticDimConsistency
		}
		sev := domain.LintSeverity(strings.ToLower(strings.TrimSpace(item.Severity)))
		if !sev.Valid() {
			sev = domain.LintSeverityWarn
		}
		pIdx := item.ParagraphIndex
		if item.ParaIdxCamel != nil {
			pIdx = *item.ParaIdxCamel
		}
		if pIdx < 0 {
			pIdx = 0
		} else if paraCount > 0 && pIdx >= paraCount {
			pIdx = paraCount - 1
		}

		findings = append(findings, domain.CriticFinding{
			ID:             fmt.Sprintf("critic-%d", len(findings)+1),
			Dimension:      dim,
			Severity:       sev,
			ParagraphIndex: pIdx,
			Title:          title,
			Detail:         detail,
			Suggestion:     strings.TrimSpace(item.Suggestion),
		})
	}

	return parsedCriticResult{
		hookPassed:  hookPassed,
		hookVerdict: verdict,
		findings:    findings,
	}, nil
}

func selectSurgicalTargets(
	paraCount int,
	explicitTargets []int,
	lintIssues []domain.LintIssue,
	criticFindings []domain.CriticFinding,
) ([]int, map[int]string) {
	reasonsByPara := make(map[int][]string)
	addReason := func(pIdx int, r string) {
		if pIdx < 0 || pIdx >= paraCount || strings.TrimSpace(r) == "" {
			return
		}
		reasonsByPara[pIdx] = append(reasonsByPara[pIdx], strings.TrimSpace(r))
	}

	for _, iss := range lintIssues {
		addReason(iss.ParagraphIndex, fmt.Sprintf("命中 AI 味/排版问题「%s」（%s）", iss.MatchedText, iss.Hint))
	}
	for _, cf := range criticFindings {
		msg := fmt.Sprintf("Critic[%s] %s：%s", cf.Dimension.Label(), cf.Title, cf.Detail)
		if cf.Suggestion != "" {
			msg += "（建议：" + cf.Suggestion + "）"
		}
		addReason(cf.ParagraphIndex, msg)
	}

	seen := make(map[int]bool)
	targets := make([]int, 0, maxSurgicalTargetParas)

	if len(explicitTargets) > 0 {
		for _, idx := range explicitTargets {
			if idx >= 0 && idx < paraCount && !seen[idx] {
				seen[idx] = true
				targets = append(targets, idx)
				if len(targets) >= maxSurgicalTargetParas {
					break
				}
			}
		}
	} else {
		for idx := range reasonsByPara {
			seen[idx] = true
			targets = append(targets, idx)
		}
		slices.Sort(targets)
		if len(targets) > maxSurgicalTargetParas {
			targets = targets[:maxSurgicalTargetParas]
		}
	}

	slices.Sort(targets)
	joinedReasons := make(map[int]string, len(targets))
	for _, idx := range targets {
		list := reasonsByPara[idx]
		if len(list) == 0 {
			joinedReasons[idx] = "精简 AI 套话与心理注水，强化动作与短句质感"
		} else {
			joinedReasons[idx] = strings.Join(list, "；")
		}
	}
	return targets, joinedReasons
}

func buildSurgicalPolishPrompts(
	book domain.Book,
	profile domain.StyleProfile,
	rules []domain.DeaiRule,
	paragraphs []string,
	targets []int,
	reasonsByPara map[int]string,
) (string, string) {
	sys := `你是男频网文手术式局部润色专家（Surgical Polish）。
核心铁律：
1. 绝不整章重写，绝不擅自增加原段落没有的剧情转折、新人物或新设定。
2. 仅针对每个目标段落（targetPara），结合其前一段（prevPara）与后一段（nextPara）的语境，消除指出的 AI 套话、对称排比、鸡汤升华、文字墙或吃书漏洞。
3. 严格贴合书籍 Style DNA（短句、动作与台词外化情绪、克制冷峻）。
4. 必须只输出一个合法 JSON 对象（禁止输出 Markdown 围栏或多余说明）：
{
  "diffs": [
    {
      "paragraph_index": 14,
      "reason": "精简后的修改原因",
      "replacement": "仅针对该目标段落改写后的完整替换段落正文"
    }
  ]
}`

	var user strings.Builder
	fmt.Fprintf(&user, "【书籍与文风约束】《%s》（%s）｜平均句长约 %.0f 字｜单段上限 %d 字\n",
		book.Title, book.Genre, profile.AvgSentenceChars, profile.MaxParagraphChars)
	if strings.TrimSpace(profile.ToneDirectives) != "" {
		fmt.Fprintf(&user, "语调要求：%s\n", strings.TrimSpace(profile.ToneDirectives))
	}

	banned := make([]string, 0, 10)
	for _, r := range rules {
		if r.Enabled && r.Category == domain.DeaiCategoryCliche && r.MaxPerChapter == 0 {
			banned = append(banned, r.Pattern)
			if len(banned) >= 10 {
				break
			}
		}
	}
	if len(banned) > 0 {
		fmt.Fprintf(&user, "严禁出现以下套话：%s\n", strings.Join(banned, "、"))
	}

	user.WriteString("\n【待手术润色的局部病灶段落切片（仅含前后各 1 段上下文）】\n")
	for _, pIdx := range targets {
		prevPara := "（无，本章首段）"
		if pIdx > 0 {
			prevPara = paragraphs[pIdx-1]
		}
		nextPara := "（无，本章末段）"
		if pIdx+1 < len(paragraphs) {
			nextPara = paragraphs[pIdx+1]
		}
		fmt.Fprintf(&user, "\n--- 目标段落 [P%d] ---\n", pIdx)
		fmt.Fprintf(&user, "病灶原因：%s\n", reasonsByPara[pIdx])
		fmt.Fprintf(&user, "前一段(prevPara)：%s\n", prevPara)
		fmt.Fprintf(&user, "目标段(targetPara [P%d])：%s\n", pIdx, paragraphs[pIdx])
		fmt.Fprintf(&user, "后一段(nextPara)：%s\n", nextPara)
	}
	return sys, user.String()
}

func parseSurgicalDiffsPayload(
	raw string,
	paragraphs []string,
	targets []int,
	reasonsByPara map[int]string,
) ([]domain.SurgicalDiff, error) {
	jsonStr, err := llm.ExtractJSONObject(raw)
	if err != nil {
		return nil, err
	}

	var dto struct {
		Diffs []struct {
			ID             string `json:"id"`
			ParagraphIndex *int   `json:"paragraph_index"`
			ParaIdxCamel   *int   `json:"paragraphIndex"`
			Reason         string `json:"reason"`
			Replacement    string `json:"replacement"`
		} `json:"diffs"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return nil, fmt.Errorf("unmarshal surgical diffs json: %w", err)
	}
	if len(dto.Diffs) == 0 {
		return nil, errors.New("JSON diffs array is empty or missing")
	}

	targetSet := make(map[int]bool, len(targets))
	for _, idx := range targets {
		targetSet[idx] = true
	}

	out := make([]domain.SurgicalDiff, 0, len(dto.Diffs))
	seenPara := make(map[int]bool)

	for i, item := range dto.Diffs {
		replacement := strings.TrimSpace(item.Replacement)
		if replacement == "" {
			continue
		}
		pIdx := -1
		if item.ParagraphIndex != nil {
			pIdx = *item.ParagraphIndex
		} else if item.ParaIdxCamel != nil {
			pIdx = *item.ParaIdxCamel
		} else if i < len(targets) {
			pIdx = targets[i]
		}
		if pIdx < 0 || pIdx >= len(paragraphs) || seenPara[pIdx] {
			continue
		}
		// 若模型返回了非请求段落且只有一个目标段，自动对齐到该目标段。
		if !targetSet[pIdx] && len(targets) == 1 && !seenPara[targets[0]] {
			pIdx = targets[0]
		}
		seenPara[pIdx] = true

		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			reason = reasonsByPara[pIdx]
		}
		out = append(out, domain.SurgicalDiff{
			ID:             fmt.Sprintf("diff-%d", pIdx),
			ParagraphIndex: pIdx,
			Reason:         reason,
			Original:       paragraphs[pIdx],
			Replacement:    replacement,
			Status:         domain.DiffStatusPending,
		})
	}

	if len(out) == 0 {
		return nil, errors.New("no valid paragraph replacement found in diffs response")
	}
	return out, nil
}

func buildStyleJSONRepairPrompts(targetSchemaDesc, brokenRaw string, parseErr error) (string, string) {
	runes := []rune(strings.TrimSpace(brokenRaw))
	preview := string(runes)
	if len(runes) > maxStyleRepairPreviewLen {
		preview = string(runes[:maxStyleRepairPreviewLen]) + "..."
	}
	sys := "你是 JSON 格式修复器。请将输入内容修复为一个严格合法的 JSON 对象并直接输出，绝不包含任何解释文字或 Markdown 代码围栏。"
	user := fmt.Sprintf(
		"你上一轮输出的 %s JSON 解析失败，报错为：%v。\n请修复并只返回合法 JSON 对象：\n\n原始内容：\n%s",
		targetSchemaDesc,
		parseErr,
		preview,
	)
	return sys, user
}
