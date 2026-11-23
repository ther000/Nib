package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nib/internal/domain"
	"nib/internal/llm"
)

const defaultRadarAnalyzeMaxTokens = 3072

// AnalyzeRadarTrend 基于勾选的上榜作品或粘贴的榜单文本，流式拆解当前爆款规律并给出 3 个差异化开书切入点。
func (r *Runner) AnalyzeRadarTrend(
	ctx context.Context,
	req domain.RadarAnalyzeRequest,
	emit func(domain.RadarAnalyzeChunk) error,
) (domain.RadarAnalysis, error) {
	if r == nil || r.store == nil || r.llm == nil {
		return domain.RadarAnalysis{}, errors.New("pipeline dependencies not initialized")
	}
	if err := req.Validate(); err != nil {
		return domain.RadarAnalysis{}, err
	}

	send := func(chunk domain.RadarAnalyzeChunk) error {
		if emit == nil {
			return nil
		}
		return emit(chunk)
	}

	route, provider, err := r.resolveFirstAvailableStageRoute(ctx, domain.StageRadar, domain.StageOutline, domain.StageDraft)
	if err != nil {
		return domain.RadarAnalysis{}, err
	}

	if err := send(domain.RadarAnalyzeChunk{Kind: domain.GenChunkStarted}); err != nil {
		return domain.RadarAnalysis{}, domain.ErrGenerationCanceled
	}

	sysPrompt, userPrompt := buildRadarAnalyzePrompts(req)
	maxTokens := route.MaxTokens
	if maxTokens < defaultRadarAnalyzeMaxTokens {
		maxTokens = defaultRadarAnalyzeMaxTokens
	}

	chatReq := llm.ChatRequest{
		Model:       route.ModelName,
		System:      sysPrompt,
		Messages:    []llm.ChatMessage{{Role: "user", Content: userPrompt}},
		Temperature: route.Temperature,
		MaxTokens:   maxTokens,
	}

	var buf strings.Builder
	var finalUsage domain.TokenUsage
	usedProv, _, streamUsage, streamErr := r.streamWithFallback(ctx, route, provider, chatReq, func(ev llm.StreamEvent) error {
		if ev.Delta != "" {
			buf.WriteString(ev.Delta)
			if err := send(domain.RadarAnalyzeChunk{Kind: domain.GenChunkDelta, Delta: ev.Delta}); err != nil {
				return err
			}
		}
		if ev.Usage != nil {
			finalUsage = *ev.Usage
		}
		return nil
	})
	if hasUsage(streamUsage) {
		finalUsage = streamUsage
	}
	if streamErr != nil {
		_ = send(domain.RadarAnalyzeChunk{Kind: domain.GenChunkError, Error: streamErr.Error()})
		return domain.RadarAnalysis{}, streamErr
	}

	analysis, parseErr := parseOrRepairStageJSON(
		ctx,
		r,
		0,
		0,
		domain.StageRadar,
		usedProv,
		route,
		defaultRadarAnalyzeMaxTokens,
		`扫榜分析 JSON（需包含 summary, hot_elements, hook_patterns, angles）`,
		buf.String(),
		parseRadarAnalysisPayload,
	)
	if parseErr != nil {
		if !errors.Is(parseErr, domain.ErrGenerationCanceled) {
			_ = send(domain.RadarAnalyzeChunk{Kind: domain.GenChunkError, Error: parseErr.Error()})
		}
		return domain.RadarAnalysis{}, parseErr
	}

	copyVal := analysis
	_ = send(domain.RadarAnalyzeChunk{
		Kind:     domain.GenChunkCompleted,
		Analysis: &copyVal,
		Usage:    usagePtrOrNil(finalUsage),
	})
	return analysis, nil
}

func buildRadarAnalyzePrompts(req domain.RadarAnalyzeRequest) (string, string) {
	sys := `你是资深中文网文主编与市场风向分析师，擅长从当前平台排行榜作品中拆解读者买单的底层卖点、开篇钩子公式，并提炼出可直接开写的差异化切入方向。
要求：
1. 不做抄袭或换皮，提炼的是“情绪价值结构、信息差机制、开篇钩子与人设反差”，并给出差异化创新切入点。
2. 输出 3 个可直接带入开书工坊的差异化切入点（angles），genre 优先从 xuanhuan / xianxia / urban / history / scifi / mystery / game / general 中选择最贴切的基底 ID。
3. 仅输出合法 JSON 对象，禁止输出任何 Markdown 解释文字。
JSON 结构：
{
  "summary": "80~140字一句话点破当前样本榜单的核心风向与读者偏好",
  "hot_elements": ["高频卖点/人设/设定元素1", "元素2", "元素3", "元素4"],
  "hook_patterns": ["标题或简介高频钩子公式1", "公式2", "公式3"],
  "angles": [
    {
      "title": "差异化切入方向短标题（8~16字）",
      "genre": "xuanhuan|xianxia|urban|history|scifi|mystery|game|general",
      "style_tags": ["风格标签1", "风格标签2"],
      "idea": "可以直接用来开书的一句话故事灵感（60~120字，包含主角处境、金手指/核心机制与爽点方向）",
      "why_it_works": "25~60字说明为什么这个切入点既吃红利又避开同质化"
    }
  ]
}`

	var user strings.Builder
	if req.Platform != "" || strings.TrimSpace(req.BoardLabel) != "" {
		fmt.Fprintf(&user, "【样本来源】%s %s\n", req.Platform, strings.TrimSpace(req.BoardLabel))
	}
	if len(req.Items) > 0 {
		user.WriteString("\n【勾选的上榜作品】\n")
		for idx, it := range req.Items {
			if idx >= 12 {
				break
			}
			fmt.Fprintf(&user, "%d. 《%s》｜作者：%s｜分类：%s %s｜%s %s\n",
				idx+1, strings.TrimSpace(it.Title), strings.TrimSpace(it.Author),
				strings.TrimSpace(it.Category), strings.TrimSpace(it.SubCategory),
				strings.TrimSpace(it.WordCount), strings.TrimSpace(it.HeatText))
			if strings.TrimSpace(it.Summary) != "" {
				fmt.Fprintf(&user, "   简介：%s\n", strings.TrimSpace(it.Summary))
			}
		}
	}
	if strings.TrimSpace(req.RawText) != "" {
		fmt.Fprintf(&user, "\n【作者补充/粘贴的榜单文本】\n%s\n", strings.TrimSpace(req.RawText))
	}
	if strings.TrimSpace(req.Focus) != "" {
		fmt.Fprintf(&user, "\n【作者关注方向】%s\n", strings.TrimSpace(req.Focus))
	}
	return sys, user.String()
}

func parseRadarAnalysisPayload(raw string) (domain.RadarAnalysis, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return domain.RadarAnalysis{}, errors.New("empty radar analysis response")
	}
	jsonStr, err := llm.ExtractJSONObject(trimmed)
	if err != nil {
		return domain.RadarAnalysis{}, err
	}

	type rawAngle struct {
		Title           string   `json:"title"`
		Genre           string   `json:"genre"`
		StyleTags       []string `json:"style_tags"`
		StyleTagsCamel  []string `json:"styleTags"`
		Idea            string   `json:"idea"`
		WhyItWorks      string   `json:"why_it_works"`
		WhyItWorksCamel string   `json:"whyItWorks"`
	}
	type rawDTO struct {
		Summary           string     `json:"summary"`
		HotElements       []string   `json:"hot_elements"`
		HotElementsCamel  []string   `json:"hotElements"`
		HookPatterns      []string   `json:"hook_patterns"`
		HookPatternsCamel []string   `json:"hookPatterns"`
		Angles            []rawAngle `json:"angles"`
	}

	var dto rawDTO
	if err := json.Unmarshal([]byte(jsonStr), &dto); err != nil {
		return domain.RadarAnalysis{}, fmt.Errorf("unmarshal radar analysis: %w", err)
	}

	cleanList := func(a, b []string) []string {
		src := a
		if len(src) == 0 {
			src = b
		}
		out := make([]string, 0, len(src))
		for _, item := range src {
			if t := strings.TrimSpace(item); t != "" {
				out = append(out, t)
			}
		}
		return out
	}

	angles := make([]domain.RadarIdeaAngle, 0, len(dto.Angles))
	for _, a := range dto.Angles {
		idea := strings.TrimSpace(a.Idea)
		if idea == "" {
			continue
		}
		genreID := domain.GenreID(strings.TrimSpace(a.Genre))
		if !genreID.Valid() {
			genreID = domain.GenreGeneral
		}
		why := strings.TrimSpace(a.WhyItWorks)
		if why == "" {
			why = strings.TrimSpace(a.WhyItWorksCamel)
		}
		title := strings.TrimSpace(a.Title)
		if title == "" {
			title = "差异化切入方向"
		}
		angles = append(angles, domain.RadarIdeaAngle{
			Title:      title,
			Genre:      genreID,
			StyleTags:  cleanList(a.StyleTags, a.StyleTagsCamel),
			Idea:       idea,
			WhyItWorks: why,
		})
	}

	summary := strings.TrimSpace(dto.Summary)
	if summary == "" && len(angles) == 0 {
		return domain.RadarAnalysis{}, errors.New("radar analysis missing summary and angles")
	}

	return domain.RadarAnalysis{
		Summary:      summary,
		HotElements:  cleanList(dto.HotElements, dto.HotElementsCamel),
		HookPatterns: cleanList(dto.HookPatterns, dto.HookPatternsCamel),
		Angles:       angles,
	}, nil
}
