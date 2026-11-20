package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"nib/internal/domain"
)

type fakeStyleStore struct {
	profile       domain.StyleProfile
	samples       []domain.StyleSample
	rules         []domain.DeaiRule
	audit         domain.ChapterAudit
	lastScene     domain.SceneType
	deletedSample int64
	deletedRule   int64
}

func (f *fakeStyleStore) GetStyleProfile(_ context.Context, bookID int64) (domain.StyleProfile, error) {
	p := f.profile
	if p.BookID == 0 {
		p = domain.DefaultStyleProfile(bookID)
	}
	return p, nil
}

func (f *fakeStyleStore) SaveStyleProfile(_ context.Context, in domain.UpsertStyleProfileInput) (domain.StyleProfile, error) {
	f.profile = domain.StyleProfile{
		ID:                  1,
		BookID:              in.BookID,
		AvgSentenceChars:    in.AvgSentenceChars,
		MaxParagraphChars:   in.MaxParagraphChars,
		DialogueRatioMin:    in.DialogueRatioMin,
		DialogueRatioMax:    in.DialogueRatioMax,
		PovRule:             in.PovRule,
		ToneDirectives:      in.ToneDirectives,
		ForbiddenTropesNote: in.ForbiddenTropesNote,
	}
	return f.profile, nil
}

func (f *fakeStyleStore) ListStyleSamples(_ context.Context, _ int64, sceneType domain.SceneType) ([]domain.StyleSample, error) {
	f.lastScene = sceneType
	return f.samples, nil
}

func (f *fakeStyleStore) UpsertStyleSample(_ context.Context, in domain.UpsertStyleSampleInput) (domain.StyleSample, error) {
	return domain.StyleSample{
		ID:        5,
		BookID:    in.BookID,
		SceneType: in.SceneType,
		Title:     in.Title,
		Excerpt:   in.Excerpt,
		CharCount: domain.CountWords(in.Excerpt),
	}, nil
}

func (f *fakeStyleStore) DeleteStyleSample(_ context.Context, id int64) error {
	f.deletedSample = id
	return nil
}

func (f *fakeStyleStore) ListDeaiRules(_ context.Context, _ int64) ([]domain.DeaiRule, error) {
	return f.rules, nil
}

func (f *fakeStyleStore) UpsertDeaiRule(_ context.Context, in domain.UpsertDeaiRuleInput) (domain.DeaiRule, error) {
	return domain.DeaiRule{
		ID:            8,
		BookID:        in.BookID,
		Category:      in.Category,
		Pattern:       in.Pattern,
		IsRegex:       in.IsRegex,
		Severity:      in.Severity,
		MaxPerChapter: in.MaxPerChapter,
		Hint:          in.Hint,
		Enabled:       in.Enabled,
	}, nil
}

func (f *fakeStyleStore) DeleteDeaiRule(_ context.Context, id int64) error {
	f.deletedRule = id
	return nil
}

func (f *fakeStyleStore) GetChapterAudit(_ context.Context, chapterID int64) (domain.ChapterAudit, error) {
	a := f.audit
	a.ChapterID = chapterID
	return a, nil
}

type fakeStyleEngine struct {
	extractedProfile domain.StyleProfile
	audit            domain.ChapterAudit
	applyRes         domain.ApplySurgicalDiffResult
	started          chan struct{}
	blockCh          chan struct{}
}

func (e *fakeStyleEngine) ExtractStyleProfile(ctx context.Context, bookID int64, _ []string) (domain.StyleProfile, error) {
	if e.started != nil {
		close(e.started)
	}
	if e.blockCh != nil {
		select {
		case <-ctx.Done():
			return domain.StyleProfile{}, ctx.Err()
		case <-e.blockCh:
		}
	}
	p := e.extractedProfile
	p.BookID = bookID
	return p, nil
}

func (e *fakeStyleEngine) LintChapterLocal(_ context.Context, chapterID int64) (domain.ChapterAudit, error) {
	a := e.audit
	a.ChapterID = chapterID
	return a, nil
}

func (e *fakeStyleEngine) RunCriticAudit(ctx context.Context, chapterID int64) (domain.ChapterAudit, error) {
	if e.started != nil {
		close(e.started)
	}
	if e.blockCh != nil {
		select {
		case <-ctx.Done():
			return domain.ChapterAudit{}, ctx.Err()
		case <-e.blockCh:
		}
	}
	a := e.audit
	a.ChapterID = chapterID
	return a, nil
}

func (e *fakeStyleEngine) GenerateSurgicalDiffs(ctx context.Context, chapterID int64, _ []int) (domain.ChapterAudit, error) {
	if e.started != nil {
		close(e.started)
	}
	if e.blockCh != nil {
		select {
		case <-ctx.Done():
			return domain.ChapterAudit{}, ctx.Err()
		case <-e.blockCh:
		}
	}
	a := e.audit
	a.ChapterID = chapterID
	return a, nil
}

func (e *fakeStyleEngine) ApplySurgicalDiff(_ context.Context, _ domain.ApplySurgicalDiffInput) (domain.ApplySurgicalDiffResult, error) {
	return e.applyRes, nil
}

func TestStyleServiceNilRuntimeGuard(t *testing.T) {
	ctx := context.Background()
	svc := NewStyleService(NewRuntime())

	if _, err := svc.GetStyleProfile(ctx, 1); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("GetStyleProfile err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.ExtractStyleProfile(ctx, 1, []string{"样章"}); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("ExtractStyleProfile err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.ListStyleSamples(ctx, 1, ""); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("ListStyleSamples err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.ListDeaiRules(ctx, 0); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("ListDeaiRules err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.GetChapterAudit(ctx, 1); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("GetChapterAudit err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.LintChapterLocal(ctx, 1); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("LintChapterLocal err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.RunCriticAudit(ctx, 1); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("RunCriticAudit err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.GenerateSurgicalDiffs(ctx, 1, nil); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("GenerateSurgicalDiffs err = %v, want ErrRuntimeNotReady", err)
	}
	if _, err := svc.ApplySurgicalDiff(ctx, domain.ApplySurgicalDiffInput{ChapterID: 1, DiffID: "d1"}); !errors.Is(err, ErrRuntimeNotReady) {
		t.Fatalf("ApplySurgicalDiff err = %v, want ErrRuntimeNotReady", err)
	}
}

func TestStyleServiceValidationNormalizationAndExportSync(t *testing.T) {
	ctx := context.Background()
	st := newFakeStore()
	exp := &fakeExporter{}
	ss := &fakeStyleStore{}
	eng := &fakeStyleEngine{
		extractedProfile: domain.DefaultStyleProfile(1),
		audit:            domain.ChapterAudit{ID: 3, DeaiScore: 92, HookPassed: true},
	}

	rt := NewRuntime()
	rt.Init(RuntimeDeps{
		Store:       st,
		Exporter:    exp,
		StyleStore:  ss,
		StyleEngine: eng,
	})
	lib := NewLibraryService(rt)
	svc := NewStyleService(rt)

	// 参数校验
	if _, err := svc.GetStyleProfile(ctx, 0); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bookId=0, got %v", err)
	}
	if _, err := svc.ExtractStyleProfile(ctx, 1, []string{"  ", ""}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty sampleTexts, got %v", err)
	}
	if _, err := svc.ListStyleSamples(ctx, 1, "bad_scene"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for bad sceneType, got %v", err)
	}
	if _, err := svc.ListDeaiRules(ctx, -1); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for negative bookId, got %v", err)
	}
	if _, err := svc.GenerateSurgicalDiffs(ctx, 1, []int{0, -2}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation for negative targetParagraph, got %v", err)
	}

	// SaveStyleProfile 默认值补齐
	prof, err := svc.SaveStyleProfile(ctx, domain.UpsertStyleProfileInput{BookID: 1, ToneDirectives: "冷峻干脆"})
	if err != nil || prof.AvgSentenceChars != domain.DefaultAvgSentenceChars || prof.MaxParagraphChars != domain.DefaultMaxParagraphChars || prof.PovRule != domain.PovThirdLimited {
		t.Fatalf("SaveStyleProfile defaults not applied: %+v err=%v", prof, err)
	}

	// ListStyleSamples / Upsert / Delete
	samp, err := svc.UpsertStyleSample(ctx, domain.UpsertStyleSampleInput{
		BookID:    1,
		SceneType: domain.SceneCombat,
		Title:     "剑阵交锋",
		Excerpt:   "青冥剑气横扫石室。",
	})
	if err != nil || samp.CharCount == 0 {
		t.Fatalf("UpsertStyleSample = (%+v, %v)", samp, err)
	}
	samples, err := svc.ListStyleSamples(ctx, 1, "combat")
	if err != nil || samples == nil || ss.lastScene != domain.SceneCombat {
		t.Fatalf("ListStyleSamples = (%+v, %v) scene=%q", samples, err, ss.lastScene)
	}
	if err := svc.DeleteStyleSample(ctx, samp.ID); err != nil || ss.deletedSample != samp.ID {
		t.Fatalf("DeleteStyleSample err=%v deleted=%d", err, ss.deletedSample)
	}

	// ListDeaiRules (bookID=0 valid!) / Upsert / Delete
	rules, err := svc.ListDeaiRules(ctx, 0)
	if err != nil || rules == nil {
		t.Fatalf("ListDeaiRules(0) = (%+v, %v)", rules, err)
	}
	rule, err := svc.UpsertDeaiRule(ctx, domain.UpsertDeaiRuleInput{
		BookID:   1,
		Category: domain.DeaiCategoryCliche,
		Pattern:  "眼神微眯",
		Enabled:  true,
	})
	if err != nil || rule.Severity != domain.LintSeverityWarn {
		t.Fatalf("UpsertDeaiRule = (%+v, %v)", rule, err)
	}
	if err := svc.DeleteDeaiRule(ctx, rule.ID); err != nil || ss.deletedRule != rule.ID {
		t.Fatalf("DeleteDeaiRule err=%v deleted=%d", err, ss.deletedRule)
	}

	// Lint / Critic / GenerateSurgicalDiffs 切片非 nil 归一化
	audit, err := svc.LintChapterLocal(ctx, 42)
	if err != nil || audit.LintIssues == nil || audit.CriticFindings == nil || audit.Diffs == nil {
		t.Fatalf("LintChapterLocal slices not normalized: %+v err=%v", audit, err)
	}

	// ApplySurgicalDiff 采纳时自动触发配置了 ExportDir 的书籍单章导出
	book, _ := lib.CreateBook(ctx, domain.CreateBookInput{Title: "太初剑渊", Genre: domain.GenreXianxia, ExportDir: `E:\Exports`})
	eng.applyRes = domain.ApplySurgicalDiffResult{
		Chapter: domain.Chapter{ID: 42, BookID: book.ID, Content: "手术润色后正文"},
		Audit:   domain.ChapterAudit{ID: 3, ChapterID: 42, DeaiScore: 98},
	}
	res, err := svc.ApplySurgicalDiff(ctx, domain.ApplySurgicalDiffInput{
		ChapterID: 42,
		DiffID:    "diff-14",
		Accept:    true,
	})
	if err != nil || res.Audit.Diffs == nil {
		t.Fatalf("ApplySurgicalDiff = (%+v, %v)", res, err)
	}
	if len(exp.exportedChapters) != 1 || exp.exportedChapters[0] != 42 {
		t.Fatalf("expected ExportChapter triggered for chapter 42 on accepted diff, got %v", exp.exportedChapters)
	}
}

func TestRuntimeCloseCancelsInFlightCriticAudit(t *testing.T) {
	started := make(chan struct{})
	eng := &fakeStyleEngine{
		started: started,
		blockCh: make(chan struct{}),
	}
	rt := NewRuntime()
	rt.Init(RuntimeDeps{StyleEngine: eng})
	svc := NewStyleService(rt)

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.RunCriticAudit(context.Background(), 42)
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("critic audit did not start in time")
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("rt.Close failed: %v", err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled on rt.Close(), got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunCriticAudit did not exit after rt.Close()")
	}
}
