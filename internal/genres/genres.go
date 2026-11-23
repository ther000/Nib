package genres

import (
	"fmt"
	"strings"

	"nib/internal/domain"
)

// commonAntiAIDirectives 是所有长篇题材共享的中文网文去 AI 味与移动端排版基线指令。
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
		Tagline:           "高武异界、血脉功法、宗门势力与战力体系",
		LoopCycleChapters: "3~5 章节奏",
		PacingFocus:       "建立力量层级与冲突筹码 → 暗中积蓄或遭遇压制 → 凭底牌与功法克制破局 → 战利品与势力格局变化 → 引出更高层级悬念",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "realm", Label: "当前境界", EntityKind: domain.EntityKindCharacter, Placeholder: "如：炼气九层 / 灵海境中期", Required: true, HighRisk: true},
			{Key: "physique", Label: "体质/血脉", EntityKind: domain.EntityKindCharacter, Placeholder: "如：荒古圣体 / 雷狱战血", HighRisk: true},
			{Key: "martial_arts", Label: "功法武技", EntityKind: domain.EntityKindCharacter, Placeholder: "如：大荒囚天指（圆满）"},
			{Key: "treasure", Label: "兵器/异火/至宝", EntityKind: domain.EntityKindCharacter, Placeholder: "如：陨落心炎 / 断魂重剑", HighRisk: true},
			{Key: "affiliation", Label: "宗门/家族", EntityKind: domain.EntityKindCharacter, Placeholder: "如：天渊宗外门 / 萧氏旁系", HighRisk: true},
			{Key: "injury", Label: "伤势/底牌冷却", EntityKind: domain.EntityKindCharacter, Placeholder: "如：经脉暗伤三日不可动真元"},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "入场定锚", PromptHint: "交代场景筹码、各方战力层级与人物当前状态", TargetWords: 400},
			{Stage: domain.BeatStageTarget, Title: "立靶起势", PromptHint: "对手依仗境界或背景争夺资源，立住明确冲突赌注", TargetWords: 500},
			{Stage: domain.BeatStageTurn, Title: "隐牌破局", PromptHint: "借功法克制、阵法或隐藏底牌打破信息差", TargetWords: 600},
			{Stage: domain.BeatStageBurst, Title: "正面交锋", PromptHint: "招式博弈见分晓，完成胜负结算与核心收益落袋", TargetWords: 700},
			{Stage: domain.BeatStageHook, Title: "收宝留悬", PromptHint: "战利品或余波牵出更高层级势力，末段卡住悬念", TargetWords: 300},
		},
		AntiAIDirectives: append([]string{
			"打斗写清距离、发力、招式克制与体能/灵力消耗，拒绝空泛的“毁天灭地”“恐怖如斯”堆砌形容词。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"严禁战力无故跨大阶碾压且毫无代价；每次越阶必须依托功法克制、阵法地形、底牌消耗或信息差。",
			"严禁反派全员降智送人头，对手行动必须符合其宗门立场、利益盘算与基本警惕心。",
			"战斗描写重招式博弈与结果落袋，每场硬仗后必须交代伤势、消耗或实质战利品变化。",
		},
		SystemDirective: "你正在撰写一部玄幻长篇。确保战力体系严谨自洽、越阶有逻辑代价与底牌支撑；角色行动符合各自立场与利益动机；动作描写狠准干脆，拒绝空泛渲染与 AI 套话。",
	},
	{
		ID:                domain.GenreXianxia,
		Name:              "仙侠",
		Tagline:           "修真问道、寿元资源、宗门博弈与因果筹算",
		LoopCycleChapters: "4~6 章节奏",
		PacingFocus:       "得机缘线索或寿元危局 → 多方试探博弈 → 借阵法情报或破境抉择破局 → 利益兑现与善后 → 因果余波留钩",
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
			{Stage: domain.BeatStageTurn, Title: "借势破局", PromptHint: "借谋算、符阵、情报差或隐匿手段扭转局势", TargetWords: 650},
			{Stage: domain.BeatStageHarvest, Title: "兑现善后", PromptHint: "收取灵药法宝、清点所得并处理追踪隐患", TargetWords: 550},
			{Stage: domain.BeatStageHook, Title: "因果余波", PromptHint: "遗物异动、命牌碎裂或高阶神识掠过，留下紧迫钩子", TargetWords: 300},
		},
		AntiAIDirectives: append([]string{
			"语感凝练克制，带适度修真韵味但不晦涩；除非作者风格标签明确要求轻松吐槽，否则避免现代网络热词出戏。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"修为境界、寿元消耗、丹药法宝品阶与灵石收支前后一致。",
			"人物行事基调保持一贯性，切忌性格左右横跳。",
			"禁止大段道经式玄学空谈与章末鸡汤感悟，道心与修为提升须通过具体事件与抉择体现。",
		},
		SystemDirective: "你正在撰写一部仙侠修真长篇。注重寿元资源约束、多方利益试探与因果逻辑；遣词凝练有修真质感，杜绝空洞玄学说教与高频 AI 腔。",
	},
	{
		ID:                domain.GenreUrban,
		Name:              "都市",
		Tagline:           "现代职场商战、现实痛点、身份错位与社会博弈",
		LoopCycleChapters: "2~3 章节奏",
		PacingFocus:       "现实痛点与利益冲突 → 信息差或关键能力介入 → 用硬结果兑现筹码 → 关系与身份位势变化 → 新局切入",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "cash_flow", Label: "可用资金/资产", EntityKind: domain.EntityKindCharacter, Placeholder: "如：流动资金 128 万 / 持星海科技 15% 股权", Required: true, HighRisk: true},
			{Key: "public_identity", Label: "明面身份", EntityKind: domain.EntityKindCharacter, Placeholder: "如：普通实习生 / 刚离职项目经理", Required: true, HighRisk: true},
			{Key: "hidden_card", Label: "底牌/马甲/系统", EntityKind: domain.EntityKindCharacter, Placeholder: "如：匿名量化账户 / 商业情报推送系统", HighRisk: true},
			{Key: "network", Label: "人脉/股权/后台", EntityKind: domain.EntityKindCharacter, Placeholder: "如：城投副总人情 / 顶奢私行黑卡经理", HighRisk: true},
			{Key: "cooldown", Label: "金手指规则与限制", EntityKind: domain.EntityKindSystem, Placeholder: "如：每日仅限刷新 1 条 72 小时内情报", HighRisk: true},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageTarget, Title: "现实痛点", PromptHint: "交代资金/职场/关系压力，立住现实利益冲突", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "局势受阻", PromptHint: "受限于明面身份或资源暂时受压，积蓄反转筹码", TargetWords: 450},
			{Stage: domain.BeatStageTurn, Title: "信息差反转", PromptHint: "关键情报或能力落地，主角抢先卡住核心资源或人脉", TargetWords: 600},
			{Stage: domain.BeatStageBurst, Title: "硬核兑现", PromptHint: "用合同、账单、行业地位或结果当场兑现收益，旁观者反应克制真实", TargetWords: 650},
			{Stage: domain.BeatStageHook, Title: "错位新局", PromptHint: "一通意外来电、身份暴露边缘或更大盘子入局，拉起下章期待", TargetWords: 350},
		},
		AntiAIDirectives: append([]string{
			"对话凌厉生活化，善用具体行业细节、账目数字与生活物件建立真实感，避免浮夸霸总腔。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"消费金额、账户余额、资产估值、股权比例与时间线必须全程算术自洽，单笔大额变动交代合理来路。",
			"对手行动必须有清晰的现实商业/面子/利益动机，严禁无脑当街挑衅送脸。",
			"严禁“区区几亿随手赏了”“天凉王破”式空洞尬词，旁观者反应通过停顿、称呼变化等克制细节体现。",
		},
		SystemDirective: "你正在撰写一部高真实感的都市长篇。痛点扎实、金额与时间线算术严谨、对手有真实利益动机，靠硬结果与克制生活细节推进戏剧张力，杜绝浮夸尬词与 AI 套话。",
	},
	{
		ID:                domain.GenreHistory,
		Name:              "历史架空",
		Tagline:           "朝堂权谋、乱世争霸、军政钱粮与时代变局",
		LoopCycleChapters: "3~5 章节奏",
		PacingFocus:       "时局危难或朝野暗流 → 派系倾轧与资源掣肘 → 借制度/军略/信息差落子破局 → 威望兵权钱粮落袋 → 牵动更大格局",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "official_rank", Label: "官职/爵位", EntityKind: domain.EntityKindCharacter, Placeholder: "如：七品县令 / 镇北侯世子", Required: true, HighRisk: true},
			{Key: "faction_camp", Label: "所属派系/阵营", EntityKind: domain.EntityKindCharacter, Placeholder: "如：东宫旧部 / 江南士族", HighRisk: true},
			{Key: "military_finance", Label: "兵权/钱粮", EntityKind: domain.EntityKindCharacter, Placeholder: "如：府兵三千、存粮可供两月", HighRisk: true},
			{Key: "origin", Label: "出身/门第", EntityKind: domain.EntityKindCharacter, Placeholder: "如：寒门进士 / 边军悍将"},
			{Key: "leverage", Label: "核心筹码/政绩", EntityKind: domain.EntityKindCharacter, Placeholder: "如：手握盐铁密账 / 改良火器图纸", HighRisk: true},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "时局入场", PromptHint: "交代年号地界、朝野局势与当前钱粮军政处境", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "派系掣肘", PromptHint: "政敌设局、粮饷受卡或外患压境，亮明政治代价", TargetWords: 500},
			{Stage: domain.BeatStageTurn, Title: "落子破局", PromptHint: "以制度漏洞、谋略、技术或人心博弈反客为主", TargetWords: 650},
			{Stage: domain.BeatStageHarvest, Title: "定局收权", PromptHint: "完成人事、钱粮或战局结算，实质提升位置与威望", TargetWords: 600},
			{Stage: domain.BeatStageHook, Title: "暗流再起", PromptHint: "一封急报、圣旨异动或更高层派系注视，留足悬念", TargetWords: 300},
		},
		AntiAIDirectives: append([]string{
			"台词兼顾时代语境与现代可读性，器物、官制、称谓前后统一，避免现代管理学术语生硬出戏。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"行军距离、粮草补给、季节气候与官场程序须符合基本历史常识与时代约束。",
			"古人自有其利益逻辑与生存智慧，严禁为衬托主角而把朝臣将领写成无脑蠢材。",
			"技术或制度改革须交代原料、工匠、阻力与落地周期，禁止凭空手搓现代工业。",
		},
		SystemDirective: "你正在撰写一部历史架空长篇。重朝堂与地方的真实利益结构、钱粮兵制约束与人心博弈；语感沉实有时代质感，杜绝违和现代口水词与 AI 套话。",
	},
	{
		ID:                domain.GenreSciFi,
		Name:              "科幻末世",
		Tagline:           "星际赛博、废土求生、科技演进与文明危机",
		LoopCycleChapters: "3~5 章节奏",
		PacingFocus:       "极端环境或技术危机 → 资源匮乏与规则压迫 → 靠硬核推演/改装/战术逆转 → 获取关键物资与权限 → 揭开更深层真相",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "tier", Label: "基因/强化阶位", EntityKind: domain.EntityKindCharacter, Placeholder: "如：二阶义体改造 / A级精神力", Required: true, HighRisk: true},
			{Key: "equipment", Label: "核心武装/载具", EntityKind: domain.EntityKindCharacter, Placeholder: "如：脉冲步枪 MK-II / 破晓号护卫舰", HighRisk: true},
			{Key: "faction", Label: "所属阵营/避难所", EntityKind: domain.EntityKindCharacter, Placeholder: "如：第七穹顶安全局 / 荒野拾荒者", HighRisk: true},
			{Key: "resources", Label: "物资/能源/算力", EntityKind: domain.EntityKindCharacter, Placeholder: "如：高纯度能源电池 3 组 / 信用点 4.2 万", HighRisk: true},
			{Key: "condition", Label: "畸变/耐久/状态", EntityKind: domain.EntityKindCharacter, Placeholder: "如：辐射值 18% / 义体过载冷却中"},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "环境定锚", PromptHint: "交代辐射/氧量/能源/坐标等硬核约束与任务目标", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "危局迫近", PromptHint: "遭遇异变体、公司围剿或系统故障，资源捉襟见肘", TargetWords: 500},
			{Stage: domain.BeatStageTurn, Title: "技术破局", PromptHint: "利用物理规律、设备改装、协议漏洞或战术配合反制", TargetWords: 650},
			{Stage: domain.BeatStageHarvest, Title: "回收升级", PromptHint: "清点核心元件、数据或生存物资，交代损耗与代价", TargetWords: 550},
			{Stage: domain.BeatStageHook, Title: "异常信号", PromptHint: "残缺日志、未知频段或更高维度威胁浮现，卡住悬念", TargetWords: 350},
		},
		AntiAIDirectives: append([]string{
			"善用具体物理细节、读数、材质与环境声响营造临场感，拒绝空洞堆砌伪科学名词。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"能源、弹药、氧气、通讯延迟与物理法则在全书设定内保持严格自洽。",
			"科技跃迁与求生反杀必须有可追溯的前置零件、数据或逻辑伏笔。",
			"各阵营与人工智能的行为符合其底层协议与生存/商业利益，不强行降智。",
		},
		SystemDirective: "你正在撰写一部科幻/末世长篇。坚守设定内物理与资源硬约束，用具体感官细节与战术推演制造紧张感，杜绝伪科学水词与 AI 套话。",
	},
	{
		ID:                domain.GenreMystery,
		Name:              "悬疑灵异",
		Tagline:           "诡异规则、案件推理、信息拼图与心理博弈",
		LoopCycleChapters: "2~4 章节奏",
		PacingFocus:       "异常现象或命案切入 → 线索矛盾与死亡规则逼近 → 捕捉细节破绽完成推理/反制 → 局部真相揭晓 → 抛出更深层谜团",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "identity", Label: "明面身份", EntityKind: domain.EntityKindCharacter, Placeholder: "如：法医助理 / 民俗调查员", Required: true, HighRisk: true},
			{Key: "motive", Label: "核心动机/秘密", EntityKind: domain.EntityKindCharacter, Placeholder: "如：追查十年前失踪案真相", HighRisk: true},
			{Key: "clues", Label: "掌握线索/物证", EntityKind: domain.EntityKindCharacter, Placeholder: "如：半张烧焦的登记簿、录音带", HighRisk: true},
			{Key: "anomaly", Label: "关联异象/规则", EntityKind: domain.EntityKindCharacter, Placeholder: "如：午夜三点禁照镜子", HighRisk: true},
			{Key: "status", Label: "嫌疑/理智状态", EntityKind: domain.EntityKindCharacter, Placeholder: "如：轻度污染 / 已被警方列为目击者"},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "异象切入", PromptHint: "精准定锚时间地点与第一处反常细节，建立压迫氛围", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "迷雾收紧", PromptHint: "线索指向矛盾、规则触发或退路被封，推高危机感", TargetWords: 550},
			{Stage: domain.BeatStageTurn, Title: "细节破局", PromptHint: "通过被忽略的物证、时间差或规则盲区识破骗局/生路", TargetWords: 650},
			{Stage: domain.BeatStageHarvest, Title: "拼图归位", PromptHint: "阶段真相落地并回收伏笔，同时交代付出的代价", TargetWords: 500},
			{Stage: domain.BeatStageHook, Title: "深层反转", PromptHint: "新线索推翻既有认知或暴露幕后黑手一角，末段留钩", TargetWords: 350},
		},
		AntiAIDirectives: append([]string{
			"用具体可验证的物证、时间线、微动作与环境细节制造寒意与悬疑，严禁反复直白宣称“诡异”“毛骨悚然”。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"所有推理结论与规则逃生解法必须基于前文已公平埋下的线索，严禁凭空机械降神。",
			"时间线、空间布局、不在场证明与诡异规则条件必须前后严密自洽。",
			"悬念靠信息差与反常细节层层剥开，禁止靠角色故意隐瞒关键信息硬拖节奏。",
		},
		SystemDirective: "你正在撰写一部悬疑/灵异长篇。严守线索公平与时间线自洽，用克制精准的环境与行为细节营造压迫感与推理快感，杜绝廉价惊悚形容词与 AI 套话。",
	},
	{
		ID:                domain.GenreGame,
		Name:              "游戏无限",
		Tagline:           "副本闯关、职业机制、数值构筑与规则博弈",
		LoopCycleChapters: "2~4 章节奏",
		PacingFocus:       "进入副本与机制亮明 → 数值或规则劣势承压 → 靠机制联动/隐藏机制破局 → 结算奖励与 Build 成长 → 下个副本或阵营异动",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "class_seq", Label: "职业/序列", EntityKind: domain.EntityKindCharacter, Placeholder: "如：机械师 Lv.18 / 序列7·魔术师", Required: true, HighRisk: true},
			{Key: "points", Label: "等级/积分", EntityKind: domain.EntityKindCharacter, Placeholder: "如：3,400 生存点 / 排位黄金 II", HighRisk: true},
			{Key: "skills", Label: "核心技能/词条", EntityKind: domain.EntityKindCharacter, Placeholder: "如：镜像置换（CD 30s）"},
			{Key: "gear", Label: "装备/关键道具", EntityKind: domain.EntityKindCharacter, Placeholder: "如：残缺的怀表（稀有）", HighRisk: true},
			{Key: "guild", Label: "公会/副本进度", EntityKind: domain.EntityKindCharacter, Placeholder: "如：灰烬小队 / 血月疗养院 60%", HighRisk: true},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "规则入场", PromptHint: "明确副本主线任务、失败惩罚与队伍配置", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "机制承压", PromptHint: "遭遇 Boss 机制杀、敌对玩家算计或信息差陷阱", TargetWords: 500},
			{Stage: domain.BeatStageTurn, Title: "机制反制", PromptHint: "通过技能组合、道具联动或卡规则漏洞逆转局势", TargetWords: 650},
			{Stage: domain.BeatStageHarvest, Title: "结算构筑", PromptHint: "击破阶段目标、落袋词条/道具并明确数值成长", TargetWords: 550},
			{Stage: domain.BeatStageHook, Title: "隐藏异动", PromptHint: "触发隐藏支线、世界公告或新对手锁定，末段留钩", TargetWords: 350},
		},
		AntiAIDirectives: append([]string{
			"数值与系统提示克制精炼，融入动作与战术决策中，禁止大段刷屏式属性面板水字数。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"技能冷却、消耗、道具次数、积分增减与副本规则必须前后严格自洽。",
			"破关靠机制理解、配合与临场决策，严禁临时篡改规则或无限叠无代价外挂。",
			"其他玩家与 NPC 具备独立目标与正常博弈意识，不强行降智送装备。",
		},
		SystemDirective: "你正在撰写一部游戏/无限流长篇。注重规则机制清晰、数值构筑自洽与高密度战术博弈，克制系统面板篇幅，杜绝水字数与 AI 套话。",
	},
	{
		ID:                domain.GenreGeneral,
		Name:              "通用长篇",
		Tagline:           "不限世界观的标准长篇结构，适配轻小说、言情、奇幻与跨界题材",
		LoopCycleChapters: "3~5 章节奏",
		PacingFocus:       "建场定锚 → 阻力推高张力 → 转折破局 → 阶段情绪或利益兑现 → 留钩推进下章",
		EntityFields: []domain.GenreFieldSpec{
			{Key: "power_tier", Label: "实力/地位层级", EntityKind: domain.EntityKindCharacter, Placeholder: "如：二阶异能者 / 学院首席 / 部门总监", Required: true, HighRisk: true},
			{Key: "core_ability", Label: "核心能力/专长", EntityKind: domain.EntityKindCharacter, Placeholder: "如：时间回溯 10 秒 / 机械重构"},
			{Key: "assets", Label: "关键资源/物品", EntityKind: domain.EntityKindCharacter, Placeholder: "如：家族信物 / 秘密账本", HighRisk: true},
			{Key: "relation", Label: "阵营/核心关系", EntityKind: domain.EntityKindCharacter, Placeholder: "如：第七避难所通缉 / 暗盟合作者", HighRisk: true},
			{Key: "status_flag", Label: "当前状态标记", EntityKind: domain.EntityKindCharacter, Placeholder: "如：轻伤、身份伪装中、信任度上升"},
		},
		DefaultBeats: []domain.BeatTemplate{
			{Stage: domain.BeatStageSetup, Title: "起手建场", PromptHint: "快速定锚时间地点、当前任务目标与核心张力", TargetWords: 450},
			{Stage: domain.BeatStageSuppress, Title: "蓄压受阻", PromptHint: "遭遇硬阻力、资源短缺或对手优势压制，推高危机张力", TargetWords: 500},
			{Stage: domain.BeatStageTurn, Title: "破局转折", PromptHint: "抓到关键漏洞、规则空子或情绪契机，夺回主导权", TargetWords: 600},
			{Stage: domain.BeatStageBurst, Title: "高潮兑现", PromptHint: "正面交锋、关键告白或计谋收口，完成阶段目标", TargetWords: 650},
			{Stage: domain.BeatStageHook, Title: "章末留钩", PromptHint: "收束旧冲突同时抛出新异常、抉择或悬念", TargetWords: 300},
		},
		AntiAIDirectives: append([]string{
			"保持叙事高密度推进：每段必须带来行动变化、信息增量或关系位移，绝不原地水字数。",
		}, commonAntiAIDirectives...),
		AntiTropeRules: []string{
			"拒绝无意义反复心理复盘与流水账过场，冲突必须有明确筹码或情感支点。",
			"配角与对手具备独立目标与逻辑一致性，不强行降智配合主角演出。",
			"规则、时间线与资源消耗前后一致，章节末尾自然兑现悬念钩子。",
		},
		SystemDirective: "你正在撰写一部强节奏、强逻辑的长篇小说。严格执行单章节拍推进与手机端短段排版，动作与对话优先于抽象总结，角色逻辑自洽，杜绝高频 AI 套话与鸡汤式结尾。",
	},
}

// List 返回全部 8 套内置题材基底的深拷贝切片。
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

// GetOrDefault 按题材 ID 查询预设，未命中时回退至通用长篇（general）。
func GetOrDefault(id domain.GenreID) domain.GenrePreset {
	if p, ok := Get(id); ok {
		return p
	}
	p, _ := Get(domain.GenreGeneral)
	return p
}

// DisplayName 返回题材的可读名称（内置题材返回中文名，自定义题材返回原文）。
func DisplayName(id domain.GenreID) string {
	if p, ok := Get(id); ok {
		return p.Name
	}
	trimmed := strings.TrimSpace(string(id))
	if trimmed != "" {
		return trimmed
	}
	return "通用长篇"
}

// PromptBlock 将指定题材基底格式化为供上下文编译器 P0 槽位直接注入的中文规范块。
func PromptBlock(id domain.GenreID) string {
	p, isBuiltin := Get(id)
	if !isBuiltin {
		p = GetOrDefault(domain.GenreGeneral)
	}
	displayName := p.Name
	if !isBuiltin && strings.TrimSpace(string(id)) != "" {
		displayName = strings.TrimSpace(string(id))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "【题材基底：%s】\n", displayName)
	fmt.Fprintf(&b, "总体定位：%s\n", p.SystemDirective)
	fmt.Fprintf(&b, "叙事参考：%s\n", p.PacingFocus)

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
