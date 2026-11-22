package store

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"nib/internal/domain"
)

func TestStoryDeadlinesAndChapterTimelineMeta(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "时间线测试", Genre: domain.GenreXianxia})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	chap, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "入门"})

	day7 := 7
	updated, err := st.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
		ID:                  chap.ID,
		Title:               chap.Title,
		EmotionTag:          chap.EmotionTag,
		HookType:            chap.HookType,
		StoryTimeLabel:      "天元三年秋·入宗第7日",
		StoryDay:            &day7,
		UpdateStoryTimeline: true,
	})
	if err != nil {
		t.Fatalf("UpdateChapterMeta with timeline: %v", err)
	}
	if updated.StoryTimeLabel != "天元三年秋·入宗第7日" || updated.StoryDay == nil || *updated.StoryDay != 7 {
		t.Fatalf("unexpected chapter timeline: %+v", updated)
	}

	// 未传 UpdateStoryTimeline 时不得清空已有时间线字段
	updated2, err := st.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
		ID:             chap.ID,
		Title:          "入门改名",
		EmotionTag:     chap.EmotionTag,
		HookType:       chap.HookType,
		OutlineSummary: "只改梗概",
	})
	if err != nil {
		t.Fatalf("UpdateChapterMeta preserve timeline: %v", err)
	}
	if updated2.StoryTimeLabel != "天元三年秋·入宗第7日" || updated2.StoryDay == nil || *updated2.StoryDay != 7 {
		t.Fatalf("expected existing timeline preserved, got %+v", updated2)
	}

	dl, err := st.UpsertStoryDeadline(ctx, domain.UpsertStoryDeadlineInput{
		BookID:          book.ID,
		Title:           "幽冥秘境开启",
		StartChapterSeq: 1,
		TargetDay:       30,
		TargetLabel:     "十月初一",
		Note:            "需在秘境开启前晋升炼气九层",
	})
	if err != nil {
		t.Fatalf("UpsertStoryDeadline: %v", err)
	}
	if dl.Status != domain.StoryDeadlineActive || dl.TargetDay != 30 {
		t.Fatalf("unexpected created deadline: %+v", dl)
	}

	activeList, err := st.ListStoryDeadlines(ctx, book.ID, domain.StoryDeadlineActive)
	if err != nil || len(activeList) != 1 {
		t.Fatalf("ListStoryDeadlines active = (%+v, %v)", activeList, err)
	}

	dlReached, err := st.UpsertStoryDeadline(ctx, domain.UpsertStoryDeadlineInput{
		ID:              dl.ID,
		BookID:          book.ID,
		Title:           dl.Title,
		StartChapterSeq: dl.StartChapterSeq,
		TargetDay:       dl.TargetDay,
		TargetLabel:     dl.TargetLabel,
		Status:          domain.StoryDeadlineReached,
		Note:            "已如期入谷",
	})
	if err != nil || dlReached.Status != domain.StoryDeadlineReached {
		t.Fatalf("update deadline status failed: %+v err=%v", dlReached, err)
	}

	if err := st.DeleteStoryDeadline(ctx, dl.ID); err != nil {
		t.Fatalf("DeleteStoryDeadline: %v", err)
	}
}

func TestEntityRelationsTimeSliceAntiSpoilerGolden(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "关系演变测试", Genre: domain.GenreXuanhuan})
	a, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "林渊"})
	b, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "周明"})

	ch80 := 80
	// 第 1~79 章为同门（ended = 80），第 80 章起变为死敌（effective = 80）
	if _, err := st.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
		BookID:              book.ID,
		SourceEntityID:      a.ID,
		TargetEntityID:      b.ID,
		RelationType:        "同门师兄",
		Description:         "青云宗外门同期",
		EffectiveChapterSeq: 1,
		EndedChapterSeq:     &ch80,
	}); err != nil {
		t.Fatalf("UpsertEntityRelation initial: %v", err)
	}
	if _, err := st.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
		BookID:              book.ID,
		SourceEntityID:      a.ID,
		TargetEntityID:      b.ID,
		RelationType:        "死敌",
		Description:         "秘境夺宝背刺决裂",
		EffectiveChapterSeq: 80,
	}); err != nil {
		t.Fatalf("UpsertEntityRelation future flip: %v", err)
	}

	// 查第 50 章时：仅可见“同门师兄”，严禁提前泄露第 80 章才出现的“死敌”
	at50, err := st.ListEntityRelationsAt(ctx, book.ID, 50, false)
	if err != nil {
		t.Fatalf("ListEntityRelationsAt(50): %v", err)
	}
	if len(at50) != 1 || at50[0].RelationType != "同门师兄" {
		t.Fatalf("chapter 50 spoiler leak or missing relation: %+v", at50)
	}

	// 查第 80 章时：“同门师兄”已结束，“死敌”正式生效
	at80, err := st.ListEntityRelationsAt(ctx, book.ID, 80, false)
	if err != nil {
		t.Fatalf("ListEntityRelationsAt(80): %v", err)
	}
	if len(at80) != 1 || at80[0].RelationType != "死敌" {
		t.Fatalf("chapter 80 expected flipped relation, got %+v", at80)
	}
}

func TestApplyDeltaDecisionsTimelineRelationsAndDeadlines(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "变更单扩展测试", Genre: domain.GenreXianxia})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	ch, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "宗门决裂"})
	e1, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "林渊"})
	e2, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "周明"})
	relOld, _ := st.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
		BookID:              book.ID,
		SourceEntityID:      e1.ID,
		TargetEntityID:      e2.ID,
		RelationType:        "同门",
		EffectiveChapterSeq: 1,
	})

	day12 := 12
	pending, err := st.SavePendingDelta(ctx, domain.ChapterDelta{
		ChapterID:      ch.ID,
		CausalSummary:  "林渊与周明在试炼谷彻底翻脸。",
		StoryTimeLabel: "入宗第十二日",
		StoryDay:       &day12,
		RelationChanges: []domain.DeltaRelationChange{
			{ID: "rel-1", Action: "end", RelationID: relOld.ID, SourceEntityID: e1.ID, SourceName: "林渊", TargetEntityID: e2.ID, TargetName: "周明", RelationType: "同门"},
			{ID: "rel-2", Action: "upsert", SourceEntityID: e1.ID, SourceName: "林渊", TargetEntityID: e2.ID, TargetName: "周明", RelationType: "仇敌", Description: "抢夺灵草未遂"},
		},
		DeadlineChanges: []domain.DeltaDeadlineChange{
			{ID: "dl-1", Action: "create", Title: "生死台决斗", TargetDay: 20, TargetLabel: "二十日之约", Note: "周明下战书"},
		},
	})
	if err != nil {
		t.Fatalf("SavePendingDelta: %v", err)
	}

	res, err := st.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{
		DeltaID:            pending.ID,
		ApplyCausalSummary: true,
		ApplyStoryTimeline: true,
		Decisions: []domain.DeltaDecision{
			{Kind: domain.DeltaDecisionKindRelation, ItemID: "rel-1", Accept: true},
			{Kind: domain.DeltaDecisionKindRelation, ItemID: "rel-2", Accept: true},
			{Kind: domain.DeltaDecisionKindDeadline, ItemID: "dl-1", Accept: true},
		},
	})
	if err != nil {
		t.Fatalf("ApplyDeltaDecisions: %v", err)
	}
	if res.UpdatedRelations != 2 || res.UpdatedDeadlines != 1 {
		t.Fatalf("unexpected updated counts: %+v", res)
	}

	chAfter, _ := st.GetChapter(ctx, ch.ID)
	if chAfter.StoryTimeLabel != "入宗第十二日" || chAfter.StoryDay == nil || *chAfter.StoryDay != 12 {
		t.Fatalf("chapter timeline not applied: %+v", chAfter)
	}
	relsAt1, _ := st.ListEntityRelationsAt(ctx, book.ID, 1, false)
	if len(relsAt1) != 1 || relsAt1[0].RelationType != "仇敌" {
		t.Fatalf("expected active relation flipped to 仇敌 at seq 1, got %+v", relsAt1)
	}
	dls, _ := st.ListStoryDeadlines(ctx, book.ID, domain.StoryDeadlineActive)
	if len(dls) != 1 || dls[0].Title != "生死台决斗" {
		t.Fatalf("expected deadline created, got %+v", dls)
	}
}

func TestEntityMergeBookRenameAndSafetySnapshotUndo(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "nib_m2_safety.db")
	st, err := Open(dbFile)
	if err != nil {
		t.Fatalf("Open disk store: %v", err)
	}
	defer st.Close()

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "安全操作测试", Genre: domain.GenreXuanhuan})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "林风卷"})
	ch1, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "初遇林长老"})
	_, _ = st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID: ch1.ID,
		Content:   "林风与林长老对坐饮茶，旁边林风云也在旁听。",
	})
	ch2, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "林风风云"})
	_, _ = st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID: ch2.ID,
		Content:   "这里只有林风云，不应被误改。",
	})

	target, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:     book.ID,
		Kind:       domain.EntityKindCharacter,
		Name:       "林玄清",
		Aliases:    []string{"青玄真人"},
		Attributes: map[string]string{"realm": "金丹中期"},
	})
	source, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:     book.ID,
		Kind:       domain.EntityKindCharacter,
		Name:       "林长老",
		Summary:    "外门传功长老",
		Attributes: map[string]string{"affiliation": "青云宗"},
	})
	third, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID: book.ID,
		Kind:   domain.EntityKindCharacter,
		Name:   "林风",
	})
	_, _ = st.ReplaceBeats(ctx, ch1.ID, []domain.Beat{
		{
			Stage:       domain.BeatStageSetup,
			Summary:     "林风拜见林长老",
			POVEntityID: &source.ID,
			EntityIDs:   []int64{source.ID, third.ID},
			TargetWords: 600,
		},
	})
	_, _ = st.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
		BookID:            book.ID,
		Title:             "林风身份疑云",
		PlantedChapterSeq: 1,
		Status:            domain.ForeshadowingPlanted,
		InvolvedEntityIDs: []int64{source.ID, third.ID},
	})
	_, _ = st.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
		BookID:         book.ID,
		SourceEntityID: third.ID,
		TargetEntityID: source.ID,
		RelationType:   "师徒",
	})

	// 1. 预览与执行实体合并（将 "林长老" 合并至 "林玄清"，不直接修改正文，只归并别名与引用）
	mergePreview, err := st.PreviewEntityMerge(ctx, domain.EntityMergePreviewInput{
		BookID:         book.ID,
		SourceEntityID: source.ID,
		TargetEntityID: target.ID,
		ReplaceInProse: true,
	})
	if err != nil {
		t.Fatalf("PreviewEntityMerge: %v", err)
	}
	if mergePreview.BeatCount != 1 || mergePreview.ForeshadowCount != 1 || mergePreview.RelationCount != 1 || len(mergePreview.TextHits) != 1 {
		t.Fatalf("unexpected merge preview: %+v", mergePreview)
	}

	mergeRes, err := st.ExecuteEntityMerge(ctx, domain.EntityMergeExecuteInput{
		BookID:         book.ID,
		SourceEntityID: source.ID,
		TargetEntityID: target.ID,
		ReplaceInProse: true,
	})
	if err != nil {
		t.Fatalf("ExecuteEntityMerge: %v", err)
	}
	if mergeRes.SnapshotID <= 0 || mergeRes.MergedRelations != 1 || mergeRes.UpdatedChapters != 1 {
		t.Fatalf("unexpected merge result: %+v", mergeRes)
	}
	if !slices.Contains(mergeRes.TargetEntity.Aliases, "林长老") {
		t.Fatalf("expected source name merged into aliases: %+v", mergeRes.TargetEntity.Aliases)
	}
	if mergeRes.TargetEntity.Attributes["affiliation"] != "青云宗" {
		t.Fatalf("expected missing source attributes merged: %+v", mergeRes.TargetEntity.Attributes)
	}
	// 验证伏笔和节拍无孤儿引用
	beatsAfter, _ := st.ListBeats(ctx, ch1.ID)
	if beatsAfter[0].POVEntityID == nil || *beatsAfter[0].POVEntityID != target.ID || !slices.Contains(beatsAfter[0].EntityIDs, target.ID) {
		t.Fatalf("beat entity references not migrated: %+v", beatsAfter)
	}
	// 验证正文在合并替换前留存了 chapter_versions 快照
	vers, _ := st.ListChapterVersions(ctx, ch1.ID)
	if len(vers) == 0 || !strings.Contains(vers[0].Content, "林长老") {
		t.Fatalf("expected chapter_versions backup before merge prose replace, got %+v", vers)
	}

	// 2. 全书安全改名预览（林风 -> 林寒），并取消勾选 ch2 避免误伤 "林风云"
	renamePrev, err := st.PreviewBookRename(ctx, domain.BookRenamePreviewInput{
		BookID:          book.ID,
		OldName:         "林风",
		NewName:         "林寒",
		IncludeOutlines: true,
		IncludeMemory:   true,
	})
	if err != nil {
		t.Fatalf("PreviewBookRename: %v", err)
	}
	foundCompoundRisk := false
	for _, h := range renamePrev.Hits {
		if h.RiskWarning != "" {
			foundCompoundRisk = true
			break
		}
	}
	if !foundCompoundRisk {
		t.Fatalf("expected compound substring warning for 林风 inside 林风云, got %+v", renamePrev.Hits)
	}

	renameRes, err := st.ExecuteBookRename(ctx, domain.BookRenameExecuteInput{
		BookID:          book.ID,
		OldName:         "林风",
		NewName:         "林寒",
		IncludeOutlines: true,
		IncludeMemory:   true,
		ExcludedHitIDs:  []string{"chapter_content:" + strconv.FormatInt(ch2.ID, 10)},
	})
	if err != nil {
		t.Fatalf("ExecuteBookRename: %v", err)
	}
	if renameRes.SnapshotID <= 0 {
		t.Fatalf("expected safety snapshot id on ExecuteBookRename")
	}
	ch2After, _ := st.GetChapter(ctx, ch2.ID)
	if !strings.Contains(ch2After.Content, "林风云") || strings.Contains(ch2After.Content, "林寒云") {
		t.Fatalf("excluded chapter 2 should remain untouched, got %q", ch2After.Content)
	}

	// 3. 通过 SafetySnapshot 一键回滚到全书改名之前的状态！
	if _, err := st.RestoreSafetySnapshot(ctx, renameRes.SnapshotID); err != nil {
		t.Fatalf("RestoreSafetySnapshot: %v", err)
	}
	ch1Restored, _ := st.GetChapter(ctx, ch1.ID)
	if !strings.Contains(ch1Restored.Content, "林风") || !strings.Contains(ch1Restored.Content, "林玄清") {
		t.Fatalf("expected ch1 restored to post-merge pre-rename state, got %q", ch1Restored.Content)
	}
}
