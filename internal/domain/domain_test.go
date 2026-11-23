package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "whitespace", raw: "   \t\n ", want: ""},
		{name: "very_short", raw: "abc", want: "****"},
		{name: "eight_chars", raw: "12345678", want: "****"},
		{name: "medium_no_prefix", raw: "1234567890", want: "***...7890"},
		{name: "openai_style", raw: "sk-1234567890abcdefa8f2", want: "sk-...a8f2"},
		{name: "anthropic_style", raw: "sk-ant-api03-999999999a8f2", want: "sk-...a8f2"},
		{name: "groq_style", raw: "gsk-abcdef1234567890z9x8", want: "gsk-...z9x8"},
		{name: "gemini_style", raw: "AIzaSyDummyKey123456a8f2", want: "***...a8f2"},
		{name: "trimmed_openai", raw: "  sk-abcdef123456a8f2  ", want: "sk-...a8f2"},
		{name: "short_sk_never_leaks_body", raw: "sk-12345", want: "****"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MaskAPIKey(tc.raw)
			if got != tc.want {
				t.Fatalf("MaskAPIKey(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			// 绝对不能回显原始密钥明文
			if trimmed := strings.TrimSpace(tc.raw); trimmed != "" && got == trimmed {
				t.Fatalf("MaskAPIKey leaked raw key: %q", got)
			}
		})
	}
}

func TestProviderToPublicAndJSONRedaction(t *testing.T) {
	p := Provider{
		ID:       7,
		Name:     "DeepSeek",
		Protocol: ProtocolOpenAICompat,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-super-secret-value-9988a8f2",
		Models:   []string{"deepseek-chat", "deepseek-reasoner"},
		Enabled:  true,
	}

	// 校验 Provider 直接 JSON 序列化不会泄露 APIKey
	rawBytes, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal(Provider) failed: %v", err)
	}
	if strings.Contains(string(rawBytes), "super-secret") {
		t.Fatalf("Provider JSON leaked APIKey: %s", string(rawBytes))
	}

	pub := p.ToPublic()
	if !pub.HasAPIKey {
		t.Fatalf("expected HasAPIKey=true")
	}
	if pub.APIKeyMasked != "sk-...a8f2" {
		t.Fatalf("APIKeyMasked = %q, want %q", pub.APIKeyMasked, "sk-...a8f2")
	}
	// 确认切片深拷贝，互不干扰
	pub.Models[0] = "mutated"
	if p.Models[0] != "deepseek-chat" {
		t.Fatalf("ToPublic did not copy Models slice")
	}

	// 空密钥场景
	pNoKey := Provider{ID: 8, Name: "Ollama", Protocol: ProtocolOpenAICompat, BaseURL: "http://127.0.0.1:11434/v1"}
	pubNoKey := pNoKey.ToPublic()
	if pubNoKey.HasAPIKey || pubNoKey.APIKeyMasked != "" {
		t.Fatalf("expected empty key state, got has=%v masked=%q", pubNoKey.HasAPIKey, pubNoKey.APIKeyMasked)
	}
}

func TestRedactSecretHeaders(t *testing.T) {
	in := map[string]string{
		"Authorization":  "Bearer sk-1234567890abcdefa8f2",
		"x-api-key":      "sk-ant-api03-abcdef123456",
		"x-goog-api-key": "AIzaSySecretTokenValue9988",
		"Content-Type":   "application/json",
	}
	got := RedactSecretHeaders(in)
	if got["Authorization"] != "Bearer sk-...a8f2" {
		t.Fatalf("Authorization = %q", got["Authorization"])
	}
	if got["x-api-key"] != "sk-...3456" {
		t.Fatalf("x-api-key = %q", got["x-api-key"])
	}
	if got["x-goog-api-key"] != "***...9988" {
		t.Fatalf("x-goog-api-key = %q", got["x-goog-api-key"])
	}
	if got["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type = %q", got["Content-Type"])
	}
	for k, v := range got {
		if strings.Contains(v, "1234567890") || strings.Contains(v, "SecretTokenValue") {
			t.Fatalf("header %s leaked secret: %q", k, v)
		}
	}
}

func TestUpsertProviderInputValidate(t *testing.T) {
	valid := UpsertProviderInput{
		Name:     "DeepSeek",
		Protocol: ProtocolOpenAICompat,
		BaseURL:  "https://api.deepseek.com/v1",
		Enabled:  true,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid input, got %v", err)
	}

	cases := []struct {
		name string
		mod  func(*UpsertProviderInput)
	}{
		{"empty_name", func(in *UpsertProviderInput) { in.Name = "   " }},
		{"bad_protocol", func(in *UpsertProviderInput) { in.Protocol = "unknown" }},
		{"empty_url", func(in *UpsertProviderInput) { in.BaseURL = "" }},
		{"non_http_scheme", func(in *UpsertProviderInput) { in.BaseURL = "ftp://example.com" }},
		{"missing_host", func(in *UpsertProviderInput) { in.BaseURL = "https://" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.mod(&in)
			err := in.Validate()
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestModelRouteValidateAndDefaults(t *testing.T) {
	defaults := DefaultModelRoutes()
	if len(defaults) != len(AllPipelineStages()) {
		t.Fatalf("expected %d default routes, got %d", len(AllPipelineStages()), len(defaults))
	}
	for _, r := range defaults {
		if err := r.Validate(); err != nil {
			t.Fatalf("default route %s invalid: %v", r.Stage, err)
		}
	}

	badTemp := ModelRoute{Stage: StageDraft, Temperature: 2.5, MaxTokens: 4096}
	if !errors.Is(badTemp.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for temperature=2.5")
	}
	badStage := ModelRoute{Stage: "invalid", Temperature: 0.7, MaxTokens: 2048}
	if !errors.Is(badStage.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for bad stage")
	}
	zeroID := int64(0)
	badPID := ModelRoute{Stage: StageDraft, ProviderID: &zeroID, Temperature: 0.7, MaxTokens: 2048}
	if !errors.Is(badPID.Validate(), ErrValidation) {
		t.Fatalf("expected ErrValidation for providerId=0")
	}
}

func TestBookVolumeChapterBeatEntityValidation(t *testing.T) {
	// Book
	if err := (CreateBookInput{Title: "太初剑渊", Genre: GenreXianxia, TargetChapterWords: 2800}).Validate(); err != nil {
		t.Fatalf("valid book failed: %v", err)
	}
	if !errors.Is((CreateBookInput{Title: "", Genre: GenreXianxia}).Validate(), ErrValidation) {
		t.Fatalf("empty title should fail validation")
	}
	if !errors.Is((CreateBookInput{Title: "太初剑渊", Genre: ""}).Validate(), ErrValidation) {
		t.Fatalf("empty genre should fail validation")
	}
	if err := (CreateBookInput{Title: "赛博修仙录", Genre: "赛博修仙"}).Validate(); err != nil {
		t.Fatalf("custom genre should pass validation: %v", err)
	}
	if !errors.Is((CreateBookInput{Title: "太初剑渊", Genre: GenreUrban, TargetChapterWords: 100}).Validate(), ErrValidation) {
		t.Fatalf("too small targetChapterWords should fail validation")
	}

	// Volume
	if err := (UpsertVolumeInput{BookID: 1, Seq: 1, Title: "第一卷 青云起"}).Validate(); err != nil {
		t.Fatalf("valid volume failed: %v", err)
	}
	if !errors.Is((UpsertVolumeInput{BookID: 0, Seq: 1, Title: "第一卷"}).Validate(), ErrValidation) {
		t.Fatalf("zero bookId should fail validation")
	}

	// Chapter
	if err := (CreateChapterInput{BookID: 1, VolumeID: 1, Title: "坊市暗流", EmotionTag: EmotionBurst, HookType: HookCrisis}).Validate(); err != nil {
		t.Fatalf("valid chapter failed: %v", err)
	}
	if !errors.Is((CreateChapterInput{BookID: 1, VolumeID: 1, Title: "坊市暗流", EmotionTag: "sad"}).Validate(), ErrValidation) {
		t.Fatalf("invalid emotion tag should fail validation")
	}
	if !errors.Is((SaveChapterDraftInput{ChapterID: 1, Content: "正文", Status: "bad"}).Validate(), ErrValidation) {
		t.Fatalf("invalid status should fail validation")
	}

	// Beat
	pov := int64(3)
	if err := (Beat{Stage: BeatStageTurn, Summary: "反杀夺丹", POVEntityID: &pov, TargetWords: 600}).Validate(); err != nil {
		t.Fatalf("valid beat failed: %v", err)
	}
	if !errors.Is((Beat{Stage: BeatStageTurn, Summary: "   "}).Validate(), ErrValidation) {
		t.Fatalf("empty beat summary should fail validation")
	}

	// Entity
	if err := (UpsertEntityInput{BookID: 1, Kind: EntityKindCharacter, Name: "林渊"}).Validate(); err != nil {
		t.Fatalf("valid entity failed: %v", err)
	}
	if !errors.Is((UpsertEntityInput{BookID: 1, Kind: "monster", Name: "灵兽"}).Validate(), ErrValidation) {
		t.Fatalf("invalid entity kind should fail validation")
	}
}

func TestEmotionWeightsAndCountWords(t *testing.T) {
	if EmotionSuppress.Weight() != 0 || EmotionSetup.Weight() != 1 || EmotionTurn.Weight() != 2 || EmotionHarvest.Weight() != 3 || EmotionBurst.Weight() != 4 {
		t.Fatalf("unexpected emotion tag weights")
	}
	text := "残阵灵光骤灭，焦糊味混着血腥气在石室散开。\n\n林渊抬手收剑。 123"
	// 21 + 7 + 3 = 31 non-whitespace runes
	if got := CountWords(text); got != 31 {
		t.Fatalf("CountWords = %d, want 31", got)
	}

	// 8000 个汉字 = 24000 UTF-8 bytes，按字数校验不应超过 20000 字上限
	note8k := UpsertScratchNoteInput{BookID: 1, Content: strings.Repeat("字", 8000)}
	if err := note8k.Validate(); err != nil {
		t.Fatalf("8000 CJK runes should pass validation, got %v", err)
	}
	noteOver := UpsertScratchNoteInput{BookID: 1, Content: strings.Repeat("字", 20001)}
	if !errors.Is(noteOver.Validate(), ErrValidation) {
		t.Fatalf("20001 CJK runes should fail validation")
	}
}
