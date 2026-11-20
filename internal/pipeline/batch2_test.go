package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/llm"
	"nib/internal/store"
)

func TestModelRouteAutomaticFallback(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "降级测试", Genre: domain.GenreXuanhuan})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Seq: 1, Title: "第一卷"})
	ch, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第一章"})

	p1, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name: "Primary", Protocol: domain.ProtocolOpenAICompat, BaseURL: "https://p1.example.com/v1",
		APIKey: "sk-1", Models: []string{"m-main"}, Enabled: true,
	})
	p2, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name: "Backup", Protocol: domain.ProtocolOpenAICompat, BaseURL: "https://p2.example.com/v1",
		APIKey: "sk-2", Models: []string{"m-backup"}, Enabled: true,
	})
	_, _ = st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage: domain.StageDraft, ProviderID: &p1.ID, ModelName: "m-main",
		FallbackProviderID: &p2.ID, FallbackModelName: "m-backup", Temperature: 0.7, MaxTokens: 2048,
	})

	calls := 0
	streamer := &fakeLLM{
		fn: func(_ context.Context, prov domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			calls++
			if prov.ID == p1.ID {
				return domain.TokenUsage{}, errors.New("429 rate limited")
			}
			if prov.ID != p2.ID || req.Model != "m-backup" {
				t.Fatalf("unexpected fallback call: prov=%+v model=%q", prov, req.Model)
			}
			_ = onEvent(llm.StreamEvent{Delta: "备用模型接管写作成功。"})
			return domain.TokenUsage{PromptTokens: 100, CompletionTokens: 30, TotalTokens: 130}, nil
		},
	}

	runner := New(st, compiler.New(st), streamer, nil)
	saved, err := runner.GenerateChapter(ctx, ch.ID, domain.GenerateChapterOptions{}, nil)
	if err != nil {
		t.Fatalf("GenerateChapter with fallback failed: %v", err)
	}
	if calls != 2 || saved.Content != "备用模型接管写作成功。" {
		t.Fatalf("expected 2 calls and fallback prose, got calls=%d content=%q", calls, saved.Content)
	}
}

func TestGenerateArcSummaryAndPersist(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "弧线汇总测试", Genre: domain.GenreXianxia})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Seq: 1, Title: "第一卷 外门风云"})
	for i := 1; i <= 3; i++ {
		ch, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "章节"})
		_, _ = st.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
			ID: ch.ID, Title: ch.Title, CausalSummary: "林渊完成阶段突破并收获灵石。",
		})
	}

	p1, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name: "Prov", Protocol: domain.ProtocolOpenAICompat, BaseURL: "https://p1.example.com/v1",
		APIKey: "sk-1", Models: []string{"m-extract"}, Enabled: true,
	})
	_, _ = st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage: domain.StageExtract, ProviderID: &p1.ID, ModelName: "m-extract", Temperature: 0.2, MaxTokens: 1024,
	})

	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			if !strings.Contains(req.Messages[0].Content, "林渊完成阶段突破") {
				return domain.TokenUsage{}, errors.New("missing causal summary material")
			}
			_ = onEvent(llm.StreamEvent{Delta: "第1至3章中，林渊在外门接连突破，积累灵石并引起执事警觉。"})
			return domain.TokenUsage{PromptTokens: 150, CompletionTokens: 40, TotalTokens: 190}, nil
		},
	}

	runner := New(st, compiler.New(st), streamer, nil)
	arc, err := runner.GenerateArcSummary(ctx, domain.GenerateArcSummaryRequest{
		BookID:   book.ID,
		Level:    domain.ArcSummaryLevelArc10,
		StartSeq: 1,
		EndSeq:   3,
	})
	if err != nil {
		t.Fatalf("GenerateArcSummary failed: %v", err)
	}
	if !strings.Contains(arc.Summary, "林渊在外门接连突破") || arc.StartSeq != 1 || arc.EndSeq != 3 {
		t.Fatalf("unexpected arc summary: %+v", arc)
	}
}
