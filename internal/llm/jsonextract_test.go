package llm

import (
	"strings"
	"testing"
)

func TestExtractJSONObjectHandlesFencesNestedBackticksAndProse(t *testing.T) {
	t.Run("skips earlier non-JSON code fence and prose braces", func(t *testing.T) {
		raw := "前言 {非JSON干扰}\n```go\nfunc main() { println(\"hi\") }\n```\n结果如下：\n```json\n{\"title\":\"第一章\",\"ok\":true}\n```"
		got, err := ExtractJSONObject(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != `{"title":"第一章","ok":true}` {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("preserves triple backticks inside JSON string literal", func(t *testing.T) {
		raw := "```json\n{\"body\":\"line1\\n```md\\nquoted\\n```\\nline2\",\"count\":2}\n```"
		got, err := ExtractJSONObject(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "{\"body\":\"line1\\n```md\\nquoted\\n```\\nline2\",\"count\":2}"
		if got != expected {
			t.Fatalf("got %q want %q", got, expected)
		}
	})

	t.Run("extracts multiple independent objects", func(t *testing.T) {
		raw := "第一段 {\"a\":1}\n第二段 {\"b\":{\"c\":2}}"
		all := ExtractAllJSONObjects(raw)
		if len(all) != 2 {
			t.Fatalf("len=%d, want 2: %v", len(all), all)
		}
		if all[0] != `{"a":1}` || all[1] != `{"b":{"c":2}}` {
			t.Fatalf("unexpected spans: %v", all)
		}
	})

	t.Run("returns explicit errors on empty or invalid", func(t *testing.T) {
		if _, err := ExtractJSONObject("  "); err == nil {
			t.Fatalf("expected error on empty")
		}
		if _, err := ExtractJSONObject("no braces"); err == nil {
			t.Fatalf("expected error without braces")
		}
		if _, err := ExtractJSONObject("{unclosed"); err == nil {
			t.Fatalf("expected error on unclosed")
		}
		if _, err := ExtractJSONObject("{bad: json}"); err == nil {
			t.Fatalf("expected error on malformed json")
		}
	})
}

func TestReadSSERejectsOversizedSingleLine(t *testing.T) {
	tooLong := "data: " + strings.Repeat("a", maxSSELineBytes+32) + "\n\n"
	err := readSSE(strings.NewReader(tooLong), func(ev sseEvent) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected error when SSE line exceeds %d bytes", maxSSELineBytes)
	}
	if !strings.Contains(err.Error(), "exceeds max size") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
