package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"nib/internal/domain"
	"nib/internal/genres"
	"nib/internal/llm"
)

const (
	defaultExtractMaxTokens = 2048
	maxRepairPreviewRunes   = 4000
)

// Store 定义完章记忆增量抽取所需的最小仓储接口（直接由 *store.Store 满足）。
type Store interface {
	GetBook(ctx context.Context, id int64) (domain.Book, error)
	GetChapter(ctx context.Context, id int64) (domain.Chapter, error)
	ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error)
	SnapshotAt(ctx context.Context, bookID int64, chapterSeq int, includePending bool) ([]domain.EntitySnapshot, error)
	ListForeshadowings(ctx context.Context, bookID int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error)
	SavePendingDelta(ctx context.Context, delta domain.ChapterDelta) (domain.ChapterDelta, error)
	GetModelRoute(ctx context.Context, stage domain.PipelineStage) (domain.ModelRoute, error)
	GetProvider(ctx context.Context, id int64) (domain.Provider, error)
}

// LLMStreamer 定义流式模型调用接口（直接由 *llm.Client 满足）。
type LLMStreamer interface {
	Stream(
		ctx context.Context,
		provider domain.Provider,
		req llm.ChatRequest,
		onEvent func(llm.StreamEvent) error,
	) (domain.TokenUsage, error)
}

// Extractor 负责在完章后抽取因果摘要、新实体、状态属性变更与伏笔更新，并落入待审变更单（永不静默提交）。
type Extractor struct {
	store Store
	llm   LLMStreamer
}

// NewExtractor 构造满足 app.DeltaExtractor 接口的完章记忆抽取器。
func NewExtractor(store Store, streamer LLMStreamer) *Extractor {
	return &Extractor{
		store: store,
		llm:   streamer,
	}
}

// ExtractChapterDelta 读取章节正文与前章世界状态快照，调用 extract 模型抽取结构化 JSON 变更单；
// 若模型首轮输出非法 JSON，自动触发 1 次纠错重问；仍失败则返回非破坏性错误（绝不改动章节正文）。
func (e *Extractor) ExtractChapterDelta(ctx context.Context, chapterID int64) (domain.ChapterDelta, error) {
	if e == nil || e.store == nil || e.llm == nil {
		return domain.ChapterDelta{}, errors.New("memory extractor dependencies not initialized")
	}
	if chapterID <= 0 {
		return domain.ChapterDelta{}, fmt.Errorf("%w: chapterId must be > 0", domain.ErrValidation)
	}

	chap, err := e.store.GetChapter(ctx, chapterID)
	if err != nil {
		return domain.ChapterDelta{}, fmt.Errorf("load chapter %d: %w", chapterID, err)
	}
	if strings.TrimSpace(chap.Content) == "" {
		return domain.ChapterDelta{}, fmt.Errorf("%w: 章节正文为空，无法抽取记忆变更", domain.ErrValidation)
	}

	book, err := e.store.GetBook(ctx, chap.BookID)
	if err != nil {
		return domain.ChapterDelta{}, fmt.Errorf("load book %d: %w", chap.BookID, err)
	}

	route, provider, err := e.resolveExtractRouteAndProvider(ctx)
	if err != nil {
		return domain.ChapterDelta{}, err
	}

	entities, err := e.store.ListEntities(ctx, book.ID, domain.EntityFilter{})
	if err != nil {
		return domain.ChapterDelta{}, fmt.Errorf("list book entities: %w", err)
	}

	prevSeq := chap.GlobalSeq - 1
	if prevSeq < 0 {
		prevSeq = 0
	}
	snapshots, err := e.store.SnapshotAt(ctx, book.ID, prevSeq, true)
	if err != nil {
		return domain.ChapterDelta{}, fmt.Errorf("load snapshot at chapter %d: %w", prevSeq, err)
	}

	allForeshadowings, err := e.store.ListForeshadowings(ctx, book.ID, "")
	if err != nil {
		return domain.ChapterDelta{}, fmt.Errorf("list book foreshadowings: %w", err)
	}
	openForeshadowings := filterOpenForeshadowings(allForeshadowings, chap.GlobalSeq)

	sysPrompt, userPrompt := buildExtractionPrompts(book, chap, snapshots, openForeshadowings)

	rawResp, err := e.collectLLMResponse(ctx, provider, route, sysPrompt, userPrompt)
	if err != nil {
		return domain.ChapterDelta{}, err
	}

	parsed, parseErr := parseRawDeltaPayload(rawResp)
	if parseErr != nil {
		if ctx.Err() != nil {
			return domain.ChapterDelta{}, domain.ErrGenerationCanceled
		}
		// 自动触发 1 次 JSON 纠错重问（遵循 docs/05 §5 风险缓解策略）。
		repairSys, repairUser := buildRepairPrompts(rawResp, parseErr)
		repairedResp, repairCallErr := e.collectLLMResponse(ctx, provider, route, repairSys, repairUser)
		if repairCallErr != nil {
			return domain.ChapterDelta{}, fmt.Errorf("完章抽取 JSON 纠错请求失败（原正文未受影响）: %w", repairCallErr)
		}
		parsed, parseErr = parseRawDeltaPayload(repairedResp)
		if parseErr != nil {
			return domain.ChapterDelta{}, fmt.Errorf("完章记忆抽取 JSON 解析失败（已自动纠错重试 1 次仍无效，原正文未受影响）: %w", parseErr)
		}
	}

	delta := normalizeDelta(chap, book.Genre, parsed, entities, snapshots, allForeshadowings)
	saved, err := e.store.SavePendingDelta(ctx, delta)
	if err != nil {
		return domain.ChapterDelta{}, fmt.Errorf("save pending delta: %w", err)
	}
	return saved, nil
}

func (e *Extractor) resolveExtractRouteAndProvider(ctx context.Context) (domain.ModelRoute, domain.Provider, error) {
	route, err := e.store.GetModelRoute(ctx, domain.StageExtract)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ModelRoute{}, domain.Provider{}, domain.ErrRouteNotConfigured
		}
		return domain.ModelRoute{}, domain.Provider{}, fmt.Errorf("load extract route: %w", err)
	}
	if route.ProviderID == nil || *route.ProviderID <= 0 || strings.TrimSpace(route.ModelName) == "" {
		return domain.ModelRoute{}, domain.Provider{}, domain.ErrRouteNotConfigured
	}

	provider, err := e.store.GetProvider(ctx, *route.ProviderID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ModelRoute{}, domain.Provider{}, domain.ErrProviderNotConfigured
		}
		return domain.ModelRoute{}, domain.Provider{}, fmt.Errorf("load provider %d: %w", *route.ProviderID, err)
	}
	if !provider.Enabled || strings.TrimSpace(provider.BaseURL) == "" {
		return domain.ModelRoute{}, domain.Provider{}, domain.ErrProviderNotConfigured
	}
	return route, provider, nil
}

func (e *Extractor) collectLLMResponse(
	ctx context.Context,
	provider domain.Provider,
	route domain.ModelRoute,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	maxTokens := route.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultExtractMaxTokens
	}
	req := llm.ChatRequest{
		Model:       route.ModelName,
		System:      systemPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userPrompt}},
		Temperature: route.Temperature,
		MaxTokens:   maxTokens,
	}

	var buf strings.Builder
	_, err := e.llm.Stream(ctx, provider, req, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func filterOpenForeshadowings(all []domain.Foreshadowing, currentSeq int) []domain.Foreshadowing {
	out := make([]domain.Foreshadowing, 0, len(all))
	for _, fs := range all {
		if fs.Status != domain.ForeshadowingPlanted && fs.Status != domain.ForeshadowingHinted {
			continue
		}
		if fs.PlantedChapterSeq > 0 && fs.PlantedChapterSeq >= currentSeq {
			continue
		}
		out = append(out, fs)
	}
	return out
}

func buildExtractionPrompts(
	book domain.Book,
	chap domain.Chapter,
	snapshots []domain.EntitySnapshot,
	openForeshadowings []domain.Foreshadowing,
) (string, string) {
	preset := genres.GetOrDefault(book.Genre)

	var sys strings.Builder
	sys.WriteString("你是男频长篇网文记忆账本抽取引擎。请严格根据给定章节正文与前章世界状态快照，提取本章实际发生的因果转折、新实体、实体属性增量变更与伏笔进展。\n")
	sys.WriteString("严格要求：\n")
	sys.WriteString("1. 必须只输出一个合法 JSON 对象，禁止输出任何解释文字或 Markdown 围栏。\n")
	sys.WriteString("2. 绝不脑补正文未明确写出的变更；若某项无变更则输出空数组 []。\n")
	sys.WriteString("3. 高危标红条件（severity 设为 \"high_risk\"）：角色死亡/失踪/夺舍/废除修为；实力境界或身份阶层跨大阶跃升；核心道具/功法/股权易主或损毁；重要角色阵营关系翻转；都市题材现金流单章变动超过 30%。其余常规变化（如普通可恢复轻伤、小境界微调）设为 \"normal\"。\n")
	sys.WriteString("4. 输出 JSON Schema：\n")
	sys.WriteString(`{
  "causal_summary": "150~250字单章因果摘要，只记关键因果转折、冲突结果与资源/伤势变化",
  "new_entities": [
    {"kind": "character|faction|location|item|system|rule", "name": "实体名", "aliases": [], "is_invariant": false, "summary": "简介", "attributes": {}, "reason": "出场原因"}
  ],
  "state_patches": [
    {"entity_id": 0, "entity_name": "实体名", "attr_key": "属性键", "prev_value": "变更前值", "new_value": "变更后值", "severity": "normal|high_risk", "reason": "变更依据"}
  ],
  "foreshadowing_updates": [
    {"action": "plant|hint|resolve|abandon", "foreshadowing_id": 0, "title": "伏笔标题", "description": "线索与状态说明", "target_resolve_seq": 0, "involved_entity_ids": []}
  ]
}`)

	var user strings.Builder
	fmt.Fprintf(&user, "【书籍信息】书名：《%s》｜题材：%s（%s）\n", book.Title, preset.Name, book.Genre)
	if len(preset.EntityFields) > 0 {
		fieldHints := make([]string, 0, len(preset.EntityFields))
		for _, f := range preset.EntityFields {
			fieldHints = append(fieldHints, fmt.Sprintf("%s(%s)", f.Key, f.Label))
		}
		fmt.Fprintf(&user, "题材推荐属性键：%s\n", strings.Join(fieldHints, "、"))
	}

	if len(snapshots) > 0 {
		user.WriteString("\n【本章开始前的实体状态快照】\n")
		for _, snap := range snapshots {
			fmt.Fprintf(&user, "- [ID:%d][%s] %s", snap.Entity.ID, snap.Entity.Kind.Label(), snap.Entity.Name)
			if len(snap.Entity.Aliases) > 0 {
				fmt.Fprintf(&user, "（别名：%s）", strings.Join(snap.Entity.Aliases, "/"))
			}
			if len(snap.EffectiveAttributes) > 0 {
				keys := make([]string, 0, len(snap.EffectiveAttributes))
				for k := range snap.EffectiveAttributes {
					keys = append(keys, k)
				}
				slices.Sort(keys)
				pairs := make([]string, 0, len(keys))
				for _, k := range keys {
					if v := strings.TrimSpace(snap.EffectiveAttributes[k]); v != "" {
						pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
					}
				}
				if len(pairs) > 0 {
					fmt.Fprintf(&user, "｜属性：%s", strings.Join(pairs, "；"))
				}
			}
			user.WriteString("\n")
		}
	}

	if len(openForeshadowings) > 0 {
		user.WriteString("\n【当前未回收伏笔清单】\n")
		for _, fs := range openForeshadowings {
			fmt.Fprintf(&user, "- [ID:%d][%s] %s（埋设第%d章，计划回收第%d章）：%s\n",
				fs.ID, fs.Status.Label(), fs.Title, fs.PlantedChapterSeq, fs.TargetResolveSeq, fs.Description)
		}
	}

	fmt.Fprintf(&user, "\n【待抽取章节正文】第%d章《%s》\n%s\n", chap.GlobalSeq, chap.Title, chap.Content)
	return sys.String(), user.String()
}

func buildRepairPrompts(brokenRaw string, parseErr error) (string, string) {
	runes := []rune(strings.TrimSpace(brokenRaw))
	preview := string(runes)
	if len(runes) > maxRepairPreviewRunes {
		preview = string(runes[:maxRepairPreviewRunes]) + "..."
	}

	sys := "你是 JSON 格式修复器。请将输入内容修复为一个严格合法的 JSON 对象并直接输出，绝不包含任何解释文字或 Markdown 代码围栏。"
	user := fmt.Sprintf(
		"你上一轮输出的记忆变更单 JSON 解析失败，报错为：%v。\n请严格按如下顶层字段结构修复并只返回合法 JSON 对象（必须包含 causal_summary、new_entities、state_patches、foreshadowing_updates）：\n\n原始内容：\n%s",
		parseErr,
		preview,
	)
	return sys, user
}
