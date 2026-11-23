package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo"

	"nib/internal/domain"
	"nib/internal/radar"
)

// RadarService 提供起点中文网与番茄小说网排行榜抓取、AI 拆榜分析桌面 RPC 绑定。
type RadarService struct {
	rt      *Runtime
	fetcher *radar.Fetcher
}

// NewRadarService 构造绑定到共享 Runtime 的 RadarService。
func NewRadarService(rt *Runtime) *RadarService {
	return &RadarService{
		rt:      rt,
		fetcher: radar.NewFetcher(),
	}
}

// ListBoards 返回全部内置支持抓取的榜单列表（起点中文网 + 番茄小说网）。
func (s *RadarService) ListBoards() []domain.RadarBoardSpec {
	return radar.ListBoards()
}

// FetchBoard 抓取指定平台与榜单的最新排行数据（默认命中 5 分钟缓存，forceRefresh=true 强制刷新）。
func (s *RadarService) FetchBoard(
	ctx context.Context,
	platform string,
	boardKey string,
	forceRefresh bool,
) (domain.RadarBoard, error) {
	return s.fetcher.FetchBoard(ctx, domain.RadarPlatform(strings.TrimSpace(platform)), strings.TrimSpace(boardKey), forceRefresh)
}

// AnalyzeTrend 调用 AI 模型对勾选的上榜作品或粘贴文本进行流式拆榜分析，输出高频卖点与差异化开书切入点。
func (s *RadarService) AnalyzeTrend(
	ctx context.Context,
	req domain.RadarAnalyzeRequest,
	ch *mygo.Channel[domain.RadarAnalyzeChunk],
) (domain.RadarAnalysis, error) {
	if err := req.Validate(); err != nil {
		return domain.RadarAnalysis{}, err
	}
	eng, genCtx, cancel, release, err := s.rt.trackRadarAnalyzer(ctx)
	if err != nil {
		return domain.RadarAnalysis{}, err
	}
	defer release()

	return eng.AnalyzeRadarTrend(genCtx, req, channelEmitter(genCtx, cancel, ch))
}
