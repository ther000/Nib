package pipeline

import (
	"context"
	"strings"
	"sync"
	"testing"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/llm"
)

type fakeConsistencyStore struct {
	*fakeStore
	muReports    sync.Mutex
	chapters     []domain.Chapter
	allEntities  []domain.Entity
	stateEvents  []domain.EntityStateEvent
	foreshadows  []domain.Foreshadowing
	deadlines    []domain.StoryDeadline
	arcSummaries []domain.ArcSummary
	savedReports []domain.BookConsistencyReport
}

func newFakeConsistencyStore() *fakeConsistencyStore {
	base := newBaseStore("")
	providerID := int64(7)
	base.routes[domain.StageCritic] = domain.ModelRoute{
		Stage:       domain.StageCritic,
		ProviderID:  &providerID,
		ModelName:   "deepseek-reasoner",
		Temperature: 0.20,
		MaxTokens:   4096,
	}
	base.book.GoldenFinger = "每日可吸收一丝混沌气，但不能连续两天吸收"
	ch1 := base.chapter
	ch1.GlobalSeq = 1
	ch1.Title = "启程"
	ch1.CausalSummary = "林渊首次尝试连续三天吸收混沌气，反而毫发无伤。"
	ch2 := domain.Chapter{
		ID:            base.chapter.ID + 1,
		BookID:        base.book.ID,
		VolumeID:      base.volume.ID,
		GlobalSeq:     2,
		Title:         "破境",
		CausalSummary: "林渊以炼气二层硬撼筑基长老不败。",
	}
	return &fakeConsistencyStore{
		fakeStore:   base,
		chapters:    []domain.Chapter{ch1, ch2},
		allEntities: []domain.Entity{{ID: 1, BookID: base.book.ID, Name: "林渊", Kind: domain.EntityKindCharacter}},
	}
}

func (s *fakeConsistencyStore) ListBookChapters(_ context.Context, _ int64) ([]domain.Chapter, error) {
	return s.chapters, nil
}

func (s *fakeConsistencyStore) ListChapters(_ context.Context, _ int64) ([]domain.Chapter, error) {
	return s.chapters, nil
}

func (s *fakeConsistencyStore) ListEntities(_ context.Context, _ int64, _ domain.EntityFilter) ([]domain.Entity, error) {
	return s.allEntities, nil
}

func (s *fakeConsistencyStore) ListStateEvents(_ context.Context, _ int64, _ int64, _ int, _ bool) ([]domain.EntityStateEvent, error) {
	return s.stateEvents, nil
}

func (s *fakeConsistencyStore) ListForeshadowings(_ context.Context, _ int64, _ domain.ForeshadowingStatus) ([]domain.Foreshadowing, error) {
	return s.foreshadows, nil
}

func (s *fakeConsistencyStore) ListStoryDeadlines(_ context.Context, _ int64, _ domain.StoryDeadlineStatus) ([]domain.StoryDeadline, error) {
	return s.deadlines, nil
}

func (s *fakeConsistencyStore) ListArcSummaries(_ context.Context, _ int64, _ domain.ArcSummaryLevel) ([]domain.ArcSummary, error) {
	return s.arcSummaries, nil
}

func (s *fakeConsistencyStore) SaveConsistencyReport(_ context.Context, rep domain.BookConsistencyReport) (domain.BookConsistencyReport, error) {
	s.muReports.Lock()
	defer s.muReports.Unlock()
	rep.ID = int64(len(s.savedReports) + 1)
	s.savedReports = append(s.savedReports, rep)
	return rep, nil
}

func TestRunAIConsistencyAuditParseAndPersist(t *testing.T) {
	ctx := context.Background()
	st := newFakeConsistencyStore()
	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			// 验证 Prompt 中确实注入了金手指与章节摘要
			if len(req.Messages) == 0 || !strings.Contains(req.Messages[0].Content, "不能连续两天吸收") {
				t.Fatalf("expected golden finger in prompt")
			}
			payload := "```json\n" + `{
  "summary": "发现 1 处金手指违例与 1 处战力跨阶过猛隐患。",
  "findings": [
    {
      "category": "rule_contradiction",
      "severity": "error",
      "title": "金手指连续吸收限制自相矛盾",
      "description": "金手指设定不能连续两天吸收混沌气，但第1章连吸三天无反噬。",
      "evidence": "核心金手指 vs 第1章因果摘要",
      "suggestion": "补充触发代价或调整金手指冷却条款。",
      "chapter_seqs": [1],
      "entity_names": ["林渊"]
    },
    {
      "category": "power_collapse",
      "severity": "warn",
      "title": "炼气抗衡筑基战力跨度过大",
      "description": "第2章炼气二层正面硬撼筑基长老不败。",
      "evidence": "第2章",
      "suggestion": "增加外力介入或环境限制。",
      "chapter_seqs": [2],
      "entity_names": ["林渊"]
    }
  ]
}` + "\n```"
			_ = onEvent(llm.StreamEvent{Delta: payload})
			return domain.TokenUsage{PromptTokens: 200, CompletionTokens: 120, TotalTokens: 320}, nil
		},
	}

	runner := New(st, compiler.New(st), streamer, nil)

	// 无效参数拦截
	if _, err := runner.RunAIConsistencyAudit(ctx, domain.RunAIConsistencyAuditRequest{
		BookID:    st.book.ID,
		ScopeType: domain.ConsistencyScopeRange,
		StartSeq:  5,
		EndSeq:    2,
	}); err == nil {
		t.Fatalf("expected validation error when endSeq < startSeq")
	}

	rep, err := runner.RunAIConsistencyAudit(ctx, domain.RunAIConsistencyAuditRequest{
		BookID:    st.book.ID,
		ScopeType: domain.ConsistencyScopeVolume,
		VolumeID:  st.volume.ID,
	})
	if err != nil {
		t.Fatalf("RunAIConsistencyAudit: %v", err)
	}
	if rep.ID != 1 || rep.StartSeq != 1 || rep.EndSeq != 2 || len(rep.Findings) != 2 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	if rep.Findings[0].Category != "rule_contradiction" || rep.Findings[0].Status != domain.ConsistencyFindingOpen {
		t.Fatalf("unexpected finding 0: %+v", rep.Findings[0])
	}
}
