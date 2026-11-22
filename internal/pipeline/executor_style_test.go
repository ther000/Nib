package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/linter"
	"nib/internal/llm"
	"nib/internal/memory"
	"nib/internal/store"
)

func TestApplySurgicalDiffRejectsDriftedOriginalWithoutCorruptingParagraph(t *testing.T) {
	ctx := context.Background()
	fs := newFakeStyleStore(t.TempDir())
	ch := fs.chapter

	// P1 已被手动修改过，与 diff.Original 完全不匹配。
	fs.audit = domain.ChapterAudit{
		ChapterID: ch.ID,
		Diffs: []domain.SurgicalDiff{
			{
				ID:             "diff-stale-1",
				ParagraphIndex: 1,
				Original:       "这是一段早就删掉的旧文本。",
				Replacement:    "错误覆写文本",
				Status:         domain.DiffStatusPending,
			},
		},
	}

	runner := New(fs, compiler.New(fs), &fakeLLM{}, nil)
	_, err := runner.ApplySurgicalDiff(ctx, domain.ApplySurgicalDiffInput{
		ChapterID: ch.ID,
		DiffID:    "diff-stale-1",
		Accept:    true,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for drifted diff original, got: %v", err)
	}
	if strings.Contains(fs.chapter.Content, "错误覆写文本") {
		t.Fatalf("drifted diff overwrote unrelated paragraph: %q", fs.chapter.Content)
	}
	if !strings.Contains(fs.chapter.Content, "王执事") {
		t.Fatalf("expected original P1 intact, got: %q", fs.chapter.Content)
	}
}

func TestFallbackNotifiesActiveModelAndPreservesPrimaryError(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "主备双错测试", Genre: domain.GenreXuanhuan})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Seq: 1, Title: "第一卷"})
	ch, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第一章"})

	p1, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name: "PrimaryProv", Protocol: domain.ProtocolOpenAICompat, BaseURL: "https://p1.example.com/v1",
		APIKey: "sk-1", Models: []string{"m-main"}, Enabled: true,
	})
	p2, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name: "BackupProv", Protocol: domain.ProtocolOpenAICompat, BaseURL: "https://p2.example.com/v1",
		APIKey: "sk-2", Models: []string{"m-backup"}, Enabled: true,
	})
	_, _ = st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage: domain.StageDraft, ProviderID: &p1.ID, ModelName: "m-main",
		FallbackProviderID: &p2.ID, FallbackModelName: "m-backup", Temperature: 0.7, MaxTokens: 2048,
	})

	primarySentinel := errors.New("429 primary quota exhausted")
	fallbackSentinel := errors.New("503 backup overloaded")

	streamer := &fakeLLM{
		fn: func(_ context.Context, prov domain.Provider, _ llm.ChatRequest, _ func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			if prov.ID == p1.ID {
				return domain.TokenUsage{}, primarySentinel
			}
			return domain.TokenUsage{}, fallbackSentinel
		},
	}

	runner := New(st, compiler.New(st), streamer, nil)
	_, genErr := runner.GenerateChapter(ctx, ch.ID, domain.GenerateChapterOptions{}, nil)
	if !errors.Is(genErr, primarySentinel) || !errors.Is(genErr, fallbackSentinel) {
		t.Fatalf("expected error wrapping both primary and fallback reasons, got: %v", genErr)
	}

	// 验证降级成功时，delta chunk 携带切换后的备用 ProviderName/ModelName。
	okStreamer := &fakeLLM{
		fn: func(_ context.Context, prov domain.Provider, _ llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			if prov.ID == p1.ID {
				return domain.TokenUsage{}, primarySentinel
			}
			_ = onEvent(llm.StreamEvent{Delta: "备用成稿内容"})
			return domain.TokenUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30}, nil
		},
	}
	runnerOK := New(st, compiler.New(st), okStreamer, nil)
	var deltaModels []string
	var deltaProvs []string
	_, okErr := runnerOK.GenerateChapter(ctx, ch.ID, domain.GenerateChapterOptions{}, func(c domain.GenChunk) error {
		if c.Kind == domain.GenChunkDelta {
			deltaModels = append(deltaModels, c.ModelName)
			deltaProvs = append(deltaProvs, c.ProviderName)
		}
		return nil
	})
	if okErr != nil {
		t.Fatalf("unexpected error on fallback success: %v", okErr)
	}
	if len(deltaModels) != 1 || deltaModels[0] != "m-backup" || deltaProvs[0] != "BackupProv" {
		t.Fatalf("expected delta chunk from BackupProv/m-backup, got prov=%v model=%v", deltaProvs, deltaModels)
	}
}

func TestAutopilotAccountsTokensAcrossAllStages(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "全环节Token统计", Genre: domain.GenreXuanhuan, TargetChapterWords: 300})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	prov, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name: "MockProv", Protocol: domain.ProtocolOpenAICompat, BaseURL: "https://api.example.com/v1",
		APIKey: "sk-test", Models: []string{"mock-model"}, Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider: %v", err)
	}
	for _, stage := range []domain.PipelineStage{
		domain.StageOutline, domain.StageDraft, domain.StageCritic, domain.StagePolish, domain.StageExtract,
	} {
		_, _ = st.SaveModelRoute(ctx, domain.ModelRoute{
			Stage: stage, ProviderID: &prov.ID, ModelName: "mock-model", Temperature: 0.4, MaxTokens: 2048,
		})
	}

	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			sys := req.System
			switch {
			case strings.Contains(sys, "outlines"):
				_ = onEvent(llm.StreamEvent{Delta: `{"outlines":[{"title":"启程","emotion_tag":"setup","hook_type":"mystery","hook_note":"神秘铃响","outline_summary":"进城","beats":[{"stage":"setup","summary":"入门","conflict_note":"","target_words":500}]}]}`})
				return domain.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}, nil
			case strings.Contains(sys, "Critic") || strings.Contains(req.Messages[0].Content, "hook_passed"):
				_ = onEvent(llm.StreamEvent{Delta: `{"hook_passed":true,"hook_verdict":"OK","critic_findings":[{"paragraph_index":0,"severity":"warn","issue":"节奏偏平","suggestion":"加强动作"}]}`})
				return domain.TokenUsage{PromptTokens: 80, CompletionTokens: 40, TotalTokens: 120}, nil
			case strings.Contains(sys, "diffs") || strings.Contains(sys, "手术式"):
				_ = onEvent(llm.StreamEvent{Delta: `{"diffs":[{"paragraph_index":0,"reason":"加强动作","replacement":"陈青收刀入鞘，青衫落满细雪。"}]}`})
				return domain.TokenUsage{PromptTokens: 60, CompletionTokens: 30, TotalTokens: 90}, nil
			case strings.Contains(sys, "state_patches") || strings.Contains(sys, "抽取"):
				_ = onEvent(llm.StreamEvent{Delta: `{"causal_summary":"陈青入城。","new_entities":[],"state_patches":[],"foreshadowing_updates":[]}`})
				return domain.TokenUsage{PromptTokens: 70, CompletionTokens: 30, TotalTokens: 100}, nil
			default:
				// Draft 正文（包含套话触发局部润色，字数 >= 35%）
				prose := linter.JoinParagraphs([]string{
					"陈青不由得深吸一口气，踏入青石城门，雪落眉梢。",
					"夜风掠过长街，远处铜铃忽然轻响三声，引得众人侧目。",
				})
				_ = onEvent(llm.StreamEvent{Delta: prose})
				return domain.TokenUsage{PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300}, nil
			}
		},
	}

	ext := memory.NewExtractor(st, streamer)
	runner := New(st, compiler.New(st), streamer, nil, WithDeltaExtractor(ext))
	defer runner.Close()

	state, err := runner.StartAutopilot(ctx, domain.AutopilotRequest{
		BookID:         book.ID,
		TargetChapters: 1,
		AutoOutline:    true,
		AutoPolish:     true,
	})
	if err != nil {
		t.Fatalf("StartAutopilot: %v", err)
	}
	if state.Status != domain.AutopilotStatusRunning {
		t.Fatalf("expected running, got %s", state.Status)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cur := runner.GetAutopilotState(book.ID)
		if cur.Status == domain.AutopilotStatusCompleted || cur.Status == domain.AutopilotStatusFailed {
			if cur.Status != domain.AutopilotStatusCompleted {
				t.Fatalf("autopilot failed: %s (msg=%s)", cur.Error, cur.Message)
			}
			// 应汇总 Outline(150) + Draft(300) + Critic(120) + Polish(90) + Extract(100) = 760 Token（或至少显著超过 Draft 300）
			if cur.TokensUsed < 760 {
				t.Fatalf("expected TokensUsed >= 760 across all 5 stages, got %d", cur.TokensUsed)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for autopilot completion")
}

func TestDirectorToolTableParityAndMalformedArgsError(t *testing.T) {
	defs := directorToolDefinitions()
	if len(defs) != 21 {
		t.Fatalf("expected 21 director tool definitions, got %d", len(defs))
	}
	prompt := buildDirectorSystemPrompt(domain.Book{Title: "测试书籍"}, "snapshot")
	hasWriteTool := false
	for _, d := range defs {
		if d.Name == "write_chapter_content" {
			hasWriteTool = true
		}
		if !strings.Contains(prompt, d.Name) {
			t.Fatalf("director system prompt missing tool %q", d.Name)
		}
		if lbl := directorToolLabel(d.Name); lbl == "" || lbl == d.Name {
			t.Fatalf("missing Chinese label for tool %q", d.Name)
		}
	}
	if !hasWriteTool {
		t.Fatalf("expected write_chapter_content in directorToolDefinitions")
	}

	// 验证畸形 JSON 参数会返回显式 ErrValidation 错误而不是静默吞掉。
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "参数校验", Genre: domain.GenreXuanhuan})
	runner := New(st, compiler.New(st), &fakeLLM{}, nil)
	_, _, _, _, toolErr := runner.executeDirectorTool(ctx, st, book.ID, "list_chapters_range", json.RawMessage(`{bad json`))
	if !errors.Is(toolErr, domain.ErrValidation) {
		t.Fatalf("expected ErrValidation on malformed JSON tool args, got: %v", toolErr)
	}
}
