package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"nib/internal/domain"
	"nib/internal/llm"
	"nib/internal/store"
)

type stubGenesisLLM struct {
	payload string
}

func (s *stubGenesisLLM) Stream(
	_ context.Context,
	_ domain.Provider,
	_ llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
) (domain.TokenUsage, error) {
	if onEvent != nil {
		if err := onEvent(llm.StreamEvent{Delta: s.payload}); err != nil {
			return domain.TokenUsage{}, err
		}
	}
	return domain.TokenUsage{PromptTokens: 120, CompletionTokens: 340, TotalTokens: 460}, nil
}

func TestGenerateAndApplyBookGenesis(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "genesis.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	prov, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "TestProvider",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-test-1234",
		Models:   []string{"test-model"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("upsert provider: %v", err)
	}
	if _, err := st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageDraft,
		ProviderID:  &prov.ID,
		ModelName:   "test-model",
		Temperature: 0.7,
		MaxTokens:   4096,
	}); err != nil {
		t.Fatalf("save draft route: %v", err)
	}

	jsonResp := `{
		"title": "三息残剑",
		"premise": "落魄杂役林渊捡到可回溯三息的残剑，在生死绝境中反杀仇敌。",
		"golden_finger": "残剑每十息可回溯三息时间，但每次使用会损耗气血。",
		"entities": [
			{"kind": "character", "name": "林渊", "aliases": ["渊哥"], "is_invariant": false, "summary": "外门杂役，隐忍果决", "attributes": {"境界": "炼气二层"}},
			{"kind": "character", "name": "赵无极", "aliases": [], "is_invariant": false, "summary": "内门执事，阴狠毒辣", "attributes": {"境界": "炼气六层"}},
			{"kind": "rule", "name": "灵气潮汐铁律", "aliases": [], "is_invariant": true, "summary": "筑基之下不可御空飞行", "attributes": {}}
		],
		"first_volume": {
			"title": "第一卷 断剑惊渊",
			"goal": "林渊借残剑在外门大比逆袭并手刃赵无极",
			"climax_plan": "外门生死台当众反杀赵无极，震动全宗"
		},
		"initial_outlines": [
			{
				"title": "绝崖拾剑",
				"emotion_tag": "suppress",
				"hook_type": "crisis",
				"hook_note": "赵无极带人堵住崖口",
				"outline_summary": "林渊被赵无极逼入断剑崖，濒死时激活残剑回溯能力。",
				"beats": [
					{"stage": "suppress", "summary": "林渊遭赵无极构陷被废灵根", "conflict_note": "实力悬殊", "target_words": 800},
					{"stage": "turn", "summary": "坠崖前林渊触碰残剑回溯三息避开致命一击", "conflict_note": "金手指首次发动", "target_words": 900}
				]
			},
			{
				"title": "三息反杀",
				"emotion_tag": "burst",
				"hook_type": "mystery",
				"hook_note": "残剑内传出古老剑灵低语",
				"outline_summary": "林渊借三息时间差反杀追兵，夺得灵石。",
				"beats": [
					{"stage": "burst", "summary": "林渊利用预判反杀赵无极手下", "conflict_note": "以弱胜强", "target_words": 1000}
				]
			}
		]
	}`

	runner := New(st, nil, &stubGenesisLLM{payload: jsonResp}, nil)

	var seenCompleted bool
	plan, err := runner.GenerateBookGenesis(ctx, domain.BookGenesisRequest{
		Idea:         "落魄剑修捡到能回溯三息的残剑，一路杀穿宗门",
		Genre:        domain.GenreXuanhuan,
		ChapterCount: 2,
	}, func(chunk domain.GenesisChunk) error {
		if chunk.Kind == domain.GenChunkCompleted && chunk.Plan != nil {
			seenCompleted = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateBookGenesis failed: %v", err)
	}
	if !seenCompleted {
		t.Fatalf("expected completed chunk with plan")
	}
	if plan.Title != "三息残剑" || len(plan.Entities) != 3 || len(plan.InitialOutlines) != 2 {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	res, err := runner.ApplyBookGenesis(ctx, domain.ApplyGenesisInput{Plan: plan})
	if err != nil {
		t.Fatalf("ApplyBookGenesis failed: %v", err)
	}
	if res.Book.ID <= 0 || res.Volume.ID <= 0 || len(res.Entities) != 3 || len(res.Chapters) != 2 {
		t.Fatalf("unexpected apply result: %+v", res)
	}

	beats, err := st.ListBeats(ctx, res.Chapters[0].ID)
	if err != nil {
		t.Fatalf("ListBeats failed: %v", err)
	}
	if len(beats) != 2 {
		t.Fatalf("expected 2 beats in chapter 1, got %d", len(beats))
	}
	if len(beats[0].EntityIDs) < 2 {
		t.Fatalf("expected beat 1 to auto-link 林渊 & 赵无极, got %+v", beats[0].EntityIDs)
	}
}
