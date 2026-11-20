package genres

import (
	"strings"
	"testing"

	"nib/internal/domain"
)

func TestListContainsAllFourMaleLeadGenres(t *testing.T) {
	presets := List()
	wantOrder := []domain.GenreID{
		domain.GenreXuanhuan,
		domain.GenreXianxia,
		domain.GenreUrban,
		domain.GenreGeneral,
	}
	if len(presets) != len(wantOrder) {
		t.Fatalf("List() len = %d, want %d", len(presets), len(wantOrder))
	}

	for i, wantID := range wantOrder {
		p := presets[i]
		if p.ID != wantID {
			t.Fatalf("presets[%d].ID = %q, want %q", i, p.ID, wantID)
		}
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.PacingFocus) == "" || strings.TrimSpace(p.SystemDirective) == "" {
			t.Fatalf("genre %q missing required descriptive metadata", p.ID)
		}
		if len(p.EntityFields) < 5 {
			t.Fatalf("genre %q expected at least 5 entity fields, got %d", p.ID, len(p.EntityFields))
		}
		if len(p.DefaultBeats) < 4 {
			t.Fatalf("genre %q expected at least 4 default beats, got %d", p.ID, len(p.DefaultBeats))
		}
		if len(p.AntiAIDirectives) < 3 {
			t.Fatalf("genre %q expected anti-AI directives, got %d", p.ID, len(p.AntiAIDirectives))
		}
		if len(p.AntiTropeRules) < 3 {
			t.Fatalf("genre %q expected anti-trope rules, got %d", p.ID, len(p.AntiTropeRules))
		}

		// 校验每个字段定义与节拍模板枚举均合法
		seenKeys := make(map[string]bool, len(p.EntityFields))
		for _, f := range p.EntityFields {
			if f.Key == "" || f.Label == "" {
				t.Fatalf("genre %q has empty field spec: %+v", p.ID, f)
			}
			if !f.EntityKind.Valid() {
				t.Fatalf("genre %q field %q has invalid entity kind %q", p.ID, f.Key, f.EntityKind)
			}
			if seenKeys[f.Key] {
				t.Fatalf("genre %q has duplicate field key %q", p.ID, f.Key)
			}
			seenKeys[f.Key] = true
		}
		for _, b := range p.DefaultBeats {
			if !b.Stage.Valid() {
				t.Fatalf("genre %q beat %q has invalid stage %q", p.ID, b.Title, b.Stage)
			}
			if strings.TrimSpace(b.Title) == "" || strings.TrimSpace(b.PromptHint) == "" || b.TargetWords <= 0 {
				t.Fatalf("genre %q has incomplete beat template: %+v", p.ID, b)
			}
		}
	}
}

func TestGenreFieldSpecsMatchProductSpec(t *testing.T) {
	expectedKeys := map[domain.GenreID][]string{
		domain.GenreXuanhuan: {"realm", "physique", "martial_arts", "treasure", "affiliation", "injury"},
		domain.GenreXianxia:  {"cultivation", "spirit_root", "magic_method", "natal_treasure", "lifespan", "status"},
		domain.GenreUrban:    {"cash_flow", "public_identity", "hidden_card", "network", "cooldown"},
		domain.GenreGeneral:  {"power_tier", "core_ability", "assets", "relation", "status_flag"},
	}

	for id, wantKeys := range expectedKeys {
		preset, ok := Get(id)
		if !ok {
			t.Fatalf("Get(%q) returned ok=false", id)
		}
		if len(preset.EntityFields) != len(wantKeys) {
			t.Fatalf("genre %q fields len = %d, want %d", id, len(preset.EntityFields), len(wantKeys))
		}
		for i, wantKey := range wantKeys {
			if preset.EntityFields[i].Key != wantKey {
				t.Fatalf("genre %q field[%d].Key = %q, want %q", id, i, preset.EntityFields[i].Key, wantKey)
			}
		}
	}
}

func TestGetReturnsDeepCopyAndFallback(t *testing.T) {
	p, ok := Get(domain.GenreXuanhuan)
	if !ok {
		t.Fatalf("expected xuanhuan preset")
	}
	p.EntityFields[0].Label = "被污染"
	p.AntiAIDirectives[0] = "被污染"

	fresh, _ := Get(domain.GenreXuanhuan)
	if fresh.EntityFields[0].Label == "被污染" || fresh.AntiAIDirectives[0] == "被污染" {
		t.Fatalf("Get() did not isolate internal registry via deep copy")
	}

	if _, ok := Get("nonexistent"); ok {
		t.Fatalf("expected ok=false for unknown genre")
	}
	fallback := GetOrDefault("nonexistent")
	if fallback.ID != domain.GenreGeneral {
		t.Fatalf("GetOrDefault fallback = %q, want %q", fallback.ID, domain.GenreGeneral)
	}
}

func TestPromptBlockIncludesCoreDirectives(t *testing.T) {
	for _, id := range domain.AllGenreIDs() {
		block := PromptBlock(id)
		if !strings.Contains(block, "防毒与逻辑铁线") || !strings.Contains(block, "去 AI 味与排版要求") {
			t.Fatalf("PromptBlock(%q) missing required sections:\n%s", id, block)
		}
		if !strings.Contains(block, "嘴角勾起") {
			t.Fatalf("PromptBlock(%q) missing common anti-AI directive", id)
		}
	}
}
