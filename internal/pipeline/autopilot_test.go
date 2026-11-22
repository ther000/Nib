package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/llm"
	"nib/internal/memory"
	"nib/internal/store"
)

type stubAutopilotLLM struct {
	outlineJSON string
	draftText   string
	extractJSON string
}

func (s *stubAutopilotLLM) Stream(
	_ context.Context,
	_ domain.Provider,
	req llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
) (domain.TokenUsage, error) {
	var resp string
	switch {
	case strings.Contains(req.System, "outlines"):
		resp = s.outlineJSON
	case strings.Contains(req.System, "causal_summary") || strings.Contains(req.System, "state_patches"):
		resp = s.extractJSON
	default:
		resp = s.draftText
	}
	if onEvent != nil {
		if err := onEvent(llm.StreamEvent{Delta: resp}); err != nil {
			return domain.TokenUsage{}, err
		}
	}
	return domain.TokenUsage{PromptTokens: 100, CompletionTokens: 200, TotalTokens: 300}, nil
}

func TestAutopilotAutoOutlineDraftAndCommitMemory(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "autopilot.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	prov, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "AutoProv",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-auto-1234",
		Models:   []string{"auto-model"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("upsert provider: %v", err)
	}
	for _, stage := range []domain.PipelineStage{domain.StageOutline, domain.StageDraft, domain.StageExtract} {
		if _, err := st.SaveModelRoute(ctx, domain.ModelRoute{
			Stage:       stage,
			ProviderID:  &prov.ID,
			ModelName:   "auto-model",
			Temperature: 0.7,
			MaxTokens:   4096,
		}); err != nil {
			t.Fatalf("save route %s: %v", stage, err)
		}
	}

	book, err := st.CreateBook(ctx, domain.CreateBookInput{
		Title:              "托管测试录",
		Genre:              domain.GenreXuanhuan,
		Premise:            "测试全自动托管",
		TargetChapterWords: 500,
	})
	if err != nil {
		t.Fatalf("create book: %v", err)
	}
	if _, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:  book.ID,
		Kind:    domain.EntityKindCharacter,
		Name:    "林渊",
		Summary: "主角",
		Attributes: map[string]string{
			"境界": "炼气一层",
		},
	}); err != nil {
		t.Fatalf("create entity: %v", err)
	}

	stubLLM := &stubAutopilotLLM{
		outlineJSON: `{"outlines":[
			{"title":"初入外门","emotion_tag":"setup","hook_type":"mystery","hook_note":"黑碑微亮","outline_summary":"林渊踏入外门广场","beats":[{"stage":"setup","summary":"林渊领取杂役令牌","conflict_note":"受人冷眼","target_words":500}]},
			{"title":"夜探剑冢","emotion_tag":"burst","hook_type":"crisis","hook_note":"钟声骤响","outline_summary":"林渊夜探剑冢得剑诀","beats":[{"stage":"burst","summary":"林渊拔出断剑突破境界","conflict_note":"剑气反噬","target_words":500}]}
		]}`,
		draftText: strings.Repeat("林渊握紧断剑，灵气顺着经脉奔涌而出，石阶上的尘土被剑风卷起。", 15),
		extractJSON: `{
			"causal_summary": "林渊突破至炼气二层。",
			"new_entities": [],
			"state_patches": [{"entity_name": "林渊", "attr_key": "境界", "new_value": "炼气二层", "severity": "normal", "reason": "拔剑突破"}],
			"foreshadow_updates": [{"action": "plant", "title": "断剑来历", "description": "剑身刻着太古符文", "target_resolve_seq": 10}]
		}`,
	}

	comp := compiler.New(st, compiler.WithMemorySource(st), compiler.WithStyleSource(st))
	ext := memory.NewExtractor(st, stubLLM)
	runner := New(st, comp, stubLLM, nil, WithDeltaExtractor(ext))

	_, err = runner.StartAutopilot(ctx, domain.AutopilotRequest{
		BookID:           book.ID,
		TargetChapters:   2,
		AutoOutline:      true,
		AutoPolish:       true,
		AutoCommitMemory: true,
	})
	if err != nil {
		t.Fatalf("StartAutopilot failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := runner.GetAutopilotState(book.ID)
		if state.Status == domain.AutopilotStatusCompleted || state.Status == domain.AutopilotStatusFailed {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	finalState := runner.GetAutopilotState(book.ID)
	if finalState.Status != domain.AutopilotStatusCompleted {
		t.Fatalf("expected autopilot completed, got status=%s err=%s msg=%s", finalState.Status, finalState.Error, finalState.Message)
	}
	if finalState.CompletedChapters != 2 {
		t.Fatalf("expected 2 completed chapters, got %d", finalState.CompletedChapters)
	}

	chaps, err := st.ListBookChapters(ctx, book.ID)
	if err != nil {
		t.Fatalf("list chapters: %v", err)
	}
	if len(chaps) != 2 {
		t.Fatalf("expected 2 chapters auto-outlined & drafted, got %d", len(chaps))
	}
	for _, ch := range chaps {
		if strings.TrimSpace(ch.Content) == "" {
			t.Fatalf("chapter %d content should not be empty", ch.GlobalSeq)
		}
	}

	// 验证 1.A：托管模式下记忆已自动提交生效（非 pending）
	snaps, err := st.SnapshotAt(ctx, book.ID, 2, false)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snaps) == 0 || snaps[0].EffectiveAttributes["境界"] != "炼气二层" {
		t.Fatalf("expected auto-committed attribute 境界=炼气二层, got %+v", snaps)
	}
}

func TestAutopilotHighRiskMemoryTriggersDecisionGate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "autopilot-risk.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	prov, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "AutoProv",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-auto-risk",
		Models:   []string{"auto-model"},
		Enabled:  true,
	})
	for _, stage := range []domain.PipelineStage{domain.StageOutline, domain.StageDraft, domain.StageExtract} {
		_, _ = st.SaveModelRoute(ctx, domain.ModelRoute{
			Stage:       stage,
			ProviderID:  &prov.ID,
			ModelName:   "auto-model",
			Temperature: 0.7,
			MaxTokens:   4096,
		})
	}
	book, _ := st.CreateBook(ctx, domain.CreateBookInput{
		Title:              "高风险门控测试",
		Genre:              domain.GenreXuanhuan,
		TargetChapterWords: 500,
	})
	_, _ = st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:     book.ID,
		Kind:       domain.EntityKindCharacter,
		Name:       "林渊",
		Attributes: map[string]string{"状态": "健康"},
	})

	stubLLM := &stubAutopilotLLM{
		outlineJSON: `{"outlines":[{"title":"绝境血战","emotion_tag":"burst","hook_type":"crisis","hook_note":"生死未卜","outline_summary":"林渊强行燃血破阵","beats":[{"stage":"burst","summary":"林渊身受重创濒死","conflict_note":"生死关头","target_words":500}]}]}`,
		draftText:   strings.Repeat("林渊口吐鲜血，长剑断折，勉强撑住最后一口灵气。", 15),
		extractJSON: `{
			"causal_summary": "林渊重伤濒死。",
			"new_entities": [],
			"state_patches": [{"entity_name": "林渊", "attr_key": "状态", "new_value": "死亡", "severity": "high_risk", "reason": "误判死亡"}],
			"foreshadow_updates": []
		}`,
	}
	comp := compiler.New(st, compiler.WithMemorySource(st), compiler.WithStyleSource(st))
	ext := memory.NewExtractor(st, stubLLM)
	runner := New(st, comp, stubLLM, nil, WithDeltaExtractor(ext))
	defer runner.Close()

	if _, err := runner.StartAutopilot(ctx, domain.AutopilotRequest{
		BookID:           book.ID,
		TargetChapters:   1,
		AutoOutline:      true,
		AutoPolish:       false,
		AutoCommitMemory: true,
	}); err != nil {
		t.Fatalf("StartAutopilot: %v", err)
	}

	var gateState domain.AutopilotState
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		gateState = runner.GetAutopilotState(book.ID)
		if gateState.Status == domain.AutopilotStatusWaitingDecision || gateState.Status == domain.AutopilotStatusFailed || gateState.Status == domain.AutopilotStatusCompleted {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if gateState.Status != domain.AutopilotStatusWaitingDecision || gateState.Gate == nil || gateState.Gate.Reason != "high_risk_memory" {
		t.Fatalf("expected waiting_decision gate high_risk_memory, got status=%s gate=%+v", gateState.Status, gateState.Gate)
	}

	// 验证在作者拍板前，高风险变更未被静默提交
	snaps, _ := st.SnapshotAt(ctx, book.ID, 1, false)
	if len(snaps) == 0 || snaps[0].EffectiveAttributes["状态"] != "健康" {
		t.Fatalf("high risk delta must not be committed before decision, got %+v", snaps)
	}

	// 选择暂停去审改台核对
	if _, err := runner.ResolveAutopilotGate(book.ID, domain.AutopilotDecisionStop); err != nil {
		t.Fatalf("ResolveAutopilotGate: %v", err)
	}
}
