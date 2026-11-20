package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nib/internal/domain"
)

func TestPhase3ToPhase4UpgradeBackupAndBatchCRUD(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "upgrade-v3-v4.db")

	allMigs, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("loadEmbeddedMigrations failed: %v", err)
	}
	if len(allMigs) < 4 {
		t.Fatalf("expected at least 4 migrations, got %d", len(allMigs))
	}

	// 1. 先用 v1..v3 迁移建库并插入数据，再升级至 v4，校验 .bak.3 备份存在。
	st3, err := openWithMigrations(dbPath, allMigs[:3])
	if err != nil {
		t.Fatalf("open v1..v3 failed: %v", err)
	}
	book, err := st3.CreateBook(ctx, domain.CreateBookInput{Title: "微批进化录", Genre: domain.GenreXuanhuan})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}
	vol, err := st3.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	if err != nil {
		t.Fatalf("UpsertVolume failed: %v", err)
	}
	ch1, err := st3.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "起风"})
	if err != nil {
		t.Fatalf("CreateChapter 1 failed: %v", err)
	}
	ch2, err := st3.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "破阵"})
	if err != nil {
		t.Fatalf("CreateChapter 2 failed: %v", err)
	}
	if err := st3.Close(); err != nil {
		t.Fatalf("Close st3 failed: %v", err)
	}

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open v4 upgrade failed: %v", err)
	}
	defer st.Close()

	if _, err := os.Stat(dbPath + ".bak.3"); err != nil {
		t.Fatalf("expected pre-upgrade backup %s.bak.3 to exist: %v", dbPath, err)
	}

	// 2. GetLatestBatchRun 无记录时返回 (nil, nil)。
	noneRun, err := st.GetLatestBatchRun(ctx, book.ID)
	if err != nil || noneRun != nil {
		t.Fatalf("expected (nil, nil) before any batch run, got (%+v, %v)", noneRun, err)
	}

	// 3. CreateBatchRun / GetBatchRun / UpdateBatchItemState / RecoverInterruptedBatchRuns
	run, err := st.CreateBatchRun(ctx, book.ID, []int64{ch1.ID, ch2.ID}, domain.BatchModeFullPipeline)
	if err != nil {
		t.Fatalf("CreateBatchRun failed: %v", err)
	}
	if run.Status != domain.BatchRunRunning || run.TotalItems != 2 || len(run.Items) != 2 {
		t.Fatalf("unexpected created BatchRun: %+v", run)
	}
	if run.Items[0].ChapterTitle != "起风" || run.Items[1].ChapterTitle != "破阵" {
		t.Fatalf("expected joined chapter titles on batch items: %+v", run.Items)
	}

	if err := st.UpdateBatchItemState(ctx, run.Items[0].ID, domain.BatchItemAwaitingReview, "hash1", ""); err != nil {
		t.Fatalf("UpdateBatchItemState 0 failed: %v", err)
	}
	if err := st.UpdateBatchItemState(ctx, run.Items[1].ID, domain.BatchItemDrafting, "hash2", ""); err != nil {
		t.Fatalf("UpdateBatchItemState 1 failed: %v", err)
	}

	// 模拟崩溃重启恢复：running 批次置为 paused，drafting 在途子项回到 queued，已完成 awaiting_review 不受影响。
	if err := st.RecoverInterruptedBatchRuns(ctx); err != nil {
		t.Fatalf("RecoverInterruptedBatchRuns failed: %v", err)
	}
	recovered, err := st.GetBatchRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetBatchRun after recovery failed: %v", err)
	}
	if recovered.Status != domain.BatchRunPaused {
		t.Fatalf("expected recovered batch status=paused, got %q", recovered.Status)
	}
	if recovered.Items[0].State != domain.BatchItemAwaitingReview || recovered.Items[1].State != domain.BatchItemQueued {
		t.Fatalf("unexpected recovered item states: [0]=%q [1]=%q", recovered.Items[0].State, recovered.Items[1].State)
	}

	latest, err := st.GetLatestBatchRun(ctx, book.ID)
	if err != nil || latest == nil || latest.ID != run.ID {
		t.Fatalf("GetLatestBatchRun = (%+v, %v), want id=%d", latest, err, run.ID)
	}

	// 4. Token Usage 流水记录与按环节/按模型聚合
	logs := []domain.TokenUsageLog{
		{BookID: book.ID, ChapterID: ch1.ID, Stage: domain.StageDraft, ProviderName: "DeepSeek", ModelName: "deepseek-chat", PromptTokens: 1000, CompletionTokens: 600, LatencyMs: 1200},
		{BookID: book.ID, ChapterID: ch2.ID, Stage: domain.StageDraft, ProviderName: "DeepSeek", ModelName: "deepseek-chat", PromptTokens: 1200, CompletionTokens: 800, LatencyMs: 1800},
		{BookID: book.ID, ChapterID: ch1.ID, Stage: domain.StageCritic, ProviderName: "Anthropic", ModelName: "claude-sonnet", PromptTokens: 900, CompletionTokens: 200, LatencyMs: 900},
	}
	for _, entry := range logs {
		if err := st.RecordTokenUsage(ctx, entry); err != nil {
			t.Fatalf("RecordTokenUsage failed: %v", err)
		}
	}

	summary, err := st.GetUsageSummary(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}
	if summary.TotalCalls != 3 || summary.TotalPromptTokens != 3100 || summary.TotalCompletionTokens != 1600 || summary.TotalTokens != 4700 {
		t.Fatalf("unexpected UsageSummary totals: %+v", summary)
	}
	if len(summary.ByStage) != 2 || len(summary.ByModel) != 2 {
		t.Fatalf("expected 2 stages and 2 models, got %+v", summary)
	}
	for _, stStat := range summary.ByStage {
		if stStat.Stage == string(domain.StageDraft) && (stStat.Calls != 2 || stStat.AvgLatencyMs != 1500) {
			t.Fatalf("unexpected draft stage stat: %+v", stStat)
		}
	}
}
