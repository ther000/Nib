package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nib/internal/domain"
)

func TestPhase2ToPhase3UpgradeBackupAndSeededDeaiRules(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "nib_p2_to_p3.db")

	allMigs, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("loadEmbeddedMigrations: %v", err)
	}
	if len(allMigs) < 3 {
		t.Fatalf("expected at least 3 migrations, got %d", len(allMigs))
	}
	v2Only := allMigs[:2]

	// 1. 先以 Phase 2 (001+002) 初始化数据库并写入一本书。
	stV2, err := openWithMigrations(dbFile, v2Only)
	if err != nil {
		t.Fatalf("open v2 db: %v", err)
	}
	book, err := stV2.CreateBook(ctx, domain.CreateBookInput{Title: "修真去AI味测试", Genre: domain.GenreXianxia})
	if err != nil {
		t.Fatalf("CreateBook v2: %v", err)
	}
	if err := stV2.Close(); err != nil {
		t.Fatalf("close v2: %v", err)
	}

	// 2. 用完整迁移（含 003_phase3_style.sql）升级到 Phase 3。
	stV3, err := Open(dbFile)
	if err != nil {
		t.Fatalf("upgrade to v3 failed: %v", err)
	}
	defer stV3.Close()

	// 必须自动生成 .bak.2 备份。
	bakPath := dbFile + ".bak.2"
	fi, err := os.Stat(bakPath)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("expected non-empty .bak.2 backup at %q, err=%v", bakPath, err)
	}
	bakStore, err := openWithMigrations(bakPath, v2Only)
	if err != nil {
		t.Fatalf("open .bak.2 db: %v", err)
	}
	defer bakStore.Close()
	bakBook, err := bakStore.GetBook(ctx, book.ID)
	if err != nil || bakBook.Title != "修真去AI味测试" {
		t.Fatalf("backup DB missing v2 book: %+v err=%v", bakBook, err)
	}

	// 3. 验证 003_phase3_style.sql 已预置全部全局去 AI 味默认规则（book_id = 0）。
	globalRules, err := stV3.ListDeaiRules(ctx, 0)
	if err != nil {
		t.Fatalf("ListDeaiRules(0): %v", err)
	}
	if len(globalRules) != 19 {
		t.Fatalf("expected 19 seeded global deai_rules, got %d", len(globalRules))
	}

	byCategory := make(map[domain.DeaiCategory]int)
	for _, r := range globalRules {
		if r.BookID != 0 || !r.Enabled {
			t.Fatalf("unexpected seeded global rule state: %+v", r)
		}
		byCategory[r.Category]++
	}
	if byCategory[domain.DeaiCategoryCliche] != 14 ||
		byCategory[domain.DeaiCategoryStructure] != 3 ||
		byCategory[domain.DeaiCategoryFormatting] != 2 {
		t.Fatalf("unexpected seeded rule counts by category: %+v", byCategory)
	}
}

func TestStyleProfileDefaultFallbackAndUpsert(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	// 不存在的书籍必须返回 ErrNotFound
	if _, err := st.GetStyleProfile(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetStyleProfile non-existent book err = %v, want ErrNotFound", err)
	}

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "文风档案测试", Genre: domain.GenreXuanhuan})

	// 尚未保存时应返回 DefaultStyleProfile(book.ID)
	defProf, err := st.GetStyleProfile(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetStyleProfile default: %v", err)
	}
	if defProf.BookID != book.ID ||
		defProf.AvgSentenceChars != domain.DefaultAvgSentenceChars ||
		defProf.MaxParagraphChars != domain.DefaultMaxParagraphChars ||
		defProf.PovRule != domain.PovThirdLimited {
		t.Fatalf("default style profile mismatch: %+v", defProf)
	}

	// 首次保存与二次更新（Upsert）
	saved, err := st.SaveStyleProfile(ctx, domain.UpsertStyleProfileInput{
		BookID:              book.ID,
		AvgSentenceChars:    16.5,
		MaxParagraphChars:   120,
		DialogueRatioMin:    0.30,
		DialogueRatioMax:    0.50,
		PovRule:             domain.PovThirdLimited,
		ToneDirectives:      "冷峻克制，短句出招",
		ForbiddenTropesNote: "禁写旁观者无脑嘲讽",
	})
	if err != nil {
		t.Fatalf("SaveStyleProfile insert: %v", err)
	}
	if saved.ID <= 0 || saved.AvgSentenceChars != 16.5 || saved.MaxParagraphChars != 120 || saved.ToneDirectives != "冷峻克制，短句出招" {
		t.Fatalf("saved style profile mismatch: %+v", saved)
	}

	updated, err := st.SaveStyleProfile(ctx, domain.UpsertStyleProfileInput{
		BookID:              book.ID,
		AvgSentenceChars:    19.0,
		MaxParagraphChars:   135,
		DialogueRatioMin:    0.25,
		DialogueRatioMax:    0.45,
		PovRule:             domain.PovFirstPerson,
		ToneDirectives:      "第一人称快节奏叙述",
		ForbiddenTropesNote: "禁写心理内耗",
	})
	if err != nil {
		t.Fatalf("SaveStyleProfile update: %v", err)
	}
	if updated.ID != saved.ID || updated.PovRule != domain.PovFirstPerson || updated.AvgSentenceChars != 19.0 {
		t.Fatalf("updated style profile mismatch: %+v", updated)
	}
}

func TestStyleSamplesCRUDAndSceneFiltering(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "样章库测试", Genre: domain.GenreXuanhuan})

	s1, err := st.UpsertStyleSample(ctx, domain.UpsertStyleSampleInput{
		BookID:    book.ID,
		SceneType: domain.SceneCombat,
		Title:     "狭巷短兵相接",
		Excerpt:   "铁剑擦着砖墙掠过，带起一串火星。\n林渊手腕一翻，剑柄重重砸在对方喉结。",
	})
	if err != nil {
		t.Fatalf("UpsertStyleSample combat: %v", err)
	}
	if s1.CharCount != domain.CountWords(s1.Excerpt) || s1.CharCount == 0 {
		t.Fatalf("unexpected charCount=%d for excerpt=%q", s1.CharCount, s1.Excerpt)
	}

	s2, err := st.UpsertStyleSample(ctx, domain.UpsertStyleSampleInput{
		BookID:    book.ID,
		SceneType: domain.SceneDialogue,
		Title:     "茶楼暗语试探",
		Excerpt:   "“王执事这杯茶，怕是不好喝。”林渊指尖轻叩桌面，没有抬头。",
	})
	if err != nil {
		t.Fatalf("UpsertStyleSample dialogue: %v", err)
	}

	// 按场景筛选
	combatOnly, err := st.ListStyleSamples(ctx, book.ID, domain.SceneCombat)
	if err != nil {
		t.Fatalf("ListStyleSamples(combat): %v", err)
	}
	if len(combatOnly) != 1 || combatOnly[0].ID != s1.ID {
		t.Fatalf("combatOnly mismatch: %+v", combatOnly)
	}

	allSamples, err := st.ListStyleSamples(ctx, book.ID, "")
	if err != nil {
		t.Fatalf("ListStyleSamples(all): %v", err)
	}
	if len(allSamples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(allSamples))
	}

	// 更新样章
	updatedS2, err := st.UpsertStyleSample(ctx, domain.UpsertStyleSampleInput{
		ID:        s2.ID,
		BookID:    book.ID,
		SceneType: domain.SceneSuspense,
		Title:     "茶楼杀机",
		Excerpt:   "窗外雨声骤停，三道神识同时锁死了二楼楼梯。",
	})
	if err != nil {
		t.Fatalf("UpsertStyleSample update: %v", err)
	}
	if updatedS2.SceneType != domain.SceneSuspense || updatedS2.Title != "茶楼杀机" {
		t.Fatalf("updated sample mismatch: %+v", updatedS2)
	}

	if err := st.DeleteStyleSample(ctx, s1.ID); err != nil {
		t.Fatalf("DeleteStyleSample: %v", err)
	}
	if err := st.DeleteStyleSample(ctx, s1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second DeleteStyleSample err = %v, want ErrNotFound", err)
	}
}

func TestDeaiRulesListingToggleAndGlobalDeleteProtection(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "规则测试", Genre: domain.GenreUrban})

	globalRules, err := st.ListDeaiRules(ctx, 0)
	if err != nil || len(globalRules) == 0 {
		t.Fatalf("ListDeaiRules(0): len=%d err=%v", len(globalRules), err)
	}
	firstGlobal := globalRules[0]

	// 1. 严禁删除全局默认规则（book_id = 0）
	if err := st.DeleteDeaiRule(ctx, firstGlobal.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("DeleteDeaiRule on global rule err = %v, want ErrValidation", err)
	}

	// 2. 允许通过 UpsertDeaiRule 禁用全局默认规则或调整阈值
	toggled, err := st.UpsertDeaiRule(ctx, domain.UpsertDeaiRuleInput{
		ID:            firstGlobal.ID,
		BookID:        0,
		Category:      firstGlobal.Category,
		Pattern:       firstGlobal.Pattern,
		IsRegex:       firstGlobal.IsRegex,
		Severity:      domain.LintSeverityWarn,
		MaxPerChapter: 2,
		Hint:          firstGlobal.Hint,
		Enabled:       false,
	})
	if err != nil {
		t.Fatalf("UpsertDeaiRule toggle global: %v", err)
	}
	if toggled.Enabled || toggled.MaxPerChapter != 2 || toggled.Severity != domain.LintSeverityWarn || toggled.BookID != 0 {
		t.Fatalf("toggled global rule mismatch: %+v", toggled)
	}

	// 3. 非法正则必须被拦截
	_, err = st.UpsertDeaiRule(ctx, domain.UpsertDeaiRuleInput{
		BookID:   book.ID,
		Category: domain.DeaiCategoryCliche,
		Pattern:  "[unclosed-regex",
		IsRegex:  true,
		Enabled:  true,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("invalid regex err = %v, want ErrValidation", err)
	}

	// 4. 新增书籍专属规则，ListDeaiRules(book.ID) 应同时返回全局规则 + 本书专属规则
	custom, err := st.UpsertDeaiRule(ctx, domain.UpsertDeaiRuleInput{
		BookID:        book.ID,
		Category:      domain.DeaiCategoryCliche,
		Pattern:       "恐怖如斯",
		IsRegex:       false,
		Severity:      domain.LintSeverityError,
		MaxPerChapter: 0,
		Hint:          "禁用斗破古早梗词",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("UpsertDeaiRule create custom: %v", err)
	}

	combined, err := st.ListDeaiRules(ctx, book.ID)
	if err != nil {
		t.Fatalf("ListDeaiRules(book.ID): %v", err)
	}
	if len(combined) != len(globalRules)+1 {
		t.Fatalf("len(combined) = %d, want %d", len(combined), len(globalRules)+1)
	}

	// 5. 书籍专属规则可正常删除
	if err := st.DeleteDeaiRule(ctx, custom.ID); err != nil {
		t.Fatalf("DeleteDeaiRule custom: %v", err)
	}
	if err := st.DeleteDeaiRule(ctx, custom.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second DeleteDeaiRule custom err = %v, want ErrNotFound", err)
	}
}

func TestChapterAuditDefaultAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	if _, err := st.GetChapterAudit(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetChapterAudit non-existent chapter err = %v, want ErrNotFound", err)
	}

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "审校测试", Genre: domain.GenreXuanhuan})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	chap, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第1章"})

	// 尚未保存质检报告时返回默认满分空报告
	defAudit, err := st.GetChapterAudit(ctx, chap.ID)
	if err != nil {
		t.Fatalf("GetChapterAudit default: %v", err)
	}
	if defAudit.ChapterID != chap.ID || defAudit.DeaiScore != 100 || len(defAudit.LintIssues) != 0 || len(defAudit.CriticFindings) != 0 || len(defAudit.Diffs) != 0 {
		t.Fatalf("default audit mismatch: %+v", defAudit)
	}

	// 保存完整质检与手术润色报告并读回验证
	saved, err := st.SaveChapterAudit(ctx, domain.ChapterAudit{
		ChapterID: chap.ID,
		LintIssues: []domain.LintIssue{
			{
				RuleID:         "rule-1",
				Category:       domain.DeaiCategoryCliche,
				Severity:       domain.LintSeverityError,
				ParagraphIndex: 2,
				StartRune:      4,
				EndRune:        12,
				MatchedText:    "嘴角勾起一抹弧度",
				Hint:           "删去套话",
			},
		},
		CriticFindings: []domain.CriticFinding{
			{
				ID:             "cf-1",
				Dimension:      domain.CriticDimConsistency,
				Severity:       domain.LintSeverityError,
				ParagraphIndex: 5,
				Title:          "伤势前后矛盾",
				Detail:         "左肩刚受重伤却双手抡重剑",
				Suggestion:     "改为单手执剑支撑",
			},
		},
		Diffs: []domain.SurgicalDiff{
			{
				ID:             "diff-2",
				ParagraphIndex: 2,
				Reason:         "去除嘴角勾起套话",
				Original:       "林渊嘴角勾起一抹弧度。",
				Replacement:    "林渊收起阵盘，转身走入暗巷。",
				Status:         domain.DiffStatusPending,
			},
		},
		HookPassed:  true,
		HookVerdict: "结尾悬念紧扣血玉令牌异动，钩子扎实。",
		DeaiScore:   86,
	})
	if err != nil {
		t.Fatalf("SaveChapterAudit: %v", err)
	}
	if saved.ID <= 0 || !saved.HookPassed || saved.HookVerdict == "" || saved.DeaiScore != 86 {
		t.Fatalf("saved audit header mismatch: %+v", saved)
	}
	if len(saved.LintIssues) != 1 || saved.LintIssues[0].MatchedText != "嘴角勾起一抹弧度" {
		t.Fatalf("saved LintIssues mismatch: %+v", saved.LintIssues)
	}
	if len(saved.CriticFindings) != 1 || saved.CriticFindings[0].Dimension != domain.CriticDimConsistency {
		t.Fatalf("saved CriticFindings mismatch: %+v", saved.CriticFindings)
	}
	if len(saved.Diffs) != 1 || saved.Diffs[0].Status != domain.DiffStatusPending {
		t.Fatalf("saved Diffs mismatch: %+v", saved.Diffs)
	}

	// 更新 Diff 状态并再次持久化
	saved.Diffs[0].Status = domain.DiffStatusAccepted
	saved.DeaiScore = 98
	updated, err := st.SaveChapterAudit(ctx, saved)
	if err != nil {
		t.Fatalf("SaveChapterAudit update: %v", err)
	}
	if updated.ID != saved.ID || updated.DeaiScore != 98 || updated.Diffs[0].Status != domain.DiffStatusAccepted {
		t.Fatalf("updated audit mismatch: %+v", updated)
	}
}
