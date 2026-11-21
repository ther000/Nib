package pipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/llm"
	"nib/internal/store"
)

func TestDirectorNativeToolCallingPersistenceUndoAndRelations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "batch3.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "原生工具测试", Genre: domain.GenreXuanhuan})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Seq: 1, Title: "第一卷"})
	ch, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第一章"})
	_, _ = st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID: ch.ID,
		Content:   "第一段原文。\n\n第二段需要改写。\n\n第三段结尾。",
	})
	e1, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "林渊"})
	e2, _ := st.UpsertEntity(ctx, domain.UpsertEntityInput{BookID: book.ID, Kind: domain.EntityKindCharacter, Name: "王执事"})

	p1, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name: "OpenAI", Protocol: domain.ProtocolOpenAICompat, BaseURL: "https://api.example.com/v1",
		APIKey: "sk-test-1234", Models: []string{"gpt-4o"}, Enabled: true,
	})
	_, _ = st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage: domain.StageOutline, ProviderID: &p1.ID, ModelName: "gpt-4o", Temperature: 0.4, MaxTokens: 2048,
	})

	step := 0
	streamer := &fakeLLM{
		fn: func(_ context.Context, _ domain.Provider, req llm.ChatRequest, onEvent func(llm.StreamEvent) error) (domain.TokenUsage, error) {
			step++
			if len(req.Tools) == 0 {
				t.Fatalf("expected native llm.ToolDefinition passed to director LLM")
			}
			switch step {
			case 1:
				// 原生 Tool Call：添加实体关系 + 局部修改第 2 段
				_ = onEvent(llm.StreamEvent{
					ToolCalls: []llm.ToolCall{
						{
							ID:        "call_rel",
							Name:      "upsert_entity_relation",
							Arguments: `{"source_name":"林渊","target_name":"王执事","relation_type":"死敌","description":"夺丹结仇"}`,
						},
						{
							ID:        "call_patch",
							Name:      "replace_chapter_paragraphs",
							Arguments: `{"chapter_seq":1,"start_paragraph":2,"end_paragraph":2,"new_text":"第二段已经改写得更有杀气。"}`,
						},
					},
				})
			default:
				_ = onEvent(llm.StreamEvent{Delta: "已记录林渊与王执事的死敌关系，并局部优化了第一章第二段。"})
			}
			return domain.TokenUsage{PromptTokens: 120, CompletionTokens: 40, TotalTokens: 160}, nil
		},
	}

	comp := compiler.New(st)
	runner := New(st, comp, streamer, nil)

	reply, err := runner.RunDirectorChat(ctx, domain.DirectorChatRequest{
		BookID:          book.ID,
		ActiveChapterID: ch.ID,
		Instruction:     "记录林渊和王执事为死敌，并把第一章第二段改写得更有杀气",
	}, nil)
	if err != nil {
		t.Fatalf("RunDirectorChat failed: %v", err)
	}
	if !reply.Mutated || len(reply.ToolCalls) != 2 {
		t.Fatalf("unexpected director reply: %+v", reply)
	}
	// 验证局部改写工具记录了可一键撤回的 UndoVersionID
	if reply.ToolCalls[1].UndoVersionID <= 0 || reply.ToolCalls[1].UndoChapterID != ch.ID {
		t.Fatalf("expected undo version id on paragraph edit tool call, got %+v", reply.ToolCalls[1])
	}

	// 验证正文仅修改第 2 段
	updatedChap, _ := st.GetChapter(ctx, ch.ID)
	if !strings.Contains(updatedChap.Content, "第二段已经改写得更有杀气。") || !strings.Contains(updatedChap.Content, "第一段原文。") {
		t.Fatalf("unexpected chapter content: %q", updatedChap.Content)
	}

	// 验证对话已持久化到 director_messages
	hist, err := st.ListDirectorMessages(ctx, book.ID)
	if err != nil || len(hist) != 2 || hist[0].Role != "user" || hist[1].Role != "assistant" {
		t.Fatalf("expected 2 persisted director messages, got %+v err=%v", hist, err)
	}

	// 验证关系网已自动注入 Compiler P2 槽位
	brief, err := comp.Compile(ctx, ch.ID, domain.BriefingOptions{PinnedEntityIDs: []int64{e1.ID, e2.ID}})
	if err != nil {
		t.Fatalf("Compile with relations failed: %v", err)
	}
	if !strings.Contains(brief.UserPrompt, "林渊 —[死敌]→ 王执事") {
		t.Fatalf("expected entity relation injected in briefing P2, got:\n%s", brief.UserPrompt)
	}

	// 验证数据库一键备份可用
	backupPath := filepath.Join(t.TempDir(), "nib-backup.db")
	if err := st.BackupToFile(ctx, backupPath); err != nil {
		t.Fatalf("BackupToFile failed: %v", err)
	}
	stBackup, err := store.Open(backupPath)
	if err != nil {
		t.Fatalf("open backed up db failed: %v", err)
	}
	defer stBackup.Close()
	if _, err := stBackup.GetBook(ctx, book.ID); err != nil {
		t.Fatalf("backed up db missing book: %v", err)
	}
}
