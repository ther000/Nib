package linter

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"nib/internal/domain"
)

const (
	fallbackMaxParagraphChars = 160
	minPsychParagraphWords    = 28
	psychMonologueRunTrigger  = 3
)

var (
	// 章末/段末鸡汤升华内置兜底正则（对应 docs/03-core-engines.md §3.2 B.3）。
	reBuiltinEndingEpiphany = regexp.MustCompile(
		`(他知道|他明白|他深知|他清楚|这一刻他意识到|所有人都明白)[^。！？\n]*(真正的考验|才刚刚开始|命运的齿轮|新的篇章|新的征程|才刚刚拉开序幕)`,
	)

	// 常见中文动作推进动词词表，用于判定某段是否为“零对话且零具体动作”的纯心理灌水段。
	concreteActionVerbs = []string{
		"斩", "刺", "劈", "挥", "踏", "退", "冲", "抓", "拍", "按", "握", "拔", "掷", "踹",
		"闪", "跃", "推", "拉", "转身", "抬手", "迈步", "递", "取", "收", "吞", "服", "喝",
		"坐", "站", "走", "跑", "奔", "杀", "击", "砸", "摔", "撞", "扣", "签", "转账",
		"拨通", "挂断", "推门", "落座", "掠", "捏", "踢", "射", "祭出", "催动", "引爆", "取出",
		"扔", "甩", "挡", "架", "翻", "跃起", "后撤", "欺身", "点指", "按住", "推开", "合上",
	}

	// 典型纯心理复盘/空泛情绪词表。
	psychReflectionMarkers = []string{
		"心想", "心中", "暗道", "思忖", "思索", "意识到", "觉得", "明白", "回忆", "想到",
		"念头", "情绪", "内心", "迷茫", "感慨", "犹豫", "彷徨", "沉思", "考量", "盘算",
		"滋味", "复盘", "意义", "命运", "苦涩", "纠结", "思绪", "脑海中", "暗自",
	}
)

// DefaultRules 返回 docs/03-core-engines.md §3.2 规定的内置中文网文去 AI 味默认规则集（BookID=0）。
func DefaultRules() []domain.DeaiRule {
	return []domain.DeaiRule{
		// A. 高频 AI 套话词库 (cliche)
		{ID: 1, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "嘴角勾起", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 套话：直接写具体动作、台词或神态细节，删去“嘴角勾起”", Enabled: true},
		{ID: 2, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "一抹弧度", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 套话：删去“一抹弧度”，改用克制的动作或对话表达情绪", Enabled: true},
		{ID: 3, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "眼神微眯", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 套话：避免“眼神微眯”，改写为观察到的具体破绽或手中动作", Enabled: true},
		{ID: 4, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: `眼中闪过一抹(精光|寒芒|玩味)`, IsRegex: true, Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 套话：禁用“眼中闪过一抹…”，用实质行动或利益算计体现意图", Enabled: true},
		{ID: 5, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "空气仿佛凝固", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 套话：删去“空气仿佛凝固”，通过器物声响或呼吸停顿写压迫感", Enabled: true},
		{ID: 6, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "心中涌起一股", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 套话：避免直接总结“心中涌起一股…”，用生理反应或动作外化", Enabled: true},
		{ID: 7, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "倒吸一口凉气", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频网文水词：禁止路人“倒吸一口凉气”，改写为克制的失态细节或改换称呼", Enabled: true},
		{ID: 8, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "这一刻", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 强调词：删去“这一刻”，直接让动作自然衔接", Enabled: true},
		{ID: 9, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "深吸一口气", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频水词：删去“深吸一口气”，直接切入人物动作或开口台词", Enabled: true},
		{ID: 10, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "缓缓说道", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 对话标签：删去“缓缓说道”，用动作伴随台词或直接出对话", Enabled: true},
		{ID: 11, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "嘴角泛起", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频 AI 套话：删去“嘴角泛起”，用具体台词或手部动作替代", Enabled: true},
		{ID: 12, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "令人窒息的", Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "高频空泛形容词：删去“令人窒息的”，写清具体压制手段或局势代价", Enabled: true},
		{ID: 13, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "不由得", Severity: domain.LintSeverityWarn, MaxPerChapter: 1, Hint: "高频连接水词：单章最多保留 1 次“不由得”，多余处直接删去", Enabled: true},
		{ID: 14, BookID: 0, Category: domain.DeaiCategoryCliche, Pattern: "宛如", Severity: domain.LintSeverityWarn, MaxPerChapter: 1, Hint: "书面比喻高频词：单章最多保留 1 次“宛如”，多用干脆动词短句", Enabled: true},

		// B. 句式结构病 (structure)
		{ID: 15, BookID: 0, Category: domain.DeaiCategoryStructure, Pattern: `不是[^，。！？\n]{1,30}而是`, IsRegex: true, Severity: domain.LintSeverityWarn, MaxPerChapter: 2, Hint: "密集“不是……而是……”对比句：单章超过 2 次易显 AI 议论腔，建议直接陈述事实", Enabled: true},
		{ID: 16, BookID: 0, Category: domain.DeaiCategoryStructure, Pattern: `与其说[^，。！？\n]{1,30}不如说`, IsRegex: true, Severity: domain.LintSeverityWarn, MaxPerChapter: 1, Hint: "翻译腔对比句：减少“与其说……不如说……”，直接写人物动作或判断", Enabled: true},
		{ID: 17, BookID: 0, Category: domain.DeaiCategoryStructure, Pattern: `(他知道|他明白|这一刻他意识到)[^。！？\n]*(真正的考验|才刚刚开始|命运的齿轮|新的篇章)`, IsRegex: true, Severity: domain.LintSeverityError, MaxPerChapter: 0, Hint: "章末/段末强行鸡汤升华：严禁套路总结，改用具体动作、对话或悬念细节收尾", Enabled: true},

		// C. 网文排版与推进病 (formatting)
		{ID: 18, BookID: 0, Category: domain.DeaiCategoryFormatting, Pattern: "单段超长文字墙 (>160字)", Severity: domain.LintSeverityWarn, MaxPerChapter: 0, Hint: "移动端网文排版病：单段超过字数上限，建议在动作转折或对话处拆分为短段", Enabled: true},
		{ID: 19, BookID: 0, Category: domain.DeaiCategoryFormatting, Pattern: "连续3段纯心理无对话动作推进", Severity: domain.LintSeverityWarn, MaxPerChapter: 0, Hint: "叙事推进病：连续 3 段以上缺乏对话与具体动作推进，建议改用行动外化情绪", Enabled: true},
	}
}

// SplitParagraphs 将章节正文按换行拆分为稳定有序的非空段落切片（索引 0..N-1）。
func SplitParagraphs(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// JoinParagraphs 将段落切片按网文标准双换行拼接回完整正文。
func JoinParagraphs(paragraphs []string) string {
	cleaned := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return strings.Join(cleaned, "\n\n")
}

// Lint 在本地 0 成本（<20ms）扫描章节正文，返回全部精确定位到段落与 Rune 区间的 LintIssue 及去 AI 味评分（0..100）。
func Lint(content string, rules []domain.DeaiRule, profile domain.StyleProfile) ([]domain.LintIssue, int) {
	paragraphs := SplitParagraphs(content)
	if len(paragraphs) == 0 {
		return []domain.LintIssue{}, domain.DefaultDeaiScore
	}

	activeRules := rules
	if activeRules == nil {
		activeRules = DefaultRules()
	}

	wallCheckEnabled := true
	psychCheckEnabled := true

	type compiledRule struct {
		rule domain.DeaiRule
		re   *regexp.Regexp
	}

	patternRules := make([]compiledRule, 0, len(activeRules))
	for _, r := range activeRules {
		// 检查内置排版规则是否被用户在规则表中显式禁用。
		if r.Category == domain.DeaiCategoryFormatting && isBuiltinFormattingDescriptor(r.Pattern) {
			if strings.Contains(r.Pattern, "文字墙") || strings.Contains(r.Pattern, "单段") {
				wallCheckEnabled = r.Enabled
			}
			if strings.Contains(r.Pattern, "心理") || strings.Contains(r.Pattern, "连续") {
				psychCheckEnabled = r.Enabled
			}
			continue
		}
		if !r.Enabled || strings.TrimSpace(r.Pattern) == "" {
			continue
		}
		var re *regexp.Regexp
		if r.IsRegex {
			compiled, err := regexp.Compile(r.Pattern)
			if err != nil {
				continue
			}
			re = compiled
		}
		patternRules = append(patternRules, compiledRule{rule: r, re: re})
	}

	type spanKey struct {
		paraIdx   int
		startRune int
		endRune   int
	}
	seenSpans := make(map[spanKey]bool)
	issues := make([]domain.LintIssue, 0)

	addIssue := func(iss domain.LintIssue) {
		k := spanKey{paraIdx: iss.ParagraphIndex, startRune: iss.StartRune, endRune: iss.EndRune}
		if seenSpans[k] {
			return
		}
		seenSpans[k] = true
		issues = append(issues, iss)
	}

	// 1. 扫描所有启用的 cliche / structure / 自定义规则（支持单章允许阈值 MaxPerChapter）。
	for _, cr := range patternRules {
		hitCount := 0
		ruleID := formatRuleID(cr.rule)
		sev := cr.rule.Severity
		if !sev.Valid() {
			sev = domain.LintSeverityWarn
		}
		hint := strings.TrimSpace(cr.rule.Hint)
		if hint == "" {
			hint = fmt.Sprintf("命中去 AI 味规则「%s」，建议改写为具体动作或对话", cr.rule.Pattern)
		}

		for pIdx, para := range paragraphs {
			matches := findPatternMatches(para, cr.rule.Pattern, cr.re)
			for _, m := range matches {
				hitCount++
				if hitCount <= cr.rule.MaxPerChapter {
					continue
				}
				addIssue(domain.LintIssue{
					RuleID:         ruleID,
					Category:       cr.rule.Category,
					Severity:       sev,
					ParagraphIndex: pIdx,
					StartRune:      m.startRune,
					EndRune:        m.endRune,
					MatchedText:    m.text,
					Hint:           hint,
				})
			}
		}
	}

	// 2. 内置句式结构病检测：同段内 AI 三段式对称排比。
	for pIdx, para := range paragraphs {
		if m, ok := detectParallelTriplet(para); ok {
			addIssue(domain.LintIssue{
				RuleID:         "structure-parallel-triplet",
				Category:       domain.DeaiCategoryStructure,
				Severity:       domain.LintSeverityWarn,
				ParagraphIndex: pIdx,
				StartRune:      m.startRune,
				EndRune:        m.endRune,
				MatchedText:    m.text,
				Hint:           "AI 三段式对称排比：同段连续三句结构高度对称，建议打碎句式改为非对称动作推进",
			})
		}
	}

	// 3. 内置结尾强检：章末最后两段强行鸡汤升华（若规则表已被去重则自动跳过）。
	startTailIdx := max(0, len(paragraphs)-2)
	for pIdx := startTailIdx; pIdx < len(paragraphs); pIdx++ {
		para := paragraphs[pIdx]
		for _, m := range findPatternMatches(para, "", reBuiltinEndingEpiphany) {
			addIssue(domain.LintIssue{
				RuleID:         "structure-ending-epiphany",
				Category:       domain.DeaiCategoryStructure,
				Severity:       domain.LintSeverityError,
				ParagraphIndex: pIdx,
				StartRune:      m.startRune,
				EndRune:        m.endRune,
				MatchedText:    m.text,
				Hint:           "章末强行鸡汤升华：禁止在结尾总结“真正的考验才刚刚开始/命运的齿轮”，必须用具体动作或悬念收尾",
			})
		}
	}

	// 4. 内置排版病 1：单段超长文字墙。
	maxParaChars := profile.MaxParagraphChars
	if maxParaChars <= 0 {
		maxParaChars = fallbackMaxParagraphChars
	}
	if wallCheckEnabled {
		for pIdx, para := range paragraphs {
			wc := domain.CountWords(para)
			if wc > maxParaChars {
				runeLen := utf8.RuneCountInString(para)
				addIssue(domain.LintIssue{
					RuleID:         "formatting-wall-of-text",
					Category:       domain.DeaiCategoryFormatting,
					Severity:       domain.LintSeverityWarn,
					ParagraphIndex: pIdx,
					StartRune:      0,
					EndRune:        runeLen,
					MatchedText:    previewText(para, 24),
					Hint:           fmt.Sprintf("文字墙告警：本段共 %d 字（超过单段上限 %d 字），手机阅读易劝退，建议按动作或对话拆段", wc, maxParaChars),
				})
			}
		}
	}

	// 5. 内置排版病 2：连续 >= 3 段既无对话也无具体动作的纯心理灌水段。
	if psychCheckEnabled {
		psychRun := 0
		for pIdx, para := range paragraphs {
			if isPurePsychParagraph(para) {
				psychRun++
				if psychRun >= psychMonologueRunTrigger {
					runeLen := utf8.RuneCountInString(para)
					addIssue(domain.LintIssue{
						RuleID:         "formatting-psych-monologue-run",
						Category:       domain.DeaiCategoryFormatting,
						Severity:       domain.LintSeverityWarn,
						ParagraphIndex: pIdx,
						StartRune:      0,
						EndRune:        runeLen,
						MatchedText:    previewText(para, 24),
						Hint:           fmt.Sprintf("连续 %d 段纯心理活动灌水（无双引号对话且无具体动作推进），建议删减内耗或改用动作/台词外化", psychRun),
					})
				}
			} else {
				psychRun = 0
			}
		}
	}

	// 按段落号与起始字符稳定排序。
	slices.SortFunc(issues, func(a, b domain.LintIssue) int {
		if a.ParagraphIndex != b.ParagraphIndex {
			return a.ParagraphIndex - b.ParagraphIndex
		}
		if a.StartRune != b.StartRune {
			return a.StartRune - b.StartRune
		}
		return a.EndRune - b.EndRune
	})

	score := computeDeaiScore(issues)
	return issues, score
}

type textMatch struct {
	startRune int
	endRune   int
	text      string
}

func findPatternMatches(para, literal string, re *regexp.Regexp) []textMatch {
	if re != nil {
		locs := re.FindAllStringIndex(para, -1)
		if len(locs) == 0 {
			return nil
		}
		out := make([]textMatch, 0, len(locs))
		for _, loc := range locs {
			if loc[0] >= loc[1] {
				continue
			}
			out = append(out, textMatch{
				startRune: utf8.RuneCountInString(para[:loc[0]]),
				endRune:   utf8.RuneCountInString(para[:loc[1]]),
				text:      para[loc[0]:loc[1]],
			})
		}
		return out
	}

	if literal == "" {
		return nil
	}
	var out []textMatch
	searchFrom := 0
	patLen := len(literal)
	for searchFrom < len(para) {
		idx := strings.Index(para[searchFrom:], literal)
		if idx < 0 {
			break
		}
		byteStart := searchFrom + idx
		byteEnd := byteStart + patLen
		out = append(out, textMatch{
			startRune: utf8.RuneCountInString(para[:byteStart]),
			endRune:   utf8.RuneCountInString(para[:byteEnd]),
			text:      para[byteStart:byteEnd],
		})
		searchFrom = byteEnd
	}
	return out
}

type clauseSpan struct {
	text      string
	runes     []rune
	startRune int
	endRune   int
}

// detectParallelTriplet 检测单段内连续 3 个高度对称的 AI 排比分句。
func detectParallelTriplet(para string) (textMatch, bool) {
	clauses := splitClausesWithOffsets(para)
	if len(clauses) < 3 {
		return textMatch{}, false
	}

	for i := 0; i+2 < len(clauses); i++ {
		c1, c2, c3 := clauses[i], clauses[i+1], clauses[i+2]
		if isParallelClauseTriplet(c1.runes, c2.runes, c3.runes) {
			allRunes := []rune(para)
			start := c1.startRune
			end := min(len(allRunes), c3.endRune)
			if start < end {
				return textMatch{
					startRune: start,
					endRune:   end,
					text:      string(allRunes[start:end]),
				}, true
			}
		}
	}
	return textMatch{}, false
}

func splitClausesWithOffsets(para string) []clauseSpan {
	runes := []rune(para)
	var out []clauseSpan
	start := 0
	for i, r := range runes {
		if isClauseDelimiter(r) {
			if span, ok := trimClauseRunes(runes, start, i); ok {
				out = append(out, span)
			}
			start = i + 1
		}
	}
	if start < len(runes) {
		if span, ok := trimClauseRunes(runes, start, len(runes)); ok {
			out = append(out, span)
		}
	}
	return out
}

func trimClauseRunes(runes []rune, start, end int) (clauseSpan, bool) {
	for start < end && (unicode.IsSpace(runes[start]) || isQuoteRune(runes[start])) {
		start++
	}
	for end > start && (unicode.IsSpace(runes[end-1]) || isQuoteRune(runes[end-1])) {
		end--
	}
	if end-start < 3 {
		return clauseSpan{}, false
	}
	slice := runes[start:end]
	return clauseSpan{
		text:      string(slice),
		runes:     slice,
		startRune: start,
		endRune:   end,
	}, true
}

func isClauseDelimiter(r rune) bool {
	switch r {
	case '，', '；', '。', '！', '？', ',', ';':
		return true
	default:
		return false
	}
}

func isQuoteRune(r rune) bool {
	switch r {
	case '“', '”', '"', '‘', '’', '\'', '「', '」', '『', '』':
		return true
	default:
		return false
	}
}

func isParallelClauseTriplet(c1, c2, c3 []rune) bool {
	l1, l2, l3 := len(c1), len(c2), len(c3)
	if l1 < 4 || l2 < 4 || l3 < 4 || l1 > 28 || l2 > 28 || l3 > 28 {
		return false
	}

	// 模式 A：首部 2+ 字完全相同的三连排比（如“那是…，那是…，那是…”、“没有…，没有…，没有…”、“他的…，他的…，他的…”）。
	pLen := commonPrefixLen(c1, c2, c3)
	if pLen >= 2 {
		return true
	}

	// 模式 B：典型递进/并列关联词三段排比（如“既…，又…，更…”、“或…，或…，或…”、“三分…，三分…，四分…”）。
	if pLen == 1 && isParallelLeadRune(c1[0]) && maxDiff3(l1, l2, l3) <= 4 {
		return true
	}
	if isCorrelativeTriplet(string(c1), string(c2), string(c3)) {
		return true
	}

	// 模式 C：字数几乎完全一致（差值 <= 1）且相同位置虚词/结构骨架对称（如“X着Y的Z，X着Y的Z，X着Y的Z”）。
	if l1 >= 5 && l1 <= 16 && maxDiff3(l1, l2, l3) <= 1 {
		if sharedParticlePosition(c1, c2, c3) {
			return true
		}
	}
	return false
}

func commonPrefixLen(c1, c2, c3 []rune) int {
	n := min(len(c1), min(len(c2), len(c3)))
	count := 0
	for i := 0; i < n; i++ {
		if c1[i] != c2[i] || c1[i] != c3[i] {
			break
		}
		count++
	}
	return count
}

func isParallelLeadRune(r rune) bool {
	switch r {
	case '那', '这', '没', '无', '不', '或', '每', '凡', '皆', '尽', '似', '如', '像', '带', '透', '含':
		return true
	default:
		return false
	}
}

func isCorrelativeTriplet(s1, s2, s3 string) bool {
	patterns := [][3]string{
		{"既", "又", "更"},
		{"不", "不", "更不"},
		{"一分", "一分", "一分"},
		{"三分", "三分", "四分"},
		{"不仅", "而且", "甚至"},
		{"不是", "不是", "而是"},
		{"无关", "无关", "只关"},
	}
	for _, p := range patterns {
		if strings.HasPrefix(s1, p[0]) && strings.HasPrefix(s2, p[1]) && strings.HasPrefix(s3, p[2]) {
			return true
		}
	}
	return false
}

func sharedParticlePosition(c1, c2, c3 []rune) bool {
	n := min(len(c1), min(len(c2), len(c3)))
	sharedAnchors := 0
	for i := 1; i < n-1; i++ {
		if c1[i] == c2[i] && c1[i] == c3[i] {
			switch c1[i] {
			case '的', '地', '得', '着', '了', '于', '如', '似', '般', '之', '与', '而':
				sharedAnchors++
			}
		}
	}
	return sharedAnchors >= 1
}

func maxDiff3(a, b, c int) int {
	hi := max(a, max(b, c))
	lo := min(a, min(b, c))
	return hi - lo
}

func isPurePsychParagraph(para string) bool {
	if domain.CountWords(para) < minPsychParagraphWords {
		return false
	}
	// 含任何对话引号则不属于纯心理段。
	for _, r := range para {
		if isQuoteRune(r) {
			return false
		}
	}
	// 含具体外化动作动词则视为有叙事动作推进。
	for _, verb := range concreteActionVerbs {
		if strings.Contains(para, verb) {
			return false
		}
	}
	// 统计心理复盘标记词出现次数。
	psychHits := 0
	for _, m := range psychReflectionMarkers {
		if strings.Contains(para, m) {
			psychHits++
		}
	}
	return psychHits >= 1
}

func isBuiltinFormattingDescriptor(pattern string) bool {
	return strings.Contains(pattern, "文字墙") ||
		strings.Contains(pattern, "单段超长") ||
		strings.Contains(pattern, "纯心理") ||
		strings.Contains(pattern, "无对话动作推进")
}

func formatRuleID(r domain.DeaiRule) string {
	if r.ID > 0 {
		return fmt.Sprintf("rule-%d", r.ID)
	}
	return fmt.Sprintf("%s-%s", r.Category, previewText(r.Pattern, 12))
}

func previewText(s string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "…"
}

func computeDeaiScore(issues []domain.LintIssue) int {
	score := domain.DefaultDeaiScore
	for _, iss := range issues {
		switch iss.Severity {
		case domain.LintSeverityError:
			score -= 12
		case domain.LintSeverityWarn:
			score -= 5
		case domain.LintSeverityInfo:
			score -= 2
		default:
			score -= 5
		}
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}
