package linter

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"nib/internal/domain"
)

// ExtractQuantitativeDNA 从作者导入的 1~5 章样章中确定性提取量化文风指纹：
//   - avgSentenceChars：平均单句汉字数（保留 1 位小数）
//   - maxParagraphChars：单段推荐字数上限（取样章段落字数的 P90 分位并钳位在 [80, 220]）
//   - dialogueRatioMin / dialogueRatioMax：对话字数占比区间（保留 2 位小数）
func ExtractQuantitativeDNA(sampleTexts []string) (
	avgSentenceChars float64,
	maxParagraphChars int,
	dialogueRatioMin float64,
	dialogueRatioMax float64,
) {
	var allParaLengths []int
	var totalSentenceWords int
	var totalSentenceCount int
	var sampleDialogueRatios []float64

	for _, sample := range sampleTexts {
		paras := SplitParagraphs(sample)
		if len(paras) == 0 {
			continue
		}

		sampleTotalWords := 0
		sampleDialogueWords := 0

		for _, para := range paras {
			pw := domain.CountWords(para)
			if pw == 0 {
				continue
			}
			allParaLengths = append(allParaLengths, pw)
			sampleTotalWords += pw
			sampleDialogueWords += countQuotedWords(para)

			sentences := splitSentences(para)
			for _, sent := range sentences {
				sw := domain.CountWords(sent)
				if sw >= 2 {
					totalSentenceWords += sw
					totalSentenceCount++
				}
			}
		}

		if sampleTotalWords > 0 {
			ratio := float64(sampleDialogueWords) / float64(sampleTotalWords)
			sampleDialogueRatios = append(sampleDialogueRatios, ratio)
		}
	}

	if totalSentenceCount == 0 || len(allParaLengths) == 0 {
		return domain.DefaultAvgSentenceChars,
			domain.DefaultMaxParagraphChars,
			domain.DefaultDialogueRatioMin,
			domain.DefaultDialogueRatioMax
	}

	avgSentenceChars = math.Round((float64(totalSentenceWords)/float64(totalSentenceCount))*10) / 10
	if avgSentenceChars < 6 {
		avgSentenceChars = 6
	} else if avgSentenceChars > 80 {
		avgSentenceChars = 80
	}

	slices.Sort(allParaLengths)
	p90Idx := int(math.Floor(float64(len(allParaLengths)-1) * 0.90))
	p90 := allParaLengths[p90Idx]
	// 给 P90 留约 10% 弹性，同时钳位在合理手机阅读范围内。
	maxParagraphChars = int(math.Round(float64(p90)*1.10/5.0)) * 5
	if maxParagraphChars < 80 {
		maxParagraphChars = 80
	} else if maxParagraphChars > 220 {
		maxParagraphChars = 220
	}

	if len(sampleDialogueRatios) == 0 {
		dialogueRatioMin = domain.DefaultDialogueRatioMin
		dialogueRatioMax = domain.DefaultDialogueRatioMax
		return
	}

	minR, maxR := sampleDialogueRatios[0], sampleDialogueRatios[0]
	sumR := 0.0
	for _, r := range sampleDialogueRatios {
		if r < minR {
			minR = r
		}
		if r > maxR {
			maxR = r
		}
		sumR += r
	}
	meanR := sumR / float64(len(sampleDialogueRatios))
	// 若只有单篇样章，围绕均值扩展 ±0.10 带宽。
	if maxR-minR < 0.12 {
		minR = max(0.05, meanR-0.10)
		maxR = min(0.85, meanR+0.10)
	} else {
		minR = max(0.05, minR-0.03)
		maxR = min(0.90, maxR+0.03)
	}
	dialogueRatioMin = math.Round(minR*100) / 100
	dialogueRatioMax = math.Round(maxR*100) / 100
	if dialogueRatioMin > dialogueRatioMax {
		dialogueRatioMin, dialogueRatioMax = dialogueRatioMax, dialogueRatioMin
	}
	return
}

// ExtractSceneExcerpts 从样章中按场景类型（combat / dialogue / suspense / daily）自动切分代表性语感锚点片段（每段约 200~500 字），
// 供离线或未配置 LLM 路由时直接入库 style_samples。
func ExtractSceneExcerpts(bookID int64, sampleTexts []string) []domain.UpsertStyleSampleInput {
	type windowCandidate struct {
		scene   domain.SceneType
		score   int
		excerpt string
	}

	bestByScene := make(map[domain.SceneType]windowCandidate)

	for _, sample := range sampleTexts {
		paras := SplitParagraphs(sample)
		if len(paras) == 0 {
			continue
		}

		// 以 2~4 段为滑动窗口切取 120~520 字的完整片段。
		for i := 0; i < len(paras); i++ {
			var windowParas []string
			words := 0
			for j := i; j < len(paras) && j < i+4; j++ {
				pw := domain.CountWords(paras[j])
				if words+pw > 520 && len(windowParas) > 0 {
					break
				}
				windowParas = append(windowParas, paras[j])
				words += pw
				if words >= 220 {
					break
				}
			}
			if words < 40 {
				continue
			}
			excerpt := JoinParagraphs(windowParas)
			scene, score := classifyExcerptScene(excerpt)
			if prev, exists := bestByScene[scene]; !exists || score > prev.score {
				bestByScene[scene] = windowCandidate{
					scene:   scene,
					score:   score,
					excerpt: excerpt,
				}
			}
		}
	}

	out := make([]domain.UpsertStyleSampleInput, 0, len(bestByScene))
	for _, scene := range domain.AllSceneTypes() {
		cand, ok := bestByScene[scene]
		if !ok || strings.TrimSpace(cand.excerpt) == "" {
			continue
		}
		out = append(out, domain.UpsertStyleSampleInput{
			BookID:    bookID,
			SceneType: scene,
			Title:     fmt.Sprintf("样章提取·%s示范", scene.Label()),
			Excerpt:   cand.excerpt,
		})
	}
	return out
}

func classifyExcerptScene(text string) (domain.SceneType, int) {
	combatHits := countKeywordHits(text, "剑", "斩", "杀", "拳", "雷", "阵", "血", "罡气", "破绽", "轰", "后撤", "交锋", "灵力", "刀")
	suspenseHits := countKeywordHits(text, "暗流", "线索", "诡异", "密库", "黑影", "神识", "追踪", "禁制", "秘密", "古宗", "试探", "疑点")
	totalWords := max(1, domain.CountWords(text))
	quotedWords := countQuotedWords(text)
	dialoguePct := (quotedWords * 100) / totalWords

	combatScore := combatHits * 12
	dialogueScore := dialoguePct
	suspenseScore := suspenseHits * 12
	dailyScore := 25

	bestScene := domain.SceneDaily
	bestScore := dailyScore
	if dialogueScore > bestScore && dialoguePct >= 35 {
		bestScene = domain.SceneDialogue
		bestScore = dialogueScore
	}
	if suspenseScore > bestScore {
		bestScene = domain.SceneSuspense
		bestScore = suspenseScore
	}
	if combatScore > bestScore {
		bestScene = domain.SceneCombat
		bestScore = combatScore
	}
	return bestScene, bestScore
}

func countKeywordHits(text string, keywords ...string) int {
	hits := 0
	for _, kw := range keywords {
		hits += strings.Count(text, kw)
	}
	return hits
}

func splitSentences(para string) []string {
	var out []string
	var cur strings.Builder
	for _, r := range para {
		cur.WriteRune(r)
		switch r {
		case '。', '！', '？', '!', '?', '；', ';':
			s := strings.TrimSpace(cur.String())
			if s != "" {
				out = append(out, s)
			}
			cur.Reset()
		}
	}
	if tail := strings.TrimSpace(cur.String()); tail != "" {
		out = append(out, tail)
	}
	return out
}

func countQuotedWords(para string) int {
	total := 0
	inQuote := false
	var closeExpected rune

	for _, r := range para {
		if !inQuote {
			switch r {
			case '“':
				inQuote = true
				closeExpected = '”'
			case '「':
				inQuote = true
				closeExpected = '」'
			case '『':
				inQuote = true
				closeExpected = '』'
			case '"':
				inQuote = true
				closeExpected = '"'
			}
			continue
		}
		if r == closeExpected {
			inQuote = false
			continue
		}
		if !isSpaceOrQuote(r) {
			total++
		}
	}
	return total
}

func isSpaceOrQuote(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || isQuoteRune(r)
}
