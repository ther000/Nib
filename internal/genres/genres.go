package genres

import (
	"fmt"
	"strings"

	"nib/internal/domain"
)

// commonAntiAIDirectives 是所有男频题材共享的中文网文去 AI 味与移动端排版基线指令。
var commonAntiAIDirectives = []string{
	"严禁使用高频 AI 套话：嘴角勾起、一抹弧度、眼神微眯、眼中闪过一抹精光/寒芒/玩味、空气仿佛凝固、心中涌起一股、倒吸一口凉气、令人窒息的。",
	"严禁章末鸡汤升华与套路总结（如“他知道真正的考验才刚刚开始”“命运的齿轮开始转动”），必须用具体动作、对话或悬念细节收尾。",
	"避免同段内连续三句结构对称的 AI 排比句；控制“不是……而是……”“不由得”“缓缓”高频复现。",
	"严格适配手机端网文阅读节奏：单段不超过 120~140 汉字，多用具体动作、利益交换与短句台词外化情绪，禁止大段空洞心理内耗。",
}

var registry = []domain.GenrePreset{
	{
		ID:                domain.GenreXuanhuan,
		Name:              "玄幻",
		Tagline:           "高武异界、血脉功法与越阶反杀爽感循环",
		LoopCycleChapters: "3~5 章小循环",
		PacingFocus:       "立嘲讽靶 → 暗中积蓄/被轻视 → 关键场域展露底牌/越阶反杀 → 收获资源与旁观震惊 → 引出更高层级势力",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "realm", Label: "当前境界", EntityKind: domain.EntityKindCharacter, Placeholder: "如：炼气九层 / 灵海境中期", Required: true, HighRisk: true},
			{Key: "physique", Label: "体质/血脉", EntityKind: domain.EntityKindCharacter, Placeholder: "如：荒古圣体 / 雷狱战血", HighRisk: true},
			{Key: "martial_arts", Label: "功法武技", EntityKind: domain.EntityKindCharacter, Placeholder: "如：大荒囚天指（圆满）"},
			{Key: "treasure", Label: "兵器/异火/至宝", EntityKind: domain.EntityKindCharacter, Placeholder: "如：陨落心炎 / 断魂重剑", HighRisk: true},
			{Key: "affiliation", Label: "宗门/家族", EntityKind: domain.EntityKindCharacter, Placeholder: "如：天渊宗外门 / 萧氏旁系", HighRisk: true},
			{Key: "injury", Label: "伤势/底牌冷却", EntityKind: domain.EntityKindCharacter, Placeholder: "如：经脉暗伤三日不可动真元"},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "入场定锚", PromptHint: "交代场景筹码、各方战力层级与主角当前隐忍状态", TargetWords: 400},
			{Stage: domain.BeatStageTarget, Title: "立靶蓄怨", PromptHint: "对手依仗境界/背景抢夺利益或出言轻贱，立住明确冲突赌注", TargetWords: 500},
			{Stage: domain.BeatStageTurn, Title: "隐牌破局", PromptHint: "借功法克制、阵法或隐藏底牌撕开对手防御，打破信息差", TargetWords: 600},
			{Stage: domain.BeatStageBurst, Title: "越阶反杀", PromptHint: "招式博弈见分晓，正面击溃强敌并当场落袋核心资源/震慑旁观者", TargetWords: 700},
			{Stage: domain.BeatStageHook, Title: "收宝留悬", PromptHint: "战利品牵出更高层级势力或新危机，最后 200 字卡住强悬念", TargetWords: 300},
		},
		AntiAIDirectives: append([]string{
			"打斗写清距离、发力、招式克制与体能/灵力消耗，拒绝空泛的“毁天灭地”“恐怖如斯”堆砌形容词。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"严禁战力无故跨大阶碾压且毫无代价；每次越阶必须依托功法克制、阵法地形、底牌消耗或信息差。",
			"严禁反派全员降智送人头，对手行动必须符合其宗门立场、利益盘算与基本警惕心。",
			"战斗描写重招式博弈与结果落袋，每场硬仗后必须交代伤势、消耗或实质战利品变化。",
		},
		SystemDirective: "你正在撰写一部硬核男频玄幻长篇。坚持“立靶—蓄压—破局—反杀—落袋留钩”节奏，战力体系严谨自洽，越阶必有逻辑代价与底牌支撑；反派保持正常智商与利益动机；动作描写狠准干脆，拒绝散文化空泛渲染与 AI 套话。",
	},
	{
		ID:                domain.GenreXianxia,
		Name:              "仙侠",
		Tagline:           "修真问道、寿元因果、宗门博弈与以小博大",
		LoopCycleChapters: "4~6 章布局循环",
		PacingFocus:       "得机缘线索/危机逼近 → 多方试探博弈 → 关键节点以小博大/破境劫数 → 利益兑现与抹除隐患 → 留更高因果悬念",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "cultivation", Label: "修为境界", EntityKind: domain.EntityKindCharacter, Placeholder: "如：筑基初期 / 金丹三转", Required: true, HighRisk: true},
			{Key: "spirit_root", Label: "灵根/道基", EntityKind: domain.EntityKindCharacter, Placeholder: "如：伪灵根 / 剑道天基", HighRisk: true},
			{Key: "magic_method", Label: "神通/秘术", EntityKind: domain.EntityKindCharacter, Placeholder: "如：敛息诀、小五行剑阵"},
			{Key: "natal_treasure", Label: "本命法宝", EntityKind: domain.EntityKindCharacter, Placeholder: "如：青冥飞剑（中品灵器）", HighRisk: true},
			{Key: "lifespan", Label: "寿元/因果线", EntityKind: domain.EntityKindCharacter, Placeholder: "如：剩余寿元八十二载 / 欠丹霞谷因果", HighRisk: true},
			{Key: "status", Label: "门派身份/洞府", EntityKind: domain.EntityKindCharacter, Placeholder: "如：青云宗杂役峰执事 / 乙字三号洞府", HighRisk: true},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "机缘与危局", PromptHint: "交代灵石/寿元/破境压力，引出机缘线索或宗门暗流", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "多方试探", PromptHint: "各路修士暗中角力、阵法禁制或修为压制形成逼仄局面", TargetWords: 550},
			{Stage: domain.BeatStageTurn, Title: "借势破局", PromptHint: "借谋算、符阵、情报差或隐匿手段以小博大逆转胜负", TargetWords: 650},
			{Stage: domain.BeatStageHarvest, Title: "兑现抹痕", PromptHint: "收取灵药法宝、清点破境所得，并冷静抹除追踪隐患", TargetWords: 550},
			{Stage: domain.BeatStageHook, Title: "因果余波", PromptHint: "遗物异动、命牌碎裂或更高阶修士神识掠过，留下紧迫钩子", TargetWords: 300},
		},
		AntiAIDirectives: append([]string{
			"语感沉稳凝练，带适度古风修真韵味但不晦涩；严禁冒出现代网络热词与口水梗破坏沉浸感。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"禁用现代网络词汇与白话口水词出戏（如“破防”“小丑”“格局打开”等现代梗）。",
			"明确遵循人物行事基调（“老六苟道”重藏拙留后手，“杀伐果断”重斩草除根），切忌性格左右横跳。",
			"禁止大段道经式玄学空谈与章末鸡汤感悟，道心与修为提升须通过具体事件与抉择体现。",
		},
		SystemDirective: "你正在撰写一部古典冷峻的男频仙侠长篇。叙事重寿元资源约束、多方利益试探、以小博大与因果后患；遣词凝练有修真古意，严禁现代网络梗、空洞玄学说教与高频 AI 腔。",
	},
	{
		ID:                domain.GenreUrban,
		Name:              "都市",
		Tagline:           "现实痛点、信息差金手指、精准变现与身份错位",
		LoopCycleChapters: "2~3 章快节奏循环",
		PacingFocus:       "现实痛点压抑/被轻贱 → 信息差或金手指触发反转 → 利益/尊严精准打脸变现 → 旁观者克制震惊与身份错位 → 新事件切入",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "cash_flow", Label: "可用资金/资产", EntityKind: domain.EntityKindCharacter, Placeholder: "如：流动资金 128 万 / 持星海科技 15% 股权", Required: true, HighRisk: true},
			{Key: "public_identity", Label: "明面身份", EntityKind: domain.EntityKindCharacter, Placeholder: "如：普通实习生 / 刚离职项目经理", Required: true, HighRisk: true},
			{Key: "hidden_card", Label: "底牌/马甲/系统", EntityKind: domain.EntityKindCharacter, Placeholder: "如：匿名量化账户 / 商业情报推送系统", HighRisk: true},
			{Key: "network", Label: "人脉/股权/后台", EntityKind: domain.EntityKindCharacter, Placeholder: "如：城投副总人情 / 顶奢私行黑卡经理", HighRisk: true},
			{Key: "cooldown", Label: "金手指规则与限制", EntityKind: domain.EntityKindSystem, Placeholder: "如：每日仅限刷新 1 条 72 小时内情报", HighRisk: true},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageTarget, Title: "现实痛点", PromptHint: "交代资金/职场/关系压力，对手出于现实利益踩低捧高", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "轻贱受阻", PromptHint: "受限于明面身份暂时被压一头，积蓄反转筹码与时间期限", TargetWords: 450},
			{Stage: domain.BeatStageTurn, Title: "信息差反转", PromptHint: "金手指或关键情报精准落地，主角抢先卡住核心资源或关键人脉", TargetWords: 600},
			{Stage: domain.BeatStageBurst, Title: "精准变现", PromptHint: "用合同、账单、行业地位或结果当场完成利益与尊严兑现，旁观者反应克制真实", TargetWords: 650},
			{Stage: domain.BeatStageHook, Title: "错位新局", PromptHint: "一通意外来电、身份暴露边缘或更大盘子入局，迅速拉起下章期待", TargetWords: 350},
		},
		AntiAIDirectives: append([]string{
			"对话凌厉生活化，善用具体行业细节、账目数字与生活物件建立真实感，避免浮夸霸总腔。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"消费金额、账户余额、资产估值、股权比例与时间线必须全程算术自洽，单笔大额变动交代合理来路。",
			"反派行动必须有清晰的现实商业/面子/利益动机，严禁无脑当街挑衅送脸。",
			"严禁“区区几亿随手赏了”“天凉王破”式空洞装逼尬词，旁观者震惊用克制细节（停顿、换称呼、改态度）体现。",
		},
		SystemDirective: "你正在撰写一部快节奏、高真实感的男频都市长篇。坚持 2~3 章一轮现实爽点兑现：痛点扎实、金额与时间线算术严谨、对手有真实利益动机、装逼打脸靠硬结果与克制细节而非尬词浮夸，杜绝 AI 套话。",
	},
	{
		ID:                domain.GenreGeneral,
		Name:              "通用男频",
		Tagline:           "适配历史架空、末世废土、无限流与硬核科幻的标准节奏引擎",
		LoopCycleChapters: "3~5 章标准循环",
		PacingFocus:       "标准 5 阶段节拍模板：建场立靶 → 蓄压受阻 → 信息差反转 → 正面爆发 → 落袋留钩",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "power_tier", Label: "实力层级", EntityKind: domain.EntityKindCharacter, Placeholder: "如：二阶异能者 / 校尉 / B级轮回者", Required: true, HighRisk: true},
			{Key: "core_ability", Label: "核心能力", EntityKind: domain.EntityKindCharacter, Placeholder: "如：时间回溯 10 秒 / 机械重构"},
			{Key: "assets", Label: "关键资源/装备", EntityKind: domain.EntityKindCharacter, Placeholder: "如：3000 积分 / 军用外骨骼", HighRisk: true},
			{Key: "relation", Label: "阵营关系", EntityKind: domain.EntityKindCharacter, Placeholder: "如：第七避难所通缉 / 暗盟合作者", HighRisk: true},
			{Key: "status_flag", Label: "当前状态标记", EntityKind: domain.EntityKindCharacter, Placeholder: "如：轻伤、弹药见底、身份伪装中"},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "起手建场", PromptHint: "快速定锚时间地点、当前任务目标与核心风险", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "蓄压受阻", PromptHint: "遭遇硬阻力、资源短缺或敌方优势压制，推高危机张力", TargetWords: 500},
			{Stage: domain.BeatStageTurn, Title: "破局转折", PromptHint: "抓到关键漏洞、规则空子或信息差，主动夺回局势主导权", TargetWords: 600},
			{Stage: domain.BeatStageBurst, Title: "高潮兑现", PromptHint: "正面交锋或计谋收口，完成目标并落袋明确收益", TargetWords: 650},
			{Stage: domain.BeatStageHook, Title: "章末留钩", PromptHint: "收束旧冲突同时抛出新异常、倒计时或更高层威胁", TargetWords: 300},
		},
		AntiAIDirectives: append([]string{
			"保持叙事高密度推进：每段必须带来行动变化、信息增量或局势位移，绝不原地水字数。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"通用中文男频防注水规范：拒绝无意义反复心理复盘与流水账过场，冲突必须有明确筹码。",
			"配角与反派具备独立目标与逻辑一致性，不强行降智配合主角演出。",
			"规则与资源消耗前后一致，章节末尾 200 字必须兑现设定的悬念钩子。",
		},
		SystemDirective: "你正在撰写一部强节奏、强逻辑的男频长篇网文。严格执行单章节拍推进与手机端短段排版，动作与对话优先于抽象总结，角色智商在线、资源逻辑自洽，杜绝高频 AI 套话与鸡汤式结尾。",
	},
}

// List 返回全部 4 套内置男频题材包的深拷贝切片（按 xuanhuan/xianxia/urban/general 固定顺序）。
func List() []domain.GenrePreset {
	out := make([]domain.GenrePreset, len(registry))
	for i, preset := range registry {
		out[i] = clonePreset(preset)
	}
	return out
}

// Get 按题材 ID 查询预设题材包（返回深拷贝，避免调用方修改全局配置）。
func Get(id domain.GenreID) (domain.GenrePreset, bool) {
	for _, preset := range registry {
		if preset.ID == id {
			return clonePreset(preset), true
		}
	}
	return domain.GenrePreset{}, false
}

// GetOrDefault 按题材 ID 查询预设，未命中时回退至通用男频（general）。
func GetOrDefault(id domain.GenreID) domain.GenrePreset {
	if p, ok := Get(id); ok {
		return p
	}
	p, _ := Get(domain.GenreGeneral)
	return p
}

// PromptBlock 将指定题材包格式化为供上下文编译器 P0 槽位直接注入的中文规范块。
func PromptBlock(id domain.GenreID) string {
	p := GetOrDefault(id)
	var b strings.Builder
	fmt.Fprintf(&b, "【题材包：%s（%s）】\n", p.Name, p.LoopCycleChapters)
	fmt.Fprintf(&b, "总体定位：%s\n", p.SystemDirective)
	fmt.Fprintf(&b, "节奏侧重：%s\n", p.PacingFocus)

	if len(p.AntiTropeRules) > 0 {
		b.WriteString("防毒与逻辑铁线：\n")
		for _, r := range p.AntiTropeRules {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	if len(p.AntiAIDirectives) > 0 {
		b.WriteString("去 AI 味与排版要求：\n")
		for _, d := range p.AntiAIDirectives {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	}
	return strings.TrimSpace(b.String())
}

func clonePreset(src domain.GenrePreset) domain.GenrePreset {
	dst := src
	dst.EntityFields = make([]domain.GenreFieldSpec, len(src.EntityFields))
	copy(dst.EntityFields, src.EntityFields)
	dst.DefaultBeats = make([]domain.BeatTemplate, len(src.DefaultBeats))
	copy(dst.DefaultBeats, src.DefaultBeats)
	dst.AntiAIDirectives = make([]string, len(src.AntiAIDirectives))
	copy(dst.AntiAIDirectives, src.AntiAIDirectives)
	dst.AntiTropeRules = make([]string, len(src.AntiTropeRules))
	copy(dst.AntiTropeRules, src.AntiTropeRules)
	return dst
}
