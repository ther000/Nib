package linter

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"nib/internal/domain"
)

// 20 段典型中文网文 AI 腔病灶样本（全量覆盖 docs/03-core-engines.md §3.2 的 A/B/C 三大类规则）。
var typicalAIismSamples = []struct {
	name         string
	text         string
	wantCategory domain.DeaiCategory
	wantSubstr   string
}{
	{
		name:         "01 嘴角勾起",
		text:         "林渊看着走来的执事，嘴角勾起一丝冷意。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "嘴角勾起",
	},
	{
		name:         "02 一抹弧度",
		text:         "他负手立于青石台上，唇边掠过一抹弧度。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "一抹弧度",
	},
	{
		name:         "03 眼神微眯",
		text:         "王煞眼神微眯，死死盯住对方袖中的古朴铁剑。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "眼神微眯",
	},
	{
		name:         "04 眼中闪过精光",
		text:         "老者闻言眼中闪过一抹精光，立刻抬手封住退路。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "眼中闪过一抹精光",
	},
	{
		name:         "05 眼中闪过寒芒",
		text:         "黑衣修士眼中闪过一抹寒芒，掌心阴雷瞬间炸开。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "眼中闪过一抹寒芒",
	},
	{
		name:         "06 空气仿佛凝固",
		text:         "两人气机对撞的瞬间，整座大殿的空气仿佛凝固了一般。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "空气仿佛凝固",
	},
	{
		name:         "07 心中涌起一股",
		text:         "望着石碑上的古篆，他心中涌起一股难以言喻的悸动。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "心中涌起一股",
	},
	{
		name:         "08 倒吸一口凉气",
		text:         "四周旁观的外门弟子纷纷倒吸一口凉气，不敢相信自己的眼睛。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "倒吸一口凉气",
	},
	{
		name:         "09 这一刻",
		text:         "剑锋落下的瞬间，这一刻天地间似乎只剩下那道青光。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "这一刻",
	},
	{
		name:         "10 深吸一口气",
		text:         "少年深吸一口气，将胸口翻涌的气血强行压了下去。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "深吸一口气",
	},
	{
		name:         "11 缓缓说道",
		text:         "大长老放下茶盏，目光扫过全场，缓缓说道：“大比现在开始。”",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "缓缓说道",
	},
	{
		name:         "12 嘴角泛起",
		text:         "听到报价翻倍，陈青嘴角泛起一抹若有若无的笑意。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "嘴角泛起",
	},
	{
		name:         "13 令人窒息的",
		text:         "一股令人窒息的威压从云层上方骤然垂落。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "令人窒息的",
	},
	{
		name:         "14 不由得超阈值复现",
		text:         "他不由得握紧了剑柄，看清来人面容后又不由得退后半步。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "不由得",
	},
	{
		name:         "15 宛如超阈值复现",
		text:         "剑光宛如流星坠地，气浪炸开时又宛如怒海狂涛。",
		wantCategory: domain.DeaiCategoryCliche,
		wantSubstr:   "宛如",
	},
	{
		name:         "16 密集不是而是对比句",
		text:         "这一剑不是试探而是绝杀，他求的不是全身而退而是彻底斩草除根，靠的不是境界碾压而是阵纹反制。",
		wantCategory: domain.DeaiCategoryStructure,
		wantSubstr:   "不是",
	},
	{
		name:         "17 密集与其说不如说",
		text:         "与其说这是一场宗门比试不如说是一场死斗，与其说是切磋功法不如说是争夺唯一生机。",
		wantCategory: domain.DeaiCategoryStructure,
		wantSubstr:   "与其说",
	},
	{
		name:         "18 AI三段式对称排比",
		text:         "那是积蓄十年的剑意，那是斩断枷锁的决绝，那是踏碎青云的锋芒。",
		wantCategory: domain.DeaiCategoryStructure,
		wantSubstr:   "那是积蓄十年的剑意",
	},
	{
		name:         "19 章末强行鸡汤升华",
		text:         "收起染血的令牌，他知道属于自己的真正的考验才刚刚开始，命运的齿轮已经悄然转动。",
		wantCategory: domain.DeaiCategoryStructure,
		wantSubstr:   "他知道",
	},
	{
		name:         "20 单段超长文字墙 (>160字)",
		text:         strings.Repeat("夜雨顺着残破的飞檐不断滑落，青石板路面映出坊市灯笼昏黄的微光，远处执事堂的铜钟已经敲过了三更。", 4),
		wantCategory: domain.DeaiCategoryFormatting,
		wantSubstr:   "夜雨",
	},
}

// 20 段优质男频网文干净样本（短句、动作干脆、对话推进、零 AI 套话，期望 0 误报）。
var cleanWebNovelSamples = []string{
	"铁剑入鞘，血珠顺着槽口滴在青石阶上。",
	"“三百灵石，少一枚现在就封炉。”林渊把丹瓶按在柜面，指节叩了两下。",
	"王煞退后半步，左手悄然扣住袖底的阴雷珠。",
	"巷口风声忽紧，三道黑影贴着墙根掠过，靴底未沾半点泥水。",
	"账单推到会议桌中央，原本喧哗的几位董事同时闭了嘴。",
	"“城投那块地批文已经下来了。”陆沉合上笔记本，“周总现在撤资还来得及。”",
	"铜钱落在桌面转了三圈，正面朝上。",
	"青冥剑横在膝头，剑脊三道裂纹在烛火下格外刺眼。",
	"“师兄来得正好，这具残阵还缺一枚阵眼灵石。”",
	"火折子亮起半寸微光，照出石壁上斑驳的古宗剑痕。",
	"掌柜抬头看了他一眼，没多问，转身从暗格取出一只黑木匣。",
	"雷纹自脚底炸开，王煞护体罡气仅撑了半息便寸寸崩裂。",
	"手机屏幕亮起银行到账短信：可用余额三千二百万。",
	"他拎起行囊跨出门槛，身后院门在风雨里重重撞上。",
	"“明日卯时之前，把东坊市三间铺子的地契交过来。”",
	"残破星盘在掌心微微发烫，第三道推演刻度终于亮起。",
	"马车停在朱雀街口，车帘掀开一角，露出一枚玄铁腰牌。",
	"茶水已经凉透，杯底沉着几片舒展开的灵芽。",
	"林渊屈指弹碎阵旗，转身隐入夜市人流。",
	"更鼓敲过四声，远处内门主峰忽然亮起三道传讯金光。",
}

func TestLinterBenchmark20AIismAnd20CleanSamples(t *testing.T) {
	rules := DefaultRules()
	profile := domain.DefaultStyleProfile(1)
	profile.MaxParagraphChars = 160

	// 1. 验证 20 段典型 AI 腔样本 100% 检出，且 StartRune..EndRune 切片与 MatchedText 完全吻合。
	for _, tc := range typicalAIismSamples {
		t.Run("AIism_"+tc.name, func(t *testing.T) {
			issues, score := Lint(tc.text, rules, profile)
			if len(issues) == 0 {
				t.Fatalf("expected AI-ism sample %q to trigger at least 1 LintIssue, got 0", tc.name)
			}
			if score >= 100 {
				t.Fatalf("expected DeaiScore < 100 on AI-ism sample, got %d", score)
			}

			paras := SplitParagraphs(tc.text)
			matchedCategory := false
			for _, iss := range issues {
				if iss.Category == tc.wantCategory && strings.Contains(iss.MatchedText, tc.wantSubstr) {
					matchedCategory = true
				}
				// 校验 Rune 偏移精确度。
				paraRunes := []rune(paras[iss.ParagraphIndex])
				if iss.StartRune < 0 || iss.EndRune > len(paraRunes) || iss.StartRune >= iss.EndRune {
					t.Fatalf("invalid rune range [%d:%d] for paragraph len %d", iss.StartRune, iss.EndRune, len(paraRunes))
				}
				if iss.Category != domain.DeaiCategoryFormatting {
					sliced := string(paraRunes[iss.StartRune:iss.EndRune])
					if sliced != iss.MatchedText {
						t.Fatalf("rune slice mismatch: para[%d:%d]=%q != MatchedText=%q", iss.StartRune, iss.EndRune, sliced, iss.MatchedText)
					}
				}
			}
			if !matchedCategory {
				t.Fatalf("expected issue in category %q containing %q, got issues=%+v", tc.wantCategory, tc.wantSubstr, issues)
			}
		})
	}

	// 2. 验证 20 段优质男频网文样本 0 误报，整章合并扫描 < 20ms。
	for i, sample := range cleanWebNovelSamples {
		issues, score := Lint(sample, rules, profile)
		if len(issues) != 0 || score != 100 {
			t.Fatalf("clean sample #%d (%q) had false positive issues: %+v (score=%d)", i+1, sample, issues, score)
		}
	}

	fullChapter := JoinParagraphs(cleanWebNovelSamples)
	start := time.Now()
	issues, score := Lint(fullChapter, rules, profile)
	elapsed := time.Since(start)
	if len(issues) != 0 || score != 100 {
		t.Fatalf("clean full chapter produced false positives: %+v", issues)
	}
	if elapsed > 20*time.Millisecond {
		t.Fatalf("expected full-chapter lint in < 20ms, took %v", elapsed)
	}
}

func TestLinterConsecutivePsychMonologueRun(t *testing.T) {
	paras := []string{
		"他心中暗自思忖着整件事的来龙去脉，脑海中不断浮现出昔日在外门受辱的种种苦涩回忆与迷茫念头。",
		"那些复杂的情绪在内心深处反复交织盘算，他觉得命运似乎总是让人在犹豫与彷徨中难以看清未来的意义。",
		"他继续在脑海中复盘着每一个纠结的念头，暗道自己如今的处境实在充满着难以言说的滋味与沉思。",
	}
	content := JoinParagraphs(paras)
	issues, score := Lint(content, DefaultRules(), domain.DefaultStyleProfile(1))

	foundPsych := false
	for _, iss := range issues {
		if iss.RuleID == "formatting-psych-monologue-run" && iss.ParagraphIndex == 2 {
			foundPsych = true
			if iss.EndRune != utf8.RuneCountInString(paras[2]) {
				t.Fatalf("unexpected EndRune=%d, want %d", iss.EndRune, utf8.RuneCountInString(paras[2]))
			}
		}
	}
	if !foundPsych || score >= 100 {
		t.Fatalf("expected 3 consecutive pure psychological paragraphs to trigger formatting-psych-monologue-run, got issues=%+v score=%d", issues, score)
	}
}

func TestExtractQuantitativeDNAAndSceneExcerpts(t *testing.T) {
	samples := []string{
		JoinParagraphs([]string{
			"剑光乍起，林渊踏步欺身，铁剑直刺王煞咽喉。雷纹在掌心轰然炸开，震碎三寸罡气。",
			"“就凭你这残阵也想拦我？”王煞挥袖震开飞石，后撤三步，虎口渗出一丝鲜血。",
			"“你可以试试。”林渊屈指扣住阵盘，第三道杀阵瞬间合拢。",
		}),
		JoinParagraphs([]string{
			"密库石门背后传来诡异的锁链声，神识探入三尺便被古宗禁制强行切断。",
			"“这处疑点不对劲。”苏晚晴压低声音，“三百年前的线索指向内门后山。”",
		}),
	}

	avgSent, maxPara, dlgMin, dlgMax := ExtractQuantitativeDNA(samples)
	if avgSent < 6 || avgSent > 40 {
		t.Fatalf("unexpected avgSentenceChars: %v", avgSent)
	}
	if maxPara < 80 || maxPara > 220 {
		t.Fatalf("unexpected maxParagraphChars: %d", maxPara)
	}
	if dlgMin <= 0 || dlgMax <= dlgMin || dlgMax > 1.0 {
		t.Fatalf("unexpected dialogue ratio range: [%v, %v]", dlgMin, dlgMax)
	}

	excerpts := ExtractSceneExcerpts(1, samples)
	if len(excerpts) == 0 {
		t.Fatalf("expected scene excerpts extracted from sample texts")
	}
	for _, ex := range excerpts {
		if err := ex.Validate(); err != nil {
			t.Fatalf("extracted sample failed Validate(): %v (%+v)", err, ex)
		}
	}
}

func TestLintPreservesBothOverlappingParagraphRules(t *testing.T) {
	// 构造第 3 段同时超长（> 80 字）且连续 3 段纯心理活动，两条整段规则必须同时报出。
	p1 := "他心中暗自思忖着整件事的来龙去脉，脑海中不断浮现出昔日在外门受辱的种种苦涩回忆与迷茫念头。"
	p2 := "那些复杂的情绪在内心深处反复交织盘算，他觉得命运似乎总是让人在犹豫与彷徨中难以看清未来的意义。"
	p3 := strings.Repeat("他继续在脑海中复盘着每一个纠结的念头，暗道自己如今的处境实在充满着难以言说的滋味与沉思。", 3)
	content := JoinParagraphs([]string{p1, p2, p3})

	profile := domain.DefaultStyleProfile(1)
	profile.MaxParagraphChars = 80
	issues, _ := Lint(content, DefaultRules(), profile)

	var gotWall, gotPsych bool
	for _, iss := range issues {
		if iss.ParagraphIndex == 2 && iss.RuleID == "formatting-wall-of-text" {
			gotWall = true
		}
		if iss.ParagraphIndex == 2 && iss.RuleID == "formatting-psych-monologue-run" {
			gotPsych = true
		}
	}
	if !gotWall || !gotPsych {
		t.Fatalf("expected both wall-of-text and psych-monologue-run on paragraph 2, got %+v", issues)
	}
}
