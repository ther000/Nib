package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nib/internal/domain"
)

func TestPhase1ToPhase2UpgradeBackupAndFTSBackfill(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "nib_upgrade.db")

	allMigs, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("loadEmbeddedMigrations: %v", err)
	}
	if len(allMigs) < 2 {
		t.Fatalf("expected at least 2 migrations, got %d", len(allMigs))
	}
	v1Only := allMigs[:1]

	// 1. 仅用 Phase 1 (001) 初始化旧库并写入章节与设定实体。
	stV1, err := openWithMigrations(dbFile, v1Only)
	if err != nil {
		t.Fatalf("open v1 db: %v", err)
	}
	book := createLegacyBook(t, stV1, "万界剑尊", domain.GenreXuanhuan)
	vol, err := stV1.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "天渊初入"})
	if err != nil {
		t.Fatalf("UpsertVolume v1: %v", err)
	}
	ch1 := createLegacyChapter(
		t,
		stV1,
		book.ID,
		vol.ID,
		1,
		"血玉令牌现世",
		"林渊在黑市地摊发现一枚刻有上古阵纹的血玉令牌。",
		domain.ChapterStatusFinalized,
	)
	if _, err := stV1.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:  book.ID,
		Kind:    domain.EntityKindItem,
		Name:    "血玉令牌",
		Aliases: []string{"古宗血令"},
		Summary: "疑似开启三百年前失踪灵脉密库的钥匙。",
	}); err != nil {
		t.Fatalf("UpsertEntity v1: %v", err)
	}
	if err := stV1.Close(); err != nil {
		t.Fatalf("close v1: %v", err)
	}

	// 2. 用完整迁移（含 002_phase2_memory.sql）升级旧库。
	stV2, err := Open(dbFile)
	if err != nil {
		t.Fatalf("upgrade to v2 failed: %v", err)
	}
	defer stV2.Close()

	// 必须自动生成 .bak.1 备份，且备份文件可读回升级前的 Phase 1 数据。
	bakPath := dbFile + ".bak.1"
	fi, err := os.Stat(bakPath)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("expected non-empty .bak.1 backup at %q, err=%v", bakPath, err)
	}
	bakStore, err := openWithMigrations(bakPath, v1Only)
	if err != nil {
		t.Fatalf("open backup db: %v", err)
	}
	defer bakStore.Close()
	var bakTitle string
	if err := bakStore.db.QueryRowContext(ctx, `SELECT title FROM books WHERE id = ?`, book.ID).Scan(&bakTitle); err != nil || bakTitle != "万界剑尊" {
		t.Fatalf("backup DB missing v1 book: title=%q err=%v", bakTitle, err)
	}

	// 验证迁移内的 FTS5 rebuild 已立即回填升级前就存在的章节和实体（3+ 字查询走 trigram FTS5）。
	hits, err := stV2.SearchMemory(ctx, book.ID, "血玉令牌", 0)
	if err != nil {
		t.Fatalf("SearchMemory after upgrade: %v", err)
	}
	var gotChapter, gotEntity bool
	for _, h := range hits {
		if h.Source == domain.MemorySearchSourceChapter && h.ChapterID == ch1.ID {
			gotChapter = true
		}
		if h.Source == domain.MemorySearchSourceEntity && h.Title == "血玉令牌" {
			gotEntity = true
		}
	}
	if !gotChapter || !gotEntity {
		t.Fatalf("expected FTS5 backfill to find both pre-existing chapter & entity, got hits=%+v", hits)
	}
}

func TestSnapshotAtTimeTravelAndPendingIsolation(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "修真账本测试", Genre: domain.GenreXianxia})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	ch1, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第一章"})
	ch2, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第二章"})
	ch3, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第三章"})
	ch4, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第四章"})

	hero, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID: book.ID,
		Kind:   domain.EntityKindCharacter,
		Name:   "林渊",
		Attributes: map[string]string{
			"cultivation": "炼气九层",
			"injury":      "完好",
		},
	})
	if err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	// Ch2: 受伤 (committed)
	d2, err := st.SavePendingDelta(ctx, domain.ChapterDelta{
		ChapterID: ch2.ID,
		StatePatches: []domain.DeltaStatePatch{
			{ID: "p1", EntityID: hero.ID, EntityName: "林渊", AttrKey: "injury", PrevValue: "完好", NewValue: "左肩阴雷暗伤", Severity: domain.ChangeSeverityNormal},
		},
	})
	if err != nil {
		t.Fatalf("SavePendingDelta ch2: %v", err)
	}
	if _, err := st.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{
		DeltaID:   d2.ID,
		Decisions: []domain.DeltaDecision{{Kind: domain.DeltaDecisionKindStatePatch, ItemID: "p1", Accept: true}},
	}); err != nil {
		t.Fatalf("ApplyDeltaDecisions ch2: %v", err)
	}

	// Ch3: 突破筑基 (pending — 尚未审定)
	if _, err := st.SavePendingDelta(ctx, domain.ChapterDelta{
		ChapterID: ch3.ID,
		StatePatches: []domain.DeltaStatePatch{
			{ID: "p2", EntityID: hero.ID, EntityName: "林渊", AttrKey: "cultivation", PrevValue: "炼气九层", NewValue: "筑基初期", Severity: domain.ChangeSeverityHighRisk},
		},
	}); err != nil {
		t.Fatalf("SavePendingDelta ch3: %v", err)
	}

	// Ch4: 伤愈 (committed — 未来章节事件)
	d4, err := st.SavePendingDelta(ctx, domain.ChapterDelta{
		ChapterID: ch4.ID,
		StatePatches: []domain.DeltaStatePatch{
			{ID: "p3", EntityID: hero.ID, EntityName: "林渊", AttrKey: "injury", PrevValue: "左肩阴雷暗伤", NewValue: "痊愈", Severity: domain.ChangeSeverityNormal},
		},
	})
	if err != nil {
		t.Fatalf("SavePendingDelta ch4: %v", err)
	}
	if _, err := st.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{
		DeltaID:   d4.ID,
		Decisions: []domain.DeltaDecision{{Kind: domain.DeltaDecisionKindStatePatch, ItemID: "p3", Accept: true}},
	}); err != nil {
		t.Fatalf("ApplyDeltaDecisions ch4: %v", err)
	}

	// 时间点 Ch1：只应看到静态底座，严禁透漏 Ch2/Ch3/Ch4。
	snap1, err := st.SnapshotAt(ctx, book.ID, ch1.GlobalSeq, true)
	if err != nil {
		t.Fatalf("SnapshotAt ch1: %v", err)
	}
	if snap1[0].EffectiveAttributes["injury"] != "完好" || snap1[0].EffectiveAttributes["cultivation"] != "炼气九层" {
		t.Fatalf("ch1 snapshot leaked future state: %+v", snap1[0])
	}
	if snap1[0].AppliedEventCount != 0 || snap1[0].PendingEventCount != 0 {
		t.Fatalf("ch1 counts = (%d,%d), want (0,0)", snap1[0].AppliedEventCount, snap1[0].PendingEventCount)
	}

	// 时间点 Ch3 (includePending=false)：看到 Ch2 伤势，不看 Ch3 pending 筑基，不看 Ch4 伤愈。
	snap3Committed, err := st.SnapshotAt(ctx, book.ID, ch3.GlobalSeq, false)
	if err != nil {
		t.Fatalf("SnapshotAt ch3 committed: %v", err)
	}
	if snap3Committed[0].EffectiveAttributes["injury"] != "左肩阴雷暗伤" || snap3Committed[0].EffectiveAttributes["cultivation"] != "炼气九层" {
		t.Fatalf("ch3 committed snapshot mismatch: %+v", snap3Committed[0])
	}
	if snap3Committed[0].AppliedEventCount != 1 || snap3Committed[0].PendingEventCount != 0 {
		t.Fatalf("ch3 committed counts = (%d,%d), want (1,0)", snap3Committed[0].AppliedEventCount, snap3Committed[0].PendingEventCount)
	}

	// 时间点 Ch3 (includePending=true)：合并 Ch3 pending 筑基，仍严格隔离 Ch4 未来伤愈。
	snap3Pending, err := st.SnapshotAt(ctx, book.ID, ch3.GlobalSeq, true)
	if err != nil {
		t.Fatalf("SnapshotAt ch3 pending: %v", err)
	}
	if snap3Pending[0].EffectiveAttributes["injury"] != "左肩阴雷暗伤" || snap3Pending[0].EffectiveAttributes["cultivation"] != "筑基初期" {
		t.Fatalf("ch3 pending snapshot mismatch: %+v", snap3Pending[0])
	}
	if snap3Pending[0].AppliedEventCount != 1 || snap3Pending[0].PendingEventCount != 1 {
		t.Fatalf("ch3 pending counts = (%d,%d), want (1,1)", snap3Pending[0].AppliedEventCount, snap3Pending[0].PendingEventCount)
	}

	// 时间点 Ch4 (includePending=false)：看到伤愈（Ch2->Ch4 折叠），未包含 Ch3 pending。
	snap4, err := st.SnapshotAt(ctx, book.ID, ch4.GlobalSeq, false)
	if err != nil {
		t.Fatalf("SnapshotAt ch4: %v", err)
	}
	if snap4[0].EffectiveAttributes["injury"] != "痊愈" || snap4[0].AppliedEventCount != 2 {
		t.Fatalf("ch4 snapshot mismatch: %+v", snap4[0])
	}

	// ListStateEvents 校验
	eventsUpTo3, err := st.ListStateEvents(ctx, book.ID, hero.ID, ch3.GlobalSeq, true)
	if err != nil {
		t.Fatalf("ListStateEvents: %v", err)
	}
	if len(eventsUpTo3) != 2 || eventsUpTo3[0].ChapterSeq != 2 || eventsUpTo3[1].ChapterSeq != 3 {
		t.Fatalf("ListStateEvents upTo3 unexpected: %+v", eventsUpTo3)
	}
}

func TestSearchMemoryFTS5AndShortCJKFallbackAndSpoilerGuard(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "检索测试", Genre: domain.GenreXuanhuan})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	ch1, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "残碑觉醒"})
	ch2, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "大荒秘境"})

	if _, err := st.SaveChapterContent(ctx, ch1.ID, "林渊手持青冥剑，在残碑前悟出大荒囚天指第一重。", domain.ChapterStatusFinalized, domain.VersionSourceManualBackup, "", ""); err != nil {
		t.Fatalf("save ch1: %v", err)
	}
	if _, err := st.SaveChapterContent(ctx, ch2.ID, "林渊在大荒秘境深处斩杀黑煞长老，大荒囚天指再进一层。", domain.ChapterStatusFinalized, domain.VersionSourceManualBackup, "", ""); err != nil {
		t.Fatalf("save ch2: %v", err)
	}
	if _, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:  book.ID,
		Kind:    domain.EntityKindCharacter,
		Name:    "林渊",
		Aliases: []string{"林执事"},
		Summary: "掌握大荒囚天指的外门弟子。",
	}); err != nil {
		t.Fatalf("upsert entity: %v", err)
	}

	// 1. 3+ 字走 FTS5 trigram（带 maxChapterSeq=1 严防第 2 章剧透）
	ftsHits, err := st.SearchMemory(ctx, book.ID, "大荒囚天指", 1)
	if err != nil {
		t.Fatalf("SearchMemory FTS5: %v", err)
	}
	var foundCh1, foundCh2, foundEnt bool
	for _, h := range ftsHits {
		if h.Source == domain.MemorySearchSourceChapter && h.ChapterID == ch1.ID {
			foundCh1 = true
		}
		if h.Source == domain.MemorySearchSourceChapter && h.ChapterID == ch2.ID {
			foundCh2 = true
		}
		if h.Source == domain.MemorySearchSourceEntity && h.Title == "林渊" {
			foundEnt = true
		}
	}
	if !foundCh1 || !foundEnt {
		t.Fatalf("FTS5 search missed expected ch1 or entity: %+v", ftsHits)
	}
	if foundCh2 {
		t.Fatalf("FTS5 search leaked future chapter ch2 beyond maxChapterSeq=1: %+v", ftsHits)
	}

	// 2. 2 字中文人名“林渊”必须自动走 LIKE 回退（trigram 否则会漏召回），同时遵守 maxChapterSeq=1。
	shortHits, err := st.SearchMemory(ctx, book.ID, "林渊", 1)
	if err != nil {
		t.Fatalf("SearchMemory 2-char LIKE: %v", err)
	}
	foundCh1, foundCh2, foundEnt = false, false, false
	for _, h := range shortHits {
		if h.Source == domain.MemorySearchSourceChapter && h.ChapterID == ch1.ID {
			foundCh1 = true
			if !strings.Contains(h.Snippet, "【林渊】") {
				t.Fatalf("expected highlighted snippet, got %q", h.Snippet)
			}
		}
		if h.Source == domain.MemorySearchSourceChapter && h.ChapterID == ch2.ID {
			foundCh2 = true
		}
		if h.Source == domain.MemorySearchSourceEntity && h.Title == "林渊" {
			foundEnt = true
		}
	}
	if !foundCh1 || !foundEnt {
		t.Fatalf("2-char LIKE fallback missed expected hits: %+v", shortHits)
	}
	if foundCh2 {
		t.Fatalf("2-char LIKE fallback leaked future chapter ch2: %+v", shortHits)
	}

	// 3. 含 FTS5 特殊符号的查询不得报错
	if _, err := st.SearchMemory(ctx, book.ID, `大荒"OR NOT*`, 0); err != nil {
		t.Fatalf("SearchMemory with FTS5 syntax chars should not error: %v", err)
	}
}

func TestDeltaWorkflowDecisionsOverridesAndUpstreamDirty(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "变更单闭环", Genre: domain.GenreXianxia})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	ch1, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第1章"})
	ch2, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第2章"})
	ch3, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第3章"})
	ch4, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第4章"})

	// 设置下游章节不同状态：ch2=outlined（应标脏），ch3=reviewing（应标脏），ch4=finalized（不应标脏）
	_, _ = st.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{ID: ch2.ID, Title: ch2.Title, Status: domain.ChapterStatusOutlined})
	_, _ = st.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{ID: ch3.ID, Title: ch3.Title, Status: domain.ChapterStatusReviewing})
	_, _ = st.FinalizeChapter(ctx, ch4.ID)

	hero, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:     book.ID,
		Kind:       domain.EntityKindCharacter,
		Name:       "林渊",
		Aliases:    []string{"林执事"},
		Attributes: map[string]string{"injury": "完好", "realm": "筑基初期"},
	})
	oldFS, err := st.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
		BookID:            book.ID,
		Title:             "残剑异动",
		PlantedChapterSeq: 1,
		TargetResolveSeq:  5,
		Status:            domain.ForeshadowingPlanted,
	})
	if err != nil {
		t.Fatalf("UpsertForeshadowing: %v", err)
	}

	// 无待审变更单时应返回 (nil, nil)
	none, err := st.GetPendingDelta(ctx, ch1.ID)
	if err != nil || none != nil {
		t.Fatalf("expected (nil, nil) when no pending delta, got (%+v, %v)", none, err)
	}

	// 保存待审变更单：包含 1 个新实体（破障丹）+ 3 条状态补丁（1 条按别名匹配既有实体、1 条被驳回、1 条指向新实体）+ 2 条伏笔（新埋设 + 回收旧伏笔）
	saved, err := st.SavePendingDelta(ctx, domain.ChapterDelta{
		ChapterID:     ch1.ID,
		CausalSummary: "林渊反杀黑煞执事，夺得破障丹。",
		NewEntities: []domain.DeltaNewEntity{
			{ID: "ent-1", Kind: domain.EntityKindItem, Name: "破障丹", Summary: "助筑基修士冲关的灵丹"},
		},
		StatePatches: []domain.DeltaStatePatch{
			{ID: "patch-1", EntityName: "林执事", AttrKey: "injury", PrevValue: "完好", NewValue: "左肩重伤", Severity: domain.ChangeSeverityNormal},
			{ID: "patch-2", EntityID: hero.ID, EntityName: "林渊", AttrKey: "realm", PrevValue: "筑基初期", NewValue: "元婴大圆满", Severity: domain.ChangeSeverityHighRisk},
			{ID: "patch-3", EntityName: "破障丹", AttrKey: "owner", PrevValue: "黑煞执事", NewValue: "林渊", Severity: domain.ChangeSeverityHighRisk},
		},
		Foreshadows: []domain.DeltaForeshadowUpdate{
			{ID: "fs-1", Action: domain.DeltaForeshadowPlant, Title: "血玉密库", Description: "三百年前古宗遗藏", TargetResolveSeq: 20},
			{ID: "fs-2", Action: domain.DeltaForeshadowResolve, ForeshadowingID: oldFS.ID, Title: oldFS.Title},
		},
	})
	if err != nil {
		t.Fatalf("SavePendingDelta: %v", err)
	}
	if saved.HighRiskFlags != 2 || saved.ReviewStatus != domain.DeltaReviewPending {
		t.Fatalf("unexpected saved delta: %+v", saved)
	}
	// 按别名自动解析出了 hero.ID
	if saved.StatePatches[0].EntityID != hero.ID {
		t.Fatalf("expected alias lookup to resolve hero.ID=%d, got %d", hero.ID, saved.StatePatches[0].EntityID)
	}

	gotPending, err := st.GetPendingDelta(ctx, ch1.ID)
	if err != nil || gotPending == nil || gotPending.ID != saved.ID {
		t.Fatalf("GetPendingDelta mismatch: %+v err=%v", gotPending, err)
	}

	// 作者审定：采纳因果摘要(带覆盖)、采纳新实体破障丹、采纳 patch-1(带值覆盖)、驳回 patch-2、采纳 patch-3、采纳两条伏笔
	res, err := st.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{
		DeltaID:               saved.ID,
		ApplyCausalSummary:    true,
		CausalSummaryOverride: "林渊借阵反杀执事，夺破障丹并轻伤左肩。",
		Decisions: []domain.DeltaDecision{
			{Kind: domain.DeltaDecisionKindNewEntity, ItemID: "ent-1", Accept: true},
			{Kind: domain.DeltaDecisionKindStatePatch, ItemID: "patch-1", Accept: true, OverrideNewValue: "左肩轻伤"},
			{Kind: domain.DeltaDecisionKindStatePatch, ItemID: "patch-2", Accept: false},
			{Kind: domain.DeltaDecisionKindStatePatch, ItemID: "patch-3", Accept: true},
			{Kind: domain.DeltaDecisionKindForeshadow, ItemID: "fs-1", Accept: true},
			{Kind: domain.DeltaDecisionKindForeshadow, ItemID: "fs-2", Accept: true},
		},
	})
	if err != nil {
		t.Fatalf("ApplyDeltaDecisions: %v", err)
	}
	if res.Delta.ReviewStatus != domain.DeltaReviewApplied || res.Delta.ReviewedAt == "" {
		t.Fatalf("delta not marked applied: %+v", res.Delta)
	}
	if res.CommittedEvents != 2 || res.UpdatedForeshadowings != 2 {
		t.Fatalf("counts mismatch: committed=%d fs=%d", res.CommittedEvents, res.UpdatedForeshadowings)
	}
	// 下游 ch2/ch3 应标脏，ch4(finalized) 不应标脏
	if len(res.MarkedDirtyChapterIDs) != 2 || res.MarkedDirtyChapterIDs[0] != ch2.ID || res.MarkedDirtyChapterIDs[1] != ch3.ID {
		t.Fatalf("MarkedDirtyChapterIDs = %v, want [%d, %d]", res.MarkedDirtyChapterIDs, ch2.ID, ch3.ID)
	}

	// 验证重复提交被拒
	if _, err := st.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{DeltaID: saved.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("double apply err = %v, want ErrConflict", err)
	}

	// 验证 ch1 因果摘要已更新，ch2/ch3 已变 upstream_dirty，ch4 仍是 finalized
	ch1After, _ := st.GetChapter(ctx, ch1.ID)
	ch2After, _ := st.GetChapter(ctx, ch2.ID)
	ch4After, _ := st.GetChapter(ctx, ch4.ID)
	if ch1After.CausalSummary != "林渊借阵反杀执事，夺破障丹并轻伤左肩。" {
		t.Fatalf("ch1 causal_summary not updated: %q", ch1After.CausalSummary)
	}
	if ch2After.Status != domain.ChapterStatusUpstreamDirty || ch4After.Status != domain.ChapterStatusFinalized {
		t.Fatalf("downstream statuses wrong: ch2=%s ch4=%s", ch2After.Status, ch4After.Status)
	}

	// 验证快照：林渊 injury 已覆盖为“左肩轻伤”，realm 仍为“筑基初期”(patch-2 被驳回)，新实体“破障丹”已落库且 owner=林渊
	snaps, err := st.SnapshotAt(ctx, book.ID, 1, true)
	if err != nil {
		t.Fatalf("SnapshotAt after apply: %v", err)
	}
	var sawHero, sawPill bool
	for _, s := range snaps {
		if s.PendingEventCount != 0 {
			t.Fatalf("expected 0 pending events after apply, got %+v", s)
		}
		if s.Entity.Name == "林渊" {
			sawHero = true
			if s.EffectiveAttributes["injury"] != "左肩轻伤" || s.EffectiveAttributes["realm"] != "筑基初期" {
				t.Fatalf("hero attrs mismatch: %+v", s.EffectiveAttributes)
			}
		}
		if s.Entity.Name == "破障丹" {
			sawPill = true
			if s.EffectiveAttributes["owner"] != "林渊" {
				t.Fatalf("new entity patch not committed: %+v", s.EffectiveAttributes)
			}
		}
	}
	if !sawHero || !sawPill {
		t.Fatalf("missing expected entities in snapshot: hero=%v pill=%v", sawHero, sawPill)
	}

	// 验证伏笔状态与 ListActiveForeshadowings 排序
	activeFS, err := st.ListActiveForeshadowings(ctx, book.ID, 10)
	if err != nil {
		t.Fatalf("ListActiveForeshadowings: %v", err)
	}
	if len(activeFS) != 1 || activeFS[0].Title != "血玉密库" {
		t.Fatalf("expected only 血玉密库 active (残剑异动 resolved), got %+v", activeFS)
	}
	resolvedFS, err := st.ListForeshadowings(ctx, book.ID, domain.ForeshadowingResolved)
	if err != nil {
		t.Fatalf("ListForeshadowings resolved: %v", err)
	}
	if len(resolvedFS) != 1 || resolvedFS[0].ID != oldFS.ID || resolvedFS[0].ResolvedChapterSeq != 1 {
		t.Fatalf("resolved foreshadowing mismatch: %+v", resolvedFS)
	}

	// 验证全部驳回时 review_status == dismissed 且不标脏下游
	saved2, err := st.SavePendingDelta(ctx, domain.ChapterDelta{
		ChapterID: ch2.ID,
		StatePatches: []domain.DeltaStatePatch{
			{ID: "p-reject", EntityID: hero.ID, EntityName: "林渊", AttrKey: "injury", NewValue: "误抽状态"},
		},
	})
	if err != nil {
		t.Fatalf("SavePendingDelta ch2: %v", err)
	}
	dismissRes, err := st.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{
		DeltaID:            saved2.ID,
		ApplyCausalSummary: false,
		Decisions:          []domain.DeltaDecision{{Kind: domain.DeltaDecisionKindStatePatch, ItemID: "p-reject", Accept: false}},
	})
	if err != nil {
		t.Fatalf("ApplyDeltaDecisions dismiss: %v", err)
	}
	if dismissRes.Delta.ReviewStatus != domain.DeltaReviewDismissed || len(dismissRes.MarkedDirtyChapterIDs) != 0 {
		t.Fatalf("dismiss result unexpected: %+v", dismissRes)
	}
}

func TestArcSummariesAndForeshadowingCRUD(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "摘要与伏笔", Genre: domain.GenreUrban})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})

	// Foreshadowing CRUD + overdue ordering
	fsLate, err := st.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
		BookID:            book.ID,
		Title:             "远期伏笔",
		PlantedChapterSeq: 2,
		TargetResolveSeq:  30,
	})
	if err != nil {
		t.Fatalf("UpsertForeshadowing late: %v", err)
	}
	fsOverdue, err := st.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
		BookID:            book.ID,
		Title:             "超期伏笔",
		PlantedChapterSeq: 1,
		TargetResolveSeq:  5,
		InvolvedEntityIDs: []int64{10, 20, 10},
	})
	if err != nil {
		t.Fatalf("UpsertForeshadowing overdue: %v", err)
	}
	fsNoTarget, err := st.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
		BookID:            book.ID,
		Title:             "无期限伏笔",
		PlantedChapterSeq: 1,
		TargetResolveSeq:  0,
	})
	if err != nil {
		t.Fatalf("UpsertForeshadowing no target: %v", err)
	}
	if len(fsOverdue.InvolvedEntityIDs) != 2 {
		t.Fatalf("expected deduplicated involvedEntityIds, got %v", fsOverdue.InvolvedEntityIDs)
	}

	active, err := st.ListActiveForeshadowings(ctx, book.ID, 12)
	if err != nil {
		t.Fatalf("ListActiveForeshadowings: %v", err)
	}
	if len(active) != 3 || active[0].ID != fsOverdue.ID || active[1].ID != fsLate.ID || active[2].ID != fsNoTarget.ID {
		t.Fatalf("active foreshadowings ordering wrong: %+v", active)
	}

	if err := st.DeleteForeshadowing(ctx, fsNoTarget.ID); err != nil {
		t.Fatalf("DeleteForeshadowing: %v", err)
	}
	if err := st.DeleteForeshadowing(ctx, fsNoTarget.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second DeleteForeshadowing err = %v, want ErrNotFound", err)
	}

	// Arc summaries CRUD
	arc, err := st.UpsertArcSummary(ctx, domain.UpsertArcSummaryInput{
		BookID:   book.ID,
		Level:    domain.ArcSummaryLevelArc10,
		StartSeq: 1,
		EndSeq:   10,
		Summary:  "前十章完成创业原始积累。",
	})
	if err != nil {
		t.Fatalf("UpsertArcSummary arc_10: %v", err)
	}
	volID := vol.ID
	volSum, err := st.UpsertArcSummary(ctx, domain.UpsertArcSummaryInput{
		BookID:   book.ID,
		Level:    domain.ArcSummaryLevelVolume,
		VolumeID: &volID,
		StartSeq: 1,
		EndSeq:   40,
		Summary:  "第一卷总览。",
	})
	if err != nil {
		t.Fatalf("UpsertArcSummary volume: %v", err)
	}
	updatedArc, err := st.UpsertArcSummary(ctx, domain.UpsertArcSummaryInput{
		ID:       arc.ID,
		BookID:   book.ID,
		Level:    domain.ArcSummaryLevelArc10,
		StartSeq: 1,
		EndSeq:   10,
		Summary:  "前十章完成原始积累并拿下首轮融资。",
	})
	if err != nil {
		t.Fatalf("UpsertArcSummary update: %v", err)
	}
	if updatedArc.Summary != "前十章完成原始积累并拿下首轮融资。" {
		t.Fatalf("updated arc summary mismatch: %+v", updatedArc)
	}

	onlyArc10, err := st.ListArcSummaries(ctx, book.ID, domain.ArcSummaryLevelArc10)
	if err != nil {
		t.Fatalf("ListArcSummaries arc_10: %v", err)
	}
	if len(onlyArc10) != 1 || onlyArc10[0].ID != arc.ID {
		t.Fatalf("onlyArc10 mismatch: %+v", onlyArc10)
	}
	allSums, err := st.ListArcSummaries(ctx, book.ID, "")
	if err != nil {
		t.Fatalf("ListArcSummaries all: %v", err)
	}
	if len(allSums) != 2 || volSum.VolumeID == nil || *volSum.VolumeID != vol.ID {
		t.Fatalf("allSums mismatch: %+v", allSums)
	}
}

func TestSearchMemoryMatchesAttributesForLongQueriesAndV10Indexes(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "属性检索测试", Genre: domain.GenreXuanhuan})
	_, _ = st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:  book.ID,
		Kind:    domain.EntityKindCharacter,
		Name:    "林渊",
		Summary: "主角",
		Attributes: map[string]string{
			"境界": "元婴大圆满",
			"功法": "九霄御雷诀",
		},
	})

	// >= 3 字关键词仅出现在 attributes_json 中，FTS5+LIKE 并集必须召回
	hits, err := st.SearchMemory(ctx, book.ID, "九霄御雷诀", 0)
	if err != nil {
		t.Fatalf("SearchMemory: %v", err)
	}
	if len(hits) == 0 || hits[0].Source != domain.MemorySearchSourceEntity || hits[0].Title != "林渊" {
		t.Fatalf("expected attribute recall for >=3 rune query, got %+v", hits)
	}

	// 验证 010 迁移索引已创建
	var idxCount int
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name IN (
			'idx_chapters_book_status',
			'idx_state_events_book_status_seq',
			'idx_batch_runs_book_id',
			'idx_entity_relations_src'
		)
	`).Scan(&idxCount); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if idxCount != 4 {
		t.Fatalf("expected 4 sampled v10 indexes, got %d", idxCount)
	}
}
