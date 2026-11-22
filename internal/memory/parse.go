package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
	"nib/internal/llm"
)

type rawDeltaPayload struct {
	CausalSummary        string                `json:"causal_summary"`
	CausalSummaryCamel   string                `json:"causalSummary"`
	StoryTimeLabel       string                `json:"story_time_label"`
	StoryTimeLabelCamel  string                `json:"storyTimeLabel"`
	StoryDay             *int                  `json:"story_day"`
	StoryDayCamel        *int                  `json:"storyDay"`
	NewEntities          []rawNewEntity        `json:"new_entities"`
	NewEntitiesCamel     []rawNewEntity        `json:"newEntities"`
	StatePatches         []rawStatePatch       `json:"state_patches"`
	StatePatchesCamel    []rawStatePatch       `json:"statePatches"`
	ForeshadowUpdates    []rawForeshadowUpdate `json:"foreshadowing_updates"`
	ForeshadowUpdatesAlt []rawForeshadowUpdate `json:"foreshadows"`
	ForeshadowCamel      []rawForeshadowUpdate `json:"foreshadowingUpdates"`
	RelationChanges      []rawRelationChange   `json:"relation_changes"`
	RelationChangesCamel []rawRelationChange   `json:"relationChanges"`
	DeadlineChanges      []rawDeadlineChange   `json:"deadline_changes"`
	DeadlineChangesCamel []rawDeadlineChange   `json:"deadlineChanges"`
}

type rawRelationChange struct {
	Action            string `json:"action"`
	RelationID        int64  `json:"relation_id"`
	RelationIDCamel   int64  `json:"relationId"`
	SourceEntityID    int64  `json:"source_entity_id"`
	SourceIDCamel     int64  `json:"sourceEntityId"`
	SourceName        string `json:"source_name"`
	SourceNameCamel   string `json:"sourceName"`
	TargetEntityID    int64  `json:"target_entity_id"`
	TargetIDCamel     int64  `json:"targetEntityId"`
	TargetName        string `json:"target_name"`
	TargetNameCamel   string `json:"targetName"`
	RelationType      string `json:"relation_type"`
	RelationTypeCamel string `json:"relationType"`
	Description       string `json:"description"`
	Reason            string `json:"reason"`
}

type rawDeadlineChange struct {
	Action           string `json:"action"`
	DeadlineID       int64  `json:"deadline_id"`
	DeadlineIDCamel  int64  `json:"deadlineId"`
	Title            string `json:"title"`
	TargetDay        int    `json:"target_day"`
	TargetDayCamel   int    `json:"targetDay"`
	TargetLabel      string `json:"target_label"`
	TargetLabelCamel string `json:"targetLabel"`
	Note             string `json:"note"`
}

type rawNewEntity struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Aliases     []string          `json:"aliases"`
	IsInvariant bool              `json:"is_invariant"`
	IsInvCamel  bool              `json:"isInvariant"`
	Summary     string            `json:"summary"`
	Attributes  map[string]string `json:"attributes"`
	Reason      string            `json:"reason"`
}

type rawStatePatch struct {
	EntityID        int64  `json:"entity_id"`
	EntityIDCamel   int64  `json:"entityId"`
	EntityName      string `json:"entity_name"`
	EntityNameCamel string `json:"entityName"`
	AttrKey         string `json:"attr_key"`
	AttrKeyCamel    string `json:"attrKey"`
	PrevValue       string `json:"prev_value"`
	PrevValueCamel  string `json:"prevValue"`
	NewValue        string `json:"new_value"`
	NewValueCamel   string `json:"newValue"`
	Severity        string `json:"severity"`
	Reason          string `json:"reason"`
}

type rawForeshadowUpdate struct {
	Action              string   `json:"action"`
	ForeshadowingID     int64    `json:"foreshadowing_id"`
	ForeshadowingIDAlt  int64    `json:"foreshadowingId"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	TargetResolveSeq    int      `json:"target_resolve_seq"`
	TargetResolveCamel  int      `json:"targetResolveSeq"`
	InvolvedEntityIDs   []int64  `json:"involved_entity_ids"`
	InvolvedIDsCamel    []int64  `json:"involvedEntityIds"`
	InvolvedEntityNames []string `json:"involved_entity_names"`
}

func parseRawDeltaPayload(raw string) (rawDeltaPayload, error) {
	jsonStr, err := llm.ExtractJSONObject(raw)
	if err != nil {
		return rawDeltaPayload{}, err
	}
	var payload rawDeltaPayload
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		return rawDeltaPayload{}, fmt.Errorf("unmarshal delta json: %w", err)
	}

	// 校验至少具备合法结构：不能是完全无关的空对象且非预期结构。
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &top); err != nil {
		return rawDeltaPayload{}, fmt.Errorf("inspect delta keys: %w", err)
	}
	knownKeys := []string{
		"causal_summary", "causalSummary",
		"new_entities", "newEntities",
		"state_patches", "statePatches",
		"foreshadowing_updates", "foreshadows", "foreshadowingUpdates",
	}
	hasKnown := false
	for _, k := range knownKeys {
		if _, ok := top[k]; ok {
			hasKnown = true
			break
		}
	}
	if !hasKnown {
		return rawDeltaPayload{}, errors.New("JSON object missing required delta fields (causal_summary / state_patches / foreshadowing_updates)")
	}
	return payload, nil
}

func normalizeDelta(
	chapter domain.Chapter,
	genre domain.GenreID,
	raw rawDeltaPayload,
	entities []domain.Entity,
	snapshots []domain.EntitySnapshot,
	foreshadowings []domain.Foreshadowing,
	activeRelations []domain.EntityRelation,
	activeDeadlines []domain.StoryDeadline,
) domain.ChapterDelta {
	// 构造实体名/别名索引和前章有效属性快照索引，便于自动补齐 EntityID 与 PrevValue。
	entityIDByName := make(map[string]int64, len(entities)*2)
	entityNameByID := make(map[int64]string, len(entities))
	for _, e := range entities {
		entityNameByID[e.ID] = e.Name
		addNameLookup(entityIDByName, e.Name, e.ID)
		for _, alias := range e.Aliases {
			addNameLookup(entityIDByName, alias, e.ID)
		}
	}

	snapAttrsByEntityID := make(map[int64]map[string]string, len(snapshots))
	for _, snap := range snapshots {
		entityNameByID[snap.Entity.ID] = snap.Entity.Name
		addNameLookup(entityIDByName, snap.Entity.Name, snap.Entity.ID)
		for _, alias := range snap.Entity.Aliases {
			addNameLookup(entityIDByName, alias, snap.Entity.ID)
		}
		snapAttrsByEntityID[snap.Entity.ID] = snap.EffectiveAttributes
	}

	fsIDByTitle := make(map[string]int64, len(foreshadowings))
	for _, fs := range foreshadowings {
		key := strings.ToLower(strings.TrimSpace(fs.Title))
		if key != "" {
			fsIDByTitle[key] = fs.ID
		}
	}

	causal := strings.TrimSpace(raw.CausalSummary)
	if causal == "" {
		causal = strings.TrimSpace(raw.CausalSummaryCamel)
	}

	// 1. 新实体提案
	rawNewEnts := raw.NewEntities
	if len(rawNewEnts) == 0 {
		rawNewEnts = raw.NewEntitiesCamel
	}
	newEntities := make([]domain.DeltaNewEntity, 0, len(rawNewEnts))
	for _, item := range rawNewEnts {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		// 若该实体名已存在于全书设定中，不再作为重复新实体提案。
		if _, exists := entityIDByName[strings.ToLower(name)]; exists {
			continue
		}
		kind := domain.EntityKind(strings.ToLower(strings.TrimSpace(item.Kind)))
		if !kind.Valid() {
			kind = domain.EntityKindCharacter
		}
		attrs := item.Attributes
		if attrs == nil {
			attrs = map[string]string{}
		}
		aliases := cleanStringSlice(item.Aliases)
		newEntities = append(newEntities, domain.DeltaNewEntity{
			ID:          fmt.Sprintf("ent-%d", len(newEntities)+1),
			Kind:        kind,
			Name:        name,
			Aliases:     aliases,
			IsInvariant: item.IsInvariant || item.IsInvCamel,
			Summary:     strings.TrimSpace(item.Summary),
			Attributes:  attrs,
			Reason:      strings.TrimSpace(item.Reason),
		})
	}

	// 2. 实体状态变更提案 + 5 条确定性高危标红规则
	rawPatches := raw.StatePatches
	if len(rawPatches) == 0 {
		rawPatches = raw.StatePatchesCamel
	}
	patches := make([]domain.DeltaStatePatch, 0, len(rawPatches))
	highRiskCount := 0
	for _, item := range rawPatches {
		entityID := item.EntityID
		if entityID <= 0 {
			entityID = item.EntityIDCamel
		}
		entityName := strings.TrimSpace(item.EntityName)
		if entityName == "" {
			entityName = strings.TrimSpace(item.EntityNameCamel)
		}
		attrKey := strings.TrimSpace(item.AttrKey)
		if attrKey == "" {
			attrKey = strings.TrimSpace(item.AttrKeyCamel)
		}
		prevVal := strings.TrimSpace(item.PrevValue)
		if prevVal == "" {
			prevVal = strings.TrimSpace(item.PrevValueCamel)
		}
		newVal := strings.TrimSpace(item.NewValue)
		if newVal == "" {
			newVal = strings.TrimSpace(item.NewValueCamel)
		}
		if attrKey == "" || (entityID <= 0 && entityName == "") {
			continue
		}

		if entityID <= 0 && entityName != "" {
			if matchedID, ok := entityIDByName[strings.ToLower(entityName)]; ok {
				entityID = matchedID
			}
		}
		if entityName == "" && entityID > 0 {
			entityName = entityNameByID[entityID]
		}
		if prevVal == "" && entityID > 0 {
			if attrs, ok := snapAttrsByEntityID[entityID]; ok {
				prevVal = strings.TrimSpace(attrs[attrKey])
			}
		}

		patch := domain.DeltaStatePatch{
			ID:         fmt.Sprintf("patch-%d", len(patches)+1),
			EntityID:   entityID,
			EntityName: entityName,
			AttrKey:    attrKey,
			PrevValue:  prevVal,
			NewValue:   newVal,
			Severity:   domain.ChangeSeverityNormal,
			Reason:     strings.TrimSpace(item.Reason),
		}

		// 结合确定性高危分类器与模型标注：只要命中任一强规则或模型显式标红即升级为 high_risk。
		rawSev := domain.ChangeSeverity(strings.ToLower(strings.TrimSpace(item.Severity)))
		detSev := ClassifyHighRisk(genre, patch)
		if detSev == domain.ChangeSeverityHighRisk || rawSev == domain.ChangeSeverityHighRisk {
			patch.Severity = domain.ChangeSeverityHighRisk
			highRiskCount++
		}
		patches = append(patches, patch)
	}

	// 3. 伏笔变更提案
	rawFS := raw.ForeshadowUpdates
	if len(rawFS) == 0 {
		rawFS = raw.ForeshadowUpdatesAlt
	}
	if len(rawFS) == 0 {
		rawFS = raw.ForeshadowCamel
	}
	foreshadowUpdates := make([]domain.DeltaForeshadowUpdate, 0, len(rawFS))
	for _, item := range rawFS {
		action := domain.DeltaForeshadowAction(strings.ToLower(strings.TrimSpace(item.Action)))
		if !action.Valid() {
			continue
		}
		fsID := item.ForeshadowingID
		if fsID <= 0 {
			fsID = item.ForeshadowingIDAlt
		}
		title := strings.TrimSpace(item.Title)
		if title == "" && fsID <= 0 {
			continue
		}
		if fsID <= 0 && title != "" && action != domain.DeltaForeshadowPlant {
			if matchedID, ok := fsIDByTitle[strings.ToLower(title)]; ok {
				fsID = matchedID
			}
		}

		targetSeq := item.TargetResolveSeq
		if targetSeq <= 0 {
			targetSeq = item.TargetResolveCamel
		}

		involvedIDs := make([]int64, 0, len(item.InvolvedEntityIDs)+len(item.InvolvedIDsCamel)+len(item.InvolvedEntityNames))
		seenID := map[int64]bool{}
		addInvolved := func(id int64) {
			if id > 0 && !seenID[id] {
				seenID[id] = true
				involvedIDs = append(involvedIDs, id)
			}
		}
		for _, id := range item.InvolvedEntityIDs {
			addInvolved(id)
		}
		for _, id := range item.InvolvedIDsCamel {
			addInvolved(id)
		}
		for _, name := range item.InvolvedEntityNames {
			if id, ok := entityIDByName[strings.ToLower(strings.TrimSpace(name))]; ok {
				addInvolved(id)
			}
		}

		foreshadowUpdates = append(foreshadowUpdates, domain.DeltaForeshadowUpdate{
			ID:                fmt.Sprintf("fs-%d", len(foreshadowUpdates)+1),
			Action:            action,
			ForeshadowingID:   fsID,
			Title:             title,
			Description:       strings.TrimSpace(item.Description),
			TargetResolveSeq:  targetSeq,
			InvolvedEntityIDs: involvedIDs,
		})
	}

	// 4. 关系演变提案
	rawRels := raw.RelationChanges
	if len(rawRels) == 0 {
		rawRels = raw.RelationChangesCamel
	}
	relChanges := make([]domain.DeltaRelationChange, 0, len(rawRels))
	for _, item := range rawRels {
		action := strings.ToLower(strings.TrimSpace(item.Action))
		if action != "end" {
			action = "upsert"
		}
		relID := item.RelationID
		if relID <= 0 {
			relID = item.RelationIDCamel
		}
		srcID := item.SourceEntityID
		if srcID <= 0 {
			srcID = item.SourceIDCamel
		}
		srcName := strings.TrimSpace(item.SourceName)
		if srcName == "" {
			srcName = strings.TrimSpace(item.SourceNameCamel)
		}
		if srcID <= 0 && srcName != "" {
			srcID = entityIDByName[strings.ToLower(srcName)]
		}
		dstID := item.TargetEntityID
		if dstID <= 0 {
			dstID = item.TargetIDCamel
		}
		dstName := strings.TrimSpace(item.TargetName)
		if dstName == "" {
			dstName = strings.TrimSpace(item.TargetNameCamel)
		}
		if dstID <= 0 && dstName != "" {
			dstID = entityIDByName[strings.ToLower(dstName)]
		}
		relType := strings.TrimSpace(item.RelationType)
		if relType == "" {
			relType = strings.TrimSpace(item.RelationTypeCamel)
		}
		if relID <= 0 && (srcID <= 0 && srcName == "" || dstID <= 0 && dstName == "") {
			continue
		}
		if action == "upsert" && relType == "" {
			continue
		}
		if action == "end" && relID <= 0 && srcID > 0 && dstID > 0 {
			for _, ar := range activeRelations {
				if ar.SourceEntityID == srcID && ar.TargetEntityID == dstID && (relType == "" || ar.RelationType == relType) {
					relID = ar.ID
					break
				}
			}
		}
		relChanges = append(relChanges, domain.DeltaRelationChange{
			ID:             fmt.Sprintf("rel-%d", len(relChanges)+1),
			Action:         action,
			RelationID:     relID,
			SourceEntityID: srcID,
			SourceName:     srcName,
			TargetEntityID: dstID,
			TargetName:     dstName,
			RelationType:   relType,
			Description:    strings.TrimSpace(item.Description),
			Reason:         strings.TrimSpace(item.Reason),
		})
	}

	// 5. 故事内期限倒计时提案
	dlIDByTitle := make(map[string]int64, len(activeDeadlines))
	for _, dl := range activeDeadlines {
		if k := strings.ToLower(strings.TrimSpace(dl.Title)); k != "" {
			dlIDByTitle[k] = dl.ID
		}
	}
	rawDLs := raw.DeadlineChanges
	if len(rawDLs) == 0 {
		rawDLs = raw.DeadlineChangesCamel
	}
	dlChanges := make([]domain.DeltaDeadlineChange, 0, len(rawDLs))
	for _, item := range rawDLs {
		action := strings.ToLower(strings.TrimSpace(item.Action))
		switch action {
		case "create", "update", "reach", "expire", "cancel":
		default:
			action = "create"
		}
		dlID := item.DeadlineID
		if dlID <= 0 {
			dlID = item.DeadlineIDCamel
		}
		title := strings.TrimSpace(item.Title)
		if dlID <= 0 && title == "" {
			continue
		}
		if dlID <= 0 && title != "" && action != "create" {
			dlID = dlIDByTitle[strings.ToLower(title)]
		}
		targetDay := item.TargetDay
		if targetDay == 0 {
			targetDay = item.TargetDayCamel
		}
		targetLabel := strings.TrimSpace(item.TargetLabel)
		if targetLabel == "" {
			targetLabel = strings.TrimSpace(item.TargetLabelCamel)
		}
		dlChanges = append(dlChanges, domain.DeltaDeadlineChange{
			ID:          fmt.Sprintf("dl-%d", len(dlChanges)+1),
			Action:      action,
			DeadlineID:  dlID,
			Title:       title,
			TargetDay:   targetDay,
			TargetLabel: targetLabel,
			Note:        strings.TrimSpace(item.Note),
		})
	}

	storyTimeLabel := strings.TrimSpace(raw.StoryTimeLabel)
	if storyTimeLabel == "" {
		storyTimeLabel = strings.TrimSpace(raw.StoryTimeLabelCamel)
	}
	storyDay := raw.StoryDay
	if storyDay == nil {
		storyDay = raw.StoryDayCamel
	}

	return domain.ChapterDelta{
		ChapterID:       chapter.ID,
		CausalSummary:   causal,
		StoryTimeLabel:  storyTimeLabel,
		StoryDay:        storyDay,
		NewEntities:     newEntities,
		StatePatches:    patches,
		Foreshadows:     foreshadowUpdates,
		RelationChanges: relChanges,
		DeadlineChanges: dlChanges,
		HighRiskFlags:   highRiskCount,
		ReviewStatus:    domain.DeltaReviewPending,
	}
}

func addNameLookup(m map[string]int64, name string, id int64) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" || id <= 0 {
		return
	}
	if _, exists := m[key]; !exists {
		m[key] = id
	}
}

func cleanStringSlice(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}
