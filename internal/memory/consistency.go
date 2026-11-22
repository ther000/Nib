package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nib/internal/domain"
)

// LocalAuditInput 汇集第一层 0 成本本地全书一致性秒扫所需的全量静态与状态数据。
type LocalAuditInput struct {
	BookID         int64
	Entities       []domain.Entity
	Chapters       []domain.Chapter
	BeatsByChapter map[int64][]domain.Beat
	StateEvents    []domain.EntityStateEvent
	Foreshadowings []domain.Foreshadowing
	Deadlines      []domain.StoryDeadline
	PendingDeltas  []domain.ChapterDelta
	Options        domain.LocalAuditOptions
}

var terminalStateKeywords = []string{
	"死亡", "陨落", "身死", "阵亡", "被杀", "身亡", "毙命", "灭口",
	"神魂俱灭", "魂飞魄散", "已死", "亡故", "死去", "绝命", "身死道消",
	"已毁", "损毁", "碎裂", "崩碎", "破碎", "报废", "自毁", "彻底毁灭",
	"永久退场", "已退场",
}

var revivalKeywords = []string{
	"复活", "重生", "借尸还魂", "起死回生", "修复", "重铸", "复原", "苏醒", "重新激活",
}

var retirementKeywords = []string{
	"退场", "离队", "闭关", "远行", "失踪", "隐退", "飞升", "离开", "归隐", "沉睡",
}

var memorialContextKeywords = []string{
	"已故", "亡故", "死后", "遗物", "遗体", "尸体", "尸身", "墓碑", "坟墓",
	"牌位", "灵位", "祭奠", "追忆", "回忆起", "想起", "提及", "昔日", "生前",
	"陨落后", "身死后", "毁掉的", "碎片", "残骸", "亡师", "悼念",
}

// RunLocalConsistencyAudit 执行 0 成本本地确定性全书体检，零模型调用，
// 覆盖：已死/已毁复现、长期掉线、超期伏笔、时间线异常、积压待审变更单、下游脏标章。
func RunLocalConsistencyAudit(in LocalAuditInput) domain.LocalAuditReport {
	started := time.Now()

	absentThreshold := in.Options.AbsentThresholdChapters
	if absentThreshold <= 0 {
		absentThreshold = 20
	}
	maxDayJump := in.Options.MaxStoryDayJump
	if maxDayJump <= 0 {
		maxDayJump = 365
	}

	sortedChaps := make([]domain.Chapter, len(in.Chapters))
	copy(sortedChaps, in.Chapters)
	sort.Slice(sortedChaps, func(i, j int) bool {
		if sortedChaps[i].GlobalSeq != sortedChaps[j].GlobalSeq {
			return sortedChaps[i].GlobalSeq < sortedChaps[j].GlobalSeq
		}
		return sortedChaps[i].ID < sortedChaps[j].ID
	})

	latestSeq := 0
	chapByID := make(map[int64]domain.Chapter, len(sortedChaps))
	for _, ch := range sortedChaps {
		chapByID[ch.ID] = ch
		if ch.GlobalSeq > latestSeq {
			latestSeq = ch.GlobalSeq
		}
	}

	entityByID := make(map[int64]domain.Entity, len(in.Entities))
	for _, ent := range in.Entities {
		entityByID[ent.ID] = ent
	}

	findings := make([]domain.LocalAuditFinding, 0)
	seqCounter := 0
	nextID := func(prefix string) string {
		seqCounter++
		return fmt.Sprintf("%s-%d", prefix, seqCounter)
	}

	// 1. 回放 committed 状态事件，追踪每个实体的终结态（死亡/损毁）及退场/闭关标记。
	type terminalState struct {
		seq    int
		reason string
	}
	terminalByEntity := make(map[int64]terminalState)
	retiredByEntity := make(map[int64]bool)

	sortedEvents := make([]domain.EntityStateEvent, 0, len(in.StateEvents))
	for _, ev := range in.StateEvents {
		if ev.Status == domain.StateEventCommitted {
			sortedEvents = append(sortedEvents, ev)
		}
	}
	sort.Slice(sortedEvents, func(i, j int) bool {
		if sortedEvents[i].ChapterSeq != sortedEvents[j].ChapterSeq {
			return sortedEvents[i].ChapterSeq < sortedEvents[j].ChapterSeq
		}
		return sortedEvents[i].ID < sortedEvents[j].ID
	})

	for _, ev := range sortedEvents {
		combined := strings.TrimSpace(ev.NewValue + " " + ev.Reason)
		if containsAny(combined, revivalKeywords...) || containsExactSurvivalAlive(ev.AttrKey, ev.NewValue) {
			delete(terminalByEntity, ev.EntityID)
			retiredByEntity[ev.EntityID] = false
			continue
		}
		if containsAny(combined, terminalStateKeywords...) {
			if _, exists := terminalByEntity[ev.EntityID]; !exists {
				reason := strings.TrimSpace(ev.NewValue)
				if reason == "" {
					reason = strings.TrimSpace(ev.Reason)
				}
				terminalByEntity[ev.EntityID] = terminalState{seq: ev.ChapterSeq, reason: reason}
			}
			retiredByEntity[ev.EntityID] = true
			continue
		}
		if containsAny(combined, retirementKeywords...) {
			retiredByEntity[ev.EntityID] = true
		}
	}

	// 也检查实体静态属性中是否带有明确退场/死亡描述
	for _, ent := range in.Entities {
		if retiredByEntity[ent.ID] {
			continue
		}
		for _, v := range ent.Attributes {
			if containsAny(v, terminalStateKeywords...) || containsAny(v, retirementKeywords...) {
				retiredByEntity[ent.ID] = true
				break
			}
		}
	}

	// 1.5 检查已死/已毁实体是否在后续章非回忆语境或节拍中再次出现
	for _, ent := range in.Entities {
		termState, isTerminal := terminalByEntity[ent.ID]
		if !isTerminal {
			continue
		}
		terms := buildEntitySearchTerms(ent)
		for _, ch := range sortedChaps {
			if ch.GlobalSeq <= termState.seq {
				continue
			}
			beatHits := countBeatAppearancesForEntity(in.BeatsByChapter[ch.ID], ent.ID)
			if beatHits > 0 {
				findings = append(findings, domain.LocalAuditFinding{
					ID:           nextID("dead"),
					Category:     domain.LocalAuditDeadReappear,
					Severity:     domain.LocalAuditSeverityError,
					Title:        fmt.Sprintf("已故/已毁实体「%s」在节拍中复现", ent.Name),
					Detail:       fmt.Sprintf("第 %d 章状态已标记为「%s」，但第 %d 章《%s》的剧情节拍仍绑定了该实体。", termState.seq, termState.reason, ch.GlobalSeq, ch.Title),
					Suggestion:   "核查该章是否误绑实体、属于回忆插叙，或前文死亡设定需要补充复活逻辑。",
					ChapterID:    ch.ID,
					ChapterSeq:   ch.GlobalSeq,
					ChapterTitle: ch.Title,
					EntityID:     ent.ID,
					EntityName:   ent.Name,
					JumpTarget:   "studio",
				})
				continue
			}
			if hasActiveMentionAfterDeath(ch.Content, terms) {
				findings = append(findings, domain.LocalAuditFinding{
					ID:           nextID("dead"),
					Category:     domain.LocalAuditDeadReappear,
					Severity:     domain.LocalAuditSeverityError,
					Title:        fmt.Sprintf("已故/已毁实体「%s」在正文复现", ent.Name),
					Detail:       fmt.Sprintf("第 %d 章状态已标记为「%s」，第 %d 章《%s》正文中再次出现该实体且未呈现回忆/祭奠/遗物语境。", termState.seq, termState.reason, ch.GlobalSeq, ch.Title),
					Suggestion:   "跳转到正文核对是否写串人名、存在吃书复活，或补充明确的追忆/遗物语境提示。",
					ChapterID:    ch.ID,
					ChapterSeq:   ch.GlobalSeq,
					ChapterTitle: ch.Title,
					EntityID:     ent.ID,
					EntityName:   ent.Name,
					JumpTarget:   "studio",
				})
			}
		}
	}

	// 2. 核心配角长期掉线检查
	presence := ComputeEntityPresence(in.BookID, in.Entities, sortedChaps, in.BeatsByChapter, -1)
	for _, st := range presence.Stats {
		ent, ok := entityByID[st.EntityID]
		if !ok {
			continue
		}
		if ent.IsInvariant || ent.Kind != domain.EntityKindCharacter {
			continue
		}
		if st.NeverAppeared || st.AbsentChapters < absentThreshold {
			continue
		}
		if _, dead := terminalByEntity[ent.ID]; dead {
			continue
		}
		if retiredByEntity[ent.ID] {
			continue
		}
		findings = append(findings, domain.LocalAuditFinding{
			ID:           nextID("absent"),
			Category:     domain.LocalAuditAbsentEntity,
			Severity:     domain.LocalAuditSeverityWarn,
			Title:        fmt.Sprintf("角色「%s」已掉线 %d 章", ent.Name, st.AbsentChapters),
			Detail:       fmt.Sprintf("最后出场于第 %d 章《%s》（累计出场 %d 章），截至第 %d 章连续 %d 章未见出场且未标记退场/死亡。", st.LastSeenSeq, st.LastSeenChapterTitle, st.AppearanceChapters, latestSeq, st.AbsentChapters),
			Suggestion:   "若角色已暂时离场或闭关，可在其状态中记录退场原因；否则可考虑安排回收或支线呼应。",
			ChapterID:    st.LastSeenChapterID,
			ChapterSeq:   st.LastSeenSeq,
			ChapterTitle: st.LastSeenChapterTitle,
			EntityID:     ent.ID,
			EntityName:   ent.Name,
			JumpTarget:   "atlas_entity",
		})
	}

	// 3. 超期未收伏笔检查
	for _, fs := range in.Foreshadowings {
		if fs.Status != domain.ForeshadowingPlanted && fs.Status != domain.ForeshadowingHinted {
			continue
		}
		if fs.TargetResolveSeq <= 0 || latestSeq <= fs.TargetResolveSeq {
			continue
		}
		overdue := latestSeq - fs.TargetResolveSeq
		sev := domain.LocalAuditSeverityWarn
		if overdue >= 15 {
			sev = domain.LocalAuditSeverityError
		}
		findings = append(findings, domain.LocalAuditFinding{
			ID:              nextID("fs"),
			Category:        domain.LocalAuditOverdueForeshadow,
			Severity:        sev,
			Title:           fmt.Sprintf("伏笔「%s」已超期 %d 章未回收", fs.Title, overdue),
			Detail:          fmt.Sprintf("计划在第 %d 章前回收（当前状态：%s），目前已推进到第 %d 章。", fs.TargetResolveSeq, fs.Status.Label(), latestSeq),
			Suggestion:      "前往伏笔看板调整回收计划、或在后续章纲安排兑现回收 / 标记废弃。",
			ChapterSeq:      fs.TargetResolveSeq,
			ForeshadowingID: fs.ID,
			JumpTarget:      "atlas_foreshadow",
		})
	}

	// 4. 故事内时间线倒流 / 异常跨度 + 期限倒计时超期检查
	var (
		prevDayChap *domain.Chapter
		lastDayVal  *int
		lastDayChap *domain.Chapter
	)
	for i := range sortedChaps {
		ch := &sortedChaps[i]
		if ch.StoryDay == nil {
			continue
		}
		if prevDayChap != nil && prevDayChap.StoryDay != nil {
			diff := *ch.StoryDay - *prevDayChap.StoryDay
			if diff < 0 {
				findings = append(findings, domain.LocalAuditFinding{
					ID:           nextID("time"),
					Category:     domain.LocalAuditTimelineAnomaly,
					Severity:     domain.LocalAuditSeverityError,
					Title:        fmt.Sprintf("第 %d 章故事天数发生倒流（%d → %d）", ch.GlobalSeq, *prevDayChap.StoryDay, *ch.StoryDay),
					Detail:       fmt.Sprintf("前序标注章第 %d 章《%s》为第 %d 天，本章《%s》变为第 %d 天。", prevDayChap.GlobalSeq, prevDayChap.Title, *prevDayChap.StoryDay, ch.Title, *ch.StoryDay),
					Suggestion:   "前往大纲设计台修正章节时间坐标，或确认是否属于回忆倒叙章节。",
					ChapterID:    ch.ID,
					ChapterSeq:   ch.GlobalSeq,
					ChapterTitle: ch.Title,
					JumpTarget:   "architect",
				})
			} else if diff > maxDayJump {
				findings = append(findings, domain.LocalAuditFinding{
					ID:           nextID("time"),
					Category:     domain.LocalAuditTimelineAnomaly,
					Severity:     domain.LocalAuditSeverityWarn,
					Title:        fmt.Sprintf("第 %d 章故事时间跨度异常（+%d 天）", ch.GlobalSeq, diff),
					Detail:       fmt.Sprintf("从第 %d 章（第 %d 天）跳跃至第 %d 章（第 %d 天），超过预设阈值 %d 天。", prevDayChap.GlobalSeq, *prevDayChap.StoryDay, ch.GlobalSeq, *ch.StoryDay, maxDayJump),
					Suggestion:   "核查故事内相对天数是否录入笔误，或在体检选项中调高单次时间跨度阈值。",
					ChapterID:    ch.ID,
					ChapterSeq:   ch.GlobalSeq,
					ChapterTitle: ch.Title,
					JumpTarget:   "architect",
				})
			}
		}
		prevDayChap = ch
		dayCopy := *ch.StoryDay
		lastDayVal = &dayCopy
		lastDayChap = ch
	}

	if lastDayVal != nil {
		for _, dl := range in.Deadlines {
			if dl.Status != domain.StoryDeadlineActive || dl.TargetDay <= 0 {
				continue
			}
			if *lastDayVal > dl.TargetDay {
				overdueDays := *lastDayVal - dl.TargetDay
				chID := int64(0)
				chSeq := latestSeq
				chTitle := ""
				if lastDayChap != nil {
					chID = lastDayChap.ID
					chSeq = lastDayChap.GlobalSeq
					chTitle = lastDayChap.Title
				}
				findings = append(findings, domain.LocalAuditFinding{
					ID:           nextID("dl"),
					Category:     domain.LocalAuditTimelineAnomaly,
					Severity:     domain.LocalAuditSeverityWarn,
					Title:        fmt.Sprintf("故事期限「%s」已超期 %d 天未结算", dl.Title, overdueDays),
					Detail:       fmt.Sprintf("目标截止第 %d 天（%s），当前故事进度已达第 %d 天，但期限仍处于「倒计时中」。", dl.TargetDay, dl.TargetLabel, *lastDayVal),
					Suggestion:   "前往伏笔与期限面板将期限标记为已兑现、已超期或取消。",
					ChapterID:    chID,
					ChapterSeq:   chSeq,
					ChapterTitle: chTitle,
					DeadlineID:   dl.ID,
					JumpTarget:   "atlas_foreshadow",
				})
			}
		}
	}

	// 5. 积压未审记忆变更单检查
	pendingList := make([]domain.ChapterDelta, 0, len(in.PendingDeltas))
	for _, d := range in.PendingDeltas {
		if d.ReviewStatus == domain.DeltaReviewPending {
			pendingList = append(pendingList, d)
		}
	}
	sort.Slice(pendingList, func(i, j int) bool {
		ci, okI := chapByID[pendingList[i].ChapterID]
		cj, okJ := chapByID[pendingList[j].ChapterID]
		if okI && okJ && ci.GlobalSeq != cj.GlobalSeq {
			return ci.GlobalSeq < cj.GlobalSeq
		}
		return pendingList[i].ChapterID < pendingList[j].ChapterID
	})
	for _, d := range pendingList {
		ch := chapByID[d.ChapterID]
		findings = append(findings, domain.LocalAuditFinding{
			ID:           nextID("delta"),
			Category:     domain.LocalAuditPendingDelta,
			Severity:     domain.LocalAuditSeverityWarn,
			Title:        fmt.Sprintf("第 %d 章存在积压待审记忆变更单", ch.GlobalSeq),
			Detail:       fmt.Sprintf("第 %d 章《%s》已抽取 %d 条状态变更与 %d 条伏笔动作，尚未在写作台确认入库。", ch.GlobalSeq, ch.Title, len(d.StatePatches), len(d.Foreshadows)),
			Suggestion:   "跳转至该章写作台右侧「记忆审改」确认或驳回提案，防止后续章节依据过时记忆写作。",
			ChapterID:    ch.ID,
			ChapterSeq:   ch.GlobalSeq,
			ChapterTitle: ch.Title,
			JumpTarget:   "studio",
		})
	}

	// 6. 下游脏标章（upstream_dirty）检查
	for _, ch := range sortedChaps {
		if ch.Status != domain.ChapterStatusUpstreamDirty {
			continue
		}
		findings = append(findings, domain.LocalAuditFinding{
			ID:           nextID("dirty"),
			Category:     domain.LocalAuditUpstreamDirty,
			Severity:     domain.LocalAuditSeverityWarn,
			Title:        fmt.Sprintf("第 %d 章《%s》标记为「前文有变动」", ch.GlobalSeq, ch.Title),
			Detail:       "上游已确认记忆变更单或关键设定发生了更新，本章大纲或正文可能受影响。",
			Suggestion:   "在脏标修复队列中复核正文/重抽变更单/重跑 Critic 审校，确认无误后清除脏标。",
			ChapterID:    ch.ID,
			ChapterSeq:   ch.GlobalSeq,
			ChapterTitle: ch.Title,
			JumpTarget:   "dirty_queue",
		})
	}

	// 统一稳定排序：严重度优先（error > warn > info），其次章号升序，其次原始 ID。
	severityRank := func(s domain.LocalAuditSeverity) int {
		switch s {
		case domain.LocalAuditSeverityError:
			return 0
		case domain.LocalAuditSeverityWarn:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		ri, rj := severityRank(findings[i].Severity), severityRank(findings[j].Severity)
		if ri != rj {
			return ri < rj
		}
		if findings[i].ChapterSeq != findings[j].ChapterSeq {
			return findings[i].ChapterSeq < findings[j].ChapterSeq
		}
		return findings[i].ID < findings[j].ID
	})

	summary := domain.LocalAuditSummary{TotalFindings: len(findings)}
	for _, f := range findings {
		switch f.Severity {
		case domain.LocalAuditSeverityError:
			summary.ErrorCount++
		case domain.LocalAuditSeverityWarn:
			summary.WarnCount++
		default:
			summary.InfoCount++
		}
		switch f.Category {
		case domain.LocalAuditDeadReappear:
			summary.DeadReappearCount++
		case domain.LocalAuditAbsentEntity:
			summary.AbsentEntityCount++
		case domain.LocalAuditOverdueForeshadow:
			summary.OverdueForeshadowCount++
		case domain.LocalAuditTimelineAnomaly:
			summary.TimelineAnomalyCount++
		case domain.LocalAuditPendingDelta:
			summary.PendingDeltaCount++
		case domain.LocalAuditUpstreamDirty:
			summary.UpstreamDirtyCount++
		}
	}

	return domain.LocalAuditReport{
		BookID:           in.BookID,
		LatestChapterSeq: latestSeq,
		ScannedChapters:  len(sortedChaps),
		ScannedEntities:  len(in.Entities),
		ElapsedMs:        time.Since(started).Milliseconds(),
		Summary:          summary,
		Findings:         findings,
	}
}

func containsExactSurvivalAlive(attrKey, newVal string) bool {
	k := strings.ToLower(strings.TrimSpace(attrKey))
	v := strings.TrimSpace(newVal)
	if !isKeyMatch(k, "alive", "survival", "life_status", "vital_status", "存活", "生死", "存活状态") {
		return false
	}
	return containsAny(v, "存活", "健在", "alive", "复活", "重生")
}

// hasActiveMentionAfterDeath 扫描正文中实体词项的非重叠命中，
// 仅当存在至少一次前后 ~25 字符上下文中不包含回忆/祭奠/遗物词的命中时返回 true。
func hasActiveMentionAfterDeath(text string, sortedTerms []string) bool {
	if text == "" || len(sortedTerms) == 0 {
		return false
	}
	for i := 0; i < len(text); {
		matchedLen := 0
		rest := text[i:]
		for _, term := range sortedTerms {
			if len(rest) >= len(term) && strings.HasPrefix(rest, term) {
				matchedLen = len(term)
				break
			}
		}
		if matchedLen > 0 {
			window := extractRuneContextWindow(text, i, i+matchedLen, 25)
			if !containsAny(window, memorialContextKeywords...) {
				return true
			}
			i += matchedLen
			continue
		}
		_, width := utf8.DecodeRuneInString(rest)
		if width <= 0 {
			width = 1
		}
		i += width
	}
	return false
}

func extractRuneContextWindow(text string, startByte, endByte, radiusRunes int) string {
	left := text[:startByte]
	leftRunes := []rune(left)
	if len(leftRunes) > radiusRunes {
		leftRunes = leftRunes[len(leftRunes)-radiusRunes:]
	}
	right := text[endByte:]
	rightRunes := []rune(right)
	if len(rightRunes) > radiusRunes {
		rightRunes = rightRunes[:radiusRunes]
	}
	return string(leftRunes) + text[startByte:endByte] + string(rightRunes)
}
