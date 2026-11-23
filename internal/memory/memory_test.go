package memory

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"nib/internal/domain"
	"nib/internal/llm"
)

// fakeMemoryStore 模拟完整事件溯源账本（支持 30 章 Golden 回归测试的回溯、提交与改章撤回）。
type fakeMemoryStore struct {
	book           domain.Book
	chapters       map[int64]domain.Chapter
	entities       map[int64]domain.Entity
	events         []domain.EntityStateEvent
	foreshadowings []domain.Foreshadowing
	pendingDeltas  map[int64]domain.ChapterDelta
	routes         map[domain.PipelineStage]domain.ModelRoute
	providers      map[int64]domain.Provider
	nextDeltaID    int64
	nextEventID    int64
}

func newFakeMemoryStore(book domain.Book) *fakeMemoryStore {
	providerID := int64(1)
	return &fakeMemoryStore{
		book:          book,
		chapters:      map[int64]domain.Chapter{},
		entities:      map[int64]domain.Entity{},
		pendingDeltas: map[int64]domain.ChapterDelta{},
		routes: map[domain.PipelineStage]domain.ModelRoute{
			domain.StageExtract: {
				Stage:       domain.StageExtract,
				ProviderID:  &providerID,
				ModelName:   "deepseek-chat",
				Temperature: 0.1,
				MaxTokens:   2048,
			},
		},
		providers: map[int64]domain.Provider{
			providerID: {
				ID:       providerID,
				Name:     "ExtractRelay",
				Protocol: domain.ProtocolOpenAICompat,
				BaseURL:  "https://api.example.com/v1",
				APIKey:   "sk-extract-test-1234",
				Models:   []string{"deepseek-chat"},
				Enabled:  true,
			},
		},
	}
}

func (s *fakeMemoryStore) GetBook(_ context.Context, id int64) (domain.Book, error) {
	if s.book.ID != id {
		return domain.Book{}, domain.ErrNotFound
	}
	return s.book, nil
}

func (s *fakeMemoryStore) GetChapter(_ context.Context, id int64) (domain.Chapter, error) {
	ch, ok := s.chapters[id]
	if !ok {
		return domain.Chapter{}, domain.ErrNotFound
	}
	return ch, nil
}

func (s *fakeMemoryStore) ListEntities(_ context.Context, _ int64, _ domain.EntityFilter) ([]domain.Entity, error) {
	out := make([]domain.Entity, 0, len(s.entities))
	for _, e := range s.entities {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b domain.Entity) int { return int(a.ID - b.ID) })
	return out, nil
}

func (s *fakeMemoryStore) SnapshotAt(_ context.Context, _ int64, chapterSeq int, includePending bool) ([]domain.EntitySnapshot, error) {
	ents, _ := s.ListEntities(context.Background(), s.book.ID, domain.EntityFilter{})
	out := make([]domain.EntitySnapshot, 0, len(ents))
	for _, e := range ents {
		eff := map[string]string{}
		maps.Copy(eff, e.Attributes)
		applied := 0
		pending := 0
		for _, ev := range s.events {
			if ev.EntityID != e.ID || ev.ChapterSeq > chapterSeq {
				continue
			}
			if ev.Status == domain.StateEventCommitted {
				eff[ev.AttrKey] = ev.NewValue
				applied++
			} else if includePending && ev.Status == domain.StateEventPending {
				eff[ev.AttrKey] = ev.NewValue
				applied++
				pending++
			}
		}
		out = append(out, domain.EntitySnapshot{
			Entity:              e,
			EffectiveAttributes: eff,
			AppliedEventCount:   applied,
			PendingEventCount:   pending,
		})
	}
	return out, nil
}

func (s *fakeMemoryStore) ListForeshadowings(_ context.Context, _ int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error) {
	out := make([]domain.Foreshadowing, 0, len(s.foreshadowings))
	for _, fs := range s.foreshadowings {
		if status != "" && fs.Status != status {
			continue
		}
		out = append(out, fs)
	}
	return out, nil
}

func (s *fakeMemoryStore) SavePendingDelta(_ context.Context, delta domain.ChapterDelta) (domain.ChapterDelta, error) {
	s.nextDeltaID++
	delta.ID = s.nextDeltaID
	delta.ReviewStatus = domain.DeltaReviewPending
	s.pendingDeltas[delta.ChapterID] = delta

	chap := s.chapters[delta.ChapterID]
	// 将该章之前残留的 pending 事件置为 reverted，再追加本轮 pending 事件（模拟 store.SavePendingDelta 语义）。
	for i := range s.events {
		if s.events[i].ChapterID == delta.ChapterID && s.events[i].Status == domain.StateEventPending {
			s.events[i].Status = domain.StateEventReverted
		}
	}
	for _, p := range delta.StatePatches {
		if p.EntityID <= 0 {
			continue
		}
		s.nextEventID++
		s.events = append(s.events, domain.EntityStateEvent{
			ID:             s.nextEventID,
			BookID:         s.book.ID,
			EntityID:       p.EntityID,
			ChapterID:      delta.ChapterID,
			ChapterSeq:     chap.GlobalSeq,
			AttrKey:        p.AttrKey,
			PrevValue:      p.PrevValue,
			NewValue:       p.NewValue,
			ChangeSeverity: p.Severity,
			Reason:         p.Reason,
			Status:         domain.StateEventPending,
		})
	}
	return delta, nil
}

func (s *fakeMemoryStore) commitChapterEvents(chapterID int64) {
	for i := range s.events {
		if s.events[i].ChapterID == chapterID && s.events[i].Status == domain.StateEventPending {
			s.events[i].Status = domain.StateEventCommitted
		}
	}
	if d, ok := s.pendingDeltas[chapterID]; ok {
		d.ReviewStatus = domain.DeltaReviewApplied
		s.pendingDeltas[chapterID] = d
	}
}

func (s *fakeMemoryStore) revertChapterEvents(chapterID int64) {
	for i := range s.events {
		if s.events[i].ChapterID == chapterID && s.events[i].Status != domain.StateEventReverted {
			s.events[i].Status = domain.StateEventReverted
		}
	}
}

func (s *fakeMemoryStore) GetModelRoute(_ context.Context, stage domain.PipelineStage) (domain.ModelRoute, error) {
	r, ok := s.routes[stage]
	if !ok {
		return domain.ModelRoute{}, domain.ErrNotFound
	}
	return r, nil
}

func (s *fakeMemoryStore) GetProvider(_ context.Context, id int64) (domain.Provider, error) {
	p, ok := s.providers[id]
	if !ok {
		return domain.Provider{}, domain.ErrNotFound
	}
	return p, nil
}

type scriptedLLM struct {
	responses []string
	calls     int
	prompts   []llm.ChatRequest
}

func (m *scriptedLLM) Stream(
	_ context.Context,
	_ domain.Provider,
	req llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
) (domain.TokenUsage, error) {
	m.prompts = append(m.prompts, req)
	idx := m.calls
	m.calls++
	if idx >= len(m.responses) {
		return domain.TokenUsage{}, errors.New("no scripted response")
	}
	resp := m.responses[idx]
	if err := onEvent(llm.StreamEvent{Delta: resp}); err != nil {
		return domain.TokenUsage{}, err
	}
	return domain.TokenUsage{PromptTokens: 200, CompletionTokens: 80, TotalTokens: 280}, nil
}

func TestClassifyHighRiskAllFiveRules(t *testing.T) {
	tests := []struct {
		name  string
		genre domain.GenreID
		patch domain.DeltaStatePatch
		want  domain.ChangeSeverity
	}{
		// 规则 1：存活/死亡/失踪/夺舍/废除修为 vs 普通可恢复轻伤
		{
			name:  "rule1 character killed is high_risk",
			genre: domain.GenreXianxia,
			patch: domain.DeltaStatePatch{EntityName: "王执事", AttrKey: "status", PrevValue: "存活", NewValue: "已被残阵反杀身死", Reason: "第42章击杀"},
			want:  domain.ChangeSeverityHighRisk,
		},
		{
			name:  "rule1 crippled cultivation is high_risk",
			genre: domain.GenreXuanhuan,
			patch: domain.DeltaStatePatch{EntityName: "萧炎", AttrKey: "injury", PrevValue: "完好", NewValue: "丹田破碎，修为尽废", Reason: "遭宗门废除修为"},
			want:  domain.ChangeSeverityHighRisk,
		},
		{
			name:  "rule1 reversible shoulder injury from docs is normal",
			genre: domain.GenreXianxia,
			patch: domain.DeltaStatePatch{EntityName: "林渊", AttrKey: "injury", PrevValue: "完好", NewValue: "左肩阴雷暗伤（三日内不可强动真元）", Reason: "第42章交战受创"},
			want:  domain.ChangeSeverityNormal,
		},

		// 规则 2：境界/阶层跨大阶跃升 vs 同大阶小层提升
		{
			name:  "rule2 major realm breakthrough is high_risk",
			genre: domain.GenreXianxia,
			patch: domain.DeltaStatePatch{EntityName: "林渊", AttrKey: "cultivation", PrevValue: "炼气九层", NewValue: "筑基初期", Reason: "服下破障丹破境"},
			want:  domain.ChangeSeverityHighRisk,
		},
		{
			name:  "rule2 sub-realm step within same major realm is normal",
			genre: domain.GenreXianxia,
			patch: domain.DeltaStatePatch{EntityName: "林渊", AttrKey: "cultivation", PrevValue: "筑基初期", NewValue: "筑基中期", Reason: "闭关稳固境界"},
			want:  domain.ChangeSeverityNormal,
		},
		{
			name:  "rule2 xuanhuan major tier jump is high_risk",
			genre: domain.GenreXuanhuan,
			patch: domain.DeltaStatePatch{EntityName: "萧尘", AttrKey: "realm", PrevValue: "灵海境巅峰", NewValue: "宗师初期", Reason: "血脉觉醒破大境"},
			want:  domain.ChangeSeverityHighRisk,
		},

		// 规则 3：核心道具/功法/股权易主或损毁
		{
			name:  "rule3 item ownership transfer from docs is high_risk",
			genre: domain.GenreXianxia,
			patch: domain.DeltaStatePatch{EntityName: "破障丹", AttrKey: "owner", PrevValue: "王执事", NewValue: "林渊", Reason: "击杀战利品易主"},
			want:  domain.ChangeSeverityHighRisk,
		},
		{
			name:  "rule3 natal treasure destroyed is high_risk",
			genre: domain.GenreXianxia,
			patch: domain.DeltaStatePatch{EntityName: "林渊", AttrKey: "natal_treasure", PrevValue: "青冥飞剑（完好）", NewValue: "青冥飞剑碎裂损毁", Reason: "硬抗雷劫自毁"},
			want:  domain.ChangeSeverityHighRisk,
		},
		{
			name:  "rule3 company equity transfer in urban is high_risk",
			genre: domain.GenreUrban,
			patch: domain.DeltaStatePatch{EntityName: "星海科技", AttrKey: "equity", PrevValue: "周明持股51%", NewValue: "周明股权被转让，陆沉控股60%", Reason: "对赌协议交割"},
			want:  domain.ChangeSeverityHighRisk,
		},

		// 规则 4：阵营关系翻转
		{
			name:  "rule4 ally to enemy flip is high_risk",
			genre: domain.GenreXuanhuan,
			patch: domain.DeltaStatePatch{EntityName: "赵长老", AttrKey: "relation", PrevValue: "青云宗盟友", NewValue: "公开反目成死敌", Reason: "密谋勾结黑煞宗"},
			want:  domain.ChangeSeverityHighRisk,
		},
		{
			name:  "rule4 faction defection is high_risk",
			genre: domain.GenreXianxia,
			patch: domain.DeltaStatePatch{EntityName: "顾寒", AttrKey: "affiliation", PrevValue: "青云宗外门", NewValue: "黑煞宗", Reason: "叛宗投敌"},
			want:  domain.ChangeSeverityHighRisk,
		},

		// 规则 5：都市现金流单章变动 >= 30% vs < 30%
		{
			name:  "rule5 urban cashflow +35% is high_risk",
			genre: domain.GenreUrban,
			patch: domain.DeltaStatePatch{EntityName: "陆沉", AttrKey: "cash_flow", PrevValue: "可用资金 100 万", NewValue: "可用资金 135 万", Reason: "短线套利落袋"},
			want:  domain.ChangeSeverityHighRisk,
		},
		{
			name:  "rule5 urban cashflow -40% in yi is high_risk",
			genre: domain.GenreUrban,
			patch: domain.DeltaStatePatch{EntityName: "陆沉", AttrKey: "cash_flow", PrevValue: "1.5亿", NewValue: "0.9亿", Reason: "竞拍核心地块支付定金"},
			want:  domain.ChangeSeverityHighRisk,
		},
		{
			name:  "rule5 urban cashflow +15% is normal",
			genre: domain.GenreUrban,
			patch: domain.DeltaStatePatch{EntityName: "陆沉", AttrKey: "cash_flow", PrevValue: "可用资金 100 万", NewValue: "可用资金 115 万", Reason: "日常分红到账"},
			want:  domain.ChangeSeverityNormal,
		},
		{
			name:  "rule5 non-urban money change does not trigger urban rule",
			genre: domain.GenreXuanhuan,
			patch: domain.DeltaStatePatch{EntityName: "林渊", AttrKey: "cash_flow", PrevValue: "100万", NewValue: "200万", Reason: "日常消耗"},
			want:  domain.ChangeSeverityNormal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyHighRisk(tc.genre, tc.patch)
			if got != tc.want {
				t.Fatalf("ClassifyHighRisk(%s, %+v) = %q, want %q", tc.genre, tc.patch, got, tc.want)
			}
		})
	}
}

func TestExtractChapterDeltaFenceStrippingAndSelfRepair(t *testing.T) {
	t.Run("strips markdown json fence and assigns stable IDs + high_risk override", func(t *testing.T) {
		store := newFakeMemoryStore(domain.Book{ID: 1, Title: "斩道", Genre: domain.GenreXianxia})
		store.entities[10] = domain.Entity{
			ID: 10, BookID: 1, Kind: domain.EntityKindCharacter, Name: "林渊",
			Attributes: map[string]string{"injury": "完好", "cultivation": "炼气九层"},
		}
		store.entities[20] = domain.Entity{
			ID: 20, BookID: 1, Kind: domain.EntityKindItem, Name: "破障丹",
			Attributes: map[string]string{"owner": "王执事"},
		}
		store.chapters[42] = domain.Chapter{
			ID: 42, BookID: 1, GlobalSeq: 42, Title: "残阵反杀",
			Content: "林渊借助残阵击杀王执事，夺得破障丹与血玉令牌，左肩被阴雷震伤。",
		}

		// 模型刻意把破障丹易主标为 normal 且包裹在 markdown 围栏和闲聊中，验证剥离与高危兜底。
		mockLLM := &scriptedLLM{
			responses: []string{
				"以下是本章记忆抽取结果：\n```json\n" + `{
  "causal_summary": "林渊借残阵反杀王执事，夺破障丹与血玉令牌，左肩受阴雷暗伤。",
  "new_entities": [
    {"kind": "item", "name": "血玉令牌", "summary": "刻有古宗阵纹的令牌", "attributes": {"owner": "林渊"}}
  ],
  "state_patches": [
    {"entity_name": "林渊", "attr_key": "injury", "new_value": "左肩阴雷暗伤（三日内不可强动真元）", "severity": "normal", "reason": "交战受创"},
    {"entity_name": "破障丹", "attr_key": "owner", "prev_value": "王执事", "new_value": "林渊", "severity": "normal", "reason": "击杀夺宝"}
  ],
  "foreshadowing_updates": [
    {"action": "plant", "title": "血玉令牌背后的宗门密库", "description": "令牌背面刻有古宗阵纹", "target_resolve_seq": 75, "involved_entity_names": ["林渊"]}
  ]
}` + "\n```\n提取完毕。",
			},
		}

		ext := NewExtractor(store, mockLLM)
		delta, err := ext.ExtractChapterDelta(context.Background(), 42)
		if err != nil {
			t.Fatalf("ExtractChapterDelta failed: %v", err)
		}
		if mockLLM.calls != 1 {
			t.Fatalf("expected 1 LLM call, got %d", mockLLM.calls)
		}
		if delta.ReviewStatus != domain.DeltaReviewPending {
			t.Fatalf("delta must stay pending, got %q", delta.ReviewStatus)
		}
		if len(delta.NewEntities) != 1 || delta.NewEntities[0].ID != "ent-1" {
			t.Fatalf("unexpected new entities: %+v", delta.NewEntities)
		}
		if len(delta.StatePatches) != 2 || delta.StatePatches[0].ID != "patch-1" || delta.StatePatches[1].ID != "patch-2" {
			t.Fatalf("unexpected state patches: %+v", delta.StatePatches)
		}
		// 验证自动补齐 EntityID 和 PrevValue。
		if delta.StatePatches[0].EntityID != 10 || delta.StatePatches[0].PrevValue != "完好" {
			t.Fatalf("expected patch-1 resolved entityID=10 & prevValue='完好', got %+v", delta.StatePatches[0])
		}
		if delta.StatePatches[0].Severity != domain.ChangeSeverityNormal {
			t.Fatalf("expected injury patch normal, got %q", delta.StatePatches[0].Severity)
		}
		// 即使模型误标 normal，规则 3 必须强制标红破障丹易主！
		if delta.StatePatches[1].EntityID != 20 || delta.StatePatches[1].Severity != domain.ChangeSeverityHighRisk {
			t.Fatalf("expected owner patch forced high_risk, got %+v", delta.StatePatches[1])
		}
		if delta.HighRiskFlags != 1 {
			t.Fatalf("expected HighRiskFlags=1, got %d", delta.HighRiskFlags)
		}
		if len(delta.Foreshadows) != 1 || delta.Foreshadows[0].ID != "fs-1" || !slices.Contains(delta.Foreshadows[0].InvolvedEntityIDs, 10) {
			t.Fatalf("unexpected foreshadowings: %+v", delta.Foreshadows)
		}
	})

	t.Run("1-shot self-repair succeeds when first response is broken JSON", func(t *testing.T) {
		store := newFakeMemoryStore(domain.Book{ID: 1, Title: "都市神豪", Genre: domain.GenreUrban})
		store.chapters[5] = domain.Chapter{ID: 5, BookID: 1, GlobalSeq: 5, Title: "精准抄底", Content: "陆沉账户资金从100万暴涨至180万。"}

		mockLLM := &scriptedLLM{
			responses: []string{
				`{"causal_summary": "未闭合的坏 JSON`,
				`{"causal_summary": "陆沉精准抄底获利八十万。", "new_entities": [], "state_patches": [], "foreshadowing_updates": []}`,
			},
		}
		ext := NewExtractor(store, mockLLM)
		delta, err := ext.ExtractChapterDelta(context.Background(), 5)
		if err != nil {
			t.Fatalf("expected 1-shot repair to succeed, got %v", err)
		}
		if mockLLM.calls != 2 {
			t.Fatalf("expected exactly 2 LLM calls (initial + 1 repair), got %d", mockLLM.calls)
		}
		if delta.CausalSummary != "陆沉精准抄底获利八十万。" {
			t.Fatalf("unexpected repaired summary: %q", delta.CausalSummary)
		}
	})

	t.Run("persistent bad JSON fails non-destructively without touching prose", func(t *testing.T) {
		store := newFakeMemoryStore(domain.Book{ID: 1, Title: "都市神豪", Genre: domain.GenreUrban})
		origContent := "原正文一字不可损坏。"
		store.chapters[6] = domain.Chapter{ID: 6, BookID: 1, GlobalSeq: 6, Title: "谈判", Content: origContent}

		mockLLM := &scriptedLLM{
			responses: []string{
				"抱歉我无法按照格式输出。",
				"仍然不是 JSON 文本。",
			},
		}
		ext := NewExtractor(store, mockLLM)
		_, err := ext.ExtractChapterDelta(context.Background(), 6)
		if err == nil {
			t.Fatalf("expected error after persistent invalid JSON")
		}
		if mockLLM.calls != 2 {
			t.Fatalf("expected initial + 1 repair attempt, got %d", mockLLM.calls)
		}
		if store.chapters[6].Content != origContent {
			t.Fatalf("chapter prose was altered on extract failure")
		}
		if _, saved := store.pendingDeltas[6]; saved {
			t.Fatalf("must not save corrupt delta when parsing failed")
		}
	})
}

// TestGolden30ChapterAntiContinuityBreak 对应 docs/05 §4 规定的 30 章防吃书 Golden 回归测试：
// 模拟连续 30 章剧情演进：
//   - 第 12 章：半块玉佩易主（林渊 -> 师妹苏婉，high_risk）
//   - 第 15 章：林渊左肩受阴雷暗伤（normal）
//   - 第 18 章：黑市拍卖玉佩被林渊拍回（苏婉 -> 林渊，high_risk）
//   - 第 20 章：林渊服药痊愈（normal）
//   - 第 25 章：林渊从炼气九层突破至筑基初期（high_risk）
//   - 第 30 章：全量时间旅行验证（ch10/ch12/ch15/ch18/ch20/ch25/ch30 绝不泄露未来章节），并模拟重写撤回第 18 章后历史回滚准确。
func TestGolden30ChapterAntiContinuityBreak(t *testing.T) {
	store := newFakeMemoryStore(domain.Book{ID: 1, Title: "三十章防吃书基准测试", Genre: domain.GenreXianxia})
	store.entities[1] = domain.Entity{
		ID: 1, BookID: 1, Kind: domain.EntityKindCharacter, Name: "林渊",
		Attributes: map[string]string{
			"cultivation": "炼气九层",
			"injury":      "完好",
		},
	}
	store.entities[2] = domain.Entity{
		ID: 2, BookID: 1, Kind: domain.EntityKindItem, Name: "半块玉佩",
		Attributes: map[string]string{
			"owner": "林渊",
		},
	}

	// 准备 30 章连续文本与关键事件脚本。
	responses := make([]string, 0, 30)
	for seq := 1; seq <= 30; seq++ {
		store.chapters[int64(seq)] = domain.Chapter{
			ID:        int64(seq),
			BookID:    1,
			GlobalSeq: seq,
			Title:     fmt.Sprintf("第%d章剧情", seq),
			Content:   fmt.Sprintf("第%d章正文内容推进。", seq),
		}
		switch seq {
		case 12:
			responses = append(responses, `{
				"causal_summary": "第12章林渊将半块玉佩交给师妹苏婉防身。",
				"state_patches": [
					{"entity_name": "半块玉佩", "attr_key": "owner", "prev_value": "林渊", "new_value": "苏婉", "severity": "normal", "reason": "林渊托付给师妹"}
				],
				"foreshadowing_updates": [
					{"action": "plant", "title": "半块玉佩暗藏古宗禁制", "description": "玉佩缺口处有灵纹微光", "target_resolve_seq": 22}
				]
			}`)
		case 15:
			responses = append(responses, `{
				"causal_summary": "第15章林渊遭遇黑煞宗伏击，左肩受阴雷暗伤。",
				"state_patches": [
					{"entity_name": "林渊", "attr_key": "injury", "prev_value": "完好", "new_value": "左肩阴雷暗伤", "severity": "normal", "reason": "夜巷伏击受创"}
				]
			}`)
		case 18:
			responses = append(responses, `{
				"causal_summary": "第18章林渊在黑市拍回遗失的半块玉佩。",
				"state_patches": [
					{"entity_name": "半块玉佩", "attr_key": "owner", "prev_value": "苏婉", "new_value": "林渊", "severity": "normal", "reason": "黑市高价拍回"}
				]
			}`)
		case 20:
			responses = append(responses, `{
				"causal_summary": "第20章林渊炼化清灵散，左肩暗伤痊愈。",
				"state_patches": [
					{"entity_name": "林渊", "attr_key": "injury", "prev_value": "左肩阴雷暗伤", "new_value": "完好", "severity": "normal", "reason": "服药调息痊愈"}
				]
			}`)
		case 25:
			responses = append(responses, `{
				"causal_summary": "第25章林渊闭关冲破瓶颈，踏入筑基初期。",
				"state_patches": [
					{"entity_name": "林渊", "attr_key": "cultivation", "prev_value": "炼气九层", "new_value": "筑基初期", "severity": "normal", "reason": "破境成功"}
				]
			}`)
		default:
			responses = append(responses, fmt.Sprintf(`{"causal_summary": "第%d章常规推进。", "state_patches": [], "foreshadowing_updates": []}`, seq))
		}
	}

	mockLLM := &scriptedLLM{responses: responses}
	ext := NewExtractor(store, mockLLM)
	ctx := context.Background()

	// 串行跑完 30 章抽取与作者审定入库。
	for seq := 1; seq <= 30; seq++ {
		delta, err := ext.ExtractChapterDelta(ctx, int64(seq))
		if err != nil {
			t.Fatalf("chapter %d extract failed: %v", seq, err)
		}
		switch seq {
		case 12, 18, 25:
			if delta.HighRiskFlags != 1 || delta.StatePatches[0].Severity != domain.ChangeSeverityHighRisk {
				t.Fatalf("chapter %d expected 1 high_risk flag, got flags=%d patches=%+v", seq, delta.HighRiskFlags, delta.StatePatches)
			}
		case 15, 20:
			if delta.HighRiskFlags != 0 || delta.StatePatches[0].Severity != domain.ChangeSeverityNormal {
				t.Fatalf("chapter %d expected normal severity, got flags=%d patches=%+v", seq, delta.HighRiskFlags, delta.StatePatches)
			}
		}

		// 验证：在未 commit 前，includePending=false 绝看不到本章 pending 变更，includePending=true 可见。
		if seq == 25 {
			committedOnly, _ := store.SnapshotAt(ctx, 1, 25, false)
			withPending, _ := store.SnapshotAt(ctx, 1, 25, true)
			if findSnapAttr(committedOnly, 1, "cultivation") != "炼气九层" {
				t.Fatalf("uncommitted ch25 event leaked into committed-only snapshot")
			}
			if findSnapAttr(withPending, 1, "cultivation") != "筑基初期" {
				t.Fatalf("pending ch25 event not visible when includePending=true")
			}
		}

		store.commitChapterEvents(int64(seq))
	}

	// 验证任意历史时间点的 SnapshotAt 绝不泄露未来章节状态。
	assertSnap := func(seq int, wantCult, wantInjury, wantOwner string) {
		t.Helper()
		snaps, err := store.SnapshotAt(ctx, 1, seq, false)
		if err != nil {
			t.Fatalf("SnapshotAt(%d) error: %v", seq, err)
		}
		if got := findSnapAttr(snaps, 1, "cultivation"); got != wantCult {
			t.Errorf("ch%d cultivation = %q, want %q", seq, got, wantCult)
		}
		if got := findSnapAttr(snaps, 1, "injury"); got != wantInjury {
			t.Errorf("ch%d injury = %q, want %q", seq, got, wantInjury)
		}
		if got := findSnapAttr(snaps, 2, "owner"); got != wantOwner {
			t.Errorf("ch%d jade owner = %q, want %q", seq, got, wantOwner)
		}
	}

	assertSnap(10, "炼气九层", "完好", "林渊")
	assertSnap(12, "炼气九层", "完好", "苏婉")
	assertSnap(15, "炼气九层", "左肩阴雷暗伤", "苏婉")
	assertSnap(17, "炼气九层", "左肩阴雷暗伤", "苏婉")
	assertSnap(18, "炼气九层", "左肩阴雷暗伤", "林渊")
	assertSnap(20, "炼气九层", "完好", "林渊")
	assertSnap(24, "炼气九层", "完好", "林渊")
	assertSnap(25, "筑基初期", "完好", "林渊")
	assertSnap(30, "筑基初期", "完好", "林渊")

	// 模拟改写并撤回第 18 章玉佩易主事件：回滚后第 18~30 章玉佩归属自动回到苏婉，不影响第 25 章突破。
	store.revertChapterEvents(18)
	assertSnap(18, "炼气九层", "左肩阴雷暗伤", "苏婉")
	assertSnap(30, "筑基初期", "完好", "苏婉")

	// 确认第 25 章抽取 prompt 读取到的是第 24 章（修为仍为炼气九层、伤势已完好），绝不含未来剧透。
	ch25Prompt := mockLLM.prompts[24].Messages[0].Content
	if !strings.Contains(ch25Prompt, "cultivation=炼气九层") || !strings.Contains(ch25Prompt, "injury=完好") {
		t.Fatalf("ch25 extract prompt did not reflect ch24 snapshot: %s", ch25Prompt)
	}
}

func findSnapAttr(snaps []domain.EntitySnapshot, entityID int64, key string) string {
	for _, s := range snaps {
		if s.Entity.ID == entityID {
			return s.EffectiveAttributes[key]
		}
	}
	return ""
}

type fallbackMockLLM struct {
	calledModels []string
}

func (m *fallbackMockLLM) Stream(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
	m.calledModels = append(m.calledModels, req.Model)
	if req.Model == "primary-down" {
		return domain.TokenUsage{}, errors.New("upstream 503")
	}
	payload := `{"causal_summary":"林渊收剑入鞘。","state_patches":[],"foreshadow_updates":[],"relation_updates":[]}`
	if onEvent != nil {
		_ = onEvent(llm.StreamEvent{Delta: payload})
	}
	return domain.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}, nil
}

func TestExtractorFallbackToBackupModel(t *testing.T) {
	st := newFakeMemoryStore(domain.Book{ID: 1, Title: "回退测试", Genre: domain.GenreXianxia})
	fbProvID := int64(2)
	st.providers[fbProvID] = domain.Provider{
		ID:       fbProvID,
		Name:     "BackupProvider",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://backup.example.com/v1",
		Enabled:  true,
	}
	r := st.routes[domain.StageExtract]
	r.ModelName = "primary-down"
	r.FallbackProviderID = &fbProvID
	r.FallbackModelName = "backup-ok"
	st.routes[domain.StageExtract] = r
	st.chapters[100] = domain.Chapter{
		ID: 100, BookID: 1, VolumeID: 1, GlobalSeq: 1, Title: "初章", Content: "林渊推开石门。",
	}

	mock := &fallbackMockLLM{}
	ext := NewExtractor(st, mock)
	delta, err := ext.ExtractChapterDelta(context.Background(), 100)
	if err != nil {
		t.Fatalf("ExtractChapterDelta should succeed via fallback model: %v", err)
	}
	if delta.CausalSummary != "林渊收剑入鞘。" {
		t.Fatalf("unexpected delta: %+v", delta)
	}
	if len(mock.calledModels) != 2 || mock.calledModels[0] != "primary-down" || mock.calledModels[1] != "backup-ok" {
		t.Fatalf("expected primary then backup model call, got %+v", mock.calledModels)
	}
}
