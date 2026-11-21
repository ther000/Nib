package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"nib/internal/domain"
	"nib/internal/store"
)

func TestReverseExtractAndApplyLegacyBook(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "reverse.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	prov, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "TestProvider",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "sk-test-1234",
		Models:   []string{"test-model"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("upsert provider: %v", err)
	}
	if _, err := st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageExtract,
		ProviderID:  &prov.ID,
		ModelName:   "test-model",
		Temperature: 0.2,
		MaxTokens:   4096,
	}); err != nil {
		t.Fatalf("save extract route: %v", err)
	}

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "旧稿反推测试", Genre: domain.GenreXianxia})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	vol, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	if err != nil {
		t.Fatalf("UpsertVolume: %v", err)
	}
	for i := 1; i <= 3; i++ {
		ch, err := st.CreateChapter(ctx, domain.CreateChapterInput{
			BookID: book.ID, VolumeID: vol.ID, Title: "第" + string(rune('0'+i)) + "章",
		})
		if err != nil {
			t.Fatalf("CreateChapter %d: %v", i, err)
		}
		content := "沈青在青云山脚救下苏婉，两人发现掌门私通魔宗的令牌。"
		if _, err := st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{ChapterID: ch.ID, Content: content}); err != nil {
			t.Fatalf("SaveChapterDraft %d: %v", i, err)
		}
	}

	payload := `{
		"entities": [
			{"name":"沈青","kind":"character","aliases":["青哥"],"summary":"青云山外门弟子，重情重义","is_invariant":false,"attributes":{"境界":"筑基初期"}},
			{"name":"苏婉","kind":"character","aliases":[],"summary":"药堂弟子，心思缜密","is_invariant":false,"attributes":{}},
			{"name":"青云山","kind":"faction","aliases":[],"summary":"正道宗门","is_invariant":false,"attributes":{}}
		],
		"relations": [
			{"source_name":"沈青","target_name":"苏婉","relation_type":"同门","description":"共同调查掌门"}
		],
		"foreshadowings": [
			{"title":"掌门私通魔宗","description":"令牌上的魔纹尚未查明","planted_chapter_seq":1,"target_resolve_seq":10}
		],
		"arc_summaries": [
			{"level":"arc_10","start_seq":1,"end_seq":3,"summary":"沈青与苏婉发现宗门内鬼线索。"}
		]
	}`

	runner := New(st, nil, &stubGenesisLLM{payload: payload}, nil)
	var completed bool
	preview, err := runner.ReverseExtractBook(ctx, domain.ReverseExtractRequest{BookID: book.ID, MaxChapters: 3}, func(chunk domain.ReverseExtractChunk) error {
		if chunk.Kind == "completed" && chunk.Preview != nil {
			completed = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReverseExtractBook: %v", err)
	}
	if !completed || len(preview.Entities) != 3 || len(preview.Relations) != 1 || len(preview.Foreshadowings) != 1 || len(preview.ArcSummaries) != 1 {
		t.Fatalf("unexpected preview: %+v completed=%v", preview, completed)
	}

	apply, err := runner.ApplyReverseExtract(ctx, domain.ApplyReverseExtractInput{BookID: book.ID, Preview: preview})
	if err != nil {
		t.Fatalf("ApplyReverseExtract: %v", err)
	}
	if apply.EntitiesCreated != 3 || apply.RelationsCreated != 1 || apply.Foreshadowings != 1 || apply.ArcSummaries != 1 {
		t.Fatalf("unexpected apply result: %+v", apply)
	}
	entities, err := st.ListEntities(ctx, book.ID, domain.EntityFilter{})
	if err != nil || len(entities) != 3 {
		t.Fatalf("entities after apply: %+v err=%v", entities, err)
	}
	relations, err := st.ListEntityRelations(ctx, book.ID)
	if err != nil || len(relations) != 1 {
		t.Fatalf("relations after apply: %+v err=%v", relations, err)
	}
	foreshadowings, err := st.ListForeshadowings(ctx, book.ID, "")
	if err != nil || len(foreshadowings) != 1 {
		t.Fatalf("foreshadowings after apply: %+v err=%v", foreshadowings, err)
	}
}
