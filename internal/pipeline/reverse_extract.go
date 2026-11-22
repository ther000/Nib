package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"nib/internal/domain"
	"nib/internal/llm"
)

const (
	defaultReverseExtractMaxTokens  = 8192
	defaultReverseExtractChapters   = 20
	reverseExtractChapterCharBudget = 2600
	reverseExtractTotalCharBudget   = 42000
)

// ReverseExtractStore 定义旧稿反推设定所需的仓储接口（由 *store.Store 自动满足）。
type ReverseExtractStore interface {
	GetBook(ctx context.Context, id int64) (domain.Book, error)
	ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error)
	ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error)
	UpsertEntity(ctx context.Context, in domain.UpsertEntityInput) (domain.Entity, error)
	UpsertEntityRelation(ctx context.Context, in domain.UpsertEntityRelationInput) (domain.EntityRelation, error)
	UpsertForeshadowing(ctx context.Context, in domain.UpsertForeshadowingInput) (domain.Foreshadowing, error)
	UpsertArcSummary(ctx context.Context, in domain.UpsertArcSummaryInput) (domain.ArcSummary, error)
}

// ReverseExtractBook 从旧稿抽样章节中流式反推角色/势力/道具、关系、伏笔与弧线摘要。
// 只返回审阅清单，不直接落库；作者确认后调用 ApplyReverseExtract。
func (r *Runner) ReverseExtractBook(
	ctx context.Context,
	req domain.ReverseExtractRequest,
	emit func(domain.ReverseExtractChunk) error,
) (domain.ReverseExtractPreview, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.ReverseExtractPreview{}, errors.New("pipeline dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.ReverseExtractPreview{}, err
	}
	rs, ok := r.store.(ReverseExtractStore)
	if !ok {
		return domain.ReverseExtractPreview{}, errors.New("store does not implement ReverseExtractStore")
	}

	send := func(chunk domain.ReverseExtractChunk) error {
		if emit == nil {
			return nil
		}
		return emit(chunk)
	}

	book, err := rs.GetBook(ctx, req.BookID)
	if err != nil {
		return domain.ReverseExtractPreview{}, err
	}
	chapters, err := rs.ListBookChapters(ctx, req.BookID)
	if err != nil {
		return domain.ReverseExtractPreview{}, err
	}
	sort.Slice(chapters, func(i, j int) bool { return chapters[i].GlobalSeq < chapters[j].GlobalSeq })
	sampled := sampleReverseExtractChapters(chapters, req)
	if len(sampled) == 0 {
		return domain.ReverseExtractPreview{}, &domain.ValidationError{Field: "chapters", Message: "所选范围内没有可采样的章节"}
	}

	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StageExtract, domain.StageOutline, domain.StageDraft)
	if err != nil {
		return domain.ReverseExtractPreview{}, err
	}

	if err := send(domain.ReverseExtractChunk{Kind: "started"}); err != nil {
		return domain.ReverseExtractPreview{}, domain.ErrGenerationCanceled
	}

	sysPrompt, userPrompt := buildReverseExtractPrompts(book, sampled)
	maxTokens := route.MaxTokens
	if maxTokens < defaultReverseExtractMaxTokens {
		maxTokens = defaultReverseExtractMaxTokens
	}
	chatReq := llm.ChatRequest{
		Model:       route.ModelName,
		System:      sysPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userPrompt}},
		Temperature: route.Temperature,
		MaxTokens:   maxTokens,
	}

	start := time.Now()
	var buf strings.Builder
	var finalUsage domain.TokenUsage
	usedProv, usedModel, streamUsage, streamErr := r.streamWithFallback(ctx, route, provider, chatReq, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
			if err := send(domain.ReverseExtractChunk{Kind: "delta", Delta: ev.Delta}); err != nil {
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
		_ = send(domain.ReverseExtractChunk{Kind: "error", Error: streamErr.Error()})
		return domain.ReverseExtractPreview{}, streamErr
	}
	if domain.StageExtract.ValidUsageStage() {
		r.recordTokenUsage(ctx, req.BookID, 0, domain.StageExtract, usedProv.Name, usedModel, finalUsage, time.Since(start).Milliseconds())
	}

	preview, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		req.BookID,
		0,
		domain.StageExtract,
		usedProv,
		route,
		defaultReverseExtractMaxTokens,
		`旧稿反推 JSON（需包含 entities, relations, foreshadowings, arc_summaries）`,
		buf.String(),
		parseReverseExtractPayload,
	)
	if parseErr != nil {
		if !errors.Is(parseErr, domain.ErrGenerationCanceled) {
			_ = send(domain.ReverseExtractChunk{Kind: "error", Error: parseErr.Error()})
		}
		return domain.ReverseExtractPreview{}, parseErr
	}

	if err := markExistingEntities(ctx, rs, req.BookID, &preview); err != nil {
		return domain.ReverseExtractPreview{}, err
	}
	if len(preview.Entities) == 0 && len(preview.Relations) == 0 && len(preview.Foreshadowings) == 0 && len(preview.ArcSummaries) == 0 {
		preview.Warnings = append(preview.Warnings, "模型没有从样章中识别出可用设定，请扩大采样范围或检查样章内容。")
	}

	previewCopy := preview
	_ = send(domain.ReverseExtractChunk{
		Kind:    "completed",
		Preview: &previewCopy,
		Usage:   usagePtrOrNil(finalUsage),
	})
	return preview, nil
}

// ApplyReverseExtract 将作者勾选后的反推结果写入设定账本（重名实体更新，不重复创建）。
func (r *Runner) ApplyReverseExtract(ctx context.Context, in domain.ApplyReverseExtractInput) (domain.ApplyReverseExtractResult, error) {
	if r == nil || r.store == nil {
		return domain.ApplyReverseExtractResult{}, errors.New("pipeline store not initialized")
	}
	if err := in.Validate(); err != nil {
		return domain.ApplyReverseExtractResult{}, err
	}
	rs, ok := r.store.(ReverseExtractStore)
	if !ok {
		return domain.ApplyReverseExtractResult{}, errors.New("store does not implement ReverseExtractStore")
	}
	existing, err := rs.ListEntities(ctx, in.BookID, domain.EntityFilter{})
	if err != nil {
		return domain.ApplyReverseExtractResult{}, err
	}
	byName := make(map[string]domain.Entity, len(existing))
	for _, entity := range existing {
		byName[normalizeEntityKey(entity.Name)] = entity
		for _, alias := range entity.Aliases {
			key := normalizeEntityKey(alias)
			if key != "" {
				if _, exists := byName[key]; !exists {
					byName[key] = entity
				}
			}
		}
	}

	result := domain.ApplyReverseExtractResult{}
	entityIDs := make(map[string]int64, len(in.Preview.Entities))
	for _, item := range in.Preview.Entities {
		key := normalizeEntityKey(item.Name)
		if key == "" {
			continue
		}
		input := domain.UpsertEntityInput{
			BookID:      in.BookID,
			Kind:        item.Kind,
			Name:        strings.TrimSpace(item.Name),
			Aliases:     item.Aliases,
			IsInvariant: item.IsInvariant,
			Summary:     item.Summary,
			Attributes:  item.Attributes,
		}
		if existingEntity, ok := byName[key]; ok {
			input.ID = existingEntity.ID
			saved, err := rs.UpsertEntity(ctx, input)
			if err != nil {
				return domain.ApplyReverseExtractResult{}, err
			}
			entityIDs[key] = saved.ID
			result.EntitiesUpdated++
			continue
		}
		saved, err := rs.UpsertEntity(ctx, input)
		if err != nil {
			return domain.ApplyReverseExtractResult{}, err
		}
		entityIDs[key] = saved.ID
		byName[key] = saved
		result.EntitiesCreated++
	}

	for _, rel := range in.Preview.Relations {
		sourceID, ok := entityIDs[normalizeEntityKey(rel.SourceName)]
		if !ok {
			if entity, exists := byName[normalizeEntityKey(rel.SourceName)]; exists {
				sourceID = entity.ID
			}
		}
		targetID, ok := entityIDs[normalizeEntityKey(rel.TargetName)]
		if !ok {
			if entity, exists := byName[normalizeEntityKey(rel.TargetName)]; exists {
				targetID = entity.ID
			}
		}
		if sourceID <= 0 || targetID <= 0 || sourceID == targetID || strings.TrimSpace(rel.RelationType) == "" {
			continue
		}
		if _, err := rs.UpsertEntityRelation(ctx, domain.UpsertEntityRelationInput{
			BookID:         in.BookID,
			SourceEntityID: sourceID,
			TargetEntityID: targetID,
			RelationType:   strings.TrimSpace(rel.RelationType),
			Description:    rel.Description,
		}); err != nil {
			return domain.ApplyReverseExtractResult{}, err
		}
		result.RelationsCreated++
	}

	for _, item := range in.Preview.Foreshadowings {
		if strings.TrimSpace(item.Title) == "" {
			continue
		}
		if _, err := rs.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
			BookID:            in.BookID,
			Title:             strings.TrimSpace(item.Title),
			Description:       item.Description,
			PlantedChapterSeq: item.PlantedChapterSeq,
			TargetResolveSeq:  item.TargetResolveSeq,
			Status:            domain.ForeshadowingPlanted,
		}); err != nil {
			return domain.ApplyReverseExtractResult{}, err
		}
		result.Foreshadowings++
	}

	for _, item := range in.Preview.ArcSummaries {
		if strings.TrimSpace(item.Summary) == "" || item.StartSeq <= 0 || item.EndSeq < item.StartSeq {
			continue
		}
		level := item.Level
		if !level.Valid() {
			level = domain.ArcSummaryLevelArc10
		}
		if _, err := rs.UpsertArcSummary(ctx, domain.UpsertArcSummaryInput{
			BookID:   in.BookID,
			Level:    level,
			StartSeq: item.StartSeq,
			EndSeq:   item.EndSeq,
			Summary:  item.Summary,
		}); err != nil {
			return domain.ApplyReverseExtractResult{}, err
		}
		result.ArcSummaries++
	}
	return result, nil
}

func sampleReverseExtractChapters(chapters []domain.Chapter, req domain.ReverseExtractRequest) []domain.Chapter {
	limit := req.MaxChapters
	if limit <= 0 {
		limit = defaultReverseExtractChapters
	}
	start := req.StartSeq
	end := req.EndSeq
	out := make([]domain.Chapter, 0, limit)
	for _, chapter := range chapters {
		if start > 0 && chapter.GlobalSeq < start {
			continue
		}
		if end > 0 && chapter.GlobalSeq > end {
			continue
		}
		out = append(out, chapter)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildReverseExtractPrompts(book domain.Book, chapters []domain.Chapter) (string, string) {
	var b strings.Builder
	fmt.Fprintf(&b, "书名：《%s》\n题材：%s\n简介：%s\n金手指：%s\n\n", book.Title, book.Genre, book.Premise, book.GoldenFinger)
	total := 0
	for _, chapter := range chapters {
		content := strings.TrimSpace(chapter.Content)
		runes := []rune(content)
		if len(runes) > reverseExtractChapterCharBudget {
			runes = runes[:reverseExtractChapterCharBudget]
			content = string(runes) + "…"
		}
		block := fmt.Sprintf("第%d章 %s\n%s\n\n", chapter.GlobalSeq, chapter.Title, content)
		total += len([]rune(block))
		if total > reverseExtractTotalCharBudget {
			break
		}
		b.WriteString(block)
	}

	sys := `你是一名资深网文责编，负责从旧稿样章中反推可复用的设定账本。请只输出严格合法的 JSON 对象，禁止 Markdown 代码围栏或解释文字。
JSON 结构：
{
  "entities": [{"name":"","kind":"character|faction|location|item|system|rule","aliases":[],"summary":"","is_invariant":false,"attributes":{}}],
  "relations": [{"source_name":"","target_name":"","relation_type":"师徒/仇敌/同盟等","description":""}],
  "foreshadowings": [{"title":"","description":"","planted_chapter_seq":1,"target_resolve_seq":0}],
  "arc_summaries": [{"level":"arc_10|volume","start_seq":1,"end_seq":10,"summary":""}]
}
要求：只抽取样章中明确出现的角色、势力、地点、关键道具、体系与硬性规则；伏笔只记录尚未回收的；不确定的不要编。`
	user := "以下是旧稿样章，请反推设定、关系、伏笔与阶段摘要：\n\n" + b.String()
	return sys, user
}

type rawReverseExtractPayload struct {
	Entities []struct {
		Name             string            `json:"name"`
		Kind             string            `json:"kind"`
		Aliases          []string          `json:"aliases"`
		Summary          string            `json:"summary"`
		IsInvariant      bool              `json:"is_invariant"`
		IsInvariantCamel bool              `json:"isInvariant"`
		Attributes       map[string]string `json:"attributes"`
	} `json:"entities"`
	Relations []struct {
		SourceName        string `json:"source_name"`
		SourceNameCamel   string `json:"sourceName"`
		TargetName        string `json:"target_name"`
		TargetNameCamel   string `json:"targetName"`
		RelationType      string `json:"relation_type"`
		RelationTypeCamel string `json:"relationType"`
		Description       string `json:"description"`
	} `json:"relations"`
	Foreshadowings []struct {
		Title               string `json:"title"`
		Description         string `json:"description"`
		PlantedChapterSeq   int    `json:"planted_chapter_seq"`
		PlantedChapterCamel int    `json:"plantedChapterSeq"`
		TargetResolveSeq    int    `json:"target_resolve_seq"`
		TargetResolveCamel  int    `json:"targetResolveSeq"`
	} `json:"foreshadowings"`
	ArcSummaries []struct {
		Level      string `json:"level"`
		StartSeq   int    `json:"start_seq"`
		StartCamel int    `json:"startSeq"`
		EndSeq     int    `json:"end_seq"`
		EndCamel   int    `json:"endSeq"`
		Summary    string `json:"summary"`
	} `json:"arc_summaries"`
}

func parseReverseExtractPayload(raw string) (domain.ReverseExtractPreview, error) {
	jsonStr, err := llm.ExtractJSONObject(raw)
	if err != nil {
		return domain.ReverseExtractPreview{}, err
	}
	var dto rawReverseExtractPayload
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return domain.ReverseExtractPreview{}, err
	}
	preview := domain.ReverseExtractPreview{
		Entities:       make([]domain.ReverseExtractEntity, 0, len(dto.Entities)),
		Relations:      make([]domain.ReverseExtractRelation, 0, len(dto.Relations)),
		Foreshadowings: make([]domain.ReverseExtractForeshadowing, 0, len(dto.Foreshadowings)),
		ArcSummaries:   make([]domain.ReverseExtractArcSummary, 0, len(dto.ArcSummaries)),
	}
	seen := make(map[string]struct{}, len(dto.Entities))
	for _, item := range dto.Entities {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		key := normalizeEntityKey(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		kind := domain.EntityKind(strings.TrimSpace(item.Kind))
		if !kind.Valid() {
			kind = domain.EntityKindCharacter
		}
		preview.Entities = append(preview.Entities, domain.ReverseExtractEntity{
			Name:        name,
			Kind:        kind,
			Aliases:     item.Aliases,
			Summary:     item.Summary,
			IsInvariant: item.IsInvariant || item.IsInvariantCamel,
			Attributes:  item.Attributes,
		})
	}
	for _, item := range dto.Relations {
		source := firstNonEmpty(item.SourceName, item.SourceNameCamel)
		target := firstNonEmpty(item.TargetName, item.TargetNameCamel)
		relation := firstNonEmpty(item.RelationType, item.RelationTypeCamel)
		if source == "" || target == "" || relation == "" {
			continue
		}
		preview.Relations = append(preview.Relations, domain.ReverseExtractRelation{
			SourceName:   source,
			TargetName:   target,
			RelationType: relation,
			Description:  item.Description,
		})
	}
	for _, item := range dto.Foreshadowings {
		if strings.TrimSpace(item.Title) == "" {
			continue
		}
		preview.Foreshadowings = append(preview.Foreshadowings, domain.ReverseExtractForeshadowing{
			Title:             strings.TrimSpace(item.Title),
			Description:       item.Description,
			PlantedChapterSeq: firstNonZero(item.PlantedChapterSeq, item.PlantedChapterCamel),
			TargetResolveSeq:  firstNonZero(item.TargetResolveSeq, item.TargetResolveCamel),
		})
	}
	for _, item := range dto.ArcSummaries {
		level := domain.ArcSummaryLevel(strings.TrimSpace(item.Level))
		if !level.Valid() {
			level = domain.ArcSummaryLevelArc10
		}
		start := firstNonZero(item.StartSeq, item.StartCamel)
		end := firstNonZero(item.EndSeq, item.EndCamel)
		if strings.TrimSpace(item.Summary) == "" || start <= 0 || end < start {
			continue
		}
		preview.ArcSummaries = append(preview.ArcSummaries, domain.ReverseExtractArcSummary{
			Level: level, StartSeq: start, EndSeq: end, Summary: item.Summary,
		})
	}
	return preview, nil
}

func markExistingEntities(ctx context.Context, rs ReverseExtractStore, bookID int64, preview *domain.ReverseExtractPreview) error {
	existing, err := rs.ListEntities(ctx, bookID, domain.EntityFilter{})
	if err != nil {
		return err
	}
	keys := make(map[string]struct{}, len(existing)*2)
	for _, entity := range existing {
		keys[normalizeEntityKey(entity.Name)] = struct{}{}
		for _, alias := range entity.Aliases {
			keys[normalizeEntityKey(alias)] = struct{}{}
		}
	}
	duplicates := 0
	for i := range preview.Entities {
		if _, ok := keys[normalizeEntityKey(preview.Entities[i].Name)]; ok {
			preview.Entities[i].ExistingMatch = true
			duplicates++
		}
	}
	if duplicates > 0 {
		preview.Warnings = append(preview.Warnings, fmt.Sprintf("有 %d 个候选实体与现有设定重名，确认后将更新现有档案而不是重复创建。", duplicates))
	}
	return nil
}

func normalizeEntityKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
