package memory

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"nib/internal/domain"
)

var (
	// 匹配中文/英文货币或数字表达式，如 "128万"、"1.5亿"、"500,000元"、"2.4M"、"300k"。
	reArabicAmount = regexp.MustCompile(`(?i)(\d[\d,]*(?:\.\d+)?)\s*(亿|千万|百万|十万|万|千|百|b|billion|m|million|k|thousand)?`)
	reCNNumeral    = regexp.MustCompile(`([零一二两三四五六七八九十百千]+)\s*(亿|万|千|百)?`)
)

// ClassifyHighRisk 依据 docs/03-core-engines.md §1.4 的 5 条强规则判定单条实体状态变更是否属于高危（high_risk）。
// 五条强制标红规则：
//  1. 角色存活状态变更（死亡、失踪、夺舍、废除修为）；
//  2. 实力境界/身份阶层跨大阶跃升（或跌落）；
//  3. 核心道具/功法/公司股权发生易主或损毁；
//  4. 重要角色阵营关系翻转（盟友反目/投降归顺/叛宗）；
//  5. 都市题材现金流单章变动幅度 >= 当前总资产 30%。
func ClassifyHighRisk(genre domain.GenreID, patch domain.DeltaStatePatch) domain.ChangeSeverity {
	key := strings.ToLower(strings.TrimSpace(patch.AttrKey))
	prevVal := strings.TrimSpace(patch.PrevValue)
	newVal := strings.TrimSpace(patch.NewValue)
	reason := strings.TrimSpace(patch.Reason)
	combinedNew := newVal + " " + reason

	// 规则 1：存活状态变更 / 死亡 / 失踪 / 夺舍 / 废除修为。
	if isSurvivalHighRisk(key, prevVal, newVal, combinedNew) {
		return domain.ChangeSeverityHighRisk
	}

	// 规则 2：实力境界或身份阶层跨大阶跃升/跌落。
	if isMajorTierJump(key, prevVal, newVal, combinedNew) {
		return domain.ChangeSeverityHighRisk
	}

	// 规则 3：核心道具/功法/公司股权易主或损毁。
	if isOwnershipOrDestructionHighRisk(key, prevVal, newVal, combinedNew) {
		return domain.ChangeSeverityHighRisk
	}

	// 规则 4：重要角色阵营关系翻转。
	if isFactionFlipHighRisk(key, prevVal, newVal, combinedNew) {
		return domain.ChangeSeverityHighRisk
	}

	// 规则 5：都市题材现金流/资产单章变动幅度 >= 30%。
	if isUrbanCashflowSwingHighRisk(genre, key, prevVal, newVal) {
		return domain.ChangeSeverityHighRisk
	}

	return domain.ChangeSeverityNormal
}

func isSurvivalHighRisk(key, prevVal, newVal, combinedNew string) bool {
	if isKeyMatch(key, "alive", "survival", "life_status", "vital_status", "存活", "生死", "存活状态") && prevVal != newVal && newVal != "" {
		return true
	}
	lethalKeywords := []string{
		"死亡", "身死", "陨落", "阵亡", "被杀", "身亡", "毙命", "灭口",
		"神魂俱灭", "魂飞魄散", "已死", "亡故", "死去", "断气", "绝命",
		"失踪", "下落不明", "失联", "生死未卜",
		"夺舍", "肉身崩毁",
		"废除修为", "修为尽废", "丹田破碎", "经脉寸断", "根基毁损", "道基崩碎", "沦为废人",
	}
	return containsAny(combinedNew, lethalKeywords...)
}

func isMajorTierJump(key, prevVal, newVal, combinedNew string) bool {
	isRealmKey := isKeyMatch(key,
		"realm", "cultivation", "power_tier", "tier", "rank", "level",
		"境界", "修为", "修为境界", "当前境界", "实力", "实力层级", "等级",
	)
	isClassKey := isKeyMatch(key,
		"status", "public_identity", "identity", "title", "position",
		"身份", "明面身份", "门派身份", "阶层", "地位", "职位",
	)
	if !isRealmKey && !isClassKey {
		return false
	}
	if prevVal == newVal || newVal == "" {
		return false
	}

	if containsAny(combinedNew, "跨大阶", "大境界突破", "破境飞升", "跨阶跃升", "阶层跃升", "渡劫成功") {
		return true
	}

	if isRealmKey {
		prevTier := parseMajorRealmTier(prevVal)
		newTier := parseMajorRealmTier(newVal)
		if prevTier >= 0 && newTier >= 0 && prevTier != newTier {
			return true
		}
	}

	if isClassKey {
		prevClass := parseSocialClassTier(prevVal)
		newClass := parseSocialClassTier(newVal)
		if prevClass >= 0 && newClass >= 0 && prevClass != newClass {
			return true
		}
	}
	return false
}

func parseMajorRealmTier(val string) int {
	lower := strings.ToLower(strings.TrimSpace(val))
	if lower == "" {
		return -1
	}
	// 按修仙/玄幻/通用男频大境界阶梯从高到低匹配，避免子串误判（如 "大宗师" 先于 "宗师"）。
	ladders := []struct {
		tier     int
		keywords []string
	}{
		{9, []string{"大帝", "仙帝", "帝境", "武帝", "道祖", "神王", "九阶", "sss级"}},
		{8, []string{"真仙", "人仙", "地仙", "飞升", "圣境", "武圣", "圣人", "八阶", "ss级"}},
		{7, []string{"大乘", "渡劫", "尊境", "武尊", "尊者", "七阶", "s级"}},
		{6, []string{"炼虚", "合体", "洞虚", "皇境", "武皇", "六阶", "a级"}},
		{5, []string{"化神", "出窍", "分神", "王境", "武王", "五阶", "b级"}},
		{4, []string{"元婴", "灵婴", "大宗师", "四阶", "c级"}},
		{3, []string{"金丹", "结丹", "紫府", "灵丹", "宗师", "三阶", "d级"}},
		{2, []string{"筑基", "先天", "灵海", "武师", "二阶", "e级"}},
		{1, []string{"炼气", "练气", "聚气", "气海", "武者", "一阶", "f级"}},
		{0, []string{"凡人", "淬体", "锻体", "炼体", "武徒", "无修为"}},
	}
	for _, entry := range ladders {
		for _, kw := range entry.keywords {
			if strings.Contains(lower, kw) {
				return entry.tier
			}
		}
	}
	return -1
}

func parseSocialClassTier(val string) int {
	lower := strings.ToLower(strings.TrimSpace(val))
	if lower == "" {
		return -1
	}
	ladders := []struct {
		tier     int
		keywords []string
	}{
		{4, []string{"宗主", "掌门", "教主", "圣主", "皇帝", "家主", "董事长", "实控人", "控股股东", "首富"}},
		{3, []string{"长老", "峰主", "太上", "圣子", "圣女", "真传", "核心弟子", "总裁", "ceo", "副总裁", "合伙人", "将军"}},
		{2, []string{"内门", "执事", "队长", "总监", "经理", "主管", "校尉"}},
		{1, []string{"外门", "普通员工", "正式员工", "专员", "组员"}},
		{0, []string{"杂役", "记名", "实习生", "无业", "散修", "奴仆", "平民", "流民"}},
	}
	for _, entry := range ladders {
		for _, kw := range entry.keywords {
			if strings.Contains(lower, kw) {
				return entry.tier
			}
		}
	}
	return -1
}

func isOwnershipOrDestructionHighRisk(key, prevVal, newVal, combinedNew string) bool {
	if isKeyMatch(key, "owner", "holder", "controller", "ownership", "持有者", "归属", "所有者", "掌控者", "主人") {
		return prevVal != newVal && newVal != ""
	}

	isCoreAssetKey := isKeyMatch(key,
		"treasure", "natal_treasure", "item", "weapon", "artifact",
		"martial_arts", "magic_method", "core_ability", "hidden_card",
		"equity", "shares", "shareholding", "network", "assets",
		"法宝", "本命法宝", "至宝", "兵器", "道具", "功法", "神通", "股权", "股份", "底牌",
	)
	if !isCoreAssetKey {
		return false
	}
	if prevVal == newVal {
		return false
	}
	return containsAny(combinedNew,
		"易主", "易手", "被夺", "抢夺", "夺得", "转让", "剥夺", "卖掉", "卖出", "清仓", "控股",
		"损毁", "毁坏", "碎裂", "崩碎", "破碎", "毁灭", "遗失", "丢失", "吞噬", "献祭", "自毁", "报废", "注销",
	)
}

func isFactionFlipHighRisk(key, prevVal, newVal, combinedNew string) bool {
	isRelationKey := isKeyMatch(key,
		"affiliation", "faction", "relation", "camp", "allegiance", "stance",
		"阵营", "势力", "宗门", "家族", "关系", "阵营关系", "立场",
	)
	if !isRelationKey || prevVal == newVal || newVal == "" {
		return false
	}

	if containsAny(combinedNew,
		"反目", "背叛", "叛变", "叛逃", "叛宗", "反水", "倒戈", "决裂",
		"投降", "归顺", "投诚", "改投", "逐出", "除名", "通缉", "死敌", "不死不休", "宣战",
	) {
		return true
	}

	// 若阵营/宗门从具体势力切换为另一不同势力，或敌友极性反转，同样判定高危。
	prevPol := relationPolarity(prevVal)
	newPol := relationPolarity(newVal)
	if prevPol != 0 && newPol != 0 && prevPol != newPol {
		return true
	}
	if isKeyMatch(key, "affiliation", "faction", "camp", "阵营", "势力", "宗门") && prevVal != "" && newVal != "" {
		return true
	}
	return false
}

func relationPolarity(val string) int {
	if containsAny(val, "盟友", "友好", "同盟", "合作", "亲信", "归顺", "效忠", "支持", "同门") {
		return 1
	}
	if containsAny(val, "敌对", "仇敌", "死敌", "追杀", "通缉", "叛徒", "敌视", "对立", "仇视") {
		return -1
	}
	return 0
}

func isUrbanCashflowSwingHighRisk(genre domain.GenreID, key, prevVal, newVal string) bool {
	if genre != domain.GenreUrban {
		return false
	}
	isCashKey := isKeyMatch(key,
		"cash_flow", "cashflow", "cash", "balance", "funds", "assets", "net_worth", "money", "wealth",
		"资金", "可用资金", "现金", "现金流", "余额", "账户余额", "资产", "总资产", "存款",
	)
	if !isCashKey {
		return false
	}
	prevAmt, okPrev := parseMonetaryAmount(prevVal)
	newAmt, okNew := parseMonetaryAmount(newVal)
	if !okPrev || !okNew {
		return false
	}
	if prevAmt == 0 {
		return newAmt != 0
	}
	swing := math.Abs(newAmt-prevAmt) / math.Abs(prevAmt)
	return swing >= 0.30
}

// parseMonetaryAmount 从中文或英文资产描述中提取数值（单位归一化为元），如 "可用资金 128 万" -> 1280000。
func parseMonetaryAmount(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}

	if m := reArabicAmount.FindStringSubmatch(s); len(m) >= 2 {
		numStr := strings.ReplaceAll(m[1], ",", "")
		base, err := strconv.ParseFloat(numStr, 64)
		if err == nil {
			unit := ""
			if len(m) >= 3 {
				unit = strings.ToLower(strings.TrimSpace(m[2]))
			}
			return base * unitMultiplier(unit), true
		}
	}

	if m := reCNNumeral.FindStringSubmatch(s); len(m) >= 2 {
		if base, ok := parseSimpleChineseNumber(m[1]); ok {
			unit := ""
			if len(m) >= 3 {
				unit = strings.TrimSpace(m[2])
			}
			return base * unitMultiplier(unit), true
		}
	}

	return 0, false
}

func unitMultiplier(unit string) float64 {
	switch unit {
	case "亿":
		return 1e8
	case "千万":
		return 1e7
	case "百万":
		return 1e6
	case "十万":
		return 1e5
	case "万":
		return 1e4
	case "千", "k", "thousand":
		return 1e3
	case "百":
		return 1e2
	case "m", "million":
		return 1e6
	case "b", "billion":
		return 1e9
	default:
		return 1
	}
}

func parseSimpleChineseNumber(s string) (float64, bool) {
	digits := map[rune]float64{
		'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
		'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
	}
	units := map[rune]float64{
		'十': 10, '百': 100, '千': 1000,
	}
	var total, current float64
	seen := false
	for _, r := range s {
		if d, ok := digits[r]; ok {
			current = d
			seen = true
		} else if u, ok := units[r]; ok {
			if current == 0 {
				current = 1
			}
			total += current * u
			current = 0
			seen = true
		}
	}
	total += current
	return total, seen
}

func isKeyMatch(key string, targets ...string) bool {
	for _, t := range targets {
		if key == t || strings.Contains(key, t) {
			return true
		}
	}
	return false
}

func containsAny(text string, keywords ...string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
