package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nib/internal/domain"
)

func zeroSleep(_ context.Context, _ time.Duration) error {
	return nil
}

func TestStreamOpenAICompat(t *testing.T) {
	const secretKey = "sk-openai-test-secret-9988"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+secretKey {
			t.Errorf("unexpected Authorization header: %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["stream"] != true {
			t.Errorf("expected stream=true, got %v", body["stream"])
		}
		streamOpts, ok := body["stream_options"].(map[string]any)
		if !ok || streamOpts["include_usage"] != true {
			t.Errorf("expected stream_options.include_usage=true, got %v", body["stream_options"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// 包含心跳注释、空 choices、连续无空行 data、以及末尾 usage + [DONE]。
		frames := []string{
			": keepalive\n\n",
			"data: {\"choices\":[]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"content\":\"林渊\"}}]}\n",
			"data: {\"choices\":[{\"delta\":{\"content\":\"抬手压剑。\"},\"finish_reason\":\"stop\"}]}\n\n",
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":120,\"completion_tokens\":45,\"total_tokens\":165}}\n\n",
			"data: [DONE]\n\n",
		}
		for _, f := range frames {
			_, _ = w.Write([]byte(f))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
	provider := domain.Provider{
		Name:     "OpenAI Relay",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  srv.URL + "/v1/",
		APIKey:   secretKey,
		Models:   []string{"deepseek-chat"},
		Enabled:  true,
	}

	var text strings.Builder
	var sawUsage *domain.TokenUsage
	var lastFinish string

	usage, err := client.Stream(context.Background(), provider, ChatRequest{
		Model:       "deepseek-chat",
		System:      "系统指令",
		Messages:    []ChatMessage{{Role: "user", Content: "写一段开场"}},
		Temperature: 0.8,
		MaxTokens:   2048,
	}, func(ev StreamEvent) error {
		text.WriteString(ev.Delta)
		if ev.Usage != nil {
			u := *ev.Usage
			sawUsage = &u
		}
		if ev.FinishReason != "" {
			lastFinish = ev.FinishReason
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if got := text.String(); got != "林渊抬手压剑。" {
		t.Fatalf("unexpected text: %q", got)
	}
	if usage.PromptTokens != 120 || usage.CompletionTokens != 45 || usage.TotalTokens != 165 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if sawUsage == nil || *sawUsage != usage {
		t.Fatalf("expected usage event matching %+v, got %+v", usage, sawUsage)
	}
	if lastFinish != "stop" {
		t.Fatalf("expected finishReason=stop, got %q", lastFinish)
	}
}

func TestStreamAnthropic(t *testing.T) {
	const secretKey = "sk-ant-test-secret-4455"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != secretKey {
			t.Errorf("unexpected x-api-key: %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("unexpected anthropic-version: %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["system"] != "世界铁律" {
			t.Errorf("expected top-level system='世界铁律', got %v", body["system"])
		}
		if body["stream"] != true {
			t.Errorf("expected stream=true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":300,\"output_tokens\":0}}}\n\n",
			"event: ping\ndata: {\"type\":\"ping\"}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"剑气\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"破空。\"}}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":88}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, f := range frames {
			_, _ = w.Write([]byte(f))
		}
	}))
	defer srv.Close()

	client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
	provider := domain.Provider{
		Name:     "Anthropic",
		Protocol: domain.ProtocolAnthropic,
		BaseURL:  srv.URL,
		APIKey:   secretKey,
		Models:   []string{"claude-sonnet-4"},
		Enabled:  true,
	}

	var out strings.Builder
	var finishReason string
	usage, err := client.Stream(context.Background(), provider, ChatRequest{
		Model:       "claude-sonnet-4",
		System:      "世界铁律",
		Messages:    []ChatMessage{{Role: "user", Content: "开始生成"}},
		Temperature: 0.7,
		MaxTokens:   1024,
	}, func(ev StreamEvent) error {
		out.WriteString(ev.Delta)
		if ev.FinishReason != "" {
			finishReason = ev.FinishReason
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if out.String() != "剑气破空。" {
		t.Fatalf("unexpected text: %q", out.String())
	}
	if usage.PromptTokens != 300 || usage.CompletionTokens != 88 || usage.TotalTokens != 388 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if finishReason != "end_turn" {
		t.Fatalf("expected finishReason=end_turn, got %q", finishReason)
	}
}

func TestStreamGemini(t *testing.T) {
	const secretKey = "AIzaSy-test-secret-7788"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-2.5-pro:streamGenerateContent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("expected alt=sse, got %q", r.URL.Query().Get("alt"))
		}
		if got := r.Header.Get("x-goog-api-key"); got != secretKey {
			t.Errorf("unexpected x-goog-api-key: %q", got)
		}

		var body geminiRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.SystemInstruction == nil || len(body.SystemInstruction.Parts) != 1 || body.SystemInstruction.Parts[0].Text != "都市铁律" {
			t.Errorf("unexpected systemInstruction: %+v", body.SystemInstruction)
		}
		if len(body.Contents) != 2 || body.Contents[0].Role != "user" || body.Contents[1].Role != "model" {
			t.Errorf("unexpected contents role mapping: %+v", body.Contents)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			": keepalive\n\n",
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"账户余额\"},{\"text\":\"跳动了一瞬。\"}]}}]}\n\n",
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"三千万到账。\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":210,\"candidatesTokenCount\":64,\"totalTokenCount\":274}}\n\n",
		}
		for _, f := range frames {
			_, _ = w.Write([]byte(f))
		}
	}))
	defer srv.Close()

	client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
	provider := domain.Provider{
		Name:     "Gemini",
		Protocol: domain.ProtocolGemini,
		BaseURL:  srv.URL,
		APIKey:   secretKey,
		Models:   []string{"gemini-2.5-pro"},
		Enabled:  true,
	}

	var out strings.Builder
	usage, err := client.Stream(context.Background(), provider, ChatRequest{
		Model:  "gemini-2.5-pro",
		System: "都市铁律",
		Messages: []ChatMessage{
			{Role: "user", Content: "上文"},
			{Role: "assistant", Content: "确认"},
		},
		Temperature: 0.8,
		MaxTokens:   1024,
	}, func(ev StreamEvent) error {
		out.WriteString(ev.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if out.String() != "账户余额跳动了一瞬。三千万到账。" {
		t.Fatalf("unexpected text: %q", out.String())
	}
	if usage.PromptTokens != 210 || usage.CompletionTokens != 64 || usage.TotalTokens != 274 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestErrorClassificationAndNoRetry(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantSentinel error
	}{
		{
			name:         "401 invalid key",
			status:       http.StatusUnauthorized,
			body:         `{"error":{"message":"Incorrect API key provided"}}`,
			wantSentinel: domain.ErrInvalidAPIKey,
		},
		{
			name:         "403 forbidden key",
			status:       http.StatusForbidden,
			body:         `{"error":{"message":"permission denied"}}`,
			wantSentinel: domain.ErrInvalidAPIKey,
		},
		{
			name:         "404 missing model",
			status:       http.StatusNotFound,
			body:         `{"error":{"message":"model not found"}}`,
			wantSentinel: nil,
		},
		{
			name:         "400 context overflow",
			status:       http.StatusBadRequest,
			body:         `{"error":{"code":"context_length_exceeded","message":"This model's maximum context length is 16384 tokens"}}`,
			wantSentinel: domain.ErrContextOverflow,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
			_, err := client.Stream(context.Background(), domain.Provider{
				Protocol: domain.ProtocolOpenAICompat,
				BaseURL:  srv.URL,
				APIKey:   "sk-no-retry-12345678",
			}, ChatRequest{
				Model:    "gpt-test",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			}, nil)

			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if got := attempts.Load(); got != 1 {
				t.Fatalf("expected non-retryable error to make 1 attempt, got %d", got)
			}
			if IsRetryable(err) {
				t.Fatalf("expected error not retryable: %v", err)
			}
			if tc.wantSentinel != nil && !errors.Is(err, tc.wantSentinel) {
				t.Fatalf("expected sentinel %v, got %v", tc.wantSentinel, err)
			}
		})
	}
}

func TestRetryBackoffOn429ThenSucceed(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		switch n {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit hit"}}`))
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"temporarily overloaded"}}`))
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"重试成功\"}}]}\n\ndata: [DONE]\n\n"))
		}
	}))
	defer srv.Close()

	var recordedSleeps []time.Duration
	client := NewClient(
		WithHTTPClient(srv.Client()),
		WithSleep(func(_ context.Context, d time.Duration) error {
			recordedSleeps = append(recordedSleeps, d)
			return nil
		}),
	)

	var text strings.Builder
	_, err := client.Stream(context.Background(), domain.Provider{
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  srv.URL,
		APIKey:   "sk-retry-key-12345678",
	}, ChatRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "go"}},
	}, func(ev StreamEvent) error {
		text.WriteString(ev.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("expected retry to eventually succeed, got %v", err)
	}
	if text.String() != "重试成功" {
		t.Fatalf("unexpected text: %q", text.String())
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
	if len(recordedSleeps) != 2 || recordedSleeps[0] != 1*time.Second || recordedSleeps[1] != 3*time.Second {
		t.Fatalf("unexpected backoff sleeps: %v", recordedSleeps)
	}
}

func TestRetryExhaustedReturnsRateLimit(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"still rate limited"}}`))
	}))
	defer srv.Close()

	var sleeps []time.Duration
	client := NewClient(
		WithHTTPClient(srv.Client()),
		WithSleep(func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		}),
	)

	_, err := client.Stream(context.Background(), domain.Provider{
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  srv.URL,
		APIKey:   "sk-retry-exhaust-9999",
	}, ChatRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "go"}},
	}, nil)

	if !errors.Is(err, domain.ErrUpstreamRateLimit) {
		t.Fatalf("expected ErrUpstreamRateLimit, got %v", err)
	}
	if got := attempts.Load(); got != 4 {
		t.Fatalf("expected 1 initial + 3 retries = 4 attempts, got %d", got)
	}
	wantSleeps := []time.Duration{1 * time.Second, 3 * time.Second, 8 * time.Second}
	if len(sleeps) != len(wantSleeps) {
		t.Fatalf("expected %v sleeps, got %v", wantSleeps, sleeps)
	}
	for i := range wantSleeps {
		if sleeps[i] != wantSleeps[i] {
			t.Fatalf("sleep[%d] = %v, want %v", i, sleeps[i], wantSleeps[i])
		}
	}
}

func TestNoRetryAfterDeltaAlreadyStreamed(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "99999")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"已输出一段\"}}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// 强行劫持并关闭连接，制造中途 UnexpectedEOF。
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer srv.Close()

	client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
	var gotText strings.Builder
	_, err := client.Stream(context.Background(), domain.Provider{
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  srv.URL,
		APIKey:   "sk-mid-eof-12345678",
	}, ChatRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "go"}},
	}, func(ev StreamEvent) error {
		gotText.WriteString(ev.Delta)
		return nil
	})
	if err == nil {
		t.Fatalf("expected mid-stream EOF error, got nil")
	}
	if !errors.Is(err, domain.ErrUpstreamUnavailable) {
		t.Fatalf("expected ErrUpstreamUnavailable, got %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("must NOT retry after delta streamed, got %d attempts", attempts.Load())
	}
	if gotText.String() != "已输出一段" {
		t.Fatalf("unexpected partial delta: %q", gotText.String())
	}
}

func TestMidStreamContextCancelNoGoroutineLeak(t *testing.T) {
	serverReleased := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverReleased)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"第一段\"} }]}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		// 挂起直到客户端断开取消。
		<-r.Context().Done()
	}))
	defer srv.Close()

	baseGoroutines := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))

	var cancelAt time.Time
	_, err := client.Stream(ctx, domain.Provider{
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  srv.URL,
		APIKey:   "sk-cancel-test-12345",
	}, ChatRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "长篇正文"}},
	}, func(ev StreamEvent) error {
		if ev.Delta == "第一段" {
			cancelAt = time.Now()
			cancel()
		}
		return nil
	})
	elapsed := time.Since(cancelAt)

	if !errors.Is(err, domain.ErrGenerationCanceled) {
		t.Fatalf("expected ErrGenerationCanceled, got %v", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("expected rapid abort after cancel, took %v", elapsed)
	}

	select {
	case <-serverReleased:
	case <-time.After(1 * time.Second):
		t.Fatalf("server handler did not observe client disconnect")
	}

	// 关闭空闲连接并等待 goroutine 回收。
	srv.Client().CloseIdleConnections()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseGoroutines+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("possible goroutine leak: baseline=%d, current=%d", baseGoroutines, runtime.NumGoroutine())
}

func TestSecretNonLeakageAndTestConnection(t *testing.T) {
	const leakedSecret = "sk-top-secret-do-not-leak-987654"

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(
			w,
			`{"error":{"message":"Invalid token Bearer %s for x-api-key=%s"}}`,
			leakedSecret,
			leakedSecret,
		)
	}))
	defer failSrv.Close()

	client := NewClient(WithHTTPClient(failSrv.Client()), WithSleep(zeroSleep))
	badProvider := domain.Provider{
		Name:     "Secret Provider",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  failSrv.URL,
		APIKey:   leakedSecret,
		Models:   []string{"deepseek-chat"},
		Enabled:  true,
	}

	_, streamErr := client.Stream(context.Background(), badProvider, ChatRequest{
		Model:    "deepseek-chat",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}, nil)
	if !errors.Is(streamErr, domain.ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", streamErr)
	}
	if strings.Contains(streamErr.Error(), leakedSecret) {
		t.Fatalf("API key leaked in Stream error: %q", streamErr.Error())
	}

	res, testErr := client.TestConnection(context.Background(), badProvider)
	if testErr != nil {
		t.Fatalf("TestConnection should report upstream 401 in result, got err: %v", testErr)
	}
	if res.OK {
		t.Fatalf("expected res.OK=false on 401")
	}
	if strings.Contains(res.Message, leakedSecret) {
		t.Fatalf("API key leaked in TestConnection message: %q", res.Message)
	}

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer okSrv.Close()

	okClient := NewClient(WithHTTPClient(okSrv.Client()), WithSleep(zeroSleep))
	okRes, okErr := okClient.TestConnection(context.Background(), domain.Provider{
		Name:     "Healthy Provider",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  okSrv.URL,
		APIKey:   leakedSecret,
		Models:   []string{"deepseek-chat"},
		Enabled:  true,
	})
	if okErr != nil {
		t.Fatalf("unexpected TestConnection error: %v", okErr)
	}
	if !okRes.OK || okRes.ModelUsed != "deepseek-chat" || okRes.LatencyMs < 0 {
		t.Fatalf("unexpected healthy TestConnection result: %+v", okRes)
	}
}

func TestHTTP200NonSSEAndEmptyResponsesFailCleanly(t *testing.T) {
	const secretKey = "sk-non-sse-secret-556677"

	t.Run("200 HTML page fails without retry and TestConnection is false", func(t *testing.T) {
		var attempts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Welcome to wrong base URL</body></html>"))
		}))
		defer srv.Close()

		client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
		provider := domain.Provider{
			Name:     "Wrong URL",
			Protocol: domain.ProtocolOpenAICompat,
			BaseURL:  srv.URL,
			APIKey:   secretKey,
			Models:   []string{"deepseek-chat"},
			Enabled:  true,
		}

		_, err := client.Stream(context.Background(), provider, ChatRequest{
			Model:    "deepseek-chat",
			Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		}, nil)
		if err == nil {
			t.Fatalf("expected error on HTTP 200 HTML page, got nil")
		}
		if IsRetryable(err) {
			t.Fatalf("expected HTTP 200 HTML error non-retryable, got %v", err)
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("expected 1 attempt on non-retryable HTML response, got %d", got)
		}

		res, testErr := client.TestConnection(context.Background(), provider)
		if testErr != nil {
			t.Fatalf("unexpected TestConnection err: %v", testErr)
		}
		if res.OK {
			t.Fatalf("expected TestConnection OK=false for HTML response, got %+v", res)
		}
	})

	t.Run("200 JSON error body classifies error and scrubs API key", func(t *testing.T) {
		var attempts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"error":{"type":"authentication_error","message":"bad key %s"}}`, secretKey)
		}))
		defer srv.Close()

		client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
		provider := domain.Provider{
			Name:     "Relay JSON Error",
			Protocol: domain.ProtocolOpenAICompat,
			BaseURL:  srv.URL,
			APIKey:   secretKey,
			Models:   []string{"deepseek-chat"},
			Enabled:  true,
		}

		_, err := client.Stream(context.Background(), provider, ChatRequest{
			Model:    "deepseek-chat",
			Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		}, nil)
		if !errors.Is(err, domain.ErrInvalidAPIKey) {
			t.Fatalf("expected ErrInvalidAPIKey on 200 JSON auth error, got %v", err)
		}
		if strings.Contains(err.Error(), secretKey) {
			t.Fatalf("API key leaked in 200 JSON error: %q", err.Error())
		}
		if attempts.Load() != 1 {
			t.Fatalf("expected no retry on 200 JSON auth error, got %d attempts", attempts.Load())
		}

		res, testErr := client.TestConnection(context.Background(), provider)
		if testErr != nil {
			t.Fatalf("unexpected TestConnection err: %v", testErr)
		}
		if res.OK {
			t.Fatalf("expected TestConnection OK=false for 200 JSON error, got %+v", res)
		}
		if strings.Contains(res.Message, secretKey) {
			t.Fatalf("API key leaked in TestConnection message: %q", res.Message)
		}
	})

	t.Run("200 empty SSE EOF retries on Stream and fails TestConnection across all 3 protocols", func(t *testing.T) {
		protocols := []domain.ProviderProtocol{
			domain.ProtocolOpenAICompat,
			domain.ProtocolAnthropic,
			domain.ProtocolGemini,
		}
		for _, proto := range protocols {
			t.Run(string(proto), func(t *testing.T) {
				var attempts atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					n := attempts.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(http.StatusOK)
					if n == 1 {
						// 首次请求返回空体 / 仅保活注释即 EOF
						_, _ = w.Write([]byte(": keepalive\n\n"))
						return
					}
					switch proto {
					case domain.ProtocolOpenAICompat:
						_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"恢复\"}}]}\n\ndata: [DONE]\n\n"))
					case domain.ProtocolAnthropic:
						_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"恢复\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
					case domain.ProtocolGemini:
						_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"恢复\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
					}
				}))
				defer srv.Close()

				client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
				provider := domain.Provider{
					Name:     "Flaky Empty",
					Protocol: proto,
					BaseURL:  srv.URL,
					APIKey:   secretKey,
					Models:   []string{"test-model"},
					Enabled:  true,
				}

				var out strings.Builder
				_, err := client.Stream(context.Background(), provider, ChatRequest{
					Model:    "test-model",
					Messages: []ChatMessage{{Role: "user", Content: "hi"}},
				}, func(ev StreamEvent) error {
					out.WriteString(ev.Delta)
					return nil
				})
				if err != nil {
					t.Fatalf("expected Stream to retry empty 200 EOF and succeed, got %v", err)
				}
				if attempts.Load() != 2 || out.String() != "恢复" {
					t.Fatalf("expected 2 attempts and delta '恢复', got attempts=%d text=%q", attempts.Load(), out.String())
				}

				// 持续空 EOF 的服务器对 TestConnection 必须返回 OK=false。
				alwaysEmptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(http.StatusOK)
				}))
				defer alwaysEmptySrv.Close()

				emptyClient := NewClient(WithHTTPClient(alwaysEmptySrv.Client()), WithSleep(zeroSleep))
				emptyProvider := provider
				emptyProvider.BaseURL = alwaysEmptySrv.URL

				res, testErr := emptyClient.TestConnection(context.Background(), emptyProvider)
				if testErr != nil {
					t.Fatalf("unexpected TestConnection error: %v", testErr)
				}
				if res.OK {
					t.Fatalf("expected TestConnection OK=false for empty 200 SSE stream, got %+v", res)
				}
			})
		}
	})
}

func TestListModelsProtocolsAndSanitization(t *testing.T) {
	t.Run("openai_compat", func(t *testing.T) {
		var gotPath, gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-reasoner"},{"id":"deepseek-chat"},{"id":"deepseek-chat"}]}`))
		}))
		defer srv.Close()

		client := NewClient(WithHTTPClient(srv.Client()))
		models, err := client.ListModels(context.Background(), domain.Provider{
			Protocol: domain.ProtocolOpenAICompat,
			BaseURL:  srv.URL + "/v1/chat/completions",
			APIKey:   "sk-test-openai-secret",
		})
		if err != nil {
			t.Fatalf("ListModels openai_compat failed: %v", err)
		}
		if gotPath != "/v1/models" {
			t.Fatalf("expected /v1/models, got %q", gotPath)
		}
		if gotAuth != "Bearer sk-test-openai-secret" {
			t.Fatalf("unexpected auth header: %q", gotAuth)
		}
		if len(models) != 2 || models[0] != "deepseek-chat" || models[1] != "deepseek-reasoner" {
			t.Fatalf("unexpected models: %v", models)
		}
	})

	t.Run("anthropic", func(t *testing.T) {
		var gotPath, gotKey, gotVer string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotKey = r.Header.Get("x-api-key")
			gotVer = r.Header.Get("anthropic-version")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-20250514"},{"id":"claude-3-7-sonnet-latest"}]}`))
		}))
		defer srv.Close()

		client := NewClient(WithHTTPClient(srv.Client()))
		models, err := client.ListModels(context.Background(), domain.Provider{
			Protocol: domain.ProtocolAnthropic,
			BaseURL:  srv.URL,
			APIKey:   "sk-ant-secret",
		})
		if err != nil {
			t.Fatalf("ListModels anthropic failed: %v", err)
		}
		if gotPath != "/v1/models" || gotKey != "sk-ant-secret" || gotVer == "" {
			t.Fatalf("unexpected anthropic request: path=%q key=%q ver=%q", gotPath, gotKey, gotVer)
		}
		if len(models) != 2 || models[0] != "claude-3-7-sonnet-latest" || models[1] != "claude-sonnet-4-20250514" {
			t.Fatalf("unexpected models: %v", models)
		}
	})

	t.Run("gemini filters generateContent and strips prefix", func(t *testing.T) {
		var gotPath, gotKey string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotKey = r.Header.Get("x-goog-api-key")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[
				{"name":"models/gemini-2.5-pro","supportedGenerationMethods":["generateContent","countTokens"]},
				{"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"]},
				{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"]}
			]}`))
		}))
		defer srv.Close()

		client := NewClient(WithHTTPClient(srv.Client()))
		models, err := client.ListModels(context.Background(), domain.Provider{
			Protocol: domain.ProtocolGemini,
			BaseURL:  srv.URL,
			APIKey:   "AIzaSy-secret-key",
		})
		if err != nil {
			t.Fatalf("ListModels gemini failed: %v", err)
		}
		if gotPath != "/v1beta/models" || gotKey != "AIzaSy-secret-key" {
			t.Fatalf("unexpected gemini request: path=%q key=%q", gotPath, gotKey)
		}
		if len(models) != 2 || models[0] != "gemini-2.5-flash" || models[1] != "gemini-2.5-pro" {
			t.Fatalf("unexpected gemini models: %v", models)
		}
	})

	t.Run("401 sanitizes api key", func(t *testing.T) {
		secret := "sk-super-secret-models-key-9988"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key ` + secret + `"}}`))
		}))
		defer srv.Close()

		client := NewClient(WithHTTPClient(srv.Client()))
		_, err := client.ListModels(context.Background(), domain.Provider{
			Protocol: domain.ProtocolOpenAICompat,
			BaseURL:  srv.URL,
			APIKey:   secret,
		})
		if err == nil {
			t.Fatalf("expected error on 401")
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("secret leaked in ListModels error: %v", err)
		}
	})
}

func TestStreamReasoningDeltaAcrossProtocols(t *testing.T) {
	t.Run("openai reasoning_content", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"先想情节\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"正文开始。\"},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		}))
		defer srv.Close()

		client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
		var reason, text strings.Builder
		_, err := client.Stream(context.Background(), domain.Provider{
			Protocol: domain.ProtocolOpenAICompat,
			BaseURL:  srv.URL,
			Enabled:  true,
		}, ChatRequest{Model: "deepseek-reasoner", Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, func(ev StreamEvent) error {
			reason.WriteString(ev.ReasoningDelta)
			text.WriteString(ev.Delta)
			return nil
		})
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if reason.String() != "先想情节" || text.String() != "正文开始。" {
			t.Fatalf("got reason=%q text=%q", reason.String(), text.String())
		}
	})

	t.Run("gemini thought part separated from content", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"内部推演\",\"thought\":true}]}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"正式正文。\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
		}))
		defer srv.Close()

		client := NewClient(WithHTTPClient(srv.Client()), WithSleep(zeroSleep))
		var reason, text strings.Builder
		_, err := client.Stream(context.Background(), domain.Provider{
			Protocol: domain.ProtocolGemini,
			BaseURL:  srv.URL,
			Enabled:  true,
		}, ChatRequest{Model: "gemini-2.5-pro", Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, func(ev StreamEvent) error {
			reason.WriteString(ev.ReasoningDelta)
			text.WriteString(ev.Delta)
			return nil
		})
		if err != nil {
			t.Fatalf("Stream error: %v", err)
		}
		if reason.String() != "内部推演" || text.String() != "正式正文。" {
			t.Fatalf("got reason=%q text=%q", reason.String(), text.String())
		}
	})
}
