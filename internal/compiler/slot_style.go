package compiler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
)

// InferDominantScene 根据章节节拍卡与情绪/钩子特征推断本章主导场景类型（combat / dialogue / suspense / daily）。
func InferDominantScene(beats []domain.Beat, chap domain.Chapter) domain.SceneType {
	scores := map[domain.SceneType]int{
		domain.SceneCombat:   0,
		domain.SceneDialogue: 0,
		domain.SceneSuspense: 0,
		domain.SceneDaily:    1,
	}

	switch chap.EmotionTag {
	case domain.EmotionBurst:
		scores[domain.SceneCombat] += 3
	case domain.EmotionTurn, domain.EmotionSuppress:
		scores[domain.SceneDialogue] += 2
	case domain.EmotionSetup, domain.EmotionHarvest:
		scores[domain.SceneDaily] += 2
	}
	if chap.HookType == domain.HookMystery || chap.HookType == domain.HookCrisis {
		scores[domain.SceneSuspense] += 2
	}

	combatWords := []string{"战", "杀", "斩", "剑", "拳", "雷", "阵", "交手", "反杀", "决战", "搏杀", "击溃", "轰", "死战"}
	dialogueWords := []string{"谈判", "对峙", "交锋", "试探", "嘲讽", "拍卖", "赌", "合同", "报价", "质问", "商议", "博弈", "当众"}
	suspenseWords := []string{"密库", "线索", "异象", "诡异", "潜入", "追踪", "暗流", "禁制", "秘密", "疑点", "调查", "窥探"}
	dailyWords := []string{"清点", "休整", "日常", "闭关", "赶路", "坊市", "盘点", "落袋"}

	for _, b := range beats {
		if b.Stage == domain.BeatStageBurst {
			scores[domain.SceneCombat] += 2
		}
		text := b.Summary + " " + b.ConflictNote
		for _, kw := range combatWords {
			if strings.Contains(text, kw) {
				scores[domain.SceneCombat] += 3
			}
		}
		for _, kw := range dialogueWords {
			if strings.Contains(text, kw) {
				scores[domain.SceneDialogue] += 3
			}
		}
		for _, kw := range suspenseWords {
			if strings.Contains(text, kw) {
				scores[domain.SceneSuspense] += 3
			}
		}
		for _, kw := range dailyWords {
			if strings.Contains(text, kw) {
				scores[domain.SceneDaily] += 2
			}
		}
	}

	order := []domain.SceneType{
		domain.SceneCombat,
		domain.SceneDialogue,
		domain.SceneSuspense,
		domain.SceneDaily,
	}
	best := domain.SceneDaily
	bestScore := -1
	for _, sc := range order {
		if scores[sc] > bestScore {
			bestScore = scores[sc]
			best = sc
		}
	}
	return best
}

func (c *Compiler) compileP7StyleSlot(
	ctx context.Context,
	bookID int64,
	chap domain.Chapter,
	beats []domain.Beat,
	budget int,
) (string, bool, error) {
	profile, err := c.style.GetStyleProfile(ctx, bookID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			profile = domain.DefaultStyleProfile(bookID)
		} else {
			return "", false, err
		}
	}

	rules, err := c.style.ListDeaiRules(ctx, bookID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", false, err
	}

	dominantScene := InferDominantScene(beats, chap)
	samples, err := c.style.ListStyleSamples(ctx, bookID, dominantScene)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", false, err
	}
	if len(samples) == 0 {
		samples, _ = c.style.ListStyleSamples(ctx, bookID, "")
	}
	if len(samples) > maxStyleSampleAnchors {
		samples = samples[:maxStyleSampleAnchors]
	}

	content, truncated := buildP7Content(profile, rules, dominantScene, samples, budget)
	return content, truncated, nil
}

func buildP7Content(
	profile domain.StyleProfile,
	rules []domain.DeaiRule,
	dominantScene domain.SceneType,
	samples []domain.StyleSample,
	budget int,
) (string, bool) {
	const header = "【Style DNA 文风指纹与场景样章锚点】\n"
	if budget <= EstimateTokens(header) {
		return "", true
	}

	avgSent := profile.AvgSentenceChars
	if avgSent <= 0 {
		avgSent = domain.DefaultAvgSentenceChars
	}
	maxPara := profile.MaxParagraphChars
	if maxPara <= 0 {
		maxPara = domain.DefaultMaxParagraphChars
	}
	dlgMin := profile.DialogueRatioMin
	dlgMax := profile.DialogueRatioMax
	if dlgMax <= 0 {
		dlgMin = domain.DefaultDialogueRatioMin
		dlgMax = domain.DefaultDialogueRatioMax
	}
	pov := profile.PovRule
	if !pov.Valid() {
		pov = domain.PovThirdLimited
	}

	var dnaBuilder strings.Builder
	dnaBuilder.WriteString(header)
	fmt.Fprintf(
		&dnaBuilder,
		"量化文风约束：平均句长约 %.0f 字｜单段上限 %d 字｜对话占比 %.0f%%~%.0f%%｜视角：%s（%s）\n",
		avgSent, maxPara, dlgMin*100, dlgMax*100, pov.Label(), pov,
	)
	if strings.TrimSpace(profile.ToneDirectives) != "" {
		fmt.Fprintf(&dnaBuilder, "叙事语调指令：%s\n", strings.TrimSpace(profile.ToneDirectives))
	}
	if strings.TrimSpace(profile.ForbiddenTropesNote) != "" {
		fmt.Fprintf(&dnaBuilder, "作者自定义禁区：%s\n", strings.TrimSpace(profile.ForbiddenTropesNote))
	}

	banned := make([]string, 0, 12)
	for _, r := range rules {
		if !r.Enabled || r.Category != domain.DeaiCategoryCliche || r.MaxPerChapter > 0 {
			continue
		}
		pat := strings.TrimSpace(r.Pattern)
		if pat != "" {
			banned = append(banned, pat)
			if len(banned) >= 12 {
				break
			}
		}
	}
	if len(banned) > 0 {
		fmt.Fprintf(&dnaBuilder, "禁用套话黑名单：%s\n", strings.Join(banned, "、"))
	}

	baseDNA := strings.TrimSpace(dnaBuilder.String())
	if len(samples) == 0 {
		if EstimateTokens(baseDNA) <= budget {
			return baseDNA, false
		}
		return truncateHeadWithinTokens(baseDNA, budget), true
	}

	renderWithSamples := func(list []domain.StyleSample) string {
		if len(list) == 0 {
			return baseDNA
		}
		var b strings.Builder
		b.WriteString(baseDNA)
		fmt.Fprintf(&b, "\n\n【场景语感示范（主导场景：%s；只学句式节奏与动作力度，严禁抄袭样章人物或剧情）】", dominantScene.Label())
		for i, sp := range list {
			fmt.Fprintf(&b, "\n示范 %d《%s》（%s）：\n%s", i+1, sp.Title, sp.SceneType.Label(), strings.TrimSpace(sp.Excerpt))
		}
		return strings.TrimSpace(b.String())
	}

	full := renderWithSamples(samples)
	if EstimateTokens(full) <= budget {
		return full, false
	}

	if len(samples) > 1 {
		oneSample := renderWithSamples(samples[:1])
		if EstimateTokens(oneSample) <= budget {
			return oneSample, true
		}
	}

	if EstimateTokens(baseDNA) <= budget {
		rem := budget - EstimateTokens(baseDNA) - 24
		if rem > 30 {
			cutExcerpt := truncateHeadWithinTokens(strings.TrimSpace(samples[0].Excerpt), rem)
			if cutExcerpt != "" {
				partialSample := domain.StyleSample{
					SceneType: samples[0].SceneType,
					Title:     samples[0].Title,
					Excerpt:   cutExcerpt,
				}
				candidate := renderWithSamples([]domain.StyleSample{partialSample})
				if EstimateTokens(candidate) <= budget {
					return candidate, true
				}
			}
		}
		return baseDNA, true
	}

	return truncateHeadWithinTokens(baseDNA, budget), true
}
