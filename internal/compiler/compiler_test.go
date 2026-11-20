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
