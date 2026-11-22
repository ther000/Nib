package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"nib/internal/domain"
	"nib/internal/llm"
)

const defaultConsistencyAuditMaxTokens = 4096

// ConsistencyAuditStore 定义第二层 AI 深度语义审计所需的最小仓储接口（由 *store.Store 自动满足）。
type ConsistencyAuditStore interface {
	ListBookChapters(ctx context.Context, bookID int64) ([]domain.Chapter, error)
	ListChapters(ctx context.Context, volumeID int64) ([]domain.Chapter, error)
	ListEntities(ctx context.Context, bookID int64, filter domain.EntityFilter) ([]domain.Entity, error)
	ListStateEvents(ctx context.Context, bookID int64, entityID int64, upToSeq int, includePending bool) ([]domain.EntityStateEvent, error)
	ListForeshadowings(ctx context.Context, bookID int64, status domain.ForeshadowingStatus) ([]domain.Foreshadowing, error)
	ListStoryDeadlines(ctx context.Context, bookID int64, status domain.StoryDeadlineStatus) ([]domain.StoryDeadline, error)
	ListArcSummaries(ctx context.Context, bookID int64, level domain.ArcSummaryLevel) ([]domain.ArcSummary, error)
	SaveConsistencyReport(ctx context.Context, rep domain.BookConsistencyReport) (domain.BookConsistencyReport, error)
}

// RunAIConsistencyAudit 对指定分卷 / 章节区间 / 全书执行第二层 AI 深度语义一致性审计，
// 聚焦本地规则无法识破的战力体系崩坏、动机前后矛盾与金手指暗冲突，并持久化报告。
func (r *Runner) RunAIConsistencyAudit(ctx context.Context, req domain.RunAIConsistencyAuditRequest) (domain.BookConsistencyReport, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.BookConsistencyReport{}, errors.New("pipeline consistency audit dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.BookConsistencyReport{}, err
	}
	cs, ok := r.store.(ConsistencyAuditStore)
	if !ok {
		return domain.BookConsistencyReport{}, errors.New("store does not implement ConsistencyAuditStore")
	}

	book, err := r.store.GetBook(ctx, req.BookID)
	if err != nil {
		return domain.BookConsistencyReport{}, fmt.Errorf("load book %d: %w", req.BookID, err)
	}

	var (
		chaps      []domain.Chapter
		scopeTitle string
		scopeRefID int64
		startSeq   = req.StartSeq
		endSeq     = req.EndSeq
	)

	switch req.ScopeType {
	case domain.ConsistencyScopeVolume:
		vol, vErr := r.store.GetVolume(ctx, req.VolumeID)
		if vErr != nil {
			return domain.BookConsistencyReport{}, fmt.Errorf("load volume %d: %w", req.VolumeID, vErr)
		}
		if vol.BookID != req.BookID {
			return domain.BookConsistencyReport{}, &domain.ValidationError{Field: "volumeId", Message: "分卷不属于指定书籍"}
		}
		scopeRefID = vol.ID
		scopeTitle = fmt.Sprintf("分卷《%s》", vol.Title)
		chaps, err = cs.ListChapters(ctx, vol.ID)
		if err != nil {
			return domain.BookConsistencyReport{}, err
		}
	case domain.ConsistencyScopeRange:
		allChaps, lErr := cs.ListBookChapters(ctx, req.BookID)
		if lErr != nil {
			return domain.BookConsistencyReport{}, lErr
		}
		for _, ch := range allChaps {
			if ch.GlobalSeq >= startSeq && ch.GlobalSeq <= endSeq {
				chaps = append(chaps, ch)
			}
		}
		scopeTitle = fmt.Sprintf("第 %d~%d 章区间", startSeq, endSeq)
	default: // ConsistencyScopeBook
		chaps, err = cs.ListBookChapters(ctx, req.BookID)
		if err != nil {
			return domain.BookConsistencyReport{}, err
		}
		scopeTitle = "全书范围"
	}

	if len(chaps) == 0 {
		return domain.BookConsistencyReport{}, fmt.Errorf("%w: 所选范围内暂无章节可供审计", domain.ErrValidation)
	}
	sort.Slice(chaps, func(i, j int) bool {
		if chaps[i].GlobalSeq != chaps[j].GlobalSeq {
			return chaps[i].GlobalSeq < chaps[j].GlobalSeq
		}
		return chaps[i].ID < chaps[j].ID
	})
	if startSeq <= 0 {
		startSeq = chaps[0].GlobalSeq
	}
	if endSeq <= 0 {
		endSeq = chaps[len(chaps)-1].GlobalSeq
	}

	// 优先使用 critic 路由，依次回退 extract / outline。
	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StageCritic, domain.StageExtract, domain.StageOutline)
	if err != nil {
		return domain.BookConsistencyReport{}, err
	}

	entities, _ := cs.ListEntities(ctx, req.BookID, domain.EntityFilter{})
	events, _ := cs.ListStateEvents(ctx, req.BookID, 0, endSeq, true)
	foreshadows, _ := cs.ListForeshadowings(ctx, req.BookID, "")
	deadlines, _ := cs.ListStoryDeadlines(ctx, req.BookID, "")
	arcSummaries, _ := cs.ListArcSummaries(ctx, req.BookID, "")

	sysPrompt, userPrompt := buildAIConsistencyAuditPrompts(
		book, scopeTitle, startSeq, endSeq,
		chaps, entities, events, foreshadows, deadlines, arcSummaries,
	)

	rawResp, err := r.collectStageLLMResponse(
		ctx, req.BookID, 0, domain.StageCritic,
		provider, route, defaultConsistencyAuditMaxTokens,
		sysPrompt, userPrompt,
	)
	if err != nil {
		return domain.BookConsistencyReport{}, err
	}

	parsed, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		req.BookID,
		0,
		domain.StageCritic,
		provider,
		route,
		defaultConsistencyAuditMaxTokens,
		"全书深度语义审计报告（需包含 summary、findings）",
		rawResp,
		parseAIConsistencyPayload,
	)
	if parseErr != nil {
		return domain.BookConsistencyReport{}, parseErr
	}

	report := domain.BookConsistencyReport{
		BookID:     req.BookID,
		ScopeType:  req.ScopeType,
		ScopeRefID: scopeRefID,
		StartSeq:   startSeq,
		EndSeq:     endSeq,
		ModelName:  route.ModelName,
		Summary:    parsed.summary,
		Findings:   parsed.findings,
	}
	report.Normalize()
	return cs.SaveConsistencyReport(ctx, report)
}

type parsedAIConsistencyResult struct {
	summary  string
	findings []domain.AIConsistencyFinding
}

func parseAIConsistencyPayload(raw string) (parsedAIConsistencyResult, error) {
	jsonStr, err := llm.ExtractJSONObject(raw)
	if err != nil {
		return parsedAIConsistencyResult{}, err
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &top); err != nil {
		return parsedAIConsistencyResult{}, err
	}
	if _, ok := top["findings"]; !ok {
		if _, okSummary := top["summary"]; !okSummary {
			return parsedAIConsistencyResult{}, errors.New("JSON missing required fields (summary / findings)")
		}
	}

	var dto struct {
		Summary  string `json:"summary"`
		Findings []struct {
			Category    string   `json:"category"`
			Severity    string   `json:"severity"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Evidence    string   `json:"evidence"`
			Suggestion  string   `json:"suggestion"`
			ChapterSeqs []int    `json:"chapter_seqs"`
			SeqsCamel   []int    `json:"chapterSeqs"`
			EntityNames []string `json:"entity_names"`
			NamesCamel  []string `json:"entityNames"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return parsedAIConsistencyResult{}, fmt.Errorf("unmarshal consistency audit json: %w", err)
	}

	findings := make([]domain.AIConsistencyFinding, 0, len(dto.Findings))
	for i, f := range dto.Findings {
		title := strings.TrimSpace(f.Title)
		desc := strings.TrimSpace(f.Description)
		if title == "" && desc == "" {
			continue
		}
		if title == "" {
			title = fmt.Sprintf("跨章逻辑异常 #%d", i+1)
		}

		cat := normalizeAIConsistencyCategory(f.Category)
		sev := normalizeLocalAuditSeverity(f.Severity)
		seqs := f.ChapterSeqs
		if len(seqs) == 0 {
			seqs = f.SeqsCamel
		}
		if seqs == nil {
			seqs = []int{}
		}
		names := f.EntityNames
		if len(names) == 0 {
			names = f.NamesCamel
		}
		if names == nil {
			names = []string{}
		}

		findings = append(findings, domain.AIConsistencyFinding{
			ID:          fmt.Sprintf("ai-%d", i+1),
			Category:    cat,
			Severity:    sev,
			Title:       title,
			Description: desc,
			Evidence:    strings.TrimSpace(f.Evidence),
			Suggestion:  strings.TrimSpace(f.Suggestion),
			ChapterSeqs: seqs,
			EntityNames: names,
			Status:      domain.ConsistencyFindingOpen,
		})
	}

	summary := strings.TrimSpace(dto.Summary)
	if summary == "" {
		if len(findings) == 0 {
			summary = "所审计区间逻辑自洽，暂未发现明显的战力、动机或设定冲突。"
		} else {
			summary = fmt.Sprintf("共检出 %d 项需要关注的跨章深度一致性隐患。", len(findings))
		}
	}

	return parsedAIConsistencyResult{
		summary:  summary,
		findings: findings,
	}, nil
}

func normalizeAIConsistencyCategory(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "power_collapse":
		return "power_collapse"
	case "motivation_conflict":
		return "motivation_conflict"
	case "rule_contradiction":
		return "rule_contradiction"
	case "timeline_logic":
		return "timeline_logic"
	default:
		return "other"
	}
}

func normalizeLocalAuditSeverity(raw string) domain.LocalAuditSeverity {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "error", "high", "critical":
		return domain.LocalAuditSeverityError
	case "info", "low":
		return domain.LocalAuditSeverityInfo
	default:
		return domain.LocalAuditSeverityWarn
	}
}

func buildAIConsistencyAuditPrompts(
	book domain.Book,
	scopeTitle string,
	startSeq int,
	endSeq int,
	chaps []domain.Chapter,
	entities []domain.Entity,
	events []domain.EntityStateEvent,
	foreshadows []domain.Foreshadowing,
	deadlines []domain.StoryDeadline,
	arcSummaries []domain.ArcSummary,
) (string, string) {
	sysPrompt := `你是资深长篇中文网文剧情总监与设定防吃书审查专家。
请通读作者提供的书目设定、世界铁律、分层弧线摘要、关键状态演变与分章剧情脉络，重点排查本地确定性规则无法识别的深层逻辑隐患：
1. power_collapse：战力体系跨阶崩坏（如境界大幅落差无合理解释反杀、越级战力前后自相矛盾）；
2. motivation_conflict：人物行为动机前后矛盾（性格、阵营利益或既定仇怨突然转向却缺乏铺垫）；
3. rule_contradiction：金手指规则、修炼/商业铁律自相矛盾或设定暗冲突；
4. timeline_logic：跨章因果链断裂、前后信息差泄密（角色提前知道了尚未获知的情报）或时间调度不合理。

严格按以下 JSON 格式输出（不要包裹在 markdown 以外的说明文字中）：
{
  "summary": "100字以内的整体深度一致性评价",
  "findings": [
    {
      "category": "power_collapse | motivation_conflict | rule_contradiction | timeline_logic | other",
      "severity": "error | warn | info",
      "title": "一句话问题标题",
      "description": "具体矛盾描述与涉及的因果冲突",
      "evidence": "引用的章节号或设定对比依据",
      "suggestion": "可执行的具体修改建议",
      "chapter_seqs": [12, 35],
      "entity_names": ["林渊"]
    }
  ]
}
若区间内无深层逻辑问题，findings 返回空数组 [] 并在 summary 说明原因。`

	var b strings.Builder
	fmt.Fprintf(&b, "【审计书目】《%s》（题材：%s）\n", book.Title, book.Genre)
	fmt.Fprintf(&b, "【审计范围】%s（第 %d~%d 章）\n", scopeTitle, startSeq, endSeq)
	if strings.TrimSpace(book.Premise) != "" {
		fmt.Fprintf(&b, "核心主线：%s\n", strings.TrimSpace(book.Premise))
	}
	if strings.TrimSpace(book.GoldenFinger) != "" {
		fmt.Fprintf(&b, "核心金手指规则：%s\n", strings.TrimSpace(book.GoldenFinger))
	}

	entityNameByID := make(map[int64]string, len(entities))
	invariants := make([]domain.Entity, 0)
	for _, ent := range entities {
		entityNameByID[ent.ID] = ent.Name
		if ent.IsInvariant {
			invariants = append(invariants, ent)
		}
	}

	if len(invariants) > 0 {
		b.WriteString("\n【世界铁律（P0 不可违背）】\n")
		for _, inv := range invariants {
			fmt.Fprintf(&b, "- %s：%s\n", inv.Name, strings.TrimSpace(inv.Summary))
		}
	}

	// 纳入与目标区间有交集的分层摘要
	relevantArcs := make([]domain.ArcSummary, 0)
	for _, arc := range arcSummaries {
		if arc.EndSeq >= startSeq && arc.StartSeq <= endSeq {
			relevantArcs = append(relevantArcs, arc)
		}
	}
	if len(relevantArcs) > 0 {
		b.WriteString("\n【相关弧线 / 卷级摘要】\n")
		for _, arc := range relevantArcs {
			fmt.Fprintf(&b, "- [%s 第%d~%d章] %s\n", arc.Level, arc.StartSeq, arc.EndSeq, strings.TrimSpace(arc.Summary))
		}
	}

	// 纳入截至 endSeq 的关键状态事件（优先保留高危，并限制条数防爆上下文）
	if len(events) > 0 {
		b.WriteString("\n【关键人物/道具状态演变事件】\n")
		maxEvents := 60
		startIdx := 0
		if len(events) > maxEvents {
			startIdx = len(events) - maxEvents
		}
		for _, ev := range events[startIdx:] {
			entName := entityNameByID[ev.EntityID]
			if entName == "" {
				entName = fmt.Sprintf("实体#%d", ev.EntityID)
			}
			riskMark := ""
			if ev.ChangeSeverity == domain.ChangeSeverityHighRisk {
				riskMark = "[高危]"
			}
			fmt.Fprintf(&b, "- 第%d章 %s%s.%s：%s -> %s（%s）\n",
				ev.ChapterSeq, riskMark, entName, ev.AttrKey, ev.PrevValue, ev.NewValue, ev.Reason)
		}
	}

	if len(foreshadows) > 0 {
		b.WriteString("\n【相关伏笔列表】\n")
		count := 0
		for _, fs := range foreshadows {
			if fs.Status == domain.ForeshadowingAbandoned {
				continue
			}
			fmt.Fprintf(&b, "- [%s] %s（埋设第%d章 / 目标回收第%d章）：%s\n",
				fs.Status.Label(), fs.Title, fs.PlantedChapterSeq, fs.TargetResolveSeq, strings.TrimSpace(fs.Description))
			count++
			if count >= 25 {
				break
			}
		}
	}

	if len(deadlines) > 0 {
		b.WriteString("\n【故事期限倒计时】\n")
		for _, dl := range deadlines {
			fmt.Fprintf(&b, "- [%s] %s（目标第%d天 %s）\n", dl.Status.Label(), dl.Title, dl.TargetDay, dl.TargetLabel)
		}
	}

	b.WriteString("\n【区间分章脉络】\n")
	for _, ch := range chaps {
		timePart := ""
		if strings.TrimSpace(ch.StoryTimeLabel) != "" || ch.StoryDay != nil {
			dayStr := ""
			if ch.StoryDay != nil {
				dayStr = fmt.Sprintf("Day %d ", *ch.StoryDay)
			}
			timePart = fmt.Sprintf(" [%s%s]", dayStr, strings.TrimSpace(ch.StoryTimeLabel))
		}
		fmt.Fprintf(&b, "第%d章《%s》%s：", ch.GlobalSeq, ch.Title, timePart)
		switch {
		case strings.TrimSpace(ch.CausalSummary) != "":
			b.WriteString(strings.TrimSpace(ch.CausalSummary))
		case strings.TrimSpace(ch.OutlineSummary) != "":
			b.WriteString("（章纲）" + strings.TrimSpace(ch.OutlineSummary))
		case strings.TrimSpace(ch.Content) != "":
			b.WriteString("（正文摘录）" + headRunes(strings.TrimSpace(ch.Content), 280))
		default:
			b.WriteString("（待写）")
		}
		b.WriteByte('\n')
	}

	return sysPrompt, b.String()
}
