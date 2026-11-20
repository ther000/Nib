package compiler

import (
	"context"
	"slices"
	"strings"
	"testing"

	"nib/internal/domain"
)

type fakeDataSource struct {
	book         domain.Book
	volume       domain.Volume
	chapter      domain.Chapter
	beats        []domain.Beat
	invariants   []domain.Entity
	entities     map[int64]domain.Entity
	prevChapters []domain.Chapter
}

func (f *fakeDataSource) GetBook(_ context.Context, id int64) (domain.Book, error) {
	if f.book.ID != id {
		return domain.Book{}, domain.ErrNotFound
	}
	return f.book, nil
}

func (f *fakeDataSource) GetVolume(_ context.Context, id int64) (domain.Volume, error) {
	if f.volume.ID != id {
		return domain.Volume{}, domain.ErrNotFound
	}
	return f.volume, nil
}

func (f *fakeDataSource) GetChapter(_ context.Context, id int64) (domain.Chapter, error) {
	if f.chapter.ID != id {
		return domain.Chapter{}, domain.ErrNotFound
	}
	return f.chapter, nil
}

func (f *fakeDataSource) ListBeats(_ context.Context, _ int64) ([]domain.Beat, error) {
	return f.beats, nil
}

func (f *fakeDataSource) ListInvariants(_ context.Context, _ int64) ([]domain.Entity, error) {
	return f.invariants, nil
}

func (f *fakeDataSource) GetEntitiesByIDs(_ context.Context, ids []int64) ([]domain.Entity, error) {
	out := make([]domain.Entity, 0, len(ids))
	for _, id := range ids {
		if e, ok := f.entities[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeDataSource) GetPreviousChapters(_ context.Context, _ int64, _ int, _ int) ([]domain.Chapter, error) {
	return f.prevChapters, nil
}

func newTestFixture() *fakeDataSource {
	povID := int64(101)
	return &fakeDataSource{
		book: domain.Book{
			ID:                 1,
			Title:              "玄渊斩道录",
			Genre:              domain.GenreXuanhuan,
			Premise:            "凡骨少年携残缺星盘逆伐九大宗门",
			GoldenFinger:       "星盘每日推演一次敌手功法破绽，需消耗同等灵石",
			TargetChapterWords: 2500,
		},
		volume: domain.Volume{
			ID:         10,
			BookID:     1,
			Seq:        1,
			Title:      "青云试锋",
			Goal:       "外门大比夺魁并拿到内门密库令牌",
			ClimaxPlan: "越阶反杀内门执事王煞",
		},
		chapter: domain.Chapter{
			ID:             100,
			BookID:         1,
			VolumeID:       10,
			GlobalSeq:      12,
			VolSeq:         12,
			Title:          "残阵反杀",
			Status:         domain.ChapterStatusOutlined,
			EmotionTag:     domain.EmotionBurst,
			HookType:       domain.HookMystery,
			HookNote:       "搜出的血玉令牌背面浮现三百年前古宗徽记",
			OutlineSummary: "林渊借残阵逆斩王执事，夺宝撤离坊市",
		},
		beats: []domain.Beat{
			{
				ID:           2,
				ChapterID:    100,
				Seq:          2,
				Stage:        domain.BeatStageBurst,
				Summary:      "引动残阵雷纹废掉王煞护体罡气并一剑绝杀",
				ConflictNote: "灵力仅剩三成，硬拼必死，必须靠阵眼反制",
				POVEntityID:  &povID,
				EntityIDs:    []int64{101, 102, 103},
				TargetWords:  900,
			},
			{
				ID:           1,
				ChapterID:    100,
				Seq:          1,
				Stage:        domain.BeatStageSuppress,
				Summary:      "王煞封锁巷口逼交破障丹，言语轻贱外门废柴",
				ConflictNote: "实力低两个小境界，退无可退",
				POVEntityID:  &povID,
				EntityIDs:    []int64{101, 102},
				TargetWords:  700,
			},
		},
		invariants: []domain.Entity{
			{
				ID:          1,
				BookID:      1,
				Kind:        domain.EntityKindRule,
				Name:        "无复活铁律",
				IsInvariant: true,
				Summary:     "全书不存在任何起死回生之术，神魂俱灭即永久退场",
			},
			{
				ID:          2,
				BookID:      1,
				Kind:        domain.EntityKindRule,
				Name:        "跨阶必付代价",
				IsInvariant: true,
				Summary:     "越阶斩敌必然损耗底牌、阵器或留下暗伤，严禁无伤碾压",
			},
		},
		entities: map[int64]domain.Entity{
			101: {
				ID:      101,
				BookID:  1,
				Kind:    domain.EntityKindCharacter,
				Name:    "林渊",
				Aliases: []string{"林师弟"},
				Summary: "外门隐忍弟子，擅推演破绽",
				Attributes: map[string]string{
					"realm":  "炼气九层",
					"injury": "左肩轻伤",
				},
			},
			102: {
				ID:      102,
				BookID:  1,
				Kind:    domain.EntityKindCharacter,
				Name:    "王煞",
				Summary: "黑煞宗内门执事，贪婪谨慎",
				Attributes: map[string]string{
					"realm":    "筑基初期",
					"treasure": "阴雷珠",
				},
			},
			103: {
				ID:      103,
				BookID:  1,
				Kind:    domain.EntityKindItem,
				Name:    "破障丹",
				Summary: "冲击筑基瓶颈的紧俏丹药",
				Attributes: map[string]string{
					"owner": "王煞",
				},
			},
			104: {
				ID:      104,
				BookID:  1,
				Kind:    domain.EntityKindCharacter,
				Name:    "苏晚晴",
				Summary: "暗中观察坊市动静的神秘女修",
				Attributes: map[string]string{
					"affiliation": "天机阁",
				},
			},
		},
		// 故意按升序传入，验证编译器自动按 GlobalSeq 降序识别 N-1 与 N-2。
		prevChapters: []domain.Chapter{
			{
				ID:        98,
				BookID:    1,
				GlobalSeq: 10,
				Title:     "黑市易物",
				Content:   strings.Repeat("前两章铺垫坊市黑市风云。", 60) + "【N2结尾标记】林渊收好残破阵图转身离去。",
			},
			{
				ID:        99,
				BookID:    1,
				GlobalSeq: 11,
				Title:     "夜巷杀机",
				Content:   strings.Repeat("上一章夜雨巷战追踪。", 160) + "【N1结尾标记】王煞的阴雷珠已在掌心亮起幽蓝电弧。",
			},
		},
	}
}

func TestCompileHappyPathAndEntitiesPinExclude(t *testing.T) {
	ds := newTestFixture()
	comp := New(ds)

	briefing, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{
		PinnedEntityIDs:   []int64{104}, // 钉选苏晚晴
		ExcludedEntityIDs: []int64{103}, // 剔除破障丹
		ExtraInstruction:  "动作描写突出雨夜泥泞地形对身法的影响",
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if briefing.ChapterID != 100 || briefing.ChapterSeq != 12 || briefing.BookTitle != "玄渊斩道录" {
		t.Fatalf("unexpected briefing metadata: %+v", briefing)
	}
	if briefing.TotalTokenBudget != domain.DefaultBriefingTokenBudget {
		t.Fatalf("expected default budget %d, got %d", domain.DefaultBriefingTokenBudget, briefing.TotalTokenBudget)
	}

	// 验证总估算等于各槽位之和。
	sumSlotTokens := 0
	slotByKey := map[string]domain.BriefingSlot{}
	for _, s := range briefing.Slots {
		sumSlotTokens += s.TokenEst
		slotByKey[s.Key] = s
	}
	if briefing.TotalTokenEst != sumSlotTokens {
		t.Fatalf("TotalTokenEst (%d) != sum of slots (%d)", briefing.TotalTokenEst, sumSlotTokens)
	}

	// P0 包含核心设定、题材规范与全部世界铁律。
	p0 := slotByKey["p0_core"]
	if p0.Truncated || !p0.Required {
		t.Fatalf("P0 must be required and never truncated: %+v", p0)
	}
	for _, must := range []string{"玄渊斩道录", "残缺星盘", "玄幻", "无复活铁律", "跨阶必付代价"} {
		if !strings.Contains(briefing.SystemPrompt, must) {
			t.Errorf("SystemPrompt missing %q", must)
		}
	}

	// P1 节拍按 seq 升序排列，且包含作者附加指令与钩子要求。
	p1 := slotByKey["p1_chapter_beats"]
	if p1.Truncated || !p1.Required {
		t.Fatalf("P1 must be required and never truncated: %+v", p1)
	}
	idx1 := strings.Index(p1.Content, "王煞封锁巷口")
	idx2 := strings.Index(p1.Content, "引动残阵雷纹")
	if idx1 < 0 || idx2 < 0 || idx1 >= idx2 {
		t.Fatalf("beats not ordered by seq in P1: idx1=%d idx2=%d", idx1, idx2)
	}
	if !strings.Contains(p1.Content, "雨夜泥泞地形") || !strings.Contains(p1.Content, "三百年前古宗徽记") {
		t.Fatalf("P1 missing extra instruction or hook note")
	}

	// P2 包含 POV(101)、钉选(104)、节拍实体(102)，不含被排除实体(103)。
	p2 := slotByKey["p2_entities"]
	if !slices.Contains(briefing.IncludedEntities, 101) ||
		!slices.Contains(briefing.IncludedEntities, 102) ||
		!slices.Contains(briefing.IncludedEntities, 104) {
		t.Fatalf("unexpected IncludedEntities: %v", briefing.IncludedEntities)
	}
	if slices.Contains(briefing.IncludedEntities, 103) {
		t.Fatalf("excluded entity 103 must not be in IncludedEntities: %v", briefing.IncludedEntities)
	}
	if !strings.Contains(p2.Content, "苏晚晴") || strings.Contains(p2.Content, "破障丹") {
		t.Fatalf("P2 content did not respect pin/exclude rules: %s", p2.Content)
	}

	// P4 包含 N-2 结尾与 N-1 结尾，且按时间顺序 N-2 在前、N-1 在后，正常窗口不标 Truncated。
	p4 := slotByKey["p4_recent_tail"]
	if p4.Truncated {
		t.Fatalf("expected P4 not truncated under default 12000 budget")
	}
	n2Pos := strings.Index(p4.Content, "【N2结尾标记】")
	n1Pos := strings.Index(p4.Content, "【N1结尾标记】")
	if n2Pos < 0 || n1Pos < 0 || n2Pos >= n1Pos {
		t.Fatalf("expected N-2 tail before N-1 tail in P4: n2=%d n1=%d", n2Pos, n1Pos)
	}
}

func TestCompileInvariantsNeverTrimmedUnderTightBudget(t *testing.T) {
	ds := newTestFixture()
	comp := New(ds)

	// 故意给一个远小于 P0+P1 的极紧预算。
	briefing, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{
		TokenBudget:      300,
		ExtraInstruction: "务必写清残阵代价",
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	slotByKey := map[string]domain.BriefingSlot{}
	for _, s := range briefing.Slots {
		slotByKey[s.Key] = s
	}

	if slotByKey["p0_core"].Truncated {
		t.Fatalf("P0 must NEVER be truncated even under ultra-tight budget")
	}
	if slotByKey["p1_chapter_beats"].Truncated {
		t.Fatalf("P1 must NEVER be truncated even under ultra-tight budget")
	}
	for _, inv := range ds.invariants {
		if !strings.Contains(briefing.SystemPrompt, inv.Name) || !strings.Contains(briefing.SystemPrompt, inv.Summary) {
			t.Fatalf("invariant %q missing from SystemPrompt under tight budget", inv.Name)
		}
	}
	if !strings.Contains(briefing.UserPrompt, "务必写清残阵代价") {
		t.Fatalf("ExtraInstruction missing from UserPrompt under tight budget")
	}
	if !slotByKey["p2_entities"].Truncated || !slotByKey["p4_recent_tail"].Truncated {
		t.Fatalf("expected P2 and P4 to report Truncated under ultra-tight budget")
	}
	if len(briefing.Warnings) == 0 {
		t.Fatalf("expected budget warning when P0+P1 exceed TokenBudget")
	}
}

func TestCompilePreviousChapterTailTrimming(t *testing.T) {
	ds := newTestFixture()
	comp := New(ds)

	// 先测出 P0 + P1 + P2 的基础占用，再留约 350 token 给 P4（足够保留 N-1 尾部，但装不下完整的 1200+400 汉字）。
	fullBrief, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{})
	if err != nil {
		t.Fatalf("full Compile failed: %v", err)
	}
	baseNonP4 := 0
	for _, s := range fullBrief.Slots {
		if s.Key != "p4_recent_tail" {
			baseNonP4 += s.TokenEst
		}
	}

	tightBudget := baseNonP4 + 320
	trimmedBrief, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{
		TokenBudget: tightBudget,
	})
	if err != nil {
		t.Fatalf("tight Compile failed: %v", err)
	}

	var p4 domain.BriefingSlot
	for _, s := range trimmedBrief.Slots {
		if s.Key == "p4_recent_tail" {
			p4 = s
		}
	}
	if !p4.Truncated {
		t.Fatalf("expected P4 to be trimmed under tight budget %d (est=%d)", tightBudget, p4.TokenEst)
	}
	if p4.TokenEst > 320 {
		t.Fatalf("P4 TokenEst (%d) exceeded remaining budget (320)", p4.TokenEst)
	}
	// N-1 紧邻本章的最末尾内容必须保留，N-2 应已被舍弃。
	if !strings.Contains(p4.Content, "【N1结尾标记】") {
		t.Fatalf("expected N-1 very end marker preserved in trimmed P4, got: %q", p4.Content)
	}
	if strings.Contains(p4.Content, "【N2结尾标记】") {
		t.Fatalf("expected N-2 dropped before truncating N-1, got: %q", p4.Content)
	}
	if trimmedBrief.TotalTokenEst > tightBudget {
		t.Fatalf("TotalTokenEst (%d) exceeded tightBudget (%d)", trimmedBrief.TotalTokenEst, tightBudget)
	}
}

type fakeMemoryDataSource struct {
	*fakeDataSource
	snapshotsCommitted []domain.EntitySnapshot
	snapshotsPending   []domain.EntitySnapshot
	foreshadowings     []domain.Foreshadowing
	arcSummaries       []domain.ArcSummary
	searchHits         []domain.MemorySearchHit

	lastSnapSeq            int
	lastSnapIncludePending bool
	lastSearchMaxSeq       int
	searchedKeywords       []string
}

func (f *fakeMemoryDataSource) SnapshotAt(_ context.Context, _ int64, chapterSeq int, includePending bool) ([]domain.EntitySnapshot, error) {
	f.lastSnapSeq = chapterSeq
	f.lastSnapIncludePending = includePending
	if includePending {
		return f.snapshotsPending, nil
	}
	return f.snapshotsCommitted, nil
}

func (f *fakeMemoryDataSource) ListForeshadowings(_ context.Context, _ int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error) {
	out := make([]domain.Foreshadowing, 0, len(f.foreshadowings))
	for _, fs := range f.foreshadowings {
		if status != "" && fs.Status != status {
			continue
		}
		out = append(out, fs)
	}
	return out, nil
}

func (f *fakeMemoryDataSource) ListArcSummaries(_ context.Context, _ int64, level domain.ArcSummaryLevel) ([]domain.ArcSummary, error) {
	out := make([]domain.ArcSummary, 0, len(f.arcSummaries))
	for _, a := range f.arcSummaries {
		if level != "" && a.Level != level {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeMemoryDataSource) SearchMemory(_ context.Context, _ int64, query string, maxChapterSeq int) ([]domain.MemorySearchHit, error) {
	f.lastSearchMaxSeq = maxChapterSeq
	f.searchedKeywords = append(f.searchedKeywords, query)
	return f.searchHits, nil
}

func newPhase2Fixture() *fakeMemoryDataSource {
	base := newTestFixture()
	base.chapter.GlobalSeq = 20

	// 静态属性是第 1 章旧值（炼气九层 / 完好），快照中已演化为最新值。
	entLin := base.entities[101]
	entWang := base.entities[102]
	entPill := base.entities[103]

	committed := []domain.EntitySnapshot{
		{
			Entity: entLin,
			EffectiveAttributes: map[string]string{
				"realm":  "筑基初期（已确认快照）",
				"injury": "左肩阴雷暗伤",
			},
			AppliedEventCount: 2,
			PendingEventCount: 0,
		},
		{
			Entity: entWang,
			EffectiveAttributes: map[string]string{
				"realm":    "筑基中期",
				"treasure": "阴雷珠（裂纹）",
			},
			AppliedEventCount: 1,
		},
		{
			Entity: entPill,
			EffectiveAttributes: map[string]string{
				"owner": "林渊（已夺得）",
			},
			AppliedEventCount: 1,
		},
	}

	pending := []domain.EntitySnapshot{
		{
			Entity: entLin,
			EffectiveAttributes: map[string]string{
				"realm":  "筑基初期（含同批待审）",
				"injury": "经脉微损（同批第19章pending）",
			},
			AppliedEventCount: 3,
			PendingEventCount: 1,
		},
		committed[1],
		committed[2],
	}

	// 构造 16 章带因果摘要的前文（含未来第 21 章干扰项，检验防剧透）。
	prevChaps := make([]domain.Chapter, 0, 18)
	for seq := 4; seq <= 19; seq++ {
		prevChaps = append(prevChaps, domain.Chapter{
			ID:            int64(seq),
			BookID:        1,
			GlobalSeq:     seq,
			Title:         "前文第" + string(rune('A'+(seq-4))) + "章",
			CausalSummary: strings.Repeat("因果推进详情。", 18) + "【因果标记:" + string(rune('A'+(seq-4))) + "】",
			Content:       strings.Repeat("正文铺陈。", 30) + "【结尾】",
		})
	}
	prevChaps = append(prevChaps, domain.Chapter{
		ID:            21,
		BookID:        1,
		GlobalSeq:     21,
		Title:         "未来剧透章",
		CausalSummary: "【未来绝对不可泄露】",
		Content:       "【未来正文不可泄露】",
	})
	base.prevChapters = prevChaps

	return &fakeMemoryDataSource{
		fakeDataSource:     base,
		snapshotsCommitted: committed,
		snapshotsPending:   pending,
		foreshadowings: []domain.Foreshadowing{
			{
				ID: 1, BookID: 1, Title: "超期核心伏笔：血玉密库",
				Description: "早该在第15章收网的宗门秘闻", PlantedChapterSeq: 3, TargetResolveSeq: 15,
				Status: domain.ForeshadowingPlanted, InvolvedEntityIDs: []int64{101},
			},
			{
				ID: 2, BookID: 1, Title: "远期普通伏笔：海外仙岛",
				Description: "计划第80章才触发", PlantedChapterSeq: 5, TargetResolveSeq: 80,
				Status: domain.ForeshadowingHinted, InvolvedEntityIDs: []int64{999},
			},
			{
				ID: 3, BookID: 1, Title: "已回收伏笔：外门试炼",
				Description: "已完结不应入P3", PlantedChapterSeq: 1, TargetResolveSeq: 10, ResolvedChapterSeq: 10,
				Status: domain.ForeshadowingResolved,
			},
			{
				ID: 4, BookID: 1, Title: "未来章才埋设的剧透伏笔",
				Description: "第22章才埋设绝不能剧透", PlantedChapterSeq: 22, TargetResolveSeq: 40,
				Status: domain.ForeshadowingPlanted,
			},
		},
		arcSummaries: []domain.ArcSummary{
			{ID: 1, BookID: 1, Level: domain.ArcSummaryLevelVolume, StartSeq: 1, EndSeq: 10, Summary: "第一卷前半：外门崛起。"},
			{ID: 2, BookID: 1, Level: domain.ArcSummaryLevelArc10, StartSeq: 1, EndSeq: 10, Summary: "1~10章弧线：黑市初遇与破障丹争夺。"},
			{ID: 3, BookID: 1, Level: domain.ArcSummaryLevelArc10, StartSeq: 11, EndSeq: 25, Summary: "未来超前弧线绝不可注入。"},
		},
		searchHits: []domain.MemorySearchHit{
			{Source: domain.MemorySearchSourceChapter, ChapterID: 7, ChapterSeq: 7, Title: "黑市偶遇", Snippet: "【命中片段1】半块玉佩在坊市发出微光。", Score: 9.5},
			{Source: domain.MemorySearchSourceChapter, ChapterID: 12, ChapterSeq: 12, Title: "密库线索", Snippet: "【命中片段2】王煞腰间挂着血玉令牌。", Score: 8.2},
			{Source: domain.MemorySearchSourceChapter, ChapterID: 14, ChapterSeq: 14, Title: "雨夜暗袭", Snippet: "【命中片段3】残阵雷纹只能催动三次。", Score: 7.1},
			{Source: domain.MemorySearchSourceChapter, ChapterID: 25, ChapterSeq: 25, Title: "未来剧透命中", Snippet: "【绝对不可出现的第25章剧透】", Score: 99.9},
		},
	}
}

func TestCompilePhase2SnapshotForeshadowPyramidAndSearch(t *testing.T) {
	ds := newPhase2Fixture()
	comp := New(ds) // 自动检测 MemoryDataSource，无需额外胶水

	brief, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{
		IncludePending: false,
		SearchKeywords: []string{"血玉令牌", "半块玉佩"},
	})
	if err != nil {
		t.Fatalf("Phase 2 Compile failed: %v", err)
	}

	if ds.lastSnapSeq != 19 || ds.lastSnapIncludePending != false {
		t.Fatalf("expected SnapshotAt(seq=19, includePending=false), got seq=%d pending=%v", ds.lastSnapSeq, ds.lastSnapIncludePending)
	}
	if ds.lastSearchMaxSeq != 19 {
		t.Fatalf("expected spoiler-free SearchMemory maxChapterSeq=19, got %d", ds.lastSearchMaxSeq)
	}

	slotByKey := map[string]domain.BriefingSlot{}
	sumEst := 0
	for _, s := range brief.Slots {
		slotByKey[s.Key] = s
		sumEst += s.TokenEst
	}
	if brief.TotalTokenEst != sumEst {
		t.Fatalf("TotalTokenEst (%d) != sum of slot estimates (%d)", brief.TotalTokenEst, sumEst)
	}

	// P2：必须以快照属性覆盖静态旧属性（筑基初期替换掉炼气九层）。
	p2 := slotByKey["p2_entities"]
	if !strings.Contains(p2.Content, "筑基初期（已确认快照）") || strings.Contains(p2.Content, "炼气九层") {
		t.Fatalf("P2 did not override stale static attributes with SnapshotAt: %s", p2.Content)
	}

	// P3：超期伏笔排首位且带预警，已回收/未来埋设伏笔绝不出现。
	p3 := slotByKey["p3_foreshadowings"]
	overdueIdx := strings.Index(p3.Content, "超期核心伏笔：血玉密库")
	futureIdx := strings.Index(p3.Content, "远期普通伏笔：海外仙岛")
	if overdueIdx < 0 || futureIdx < 0 || overdueIdx >= futureIdx {
		t.Fatalf("expected overdue foreshadowing ranked ahead of distant foreshadowing: %s", p3.Content)
	}
	if strings.Contains(p3.Content, "已回收伏笔") || strings.Contains(p3.Content, "未来章才埋设") {
		t.Fatalf("resolved or future foreshadowing leaked into P3: %s", p3.Content)
	}

	// P5：包含卷摘要 + 1~10章弧线摘要 + 近15章因果摘要（第5~19章，不含第4章也不含未来第21章或11~25弧线）。
	p5 := slotByKey["p5_summary_pyramid"]
	if !strings.Contains(p5.Content, "第一卷前半：外门崛起") || !strings.Contains(p5.Content, "1~10章弧线") {
		t.Fatalf("P5 missing volume or arc_10 summary: %s", p5.Content)
	}
	if strings.Contains(p5.Content, "未来绝对不可泄露") || strings.Contains(p5.Content, "未来超前弧线") {
		t.Fatalf("future summary leaked into P5: %s", p5.Content)
	}
	if strings.Contains(p5.Content, "【因果标记:A】") {
		t.Fatalf("expected only recent 15 chapters (ch5..ch19), ch4 should be excluded")
	}
	if !strings.Contains(p5.Content, "【因果标记:P】") {
		t.Fatalf("expected latest ch19 causal summary present in P5")
	}

	// P6：最多 3 条且严防第 25 章未来剧透片段进入。
	p6 := slotByKey["p6_memory_search"]
	if !strings.Contains(p6.Content, "【命中片段1】") || !strings.Contains(p6.Content, "【命中片段3】") {
		t.Fatalf("P6 missing expected search hits: %s", p6.Content)
	}
	if strings.Contains(p6.Content, "第25章剧透") {
		t.Fatalf("future chapter search hit leaked into P6: %s", p6.Content)
	}

	// 验证 IncludePending=true 切换后 P2 读取同批待审状态。
	pendingBrief, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{
		IncludePending: true,
	})
	if err != nil {
		t.Fatalf("Compile(IncludePending=true) failed: %v", err)
	}
	if !ds.lastSnapIncludePending {
		t.Fatalf("expected IncludePending=true forwarded to SnapshotAt")
	}
	if !strings.Contains(pendingBrief.UserPrompt, "经脉微损（同批第19章pending）") {
		t.Fatalf("expected pending state attribute visible when IncludePending=true")
	}
}

func TestCompilePhase2SummaryPyramidTrimmingAndForeshadowBudget(t *testing.T) {
	ds := newPhase2Fixture()
	comp := New(ds)

	full, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{})
	if err != nil {
		t.Fatalf("full compile failed: %v", err)
	}
	// 计算 P0+P1+P2+P3+P4 占用，然后仅给 P5 留下约 180 token，迫使 P5 从最远单章因果摘要开始收缩。
	preP5Tokens := 0
	for _, s := range full.Slots {
		if s.Priority == "P0" || s.Priority == "P1" || s.Priority == "P2" || s.Priority == "P3" || s.Priority == "P4" {
			preP5Tokens += s.TokenEst
		}
	}
	tightBudget := preP5Tokens + 180

	tight, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{
		TokenBudget: tightBudget,
	})
	if err != nil {
		t.Fatalf("tight compile failed: %v", err)
	}
	var p5 domain.BriefingSlot
	for _, s := range tight.Slots {
		if s.Key == "p5_summary_pyramid" {
			p5 = s
		}
	}
	if !p5.Truncated {
		t.Fatalf("expected P5 truncated under tight budget %d (est=%d)", tightBudget, p5.TokenEst)
	}
	if p5.TokenEst > 180 {
		t.Fatalf("P5 TokenEst (%d) exceeded remaining cap (180)", p5.TokenEst)
	}
	if tight.TotalTokenEst > tightBudget {
		t.Fatalf("TotalTokenEst (%d) exceeded tightBudget (%d)", tight.TotalTokenEst, tightBudget)
	}
	// 最远的第 5 章（标记 B）应比最新章节更早被裁剪掉，高浓缩卷/弧线摘要得到优先保留。
	if strings.Contains(p5.Content, "【因果标记:B】") {
		t.Fatalf("oldest chapter causal summary should have been trimmed first: %s", p5.Content)
	}
}

type fakeStyleDataSource struct {
	*fakeMemoryDataSource
	profile          domain.StyleProfile
	samples          []domain.StyleSample
	rules            []domain.DeaiRule
	lastQueriedScene domain.SceneType
}

func (f *fakeStyleDataSource) GetStyleProfile(_ context.Context, _ int64) (domain.StyleProfile, error) {
	return f.profile, nil
}

func (f *fakeStyleDataSource) ListStyleSamples(_ context.Context, _ int64, sceneType domain.SceneType) ([]domain.StyleSample, error) {
	f.lastQueriedScene = sceneType
	out := make([]domain.StyleSample, 0, len(f.samples))
	for _, s := range f.samples {
		if sceneType != "" && s.SceneType != sceneType {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeStyleDataSource) ListDeaiRules(_ context.Context, _ int64) ([]domain.DeaiRule, error) {
	return f.rules, nil
}

func newPhase3Fixture() *fakeStyleDataSource {
	p2 := newPhase2Fixture()
	return &fakeStyleDataSource{
		fakeMemoryDataSource: p2,
		profile: domain.StyleProfile{
			ID:                  1,
			BookID:              1,
			AvgSentenceChars:    16.5,
			MaxParagraphChars:   120,
			DialogueRatioMin:    0.30,
			DialogueRatioMax:    0.50,
			PovRule:             domain.PovThirdLimited,
			ToneDirectives:      "冷峻克制，动作与招式克制优先于心理描写",
			ForbiddenTropesNote: "严禁嘴角勾起与章末鸡汤",
		},
		samples: []domain.StyleSample{
			{
				ID:        1,
				BookID:    1,
				SceneType: domain.SceneCombat,
				Title:     "雨巷反杀锚点一",
				Excerpt:   "【战斗样章1】铁剑贴着阴雷珠擦过，火星溅入雨水，林渊借反震之力横移三尺，剑脊重重拍碎阵旗。" + strings.Repeat("剑锋破空，招式狠准。", 20),
			},
			{
				ID:        2,
				BookID:    1,
				SceneType: domain.SceneCombat,
				Title:     "残阵斩敌锚点二",
				Excerpt:   "【战斗样章2】三道阵纹同时亮起，王煞左脚刚踏入死门，护体罡气便被地底引出的阴雷撕开一道缺口。" + strings.Repeat("雷光炸裂，寸步不让。", 20),
			},
			{
				ID:        3,
				BookID:    1,
				SceneType: domain.SceneDialogue,
				Title:     "茶楼博弈锚点",
				Excerpt:   "【对话样章】“三百灵石买你一条退路。”他放下茶盏。",
			},
		},
		rules: []domain.DeaiRule{
			{ID: 1, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "嘴角勾起", Severity: domain.LintSeverityError, MaxPerChapter: 0, Enabled: true},
			{ID: 2, BookID: 1, Category: domain.DeaiCategoryCliche, Pattern: "恐怖如斯", Severity: domain.LintSeverityError, MaxPerChapter: 0, Enabled: true},
		},
	}
}

func TestCompilePhase3StyleDNASceneMatchingAndTrimming(t *testing.T) {
	ds := newPhase3Fixture()
	comp := New(ds) // 自动通过类型断言激活 MemoryDataSource + StyleDataSource

	brief, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{})
	if err != nil {
		t.Fatalf("Phase 3 Compile failed: %v", err)
	}

	// 本章节拍为“引动残阵雷纹废掉王煞护体罡气并一剑绝杀”，应自动推断主导场景为 combat。
	if ds.lastQueriedScene != domain.SceneCombat {
		t.Fatalf("expected dominant scene inferred as combat, got %q", ds.lastQueriedScene)
	}

	var p7 domain.BriefingSlot
	foundP7 := false
	sumEst := 0
	for _, s := range brief.Slots {
		sumEst += s.TokenEst
		if s.Key == "p7_style_dna" {
			p7 = s
			foundP7 = true
		}
	}
	if !foundP7 {
		t.Fatalf("expected p7_style_dna slot in briefing slots")
	}
	if brief.TotalTokenEst != sumEst {
		t.Fatalf("TotalTokenEst (%d) != sum of slots (%d)", brief.TotalTokenEst, sumEst)
	}
	if p7.Truncated {
		t.Fatalf("expected P7 not truncated under default 12000 budget")
	}
	for _, must := range []string{"平均句长约 16", "单段上限 120 字", "冷峻克制", "嘴角勾起", "恐怖如斯", "【战斗样章1】", "【战斗样章2】"} {
		if !strings.Contains(p7.Content, must) {
			t.Errorf("P7 content missing %q:\n%s", must, p7.Content)
		}
	}
	if strings.Contains(p7.Content, "【对话样章】") {
		t.Errorf("P7 should only include matching combat scene samples")
	}

	// 验证预算收紧时 P7 自动从 2 段样章缩减为 1 段样章（遵循 docs/03 §1.3 裁剪策略）。
	preP7Tokens := brief.TotalTokenEst - p7.TokenEst
	oneSampleBudget := preP7Tokens + (p7.TokenEst * 2 / 3)
	tightBrief, err := comp.Compile(context.Background(), 100, domain.BriefingOptions{
		TokenBudget: oneSampleBudget,
	})
	if err != nil {
		t.Fatalf("tight Phase 3 Compile failed: %v", err)
	}
	var tightP7 domain.BriefingSlot
	for _, s := range tightBrief.Slots {
		if s.Key == "p7_style_dna" {
			tightP7 = s
		}
	}
	if !tightP7.Truncated {
		t.Fatalf("expected P7 Truncated=true when shrinking from 2 samples to 1")
	}
	if !strings.Contains(tightP7.Content, "【战斗样章1】") || strings.Contains(tightP7.Content, "【战斗样章2】") {
		t.Fatalf("expected P7 shrunk to 1 sample excerpt, got:\n%s", tightP7.Content)
	}
}
