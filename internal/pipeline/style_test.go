package pipeline

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/linter"
	"nib/internal/llm"
)

type fakeStyleStore struct {
	*fakeStore
	profile domain.StyleProfile
	samples []domain.StyleSample
	rules   []domain.DeaiRule
	audit   domain.ChapterAudit
}

func newFakeStyleStore(exportDir string) *fakeStyleStore {
	base := newBaseStore(exportDir)
	providerID := int64(7)
	base.routes[domain.StageOutline] = domain.ModelRoute{
		Stage:       domain.StageOutline,
		ProviderID:  &providerID,
		ModelName:   "deepseek-chat",
		Temperature: 0.65,
		MaxTokens:   2048,
	}
	base.routes[domain.StageCritic] = domain.ModelRoute{
		Stage:       domain.StageCritic,
		ProviderID:  &providerID,
		ModelName:   "deepseek-reasoner",
		Temperature: 0.20,
		MaxTokens:   2048,
	}
	base.routes[domain.StagePolish] = domain.ModelRoute{
		Stage:       domain.StagePolish,
		ProviderID:  &providerID,
		ModelName:   "deepseek-chat",
		Temperature: 0.55,
		MaxTokens:   2048,
	}

	// 设置 3 段正文：P0 干净，P1 含高频 AI 套话，P2 干净。
	base.chapter.Content = linter.JoinParagraphs([]string{
		"夜雨砸在青石阶上，陈青左手按住阵盘，指节微微发白。",
		"王执事嘴角勾起一抹弧度，眼中闪过一抹寒芒，心中不由得暗自冷笑。",
		"阵旗轰然炸裂，三道雷光贴着地面直扑巷口。",
	})
	base.chapter.WordCount = domain.CountWords(base.chapter.Content)

	return &fakeStyleStore{
		fakeStore: base,
		profile:   domain.DefaultStyleProfile(base.book.ID),
		rules:     linter.DefaultRules(),
		audit: domain.ChapterAudit{
			ChapterID:      base.chapter.ID,
			LintIssues:     []domain.LintIssue{},
			CriticFindings: []domain.CriticFinding{},
			Diffs:          []domain.SurgicalDiff{},
			DeaiScore:      100,
		},
	}
}

func (s *fakeStyleStore) GetStyleProfile(_ context.Context, bookID int64) (domain.StyleProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.profile.BookID == 0 {
		return domain.DefaultStyleProfile(bookID), nil
	}
	return s.profile, nil
}

func (s *fakeStyleStore) SaveStyleProfile(_ context.Context, in domain.UpsertStyleProfileInput) (domain.StyleProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profile = domain.StyleProfile{
		ID:                  1,
		BookID:              in.BookID,
		AvgSentenceChars:    in.AvgSentenceChars,
		MaxParagraphChars:   in.MaxParagraphChars,
		DialogueRatioMin:    in.DialogueRatioMin,
		DialogueRatioMax:    in.DialogueRatioMax,
		PovRule:             in.PovRule,
		ToneDirectives:      in.ToneDirectives,
		ForbiddenTropesNote: in.ForbiddenTropesNote,
	}
	return s.profile, nil
}

func (s *fakeStyleStore) ListStyleSamples(_ context.Context, _ int64, sceneType domain.SceneType) ([]domain.StyleSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.StyleSample, 0, len(s.samples))
	for _, sp := range s.samples {
		if sceneType != "" && sp.SceneType != sceneType {
			continue
		}
		out = append(out, sp)
	}
	return out, nil
}

func (s *fakeStyleStore) UpsertStyleSample(_ context.Context, in domain.UpsertStyleSampleInput) (domain.StyleSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := domain.StyleSample{
		ID:        int64(len(s.samples) + 1),
		BookID:    in.BookID,
		SceneType: in.SceneType,
		Title:     in.Title,
		Excerpt:   in.Excerpt,
		CharCount: domain.CountWords(in.Excerpt),
	}
	s.samples = append(s.samples, sp)
	return sp, nil
}

func (s *fakeStyleStore) ListDeaiRules(_ context.Context, _ int64) ([]domain.DeaiRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.rules), nil
}

func (s *fakeStyleStore) GetChapterAudit(_ context.Context, chapterID int64) (domain.ChapterAudit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.audit.ChapterID != chapterID {
		return domain.ChapterAudit{
			ChapterID:      chapterID,
			LintIssues:     []domain.LintIssue{},
			CriticFindings: []domain.CriticFinding{},
			Diffs:          []domain.SurgicalDiff{},
			DeaiScore:      100,
		}, nil
	}
	return s.audit, nil
}

func (s *fakeStyleStore) SaveChapterAudit(_ context.Context, audit domain.ChapterAudit) (domain.ChapterAudit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if audit.ID == 0 {
		audit.ID = 1
	}
	s.audit = audit
	return s.audit, nil
}

func TestExtractStyleProfileWithLLMAndOfflineFallback(t *testing.T) {
	samples := []string{
		linter.JoinParagraphs([]string{
			"剑光乍起，陈青踏步欺身，铁剑直取王煞咽喉。",
			"“三百灵石，买你一条命。”陈青冷声道。",
			"雷纹在脚底炸开，护体罡气应声崩裂。",
		}),
	}

	t.Run("with LLM route enriches qualitative directives and samples", func(t *testing.T) {
		store := newFakeStyleStore("")
		streamer := &fakeLLM{
			fn: func(_ context.Context, _ domain.Provider, _ llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
				resp := "```json\n" + `{
					"pov_rule": "third_limited",
					"tone_directives": "冷峻短句，动作先于心理",
					"forbidden_tropes_note": "禁空洞形容词堆砌",
					"samples": [
						{"scene_type": "combat", "title": "快剑反击", "excerpt": "剑光乍起，陈青踏步欺身，铁剑直取王煞咽喉。"}
					]
				}` + "\n```"
				_ = onEvent(llm.StreamEvent{Delta: resp})
				return domain.TokenUsage{TotalTokens: 150}, nil
			},
		}

		runner := New(store, compiler.New(store), streamer, nil)
		profile, err := runner.ExtractStyleProfile(context.Background(), 1, samples)
		if err != nil {
			t.Fatalf("ExtractStyleProfile failed: %v", err)
		}
		if profile.ToneDirectives != "冷峻短句，动作先于心理" || profile.ForbiddenTropesNote != "禁空洞形容词堆砌" {
			t.Fatalf("unexpected profile directives: %+v", profile)
		}
		if len(store.samples) != 1 || store.samples[0].SceneType != domain.SceneCombat {
			t.Fatalf("expected combat style sample saved, got %+v", store.samples)
		}
	})

	t.Run("offline fallback succeeds when no LLM route is configured", func(t *testing.T) {
		store := newFakeStyleStore("")
		store.routes = map[domain.PipelineStage]domain.ModelRoute{}
		runner := New(store, compiler.New(store), &fakeLLM{}, nil)

		profile, err := runner.ExtractStyleProfile(context.Background(), 1, samples)
		if err != nil {
			t.Fatalf("expected offline ExtractStyleProfile fallback to succeed, got %v", err)
		}
		if profile.AvgSentenceChars <= 0 || profile.MaxParagraphChars <= 0 || profile.ToneDirectives == "" {
			t.Fatalf("unexpected offline profile: %+v", profile)
		}
		if len(store.samples) == 0 {
			t.Fatalf("expected heuristic scene samples extracted offline")
		}
	})
}

func TestLintChapterLocalAndRunCriticAuditWithRepair(t *testing.T) {
	store := newFakeStyleStore("")
	comp := compiler.New(store)

	calls := 0
	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			calls++
			if calls == 1 && !strings.Contains(req.Messages[0].Content, "[P1]") {
				t.Errorf("expected numbered paragraphs in initial Critic prompt")
			}
			if calls == 1 {
				// 首轮故意输出损坏 JSON，测试 1 次自动纠错重问。
				_ = onEvent(llm.StreamEvent{Delta: `{"hook_passed": false, broken`})
				return domain.TokenUsage{}, nil
			}
			_ = onEvent(llm.StreamEvent{Delta: `{
				"hook_passed": true,
				"hook_verdict": "结尾雷光炸裂直扑巷口，准确兑现突发危机钩子",
				"critic_findings": [
					{
						"dimension": "consistency",
						"severity": "error",
						"paragraph_index": 1,
						"title": "王执事神态描写套路化",
						"detail": "第2段连续堆叠嘴角勾起与寒芒套话",
						"suggestion": "改为直接捏碎阴雷珠的动作"
					}
				]
			}`})
			return domain.TokenUsage{TotalTokens: 320}, nil
		},
	}

	runner := New(store, comp, streamer, nil)

	// 1. 本地 0 成本扫描应检出 P1 中的套话。
	localAudit, err := runner.LintChapterLocal(context.Background(), 3)
	if err != nil {
		t.Fatalf("LintChapterLocal failed: %v", err)
	}
	if len(localAudit.LintIssues) < 2 || localAudit.DeaiScore >= 100 {
		t.Fatalf("expected P1 AI clichés detected locally, got issues=%+v score=%d", localAudit.LintIssues, localAudit.DeaiScore)
	}

	// 2. Critic 盲审（含 1 次自动纠错重试）应合并本地 LintIssues 与 CriticFindings。
	criticAudit, err := runner.RunCriticAudit(context.Background(), 3)
	if err != nil {
		t.Fatalf("RunCriticAudit failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 LLM calls (initial + 1 self-repair), got %d", calls)
	}
	if !criticAudit.HookPassed || !strings.Contains(criticAudit.HookVerdict, "突发危机") {
		t.Fatalf("unexpected hook verdict: passed=%v verdict=%q", criticAudit.HookPassed, criticAudit.HookVerdict)
	}
	if len(criticAudit.CriticFindings) != 1 || criticAudit.CriticFindings[0].ParagraphIndex != 1 {
		t.Fatalf("unexpected critic findings: %+v", criticAudit.CriticFindings)
	}
	if len(criticAudit.LintIssues) < 2 {
		t.Fatalf("expected local lint issues preserved in critic audit")
	}
}

func TestGenerateAndApplySurgicalDiffs(t *testing.T) {
	store := newFakeStyleStore(`E:\Exports\Nib`)
	comp := compiler.New(store)
	exp := &fakeExporter{}

	var polishPrompt string
	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			polishPrompt = req.Messages[0].Content
			_ = onEvent(llm.StreamEvent{Delta: `{
				"diffs": [
					{
						"paragraph_index": 1,
						"reason": "消除嘴角勾起与眼中寒芒高频套话",
						"replacement": "王执事五指骤然收拢，掌心阴雷珠发出刺耳的电流爆鸣：“把阵盘交出来。”"
					}
				]
			}`})
			return domain.TokenUsage{TotalTokens: 210}, nil
		},
	}

	runner := New(store, comp, streamer, exp)

	// 1. 生成手术润色建议（自动定位被 Lint 标红的 P1，且只打包 [P0, P1, P2] 局部上下文）。
	audit, err := runner.GenerateSurgicalDiffs(context.Background(), 3, nil)
	if err != nil {
		t.Fatalf("GenerateSurgicalDiffs failed: %v", err)
	}
	if !strings.Contains(polishPrompt, "目标段落 [P1]") ||
		!strings.Contains(polishPrompt, "前一段(prevPara)") ||
		!strings.Contains(polishPrompt, "后一段(nextPara)") {
		t.Fatalf("expected [prevPara, targetPara, nextPara] window in polish prompt:\n%s", polishPrompt)
	}
	if len(audit.Diffs) != 1 || audit.Diffs[0].ParagraphIndex != 1 || audit.Diffs[0].Status != domain.DiffStatusPending {
		t.Fatalf("unexpected generated diffs: %+v", audit.Diffs)
	}
	diffID := audit.Diffs[0].ID

	// 2. 采纳该条手术润色：必须先将原正文归档至 chapter_versions(source='surgical_polish')，仅替换 P1，重算 DeaiScore=100 并触发导出。
	origChapterContent := store.chapter.Content
	res, err := runner.ApplySurgicalDiff(context.Background(), domain.ApplySurgicalDiffInput{
		ChapterID: 3,
		DiffID:    diffID,
		Accept:    true,
	})
	if err != nil {
		t.Fatalf("ApplySurgicalDiff(accept) failed: %v", err)
	}
	if len(store.versions) != 1 ||
		store.versions[0].Source != domain.VersionSourceSurgicalPolish ||
		store.versions[0].Content != origChapterContent {
		t.Fatalf("expected pre-polish snapshot archived with source=surgical_polish, got %+v", store.versions)
	}

	updatedParas := linter.SplitParagraphs(res.Chapter.Content)
	if len(updatedParas) != 3 {
		t.Fatalf("expected 3 paragraphs preserved, got %d", len(updatedParas))
	}
	if updatedParas[0] != "夜雨砸在青石阶上，陈青左手按住阵盘，指节微微发白。" ||
		updatedParas[2] != "阵旗轰然炸裂，三道雷光贴着地面直扑巷口。" {
		t.Fatalf("non-target paragraphs P0/P2 must remain untouched: %+v", updatedParas)
	}
	if !strings.Contains(updatedParas[1], "掌心阴雷珠发出刺耳的电流爆鸣") {
		t.Fatalf("target paragraph P1 was not replaced: %q", updatedParas[1])
	}
	if res.Audit.DeaiScore != 100 || len(res.Audit.LintIssues) != 0 {
		t.Fatalf("expected DeaiScore=100 after replacing P1 clichés, got score=%d issues=%+v", res.Audit.DeaiScore, res.Audit.LintIssues)
	}
	if res.Audit.Diffs[0].Status != domain.DiffStatusAccepted {
		t.Fatalf("expected diff status=accepted, got %q", res.Audit.Diffs[0].Status)
	}
	if exp.calls != 1 {
		t.Fatalf("expected exporter triggered on surgical polish accept, calls=%d", exp.calls)
	}

	// 3. 测试忽略分支（Accept=false）：不改动正文，仅将状态置为 ignored。
	store.audit.Diffs[0].Status = domain.DiffStatusPending
	contentBeforeIgnore := store.chapter.Content
	ignoreRes, err := runner.ApplySurgicalDiff(context.Background(), domain.ApplySurgicalDiffInput{
		ChapterID: 3,
		DiffID:    diffID,
		Accept:    false,
	})
	if err != nil {
		t.Fatalf("ApplySurgicalDiff(ignore) failed: %v", err)
	}
	if ignoreRes.Chapter.Content != contentBeforeIgnore {
		t.Fatalf("chapter content must not change when diff is ignored")
	}
	if ignoreRes.Audit.Diffs[0].Status != domain.DiffStatusIgnored {
		t.Fatalf("expected diff status=ignored, got %q", ignoreRes.Audit.Diffs[0].Status)
	}
	if _, err := runner.ApplySurgicalDiff(context.Background(), domain.ApplySurgicalDiffInput{
		ChapterID: 3,
		DiffID:    "non-existent",
		Accept:    true,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on unknown diffID, got %v", err)
	}
}
