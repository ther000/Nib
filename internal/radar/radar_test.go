package radar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nib/internal/domain"
)

func TestListBoardsContainsQidianAndFanqie(t *testing.T) {
	boards := ListBoards()
	qidianCount, fanqieCount := 0, 0
	for _, b := range boards {
		if b.Platform == domain.RadarPlatformQidian {
			qidianCount++
		} else if b.Platform == domain.RadarPlatformFanqie {
			fanqieCount++
		}
	}
	if qidianCount < 9 || fanqieCount < 10 {
		t.Fatalf("unexpected board counts: qidian=%d fanqie=%d", qidianCount, fanqieCount)
	}
}

func TestParseQidianMobileHTML(t *testing.T) {
	sample := `<!DOCTYPE html><html><body><script id="vite-plugin-ssr_pageContext" type="application/json">{"pageContext":{"pageProps":{"pageData":{"records":[{"rankNum":1,"bid":"1040765595","bName":"夜无疆","bAuth":"辰东","cat":"玄幻","subCat":"东方玄幻","cnt":"432.36万字","desc":"那一天太阳落下再也没有升起"},{"rankNum":2,"bid":"1036010291","bName":"玄鉴仙族","bAuth":"季越人","cat":"仙侠","subCat":"幻想修仙","cnt":"350.1万字","desc":"陆江仙一觉醒来成为青池宗一面古镜"}]}}}}</script></body></html>`

	items, err := ParseQidianMobileHTML(sample)
	if err != nil {
		t.Fatalf("ParseQidianMobileHTML failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Rank != 1 || items[0].Title != "夜无疆" || items[0].Author != "辰东" || items[0].DetailURL != "https://book.qidian.com/info/1040765595/" {
		t.Fatalf("unexpected item[0]: %+v", items[0])
	}
}

func TestParseFanqieRankHTML(t *testing.T) {
	// 贴近番茄线上真实 IIFE 包装结构（尾随换行、}、)() 与 </script>）
	sample := `<!doctype html><html><body><script>(function(){
		window.__INITIAL_STATE__={"common":{"css":"@font-face{font-family:DNMrHsV173Pd4pgy;src:url(https://example.com/font.woff2) format(\"woff2\");}"},"rank":{"book_list":[{"bookId":"7077516958534470656","bookName":"凡骨","author":"壹更大师","abstract":"一介凡骨斩妖除魔；含引号\"与花括号{}测试","wordNumber":"8323357","read_count":"563206","creationStatus":"0","currentPos":1}]}};
	})()
	</script></body></html>`

	items, fontCSS, fontFamily, err := ParseFanqieRankHTML(sample, "仙侠")
	if err != nil {
		t.Fatalf("ParseFanqieRankHTML failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if fontFamily != "DNMrHsV173Pd4pgy" || !strings.Contains(fontCSS, "font.woff2") {
		t.Fatalf("unexpected font metadata: family=%q css=%q", fontFamily, fontCSS)
	}
	if items[0].Title != "凡骨" || items[0].Category != "仙侠" || items[0].StatusText != "已完结" || items[0].HeatText != "56.3万人在读" || items[0].WordCount != "832.3万字" {
		t.Fatalf("unexpected fanqie item: %+v", items[0])
	}
}

func TestParseErrorsOnBrokenHTML(t *testing.T) {
	if _, err := ParseQidianMobileHTML("<html><body>empty</body></html>"); err == nil {
		t.Fatalf("expected error on broken Qidian HTML")
	}
	if _, _, _, err := ParseFanqieRankHTML("<html><body>empty</body></html>", "玄幻"); err == nil {
		t.Fatalf("expected error on broken Fanqie HTML")
	}
}

func TestFetcherCacheAndForceRefresh(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<script id="vite-plugin-ssr_pageContext" type="application/json">{"pageContext":{"pageProps":{"pageData":{"records":[{"rankNum":1,"bid":"100","bName":"测试畅销书","bAuth":"作者甲","cat":"玄幻","cnt":"100万字","desc":"简介"}]}}}}</script>`))
	}))
	defer srv.Close()

	f := NewFetcher()
	f.cacheTTL = time.Minute
	f.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req2 := req.Clone(req.Context())
			req2.URL.Scheme = "http"
			req2.URL.Host = strings.TrimPrefix(srv.URL, "http://")
			return http.DefaultTransport.RoundTrip(req2)
		}),
	}

	b1, err := f.FetchBoard(context.Background(), domain.RadarPlatformQidian, "hotsales", false)
	if err != nil || b1.Cached || len(b1.Items) != 1 {
		t.Fatalf("first fetch unexpected: %+v, err=%v", b1, err)
	}
	b2, err := f.FetchBoard(context.Background(), domain.RadarPlatformQidian, "hotsales", false)
	if err != nil || !b2.Cached || hits.Load() != 1 {
		t.Fatalf("expected cached hit without extra HTTP request: cached=%v hits=%d err=%v", b2.Cached, hits.Load(), err)
	}
	b3, err := f.FetchBoard(context.Background(), domain.RadarPlatformQidian, "hotsales", true)
	if err != nil || b3.Cached || hits.Load() != 2 {
		t.Fatalf("expected forceRefresh to bypass cache: cached=%v hits=%d err=%v", b3.Cached, hits.Load(), err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
