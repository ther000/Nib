package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nib/app"
	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/exporter"
	"nib/internal/llm"
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
