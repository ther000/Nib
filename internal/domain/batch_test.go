package domain

import (
	"errors"
	"testing"
)

func TestBatchEnumsAndValidation(t *testing.T) {
	for _, m := range []BatchMode{BatchModeDraftOnly, BatchModeDraftAndPolish, BatchModeFullPipeline} {
		if !m.Valid() || m.Label() == "" {
			t.Fatalf("expected valid BatchMode %q", m)
		}
	}
	if BatchMode("auto_100").Valid() {
		t.Fatalf("expected invalid BatchMode")
	}

	for _, s := range []BatchRunStatus{BatchRunRunning, BatchRunPaused, BatchRunCompleted, BatchRunCanceled, BatchRunFailed} {
		if !s.Valid() {
			t.Fatalf("expected valid BatchRunStatus %q", s)
		}
	}
	if BatchRunStatus("unknown").Valid() {
		t.Fatalf("expected invalid BatchRunStatus")
	}

	for _, st := range []BatchItemState{
		BatchItemQueued,
		BatchItemCompiling,
		BatchItemDrafting,
		BatchItemLinting,
		BatchItemCritiquing,
		BatchItemPolishing,
		BatchItemExtracting,
		BatchItemAwaitingReview,
		BatchItemFailed,
		BatchItemCanceled,
	} {
		if !st.Valid() {
			t.Fatalf("expected valid BatchItemState %q", st)
		}
	}
	if BatchItemState("done").Valid() {
		t.Fatalf("expected invalid BatchItemState")
	}

	for _, k := range []PacingAlertKind{PacingAlertSuppressOverload, PacingAlertMissingBurst, PacingAlertOverdueForeshadow} {
		if !k.Valid() {
			t.Fatalf("expected valid PacingAlertKind %q", k)
		}
	}
	if PacingAlertKind("bad").Valid() {
		t.Fatalf("expected invalid PacingAlertKind")
	}

	// BatchOutlineRequest
	if err := (BatchOutlineRequest{BookID: 1, VolumeID: 2, StartSeq: 5, Count: 5, ArcGoal: "反杀夺宝"}).Validate(); err != nil {
		t.Fatalf("valid BatchOutlineRequest failed: %v", err)
	}
	if !errors.Is((BatchOutlineRequest{BookID: 0, VolumeID: 2}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for bookId=0")
	}
	if !errors.Is((BatchOutlineRequest{BookID: 1, VolumeID: 2, Count: 15}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for count=15 (>10)")
	}

	// TokenUsageLog
	if err := (TokenUsageLog{
		BookID:           1,
		Stage:            StageStyle,
		ProviderName:     "DeepSeek",
		ModelName:        "deepseek-chat",
		PromptTokens:     300,
		CompletionTokens: 120,
		LatencyMs:        450,
	}).Validate(); err != nil {
		t.Fatalf("valid TokenUsageLog(style) failed: %v", err)
	}
	if !errors.Is((TokenUsageLog{BookID: 1, Stage: "invalid", ProviderName: "p", ModelName: "m"}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid usage stage")
	}
}

func TestDiagnoseVolumePacingAllThreeRules(t *testing.T) {
	// 构造 6 章：
	// 第 1 章 setup (w=1)
	// 第 2,3,4 章连续 3 章 suppress (w=0) -> 触发 suppress_overload (2..4)
	// 第 5 章 turn (w=2) -> 前 5 章(1..5)全无 burst -> 触发 missing_burst (1..5)
	// 第 6 章 burst (w=4) -> 截断 missing_burst
	chapters := []Chapter{
		{ID: 101, GlobalSeq: 1, VolSeq: 1, Title: "第一章", EmotionTag: EmotionSetup, HookType: HookMystery, Status: ChapterStatusFinalized},
		{ID: 102, GlobalSeq: 2, VolSeq: 2, Title: "第二章", EmotionTag: EmotionSuppress, HookType: HookCrisis, Status: ChapterStatusFinalized},
		{ID: 103, GlobalSeq: 3, VolSeq: 3, Title: "第三章", EmotionTag: EmotionSuppress, HookType: HookCrisis, Status: ChapterStatusFinalized},
		{ID: 104, GlobalSeq: 4, VolSeq: 4, Title: "第四章", EmotionTag: EmotionSuppress, HookType: HookCrisis, Status: ChapterStatusOutlined},
		{ID: 105, GlobalSeq: 5, VolSeq: 5, Title: "第五章", EmotionTag: EmotionTurn, HookType: HookCounterattackEve, Status: ChapterStatusOutlined},
		{ID: 106, GlobalSeq: 6, VolSeq: 6, Title: "第六章", EmotionTag: EmotionBurst, HookType: HookRewardPending, Status: ChapterStatusOutlined},
	}

	foreshadowings := []Foreshadowing{
		{ID: 11, Title: "古剑碑残魂", Status: ForeshadowingPlanted, TargetResolveSeq: 5},  // 超期 (6 >= 5)
		{ID: 12, Title: "已回收伏笔", Status: ForeshadowingResolved, TargetResolveSeq: 3}, // 已关闭，不告警
		{ID: 13, Title: "远期伏笔", Status: ForeshadowingHinted, TargetResolveSeq: 20},  // 未超期，不告警
	}

	report := DiagnoseVolumePacing(9, chapters, foreshadowings)
	if report.VolumeID != 9 || len(report.Points) != 6 {
		t.Fatalf("unexpected report points: vol=%d len=%d", report.VolumeID, len(report.Points))
	}
	if len(report.Alerts) != 3 {
		t.Fatalf("expected 3 pacing alerts, got %d: %+v", len(report.Alerts), report.Alerts)
	}

	byKind := make(map[PacingAlertKind]PacingAlert, len(report.Alerts))
	for _, a := range report.Alerts {
		byKind[a.Kind] = a
	}

	sup, ok := byKind[PacingAlertSuppressOverload]
	if !ok || sup.StartSeq != 2 || sup.EndSeq != 4 || len(sup.ChapterIDs) != 3 {
		t.Fatalf("unexpected suppress_overload alert: %+v", sup)
	}

	mb, ok := byKind[PacingAlertMissingBurst]
	if !ok || mb.StartSeq != 1 || mb.EndSeq != 5 || len(mb.ChapterIDs) != 5 {
		t.Fatalf("unexpected missing_burst alert: %+v", mb)
	}

	of, ok := byKind[PacingAlertOverdueForeshadow]
	if !ok || of.ForeshadowingID != 11 || of.StartSeq != 5 || of.EndSeq != 6 {
		t.Fatalf("unexpected overdue_foreshadowing alert: %+v", of)
	}
}
