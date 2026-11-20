package store

import (
	"context"
	"testing"

	"nib/internal/domain"
)

func TestAutopilotPersistenceRecoveryAndPricing(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "托管与定价测试", Genre: domain.GenreXuanhuan})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	// 1. 持久化 Autopilot 运行记录与崩溃恢复
	runID, err := st.CreateAutopilotRun(ctx, domain.AutopilotState{
		BookID:           book.ID,
		Status:           domain.AutopilotStatusRunning,
		Step:             domain.AutopilotStepDrafting,
		TargetChapters:   5,
		CompletedChapters: 2,
		AutoOutline:      true,
		AutoPolish:       true,
		AutoCommitMemory: true,
		TokensUsed:       4200,
		Message:          "正在写第 3 章",
	})
	if err != nil || runID <= 0 {
		t.Fatalf("CreateAutopilotRun failed: id=%d err=%v", runID, err)
	}

	if err := st.RecoverInterruptedAutopilotRuns(ctx); err != nil {
		t.Fatalf("RecoverInterruptedAutopilotRuns: %v", err)
	}
	latest, err := st.GetLatestAutopilotRun(ctx, book.ID)
	if err != nil || latest == nil {
		t.Fatalf("GetLatestAutopilotRun: %+v, err=%v", latest, err)
	}
	if latest.Status != domain.AutopilotStatusStopped || latest.CompletedChapters != 2 || latest.TargetChapters != 5 {
		t.Fatalf("unexpected recovered autopilot state: %+v", latest)
	}

	// 2. 备用模型路由持久化
	p1, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "PrimaryProv",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-primary-12345678",
		Models:   []string{"main-model"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider p1: %v", err)
	}
	p2, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "FallbackProv",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://backup.example.com/v1",
		APIKey:   "sk-backup-12345678",
		Models:   []string{"backup-model"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider p2: %v", err)
	}

	savedRoute, err := st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:              domain.StageDraft,
		ProviderID:         &p1.ID,
		ModelName:          "main-model",
		FallbackProviderID: &p2.ID,
		FallbackModelName:  "backup-model",
		Temperature:        0.8,
		MaxTokens:          4096,
	})
	if err != nil {
		t.Fatalf("SaveModelRoute: %v", err)
	}
	if savedRoute.FallbackProviderID == nil || *savedRoute.FallbackProviderID != p2.ID || savedRoute.FallbackModelName != "backup-model" {
		t.Fatalf("fallback route not persisted: %+v", savedRoute)
	}

	// 3. 自定义单价与用量费用计算
	if _, err := st.UpsertModelPricing(ctx, domain.ModelPricing{
		ProviderName:    "PrimaryProv",
		ModelName:       "main-model",
		InputPricePerM:  2.0,
		OutputPricePerM: 8.0,
		Currency:        "CNY",
	}); err != nil {
		t.Fatalf("UpsertModelPricing: %v", err)
	}
	if err := st.RecordTokenUsage(ctx, domain.TokenUsageLog{
		BookID:           book.ID,
		Stage:            domain.StageDraft,
		ProviderName:     "PrimaryProv",
		ModelName:        "main-model",
		PromptTokens:     500_000,
		CompletionTokens: 250_000,
		LatencyMs:        1200,
	}); err != nil {
		t.Fatalf("RecordTokenUsage: %v", err)
	}

	summary, err := st.GetUsageSummary(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetUsageSummary: %v", err)
	}
	// 0.5M * 2 + 0.25M * 8 = 1 + 2 = 3.0 CNY
	if summary.EstimatedCostCNY < 2.99 || summary.EstimatedCostCNY > 3.01 {
		t.Fatalf("EstimatedCostCNY = %f, want 3.0", summary.EstimatedCostCNY)
	}
}
