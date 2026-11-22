package compiler

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"nib/internal/domain"
)

func buildP2Content(candidates []p2Candidate, budget int) (string, []int64, bool) {
	if len(candidates) == 0 {
		return "", []int64{}, false
	}

	const header = "【本章节拍出场与钉选实体状态快照】\n"
	headerTokens := EstimateTokens(header)
	if budget <= headerTokens {
		return "", []int64{}, true
	}

	// 第 1 档：全量详细档案
	fullLines := make([]string, len(candidates))
	allIDs := make([]int64, len(candidates))
	for i, cand := range candidates {
		fullLines[i] = formatCandidateDetailed(cand)
		allIDs[i] = cand.entity.ID
	}
	fullText := strings.TrimSpace(header + strings.Join(fullLines, "\n"))
	if EstimateTokens(fullText) <= budget {
		return fullText, allIDs, false
	}

	// 第 2 档：POV/钉选保留详细属性，其余实体压缩为单行摘要
	mixedLines := make([]string, len(candidates))
	for i, cand := range candidates {
		if cand.highPriority {
			mixedLines[i] = formatCandidateDetailed(cand)
		} else {
			mixedLines[i] = formatEntityCompact(cand.entity)
		}
	}
	mixedText := strings.TrimSpace(header + strings.Join(mixedLines, "\n"))
	if EstimateTokens(mixedText) <= budget {
		return mixedText, allIDs, true
	}

	// 第 3 档：预算仍吃紧，维护增量 Token 计数器按优先级逐条装配（O(N)）。
	keptLines := make([]string, 0, len(candidates))
	keptIDs := make([]int64, 0, len(candidates))
	usedTokens := headerTokens

	for _, cand := range candidates {
		if cand.highPriority {
			detailed := formatCandidateDetailed(cand)
			detailedCost := EstimateTokens(detailed) + 1
			if usedTokens+detailedCost <= budget {
				keptLines = append(keptLines, detailed)
				keptIDs = append(keptIDs, cand.entity.ID)
				usedTokens += detailedCost
				continue
			}
		}
		line := formatEntityCompact(cand.entity)
		lineCost := EstimateTokens(line) + 1
		if usedTokens+lineCost <= budget {
			keptLines = append(keptLines, line)
			keptIDs = append(keptIDs, cand.entity.ID)
			usedTokens += lineCost
			continue
		}
		if len(keptLines) == 0 {
			rem := budget - headerTokens
			if truncated := truncateHeadWithinTokens(line, rem); truncated != "" {
				keptLines = append(keptLines, truncated)
				keptIDs = append(keptIDs, cand.entity.ID)
			}
		}
		break
	}

	if len(keptLines) == 0 {
		return "", []int64{}, true
	}
	return strings.TrimSpace(header + strings.Join(keptLines, "\n")), keptIDs, true
}

func formatRelevantRelations(rels []domain.EntityRelation, includedEntityIDs []int64, remainingBudget int) string {
	if len(rels) == 0 || len(includedEntityIDs) == 0 || remainingBudget <= 30 {
		return ""
	}
	inc := make(map[int64]bool, len(includedEntityIDs))
	for _, id := range includedEntityIDs {
		inc[id] = true
	}

	const header = "### 出场角色/势力关系网\n"
	usedTokens := EstimateTokens(header)
	if usedTokens >= remainingBudget {
		return ""
	}

	kept := make([]string, 0, len(rels))
	for _, r := range rels {
		if !inc[r.SourceEntityID] && !inc[r.TargetEntityID] {
			continue
		}
		line := fmt.Sprintf("- %s —[%s]→ %s", r.SourceName, r.RelationType, r.TargetName)
		if d := strings.TrimSpace(r.Description); d != "" {
			line += "：" + d
		}
		cost := EstimateTokens(line) + 1
		if usedTokens+cost > remainingBudget {
			break
		}
		kept = append(kept, line)
		usedTokens += cost
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.TrimSpace(header + strings.Join(kept, "\n"))
}

type scoredForeshadow struct {
	fs          domain.Foreshadowing
	overdue     bool
	overlapCnt  int
	urgencyDiff int // targetResolveSeq - currentSeq (越小越紧迫；未设目标则为极大值)
}

func buildP3Content(
	all []domain.Foreshadowing,
	currentSeq int,
	includedEntityIDs []int64,
	budget int,
) (string, bool, int) {
	includedSet := make(map[int64]bool, len(includedEntityIDs))
	for _, id := range includedEntityIDs {
		includedSet[id] = true
	}

	const noTargetDiff = 1_000_000
	candidates := make([]scoredForeshadow, 0, len(all))
	overdueCount := 0

	for _, fs := range all {
		if fs.Status != domain.ForeshadowingPlanted && fs.Status != domain.ForeshadowingHinted {
			continue
		}
		// 防剧透：绝不注入未来章节才埋设的伏笔。
		if fs.PlantedChapterSeq > 0 && fs.PlantedChapterSeq >= currentSeq {
			continue
		}

		overlap := 0
		for _, eid := range fs.InvolvedEntityIDs {
			if includedSet[eid] {
				overlap++
			}
		}
		overdue := fs.TargetResolveSeq > 0 && currentSeq > fs.TargetResolveSeq
		if overdue {
			overdueCount++
		}
		diff := noTargetDiff
		if fs.TargetResolveSeq > 0 {
			diff = fs.TargetResolveSeq - currentSeq
		}

		candidates = append(candidates, scoredForeshadow{
			fs:          fs,
			overdue:     overdue,
			overlapCnt:  overlap,
			urgencyDiff: diff,
		})
	}

	if len(candidates) == 0 {
		return "", false, 0
	}

	// 优先级排序：1) 超期伏笔优先（超期越久越靠前）→ 2) 关联本章节拍出场实体 → 3) 临近 target_resolve_seq → 4) ID 稳定序。
	slices.SortFunc(candidates, func(a, b scoredForeshadow) int {
		if a.overdue != b.overdue {
			if a.overdue {
				return -1
			}
			return 1
		}
		if a.overdue && b.overdue && a.urgencyDiff != b.urgencyDiff {
			return a.urgencyDiff - b.urgencyDiff
		}
		if (a.overlapCnt > 0) != (b.overlapCnt > 0) {
			if a.overlapCnt > 0 {
				return -1
			}
			return 1
		}
		if a.urgencyDiff != b.urgencyDiff {
			return a.urgencyDiff - b.urgencyDiff
		}
		if a.overlapCnt != b.overlapCnt {
			return b.overlapCnt - a.overlapCnt
		}
		return int(a.fs.ID - b.fs.ID)
	})

	const header = "【未回收伏笔与临期/超期预警】\n"
	headerTokens := EstimateTokens(header)
	if budget <= headerTokens {
		return "", true, overdueCount
	}

	kept := make([]string, 0, len(candidates))
	usedTokens := headerTokens
	truncated := false
	for _, cand := range candidates {
		line := formatForeshadowLine(cand, currentSeq)
		cost := EstimateTokens(line) + 1
		if usedTokens+cost <= budget {
			kept = append(kept, line)
			usedTokens += cost
			continue
		}
		truncated = true
		if len(kept) == 0 {
			rem := budget - headerTokens
			if cut := truncateHeadWithinTokens(line, rem); cut != "" {
				kept = append(kept, cut)
			}
		}
		break
	}
	if len(kept) == 0 {
		return "", true, overdueCount
	}
	return strings.TrimSpace(header + strings.Join(kept, "\n")), truncated, overdueCount
}

func formatForeshadowLine(cand scoredForeshadow, currentSeq int) string {
	fs := cand.fs
	var tag string
	switch {
	case cand.overdue:
		tag = fmt.Sprintf("【超期 %d 章】", currentSeq-fs.TargetResolveSeq)
	case fs.TargetResolveSeq > 0 && fs.TargetResolveSeq-currentSeq <= 3:
		tag = fmt.Sprintf("【临期·目标第%d章】", fs.TargetResolveSeq)
	case fs.TargetResolveSeq > 0:
		tag = fmt.Sprintf("【目标第%d章回收】", fs.TargetResolveSeq)
	default:
		tag = "【长期悬念】"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "- %s[%s] %s（第%d章埋设", tag, fs.Status.Label(), fs.Title, fs.PlantedChapterSeq)
	if fs.LastHintSeq > 0 {
		fmt.Fprintf(&b, "，第%d章暗示", fs.LastHintSeq)
	}
	b.WriteString("）")
	if strings.TrimSpace(fs.Description) != "" {
		fmt.Fprintf(&b, "：%s", strings.TrimSpace(fs.Description))
	}
	return b.String()
}

type summaryLineEntry struct {
	text string
	cost int
}

func buildP5Content(
	recentChapters []domain.Chapter,
	arcSummaries []domain.ArcSummary,
	maxSeq int,
	budget int,
) (string, bool) {
	var volEntries []summaryLineEntry
	var arcEntries []summaryLineEntry

	filteredArcs := make([]domain.ArcSummary, 0, len(arcSummaries))
	for _, a := range arcSummaries {
		if strings.TrimSpace(a.Summary) == "" || maxSeq == 0 || (maxSeq > 0 && a.EndSeq > maxSeq) {
			continue
		}
		filteredArcs = append(filteredArcs, a)
	}
	slices.SortFunc(filteredArcs, func(a, b domain.ArcSummary) int {
		if a.StartSeq != b.StartSeq {
			return a.StartSeq - b.StartSeq
		}
		return a.EndSeq - b.EndSeq
	})

	for _, a := range filteredArcs {
		var text string
		switch a.Level {
		case domain.ArcSummaryLevelVolume:
			text = fmt.Sprintf("- [卷摘要 第%d~%d章] %s", a.StartSeq, a.EndSeq, strings.TrimSpace(a.Summary))
			volEntries = append(volEntries, summaryLineEntry{text: text, cost: EstimateTokens(text) + 1})
		case domain.ArcSummaryLevelArc10:
			text = fmt.Sprintf("- [弧线摘要 第%d~%d章] %s", a.StartSeq, a.EndSeq, strings.TrimSpace(a.Summary))
			arcEntries = append(arcEntries, summaryLineEntry{text: text, cost: EstimateTokens(text) + 1})
		}
	}

	chapEntries := make([]summaryLineEntry, 0, len(recentChapters))
	for _, ch := range recentChapters {
		text := fmt.Sprintf("- [第%d章《%s》] %s", ch.GlobalSeq, ch.Title, strings.TrimSpace(ch.CausalSummary))
		chapEntries = append(chapEntries, summaryLineEntry{text: text, cost: EstimateTokens(text) + 1})
	}

	if len(volEntries) == 0 && len(arcEntries) == 0 && len(chapEntries) == 0 {
		return "", false
	}

	const header = "【历史剧情摘要金字塔（卷摘要 / 10章弧线 / 近15章因果）】\n"
	headerTokens := EstimateTokens(header)

	renderEntries := func(vols, arcs, chaps []summaryLineEntry) string {
		total := len(vols) + len(arcs) + len(chaps)
		if total == 0 {
			return ""
		}
		lines := make([]string, 0, total)
		for _, e := range vols {
			lines = append(lines, e.text)
		}
		for _, e := range arcs {
			lines = append(lines, e.text)
		}
		for _, e := range chaps {
			lines = append(lines, e.text)
		}
		return strings.TrimSpace(header + strings.Join(lines, "\n"))
	}

	totalCost := headerTokens
	for _, e := range volEntries {
		totalCost += e.cost
	}
	for _, e := range arcEntries {
		totalCost += e.cost
	}
	for _, e := range chapEntries {
		totalCost += e.cost
	}

	if totalCost <= budget {
		return renderEntries(volEntries, arcEntries, chapEntries), false
	}
	full := renderEntries(volEntries, arcEntries, chapEntries)
	if EstimateTokens(full) <= budget {
		return full, false
	}
	if budget <= headerTokens {
		return "", true
	}

	// O(N) 增量裁剪：从最远的单章因果摘要 → 弧线摘要 → 卷摘要逐条剥离（仅在必要时校验精确 EstimateTokens）。
	curChaps := chapEntries
	curArcs := arcEntries
	curVols := volEntries

	for len(curChaps) > 0 && totalCost > budget {
		totalCost -= curChaps[0].cost
		curChaps = curChaps[1:]
	}
	for len(curArcs) > 0 && totalCost > budget {
		totalCost -= curArcs[0].cost
		curArcs = curArcs[1:]
	}
	for len(curVols) > 0 && totalCost > budget {
		totalCost -= curVols[0].cost
		curVols = curVols[1:]
	}

	if len(curChaps) > 0 || len(curArcs) > 0 || len(curVols) > 0 {
		candidate := renderEntries(curVols, curArcs, curChaps)
		if EstimateTokens(candidate) <= budget {
			return candidate, true
		}
	}

	// 若连单条最近摘要都超过预算，则截断最新一条单章、弧线或卷摘要作为保底。
	fallback := ""
	switch {
	case len(chapEntries) > 0:
		fallback = chapEntries[len(chapEntries)-1].text
	case len(arcEntries) > 0:
		fallback = arcEntries[len(arcEntries)-1].text
	case len(volEntries) > 0:
		fallback = volEntries[len(volEntries)-1].text
	}
	if cut := truncateHeadWithinTokens(fallback, budget-headerTokens); cut != "" {
		return strings.TrimSpace(header + cut), true
	}
	return "", true
}

func buildSearchKeywords(explicit []string, p2Candidates []p2Candidate, beats []domain.Beat) []string {
	const (
		maxExplicitKeywords = 5
		maxPrimaryNames     = 3
		maxAutoKeywords     = 6
		maxAliasesPerEntity = 2
	)
	out := make([]string, 0, maxAutoKeywords)
	seen := make(map[string]bool)
	add := func(raw string) bool {
		kw := strings.TrimSpace(raw)
		if kw == "" || len([]rune(kw)) < 2 {
			return false
		}
		lower := strings.ToLower(kw)
		if seen[lower] {
			return false
		}
		seen[lower] = true
		out = append(out, kw)
		return true
	}

	for _, kw := range explicit {
		add(kw)
	}
	if len(out) > 0 {
		if len(out) > maxExplicitKeywords {
			return out[:maxExplicitKeywords]
		}
		return out
	}

	// 1. 优先收集出场实体主名（非高优实体优先，再补 POV/钉选实体，最多 3 个主名），防止单个角色的别名挤占其他出场角色。
	for _, cand := range p2Candidates {
		if !cand.highPriority {
			add(cand.entity.Name)
		}
		if len(out) >= maxPrimaryNames {
			break
		}
	}
	if len(out) < maxPrimaryNames {
		for _, cand := range p2Candidates {
			add(cand.entity.Name)
			if len(out) >= maxPrimaryNames {
				break
			}
		}
	}

	// 2. 补充出场实体别名（每实体最多 2 个，总计最多 6 个），避免角色换称呼或合并后历史正文漏召回。
	addAliases := func(highPriority bool) {
		for _, cand := range p2Candidates {
			if cand.highPriority != highPriority {
				continue
			}
			addedForEntity := 0
			for _, alias := range cand.entity.Aliases {
				if len(out) >= maxAutoKeywords || addedForEntity >= maxAliasesPerEntity {
					break
				}
				if add(alias) {
					addedForEntity++
				}
			}
			if len(out) >= maxAutoKeywords {
				return
			}
		}
	}
	addAliases(false)
	if len(out) < maxAutoKeywords {
		addAliases(true)
	}

	if len(out) >= 2 {
		return out
	}
	for _, b := range beats {
		for _, tok := range strings.FieldsFunc(b.ConflictNote+" "+b.Summary, isKeywordDelimiter) {
			if r := []rune(tok); len(r) >= 2 && len(r) <= 8 {
				add(tok)
				if len(out) >= 2 {
					return out
				}
			}
		}
	}
	return out
}

func isKeywordDelimiter(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '，', '。', '、', '；', '：', '！', '？', '（', '）', '《', '》', '“', '”', ',', '.', ';', ':', '!', '?', '/', '|', '-':
		return true
	default:
		return false
	}
}

func runSpoilerFreeMemorySearch(
	ctx context.Context,
	mem MemoryDataSource,
	bookID int64,
	maxChapterSeq int,
	keywords []string,
) ([]domain.MemorySearchHit, error) {
	if maxChapterSeq <= 0 || len(keywords) == 0 {
		return nil, nil
	}

	type hitKey struct {
		source    string
		chapterID int64
		entityID  int64
	}
	dedup := make(map[hitKey]domain.MemorySearchHit)
	for _, kw := range keywords {
		hits, err := mem.SearchMemory(ctx, bookID, kw, maxChapterSeq)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			// P3 已装配未回收伏笔、P5 已装配摘要金字塔，P6 专注保留章节原文与实体设定出处，避免重复占位。
			if h.Source != domain.MemorySearchSourceChapter && h.Source != domain.MemorySearchSourceEntity {
				continue
			}
			if h.Source == domain.MemorySearchSourceChapter && h.ChapterSeq > maxChapterSeq {
				continue
			}
			if strings.TrimSpace(h.Snippet) == "" {
				continue
			}
			k := hitKey{source: h.Source, chapterID: h.ChapterID, entityID: h.EntityID}
			if existing, ok := dedup[k]; !ok || h.Score > existing.Score {
				dedup[k] = h
			}
		}
	}

	merged := make([]domain.MemorySearchHit, 0, len(dedup))
	for _, h := range dedup {
		merged = append(merged, h)
	}
	slices.SortFunc(merged, func(a, b domain.MemorySearchHit) int {
		if a.Source != b.Source {
			if a.Source == domain.MemorySearchSourceChapter {
				return -1
			}
			return 1
		}
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		if a.ChapterSeq != b.ChapterSeq {
			return b.ChapterSeq - a.ChapterSeq
		}
		return int(a.ChapterID - b.ChapterID)
	})
	if len(merged) > maxSearchHits {
		merged = merged[:maxSearchHits]
	}
	return merged, nil
}

func buildP6Content(hits []domain.MemorySearchHit, budget int) (string, bool) {
	if len(hits) == 0 {
		return "", false
	}

	const header = "【前文精确检索命中片段（历史出处核验）】\n"
	headerTokens := EstimateTokens(header)
	if budget <= headerTokens {
		return "", true
	}

	kept := make([]string, 0, len(hits))
	usedTokens := headerTokens
	truncated := false
	for _, h := range hits {
		var line string
		if h.Source == domain.MemorySearchSourceChapter {
			line = fmt.Sprintf("- [第%d章《%s》] %s", h.ChapterSeq, h.Title, strings.TrimSpace(h.Snippet))
		} else {
			line = fmt.Sprintf("- [设定《%s》] %s", h.Title, strings.TrimSpace(h.Snippet))
		}
		cost := EstimateTokens(line) + 1
		if usedTokens+cost <= budget {
			kept = append(kept, line)
			usedTokens += cost
			continue
		}
		truncated = true
		if len(kept) == 0 {
			rem := budget - headerTokens
			if cut := truncateHeadWithinTokens(line, rem); cut != "" {
				kept = append(kept, cut)
			}
		}
		break
	}
	if len(kept) == 0 {
		return "", true
	}
	return strings.TrimSpace(header + strings.Join(kept, "\n")), truncated
}

func formatCandidateDetailed(cand p2Candidate) string {
	base := formatEntityDetailed(cand.entity)
	if cand.pendingCount > 0 {
		base += fmt.Sprintf("\n  状态备注：包含 %d 条同批待审变更", cand.pendingCount)
	}
	return base
}

func formatEntityDetailed(e domain.Entity) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- [%s] %s", e.Kind.Label(), e.Name)
	if len(e.Aliases) > 0 {
		fmt.Fprintf(&b, "（别名：%s）", strings.Join(e.Aliases, "/"))
	}
	if strings.TrimSpace(e.Summary) != "" {
		fmt.Fprintf(&b, "：%s", strings.TrimSpace(e.Summary))
	}
	if len(e.Attributes) > 0 {
		keys := sortedMapKeys(e.Attributes)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			v := strings.TrimSpace(e.Attributes[k])
			if v == "" {
				continue
			}
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
		}
		if len(pairs) > 0 {
			fmt.Fprintf(&b, "\n  属性：%s", strings.Join(pairs, "；"))
		}
	}
	return b.String()
}

func formatEntityCompact(e domain.Entity) string {
	var parts []string
	if strings.TrimSpace(e.Summary) != "" {
		parts = append(parts, strings.TrimSpace(e.Summary))
	}
	if len(e.Attributes) > 0 {
		keys := sortedMapKeys(e.Attributes)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			v := strings.TrimSpace(e.Attributes[k])
			if v == "" {
				continue
			}
			pairs = append(pairs, fmt.Sprintf("%s:%s", k, v))
		}
		if len(pairs) > 0 {
			parts = append(parts, strings.Join(pairs, "; "))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("- [%s] %s", e.Kind.Label(), e.Name)
	}
	return fmt.Sprintf("- [%s] %s：%s", e.Kind.Label(), e.Name, strings.Join(parts, " | "))
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
