package store

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"nib/internal/domain"
	"nib/internal/memory"
)

func TestM3RelationGraphTimeSlicesAndEntityPresence(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "M3 图谱与出场雷达测试", Genre: domain.GenreXianxia})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	vol, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	if err != nil {
		t.Fatalf("UpsertVolume: %v", err)
	}

	lin, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "林渊", Aliases: []string{"阿渊"},
	})
	zhou, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "周明",
	})
	su, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "苏清浅",
	})
	sect, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID: book.ID, Kind: domain.EntityKindFaction, Name: "青云宗",
	})

	// 关系演变：
	// - 林渊 -> 青云宗：第 0 章起隶属（全程）
	// - 林渊 -> 周明：第 1 章起同门（ended = 5）
	// - 林渊 -> 周明：第 5 章起死敌（至今）
	// - 林渊 -> 苏清浅：第 3 章起同盟（pending，测试 includePending）
	ch5 := 5
	if _, err := st.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
		BookID: book.ID, SourceEntityID: lin.ID, TargetEntityID: sect.ID,
		RelationType: "隶属", EffectiveChapterSeq: 0,
	}); err != nil {
		t.Fatalf("UpsertEntityRelation sect: %v", err)
	}
	if _, err := st.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
		BookID: book.ID, SourceEntityID: lin.ID, TargetEntityID: zhou.ID,
		RelationType: "同门", EffectiveChapterSeq: 1, EndedChapterSeq: &ch5,
	}); err != nil {
		t.Fatalf("UpsertEntityRelation ally: %v", err)
	}
	if _, err := st.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
		BookID: book.ID, SourceEntityID: lin.ID, TargetEntityID: zhou.ID,
		RelationType: "死敌", EffectiveChapterSeq: 5,
	}); err != nil {
		t.Fatalf("UpsertEntityRelation enemy: %v", err)
	}
	if _, err := st.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
		BookID: book.ID, SourceEntityID: lin.ID, TargetEntityID: su.ID,
		RelationType: "同盟", EffectiveChapterSeq: 3, Status: domain.StateEventPending,
	}); err != nil {
		t.Fatalf("UpsertEntityRelation pending: %v", err)
	}

	// 验证多时间点边集过滤：0 / 3 / 5 / 10
	r0, err := st.ListEntityRelationsAt(ctx, book.ID, 0, false)
	if err != nil || len(r0) != 1 || r0[0].RelationType != "隶属" {
		t.Fatalf("chapter 0 edges = (%+v, %v), want [隶属]", r0, err)
	}
	r3Committed, _ := st.ListEntityRelationsAt(ctx, book.ID, 3, false)
	if len(r3Committed) != 2 {
		t.Fatalf("chapter 3 committed edges = %d, want 2: %+v", len(r3Committed), r3Committed)
	}
	r3WithPending, _ := st.ListEntityRelationsAt(ctx, book.ID, 3, true)
	if len(r3WithPending) != 3 {
		t.Fatalf("chapter 3 pending edges = %d, want 3: %+v", len(r3WithPending), r3WithPending)
	}
	r5, _ := st.ListEntityRelationsAt(ctx, book.ID, 5, false)
	types5 := []string{r5[0].RelationType, r5[1].RelationType}
	if len(r5) != 2 || slices.Contains(types5, "同门") || !slices.Contains(types5, "死敌") {
		t.Fatalf("chapter 5 edges mismatch: %+v", r5)
	}
	r10, _ := st.ListEntityRelationsAt(ctx, book.ID, 10, true)
	if len(r10) != 3 {
		t.Fatalf("chapter 10 with pending edges = %d, want 3", len(r10))
	}

	// 创建 10 章正文与节拍：
	// - 林渊每章都在正文出现（第 10 章掉线 0）
	// - 周明仅在第 1 章出现 1 次（第 10 章掉线 9 章）
	// - 苏清浅仅在第 4 章节拍出场（正文未提及其名字，第 10 章掉线 6 章）
	// - 青云宗在第 2 章正文和节拍同时出现，第 6 章正文出现（第 10 章掉线 4 章）
	chaptersBySeq := make(map[int]domain.Chapter, 10)
	for seq := 1; seq <= 10; seq++ {
		ch, err := st.CreateChapter(ctx, domain.CreateChapterInput{
			BookID: book.ID, VolumeID: vol.ID, Title: fmt.Sprintf("第%d章", seq),
		})
		if err != nil {
			t.Fatalf("CreateChapter %d: %v", seq, err)
		}
		var content string
		switch seq {
		case 1:
			content = "林渊与周明同行入山。"
		case 2:
			content = "阿渊拜入青云宗外门。"
		case 6:
			content = "林渊重返青云宗大殿。"
		default:
			content = "林渊独自修行，吐纳灵气。"
		}
		saved, err := st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
			ChapterID: ch.ID, Title: ch.Title, Content: content, Status: domain.ChapterStatusFinalized,
		})
		if err != nil {
			t.Fatalf("SaveChapterDraft %d: %v", seq, err)
		}
		chaptersBySeq[seq] = saved
	}

	// 第 2 章绑定青云宗节拍；第 4 章仅通过节拍绑定苏清浅（测试正文+节拍联动及 ListBookBeats）
	if _, err := st.ReplaceBeats(ctx, chaptersBySeq[2].ID, []domain.Beat{
		{Stage: domain.BeatStageSetup, Summary: "拜入宗门", EntityIDs: []int64{lin.ID, sect.ID}},
	}); err != nil {
		t.Fatalf("ReplaceBeats ch2: %v", err)
	}
	if _, err := st.ReplaceBeats(ctx, chaptersBySeq[4].ID, []domain.Beat{
		{Stage: domain.BeatStageTurn, Summary: "苏清浅暗中传信", POVEntityID: &su.ID, EntityIDs: []int64{su.ID}},
	}); err != nil {
		t.Fatalf("ReplaceBeats ch4: %v", err)
	}

	entities, err := st.ListEntities(ctx, book.ID, domain.EntityFilter{})
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	chapters, err := st.ListBookChapters(ctx, book.ID)
	if err != nil {
		t.Fatalf("ListBookChapters: %v", err)
	}
	beatsMap, err := st.ListBookBeats(ctx, book.ID)
	if err != nil {
		t.Fatalf("ListBookBeats: %v", err)
	}
	if len(beatsMap) != 2 {
		t.Fatalf("expected 2 chapters with beats, got %d", len(beatsMap))
	}

	rep := memory.ComputeEntityPresence(book.ID, entities, chapters, beatsMap, -1)
	if rep.LatestChapterSeq != 10 || rep.ReferenceSeq != 10 {
		t.Fatalf("unexpected presence report header: %+v", rep)
	}
	byID := make(map[int64]domain.EntityPresenceStat, len(rep.Stats))
	for _, s := range rep.Stats {
		byID[s.EntityID] = s
	}

	if byID[lin.ID].AppearanceChapters != 10 || byID[lin.ID].AbsentChapters != 0 || byID[lin.ID].LastSeenSeq != 10 {
		t.Fatalf("lin presence mismatch: %+v", byID[lin.ID])
	}
	if byID[zhou.ID].AppearanceChapters != 1 || byID[zhou.ID].LastSeenSeq != 1 || byID[zhou.ID].AbsentChapters != 9 {
		t.Fatalf("zhou presence mismatch: %+v", byID[zhou.ID])
	}
	if byID[su.ID].AppearanceChapters != 1 || byID[su.ID].BeatAppearances != 1 || byID[su.ID].MentionCount != 0 || byID[su.ID].LastSeenSeq != 4 || byID[su.ID].AbsentChapters != 6 {
		t.Fatalf("su presence mismatch: %+v", byID[su.ID])
	}
	if byID[sect.ID].AppearanceChapters != 2 || byID[sect.ID].LastSeenSeq != 6 || byID[sect.ID].AbsentChapters != 4 {
		t.Fatalf("sect presence mismatch: %+v", byID[sect.ID])
	}
}
