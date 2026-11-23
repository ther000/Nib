package pipeline

import (
	"context"
	"testing"

	"nib/internal/domain"
)

func TestAnalyzeRadarTrend(t *testing.T) {
	ctx := context.Background()
	st := openGenesisTestStore(t)

	llmStub := &recordingGenesisLLM{
		payloads: []string{
			`{
				"summary": "当前修仙与都市高武赛道偏好家族群像与现实资源困境反差。",
				"hot_elements": ["家族群像", "修仙经济账", "底层逆袭"],
				"hook_patterns": ["标题直给痛点反问", "简介首句亮明金手指规则"],
				"angles": [
					{
						"title": "修真界债主账房",
						"genre": "xianxia",
						"style_tags": ["慢热布局", "群像博弈"],
						"idea": "没落修仙家族的账房弟子得到一本能核算灵石与因果利滚利的古账簿，以借贷与并购吞并各方宗门。",
						"why_it_works": "结合家族经营与修真经济反差，避开传统单人升级同质化"
					}
				]
			}`,
		},
	}

	runner := New(st, nil, llmStub, nil)
	var seenCompleted bool
	res, err := runner.AnalyzeRadarTrend(ctx, domain.RadarAnalyzeRequest{
		Platform:   domain.RadarPlatformQidian,
		BoardLabel: "畅销榜",
		Items: []domain.RadarItem{
			{Rank: 1, Title: "玄鉴仙族", Author: "季越人", Category: "仙侠", Summary: "家族修仙群像"},
			{Rank: 2, Title: "没钱修什么仙？", Author: "熊狼狗", Category: "仙侠", Summary: "修仙与资本"},
		},
	}, func(chunk domain.RadarAnalyzeChunk) error {
		if chunk.Kind == domain.GenChunkCompleted && chunk.Analysis != nil {
			seenCompleted = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("AnalyzeRadarTrend failed: %v", err)
	}
	if !seenCompleted || len(res.Angles) != 1 || res.Angles[0].Genre != domain.GenreXianxia {
		t.Fatalf("unexpected radar analysis: %+v", res)
	}
}
