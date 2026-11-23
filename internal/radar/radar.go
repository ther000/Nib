package radar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"nib/internal/domain"
)

const (
	defaultCacheTTL     = 5 * time.Minute
	defaultHTTPTimeout  = 15 * time.Second
	maxResponseBodySize = 4 << 20 // 4MB
	mobileUserAgent     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	desktopUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	reQidianPageContext = regexp.MustCompile(`<script[^>]+id=["']vite-plugin-ssr_pageContext["'][^>]*>(\{[\s\S]*?\})</script>`)
	reFontFamily        = regexp.MustCompile(`font-family\s*:\s*["']?([A-Za-z0-9_-]+)["']?`)
)

var builtinBoards = []domain.RadarBoardSpec{
	// 起点中文网（移动端公开 SSR，全部 9 个官方榜单明文可读）
	{Platform: domain.RadarPlatformQidian, Key: "hotsales", Label: "畅销榜"},
	{Platform: domain.RadarPlatformQidian, Key: "yuepiao", Label: "月票榜"},
	{Platform: domain.RadarPlatformQidian, Key: "readindex", Label: "阅读指数榜"},
	{Platform: domain.RadarPlatformQidian, Key: "rec", Label: "推荐榜"},
	{Platform: domain.RadarPlatformQidian, Key: "newbook", Label: "新书榜"},
	{Platform: domain.RadarPlatformQidian, Key: "sign", Label: "签约榜"},
	{Platform: domain.RadarPlatformQidian, Key: "newauthor", Label: "新人作者榜"},
	{Platform: domain.RadarPlatformQidian, Key: "newfans", Label: "书友周榜"},
	{Platform: domain.RadarPlatformQidian, Key: "update", Label: "更新榜"},

	// 番茄小说网（公开排行榜页，附原站 @font-face 供正常渲染浏览）
	{Platform: domain.RadarPlatformFanqie, Key: "1_2_258", Label: "传统玄幻 · 阅读榜", Category: "玄幻"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_2_257", Label: "玄幻脑洞 · 阅读榜", Category: "玄幻"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_2_1140", Label: "东方仙侠 · 阅读榜", Category: "仙侠"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_2_262", Label: "都市脑洞 · 阅读榜", Category: "都市"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_2_261", Label: "都市日常 · 阅读榜", Category: "都市"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_2_272", Label: "历史脑洞 · 阅读榜", Category: "历史"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_2_8", Label: "科幻末世 · 阅读榜", Category: "科幻"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_2_539", Label: "悬疑脑洞 · 阅读榜", Category: "悬疑"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_1_258", Label: "传统玄幻 · 新书榜", Category: "玄幻"},
	{Platform: domain.RadarPlatformFanqie, Key: "1_1_262", Label: "都市脑洞 · 新书榜", Category: "都市"},
	{Platform: domain.RadarPlatformFanqie, Key: "0_2_1139", Label: "古风世情 · 女频阅读榜", Category: "女频"},
}

type cacheEntry struct {
	board     domain.RadarBoard
	expiresAt time.Time
}

// Fetcher 负责从起点中文网与番茄小说网抓取公开榜单并维护短时内存缓存（零框架耦合）。
type Fetcher struct {
	httpClient *http.Client
	cacheTTL   time.Duration
	now        func() time.Time

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

// NewFetcher 创建默认扫榜抓取器。
func NewFetcher() *Fetcher {
	return &Fetcher{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		cacheTTL:   defaultCacheTTL,
		now:        time.Now,
		cache:      make(map[string]cacheEntry),
	}
}

// ListBoards 返回全部内置可抓取榜单规格。
func ListBoards() []domain.RadarBoardSpec {
	out := make([]domain.RadarBoardSpec, len(builtinBoards))
	copy(out, builtinBoards)
	return out
}

func findBoardSpec(platform domain.RadarPlatform, key string) (domain.RadarBoardSpec, bool) {
	for _, spec := range builtinBoards {
		if spec.Platform == platform && spec.Key == key {
			return spec, true
		}
	}
	return domain.RadarBoardSpec{}, false
}

// FetchBoard 抓取指定平台与榜单的数据；默认命中 5 分钟缓存，forceRefresh=true 时强制回源。
func (f *Fetcher) FetchBoard(
	ctx context.Context,
	platform domain.RadarPlatform,
	boardKey string,
	forceRefresh bool,
) (domain.RadarBoard, error) {
	if !platform.Valid() {
		return domain.RadarBoard{}, fmt.Errorf("%w: 不支持的扫榜平台 %q", domain.ErrValidation, platform)
	}
	spec, ok := findBoardSpec(platform, strings.TrimSpace(boardKey))
	if !ok {
		return domain.RadarBoard{}, fmt.Errorf("%w: 不支持的榜单 %q", domain.ErrValidation, boardKey)
	}

	cacheKey := string(platform) + ":" + spec.Key
	if !forceRefresh {
		f.mu.RLock()
		if entry, hit := f.cache[cacheKey]; hit && f.now().Before(entry.expiresAt) {
			cached := cloneBoard(entry.board)
			cached.Cached = true
			f.mu.RUnlock()
			return cached, nil
		}
		f.mu.RUnlock()
	}

	var (
		board domain.RadarBoard
		err   error
	)
	switch platform {
	case domain.RadarPlatformQidian:
		board, err = f.fetchQidian(ctx, spec)
	case domain.RadarPlatformFanqie:
		board, err = f.fetchFanqie(ctx, spec)
	}
	if err != nil {
		return domain.RadarBoard{}, err
	}

	board.FetchedAt = f.now().Format(time.RFC3339)
	board.Cached = false

	f.mu.Lock()
	f.cache[cacheKey] = cacheEntry{
		board:     cloneBoard(board),
		expiresAt: f.now().Add(f.cacheTTL),
	}
	f.mu.Unlock()

	return board, nil
}

func (f *Fetcher) httpGet(ctx context.Context, targetURL, ua string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("网络请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("上游站点返回状态码 %d，可能暂时限流或页面策略变更", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return "", fmt.Errorf("读取榜单页面失败: %w", err)
	}
	return string(body), nil
}

func (f *Fetcher) fetchQidian(ctx context.Context, spec domain.RadarBoardSpec) (domain.RadarBoard, error) {
	sourceURL := "https://m.qidian.com/rank/" + spec.Key
	html, err := f.httpGet(ctx, sourceURL, mobileUserAgent)
	if err != nil {
		return domain.RadarBoard{}, err
	}
	items, err := ParseQidianMobileHTML(html)
	if err != nil {
		return domain.RadarBoard{}, err
	}
	return domain.RadarBoard{
		Platform:      domain.RadarPlatformQidian,
		BoardKey:      spec.Key,
		BoardLabel:    spec.Label,
		SourceURL:     sourceURL,
		FontProtected: false,
		Items:         items,
	}, nil
}

func (f *Fetcher) fetchFanqie(ctx context.Context, spec domain.RadarBoardSpec) (domain.RadarBoard, error) {
	sourceURL := "https://fanqienovel.com/rank/" + spec.Key
	html, err := f.httpGet(ctx, sourceURL, desktopUserAgent)
	if err != nil {
		return domain.RadarBoard{}, err
	}
	items, fontCSS, fontFamily, err := ParseFanqieRankHTML(html, spec.Category)
	if err != nil {
		return domain.RadarBoard{}, err
	}
	return domain.RadarBoard{
		Platform:      domain.RadarPlatformFanqie,
		BoardKey:      spec.Key,
		BoardLabel:    spec.Label,
		SourceURL:     sourceURL,
		FontProtected: fontCSS != "" || fontFamily != "",
		FontCSS:       fontCSS,
		FontFamily:    fontFamily,
		Items:         items,
	}, nil
}

// ParseQidianMobileHTML 从起点移动端排行榜 SSR 页面提取明文榜单条目。
func ParseQidianMobileHTML(html string) ([]domain.RadarItem, error) {
	m := reQidianPageContext.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil, errors.New("起点页面结构已变更，未找到榜单数据（可稍后重试或改用粘贴文本拆榜）")
	}

	type qidianRecord struct {
		RankNum int    `json:"rankNum"`
		Bid     string `json:"bid"`
		BName   string `json:"bName"`
		BAuth   string `json:"bAuth"`
		Cat     string `json:"cat"`
		SubCat  string `json:"subCat"`
		Cnt     string `json:"cnt"`
		Desc    string `json:"desc"`
	}
	var payload struct {
		PageContext struct {
			PageProps struct {
				PageData struct {
					Records []qidianRecord `json:"records"`
				} `json:"pageData"`
			} `json:"pageProps"`
		} `json:"pageContext"`
	}
	if err := json.Unmarshal([]byte(m[1]), &payload); err != nil {
		return nil, fmt.Errorf("解析起点榜单 JSON 失败: %w", err)
	}
	recs := payload.PageContext.PageProps.PageData.Records
	if len(recs) == 0 {
		return nil, errors.New("起点榜单返回为空，可能遇到临时风控或页面改版")
	}

	items := make([]domain.RadarItem, 0, len(recs))
	for i, r := range recs {
		title := strings.TrimSpace(r.BName)
		if title == "" {
			continue
		}
		rank := r.RankNum
		if rank <= 0 {
			rank = i + 1
		}
		detailURL := ""
		if bid := strings.TrimSpace(r.Bid); bid != "" {
			detailURL = "https://book.qidian.com/info/" + bid + "/"
		}
		items = append(items, domain.RadarItem{
			Rank:        rank,
			BookID:      strings.TrimSpace(r.Bid),
			Title:       title,
			Author:      strings.TrimSpace(r.BAuth),
			Category:    strings.TrimSpace(r.Cat),
			SubCategory: strings.TrimSpace(r.SubCat),
			WordCount:   strings.TrimSpace(r.Cnt),
			Summary:     strings.TrimSpace(r.Desc),
			DetailURL:   detailURL,
		})
	}
	if len(items) == 0 {
		return nil, errors.New("未能从起点榜单提取有效作品")
	}
	return items, nil
}

// ParseFanqieRankHTML 从番茄小说网排行榜 SSR 页面提取作品条目与原站公开字体样式。
func ParseFanqieRankHTML(html string, fallbackCategory string) ([]domain.RadarItem, string, string, error) {
	jsonStr, err := extractFanqieInitialState(html)
	if err != nil {
		return nil, "", "", err
	}

	type fanqieBook struct {
		BookID         string `json:"bookId"`
		BookName       string `json:"bookName"`
		Author         string `json:"author"`
		Abstract       string `json:"abstract"`
		Category       string `json:"category"`
		WordNumber     string `json:"wordNumber"`
		ReadCount      string `json:"read_count"`
		ReadCountCamel string `json:"readCount"`
		CreationStatus string `json:"creationStatus"`
		CurrentPos     int    `json:"currentPos"`
	}
	var payload struct {
		Common struct {
			CSS string `json:"css"`
		} `json:"common"`
		Rank struct {
			BookList []fanqieBook `json:"book_list"`
		} `json:"rank"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		return nil, "", "", fmt.Errorf("解析番茄榜单 JSON 失败: %w", err)
	}
	if len(payload.Rank.BookList) == 0 {
		return nil, "", "", errors.New("番茄榜单返回为空，可能遇到页面改版或限流")
	}

	fontCSS := strings.TrimSpace(payload.Common.CSS)
	fontFamily := ""
	if fm := reFontFamily.FindStringSubmatch(fontCSS); len(fm) >= 2 {
		fontFamily = strings.TrimSpace(fm[1])
	}

	items := make([]domain.RadarItem, 0, len(payload.Rank.BookList))
	for i, b := range payload.Rank.BookList {
		title := strings.TrimSpace(b.BookName)
		if title == "" {
			continue
		}
		rank := b.CurrentPos
		if rank <= 0 {
			rank = i + 1
		}
		cat := strings.TrimSpace(b.Category)
		if cat == "" {
			cat = fallbackCategory
		}
		statusText := ""
		switch strings.TrimSpace(b.CreationStatus) {
		case "0":
			statusText = "已完结"
		case "1":
			statusText = "连载中"
		}
		readRaw := strings.TrimSpace(b.ReadCount)
		if readRaw == "" || readRaw == "0" {
			readRaw = strings.TrimSpace(b.ReadCountCamel)
		}
		detailURL := ""
		if bid := strings.TrimSpace(b.BookID); bid != "" {
			detailURL = "https://fanqienovel.com/page/" + bid
		}
		items = append(items, domain.RadarItem{
			Rank:       rank,
			BookID:     strings.TrimSpace(b.BookID),
			Title:      title,
			Author:     strings.TrimSpace(b.Author),
			Category:   cat,
			WordCount:  formatCNCount(b.WordNumber, "字"),
			HeatText:   formatCNCount(readRaw, "人在读"),
			StatusText: statusText,
			Summary:    strings.TrimSpace(b.Abstract),
			DetailURL:  detailURL,
		})
	}
	if len(items) == 0 {
		return nil, "", "", errors.New("未能从番茄榜单提取有效作品")
	}
	return items, fontCSS, fontFamily, nil
}

func extractFanqieInitialState(html string) (string, error) {
	const marker = "window.__INITIAL_STATE__"
	idx := strings.Index(html, marker)
	if idx < 0 {
		return "", errors.New("番茄页面结构已变更，未找到榜单数据（可稍后重试或改用粘贴文本拆榜）")
	}
	rest := html[idx+len(marker):]
	eqIdx := strings.IndexByte(rest, '=')
	if eqIdx < 0 {
		return "", errors.New("番茄榜单数据边界解析失败")
	}
	rest = strings.TrimSpace(rest[eqIdx+1:])
	if !strings.HasPrefix(rest, "{") {
		return "", errors.New("番茄榜单数据边界解析失败")
	}
	dec := json.NewDecoder(strings.NewReader(rest))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return "", fmt.Errorf("番茄榜单数据边界解析失败: %w", err)
	}
	return string(raw), nil
}

func formatCNCount(raw string, unit string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return ""
	}
	if n >= 10000 {
		return fmt.Sprintf("%.1f万%s", float64(n)/10000.0, unit)
	}
	return fmt.Sprintf("%d%s", n, unit)
}

func cloneBoard(src domain.RadarBoard) domain.RadarBoard {
	dst := src
	dst.Items = make([]domain.RadarItem, len(src.Items))
	copy(dst.Items, src.Items)
	return dst
}
