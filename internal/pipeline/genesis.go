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

const (
	defaultGenesisMaxTokens = 6144
	defaultConceptMaxTokens = 2048
)

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
	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StageOutline, domain.StageDraft)
	if err != nil {
		return domain.BookGenesisPlan{}, err
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
	usedProv, _, streamUsage, streamErr := r.streamWithFallback(ctx, route, provider, chatReq, func(ev llm.StreamEvent) error {
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

	plan, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		0,
		0,
		domain.StageOutline,
		usedProv,
		route,
		defaultGenesisMaxTokens,
		`开书企划 JSON（需包含 title, premise, golden_finger, entities, first_volume, initial_outlines）`,
		buf.String(),
		func(raw string) (domain.BookGenesisPlan, error) {
			return parseBookGenesisPayload(raw, genreID, chapterCount, targetWords)
		},
	)
	if parseErr != nil {
		if !errors.Is(parseErr, domain.ErrGenerationCanceled) {
			_ = send(domain.GenesisChunk{Kind: domain.GenChunkError, Error: parseErr.Error()})
		}
		return domain.BookGenesisPlan{}, parseErr
	}
	plan.POVMode = strings.TrimSpace(req.POVMode)

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
		POVMode:            strings.TrimSpace(plan.POVMode),
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

	// 若作者选择不生成初始章纲，仍自动预建一章空白首章，确保建书后可直接进入写作台开写。
	if len(savedChapters) == 0 {
		firstChap, cErr := gs.CreateChapter(ctx, domain.CreateChapterInput{
			BookID:         book.ID,
			VolumeID:       vol.ID,
			Title:          "初章",
			EmotionTag:     domain.EmotionSetup,
			HookType:       domain.HookMystery,
			HookNote:       "",
			OutlineSummary: "",
		})
		if cErr != nil {
			return domain.ApplyGenesisResult{}, fmt.Errorf("create fallback first chapter: %w", cErr)
		}
		savedChapters = append(savedChapters, firstChap)
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
	jsonStr, err := llm.ExtractJSONObject(trimmed)
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

// GenerateBookConcept 根据作者灵感流式生成（或按反馈微调）核心立意：候选书名、简介、金手指与核心冲突。
func (r *Runner) GenerateBookConcept(
	ctx context.Context,
	req domain.BookConceptRequest,
	emit func(domain.ConceptChunk) error,
) (domain.BookConcept, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.BookConcept{}, errors.New("pipeline dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.BookConcept{}, err
	}

	genreID := req.Genre
	if genreID == "" && req.CurrentConcept != nil && req.CurrentConcept.Genre.Valid() {
		genreID = req.CurrentConcept.Genre
	}
	if genreID == "" {
		genreID = domain.GenreXuanhuan
	}
	povMode := strings.TrimSpace(req.POVMode)
	if povMode == "" && req.CurrentConcept != nil {
		povMode = strings.TrimSpace(req.CurrentConcept.POVMode)
	}

	send := func(chunk domain.ConceptChunk) error {
		if emit == nil {
			return nil
		}
		return emit(chunk)
	}

	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StageOutline, domain.StageDraft)
	if err != nil {
		return domain.BookConcept{}, err
	}

	if err := send(domain.ConceptChunk{Kind: domain.GenChunkStarted}); err != nil {
		return domain.BookConcept{}, domain.ErrGenerationCanceled
	}

	sysPrompt, userPrompt := buildBookConceptPrompts(req, genreID, povMode)
	maxTokens := route.MaxTokens
	if maxTokens < defaultConceptMaxTokens {
		maxTokens = defaultConceptMaxTokens
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
	usedProv, _, streamUsage, streamErr := r.streamWithFallback(ctx, route, provider, chatReq, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
			if err := send(domain.ConceptChunk{Kind: domain.GenChunkDelta, Delta: ev.Delta}); err != nil {
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
		_ = send(domain.ConceptChunk{Kind: domain.GenChunkError, Error: streamErr.Error()})
		return domain.BookConcept{}, streamErr
	}

	concept, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		0,
		0,
		domain.StageOutline,
		usedProv,
		route,
		defaultConceptMaxTokens,
		`开书立意 JSON（需包含 title, title_candidates, premise, golden_finger, core_conflict）`,
		buf.String(),
		func(raw string) (domain.BookConcept, error) {
			return parseBookConceptPayload(raw, genreID, povMode)
		},
	)
	if parseErr != nil {
		if !errors.Is(parseErr, domain.ErrGenerationCanceled) {
			_ = send(domain.ConceptChunk{Kind: domain.GenChunkError, Error: parseErr.Error()})
		}
		return domain.BookConcept{}, parseErr
	}

	copyVal := concept
	_ = send(domain.ConceptChunk{
		Kind:    domain.GenChunkCompleted,
		Concept: &copyVal,
		Usage:   usagePtrOrNil(finalUsage),
	})
	return concept, nil
}

// GenerateBookBlueprint 基于作者拍板的核心立意，流式生成第一卷、初始设定与初始章纲节拍。
// 支持 section="all"（全量）、"entities"（仅设定）、"outlines"（仅第一卷与章纲）。
func (r *Runner) GenerateBookBlueprint(
	ctx context.Context,
	req domain.BookBlueprintRequest,
	emit func(domain.BlueprintChunk) error,
) (domain.BookBlueprint, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.BookBlueprint{}, errors.New("pipeline dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.BookBlueprint{}, err
	}

	genreID := req.Concept.Genre
	if genreID == "" {
		genreID = domain.GenreXuanhuan
	}
	section := strings.ToLower(strings.TrimSpace(req.Section))
	if section == "" {
		section = "all"
	}
	chapterCount := req.ChapterCount
	if section == "outlines" && chapterCount <= 0 {
		chapterCount = domain.DefaultGenesisChapterCount
	}
	targetWords := req.TargetChapterWords
	if targetWords <= 0 {
		targetWords = domain.DefaultTargetChapterWords
	}

	send := func(chunk domain.BlueprintChunk) error {
		if emit == nil {
			return nil
		}
		return emit(chunk)
	}

	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StageOutline, domain.StageDraft)
	if err != nil {
		return domain.BookBlueprint{}, err
	}

	if err := send(domain.BlueprintChunk{Kind: domain.GenChunkStarted}); err != nil {
		return domain.BookBlueprint{}, domain.ErrGenerationCanceled
	}

	sysPrompt, userPrompt := buildBookBlueprintPrompts(req.Concept, genreID, section, chapterCount, targetWords, req.ExistingEntities)
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
	usedProv, _, streamUsage, streamErr := r.streamWithFallback(ctx, route, provider, chatReq, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
			if err := send(domain.BlueprintChunk{Kind: domain.GenChunkDelta, Delta: ev.Delta}); err != nil {
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
		_ = send(domain.BlueprintChunk{Kind: domain.GenChunkError, Error: streamErr.Error()})
		return domain.BookBlueprint{}, streamErr
	}

	schemaHint := `开书蓝图 JSON（包含 first_volume, entities, initial_outlines）`
	if section == "entities" {
		schemaHint = `初始设定 JSON（包含 entities 数组）`
	} else if section == "outlines" {
		schemaHint = `卷纲与章纲 JSON（包含 first_volume 与 initial_outlines）`
	}

	bp, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		0,
		0,
		domain.StageOutline,
		usedProv,
		route,
		defaultGenesisMaxTokens,
		schemaHint,
		buf.String(),
		func(raw string) (domain.BookBlueprint, error) {
			return parseBookBlueprintPayload(raw, section, chapterCount)
		},
	)
	if parseErr != nil {
		if !errors.Is(parseErr, domain.ErrGenerationCanceled) {
			_ = send(domain.BlueprintChunk{Kind: domain.GenChunkError, Error: parseErr.Error()})
		}
		return domain.BookBlueprint{}, parseErr
	}

	copyVal := bp
	_ = send(domain.BlueprintChunk{
		Kind:      domain.GenChunkCompleted,
		Blueprint: &copyVal,
		Usage:     usagePtrOrNil(finalUsage),
	})
	return bp, nil
}

func povLabel(povMode string) string {
	switch strings.TrimSpace(povMode) {
	case "first_person":
		return "第一人称"
	case "third_omniscient":
		return "第三人称全知"
	case "multi_pov":
		return "多视角群像"
	default:
		return "第三人称限知"
	}
}

func buildBookConceptPrompts(req domain.BookConceptRequest, genreID domain.GenreID, povMode string) (string, string) {
	preset := genres.GetOrDefault(genreID)
	sys := `你是顶级中文网文策划总监，负责在开书第一阶段帮助作者快速定准「书名、核心立意、金手指与主线冲突」。
要求：
1. 提供 3 个不同侧重、简短有记忆点的候选书名（不含书名号），并把最推荐的放在 title 中。
2. premise（100~180字）直击主角开局处境、核心卖点与爽点钩子，拒绝套话空泛。
3. golden_finger 必须写清三要素：具体能力、触发条件、代价或限制。
4. core_conflict 用 1~2 句话点明主角初期要面对的生死/利益困局与破局方向。
5. 仅输出合法 JSON 对象，禁止输出任何 Markdown 解释文字。
JSON 结构：
{
  "title": "主推荐书名",
  "title_candidates": ["主推荐书名", "备选书名二", "备选书名三"],
  "premise": "100~180字核心卖点与开局处境简介",
  "golden_finger": "金手指能力、触发条件与核心限制",
  "core_conflict": "1~2句初期核心冲突与反转钩子"
}`

	var user strings.Builder
	if strings.TrimSpace(req.Idea) != "" {
		fmt.Fprintf(&user, "【作者灵感】%s\n", strings.TrimSpace(req.Idea))
	}
	fmt.Fprintf(&user, "【题材方向】%s（节奏周期：%s｜爽点侧重：%s）\n", preset.Name, preset.LoopCycleChapters, preset.PacingFocus)
	fmt.Fprintf(&user, "【叙事视角】%s\n", povLabel(povMode))
	if req.CurrentConcept != nil {
		fmt.Fprintf(&user, "\n【当前已有立意草案】\n书名：%s\n简介：%s\n金手指：%s\n核心冲突：%s\n",
			strings.TrimSpace(req.CurrentConcept.Title),
			strings.TrimSpace(req.CurrentConcept.Premise),
			strings.TrimSpace(req.CurrentConcept.GoldenFinger),
			strings.TrimSpace(req.CurrentConcept.CoreConflict),
		)
	}
	if strings.TrimSpace(req.Feedback) != "" {
		fmt.Fprintf(&user, "\n【作者微调要求】%s\n请保留合理部分，重点按以上意见重写并优化立意。\n", strings.TrimSpace(req.Feedback))
	}
	return sys, user.String()
}

func parseBookConceptPayload(raw string, genreID domain.GenreID, povMode string) (domain.BookConcept, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return domain.BookConcept{}, errors.New("empty book concept response")
	}
	jsonStr, err := llm.ExtractJSONObject(trimmed)
	if err != nil {
		return domain.BookConcept{}, err
	}

	type rawConceptDTO struct {
		Title                string   `json:"title"`
		TitleCandidates      []string `json:"title_candidates"`
		TitleCandidatesCamel []string `json:"titleCandidates"`
		Premise              string   `json:"premise"`
		GoldenFinger         string   `json:"golden_finger"`
		GoldenFingerCamel    string   `json:"goldenFinger"`
		CoreConflict         string   `json:"core_conflict"`
		CoreConflictCamel    string   `json:"coreConflict"`
	}

	var dto rawConceptDTO
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return domain.BookConcept{}, fmt.Errorf("unmarshal concept: %w", err)
	}

	cleanTitle := func(s string) string {
		return strings.TrimSpace(strings.Trim(s, "《》"))
	}

	title := cleanTitle(dto.Title)
	rawCandidates := dto.TitleCandidates
	if len(rawCandidates) == 0 {
		rawCandidates = dto.TitleCandidatesCamel
	}
	seen := make(map[string]struct{}, 4)
	candidates := make([]string, 0, 4)
	if title != "" {
		seen[title] = struct{}{}
		candidates = append(candidates, title)
	}
	for _, c := range rawCandidates {
		t := cleanTitle(c)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		candidates = append(candidates, t)
	}
	if title == "" && len(candidates) > 0 {
		title = candidates[0]
	}
	if title == "" {
		return domain.BookConcept{}, errors.New("book concept missing title")
	}

	goldenFinger := strings.TrimSpace(dto.GoldenFinger)
	if goldenFinger == "" {
		goldenFinger = strings.TrimSpace(dto.GoldenFingerCamel)
	}
	coreConflict := strings.TrimSpace(dto.CoreConflict)
	if coreConflict == "" {
		coreConflict = strings.TrimSpace(dto.CoreConflictCamel)
	}

	return domain.BookConcept{
		Title:           title,
		TitleCandidates: candidates,
		Genre:           genreID,
		POVMode:         povMode,
		Premise:         strings.TrimSpace(dto.Premise),
		GoldenFinger:    goldenFinger,
		CoreConflict:    coreConflict,
	}, nil
}

func buildBookBlueprintPrompts(
	concept domain.BookConcept,
	genreID domain.GenreID,
	section string,
	chapterCount int,
	targetWords int,
	existingEntities []domain.GenesisEntity,
) (string, string) {
	preset := genres.GetOrDefault(genreID)

	var fieldHints []string
	for _, f := range preset.EntityFields {
		fieldHints = append(fieldHints, f.Label)
	}
	attrHint := strings.Join(fieldHints, "、")
	if attrHint == "" {
		attrHint = "实力层级、身份、性格"
	}

	var sys string
	switch section {
	case "entities":
		sys = fmt.Sprintf(`你是顶级中文网文设定总监，请严格依据作者已拍板的开书立意，产出 4~6 个核心初始档案。
要求：
1. 必须包含：主角、核心反派/压迫源、1~2 个关键配角或势力、以及 1~2 条世界铁律（kind="rule", is_invariant=true）。
2. 角色 attributes 优先使用该题材高频字段：%s。
3. 仅输出合法 JSON 对象，禁止输出任何 Markdown 解释文字。
JSON 结构：
{
  "entities": [
    {
      "kind": "character|faction|location|item|system|rule",
      "name": "名称",
      "aliases": ["别称"],
      "is_invariant": false,
      "summary": "40~100字核心设定",
      "attributes": {"字段名": "数值或状态"}
    }
  ]
}`, attrHint)
	case "outlines":
		sys = `你是顶级中文网文大纲总编，请严格依据作者已拍板的开书立意与既定角色设定，产出第一卷规划与黄金开篇章纲。
要求：
1. 第一卷规划明确卷目标与卷末大高潮。
2. 章纲严格落实“开篇立人设与危机 -> 金手指初显 -> 首次小高潮反击”的开篇节奏，每章 3~5 张节拍卡，节拍中直接写出既定人物名称。
3. 仅输出合法 JSON 对象，禁止输出任何 Markdown 解释文字。
JSON 结构：
{
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
          "summary": "节拍具体情节（直接写出人物名称）",
          "conflict_note": "冲突筹码或反转点",
          "target_words": 500
        }
      ]
    }
  ]
}`
	default:
		outlinesRule := fmt.Sprintf("3. 严格落实“开篇立人设与危机 -> 金手指初显 -> 首次小高潮反击”的节奏，生成前 %d 章章纲与每章 3~5 张节拍卡，节拍中尽量直接写出角色姓名。", chapterCount)
		if chapterCount == 0 {
			outlinesRule = "3. 作者暂不需要初始章纲，请将 initial_outlines 设为空数组 []，重点把第一卷规划和核心设定做扎实。"
		}
		sys = fmt.Sprintf(`你是顶级中文网文策划总监，请严格依据作者已拍板的开书立意，产出第一卷规划、初始核心设定与开篇章纲。
要求：
1. 初始设定包含 4~6 个核心档案：主角、核心反派/压迫源、关键配角或势力、以及 1~2 条世界铁律（kind="rule", is_invariant=true）；角色 attributes 优先参考字段：%s。
2. 第一卷规划明确卷目标与卷末大高潮。
%s
4. 仅输出合法 JSON 对象，禁止输出任何 Markdown 解释文字。
JSON 结构：
{
  "first_volume": {
    "title": "第一卷卷名",
    "goal": "本卷主角要达成的阶段目标",
    "climax_plan": "本卷高潮大事件与反转"
  },
  "entities": [
    {
      "kind": "character|faction|location|item|system|rule",
      "name": "名称",
      "aliases": ["别称"],
      "is_invariant": false,
      "summary": "40~100字核心设定",
      "attributes": {"字段名": "数值或状态"}
    }
  ],
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
          "summary": "节拍具体情节（直接写出人物名称）",
          "conflict_note": "冲突筹码或反转点",
          "target_words": 500
        }
      ]
    }
  ]
}`, attrHint, outlinesRule)
	}

	var user strings.Builder
	fmt.Fprintf(&user, "【已拍板立意】\n书名：%s\n题材：%s（%s｜%s）\n视角：%s\n",
		strings.TrimSpace(concept.Title), preset.Name, preset.LoopCycleChapters, preset.PacingFocus, povLabel(concept.POVMode))
	if strings.TrimSpace(concept.Premise) != "" {
		fmt.Fprintf(&user, "简介：%s\n", strings.TrimSpace(concept.Premise))
	}
	if strings.TrimSpace(concept.GoldenFinger) != "" {
		fmt.Fprintf(&user, "金手指：%s\n", strings.TrimSpace(concept.GoldenFinger))
	}
	if strings.TrimSpace(concept.CoreConflict) != "" {
		fmt.Fprintf(&user, "核心冲突：%s\n", strings.TrimSpace(concept.CoreConflict))
	}
	if section == "outlines" && len(existingEntities) > 0 {
		user.WriteString("\n【已确定的核心设定（章纲与节拍必须严格沿用）】\n")
		for _, ent := range existingEntities {
			if strings.TrimSpace(ent.Name) == "" {
				continue
			}
			fmt.Fprintf(&user, "- [%s] %s：%s\n", ent.Kind, strings.TrimSpace(ent.Name), strings.TrimSpace(ent.Summary))
		}
	}
	if section != "entities" {
		if chapterCount > 0 {
			fmt.Fprintf(&user, "\n【开篇规格】每章目标约 %d 字，请规划前 %d 章章纲与节拍卡。\n", targetWords, chapterCount)
		} else {
			fmt.Fprintf(&user, "\n【开篇规格】每章目标约 %d 字，本次无需生成初始章纲（initial_outlines 返回 []）。\n", targetWords)
		}
	}
	return sys, user.String()
}

func parseBookBlueprintPayload(raw string, section string, expectedChapters int) (domain.BookBlueprint, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return domain.BookBlueprint{}, errors.New("empty book blueprint response")
	}
	jsonStr, err := llm.ExtractJSONObject(trimmed)
	if err != nil {
		return domain.BookBlueprint{}, err
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
	type rawBlueprintDTO struct {
		Entities             []rawEntityDTO  `json:"entities"`
		FirstVolume          rawVolumeDTO    `json:"first_volume"`
		FirstVolumeCamel     rawVolumeDTO    `json:"firstVolume"`
		InitialOutlines      json.RawMessage `json:"initial_outlines"`
		InitialOutlinesCamel json.RawMessage `json:"initialOutlines"`
		Outlines             json.RawMessage `json:"outlines"`
	}

	var dto rawBlueprintDTO
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return domain.BookBlueprint{}, fmt.Errorf("unmarshal blueprint: %w", err)
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
	if volTitle == "" && section != "entities" {
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
	if section != "entities" && expectedChapters > 0 && len(outlinesRaw) > 0 {
		wrapped := fmt.Sprintf(`{"outlines": %s}`, string(outlinesRaw))
		parsed, oErr := parseBatchOutlinesPayload(wrapped, expectedChapters)
		if oErr == nil {
			outlines = parsed
		}
	}
	if outlines == nil {
		outlines = []domain.GeneratedChapterOutline{}
	}

	if section == "entities" && len(entities) == 0 {
		return domain.BookBlueprint{}, errors.New("blueprint entities response is empty")
	}

	return domain.BookBlueprint{
		FirstVolume: domain.GenesisVolume{
			Title:      volTitle,
			Goal:       strings.TrimSpace(volDTO.Goal),
			ClimaxPlan: climax,
		},
		Entities:        entities,
		InitialOutlines: outlines,
	}, nil
}
