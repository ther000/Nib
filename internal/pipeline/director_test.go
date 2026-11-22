package pipeline

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"nib/internal/compiler"
	"nib/internal/domain"
	"nib/internal/llm"
	"nib/internal/store"
)

type stubDirectorLLM struct {
	mu      sync.Mutex
	replies []string
	idx     int
}

func (s *stubDirectorLLM) Stream(
	_ context.Context,
	_ domain.Provider,
	_ llm.ChatRequest,
	onEvent func(llm.StreamEvent) error,
) (domain.TokenUsage, error) {
	s.mu.Lock()
	var resp string
	if s.idx < len(s.replies) {
		resp = s.replies[s.idx]
		s.idx++
	}
	s.mu.Unlock()
	if onEvent != nil && resp != "" {
		if err := onEvent(llm.StreamEvent{Delta: resp}); err != nil {
			return domain.TokenUsage{}, err
		}
	}
	return domain.TokenUsage{PromptTokens: 80, CompletionTokens: 60, TotalTokens: 140}, nil
}

func TestDirectorMultiStepToolExecution(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "director.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	prov, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "DirProv",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-dir-1234",
		Models:   []string{"dir-model"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("upsert provider: %v", err)
	}
	if _, err := st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageOutline,
		ProviderID:  &prov.ID,
		ModelName:   "dir-model",
		Temperature: 0.4,
		MaxTokens:   4096,
	}); err != nil {
		t.Fatalf("save route: %v", err)
	}

	book, err := st.CreateBook(ctx, domain.CreateBookInput{
		Title:              "导演测试书",
		Genre:              domain.GenreXuanhuan,
		Premise:            "测试 AI 导演跨模块操控",
		TargetChapterWords: 2500,
	})
	if err != nil {
		t.Fatalf("create book: %v", err)
	}

	stub := &stubDirectorLLM{
		replies: []string{
			`{"thought":"先更新反派长老修为","action":"tool","tool_name":"upsert_entity","tool_args":{"kind":"character","name":"墨长老","summary":"外门执法长老，阴险护短","attributes":{"境界":"金丹初期"}}}`,
			`{"thought":"再创建第一章大纲","action":"tool","tool_name":"upsert_chapter_outline","tool_args":{"title":"墨长老施压","emotion_tag":"suppress","hook_type":"crisis","hook_note":"大比提前至三日后","outline_summary":"墨长老以金丹威压震慑外门，逼林渊签生死状。","beats":[{"stage":"suppress","summary":"墨长老现身演武场","conflict_note":"修为压制","target_words":800}]}}`,
			`{"thought":"全部完成","action":"final","reply":"已将墨长老修为设为金丹初期，并新增第 1 章《墨长老施压》的大纲与节拍。"}`,
		},
	}

	comp := compiler.New(st, compiler.WithMemorySource(st), compiler.WithStyleSource(st))
	runner := New(st, comp, stub, nil)

	var seenTools int
	reply, err := runner.RunDirectorChat(ctx, domain.DirectorChatRequest{
		BookID:      book.ID,
		Instruction: "加一个金丹初期的反派墨长老，并规划第一章大纲",
	}, func(ev domain.DirectorEvent) error {
		if ev.Kind == domain.DirectorEventToolResult {
			seenTools++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunDirectorChat failed: %v", err)
	}
	if !reply.Mutated {
		t.Fatalf("expected Mutated=true")
	}
	if len(reply.ToolCalls) != 2 || seenTools != 2 {
		t.Fatalf("expected 2 tool calls, got %d (seen=%d)", len(reply.ToolCalls), seenTools)
	}
	if reply.ToolCalls[0].Status != "ok" || reply.ToolCalls[1].Status != "ok" {
		t.Fatalf("expected all tools ok, got %+v", reply.ToolCalls)
	}

	ents, err := st.ListEntities(ctx, book.ID, domain.EntityFilter{})
	if err != nil || len(ents) != 1 || ents[0].Attributes["境界"] != "金丹初期" {
		t.Fatalf("expected entity 墨长老 persisted, got %+v (err=%v)", ents, err)
	}
	chaps, err := st.ListBookChapters(ctx, book.ID)
	if err != nil || len(chaps) != 1 || chaps[0].Title != "墨长老施压" {
		t.Fatalf("expected chapter persisted, got %+v (err=%v)", chaps, err)
	}
}

func TestDirectorUpdateBookPreservesProfile(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "director-profile.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	prov, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name: "DirProv", Protocol: domain.ProtocolOpenAICompat, BaseURL: "https://api.example.com/v1", APIKey: "sk-dir", Models: []string{"dir-model"}, Enabled: true,
	})
	_, _ = st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage: domain.StageOutline, ProviderID: &prov.ID, ModelName: "dir-model", Temperature: 0.4, MaxTokens: 4096,
	})
	book, err := st.CreateBook(ctx, domain.CreateBookInput{
		Title:              "原始书名",
		Genre:              domain.GenreXuanhuan,
		Premise:            "原始简介",
		TargetChapterWords: 2500,
		POVMode:            "first_person",
		PacingStyle:        "fast_cool",
		WritingPrompt:      "禁止水字数，句句见血",
		DailyWordGoal:      6000,
	})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	stub := &stubDirectorLLM{
		replies: []string{
			`{"thought":"修改简介","action":"tool","tool_name":"update_book","tool_args":{"premise":"新简介"}}`,
			`{"thought":"完成","action":"final","reply":"已更新简介"}`,
		},
	}
	runner := New(st, compiler.New(st), stub, nil)
	if _, err := runner.RunDirectorChat(ctx, domain.DirectorChatRequest{
		BookID:      book.ID,
		Instruction: "把简介改成新简介",
	}, nil); err != nil {
		t.Fatalf("RunDirectorChat: %v", err)
	}

	after, err := st.GetBook(ctx, book.ID)
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if after.Premise != "新简介" {
		t.Fatalf("expected updated premise, got %q", after.Premise)
	}
	if after.POVMode != "first_person" || after.PacingStyle != "fast_cool" || after.WritingPrompt != "禁止水字数，句句见血" || after.DailyWordGoal != 6000 {
		t.Fatalf("expected profile fields preserved, got %+v", after)
	}
}

func TestDirectorAutoCreatesSafetySnapshotOnMutatingTurn(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "nib_director_undo.db")
	st, err := store.Open(dbFile)
	if err != nil {
		t.Fatalf("open disk store: %v", err)
	}
	defer st.Close()

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "导演撤销点测试", Genre: domain.GenreXuanhuan, Premise: "原初设定"})
	prov, _ := st.UpsertProvider(ctx, domain.UpsertProviderInput{Name: "stub", Protocol: domain.ProtocolOpenAICompat, BaseURL: "http://localhost", Models: []string{"m"}, Enabled: true})
	_, _ = st.SaveModelRoute(ctx, domain.ModelRoute{Stage: domain.StageOutline, ProviderID: &prov.ID, ModelName: "m"})

	// 1. 纯只读轮次不得产生 UndoSnapshotID
	readOnlyLLM := &stubDirectorLLM{
		replies: []string{
			`{"thought":"查看概览","action":"tool","tool_name":"get_book_overview","tool_args":{}}`,
			`{"thought":"汇报","action":"final","reply":"全书设定正常"}`,
		},
	}
	runner1 := New(st, compiler.New(st), readOnlyLLM, nil)
	rep1, err := runner1.RunDirectorChat(ctx, domain.DirectorChatRequest{BookID: book.ID, Instruction: "看看书架概览"}, nil)
	if err != nil {
		t.Fatalf("readOnly RunDirectorChat: %v", err)
	}
	if rep1.Mutated || rep1.UndoSnapshotID != 0 {
		t.Fatalf("expected no mutation or undo snapshot for read-only turn, got %+v", rep1)
	}

	// 2. 写库轮次自动生成 UndoSnapshotID 并允许一键回滚到修改前状态
	mutLLM := &stubDirectorLLM{
		replies: []string{
			`{"thought":"改简介","action":"tool","tool_name":"update_book","tool_args":{"premise":"已被导演改歪的简介"}}`,
			`{"thought":"完成","action":"final","reply":"已调整简介"}`,
		},
	}
	runner2 := New(st, compiler.New(st), mutLLM, nil)
	rep2, err := runner2.RunDirectorChat(ctx, domain.DirectorChatRequest{BookID: book.ID, Instruction: "改一下简介"}, nil)
	if err != nil {
		t.Fatalf("mutating RunDirectorChat: %v", err)
	}
	if !rep2.Mutated || rep2.UndoSnapshotID <= 0 {
		t.Fatalf("expected Mutated=true and UndoSnapshotID > 0, got %+v", rep2)
	}

	bMut, _ := st.GetBook(ctx, book.ID)
	if bMut.Premise != "已被导演改歪的简介" {
		t.Fatalf("expected premise modified before undo, got %q", bMut.Premise)
	}

	if _, err := st.RestoreSafetySnapshot(ctx, rep2.UndoSnapshotID); err != nil {
		t.Fatalf("RestoreSafetySnapshot: %v", err)
	}
	bRestored, _ := st.GetBook(ctx, book.ID)
	if bRestored.Premise != "原初设定" {
		t.Fatalf("expected book premise restored after undo snapshot, got %q", bRestored.Premise)
	}
}
