package radar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"nib/internal/domain"
)

const (
	defaultCacheTTL     = 5 * time.Minute
	fontCacheTTL        = 30 * time.Minute
	defaultHTTPTimeout  = 15 * time.Second
	maxResponseBodySize = 4 << 20 // 4MB
	qidianPagesToFetch  = 3       // 每页 20 本，共 Top 60
	fanqiePagesToFetch  = 2       // 每页 30 本，共 Top 60
	mobileUserAgent     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	desktopUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	reQidianPageContext = regexp.MustCompile(`<script[^>]+id=["']vite-plugin-ssr_pageContext["'][^>]*>(\{[\s\S]*?\})</script>`)
	reFontFamily        = regexp.MustCompile(`font-family\s*:\s*["']?([A-Za-z0-9_-]+)["']?`)
)

// qidianMajaxMap 映射起点移动端榜型到其 majax 分页接口名。
var qidianMajaxMap = map[string]string{
	"hotsales":  "hotsaleslist",
	"yuepiao":   "yuepiaolist",
	"readindex": "readIndexlist",
	"rec":       "reclist",
	"collect":   "collectlist",
	"reward":    "rewardlist",
	"newbook":   "newbooklist",
	"sign":      "signlist",
	"newauthor": "newauthorlist",
	"newfans":   "newfanslist",
	"update":    "updatelist",
}

// qidianCategoryNames 映射起点移动端官方 catId 到中文分类名。
var qidianCategoryNames = map[int]string{
	-1:    "全站",
	21:    "玄幻",
	22:    "仙侠",
	4:     "都市",
	5:     "历史",
	9:     "科幻",
	10:    "悬疑灵异",
	7:     "游戏",
	20109: "诸天无限",
	12:    "轻小说",
	1:     "奇幻",
	2:     "武侠",
	15:    "现实",
	6:     "军事",
	8:     "体育",
}

var builtinBoards = []domain.RadarBoardSpec{
	// 起点中文网（移动端公开 SSR + majax 分页，11 个官方榜单 × 15 分类，每榜 Top 60 明文）
	{Platform: domain.RadarPlatformQidian, Key: "hotsales", Label: "畅销榜"},
	{Platform: domain.RadarPlatformQidian, Key: "yuepiao", Label: "月票榜"},
	{Platform: domain.RadarPlatformQidian, Key: "readindex", Label: "阅读指数榜"},
	{Platform: domain.RadarPlatformQidian, Key: "rec", Label: "推荐榜"},
	{Platform: domain.RadarPlatformQidian, Key: "collect", Label: "收藏榜"},
	{Platform: domain.RadarPlatformQidian, Key: "reward", Label: "打赏榜"},
	{Platform: domain.RadarPlatformQidian, Key: "newbook", Label: "新书榜"},
	{Platform: domain.RadarPlatformQidian, Key: "sign", Label: "签约榜"},
	{Platform: domain.RadarPlatformQidian, Key: "newauthor", Label: "新人作者榜"},
	{Platform: domain.RadarPlatformQidian, Key: "newfans", Label: "书友周榜"},
	{Platform: domain.RadarPlatformQidian, Key: "update", Label: "更新榜"},

	// 番茄小说网 · 全站总榜（纯明文 JSON，Top 30，可直接勾选 AI 拆榜）
	{Platform: domain.RadarPlatformFanqie, Key: "top_all", Label: "全站热门总榜（明文）", Category: "全站总榜"},

	// 番茄小说网 · 男频最热（library API，每榜 Top 60，格式：lib_{gender}_{sort}_{catId}）
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_258", Label: "传统玄幻", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_257", Label: "玄幻脑洞", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_1140", Label: "东方仙侠", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_1141", Label: "西方奇幻", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_262", Label: "都市脑洞", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_261", Label: "都市日常", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_1014", Label: "都市高武", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_124", Label: "都市修真", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_263", Label: "都市种田", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_273", Label: "历史古代", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_272", Label: "历史脑洞", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_8", Label: "科幻末世", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_539", Label: "悬疑脑洞", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_751", Label: "悬疑灵异", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_746", Label: "游戏体育", Category: "男频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_0_718", Label: "动漫衍生", Category: "男频最热"},

	// 番茄小说网 · 男频新书（sort=1 最新热门，每榜 Top 60）
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_1_258", Label: "传统玄幻 · 新书", Category: "男频新书"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_1_257", Label: "玄幻脑洞 · 新书", Category: "男频新书"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_1_1140", Label: "东方仙侠 · 新书", Category: "男频新书"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_1_262", Label: "都市脑洞 · 新书", Category: "男频新书"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_1_272", Label: "历史脑洞 · 新书", Category: "男频新书"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_1_8", Label: "科幻末世 · 新书", Category: "男频新书"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_1_539", Label: "悬疑脑洞 · 新书", Category: "男频新书"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_1_1_746", Label: "游戏体育 · 新书", Category: "男频新书"},

	// 番茄小说网 · 女频最热（每榜 Top 60）
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_1139", Label: "古风世情", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_267", Label: "现言脑洞", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_253", Label: "古言脑洞", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_248", Label: "玄幻言情", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_246", Label: "宫斗宅斗", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_23", Label: "种田", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_79", Label: "年代", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_24", Label: "快穿", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_749", Label: "青春甜宠", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_748", Label: "豪门总裁", Category: "女频最热"},
	{Platform: domain.RadarPlatformFanqie, Key: "lib_0_0_747", Label: "女频悬疑", Category: "女频最热"},
}

type cacheEntry struct {
	board     domain.RadarBoard
	expiresAt time.Time
}

type fontCacheEntry struct {
	css       string
	family    string
	expiresAt time.Time
}

// Fetcher 负责从起点中文网与番茄小说网抓取公开榜单并维护短时内存缓存（零框架耦合）。
type Fetcher struct {
	httpClient *http.Client
	cacheTTL   time.Duration
	now        func() time.Time

	mu        sync.RWMutex
	cache     map[string]cacheEntry
	fontCache fontCacheEntry
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

// parseQidianBoardKey 解析起点 boardKey（支持 "hotsales" 或 "hotsales:21" 形式）。
func parseQidianBoardKey(raw string) (baseKey string, catID int, catName string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	baseKey = trimmed
	catID = -1
	if idx := strings.IndexByte(trimmed, ':'); idx > 0 {
		baseKey = strings.TrimSpace(trimmed[:idx])
		parsed, err := strconv.Atoi(strings.TrimSpace(trimmed[idx+1:]))
		if err != nil {
			return "", 0, "", false
		}
		catID = parsed
	}
	name, validCat := qidianCategoryNames[catID]
	if !validCat {
		return "", 0, "", false
	}
	return baseKey, catID, name, true
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

	cleanKey := strings.TrimSpace(boardKey)
	var (
		spec       domain.RadarBoardSpec
		qidianCat  = -1
		qidianName = "全站"
	)
	if platform == domain.RadarPlatformQidian {
		baseKey, catID, catLabel, ok := parseQidianBoardKey(cleanKey)
		if !ok {
			return domain.RadarBoard{}, fmt.Errorf("%w: 不支持的起点榜单或分类 %q", domain.ErrValidation, boardKey)
		}
		var found bool
		spec, found = findBoardSpec(platform, baseKey)
		if !found {
			return domain.RadarBoard{}, fmt.Errorf("%w: 不支持的榜单 %q", domain.ErrValidation, boardKey)
		}
		qidianCat = catID
		qidianName = catLabel
		if catID != -1 {
			cleanKey = fmt.Sprintf("%s:%d", baseKey, catID)
		} else {
			cleanKey = baseKey
		}
	} else {
		var found bool
		spec, found = findBoardSpec(platform, cleanKey)
		if !found {
			return domain.RadarBoard{}, fmt.Errorf("%w: 不支持的榜单 %q", domain.ErrValidation, boardKey)
		}
		cleanKey = spec.Key
	}

	cacheKey := string(platform) + ":" + cleanKey
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
		board, err = f.fetchQidian(ctx, spec, cleanKey, qidianCat, qidianName)
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

func (f *Fetcher) httpGetWithClient(ctx context.Context, client *http.Client, targetURL, ua, referer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json, text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := client.Do(req)
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

func (f *Fetcher) fetchQidian(
	ctx context.Context,
	spec domain.RadarBoardSpec,
	effectiveKey string,
	catID int,
	catName string,
) (domain.RadarBoard, error) {
	jar, _ := cookiejar.New(nil)
	sessClient := &http.Client{
		Timeout:   f.httpClient.Timeout,
		Transport: f.httpClient.Transport,
		Jar:       jar,
	}

	// collect / reward 仅提供 majax 接口，使用 hotsales 首页拿到同会话 _csrfToken Cookie。
	majaxOnly := spec.Key == "collect" || spec.Key == "reward"
	ssrPath := spec.Key
	if majaxOnly {
		ssrPath = "hotsales"
	}
	sourceURL := "https://m.qidian.com/rank/" + ssrPath
	html, err := f.httpGetWithClient(ctx, sessClient, sourceURL, mobileUserAgent, "")
	if err != nil {
		return domain.RadarBoard{}, err
	}

	var items []domain.RadarItem
	startPage := 1
	// 全站且存在 SSR 页时，直接复用第 1 页 SSR 省去一次请求；按分类或仅 majax 榜时从 page 1 走 majax。
	if catID == -1 && !majaxOnly {
		items, err = ParseQidianMobileHTML(html)
		if err != nil {
			return domain.RadarBoard{}, err
		}
		startPage = 2
	}

	if ep, ok := qidianMajaxMap[spec.Key]; ok && jar != nil {
		if parsedURL, uErr := url.Parse(sourceURL); uErr == nil {
			csrf := ""
			for _, ck := range jar.Cookies(parsedURL) {
				if ck.Name == "_csrfToken" {
					csrf = ck.Value
					break
				}
			}
			if csrf != "" {
				seen := make(map[string]struct{}, qidianPagesToFetch*20)
				for _, it := range items {
					seen[it.BookID+"|"+it.Title] = struct{}{}
				}
				for page := startPage; page <= qidianPagesToFetch; page++ {
					pageURL := fmt.Sprintf("https://m.qidian.com/majax/rank/%s?_csrfToken=%s&pageNum=%d&catId=%d",
						ep, url.QueryEscape(csrf), page, catID)
					rawJSON, pErr := f.httpGetWithClient(ctx, sessClient, pageURL, mobileUserAgent, sourceURL)
					if pErr != nil {
						if len(items) > 0 {
							break
						}
						return domain.RadarBoard{}, pErr
					}
					more, mErr := ParseQidianMajaxJSON(rawJSON, len(items))
					if mErr != nil || len(more) == 0 {
						if len(items) > 0 {
							break
						}
						if mErr != nil {
							return domain.RadarBoard{}, mErr
						}
						break
					}
					for _, it := range more {
						k := it.BookID + "|" + it.Title
						if _, dup := seen[k]; dup {
							continue
						}
						seen[k] = struct{}{}
						it.Rank = len(items) + 1
						items = append(items, it)
					}
				}
			}
		}
	}

	if len(items) == 0 {
		return domain.RadarBoard{}, errors.New("起点该分类榜单返回为空，请稍后重试")
	}

	boardLabel := spec.Label
	if catID != -1 && catName != "" {
		boardLabel = spec.Label + " · " + catName
	}

	return domain.RadarBoard{
		Platform:      domain.RadarPlatformQidian,
		BoardKey:      effectiveKey,
		BoardLabel:    boardLabel,
		SourceURL:     sourceURL,
		FontProtected: false,
		Items:         items,
	}, nil
}

func (f *Fetcher) fetchFanqie(ctx context.Context, spec domain.RadarBoardSpec) (domain.RadarBoard, error) {
	if spec.Key == "top_all" {
		apiURL := "https://fanqienovel.com/api/author/misc/top_book_list/v1/?limit=30&offset=0"
		raw, err := f.httpGetWithClient(ctx, f.httpClient, apiURL, desktopUserAgent, "https://fanqienovel.com/rank")
		if err != nil {
			return domain.RadarBoard{}, err
		}
		items, err := ParseFanqieTopBookListJSON(raw)
		if err != nil {
			return domain.RadarBoard{}, err
		}
		return domain.RadarBoard{
			Platform:      domain.RadarPlatformFanqie,
			BoardKey:      spec.Key,
			BoardLabel:    spec.Label,
			SourceURL:     "https://fanqienovel.com/rank",
			FontProtected: false,
			Items:         items,
		}, nil
	}

	if strings.HasPrefix(spec.Key, "lib_") {
		parts := strings.Split(spec.Key, "_")
		if len(parts) != 4 {
			return domain.RadarBoard{}, fmt.Errorf("无效的番茄分类榜配置 %q", spec.Key)
		}
		gender, sortVal, catID := parts[1], parts[2], parts[3]
		fontCSS, fontFamily := f.getFanqieFont(ctx)

		items := make([]domain.RadarItem, 0, fanqiePagesToFetch*30)
		seen := make(map[string]struct{}, fanqiePagesToFetch*30)
		for page := 0; page < fanqiePagesToFetch; page++ {
			apiURL := fmt.Sprintf(
				"https://fanqienovel.com/api/author/library/book_list/v0/?page_count=30&page_index=%d&gender=%s&category_id=%s&creation_status=-1&word_count=-1&book_type=-1&sort=%s",
				page, gender, catID, sortVal,
			)
			raw, err := f.httpGetWithClient(ctx, f.httpClient, apiURL, desktopUserAgent, "https://fanqienovel.com/library")
			if err != nil {
				if len(items) > 0 {
					break
				}
				return domain.RadarBoard{}, err
			}
			pageItems, pErr := ParseFanqieLibraryJSON(raw, spec.Label, len(items))
			if pErr != nil || len(pageItems) == 0 {
				if len(items) > 0 {
					break
				}
				if pErr != nil {
					return domain.RadarBoard{}, pErr
				}
				break
			}
			for _, it := range pageItems {
				k := it.BookID + "|" + it.Title
				if _, dup := seen[k]; dup {
					continue
				}
				seen[k] = struct{}{}
				it.Rank = len(items) + 1
				items = append(items, it)
			}
		}
		if len(items) == 0 {
			return domain.RadarBoard{}, errors.New("番茄分类榜返回为空，请稍后重试")
		}
		sourceURL := fmt.Sprintf("https://fanqienovel.com/rank/%s_2_%s", gender, catID)
		return domain.RadarBoard{
			Platform:      domain.RadarPlatformFanqie,
			BoardKey:      spec.Key,
			BoardLabel:    spec.Category + " · " + spec.Label,
			SourceURL:     sourceURL,
			FontProtected: fontCSS != "" || fontFamily != "",
			FontCSS:       fontCSS,
			FontFamily:    fontFamily,
			Items:         items,
		}, nil
	}

	// 兼容旧版 /rank/{key} 形式
	sourceURL := "https://fanqienovel.com/rank/" + spec.Key
	html, err := f.httpGetWithClient(ctx, f.httpClient, sourceURL, desktopUserAgent, "")
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

func (f *Fetcher) getFanqieFont(ctx context.Context) (string, string) {
	f.mu.RLock()
	if f.fontCache.css != "" && f.now().Before(f.fontCache.expiresAt) {
		css, fam := f.fontCache.css, f.fontCache.family
		f.mu.RUnlock()
		return css, fam
	}
	f.mu.RUnlock()

	html, err := f.httpGetWithClient(ctx, f.httpClient, "https://fanqienovel.com/rank", desktopUserAgent, "")
	if err != nil {
		return "", ""
	}
	_, fontCSS, fontFamily, err := ParseFanqieRankHTML(html, "")
	if err != nil || fontCSS == "" {
		return "", ""
	}
	f.mu.Lock()
	f.fontCache = fontCacheEntry{
		css:       fontCSS,
		family:    fontFamily,
		expiresAt: f.now().Add(fontCacheTTL),
	}
	f.mu.Unlock()
	return fontCSS, fontFamily
}

type qidianRecordDTO struct {
	RankNum int    `json:"rankNum"`
	Bid     string `json:"bid"`
	BName   string `json:"bName"`
	BAuth   string `json:"bAuth"`
	Cat     string `json:"cat"`
	SubCat  string `json:"subCat"`
	Cnt     string `json:"cnt"`
	Desc    string `json:"desc"`
}

func mapQidianRecords(recs []qidianRecordDTO, rankOffset int) []domain.RadarItem {
	items := make([]domain.RadarItem, 0, len(recs))
	for i, r := range recs {
		title := strings.TrimSpace(r.BName)
		if title == "" {
			continue
		}
		rank := r.RankNum
		if rank <= 0 {
			rank = rankOffset + i + 1
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
	return items
}

// ParseQidianMobileHTML 从起点移动端排行榜 SSR 页面提取明文榜单条目。
func ParseQidianMobileHTML(html string) ([]domain.RadarItem, error) {
	m := reQidianPageContext.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil, errors.New("起点页面结构已变更，未找到榜单数据（可稍后重试或改用粘贴文本拆榜）")
	}

	var payload struct {
		PageContext struct {
			PageProps struct {
				PageData struct {
					Records []qidianRecordDTO `json:"records"`
				} `json:"pageData"`
			} `json:"pageProps"`
		} `json:"pageContext"`
	}
	if err := json.Unmarshal([]byte(m[1]), &payload); err != nil {
		return nil, fmt.Errorf("解析起点榜单 JSON 失败: %w", err)
	}
	items := mapQidianRecords(payload.PageContext.PageProps.PageData.Records, 0)
	if len(items) == 0 {
		return nil, errors.New("起点榜单返回为空，可能遇到临时风控或页面改版")
	}
	return items, nil
}

// ParseQidianMajaxJSON 解析起点移动端 majax 分页接口返回的 JSON。
func ParseQidianMajaxJSON(raw string, rankOffset int) ([]domain.RadarItem, error) {
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Records []qidianRecordDTO `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("解析起点分页 JSON 失败: %w", err)
	}
	if payload.Code != 0 {
		return nil, fmt.Errorf("起点分页接口返回错误码 %d", payload.Code)
	}
	return mapQidianRecords(payload.Data.Records, rankOffset), nil
}

// ParseFanqieTopBookListJSON 解析番茄全站热门总榜公开明文 JSON（无字体混淆）。
func ParseFanqieTopBookListJSON(raw string) ([]domain.RadarItem, error) {
	var payload struct {
		BookList []struct {
			BookID         string `json:"book_id"`
			BookName       string `json:"book_name"`
			Author         string `json:"author"`
			Category       string `json:"category"`
			CreationStatus int    `json:"creation_status"`
		} `json:"book_list"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("解析番茄全站总榜 JSON 失败: %w", err)
	}
	if len(payload.BookList) == 0 {
		return nil, errors.New("番茄全站总榜返回为空")
	}
	items := make([]domain.RadarItem, 0, len(payload.BookList))
	for i, b := range payload.BookList {
		title := strings.TrimSpace(b.BookName)
		if title == "" {
			continue
		}
		statusText := "连载中"
		if b.CreationStatus == 0 {
			statusText = "已完结"
		}
		detailURL := ""
		if bid := strings.TrimSpace(b.BookID); bid != "" {
			detailURL = "https://fanqienovel.com/page/" + bid
		}
		items = append(items, domain.RadarItem{
			Rank:       i + 1,
			BookID:     strings.TrimSpace(b.BookID),
			Title:      title,
			Author:     strings.TrimSpace(b.Author),
			Category:   strings.TrimSpace(b.Category),
			StatusText: statusText,
			DetailURL:  detailURL,
		})
	}
	return items, nil
}

// ParseFanqieLibraryJSON 解析番茄分类书库分页接口（每页 30 本）。
func ParseFanqieLibraryJSON(raw string, categoryLabel string, rankOffset int) ([]domain.RadarItem, error) {
	var payload struct {
		Code int `json:"code"`
		Data struct {
			BookList []struct {
				BookID         string `json:"book_id"`
				BookName       string `json:"book_name"`
				Author         string `json:"author"`
				Abstract       string `json:"abstract"`
				WordCount      string `json:"word_count"`
				ReadCount      string `json:"read_count"`
				CreationStatus int    `json:"creation_status"`
			} `json:"book_list"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("解析番茄分类榜 JSON 失败: %w", err)
	}
	if payload.Code != 0 {
		return nil, fmt.Errorf("番茄分类榜接口返回错误码 %d", payload.Code)
	}
	items := make([]domain.RadarItem, 0, len(payload.Data.BookList))
	for i, b := range payload.Data.BookList {
		title := strings.TrimSpace(b.BookName)
		if title == "" {
			continue
		}
		statusText := "连载中"
		if b.CreationStatus == 0 {
			statusText = "已完结"
		}
		detailURL := ""
		if bid := strings.TrimSpace(b.BookID); bid != "" {
			detailURL = "https://fanqienovel.com/page/" + bid
		}
		items = append(items, domain.RadarItem{
			Rank:       rankOffset + i + 1,
			BookID:     strings.TrimSpace(b.BookID),
			Title:      title,
			Author:     strings.TrimSpace(b.Author),
			Category:   strings.TrimSpace(strings.TrimSuffix(categoryLabel, " · 新书")),
			WordCount:  strings.TrimSpace(b.WordCount),
			HeatText:   strings.TrimSpace(b.ReadCount),
			StatusText: statusText,
			Summary:    strings.TrimSpace(b.Abstract),
			DetailURL:  detailURL,
		})
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
