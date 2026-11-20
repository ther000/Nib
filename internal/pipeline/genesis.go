package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nib/internal/domain"
	"nib/internal/genres"
	"nib/internal/llm"
)

const defaultGenesisMaxTokens = 6144

// GenesisStore 定义「AI 一句话开书」一次性落库所需的仓储接口（由 *store.Store 自动满足）。
type GenesisStore interface {
	CreateBook(ctx context.Context, in domain.CreateBookInput) (domain.Book, error)
	UpsertVolume(ctx context.Context, in domain.UpsertVolumeInput) (domain.Volume, error)
	UpsertEntity(ctx context.Context, in domain.UpsertEntityInput) (domain.Entity, error)
	CreateChapter(ctx context.Context, in domain.CreateChapterInput) (domain.Chapter, error)
	UpdateChapterMeta(ctx context.Context, in domain.UpdateChapterMetaInput) (domain.Chapter, error)
	ReplaceBeats(ctx context.Context, chapterID int64, beats []domain.Beat) ([]domain.Beat, error)
}

// GenerateBookGenesis 根据一句话灵感流式生成完整开书企划（书名、简介、金手指、初始设定、第一卷卷纲与初始章纲）。
func (r *Runner) GenerateBookGenesis(
	ctx context.Context,
	req domain.BookGenesisRequest,
	emit func(domain.GenesisChunk) error,
) (domain.BookGenesisPlan, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.BookGenesisPlan{}, errors.New("pipeline dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.BookGenesisPlan{}, err
	}

	genreID := req.Genre
	if genreID == "" {
		genreID = domain.GenreXuanhuan
	}
	chapterCount := req.ChapterCount
	if chapterCount <= 0 {
		chapterCount = domain.DefaultGenesisChapterCount
	}
	targetWords := req.TargetChapterWords
	if targetWords <= 0 {
		targetWords = domain.DefaultTargetChapterWords
	}

	send := func(chunk domain.GenesisChunk) error {
		if emit == nil {
			return nil
		}
		return emit(chunk)
	}

	// 优先使用 outline 路由；若用户仅配置了 draft 路由则自动回退，降低开书门槛。
	route, provider, err := r.resolveStageRouteAndProvider(ctx, domain.StageOutline)
	if err != nil {
		route, provider, err = r.resolveStageRouteAndProvider(ctx, domain.StageDraft)
		if err != nil {
			return domain.BookGenesisPlan{}, err
		}
	}

	if err := send(domain.GenesisChunk{Kind: domain.GenChunkStarted}); err != nil {
		return domain.BookGenesisPlan{}, domain.ErrGenerationCanceled
	}

	sysPrompt, userPrompt := buildBookGenesisPrompts(req.Idea, genreID, chapterCount, targetWords)
	maxTokens := route.MaxTokens
	if maxTokens < defaultGenesisMaxTokens {
		maxTokens = defaultGenesisMaxTokens
	}

	chatReq := llm.ChatRequest{
		Model:       route.ModelName,
		System:      sysPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userPrompt}},
		Temperature: route.Temperature,
		MaxTokens:   maxTokens,
	}

	var buf strings.Builder
	var finalUsage domain.TokenUsage
	streamUsage, streamErr := r.llm.Stream(ctx, provider, chatReq, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
			if err := send(domain.GenesisChunk{Kind: domain.GenChunkDelta, Delta: ev.Delta}); err != nil {
				return err
			}
		}
		if ev.Usage != nil {
			finalUsage = *ev.Usage
		}
		return nil
	})
	if hasUsage(streamUsage) {
		finalUsage = streamUsage
	}
	if streamErr != nil {
		_ = send(domain.GenesisChunk{Kind: domain.GenChunkError, Error: streamErr.Error()})
		return domain.BookGenesisPlan{}, streamErr
	}

	plan, parseErr := parseBookGenesisPayload(buf.String(), genreID, chapterCount, targetWords)
	if parseErr != nil {
		if ctx.Err() != nil {
			return domain.BookGenesisPlan{}, domain.ErrGenerationCanceled
		}
		repSys, repUser := buildStyleJSONRepairPrompts(
			`开书企划 JSON（需包含 title, premise, golden_finger, entities, first_volume, initial_outlines）`,
			buf.String(),
			parseErr,
		)
		repResp, repErr := r.collectStageLLMResponse(ctx, 0, 0, domain.StageOutline, provider, route, defaultGenesisMaxTokens, repSys, repUser)
		if repErr != nil {
			_ = send(domain.GenesisChunk{Kind: domain.GenChunkError, Error: repErr.Error()})
			return domain.BookGenesisPlan{}, fmt.Errorf("开书企划 JSON 纠错请求失败: %w", repErr)
		}
		plan, parseErr = parseBookGenesisPayload(repResp, genreID, chapterCount, targetWords)
		if parseErr != nil {
			_ = send(domain.GenesisChunk{Kind: domain.GenChunkError, Error: parseErr.Error()})
			return domain.BookGenesisPlan{}, fmt.Errorf("开书企划 JSON 解析失败（重试后仍无效）: %w", parseErr)
		}
	}

	planCopy := plan
	_ = send(domain.GenesisChunk{
		Kind:  domain.GenChunkCompleted,
		Plan:  &planCopy,
		Usage: usagePtrOrNil(finalUsage),
	})
	return plan, nil
}

// ApplyBookGenesis 将作者确认后的开书企划一次性写入书库：创建书籍、第一卷、初始设定与初始章纲节拍。
func (r *Runner) ApplyBookGenesis(ctx context.Context, in domain.ApplyGenesisInput) (domain.ApplyGenesisResult, error) {
	if r == nil || r.store == nil {
		return domain.ApplyGenesisResult{}, errors.New("pipeline store not initialized")
	}
	gs, ok := r.store.(GenesisStore)
	if !ok {
		return domain.ApplyGenesisResult{}, errors.New("store does not implement GenesisStore")
	}
	if err := in.Validate(); err != nil {
		return domain.ApplyGenesisResult{}, err
	}

	plan := in.Plan
	targetWords := plan.TargetChapterWords
	if targetWords <= 0 {
		targetWords = domain.DefaultTargetChapterWords
	}

	book, err := gs.CreateBook(ctx, domain.CreateBookInput{
		Title:              strings.TrimSpace(plan.Title),
		Genre:              plan.Genre,
		Premise:            strings.TrimSpace(plan.Premise),
		GoldenFinger:       strings.TrimSpace(plan.GoldenFinger),
		TargetChapterWords: targetWords,
		ExportDir:          strings.TrimSpace(in.ExportDir),
	})
	if err != nil {
		return domain.ApplyGenesisResult{}, fmt.Errorf("create book: %w", err)
	}

	volTitle := strings.TrimSpace(plan.FirstVolume.Title)
	if volTitle == "" {
		volTitle = "第一卷 初露锋芒"
	}
	vol, err := gs.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID:     book.ID,
		Seq:        1,
		Title:      volTitle,
		Goal:       strings.TrimSpace(plan.FirstVolume.Goal),
		ClimaxPlan: strings.TrimSpace(plan.FirstVolume.ClimaxPlan),
	})
	if err != nil {
		return domain.ApplyGenesisResult{}, fmt.Errorf("create first volume: %w", err)
	}

	savedEntities := make([]domain.Entity, 0, len(plan.Entities))
	for _, ent := range plan.Entities {
		name := strings.TrimSpace(ent.Name)
		if name == "" {
			continue
		}
		kind := ent.Kind
		if !kind.Valid() {
			kind = domain.EntityKindCharacter
		}
		aliases := ent.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		attrs := ent.Attributes
		if attrs == nil {
			attrs = map[string]string{}
		}
		saved, eErr := gs.UpsertEntity(ctx, domain.UpsertEntityInput{
			BookID:      book.ID,
			Kind:        kind,
			Name:        name,
			Aliases:     aliases,
			IsInvariant: ent.IsInvariant || kind == domain.EntityKindRule,
			Summary:     strings.TrimSpace(ent.Summary),
			Attributes:  attrs,
		})
		if eErr != nil {
			return domain.ApplyGenesisResult{}, fmt.Errorf("create entity %q: %w", name, eErr)
		}
		savedEntities = append(savedEntities, saved)
	}

	savedChapters := make([]domain.Chapter, 0, len(plan.InitialOutlines))
	for idx, out := range plan.InitialOutlines {
		title := strings.TrimSpace(out.Title)
		if title == "" {
			title = fmt.Sprintf("第%d章", idx+1)
		}
		emotion := out.EmotionTag
		if !emotion.Valid() {
			emotion = domain.EmotionSetup
		}
		hookType := out.HookType
		if !hookType.Valid() {
			hookType = domain.HookMystery
		}

		created, cErr := gs.CreateChapter(ctx, domain.CreateChapterInput{
			BookID:         book.ID,
			VolumeID:       vol.ID,
			Title:          title,
			EmotionTag:     emotion,
			HookType:       hookType,
			HookNote:       strings.TrimSpace(out.HookNote),
			OutlineSummary: strings.TrimSpace(out.OutlineSummary),
		})
		if cErr != nil {
			return domain.ApplyGenesisResult{}, fmt.Errorf("create chapter #%d: %w", idx+1, cErr)
		}
		chap, uErr := gs.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
			ID:             created.ID,
			Title:          created.Title,
			Status:         domain.ChapterStatusOutlined,
			EmotionTag:     emotion,
			HookType:       hookType,
			HookNote:       strings.TrimSpace(out.HookNote),
			OutlineSummary: strings.TrimSpace(out.OutlineSummary),
		})
		if uErr != nil {
			return domain.ApplyGenesisResult{}, fmt.Errorf("mark chapter #%d outlined: %w", idx+1, uErr)
		}

		if len(out.Beats) > 0 {
			beats := make([]domain.Beat, 0, len(out.Beats))
			for bIdx, b := range out.Beats {
				if strings.TrimSpace(b.Summary) == "" {
					continue
				}
				st := b.Stage
				if !st.Valid() {
					st = domain.BeatStageSetup
				}
				tw := b.TargetWords
				if tw <= 0 {
					tw = domain.DefaultBeatTargetWords
				}
				matchedIDs := matchMentionedEntityIDs(b.Summary+" "+b.ConflictNote+" "+out.OutlineSummary, savedEntities)
				beats = append(beats, domain.Beat{
					ChapterID:    chap.ID,
					Seq:          bIdx + 1,
					Stage:        st,
					Summary:      strings.TrimSpace(b.Summary),
					ConflictNote: strings.TrimSpace(b.ConflictNote),
					EntityIDs:    matchedIDs,
					TargetWords:  tw,
				})
			}
			if len(beats) > 0 {
				if _, bErr := gs.ReplaceBeats(ctx, chap.ID, beats); bErr != nil {
					return domain.ApplyGenesisResult{}, fmt.Errorf("save beats for chapter %d: %w", chap.ID, bErr)
				}
			}
		}
		savedChapters = append(savedChapters, chap)
	}

	// 记录一次轻量日志（如有 batchStore 且后续需要可扩展）。
	_ = time.Now()

	return domain.ApplyGenesisResult{
		Book:     book,
		Volume:   vol,
		Entities: savedEntities,
		Chapters: savedChapters,
	}, nil
}

// matchMentionedEntityIDs 根据节拍文本自动关联刚生成的实体，减少作者手动勾选。
func matchMentionedEntityIDs(text string, entities []domain.Entity) []int64 {
	if strings.TrimSpace(text) == "" || len(entities) == 0 {
		return []int64{}
	}
	ids := make([]int64, 0, 4)
	for _, ent := range entities {
		if ent.Kind == domain.EntityKindRule {
			continue
		}
		matched := ent.Name != "" && strings.Contains(text, ent.Name)
		if !matched {
			for _, alias := range ent.Aliases {
				if alias != "" && strings.Contains(text, alias) {
					matched = true
					break
				}
			}
		}
		if matched {
			ids = append(ids, ent.ID)
		}
	}
	return ids
}

func buildBookGenesisPrompts(idea string, genreID domain.GenreID, chapterCount int, targetWords int) (string, string) {
	preset := genres.GetOrDefault(genreID)
	sys := `你是顶级中文网文策划总监，擅长根据作者的一句话灵感快速产出可直接开写的完整开书企划。
要求：
1. 书名简短有记忆点，简介直击核心卖点与开局钩子，金手指规则明确（包含能力、触发条件与代价/限制）。
2. 初始设定包含 4~6 个核心档案：至少包含主角、核心反派/压迫源、关键配角或势力、以及 1~2 条世界铁律（kind="rule", is_invariant=true）。
3. 第一卷规划明确卷目标与卷末大高潮；初始章纲严格落实“开篇立人设与危机 -> 金手指初显 -> 首次小高潮反击”的黄金三章节奏。
4. 仅输出合法 JSON 对象，禁止输出任何 Markdown 解释文字。
JSON 结构：
{
  "title": "书名（不含书名号）",
  "premise": "100~200字核心卖点与开局处境简介",
  "golden_finger": "金手指能力、触发条件与核心限制",
  "entities": [
    {
      "kind": "character|faction|location|item|system|rule",
      "name": "名称",
      "aliases": ["别称"],
      "is_invariant": false,
      "summary": "40~100字核心设定",
      "attributes": {"境界": "...", "性格": "..."}
    }
  ],
  "first_volume": {
    "title": "第一卷卷名",
    "goal": "本卷主角要达成的阶段目标",
    "climax_plan": "本卷高潮大事件与反转"
  },
  "initial_outlines": [
    {
      "title": "章节名（不含第X章前缀）",
      "emotion_tag": "setup|suppress|turn|burst|harvest",
      "hook_type": "mystery|crisis|counterattack_eve|reward_pending|new_arc",
      "hook_note": "章末悬念落点",
      "outline_summary": "80~140字本章剧情推进",
      "beats": [
        {
          "stage": "setup|target|suppress|turn|burst|harvest|hook",
          "summary": "节拍具体情节（尽量直接写出人物名称）",
          "conflict_note": "冲突筹码或反转点",
          "target_words": 500
        }
      ]
    }
  ]
}`

	var user strings.Builder
	fmt.Fprintf(&user, "【作者灵感】%s\n", strings.TrimSpace(idea))
	fmt.Fprintf(&user, "【题材方向】%s（节奏周期：%s｜爽点侧重：%s）\n", preset.Name, preset.LoopCycleChapters, preset.PacingFocus)
	fmt.Fprintf(&user, "【开书规格】每章目标约 %d 字，请一并规划前 %d 章的章纲与每章 3~5 张节拍卡。\n", targetWords, chapterCount)
	return sys, user.String()
}

func parseBookGenesisPayload(raw string, genreID domain.GenreID, expectedChapters int, targetWords int) (domain.BookGenesisPlan, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return domain.BookGenesisPlan{}, errors.New("empty book genesis response")
	}
	jsonStr, err := extractStyleJSONObject(trimmed)
	if err != nil {
		return domain.BookGenesisPlan{}, err
	}

	type rawEntityDTO struct {
		Kind             string            `json:"kind"`
		Name             string            `json:"name"`
		Aliases          []string          `json:"aliases"`
		IsInvariant      bool              `json:"is_invariant"`
		IsInvariantCamel bool              `json:"isInvariant"`
		Summary          string            `json:"summary"`
		Attributes       map[string]string `json:"attributes"`
	}
	type rawVolumeDTO struct {
		Title           string `json:"title"`
		Goal            string `json:"goal"`
		ClimaxPlan      string `json:"climax_plan"`
		ClimaxPlanCamel string `json:"climaxPlan"`
	}
	type rawPlanDTO struct {
		Title                string          `json:"title"`
		Premise              string          `json:"premise"`
		GoldenFinger         string          `json:"golden_finger"`
		GoldenFingerCamel    string          `json:"goldenFinger"`
		Entities             []rawEntityDTO  `json:"entities"`
		FirstVolume          rawVolumeDTO    `json:"first_volume"`
		FirstVolumeCamel     rawVolumeDTO    `json:"firstVolume"`
		InitialOutlines      json.RawMessage `json:"initial_outlines"`
		InitialOutlinesCamel json.RawMessage `json:"initialOutlines"`
		Outlines             json.RawMessage `json:"outlines"`
	}

	var dto rawPlanDTO
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return domain.BookGenesisPlan{}, fmt.Errorf("unmarshal genesis plan: %w", err)
	}

	title := strings.TrimSpace(strings.Trim(dto.Title, "《》"))
	if title == "" {
		return domain.BookGenesisPlan{}, errors.New("genesis plan missing title")
	}
	goldenFinger := strings.TrimSpace(dto.GoldenFinger)
	if goldenFinger == "" {
		goldenFinger = strings.TrimSpace(dto.GoldenFingerCamel)
	}

	volDTO := dto.FirstVolume
	if strings.TrimSpace(volDTO.Title) == "" && strings.TrimSpace(dto.FirstVolumeCamel.Title) != "" {
		volDTO = dto.FirstVolumeCamel
	}
	climax := strings.TrimSpace(volDTO.ClimaxPlan)
	if climax == "" {
		climax = strings.TrimSpace(volDTO.ClimaxPlanCamel)
	}
	volTitle := strings.TrimSpace(volDTO.Title)
	if volTitle == "" {
		volTitle = "第一卷 初露锋芒"
	}

	entities := make([]domain.GenesisEntity, 0, len(dto.Entities))
	for _, e := range dto.Entities {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		kind := domain.EntityKind(strings.ToLower(strings.TrimSpace(e.Kind)))
		if !kind.Valid() {
			kind = domain.EntityKindCharacter
		}
		aliases := make([]string, 0, len(e.Aliases))
		for _, a := range e.Aliases {
			if t := strings.TrimSpace(a); t != "" {
				aliases = append(aliases, t)
			}
		}
		attrs := e.Attributes
		if attrs == nil {
			attrs = map[string]string{}
		}
		entities = append(entities, domain.GenesisEntity{
			Kind:        kind,
			Name:        name,
			Aliases:     aliases,
			IsInvariant: e.IsInvariant || e.IsInvariantCamel || kind == domain.EntityKindRule,
			Summary:     strings.TrimSpace(e.Summary),
			Attributes:  attrs,
		})
	}

	outlinesRaw := dto.InitialOutlines
	if len(outlinesRaw) == 0 {
		outlinesRaw = dto.InitialOutlinesCamel
	}
	if len(outlinesRaw) == 0 {
		outlinesRaw = dto.Outlines
	}
	var outlines []domain.GeneratedChapterOutline
	if len(outlinesRaw) > 0 {
		wrapped := fmt.Sprintf(`{"outlines": %s}`, string(outlinesRaw))
		parsed, oErr := parseBatchOutlinesPayload(wrapped, expectedChapters)
		if oErr == nil {
			outlines = parsed
		}
	}
	if outlines == nil {
		outlines = []domain.GeneratedChapterOutline{}
	}

	return domain.BookGenesisPlan{
		Title:              title,
		Genre:              genreID,
		Premise:            strings.TrimSpace(dto.Premise),
		GoldenFinger:       goldenFinger,
		TargetChapterWords: targetWords,
		Entities:           entities,
		FirstVolume: domain.GenesisVolume{
			Title:      volTitle,
			Goal:       strings.TrimSpace(volDTO.Goal),
			ClimaxPlan: climax,
		},
		InitialOutlines: outlines,
	}, nil
}
