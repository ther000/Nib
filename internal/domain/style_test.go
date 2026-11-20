package domain

import (
	"errors"
	"testing"
)

func TestStyleEnumsValidation(t *testing.T) {
	for _, s := range AllSceneTypes() {
		if !s.Valid() || s.Label() == "" {
			t.Fatalf("expected valid SceneType %q", s)
		}
	}
	if SceneType("romance").Valid() {
		t.Fatalf("expected invalid SceneType")
	}

	for _, c := range []DeaiCategory{DeaiCategoryCliche, DeaiCategoryStructure, DeaiCategoryFormatting} {
		if !c.Valid() || c.Label() == "" {
			t.Fatalf("expected valid DeaiCategory %q", c)
		}
	}
	if DeaiCategory("other").Valid() {
		t.Fatalf("expected invalid DeaiCategory")
	}

	for _, sev := range []LintSeverity{LintSeverityInfo, LintSeverityWarn, LintSeverityError} {
		if !sev.Valid() || sev.Label() == "" {
			t.Fatalf("expected valid LintSeverity %q", sev)
		}
	}
	if LintSeverity("fatal").Valid() {
		t.Fatalf("expected invalid LintSeverity")
	}

	for _, p := range []PovRule{PovThirdLimited, PovFirstPerson, PovThirdOmniscient} {
		if !p.Valid() || p.Label() == "" {
			t.Fatalf("expected valid PovRule %q", p)
		}
	}
	if PovRule("second_person").Valid() {
		t.Fatalf("expected invalid PovRule")
	}

	for _, d := range []CriticDimension{CriticDimConsistency, CriticDimPoisonPoint, CriticDimBeatCoverage, CriticDimHook} {
		if !d.Valid() || d.Label() == "" {
			t.Fatalf("expected valid CriticDimension %q", d)
		}
	}
	if CriticDimension("grammar").Valid() {
		t.Fatalf("expected invalid CriticDimension")
	}

	for _, st := range []DiffStatus{DiffStatusPending, DiffStatusAccepted, DiffStatusIgnored} {
		if !st.Valid() {
			t.Fatalf("expected valid DiffStatus %q", st)
		}
	}
	if DiffStatus("reverted").Valid() {
		t.Fatalf("expected invalid DiffStatus")
	}
}

func TestStyleInputsValidation(t *testing.T) {
	def := DefaultStyleProfile(7)
	if def.BookID != 7 || def.AvgSentenceChars != DefaultAvgSentenceChars || def.MaxParagraphChars != DefaultMaxParagraphChars || def.PovRule != PovThirdLimited {
		t.Fatalf("unexpected DefaultStyleProfile: %+v", def)
	}

	validProfile := UpsertStyleProfileInput{
		BookID:            1,
		AvgSentenceChars:  18.5,
		MaxParagraphChars: 130,
		DialogueRatioMin:  0.25,
		DialogueRatioMax:  0.55,
		PovRule:           PovThirdLimited,
		ToneDirectives:    "冷峻克制，动作先于心理",
	}
	if err := validProfile.Validate(); err != nil {
		t.Fatalf("valid StyleProfile input failed: %v", err)
	}
	if !errors.Is((UpsertStyleProfileInput{BookID: 0}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for bookId=0")
	}
	if !errors.Is((UpsertStyleProfileInput{BookID: 1, AvgSentenceChars: -1}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for negative avgSentenceChars")
	}
	if !errors.Is((UpsertStyleProfileInput{BookID: 1, DialogueRatioMin: 0.7, DialogueRatioMax: 0.3}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for dialogueRatioMin > dialogueRatioMax")
	}
	if !errors.Is((UpsertStyleProfileInput{BookID: 1, PovRule: "bad"}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid povRule")
	}

	// StyleSample
	validSample := UpsertStyleSampleInput{
		BookID:    1,
		SceneType: SceneCombat,
		Title:     "残阵反杀样章",
		Excerpt:   "剑锋擦过石壁，火星迸溅三尺。",
	}
	if err := validSample.Validate(); err != nil {
		t.Fatalf("valid StyleSample input failed: %v", err)
	}
	if !errors.Is((UpsertStyleSampleInput{BookID: 1, SceneType: "invalid", Title: "t", Excerpt: "e"}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid sceneType")
	}
	if !errors.Is((UpsertStyleSampleInput{BookID: 1, SceneType: SceneCombat, Title: " ", Excerpt: "e"}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for empty title")
	}
	if !errors.Is((UpsertStyleSampleInput{BookID: 1, SceneType: SceneCombat, Title: "t", Excerpt: " \n "}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for empty excerpt")
	}

	// DeaiRule (BookID=0 valid for global rules)
	validRule := UpsertDeaiRuleInput{
		BookID:        0,
		Category:      DeaiCategoryCliche,
		Pattern:       "嘴角勾起一抹弧度",
		Severity:      LintSeverityError,
		MaxPerChapter: 0,
		Hint:          "删去套话，改写具体微表情",
		Enabled:       true,
	}
	if err := validRule.Validate(); err != nil {
		t.Fatalf("valid DeaiRule input failed: %v", err)
	}
	if !errors.Is((UpsertDeaiRuleInput{BookID: -1, Category: DeaiCategoryCliche, Pattern: "x"}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for negative bookId")
	}
	if !errors.Is((UpsertDeaiRuleInput{BookID: 1, Category: "bad", Pattern: "x"}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid category")
	}
	if !errors.Is((UpsertDeaiRuleInput{BookID: 1, Category: DeaiCategoryCliche, Pattern: " "}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for empty pattern")
	}

	// ApplySurgicalDiffInput
	if err := (ApplySurgicalDiffInput{ChapterID: 42, DiffID: "diff-14", Accept: true}).Validate(); err != nil {
		t.Fatalf("valid ApplySurgicalDiffInput failed: %v", err)
	}
	if !errors.Is((ApplySurgicalDiffInput{ChapterID: 0, DiffID: "diff-14"}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for chapterId=0")
	}
	if !errors.Is((ApplySurgicalDiffInput{ChapterID: 42, DiffID: "  "}).Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for empty diffId")
	}
}
