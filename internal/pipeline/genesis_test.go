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

type recordingGenesisLLM struct {
	payloads []string
	requests []llm.ChatRequest
	idx      int
}

func (s *recordingGenesisLLM) Stream(
	_ context.Context,
	_ domain.Provider,
	req llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
) (domain.TokenUsage, error) {
	s.requests = append(s.requests, req)
	payload := ""
	if s.idx < len(s.payloads) {
		payload = s.payloads[s.idx]
		s.idx++
	}
	if onEvent != nil && payload != "" {
		if err := onEvent(llm.StreamEvent{Delta: payload}); err != nil {
			return domain.TokenUsage{}, err
		}
	}
	return domain.TokenUsage{PromptTokens: 100, CompletionTokens: 200, TotalTokens: 300}, nil
}

func openGenesisTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "genesis2.db"))
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
		Stage:       domain.StageOutline,
		ProviderID:  &prov.ID,
		ModelName:   "test-model",
		Temperature: 0.7,
		MaxTokens:   4096,
	}); err != nil {
		t.Fatalf("save outline route: %v", err)
	}
	return st
}

func TestGenerateBookConceptAndRefine(t *testing.T) {
	ctx := context.Background()
	st := openGenesisTestStore(t)

	llmStub := &recordingGenesisLLM{
		payloads: []string{
			`{
				"title": "《寿元典当行》",
				"title_candidates": ["寿元典当行", "因果黑市", "我以寿元换长生"],
				"premise": "散修陈长生继承黑市典当行，以他人因果换取寿元功法。",
				"golden_finger": "典当契约可收割自愿典当者的寿元与秘法，但违约必遭反噬。",
				"core_conflict": "三大仙门追查失踪秘宝，陈长生必须在藏拙中借力打力。"
			}`,
			`{
				"title": "苟在黑市当寿元",
				"title_candidates": ["苟在黑市当寿元", "寿元典当行"],
				"premise": "陈长生表面是普通掌柜，暗中操控修真界黑市寿元交易。",
				"golden_finger": "契约必成，越是强敌违约反噬越烈。",
				"core_conflict": "既要稳住坊市明面身份，又要防备金丹老祖查上门。"
			}`,
		},
	}

	runner := New(st, nil, llmStub, nil)
	concept, err := runner.GenerateBookConcept(ctx, domain.BookConceptRequest{
		Idea:    "散修继承能交易寿元的黑市典当行",
		Genre:   domain.GenreXianxia,
		POVMode: "first_person",
	}, nil)
	if err != nil {
		t.Fatalf("GenerateBookConcept failed: %v", err)
	}
	if concept.Title != "寿元典当行" || len(concept.TitleCandidates) != 3 || concept.POVMode != "first_person" {
		t.Fatalf("unexpected concept: %+v", concept)
	}

	refined, err := runner.GenerateBookConcept(ctx, domain.BookConceptRequest{
		Idea:           "散修继承能交易寿元的黑市典当行",
		Genre:          domain.GenreXianxia,
		POVMode:        "first_person",
		Feedback:       "更偏苟道流，主角藏拙更深",
		CurrentConcept: &concept,
	}, nil)
	if err != nil {
		t.Fatalf("refine GenerateBookConcept failed: %v", err)
	}
	if refined.Title != "苟在黑市当寿元" || len(llmStub.requests) != 2 {
		t.Fatalf("unexpected refined concept: %+v", refined)
	}
}

func TestGenerateBookBlueprintSectionsAndZeroChapters(t *testing.T) {
	ctx := context.Background()
	st := openGenesisTestStore(t)

	llmStub := &recordingGenesisLLM{
		payloads: []string{
			// 1. entities only
			`{
				"entities": [
					{"kind": "character", "name": "陈长生", "aliases": ["陈掌柜"], "is_invariant": false, "summary": "黑市典当行掌柜", "attributes": {"修为境界": "炼气五层"}},
					{"kind": "rule", "name": "契约反噬律", "aliases": [], "is_invariant": true, "summary": "违背典当契约者寿元减半", "attributes": {}}
				]
			}`,
			// 2. all with chapterCount = 0
			`{
				"first_volume": {"title": "第一卷 黑市起家", "goal": "站稳坊市脚跟", "climax_plan": "坑杀来抄家的筑基执事"},
				"entities": [
					{"kind": "character", "name": "陈长生", "aliases": [], "is_invariant": false, "summary": "藏拙掌柜", "attributes": {"修为境界": "炼气五层"}}
				],
				"initial_outlines": []
			}`,
		},
	}

	runner := New(st, nil, llmStub, nil)
	concept := domain.BookConcept{
		Title:        "苟在黑市当寿元",
		Genre:        domain.GenreXianxia,
		POVMode:      "third_limited",
		Premise:      "散修藏拙黑市，以契约交易寿元。",
		GoldenFinger: "黑市典当契约。",
	}

	entBP, err := runner.GenerateBookBlueprint(ctx, domain.BookBlueprintRequest{
		Concept: concept,
		Section: "entities",
	}, nil)
	if err != nil {
		t.Fatalf("entities blueprint failed: %v", err)
	}
	if len(entBP.Entities) != 2 || len(entBP.InitialOutlines) != 0 {
		t.Fatalf("unexpected entities blueprint: %+v", entBP)
	}

	zeroBP, err := runner.GenerateBookBlueprint(ctx, domain.BookBlueprintRequest{
		Concept:            concept,
		ChapterCount:       0,
		TargetChapterWords: 3000,
		Section:            "all",
	}, nil)
	if err != nil {
		t.Fatalf("zero-chapter blueprint failed: %v", err)
	}
	if len(zeroBP.InitialOutlines) != 0 || zeroBP.FirstVolume.Title != "第一卷 黑市起家" {
		t.Fatalf("unexpected zero-chapter blueprint: %+v", zeroBP)
	}

	applied, err := runner.ApplyBookGenesis(ctx, domain.ApplyGenesisInput{
		Plan: domain.BookGenesisPlan{
			Title:              concept.Title,
			Genre:              concept.Genre,
			POVMode:            concept.POVMode,
			Premise:            concept.Premise,
			GoldenFinger:       concept.GoldenFinger,
			TargetChapterWords: 3000,
			FirstVolume:        zeroBP.FirstVolume,
			Entities:           zeroBP.Entities,
			InitialOutlines:    zeroBP.InitialOutlines,
		},
	})
	if err != nil {
		t.Fatalf("ApplyBookGenesis failed: %v", err)
	}
	if applied.Book.POVMode != "third_limited" {
		t.Fatalf("expected POVMode persisted, got %q", applied.Book.POVMode)
	}
	if len(applied.Chapters) != 1 || applied.Chapters[0].Title != "初章" {
		t.Fatalf("expected fallback first chapter when outlines empty, got %+v", applied.Chapters)
	}
}
