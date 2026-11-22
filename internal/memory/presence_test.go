package memory

import (
	"slices"
	"testing"

	"nib/internal/domain"
)

func TestComputeEntityPresenceGolden(t *testing.T) {
	idQingyun := int64(1)
	idElder := int64(2)
	idGhost := int64(3)
	idLaw := int64(4)

	entities := []domain.Entity{
		{
			ID:      idQingyun,
			BookID:  1,
			Kind:    domain.EntityKindFaction,
			Name:    "青云宗",
			Aliases: []string{"青云", "宗"},
		},
		{
			ID:      idElder,
			BookID:  1,
			Kind:    domain.EntityKindCharacter,
			Name:    "玄风长老",
			Aliases: []string{"玄风", "风"},
		},
		{
			ID:   idGhost,
			BookID: 1,
			Kind: domain.EntityKindCharacter,
			Name: "幽冥老祖",
		},
		{
			ID:          idLaw,
			BookID:      1,
			Kind:        domain.EntityKindRule,
			Name:        "灵脉枯竭铁律",
			IsInvariant: true,
		},
	}

	chapters := []domain.Chapter{
		{ID: 103, BookID: 1, GlobalSeq: 3, Title: "风波平息", Content: "风吹过山门，今日并无要事。"},
		{ID: 101, BookID: 1, GlobalSeq: 1, Title: "入门大典", Content: "青云宗收徒日，青云外门弟子齐聚，玄风亲自主持。"},
		{ID: 102, BookID: 1, GlobalSeq: 2, Title: "暗潮涌动", Content: "青云宗山门戒备森严，众弟子只听见远山阵风呼啸。"},
		{ID: 104, BookID: 1, GlobalSeq: 4, Title: "秘境传讯", Content: "宗门之外再起传闻，玄风长老闭关不出。"},
	}

	beatsByChapter := map[int64][]domain.Beat{
		101: {
			// 同一节拍中 POV 与 EntityIDs 同时命中玄风长老，仅计 1 次 BeatAppearances。
			{ID: 1, ChapterID: 101, Seq: 1, Stage: domain.BeatStageSetup, POVEntityID: &idElder, EntityIDs: []int64{idElder, idQingyun}},
		},
		103: {
			// 第 3 章正文中「风」是单字别名，不得误匹配；但节拍明确绑定了玄风长老。
			{ID: 2, ChapterID: 103, Seq: 1, Stage: domain.BeatStageTurn, EntityIDs: []int64{idElder}},
		},
	}

	// 1) 全书最新章汇总（upToSeq = -1 => referenceSeq = 4）
	full := ComputeEntityPresence(1, entities, chapters, beatsByChapter, -1)
	if full.LatestChapterSeq != 4 || full.ReferenceSeq != 4 {
		t.Fatalf("unexpected header: %+v", full)
	}
	if len(full.Stats) != 4 {
		t.Fatalf("expected 4 stats, got %d", len(full.Stats))
	}

	statByID := make(map[int64]domain.EntityPresenceStat, len(full.Stats))
	for _, s := range full.Stats {
		statByID[s.EntityID] = s
	}

	// 青云宗：第 1 章正文出现「青云宗」和「青云」共 2 次 + 节拍 1 次；第 2 章正文「青云宗」1 次；第 4 章中单字别名「宗」应被忽略。
	qy := statByID[idQingyun]
	if qy.AppearanceChapters != 2 || qy.MentionCount != 3 || qy.BeatAppearances != 1 {
		t.Fatalf("qingyun counts mismatch: %+v", qy)
	}
	if qy.FirstSeenSeq != 1 || qy.LastSeenSeq != 2 || qy.AbsentChapters != 2 || qy.NeverAppeared {
		t.Fatalf("qingyun timeline mismatch: %+v", qy)
	}
	if !slices.Equal(qy.AppearedChapterSeqs, []int{1, 2}) {
		t.Fatalf("qingyun seqs = %v, want [1 2]", qy.AppearedChapterSeqs)
	}

	// 玄风长老：第 1 章正文 1 次 + 节拍 1 次；第 3 章单字「风」不匹配正文，由节拍计入 1 次；第 4 章正文 1 次。
	elder := statByID[idElder]
	if elder.AppearanceChapters != 3 || elder.MentionCount != 2 || elder.BeatAppearances != 2 {
		t.Fatalf("elder counts mismatch: %+v", elder)
	}
	if elder.LastSeenSeq != 4 || elder.LastSeenChapterTitle != "秘境传讯" || elder.AbsentChapters != 0 {
		t.Fatalf("elder timeline mismatch: %+v", elder)
	}
	if !slices.Equal(elder.AppearedChapterSeqs, []int{1, 3, 4}) {
		t.Fatalf("elder seqs = %v, want [1 3 4]", elder.AppearedChapterSeqs)
	}

	// 幽冥老祖：全书从未出场，AppearedChapterSeqs 必须是非 nil 空切片。
	ghost := statByID[idGhost]
	if !ghost.NeverAppeared || ghost.AbsentChapters != 0 || ghost.AppearanceChapters != 0 || ghost.AppearedChapterSeqs == nil || len(ghost.AppearedChapterSeqs) != 0 {
		t.Fatalf("ghost stat mismatch: %+v", ghost)
	}

	// 2) 截至第 2 章切片（upToSeq = 2）：玄风长老只看到第 1 章，掉线 1 章
	mid := ComputeEntityPresence(1, entities, chapters, beatsByChapter, 2)
	if mid.ReferenceSeq != 2 || mid.LatestChapterSeq != 4 {
		t.Fatalf("unexpected mid report header: %+v", mid)
	}
	for _, s := range mid.Stats {
		if s.EntityID == idElder {
			if s.AppearanceChapters != 1 || s.LastSeenSeq != 1 || s.AbsentChapters != 1 {
				t.Fatalf("mid elder stat mismatch: %+v", s)
			}
		}
	}

	// 3) 第 0 章初始态：不统计任何章节出场，全部标记 NeverAppeared
	initRep := ComputeEntityPresence(1, entities, chapters, beatsByChapter, 0)
	if initRep.ReferenceSeq != 0 {
		t.Fatalf("expected ReferenceSeq=0, got %d", initRep.ReferenceSeq)
	}
	for _, s := range initRep.Stats {
		if !s.NeverAppeared || s.AppearanceChapters != 0 || s.AbsentChapters != 0 {
			t.Fatalf("init stat mismatch for %d: %+v", s.EntityID, s)
		}
	}
}
