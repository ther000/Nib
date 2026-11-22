package memory

import (
	"fmt"
	"testing"

	"nib/internal/domain"
)

func TestRunLocalConsistencyAudit50ChaptersGolden(t *testing.T) {
	const bookID = int64(1)

	var (
		idHero      = int64(1)
		idDeadVill  = int64(2) // 正样本：第 12 章死，第 35 章正文复现，第 38 章节拍复现
		idAbsent    = int64(3) // 正样本：第 15 章末见，掉线 35 章
		idDeadMem   = int64(4) // 负样本：第 5 章陨落，第 40 章仅祭奠回忆，不得误报
		idRevived   = int64(5) // 负样本：第 10 章死，第 25 章复活，第 30 章出现，不得误报
		idRetired   = int64(6) // 负样本：第 10 章闭关退场，不得报掉线
		idInvariant = int64(7) // 负样本：世界铁律，不得报掉线
	)

	entities := []domain.Entity{
		{ID: idHero, BookID: bookID, Kind: domain.EntityKindCharacter, Name: "林渊"},
		{ID: idDeadVill, BookID: bookID, Kind: domain.EntityKindCharacter, Name: "血煞长老"},
		{ID: idAbsent, BookID: bookID, Kind: domain.EntityKindCharacter, Name: "苏清浅"},
		{ID: idDeadMem, BookID: bookID, Kind: domain.EntityKindCharacter, Name: "玄机子"},
		{ID: idRevived, BookID: bookID, Kind: domain.EntityKindCharacter, Name: "幽冥王"},
		{ID: idRetired, BookID: bookID, Kind: domain.EntityKindCharacter, Name: "赵铁柱"},
		{ID: idInvariant, BookID: bookID, Kind: domain.EntityKindRule, Name: "天道压制铁律", IsInvariant: true},
	}

	chapters := make([]domain.Chapter, 0, 50)
	beatsByChapter := make(map[int64][]domain.Beat)
	day := 0
	for seq := 1; seq <= 50; seq++ {
		day += 2
		storyDayVal := day
		switch seq {
		case 27:
			// 正样本：时间线倒流（第 26 章 day=52 → 第 27 章 day=45）
			storyDayVal = 45
			day = 45
		case 41:
			// 正样本：时间线异常跳跃（第 40 章 day=71 → 第 41 章 day=500）
			storyDayVal = 500
			day = 500
		}

		content := "林渊在山谷之中潜心参悟剑意。"
		switch seq {
		case 1, 15:
			content = "林渊与苏清浅联手破开古阵。"
		case 10:
			content = "林渊目送赵铁柱闭关远行，随后遭遇幽冥王。"
		case 30:
			content = "幽冥王重塑肉身之后卷土重来，与林渊交手。"
		case 35:
			content = "血煞长老冷笑一声，抬手向林渊拍出一掌。"
		case 36:
			content = "幽冥王撤回魔域休整，暂避锋芒。"
		case 40:
			content = "林渊站在玄机子墓碑前祭奠亡师，回想起生前教诲。"
		}

		status := domain.ChapterStatusFinalized
		if seq == 49 || seq == 50 {
			// 正样本：下游脏标章
			status = domain.ChapterStatusUpstreamDirty
		}

		chID := int64(100 + seq)
		chapters = append(chapters, domain.Chapter{
			ID:        chID,
			BookID:    bookID,
			GlobalSeq: seq,
			Title:     fmt.Sprintf("第%d章", seq),
			Status:    status,
			StoryDay:  &storyDayVal,
			Content:   content,
		})
	}

	// 正样本：血煞长老第 38 章节拍复现
	beatsByChapter[138] = []domain.Beat{
		{ID: 1, ChapterID: 138, Seq: 1, Stage: domain.BeatStageTurn, EntityIDs: []int64{idDeadVill}},
	}

	events := []domain.EntityStateEvent{
		{ID: 1, BookID: bookID, EntityID: idDeadMem, ChapterSeq: 5, AttrKey: "存活状态", NewValue: "陨落", Status: domain.StateEventCommitted},
		{ID: 2, BookID: bookID, EntityID: idRevived, ChapterSeq: 10, AttrKey: "存活状态", NewValue: "死亡", Status: domain.StateEventCommitted},
		{ID: 3, BookID: bookID, EntityID: idRetired, ChapterSeq: 10, AttrKey: "行踪", NewValue: "闭关退场", Status: domain.StateEventCommitted},
		{ID: 4, BookID: bookID, EntityID: idDeadVill, ChapterSeq: 12, AttrKey: "存活状态", NewValue: "身死道消", Status: domain.StateEventCommitted},
		{ID: 5, BookID: bookID, EntityID: idRevived, ChapterSeq: 25, AttrKey: "存活状态", NewValue: "借尸还魂复活", Status: domain.StateEventCommitted},
	}

	foreshadowings := []domain.Foreshadowing{
		{ID: 1, BookID: bookID, Title: "青铜古镜来历", Status: domain.ForeshadowingPlanted, TargetResolveSeq: 30},
		{ID: 2, BookID: bookID, Title: "外门玉佩之谜", Status: domain.ForeshadowingResolved, TargetResolveSeq: 20},
	}

	deadlines := []domain.StoryDeadline{
		{ID: 1, BookID: bookID, Title: "宗门大比", TargetDay: 60, TargetLabel: "五月初五", Status: domain.StoryDeadlineActive},
		{ID: 2, BookID: bookID, Title: "内门考核", TargetDay: 30, TargetLabel: "三月初一", Status: domain.StoryDeadlineReached},
	}

	pendingDeltas := []domain.ChapterDelta{
		{
			ID:           10,
			ChapterID:    148, // 第 48 章
			ReviewStatus: domain.DeltaReviewPending,
			StatePatches: []domain.DeltaStatePatch{{ID: "p1", EntityName: "林渊", AttrKey: "境界", NewValue: "筑基初期"}},
		},
		{
			ID:           11,
			ChapterID:    147, // 已审核，不应计入
			ReviewStatus: domain.DeltaReviewApplied,
		},
	}

	report := RunLocalConsistencyAudit(LocalAuditInput{
		BookID:         bookID,
		Entities:       entities,
		Chapters:       chapters,
		BeatsByChapter: beatsByChapter,
		StateEvents:    events,
		Foreshadowings: foreshadowings,
		Deadlines:      deadlines,
		PendingDeltas:  pendingDeltas,
		Options: domain.LocalAuditOptions{
			AbsentThresholdChapters: 20,
			MaxStoryDayJump:         365,
		},
	})

	if report.LatestChapterSeq != 50 || report.ScannedChapters != 50 || report.ScannedEntities != 7 {
		t.Fatalf("unexpected report scope: %+v", report)
	}

	// 验证总命中与分项计数：
	// - dead_reappear: 2 (第35章正文 + 第38章节拍)
	// - absent_entity: 1 (苏清浅)
	// - overdue_foreshadow: 1 (青铜古镜来历)
	// - timeline_anomaly: 3 (第27章倒流 + 第41章跳跃 + 宗门大比期限过期)
	// - pending_delta: 1 (第48章)
	// - upstream_dirty: 2 (第49, 50章)
	if report.Summary.DeadReappearCount != 2 {
		t.Fatalf("DeadReappearCount = %d, want 2; findings=%+v", report.Summary.DeadReappearCount, report.Findings)
	}
	if report.Summary.AbsentEntityCount != 1 {
		t.Fatalf("AbsentEntityCount = %d, want 1; findings=%+v", report.Summary.AbsentEntityCount, report.Findings)
	}
	if report.Summary.OverdueForeshadowCount != 1 {
		t.Fatalf("OverdueForeshadowCount = %d, want 1", report.Summary.OverdueForeshadowCount)
	}
	if report.Summary.TimelineAnomalyCount != 3 {
		t.Fatalf("TimelineAnomalyCount = %d, want 3; findings=%+v", report.Summary.TimelineAnomalyCount, report.Findings)
	}
	if report.Summary.PendingDeltaCount != 1 {
		t.Fatalf("PendingDeltaCount = %d, want 1", report.Summary.PendingDeltaCount)
	}
	if report.Summary.UpstreamDirtyCount != 2 {
		t.Fatalf("UpstreamDirtyCount = %d, want 2", report.Summary.UpstreamDirtyCount)
	}
	if report.Summary.TotalFindings != 10 {
		t.Fatalf("TotalFindings = %d, want 10", report.Summary.TotalFindings)
	}

	// 验证负样本均零误报
	for _, f := range report.Findings {
		if f.EntityID == idDeadMem || f.EntityID == idRevived || f.EntityID == idRetired || f.EntityID == idInvariant {
			t.Fatalf("unexpected false positive finding: %+v", f)
		}
		if f.ForeshadowingID == 2 || f.DeadlineID == 2 {
			t.Fatalf("unexpected resolved foreshadow/deadline finding: %+v", f)
		}
	}
}
