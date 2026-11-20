package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nib/app"
	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/exporter"
	"nib/internal/llm"
	"nib/internal/memory"
	"nib/internal/pipeline"
	"nib/internal/store"
)

func TestPhase1EndToEndBackendWorkflow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nib.db")
	exportDir := filepath.Join(tmpDir, "exports")

	const (
		rawSecretKey = "sk-e2e-secret-token-9988a8f2"
		modelName    = "mock-xuanhuan-v1"
	)

	// 1. 构造模拟 OpenAI 兼容流式 SSE 服务端（同时服务 TestProvider 与 GenerateChapter）
	var (
		seenAuthHeader  string
		seenUserPrompts []string
	)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		seenAuthHeader = r.Header.Get("Authorization")
		if seenAuthHeader != "Bearer "+rawSecretKey {
			http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
			return
		}

		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		for _, m := range body.Messages {
			if m.Role == "user" {
				seenUserPrompts = append(seenUserPrompts, m.Content)
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			"残阵灵光骤灭，焦糊味混着血腥气在石室散开。\n\n",
			"萧衍按住左肩暗伤，目光落在王执事尸身腰间的青玉瓶上。\n\n",
			"就在指尖触到瓶身的刹那，石门外陡然响起急促的破空声。",
		}
		// TestConnection 短探针仅返回 OK
		if len(body.Messages) == 1 && strings.Contains(body.Messages[0].Content, "OK") {
			chunks = []string{"OK"}
		}

		_, _ = fmt.Fprint(w, ": keepalive\n\n")
		for _, delta := range chunks {
			payload, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{
					{"delta": map[string]string{"content": delta}},
				},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
		}
		usagePayload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop"}},
			"usage": map[string]int{
				"prompt_tokens":     320,
				"completion_tokens": 96,
				"total_tokens":      416,
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", usagePayload)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer mockServer.Close()

	// 2. 真实装配 Store + Exporter + LLMClient + Compiler + Pipeline + App Services
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	storeExp := exporter.NewStoreExporter(st)
	pipeExp := exporter.New()
	llmClient := llm.NewClient(llm.WithHTTPClient(mockServer.Client()))
	comp := compiler.New(st)
	runner := pipeline.New(st, comp, llmClient, pipeExp)

	rt := app.NewRuntime()
	rt.Init(app.RuntimeDeps{
		Store:    st,
		Exporter: storeExp,
		LLM:      llmClient,
		Compiler: comp,
		Pipeline: runner,
	})
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("rt.Close failed: %v", err)
		}
	}()

	lib := app.NewLibraryService(rt)
	atlas := app.NewAtlasService(rt)
	settings := app.NewSettingsService(rt)
	studio := app.NewStudioService(rt)

	// 3. 创建玄幻书籍（带导出目录）、第一卷与第 1/2 章
	book, err := lib.CreateBook(ctx, domain.CreateBookInput{
		Title:              "荒古剑尊",
		Genre:              domain.GenreXuanhuan,
		Premise:            "外门废柴凭残破剑碑逆推功法弱点，一路越阶斩敌。",
		GoldenFinger:       "古剑碑可推演任何接触过的功法破绽，每日限一次。",
		TargetChapterWords: 2500,
		ExportDir:          exportDir,
	})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}

	vol, err := lib.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID:     book.ID,
		Seq:        1,
		Title:      "青云起",
		Goal:       "外门大比夺魁并揭露黑煞宗卧底。",
		ClimaxPlan: "石室残阵反杀内门执事。",
	})
	if err != nil {
		t.Fatalf("UpsertVolume failed: %v", err)
	}

	ch1, err := lib.CreateChapter(ctx, domain.CreateChapterInput{
		BookID:         book.ID,
		VolumeID:       vol.ID,
		Title:          "残阵反杀",
		EmotionTag:     domain.EmotionBurst,
		HookType:       domain.HookCrisis,
		HookNote:       "刚夺得破障丹，石门外已有追兵杀至。",
		OutlineSummary: "萧衍借石室残阵引诱王执事入局，以伤换命反杀夺宝。",
	})
	if err != nil {
		t.Fatalf("CreateChapter ch1 failed: %v", err)
	}
	ch2, err := lib.CreateChapter(ctx, domain.CreateChapterInput{
		BookID:     book.ID,
		VolumeID:   vol.ID,
		Title:      "夜遁荒山",
		EmotionTag: domain.EmotionHarvest,
		HookType:   domain.HookMystery,
	})
	if err != nil {
		t.Fatalf("CreateChapter ch2 failed: %v", err)
	}
	if ch1.GlobalSeq != 1 || ch2.GlobalSeq != 2 {
		t.Fatalf("unexpected chapter seqs: ch1=%d ch2=%d", ch1.GlobalSeq, ch2.GlobalSeq)
	}

	// 4. 在 Atlas 添加世界铁律 + 角色实体，并绑定至第 1 章节拍卡
	inv, err := atlas.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:      book.ID,
		Kind:        domain.EntityKindRule,
		Name:        "战力越阶铁律",
		IsInvariant: true,
		Summary:     "严禁无代价跨大阶碾压，越阶必借阵法或消耗底牌。",
	})
	if err != nil {
		t.Fatalf("UpsertEntity invariant failed: %v", err)
	}
	hero, err := atlas.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:  book.ID,
		Kind:    domain.EntityKindCharacter,
		Name:    "萧衍",
		Aliases: []string{"萧师弟"},
		Summary: "天渊宗外门弟子，行事狠辣克制。",
		Attributes: map[string]string{
			"realm":  "炼气九层",
			"injury": "左肩旧创未愈",
		},
	})
	if err != nil {
		t.Fatalf("UpsertEntity character failed: %v", err)
	}

	beats, err := lib.ReplaceBeats(ctx, ch1.ID, []domain.Beat{
		{
			Stage:       domain.BeatStageTurn,
			Summary:     "萧衍启动石室残阵困住王执事。",
			POVEntityID: &hero.ID,
			EntityIDs:   []int64{hero.ID},
			TargetWords: 600,
		},
		{
			Stage:       domain.BeatStageHook,
			Summary:     "反杀夺得破障丹，石门外突现宗门追兵破空声。",
			POVEntityID: &hero.ID,
			EntityIDs:   []int64{hero.ID},
			TargetWords: 400,
		},
	})
	if err != nil || len(beats) != 2 {
		t.Fatalf("ReplaceBeats err=%v len=%d", err, len(beats))
	}

	// 5. 添加供应商并验证脱敏掩码、连通性测速及 draft 路由配置
	provPub, err := settings.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "MockOpenAI",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  mockServer.URL + "/v1",
		APIKey:   rawSecretKey,
		Models:   []string{modelName},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}
	if !provPub.HasAPIKey || provPub.APIKeyMasked != "sk-...a8f2" {
		t.Fatalf("UpsertProvider mask mismatch: has=%v masked=%q", provPub.HasAPIKey, provPub.APIKeyMasked)
	}

	providers, err := settings.ListProviders(ctx)
	if err != nil || len(providers) != 1 {
		t.Fatalf("ListProviders err=%v len=%d", err, len(providers))
	}
	if providers[0].APIKeyMasked != "sk-...a8f2" || strings.Contains(providers[0].APIKeyMasked, "secret-token") {
		t.Fatalf("ListProviders leaked or mis-masked key: %+v", providers[0])
	}

	testRes, err := settings.TestProvider(ctx, provPub.ID)
	if err != nil || !testRes.OK {
		t.Fatalf("TestProvider failed: res=%+v err=%v", testRes, err)
	}

	provID := provPub.ID
	if _, err := settings.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageDraft,
		ProviderID:  &provID,
		ModelName:   modelName,
		Temperature: 0.75,
		MaxTokens:   2048,
	}); err != nil {
		t.Fatalf("SaveModelRoute failed: %v", err)
	}

	// 6. 预览作战简报：验证 P0 包含世界铁律，P2 包含出场角色萧衍
	briefing, err := studio.PreviewBriefing(ctx, ch1.ID, domain.BriefingOptions{})
	if err != nil {
		t.Fatalf("PreviewBriefing failed: %v", err)
	}
	combinedPrompt := briefing.SystemPrompt + "\n" + briefing.UserPrompt
	if !strings.Contains(combinedPrompt, inv.Name) || !strings.Contains(combinedPrompt, inv.Summary) {
		t.Fatalf("briefing missing invariant %q", inv.Name)
	}
	if !strings.Contains(combinedPrompt, hero.Name) || !strings.Contains(combinedPrompt, "炼气九层") {
		t.Fatalf("briefing missing character %q or attributes", hero.Name)
	}

	// 7. 生成第 1 章（先通过 StudioService 走完整绑定通道，再验证流式分片 & Markdown 导出）
	var emittedChunks []domain.GenChunk
	genChap, err := runner.GenerateChapter(ctx, ch1.ID, domain.GenerateChapterOptions{}, func(c domain.GenChunk) error {
		emittedChunks = append(emittedChunks, c)
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateChapter failed: %v", err)
	}
	if genChap.Status != domain.ChapterStatusReviewing {
		t.Fatalf("generated chapter status = %q, want %q", genChap.Status, domain.ChapterStatusReviewing)
	}
	if !strings.Contains(genChap.Content, "萧衍按住左肩暗伤") || genChap.WordCount == 0 {
		t.Fatalf("unexpected generated content: %q (words=%d)", genChap.Content, genChap.WordCount)
	}
	if len(emittedChunks) < 3 {
		t.Fatalf("expected streaming chunks emitted, got %d", len(emittedChunks))
	}

	// 验证单向 Markdown 自动镜像已写入磁盘
	expectedMD := filepath.Join(exportDir, "荒古剑尊", "第01卷_青云起", "第001章_残阵反杀.md")
	mdBytes, err := os.ReadFile(expectedMD)
	if err != nil {
		t.Fatalf("expected exported markdown at %q: %v", expectedMD, err)
	}
	if !strings.Contains(string(mdBytes), "萧衍按住左肩暗伤") {
		t.Fatalf("exported markdown missing generated prose: %s", string(mdBytes))
	}

	// 验证第 2 章简报能读到第 1 章尾部衔接内容（P4）
	ch2Briefing, err := studio.PreviewBriefing(ctx, ch2.ID, domain.BriefingOptions{})
	if err != nil {
		t.Fatalf("PreviewBriefing ch2 failed: %v", err)
	}
	if !strings.Contains(ch2Briefing.UserPrompt, "破空声") {
		t.Fatalf("ch2 briefing missing ch1 tail continuity")
	}

	// 8. 审改台编辑保存草稿 -> 验证旧版正文已归档至 chapter_versions，且磁盘 .md 同步更新
	editedContent := genChap.Content + "\n\n萧衍眼神一沉，反手掐诀震碎石灯。"
	savedChap, err := lib.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID: ch1.ID,
		Content:   editedContent,
		Status:    domain.ChapterStatusReviewing,
	})
	if err != nil {
		t.Fatalf("SaveChapterDraft failed: %v", err)
	}
	if savedChap.Content != editedContent {
		t.Fatalf("saved content mismatch")
	}

	versions, err := studio.ListChapterVersions(ctx, ch1.ID)
	if err != nil || len(versions) == 0 {
		t.Fatalf("expected archived chapter_versions after overwrite, err=%v len=%d", err, len(versions))
	}
	if versions[0].Content != genChap.Content {
		t.Fatalf("archived version content = %q, want original AI draft %q", versions[0].Content, genChap.Content)
	}

	mdAfterEdit, err := os.ReadFile(expectedMD)
	if err != nil || !strings.Contains(string(mdAfterEdit), "反手掐诀震碎石灯") {
		t.Fatalf("markdown mirror not updated after SaveChapterDraft: err=%v", err)
	}

	// 9. 回滚至旧版本 -> 验证当前编辑版归档为 restore_backup，正文恢复为原稿，磁盘 .md 同步回滚
	restoredChap, err := studio.RestoreVersion(ctx, versions[0].ID)
	if err != nil {
		t.Fatalf("RestoreVersion failed: %v", err)
	}
	if restoredChap.Content != genChap.Content {
		t.Fatalf("restored content mismatch: got %q, want %q", restoredChap.Content, genChap.Content)
	}

	versionsAfterRestore, err := studio.ListChapterVersions(ctx, ch1.ID)
	if err != nil || len(versionsAfterRestore) < 2 {
		t.Fatalf("expected restore_backup archived, err=%v len=%d", err, len(versionsAfterRestore))
	}
	if versionsAfterRestore[0].Source != domain.VersionSourceRestoreBackup || versionsAfterRestore[0].Content != editedContent {
		t.Fatalf("expected newest version to be restore_backup of editedContent, got source=%q", versionsAfterRestore[0].Source)
	}

	// 10. 定稿章节并触发整书导出（验证 00_设定与铁律汇总.md 产出）
	finalChap, err := lib.FinalizeChapter(ctx, ch1.ID)
	if err != nil {
		t.Fatalf("FinalizeChapter failed: %v", err)
	}
	if finalChap.Status != domain.ChapterStatusFinalized {
		t.Fatalf("final status = %q, want %q", finalChap.Status, domain.ChapterStatusFinalized)
	}

	expRes, err := lib.ExportBookMarkdown(ctx, book.ID)
	if err != nil {
		t.Fatalf("ExportBookMarkdown failed: %v", err)
	}
	if expRes.ChapterCount != 2 {
		t.Fatalf("ExportBookMarkdown chapterCount = %d, want 2", expRes.ChapterCount)
	}
	atlasMD := filepath.Join(expRes.BookDir, "00_设定与铁律汇总.md")
	atlasBytes, err := os.ReadFile(atlasMD)
	if err != nil || !strings.Contains(string(atlasBytes), inv.Name) {
		t.Fatalf("atlas summary markdown missing invariant: err=%v", err)
	}
}

type observingPipeline struct {
	inner   app.PipelineRunner
	onDelta func()
}

func (p *observingPipeline) GenerateChapter(
	ctx context.Context,
	chapterID int64,
	opts domain.GenerateChapterOptions,
	emit func(domain.GenChunk) error,
) (domain.Chapter, error) {
	return p.inner.GenerateChapter(ctx, chapterID, opts, func(c domain.GenChunk) error {
		if c.Kind == domain.GenChunkDelta && p.onDelta != nil {
			p.onDelta()
		}
		return emit(c)
	})
}

func TestRuntimeCloseCancelsInFlightPipelineBeforeClosingDB(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "shutdown.db")

	deltaReceived := make(chan struct{})
	var deltaOnce bool
	releaseStream := make(chan struct{})

	// 模拟长时间流式响应，测试 OnBeforeQuit -> Runtime.Close() 优雅取消并保底落库后再关库。
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]string{"content": "半截已流出的正文内容。"}},
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}

		select {
		case <-r.Context().Done():
		case <-releaseStream:
		}
	}))
	defer slowServer.Close()

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	llmClient := llm.NewClient(llm.WithHTTPClient(slowServer.Client()))
	comp := compiler.New(st)
	realRunner := pipeline.New(st, comp, llmClient, nil)
	runner := &observingPipeline{
		inner: realRunner,
		onDelta: func() {
			if !deltaOnce {
				deltaOnce = true
				close(deltaReceived)
			}
		},
	}

	rt := app.NewRuntime()
	rt.Init(app.RuntimeDeps{
		Store:    st,
		LLM:      llmClient,
		Compiler: comp,
		Pipeline: runner,
	})

	lib := app.NewLibraryService(rt)
	settings := app.NewSettingsService(rt)
	studio := app.NewStudioService(rt)

	book, _ := lib.CreateBook(ctx, domain.CreateBookInput{Title: "测试书", Genre: domain.GenreGeneral})
	vol, _ := lib.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Seq: 1, Title: "第一卷"})
	ch, _ := lib.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第一章"})
	prov, _ := settings.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "SlowMock",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  slowServer.URL + "/v1",
		APIKey:   "sk-slow-mock-key-1234",
		Models:   []string{"slow-model"},
		Enabled:  true,
	})
	pid := prov.ID
	_, _ = settings.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageDraft,
		ProviderID:  &pid,
		ModelName:   "slow-model",
		Temperature: 0.7,
		MaxTokens:   1024,
	})

	genDone := make(chan struct{})
	var genChap domain.Chapter
	var genErr error
	go func() {
		defer close(genDone)
		genChap, genErr = studio.GenerateChapter(ctx, ch.ID, domain.GenerateChapterOptions{}, nil)
	}()

	select {
	case <-deltaReceived:
	case <-time.After(3 * time.Second):
		close(releaseStream)
		t.Fatalf("timed out waiting for first streamed delta")
	}

	// 模拟 OnBeforeQuit：Close() 必须先取消流式任务、等待 partial 写库完成，再关闭 SQLite。
	if err := rt.Close(); err != nil {
		t.Fatalf("rt.Close failed: %v", err)
	}
	<-genDone
	if genChap.Status != domain.ChapterStatusPartial || !strings.Contains(genChap.Content, "半截已流出的正文内容") {
		t.Fatalf("expected partial chapter saved before DB close, got status=%q content=%q err=%v", genChap.Status, genChap.Content, genErr)
	}

	// 重新打开同一数据库文件，确认 partial 正文已真实落盘持久化！
	st2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store failed: %v", err)
	}
	defer st2.Close()
	persisted, err := st2.GetChapter(ctx, ch.ID)
	if err != nil {
		t.Fatalf("GetChapter after reopen failed: %v", err)
	}
	if persisted.Status != domain.ChapterStatusPartial || !strings.Contains(persisted.Content, "半截已流出的正文内容") {
		t.Fatalf("persisted chapter mismatch after graceful shutdown: status=%q content=%q", persisted.Status, persisted.Content)
	}
}

func TestPhase2MemoryEndToEndWorkflow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nib-phase2.db")

	const (
		rawSecretKey = "sk-phase2-extract-key-7744b9c1"
		extractModel = "mock-extractor-v1"
	)

	var (
		heroID int64
		itemID int64
	)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+rawSecretKey {
			http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
			return
		}

		extractJSON, _ := json.Marshal(map[string]any{
			"causal_summary": "林渊于石室残阵反杀王执事，夺得破障丹与血玉令牌，左肩受阴雷暗伤，连夜撤离坊市。",
			"new_entities":   []any{},
			"state_patches": []map[string]any{
				{
					"entity_id":   itemID,
					"entity_name": "破障丹",
					"attr_key":    "owner",
					"prev_value":  "王执事",
					"new_value":   "林渊",
					"severity":    "high_risk",
					"reason":      "击杀王执事后战利品易主",
				},
				{
					"entity_id":   heroID,
					"entity_name": "林渊",
					"attr_key":    "injury",
					"prev_value":  "完好",
					"new_value":   "左肩阴雷暗伤",
					"severity":    "normal",
					"reason":      "残阵反杀时被阴雷擦伤",
				},
			},
			"foreshadowing_updates": []map[string]any{
				{
					"action":              "plant",
					"foreshadowing_id":    0,
					"title":               "血玉令牌密库",
					"description":         "血玉令牌背面古阵纹指向三百年前失踪灵脉。",
					"target_resolve_seq":  10,
					"involved_entity_ids": []int64{heroID},
				},
			},
		})

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]string{"content": string(extractJSON)}},
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		usagePayload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 220, "completion_tokens": 110, "total_tokens": 330},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", usagePayload)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer mockServer.Close()

	// 1. 真实装配 SQLite (001+002) + MemoryExtractor + Phase 2 Compiler + Services
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	llmClient := llm.NewClient(llm.WithHTTPClient(mockServer.Client()))
	comp := compiler.New(st, compiler.WithMemorySource(st))
	runner := pipeline.New(st, comp, llmClient, exporter.New())
	extractor := memory.NewExtractor(st, llmClient)

	rt := app.NewRuntime()
	rt.Init(app.RuntimeDeps{
		Store:          st,
		Exporter:       exporter.NewStoreExporter(st),
		LLM:            llmClient,
		Compiler:       comp,
		Pipeline:       runner,
		MemoryStore:    st,
		DeltaExtractor: extractor,
	})
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("rt.Close failed: %v", err)
		}
	}()

	lib := app.NewLibraryService(rt)
	atlas := app.NewAtlasService(rt)
	settings := app.NewSettingsService(rt)
	studio := app.NewStudioService(rt)
	memSvc := app.NewMemoryService(rt)

	// 2. 创建书、卷、3 章（ch2/ch3 设为 outlined 供测试下游 upstream_dirty）、林渊 + 破障丹
	book, err := lib.CreateBook(ctx, domain.CreateBookInput{
		Title:        "太初剑渊",
		Genre:        domain.GenreXianxia,
		Premise:      "散修林渊凭古阵盘于修真界步步为营。",
		GoldenFinger: "残缺古阵盘可反推灵阵破绽。",
	})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}
	vol, err := lib.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: book.ID,
		Seq:    1,
		Title:  "青云起",
		Goal:   "坊市反杀夺丹并逃出黑煞宗封锁。",
	})
	if err != nil {
		t.Fatalf("UpsertVolume failed: %v", err)
	}

	ch1, err := lib.CreateChapter(ctx, domain.CreateChapterInput{
		BookID:     book.ID,
		VolumeID:   vol.ID,
		Title:      "残阵反杀",
		EmotionTag: domain.EmotionBurst,
		HookType:   domain.HookCrisis,
	})
	if err != nil {
		t.Fatalf("CreateChapter ch1 failed: %v", err)
	}
	ch2, err := lib.CreateChapter(ctx, domain.CreateChapterInput{
		BookID:         book.ID,
		VolumeID:       vol.ID,
		Title:          "夜遁荒山",
		EmotionTag:     domain.EmotionSuppress,
		HookType:       domain.HookMystery,
		OutlineSummary: "林渊带伤清点破障丹并查验血玉令牌。",
	})
	if err != nil {
		t.Fatalf("CreateChapter ch2 failed: %v", err)
	}
	ch3, err := lib.CreateChapter(ctx, domain.CreateChapterInput{
		BookID:     book.ID,
		VolumeID:   vol.ID,
		Title:      "黑煞老祖降临",
		EmotionTag: domain.EmotionTurn,
		HookType:   domain.HookCrisis,
	})
	if err != nil {
		t.Fatalf("CreateChapter ch3 failed: %v", err)
	}

	if _, err := lib.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
		ID:             ch2.ID,
		Title:          ch2.Title,
		Status:         domain.ChapterStatusOutlined,
		EmotionTag:     domain.EmotionSuppress,
		HookType:       domain.HookMystery,
		OutlineSummary: "林渊带伤清点破障丹并查验血玉令牌。",
	}); err != nil {
		t.Fatalf("UpdateChapterMeta ch2 failed: %v", err)
	}
	if _, err := lib.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
		ID:         ch3.ID,
		Title:      ch3.Title,
		Status:     domain.ChapterStatusOutlined,
		EmotionTag: domain.EmotionTurn,
		HookType:   domain.HookCrisis,
	}); err != nil {
		t.Fatalf("UpdateChapterMeta ch3 failed: %v", err)
	}

	// 保存 ch1 与 ch3 正文（ch3 含未来章独有关键词 "黑煞老祖"，用于验证 maxChapterSeq 防剧透）
	if _, err := lib.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID: ch1.ID,
		Content:   "残阵灵光骤灭，林渊忍着左肩阴雷暗伤，从王执事腰间摸出装有破障丹的青玉瓶与一枚血玉令牌。",
		Status:    domain.ChapterStatusReviewing,
	}); err != nil {
		t.Fatalf("SaveChapterDraft ch1 failed: %v", err)
	}
	if _, err := lib.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID: ch3.ID,
		Content:   "三日后，黑煞老祖亲临坊市废墟，神识横扫百里追查血玉令牌下落。",
		Status:    domain.ChapterStatusOutlined,
	}); err != nil {
		t.Fatalf("SaveChapterDraft ch3 failed: %v", err)
	}

	hero, err := atlas.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:  book.ID,
		Kind:    domain.EntityKindCharacter,
		Name:    "林渊",
		Summary: "青云宗散修出身，谨慎狠辣。",
		Attributes: map[string]string{
			"cultivation": "筑基初期",
			"injury":      "完好",
		},
	})
	if err != nil {
		t.Fatalf("UpsertEntity 林渊 failed: %v", err)
	}
	item, err := atlas.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:  book.ID,
		Kind:    domain.EntityKindItem,
		Name:    "破障丹",
		Summary: "可助筑基修士冲关破境的稀缺灵丹。",
		Attributes: map[string]string{
			"owner": "王执事",
		},
	})
	if err != nil {
		t.Fatalf("UpsertEntity 破障丹 failed: %v", err)
	}
	heroID = hero.ID
	itemID = item.ID

	if _, err := lib.ReplaceBeats(ctx, ch2.ID, []domain.Beat{
		{
			Stage:       domain.BeatStageSetup,
			Summary:     "林渊遁入荒山石窟，检查左肩伤势与破障丹。",
			POVEntityID: &hero.ID,
			EntityIDs:   []int64{hero.ID, item.ID},
			TargetWords: 600,
		},
	}); err != nil {
		t.Fatalf("ReplaceBeats ch2 failed: %v", err)
	}

	// 3. 配置供应商与 extract 路由，并调用 MemoryService.ExtractChapterDelta
	prov, err := settings.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "MockExtract",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  mockServer.URL + "/v1",
		APIKey:   rawSecretKey,
		Models:   []string{extractModel},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}
	pid := prov.ID
	if _, err := settings.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageExtract,
		ProviderID:  &pid,
		ModelName:   extractModel,
		Temperature: 0.1,
		MaxTokens:   2048,
	}); err != nil {
		t.Fatalf("SaveModelRoute extract failed: %v", err)
	}

	delta, err := memSvc.ExtractChapterDelta(ctx, ch1.ID)
	if err != nil {
		t.Fatalf("ExtractChapterDelta failed: %v", err)
	}
	if delta.ReviewStatus != domain.DeltaReviewPending || delta.HighRiskFlags < 1 {
		t.Fatalf("unexpected extracted delta status=%q highRisk=%d", delta.ReviewStatus, delta.HighRiskFlags)
	}
	if len(delta.StatePatches) != 2 || len(delta.Foreshadows) != 1 {
		t.Fatalf("unexpected delta counts: patches=%d foreshadows=%d", len(delta.StatePatches), len(delta.Foreshadows))
	}

	// 确认 GetPendingDelta 能读到同一条 pending 变更单
	pending, err := memSvc.GetPendingDelta(ctx, ch1.ID)
	if err != nil || pending == nil || pending.ID != delta.ID {
		t.Fatalf("GetPendingDelta = (%+v, %v), want id=%d", pending, err, delta.ID)
	}

	// 4. 未确认前：SnapshotAt(1, false) 不应看到 pending 变更；SnapshotAt(1, true) 应能看到
	findSnap := func(snaps []domain.EntitySnapshot, id int64) domain.EntitySnapshot {
		for _, s := range snaps {
			if s.Entity.ID == id {
				return s
			}
		}
		t.Fatalf("entity %d not found in snapshot", id)
		return domain.EntitySnapshot{}
	}

	snapsCommittedOnly, err := memSvc.SnapshotAt(ctx, book.ID, 1, false)
	if err != nil {
		t.Fatalf("SnapshotAt(1, false) failed: %v", err)
	}
	if got := findSnap(snapsCommittedOnly, item.ID).EffectiveAttributes["owner"]; got != "王执事" {
		t.Fatalf("unconfirmed pending leaked into committed SnapshotAt: owner=%q, want 王执事", got)
	}
	if got := findSnap(snapsCommittedOnly, hero.ID).EffectiveAttributes["injury"]; got != "完好" {
		t.Fatalf("unconfirmed pending leaked into committed SnapshotAt: injury=%q, want 完好", got)
	}

	snapsWithPending, err := memSvc.SnapshotAt(ctx, book.ID, 1, true)
	if err != nil {
		t.Fatalf("SnapshotAt(1, true) failed: %v", err)
	}
	if got := findSnap(snapsWithPending, item.ID).EffectiveAttributes["owner"]; got != "林渊" {
		t.Fatalf("SnapshotAt(1, true) missing pending owner transfer: got=%q", got)
	}
	if got := findSnap(snapsWithPending, hero.ID).EffectiveAttributes["injury"]; got != "左肩阴雷暗伤" {
		t.Fatalf("SnapshotAt(1, true) missing pending injury patch: got=%q", got)
	}

	// 5. 审核确认变更单（接受全部 patches + foreshadowing + causal summary）
	decisions := make([]domain.DeltaDecision, 0, len(delta.StatePatches)+len(delta.Foreshadows))
	for _, p := range delta.StatePatches {
		decisions = append(decisions, domain.DeltaDecision{
			Kind:   domain.DeltaDecisionKindStatePatch,
			ItemID: p.ID,
			Accept: true,
		})
	}
	for _, fs := range delta.Foreshadows {
		decisions = append(decisions, domain.DeltaDecision{
			Kind:   domain.DeltaDecisionKindForeshadow,
			ItemID: fs.ID,
			Accept: true,
		})
	}

	applyRes, err := memSvc.ApplyDeltaDecisions(ctx, domain.ApplyDeltaInput{
		DeltaID:            delta.ID,
		ApplyCausalSummary: true,
		Decisions:          decisions,
	})
	if err != nil {
		t.Fatalf("ApplyDeltaDecisions failed: %v", err)
	}
	if applyRes.CommittedEvents != 2 || applyRes.UpdatedForeshadowings != 1 {
		t.Fatalf("unexpected applyRes counts: %+v", applyRes)
	}

	// (a) SnapshotAt(1, false) 已反映 committed 状态
	snapsAfterCommit, err := memSvc.SnapshotAt(ctx, book.ID, 1, false)
	if err != nil {
		t.Fatalf("SnapshotAt(1, false) after commit failed: %v", err)
	}
	if got := findSnap(snapsAfterCommit, item.ID).EffectiveAttributes["owner"]; got != "林渊" {
		t.Fatalf("committed owner = %q, want 林渊", got)
	}
	if got := findSnap(snapsAfterCommit, hero.ID).EffectiveAttributes["injury"]; got != "左肩阴雷暗伤" {
		t.Fatalf("committed injury = %q, want 左肩阴雷暗伤", got)
	}

	// (b) SnapshotAt(0, false) 仍严格保持 ch1 之前的初始静态状态（绝不反向剧透）
	snapsBeforeCh1, err := memSvc.SnapshotAt(ctx, book.ID, 0, false)
	if err != nil {
		t.Fatalf("SnapshotAt(0, false) failed: %v", err)
	}
	if got := findSnap(snapsBeforeCh1, item.ID).EffectiveAttributes["owner"]; got != "王执事" {
		t.Fatalf("SnapshotAt(0, false) leaked future owner: %q", got)
	}
	if got := findSnap(snapsBeforeCh1, hero.ID).EffectiveAttributes["injury"]; got != "完好" {
		t.Fatalf("SnapshotAt(0, false) leaked future injury: %q", got)
	}

	// (c) 下游 outlined 章节 ch2 / ch3 均被标为 upstream_dirty
	if len(applyRes.MarkedDirtyChapterIDs) != 2 {
		t.Fatalf("expected 2 downstream chapters marked upstream_dirty, got %v", applyRes.MarkedDirtyChapterIDs)
	}
	ch2Reloaded, _ := lib.GetChapter(ctx, ch2.ID)
	ch3Reloaded, _ := lib.GetChapter(ctx, ch3.ID)
	if ch2Reloaded.Status != domain.ChapterStatusUpstreamDirty || ch3Reloaded.Status != domain.ChapterStatusUpstreamDirty {
		t.Fatalf("downstream statuses = (%q, %q), want both upstream_dirty", ch2Reloaded.Status, ch3Reloaded.Status)
	}

	// (d) SearchMemory 同时支持 2 字人名("林渊" LIKE 回退)与 3 字道具("破障丹" FTS5 trigram)，且严守 maxChapterSeq 防剧透
	hits2Char, err := memSvc.SearchMemory(ctx, book.ID, "林渊", 1)
	if err != nil || len(hits2Char) == 0 {
		t.Fatalf("SearchMemory 2-char 林渊 failed: hits=%+v err=%v", hits2Char, err)
	}
	hits3Char, err := memSvc.SearchMemory(ctx, book.ID, "破障丹", 1)
	if err != nil || len(hits3Char) == 0 {
		t.Fatalf("SearchMemory 3-char 破障丹 failed: hits=%+v err=%v", hits3Char, err)
	}
	hitsFutureBlocked, err := memSvc.SearchMemory(ctx, book.ID, "黑煞老祖", 1)
	if err != nil {
		t.Fatalf("SearchMemory maxChapterSeq=1 failed: %v", err)
	}
	for _, h := range hitsFutureBlocked {
		if h.Source == domain.MemorySearchSourceChapter && h.ChapterSeq > 1 {
			t.Fatalf("SearchMemory leaked future chapter seq=%d when maxChapterSeq=1: %+v", h.ChapterSeq, h)
		}
	}
	hitsFutureAllowed, err := memSvc.SearchMemory(ctx, book.ID, "黑煞老祖", 3)
	if err != nil || len(hitsFutureAllowed) == 0 {
		t.Fatalf("SearchMemory maxChapterSeq=3 should find ch3: hits=%+v err=%v", hitsFutureAllowed, err)
	}

	// (e) StudioService.PreviewBriefing(ch2) 包含 P2 有效快照、P3 伏笔、P5 因果摘要、P6 检索片段
	briefing, err := studio.PreviewBriefing(ctx, ch2.ID, domain.BriefingOptions{
		SearchKeywords: []string{"破障丹"},
	})
	if err != nil {
		t.Fatalf("PreviewBriefing ch2 failed: %v", err)
	}
	slotByPriority := make(map[string]domain.BriefingSlot, len(briefing.Slots))
	for _, sl := range briefing.Slots {
		slotByPriority[sl.Priority] = sl
	}
	if p2, ok := slotByPriority["P2"]; !ok || !strings.Contains(p2.Content, "左肩阴雷暗伤") || !strings.Contains(p2.Content, "林渊") {
		t.Fatalf("P2 slot missing committed effective snapshot: %+v", p2)
	}
	if p3, ok := slotByPriority["P3"]; !ok || !strings.Contains(p3.Content, "血玉令牌密库") {
		t.Fatalf("P3 slot missing foreshadowing: %+v", p3)
	}
	if p5, ok := slotByPriority["P5"]; !ok || !strings.Contains(p5.Content, "反杀王执事") {
		t.Fatalf("P5 slot missing ch1 causal summary: %+v", p5)
	}
	if p6, ok := slotByPriority["P6"]; !ok || !strings.Contains(p6.Content, "破障丹") {
		t.Fatalf("P6 slot missing FTS5 search snippet: %+v", p6)
	}
}

func TestPhase3StyleAndDeaiEndToEndWorkflow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nib-phase3.db")
	exportDir := filepath.Join(tmpDir, "exports-p3")

	const (
		rawSecretKey = "sk-phase3-style-key-5511c2d9"
		criticModel  = "mock-critic-v1"
		polishModel  = "mock-polish-v1"
	)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+rawSecretKey {
			http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
			return
		}

		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		var respJSON []byte
		switch body.Model {
		case criticModel:
			respJSON, _ = json.Marshal(map[string]any{
				"hook_passed":  false,
				"hook_verdict": "章末落入鸡汤式说教感悟，未兑现突发危机钩子",
				"critic_findings": []map[string]any{
					{
						"dimension":       "consistency",
						"severity":        "error",
						"paragraph_index": 1,
						"title":           "动作与套话失真",
						"detail":          "第 1 段连用嘴角勾起一抹弧度与眼神微眯，且忽略了左肩伤势限制。",
						"suggestion":      "改为单手扣住玉瓶并转身隐入石门阴影。",
					},
					{
						"dimension":       "hook",
						"severity":        "error",
						"paragraph_index": 2,
						"title":           "章末鸡汤升华",
						"detail":          "结尾使用“他明白真正的考验才刚刚开始”空洞总结。",
						"suggestion":      "改为石门外传来血煞宗长老破空威压。",
					},
				},
			})
		case polishModel:
			respJSON, _ = json.Marshal(map[string]any{
				"diffs": []map[string]any{
					{
						"paragraph_index": 1,
						"reason":          "剔除高频 AI 套话「嘴角勾起一抹弧度」「眼神微眯」",
						"replacement":     "林渊指尖扣紧那枚微温玉瓶，侧耳听着廊道动静，转身掠入石室暗门。",
					},
					{
						"paragraph_index": 2,
						"reason":          "剔除章末鸡汤升华，兑现突发危机钩子",
						"replacement":     "石门刚合拢半寸，百丈外骤然炸开一道血色神识，整座石窟碎石簌簌坠落。",
					},
				},
			})
		default:
			http.Error(w, "unknown model", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]string{"content": string(respJSON)}},
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		usagePayload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 260, "completion_tokens": 120, "total_tokens": 380},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", usagePayload)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer mockServer.Close()

	// 1. 真实装配 SQLite (001+002+003) + Compiler(WithMemorySource+WithStyleSource) + Pipeline(WithStyleStore) + Services
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	llmClient := llm.NewClient(llm.WithHTTPClient(mockServer.Client()))
	comp := compiler.New(st, compiler.WithMemorySource(st), compiler.WithStyleSource(st))
	runner := pipeline.New(st, comp, llmClient, exporter.New(), pipeline.WithStyleStore(st))
	extractor := memory.NewExtractor(st, llmClient)

	rt := app.NewRuntime()
	rt.Init(app.RuntimeDeps{
		Store:          st,
		Exporter:       exporter.NewStoreExporter(st),
		LLM:            llmClient,
		Compiler:       comp,
		Pipeline:       runner,
		MemoryStore:    st,
		DeltaExtractor: extractor,
		StyleStore:     st,
		StyleEngine:    runner,
	})
	defer func() {
		if err := rt.Close(); err != nil {
			t.Errorf("rt.Close failed: %v", err)
		}
	}()

	lib := app.NewLibraryService(rt)
	settings := app.NewSettingsService(rt)
	studio := app.NewStudioService(rt)
	styleSvc := app.NewStyleService(rt)

	// 2. 验证 003 迁移预置的 19 条全局 deai_rules (book_id=0)：禁止删除全局规则，但允许开关状态
	globalRules, err := styleSvc.ListDeaiRules(ctx, 0)
	if err != nil {
		t.Fatalf("ListDeaiRules(0) failed: %v", err)
	}
	if len(globalRules) != 19 {
		t.Fatalf("expected 19 seeded global deai_rules, got %d", len(globalRules))
	}
	firstGlobal := globalRules[0]
	if err := styleSvc.DeleteDeaiRule(ctx, firstGlobal.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation when deleting global rule, got %v", err)
	}
	toggledRule, err := styleSvc.UpsertDeaiRule(ctx, domain.UpsertDeaiRuleInput{
		ID:            firstGlobal.ID,
		BookID:        0,
		Category:      firstGlobal.Category,
		Pattern:       firstGlobal.Pattern,
		IsRegex:       firstGlobal.IsRegex,
		Severity:      firstGlobal.Severity,
		MaxPerChapter: firstGlobal.MaxPerChapter,
		Hint:          firstGlobal.Hint,
		Enabled:       false,
	})
	if err != nil || toggledRule.Enabled {
		t.Fatalf("expected global rule toggled to Enabled=false, got %+v err=%v", toggledRule, err)
	}
	// 恢复启用以免影响后续套话检出
	_, _ = styleSvc.UpsertDeaiRule(ctx, domain.UpsertDeaiRuleInput{
		ID:            firstGlobal.ID,
		BookID:        0,
		Category:      firstGlobal.Category,
		Pattern:       firstGlobal.Pattern,
		IsRegex:       firstGlobal.IsRegex,
		Severity:      firstGlobal.Severity,
		MaxPerChapter: firstGlobal.MaxPerChapter,
		Hint:          firstGlobal.Hint,
		Enabled:       true,
	})

	// 3. 创建书籍、卷、章节与战斗节拍，运行 ExtractStyleProfile 并验证 Slot P7 装配
	book, err := lib.CreateBook(ctx, domain.CreateBookInput{
		Title:     "剑起天渊",
		Genre:     domain.GenreXuanhuan,
		Premise:   "散修夺宝破局。",
		ExportDir: exportDir,
	})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}
	vol, err := lib.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: book.ID,
		Seq:    1,
		Title:  "青云起",
	})
	if err != nil {
		t.Fatalf("UpsertVolume failed: %v", err)
	}
	ch1, err := lib.CreateChapter(ctx, domain.CreateChapterInput{
		BookID:     book.ID,
		VolumeID:   vol.ID,
		Title:      "石室夺丹",
		EmotionTag: domain.EmotionBurst,
		HookType:   domain.HookCrisis,
		HookNote:   "百丈外血色神识降临。",
	})
	if err != nil {
		t.Fatalf("CreateChapter failed: %v", err)
	}
	if _, err := lib.ReplaceBeats(ctx, ch1.ID, []domain.Beat{
		{Stage: domain.BeatStageBurst, Summary: "林渊斩杀王执事夺取玉瓶。", TargetWords: 600},
		{Stage: domain.BeatStageHook, Summary: "门外强敌神识突然扫过石室。", TargetWords: 300},
	}); err != nil {
		t.Fatalf("ReplaceBeats failed: %v", err)
	}

	sampleTexts := []string{
		"剑锋擦过石壁，火星迸溅三尺。\n\n“想走？”林渊反手拧断铁锁，短剑直刺对方咽喉。\n\n血光一闪，黑衣执事踉跄跌退，撞碎丹架。",
	}
	profile, err := styleSvc.ExtractStyleProfile(ctx, book.ID, sampleTexts)
	if err != nil {
		t.Fatalf("ExtractStyleProfile failed: %v", err)
	}
	if profile.BookID != book.ID || profile.AvgSentenceChars <= 0 || profile.MaxParagraphChars <= 0 {
		t.Fatalf("unexpected extracted StyleProfile: %+v", profile)
	}
	samples, err := styleSvc.ListStyleSamples(ctx, book.ID, "")
	if err != nil || len(samples) == 0 {
		t.Fatalf("expected style samples persisted after ExtractStyleProfile, len=%d err=%v", len(samples), err)
	}

	briefing, err := studio.PreviewBriefing(ctx, ch1.ID, domain.BriefingOptions{})
	if err != nil {
		t.Fatalf("PreviewBriefing failed: %v", err)
	}
	var foundP7 bool
	for _, sl := range briefing.Slots {
		if sl.Priority == "P7" || sl.Key == "p7_style_dna" {
			foundP7 = true
			if !strings.Contains(sl.Content, "剑锋擦过石壁") {
				t.Fatalf("P7 slot missing extracted combat sample anchor: %s", sl.Content)
			}
		}
	}
	if !foundP7 {
		t.Fatalf("PreviewBriefing missing Slot P7 (p7_style_dna)")
	}

	// 4. 保存含典型 AI 套话与鸡汤结尾的 3 段草稿，执行 0 成本 LintChapterLocal
	para0 := "残阵灵光骤灭，焦糊味混着血腥气在石室散开。"
	para1 := "林渊嘴角勾起一抹弧度，眼神微眯，心中涌起一股难以言喻的喜悦。"
	para2 := "这一刻他意识到，真正的考验才刚刚开始，命运的齿轮开始转动。"
	initialContent := strings.Join([]string{para0, para1, para2}, "\n\n")

	if _, err := lib.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID: ch1.ID,
		Content:   initialContent,
		Status:    domain.ChapterStatusReviewing,
	}); err != nil {
		t.Fatalf("SaveChapterDraft failed: %v", err)
	}

	lintAudit, err := styleSvc.LintChapterLocal(ctx, ch1.ID)
	if err != nil {
		t.Fatalf("LintChapterLocal failed: %v", err)
	}
	if len(lintAudit.LintIssues) < 3 || lintAudit.DeaiScore >= 100 {
		t.Fatalf("expected >=3 lint issues and score < 100, got issues=%d score=%d", len(lintAudit.LintIssues), lintAudit.DeaiScore)
	}
	scoreBeforePolish := lintAudit.DeaiScore

	// 5. 配置 critic 与 polish 路由，执行 RunCriticAudit 与 GenerateSurgicalDiffs
	prov, err := settings.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "MockStyleLLM",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  mockServer.URL + "/v1",
		APIKey:   rawSecretKey,
		Models:   []string{criticModel, polishModel},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}
	pid := prov.ID
	_, _ = settings.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageCritic,
		ProviderID:  &pid,
		ModelName:   criticModel,
		Temperature: 0.2,
		MaxTokens:   2048,
	})
	_, _ = settings.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StagePolish,
		ProviderID:  &pid,
		ModelName:   polishModel,
		Temperature: 0.55,
		MaxTokens:   2048,
	})

	criticAudit, err := styleSvc.RunCriticAudit(ctx, ch1.ID)
	if err != nil {
		t.Fatalf("RunCriticAudit failed: %v", err)
	}
	if criticAudit.HookPassed || len(criticAudit.CriticFindings) != 2 {
		t.Fatalf("unexpected CriticAudit: hookPassed=%v findings=%d", criticAudit.HookPassed, len(criticAudit.CriticFindings))
	}

	diffAudit, err := styleSvc.GenerateSurgicalDiffs(ctx, ch1.ID, nil)
	if err != nil {
		t.Fatalf("GenerateSurgicalDiffs failed: %v", err)
	}
	if len(diffAudit.Diffs) != 2 {
		t.Fatalf("expected 2 surgical diffs, got %d", len(diffAudit.Diffs))
	}

	// 6. 逐条采纳手术润色建议：验证好段落 para0 毫发无伤、surgical_polish 快照已归档、DeaiScore 显著提升、磁盘 .md 已同步
	for _, d := range diffAudit.Diffs {
		applyRes, err := styleSvc.ApplySurgicalDiff(ctx, domain.ApplySurgicalDiffInput{
			ChapterID: ch1.ID,
			DiffID:    d.ID,
			Accept:    true,
		})
		if err != nil {
			t.Fatalf("ApplySurgicalDiff(%s) failed: %v", d.ID, err)
		}
		if !strings.HasPrefix(applyRes.Chapter.Content, para0) {
			t.Fatalf("surgical diff corrupted untouched paragraph 0: %q", applyRes.Chapter.Content)
		}
	}

	finalAudit, err := styleSvc.GetChapterAudit(ctx, ch1.ID)
	if err != nil {
		t.Fatalf("GetChapterAudit failed: %v", err)
	}
	if finalAudit.DeaiScore <= scoreBeforePolish || len(finalAudit.LintIssues) != 0 {
		t.Fatalf("expected clean prose & improved score after surgical polish, got score %d -> %d, issues=%+v",
			scoreBeforePolish, finalAudit.DeaiScore, finalAudit.LintIssues)
	}

	versions, err := studio.ListChapterVersions(ctx, ch1.ID)
	if err != nil || len(versions) == 0 {
		t.Fatalf("expected chapter_versions archived before surgical polish, err=%v len=%d", err, len(versions))
	}
	if versions[0].Source != domain.VersionSourceSurgicalPolish {
		t.Fatalf("newest version source = %q, want %q", versions[0].Source, domain.VersionSourceSurgicalPolish)
	}

	expectedMD := filepath.Join(exportDir, "剑起天渊", "第01卷_青云起", "第001章_石室夺丹.md")
	mdBytes, err := os.ReadFile(expectedMD)
	if err != nil {
		t.Fatalf("expected exported markdown at %q: %v", expectedMD, err)
	}
	mdText := string(mdBytes)
	if strings.Contains(mdText, "嘴角勾起一抹弧度") || !strings.Contains(mdText, "指尖扣紧那枚微温玉瓶") {
		t.Fatalf("exported markdown not updated with surgical polish replacement:\n%s", mdText)
	}
}

func TestPhase4BatchAndPacingEndToEndWorkflow(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nib-phase4.db")

	const (
		rawSecretKey = "sk-phase4-batch-key-8844aa11"
		outlineModel = "mock-outline-v1"
		draftModel   = "mock-draft-v1"
		criticModel  = "mock-critic-v1"
		extractModel = "mock-extract-v1"
	)

	var draftPromptsMu sync.Mutex
	var draftPrompts []string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+rawSecretKey {
			http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
			return
		}

		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		var contentOut string
		switch body.Model {
		case outlineModel:
			raw, _ := json.Marshal(map[string]any{
				"outlines": []map[string]any{
					{
						"title":           "坊市暗算",
						"emotion_tag":     "suppress",
						"hook_type":       "crisis",
						"hook_note":       "黑煞执事封锁坊市南门",
						"outline_summary": "林渊探查破障丹遭伏击受轻伤。",
						"beats": []map[string]any{
							{"stage": "setup", "summary": "林渊夜入坊市查探丹药", "target_words": 500},
							{"stage": "suppress", "summary": "黑煞执事截杀震伤左肩", "target_words": 600},
						},
					},
					{
						"title":           "破阵反杀",
						"emotion_tag":     "burst",
						"hook_type":       "mystery",
						"hook_note":       "血玉令牌背面古阵纹亮起",
						"outline_summary": "林渊带伤引动残阵反杀王执事。",
						"beats": []map[string]any{
							{"stage": "burst", "summary": "残阵雷光轰杀强敌夺宝", "target_words": 700},
						},
					},
				},
			})
			contentOut = string(raw)
		case draftModel:
			userMsg := ""
			for _, m := range body.Messages {
				if m.Role == "user" {
					userMsg += m.Content
				}
			}
			draftPromptsMu.Lock()
			draftPrompts = append(draftPrompts, userMsg)
			draftPromptsMu.Unlock()
			contentOut = "夜雨浇透青石板，林渊按住阵盘退后半步。\n\n雷光轰鸣炸开，敌修护体灵光寸寸崩裂。"
		case criticModel:
			raw, _ := json.Marshal(map[string]any{
				"hook_passed":     true,
				"hook_verdict":    "结尾冲突紧凑，符合钩子要求",
				"critic_findings": []any{},
			})
			contentOut = string(raw)
		case extractModel:
			raw, _ := json.Marshal(map[string]any{
				"causal_summary": "林渊在坊市交手中左肩受阴雷暗伤，夺得血玉令牌。",
				"new_entities":   []any{},
				"state_patches": []map[string]any{
					{
						"entity_name": "林渊",
						"attr_key":    "injury",
						"prev_value":  "完好",
						"new_value":   "左肩阴雷暗伤（微批接力pending）",
						"severity":    "normal",
						"reason":      "首章伏击受创",
					},
				},
				"foreshadowing_updates": []any{},
			})
			contentOut = string(raw)
		default:
			http.Error(w, "unknown model", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]string{"content": contentOut}}},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		usagePayload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 240, "completion_tokens": 110, "total_tokens": 350},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", usagePayload)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer mockServer.Close()

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	if err := st.RecoverInterruptedBatchRuns(ctx); err != nil {
		t.Fatalf("RecoverInterruptedBatchRuns failed: %v", err)
	}

	llmClient := llm.NewClient(llm.WithHTTPClient(mockServer.Client()))
	comp := compiler.New(st, compiler.WithMemorySource(st), compiler.WithStyleSource(st))
	extractor := memory.NewExtractor(st, llmClient)

	var progressMu sync.Mutex
	var progressList []domain.BatchProgress
	runner := pipeline.New(
		st,
		comp,
		llmClient,
		exporter.New(),
		pipeline.WithStyleStore(st),
		pipeline.WithBatchStore(st),
		pipeline.WithDeltaExtractor(extractor),
		pipeline.WithProgressListener(func(p domain.BatchProgress) {
			progressMu.Lock()
			progressList = append(progressList, p)
			progressMu.Unlock()
		}),
	)

	rt := app.NewRuntime()
	rt.Init(app.RuntimeDeps{
		Store:          st,
		Exporter:       exporter.NewStoreExporter(st),
		LLM:            llmClient,
		Compiler:       comp,
		Pipeline:       runner,
		MemoryStore:    st,
		DeltaExtractor: extractor,
		StyleStore:     st,
		StyleEngine:    runner,
		BatchStore:     st,
		BatchEngine:    runner,
	})
	defer func() {
		runner.Close()
		if err := rt.Close(); err != nil {
			t.Errorf("rt.Close failed: %v", err)
		}
	}()

	lib := app.NewLibraryService(rt)
	atlas := app.NewAtlasService(rt)
	settings := app.NewSettingsService(rt)
	memSvc := app.NewMemoryService(rt)
	batchSvc := app.NewBatchService(rt)

	// 1. 建书、建卷、建主角实体 + 配置供应商与 4 个模型路由
	book, err := lib.CreateBook(ctx, domain.CreateBookInput{
		Title:   "微批斩仙",
		Genre:   domain.GenreXianxia,
		Premise: "散修靠微批接力稳推长篇。",
	})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}
	vol, err := lib.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: book.ID,
		Seq:    1,
		Title:  "坊市风云",
		Goal:   "反杀黑煞执事夺取密库令牌",
	})
	if err != nil {
		t.Fatalf("UpsertVolume failed: %v", err)
	}
	hero, err := atlas.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:     book.ID,
		Kind:       domain.EntityKindCharacter,
		Name:       "林渊",
		Summary:    "谨慎散修",
		Attributes: map[string]string{"injury": "完好"},
	})
	if err != nil {
		t.Fatalf("UpsertEntity failed: %v", err)
	}

	prov, err := settings.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "MockBatchRelay",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  mockServer.URL + "/v1",
		APIKey:   rawSecretKey,
		Models:   []string{outlineModel, draftModel, criticModel, extractModel},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider failed: %v", err)
	}
	pid := prov.ID
	for stage, model := range map[domain.PipelineStage]string{
		domain.StageOutline: outlineModel,
		domain.StageDraft:   draftModel,
		domain.StageCritic:  criticModel,
		domain.StageExtract: extractModel,
	} {
		if _, err := settings.SaveModelRoute(ctx, domain.ModelRoute{
			Stage:       stage,
			ProviderID:  &pid,
			ModelName:   model,
			Temperature: 0.5,
			MaxTokens:   2048,
		}); err != nil {
			t.Fatalf("SaveModelRoute(%s) failed: %v", stage, err)
		}
	}

	// 2. 批量生成 2 章结构化章纲与节拍卡
	outlinedChaps, err := lib.BatchGenerateOutlines(ctx, domain.BatchOutlineRequest{
		BookID:   book.ID,
		VolumeID: vol.ID,
		StartSeq: 1,
		Count:    2,
		ArcGoal:  "先抑后扬反杀夺宝",
	}, nil)
	if err != nil {
		t.Fatalf("BatchGenerateOutlines failed: %v", err)
	}
	if len(outlinedChaps) != 2 {
		t.Fatalf("expected 2 outlined chapters, got %d", len(outlinedChaps))
	}
	// 将林渊绑定到第 2 章的节拍上，以验证第 2 章编译 P2 槽位时能接力读到第 1 章刚抽出的 pending 伤势！
	if _, err := lib.ReplaceBeats(ctx, outlinedChaps[1].ID, []domain.Beat{
		{Stage: domain.BeatStageBurst, Summary: "林渊带伤反杀", POVEntityID: &hero.ID, EntityIDs: []int64{hero.ID}, TargetWords: 700},
	}); err != nil {
		t.Fatalf("ReplaceBeats ch2 failed: %v", err)
	}

	// 3. 验证卷节奏曲线与超期伏笔诊断 (GetVolumePacing)
	if _, err := memSvc.UpsertForeshadowing(ctx, domain.UpsertForeshadowingInput{
		BookID:            book.ID,
		Title:             "古宗密库超期伏笔",
		PlantedChapterSeq: 1,
		TargetResolveSeq:  2,
		Status:            domain.ForeshadowingPlanted,
	}); err != nil {
		t.Fatalf("UpsertForeshadowing failed: %v", err)
	}
	pacing, err := lib.GetVolumePacing(ctx, vol.ID)
	if err != nil {
		t.Fatalf("GetVolumePacing failed: %v", err)
	}
	if len(pacing.Points) != 2 || len(pacing.Alerts) == 0 {
		t.Fatalf("expected 2 pacing points and overdue foreshadow alert, got %+v", pacing)
	}

	// 4. 启动 2 章 full_pipeline 串行微批，验证 ch1 pending 记忆接力传给 ch2，且最终落库 pending 变更单与 Token 统计
	run, err := batchSvc.StartBatchRun(ctx, book.ID, []int64{outlinedChaps[0].ID, outlinedChaps[1].ID}, string(domain.BatchModeFullPipeline))
	if err != nil {
		t.Fatalf("StartBatchRun failed: %v", err)
	}
	runner.WaitAllBatches()

	finalRun, err := batchSvc.GetBatchRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetBatchRun failed: %v", err)
	}
	if finalRun.Status != domain.BatchRunCompleted || finalRun.DoneItems != 2 {
		t.Fatalf("expected completed batch run with 2 done items, got %+v", finalRun)
	}

	// 关键校验：第 2 章出稿 Prompt 必须通过 IncludePending=true 接力读取到第 1 章刚抽出的 pending 伤势！
	draftPromptsMu.Lock()
	defer draftPromptsMu.Unlock()
	if len(draftPrompts) != 2 {
		t.Fatalf("expected 2 draft prompts, got %d", len(draftPrompts))
	}
	if !strings.Contains(draftPrompts[1], "左肩阴雷暗伤（微批接力pending）") {
		t.Fatalf("expected chapter 2 prompt to relay chapter 1 pending state, got:\n%s", draftPrompts[1])
	}

	// 关键校验：Token 统计聚合（outline + draft*2 + critic*2 + extract*2）已持久化并可查！
	usageSummary, err := settings.GetUsageSummary(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetUsageSummary failed: %v", err)
	}
	if usageSummary.TotalCalls < 7 || usageSummary.TotalTokens <= 0 || len(usageSummary.ByStage) < 4 {
		t.Fatalf("unexpected usage summary after full batch: %+v", usageSummary)
	}
	var sawExtractStage bool
	for _, stStat := range usageSummary.ByStage {
		if stStat.Stage == string(domain.StageExtract) && stStat.Calls >= 2 && stStat.TotalTokens > 0 {
			sawExtractStage = true
		}
	}
	if !sawExtractStage {
		t.Fatalf("expected StageExtract logged in GetUsageSummary.ByStage, got %+v", usageSummary.ByStage)
	}
	progressMu.Lock()
	pCount := len(progressList)
	progressMu.Unlock()
	if pCount < 6 {
		t.Fatalf("expected batch progress broadcasts >= 6, got %d", pCount)
	}
}
