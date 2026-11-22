package compiler

import (
	"fmt"
	"strings"

	"nib/internal/domain"
	"nib/internal/genres"
)

func buildP0Content(book domain.Book, vol domain.Volume, invariants []domain.Entity) string {
	var b strings.Builder
	b.WriteString("【写作引擎系统指令】\n")
	b.WriteString("你是专业中文男频长篇网文写作引擎。请严格遵循题材规则、世界铁律与本章节拍卡直接输出章节正文。\n")
	b.WriteString("输出约束：\n")
	b.WriteString("- 只输出纯净小说正文，不要输出“好的”“以下是正文”等客套话、不要输出 Markdown 标题或节拍复述。\n")
	b.WriteString("- 严禁违背世界铁律与已出场实体设定，人物对话、资源消耗与招式结果保持强因果自洽。\n")
	b.WriteString("- 保持中文手机端网文短段节奏，动作与对话优先于空洞心理总结。\n\n")

	targetWords := book.TargetChapterWords
	if targetWords <= 0 {
		targetWords = domain.DefaultTargetChapterWords
	}
	b.WriteString("【书籍核心设定】\n")
	fmt.Fprintf(&b, "书名：%s\n", book.Title)
	fmt.Fprintf(&b, "目标章字数：约 %d 字\n", targetWords)
	if strings.TrimSpace(book.Premise) != "" {
		fmt.Fprintf(&b, "核心卖点与主线：%s\n", strings.TrimSpace(book.Premise))
	}
	if strings.TrimSpace(book.GoldenFinger) != "" {
		fmt.Fprintf(&b, "金手指与规则：%s\n", strings.TrimSpace(book.GoldenFinger))
	}
	if povLabel := formatBookPOVMode(book.POVMode); povLabel != "" {
		fmt.Fprintf(&b, "本书叙事视角规范：%s\n", povLabel)
	}
	if strings.TrimSpace(book.PacingStyle) != "" {
		fmt.Fprintf(&b, "本书节奏与平台风格：%s\n", strings.TrimSpace(book.PacingStyle))
	}
	if strings.TrimSpace(book.WritingPrompt) != "" {
		fmt.Fprintf(&b, "本书固定写作指令：%s\n", strings.TrimSpace(book.WritingPrompt))
	}
	if vol.Title != "" || vol.Goal != "" || vol.ClimaxPlan != "" {
		fmt.Fprintf(&b, "当前卷：第%d卷 %s\n", vol.Seq, vol.Title)
		if strings.TrimSpace(vol.Goal) != "" {
			fmt.Fprintf(&b, "卷阶段目标：%s\n", strings.TrimSpace(vol.Goal))
		}
		if strings.TrimSpace(vol.ClimaxPlan) != "" {
			fmt.Fprintf(&b, "卷高潮规划：%s\n", strings.TrimSpace(vol.ClimaxPlan))
		}
	}

	b.WriteString("\n")
	b.WriteString(genres.PromptBlock(book.Genre))

	if len(invariants) > 0 {
		b.WriteString("\n\n【世界铁律（全书硬约束，严禁违背）】\n")
		for _, inv := range invariants {
			b.WriteString(formatEntityDetailed(inv))
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func formatBookPOVMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "first_person":
		return "第一人称（全程以“我”视角推进，不泄露主角不知情的他人内心）"
	case "third_limited":
		return "第三人称限知（紧贴当前 POV 人物视角与感官）"
	case "third_omniscient":
		return "第三人称全知（允许自然切换视角展现群像博弈）"
	case "multi_pov":
		return "多视角群像（按节拍 POV 清晰切镜，切镜需有明显场景过渡）"
	default:
		return strings.TrimSpace(mode)
	}
}

func buildP1Content(
	book domain.Book,
	chap domain.Chapter,
	prevChapters []domain.Chapter,
	activeDeadlines []domain.StoryDeadline,
	beats []domain.Beat,
	entityByID map[int64]domain.Entity,
	extraInstruction string,
) string {
	var b strings.Builder
	b.WriteString("【本章创作任务与节拍编排】\n")
	fmt.Fprintf(&b, "章节：第%d章《%s》\n", chap.GlobalSeq, chap.Title)
	fmt.Fprintf(&b, "主情绪极性：%s（%s）\n", chap.EmotionTag.Label(), chap.EmotionTag)
	fmt.Fprintf(&b, "章末钩子类型：%s（%s）\n", chap.HookType.Label(), chap.HookType)
	if strings.TrimSpace(chap.HookNote) != "" {
		fmt.Fprintf(&b, "章末钩子落点要求：%s\n", strings.TrimSpace(chap.HookNote))
	}
	if strings.TrimSpace(chap.OutlineSummary) != "" {
		fmt.Fprintf(&b, "本章概要：%s\n", strings.TrimSpace(chap.OutlineSummary))
	}

	if tlBlock := formatStoryTimelineBlock(chap, prevChapters, activeDeadlines); tlBlock != "" {
		b.WriteString(tlBlock)
		b.WriteByte('\n')
	}

	beatWordTotal := 0
	for _, bt := range beats {
		if bt.TargetWords > 0 {
			beatWordTotal += bt.TargetWords
		}
	}
	targetWords := book.TargetChapterWords
	if targetWords <= 0 {
		targetWords = domain.DefaultTargetChapterWords
	}
	if beatWordTotal > 0 {
		targetWords = beatWordTotal
	}
	fmt.Fprintf(&b, "本章目标字数：约 %d 字\n", targetWords)

	if len(beats) > 0 {
		b.WriteString("\n【按顺序执行以下节拍卡】\n")
		for i, bt := range beats {
			seq := bt.Seq
			if seq <= 0 {
				seq = i + 1
			}
			words := bt.TargetWords
			if words <= 0 {
				words = domain.DefaultBeatTargetWords
			}
			fmt.Fprintf(&b, "%d. [%s]（约 %d 字）%s", seq, bt.Stage.Label(), words, strings.TrimSpace(bt.Summary))
			if strings.TrimSpace(bt.ConflictNote) != "" {
				fmt.Fprintf(&b, "\n   核心冲突/博弈：%s", strings.TrimSpace(bt.ConflictNote))
			}
			if bt.POVEntityID != nil && *bt.POVEntityID > 0 {
				if ent, ok := entityByID[*bt.POVEntityID]; ok {
					fmt.Fprintf(&b, "\n   视角人物(POV)：%s", ent.Name)
				}
			}
			if len(bt.EntityIDs) > 0 {
				names := make([]string, 0, len(bt.EntityIDs))
				for _, eid := range bt.EntityIDs {
					if ent, ok := entityByID[eid]; ok {
						names = append(names, ent.Name)
					}
				}
				if len(names) > 0 {
					fmt.Fprintf(&b, "\n   出场实体：%s", strings.Join(names, "、"))
				}
			}
			b.WriteString("\n")
		}
	}

	if trimmed := strings.TrimSpace(extraInstruction); trimmed != "" {
		b.WriteString("\n【作者本轮附加指令】\n")
		b.WriteString(trimmed)
		b.WriteString("\n")
	}

	b.WriteString("\n请严格按以上节拍顺序直接撰写本章完整正文，确保结尾 200 字准确兑现章末钩子。")
	return strings.TrimSpace(b.String())
}

func formatStoryTimelineBlock(chap domain.Chapter, prevChapters []domain.Chapter, activeDeadlines []domain.StoryDeadline) string {
	var refDay *int
	var refLabel string
	var refSource string
	if strings.TrimSpace(chap.StoryTimeLabel) != "" || chap.StoryDay != nil {
		refLabel = strings.TrimSpace(chap.StoryTimeLabel)
		refDay = chap.StoryDay
		refSource = "本章故事时间"
	} else {
		for _, prev := range prevChapters {
			if prev.GlobalSeq >= chap.GlobalSeq {
				continue
			}
			if strings.TrimSpace(prev.StoryTimeLabel) != "" || prev.StoryDay != nil {
				refLabel = strings.TrimSpace(prev.StoryTimeLabel)
				refDay = prev.StoryDay
				refSource = fmt.Sprintf("前章（第%d章）故事时间", prev.GlobalSeq)
				break
			}
		}
	}

	if refLabel == "" && refDay == nil && len(activeDeadlines) == 0 {
		return ""
	}

	var b strings.Builder
	if refLabel != "" || refDay != nil {
		parts := make([]string, 0, 2)
		if refLabel != "" {
			parts = append(parts, refLabel)
		}
		if refDay != nil {
			parts = append(parts, fmt.Sprintf("Day %d", *refDay))
		}
		fmt.Fprintf(&b, "%s：%s\n", refSource, strings.Join(parts, " · "))
	}
	if len(activeDeadlines) > 0 {
		b.WriteString("故事内活跃期限倒计时：\n")
		for _, dl := range activeDeadlines {
			countdown := ""
			if refDay != nil && dl.TargetDay > 0 {
				diff := dl.TargetDay - *refDay
				switch {
				case diff > 0:
					countdown = fmt.Sprintf("（还剩 %d 天）", diff)
				case diff == 0:
					countdown = "（今日到期）"
				default:
					countdown = fmt.Sprintf("（已超期 %d 天）", -diff)
				}
			} else if dl.TargetDay > 0 {
				countdown = fmt.Sprintf("（目标 Day %d）", dl.TargetDay)
			}
			if strings.TrimSpace(dl.TargetLabel) != "" {
				countdown += " " + strings.TrimSpace(dl.TargetLabel)
			}
			fmt.Fprintf(&b, "- %s%s", dl.Title, countdown)
			if strings.TrimSpace(dl.Note) != "" {
				fmt.Fprintf(&b, "：%s", strings.TrimSpace(dl.Note))
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func buildP4Content(prevOrdered []domain.Chapter, budget int) (string, bool) {
	if len(prevOrdered) == 0 {
		return "", false
	}

	const slotHeader = "【前文结尾衔接（保持场景与语感无缝衔接，勿重复前文已发生剧情）】\n"
	prev1 := prevOrdered[0]
	tail1, _ := tailRunes(prev1.Content, prevChapter1TailRunes)
	prev1Header := fmt.Sprintf("── 上一章（第%d章《%s》）结尾 ──\n", prev1.GlobalSeq, prev1.Title)

	var hasPrev2 bool
	var prev2Header, tail2 string
	if len(prevOrdered) > 1 {
		prev2 := prevOrdered[1]
		tail2, _ = tailRunes(prev2.Content, prevChapter2TailRunes)
		if tail2 != "" {
			hasPrev2 = true
			prev2Header = fmt.Sprintf("── 上两章（第%d章《%s》）结尾 ──\n", prev2.GlobalSeq, prev2.Title)
		}
	}

	renderFull := func(t2, t1 string) string {
		var parts []string
		if t2 != "" {
			parts = append(parts, prev2Header+t2)
		}
		if t1 != "" {
			parts = append(parts, prev1Header+t1)
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.TrimSpace(slotHeader + strings.Join(parts, "\n\n"))
	}

	full := renderFull(tail2, tail1)
	if EstimateTokens(full) <= budget {
		return full, false
	}

	if budget <= EstimateTokens(slotHeader) {
		return "", true
	}

	onlyPrev1 := renderFull("", tail1)
	if EstimateTokens(onlyPrev1) <= budget {
		if hasPrev2 {
			remForPrev2 := budget - EstimateTokens(onlyPrev1) - 4
			if fitted2, kept2 := fitTailWithinTokens(prev2Header, tail2, remForPrev2); kept2 > 0 {
				combined := strings.TrimSpace(slotHeader + fitted2 + "\n\n" + prev1Header + tail1)
				if EstimateTokens(combined) <= budget {
					return combined, true
				}
			}
		}
		return onlyPrev1, true
	}

	remForPrev1 := budget - EstimateTokens(slotHeader)
	fitted1, kept1 := fitTailWithinTokens(prev1Header, tail1, remForPrev1)
	if kept1 == 0 {
		return "", true
	}
	return strings.TrimSpace(slotHeader + fitted1), true
}
