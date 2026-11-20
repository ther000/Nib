package domain

import (
	"errors"
	"testing"
)

func TestMemoryEnumsValidation(t *testing.T) {
	for _, s := range []StateEventStatus{StateEventPending, StateEventCommitted, StateEventReverted} {
		if !s.Valid() {
			t.Fatalf("expected valid StateEventStatus %q", s)
		}
	}
	if StateEventStatus("bad").Valid() {
		t.Fatalf("expected invalid StateEventStatus")
	}

	for _, s := range []ChangeSeverity{ChangeSeverityNormal, ChangeSeverityHighRisk} {
		if !s.Valid() {
			t.Fatalf("expected valid ChangeSeverity %q", s)
		}
	}
	if ChangeSeverity("critical").Valid() {
		t.Fatalf("expected invalid ChangeSeverity")
	}

	for _, s := range []ForeshadowingStatus{
		ForeshadowingPendingPlant,
		ForeshadowingPlanted,
		ForeshadowingHinted,
		ForeshadowingResolved,
		ForeshadowingAbandoned,
	} {
		if !s.Valid() || s.Label() == "" {
			t.Fatalf("expected valid ForeshadowingStatus with label: %q", s)
		}
	}
	if ForeshadowingStatus("closed").Valid() {
		t.Fatalf("expected invalid ForeshadowingStatus")
	}

	for _, l := range []ArcSummaryLevel{ArcSummaryLevelArc10, ArcSummaryLevelVolume} {
		if !l.Valid() {
			t.Fatalf("expected valid ArcSummaryLevel %q", l)
		}
	}
	if ArcSummaryLevel("chapter").Valid() {
		t.Fatalf("expected invalid ArcSummaryLevel")
	}

	for _, r := range []DeltaReviewStatus{DeltaReviewPending, DeltaReviewApplied, DeltaReviewDismissed} {
		if !r.Valid() {
			t.Fatalf("expected valid DeltaReviewStatus %q", r)
		}
	}
	if DeltaReviewStatus("approved").Valid() {
		t.Fatalf("expected invalid DeltaReviewStatus")
	}

	for _, a := range []DeltaForeshadowAction{
		DeltaForeshadowPlant,
		DeltaForeshadowHint,
		DeltaForeshadowResolve,
		DeltaForeshadowAbandon,
	} {
		if !a.Valid() {
			t.Fatalf("expected valid DeltaForeshadowAction %q", a)
		}
	}
	if DeltaForeshadowAction("delete").Valid() {
		t.Fatalf("expected invalid DeltaForeshadowAction")
	}
}

func TestMemoryInputsValidation(t *testing.T) {
	// EntityStateEvent
	validEvt := EntityStateEvent{
		BookID:         1,
		EntityID:       2,
		ChapterID:      3,
		ChapterSeq:     42,
		AttrKey:        "injury",
		NewValue:       "左肩阴雷暗伤",
		ChangeSeverity: ChangeSeverityNormal,
		Status:         StateEventPending,
	}
	if err := validEvt.Validate(); err != nil {
		t.Fatalf("valid state event failed: %v", err)
	}
	badEvt := validEvt
	badEvt.AttrKey = "  "
	if !errors.Is(badEvt.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for empty attrKey")
	}
	badEvt2 := validEvt
	badEvt2.ChangeSeverity = "unknown"
	if !errors.Is(badEvt2.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid changeSeverity")
	}

	// UpsertForeshadowingInput
	validFS := UpsertForeshadowingInput{
		BookID:            1,
		Title:             "血玉令牌密库",
		PlantedChapterSeq: 42,
		TargetResolveSeq:  75,
		Status:            ForeshadowingPlanted,
	}
	if err := validFS.Validate(); err != nil {
		t.Fatalf("valid foreshadowing failed: %v", err)
	}
	if !errors.Is((UpsertForeshadowingInput{BookID: 1, Title: ""}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for empty foreshadowing title")
	}
	if !errors.Is((UpsertForeshadowingInput{BookID: 1, Title: "x", TargetResolveSeq: -1}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for negative targetResolveSeq")
	}
	if !errors.Is((UpsertForeshadowingInput{BookID: 1, Title: "x", Status: "bad"}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid foreshadowing status")
	}

	// UpsertArcSummaryInput
	volID := int64(2)
	validArc := UpsertArcSummaryInput{
		BookID:   1,
		Level:    ArcSummaryLevelVolume,
		VolumeID: &volID,
		StartSeq: 1,
		EndSeq:   30,
		Summary:  "第一卷主角凭残阵反杀执事，夺丹遁走。",
	}
	if err := validArc.Validate(); err != nil {
		t.Fatalf("valid arc summary failed: %v", err)
	}
	badArcRange := validArc
	badArcRange.StartSeq = 20
	badArcRange.EndSeq = 10
	if !errors.Is(badArcRange.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for endSeq < startSeq")
	}
	zeroVol := int64(0)
	badArcVol := validArc
	badArcVol.VolumeID = &zeroVol
	if !errors.Is(badArcVol.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for volumeId=0")
	}

	// ApplyDeltaInput
	validApply := ApplyDeltaInput{
		DeltaID:            10,
		ApplyCausalSummary: true,
		Decisions: []DeltaDecision{
			{Kind: DeltaDecisionKindStatePatch, ItemID: "patch-1", Accept: true},
			{Kind: DeltaDecisionKindForeshadow, ItemID: "fs-1", Accept: false},
			{Kind: DeltaDecisionKindNewEntity, ItemID: "ent-1", Accept: true},
		},
	}
	if err := validApply.Validate(); err != nil {
		t.Fatalf("valid ApplyDeltaInput failed: %v", err)
	}
	if !errors.Is((ApplyDeltaInput{DeltaID: 0}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for deltaId=0")
	}
	badDecisionKind := ApplyDeltaInput{
		DeltaID:   10,
		Decisions: []DeltaDecision{{Kind: "invalid", ItemID: "p1", Accept: true}},
	}
	if !errors.Is(badDecisionKind.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid decision kind")
	}
	badDecisionItem := ApplyDeltaInput{
		DeltaID:   10,
		Decisions: []DeltaDecision{{Kind: DeltaDecisionKindStatePatch, ItemID: " ", Accept: true}},
	}
	if !errors.Is(badDecisionItem.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for empty decision itemId")
	}
}
