package store

import (
	"context"
	"testing"

	"nib/internal/domain"
)

func TestM4ConsistencyReportsAndDirtyQueueLifecycle(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "M4 体检测试", Genre: domain.GenreXianxia})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	vol, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	if err != nil {
		t.Fatalf("UpsertVolume: %v", err)
	}

	ch1, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第一章"})
	_, _ = st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{ChapterID: ch1.ID, Title: ch1.Title, Content: "林渊破境筑基。", Status: domain.ChapterStatusFinalized})
	ch2, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第二章"})
	_, _ = st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{ChapterID: ch2.ID, Title: ch2.Title, Content: "第二章原有正文。", Status: domain.ChapterStatusReviewing})
	ch3, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第三章", OutlineSummary: "第三章既有章纲"})
	_, _ = st.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{ID: ch3.ID, Title: ch3.Title, Status: domain.ChapterStatusOutlined, EmotionTag: ch3.EmotionTag, HookType: ch3.HookType, OutlineSummary: "第三章既有章纲"})
	ch4, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第四章"})

	ent, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "林渊"})

	// 1. 保存两份变更单：ch1 审核入库触发 ch2/ch3 变 upstream_dirty，ch4 保留 pending
	d1, err := st.SavePendingDelta(ctx, domain.ChapterDelta{
		ChapterID:     ch1.ID,
		CausalSummary: "林渊突破筑基初期",
		StatePatches: []domain.DeltaStatePatch{{
			ID: "p1", EntityID: ent.ID, EntityName: "林渊", AttrKey: "境界", PrevValue: "炼气十层", NewValue: "筑基初期",
		}},
	})
	if err != nil {
		t.Fatalf("SavePendingDelta ch1: %v", err)
	}
	if _, err := st.SavePendingDelta(ctx, domain.ChapterDelta{
		ChapterID:     ch4.ID,
		CausalSummary: "第四章待审",
		StatePatches:  []domain.DeltaStatePatch{{ID: "p2", EntityID: ent.ID, EntityName: "林渊", AttrKey: "灵石", NewValue: "100"}},
	}); err != nil {
		t.Fatalf("SavePendingDelta ch4: %v", err)
	}

	pendingList, err := st.ListPendingDeltas(ctx, book.ID)
	if err != nil || len(pendingList) != 2 {
		t.Fatalf("ListPendingDeltas = (%d, %v), want 2", len(pendingList), err)
	}

	applyRes, err := st.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{
		DeltaID:            d1.ID,
		ApplyCausalSummary: true,
		Decisions:          []domain.DeltaDecision{{Kind: domain.DeltaDecisionKindStatePatch, ItemID: "p1", Accept: true}},
	})
	if err != nil || len(applyRes.MarkedDirtyChapterIDs) != 2 {
		t.Fatalf("ApplyDeltaDecisions = (%+v, %v), want 2 marked dirty", applyRes, err)
	}

	// 2. 验证 ListDirtyChapters 正确汇总待修复章节与上游修改来源
	dirtyList, err := st.ListDirtyChapters(ctx, book.ID)
	if err != nil || len(dirtyList) != 2 {
		t.Fatalf("ListDirtyChapters = (%+v, %v), want 2", dirtyList, err)
	}
	if dirtyList[0].ChapterID != ch2.ID || !dirtyList[0].HasContent || dirtyList[0].UpstreamSourceSeq != 1 {
		t.Fatalf("unexpected dirty item 0: %+v", dirtyList[0])
	}
	if dirtyList[1].ChapterID != ch3.ID || dirtyList[1].HasContent {
		t.Fatalf("unexpected dirty item 1: %+v", dirtyList[1])
	}

	// 3. 验证 ClearDirtyChapters：ch2 有正文恢复为 reviewing，ch3 无正文有章纲恢复为 outlined
	clearRes, err := st.ClearDirtyChapters(ctx, book.ID, nil)
	if err != nil || clearRes.ClearedCount != 2 {
		t.Fatalf("ClearDirtyChapters = (%+v, %v), want 2", clearRes, err)
	}
	ch2After, _ := st.GetChapter(ctx, ch2.ID)
	ch3After, _ := st.GetChapter(ctx, ch3.ID)
	if ch2After.Status != domain.ChapterStatusReviewing || ch3After.Status != domain.ChapterStatusOutlined {
		t.Fatalf("restored statuses = (%q, %q), want (reviewing, outlined)", ch2After.Status, ch3After.Status)
	}

	// 4. 验证 book_consistency_reports CRUD 与单条发现状态更新
	savedRep, err := st.SaveConsistencyReport(ctx, domain.BookConsistencyReport{
		BookID:    book.ID,
		ScopeType: domain.ConsistencyScopeBook,
		StartSeq:  1,
		EndSeq:    4,
		ModelName: "deepseek-reasoner",
		Summary:   "发现一处战力体系隐患",
		Findings: []domain.AIConsistencyFinding{{
			ID:          "ai-1",
			Category:    "power_collapse",
			Severity:    domain.LocalAuditSeverityWarn,
			Title:       "越阶战力缺乏解释",
			Description: "筑基初期正面击败金丹",
			ChapterSeqs: []int{2},
			EntityNames: []string{"林渊"},
			Status:      domain.ConsistencyFindingOpen,
		}},
	})
	if err != nil || savedRep.ID <= 0 {
		t.Fatalf("SaveConsistencyReport = (%+v, %v)", savedRep, err)
	}

	reports, err := st.ListConsistencyReports(ctx, book.ID)
	if err != nil || len(reports) != 1 || len(reports[0].Findings) != 1 {
		t.Fatalf("ListConsistencyReports = (%+v, %v)", reports, err)
	}

	updatedRep, err := st.UpdateConsistencyFindingStatus(ctx, savedRep.ID, "ai-1", domain.ConsistencyFindingResolved)
	if err != nil || updatedRep.Findings[0].Status != domain.ConsistencyFindingResolved {
		t.Fatalf("UpdateConsistencyFindingStatus = (%+v, %v)", updatedRep, err)
	}

	if err := st.DeleteConsistencyReport(ctx, savedRep.ID); err != nil {
		t.Fatalf("DeleteConsistencyReport: %v", err)
	}
	reportsAfter, _ := st.ListConsistencyReports(ctx, book.ID)
	if len(reportsAfter) != 0 {
		t.Fatalf("expected 0 reports after delete, got %d", len(reportsAfter))
	}
}
