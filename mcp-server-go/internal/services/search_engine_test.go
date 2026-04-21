package services

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustJSONLine(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	return string(raw)
}

func TestParseOutputIncludesContext(t *testing.T) {
	engine := NewRipgrepEngine()
	output := strings.Join([]string{
		mustJSONLine(t, map[string]any{
			"type": "context",
			"data": map[string]any{
				"path":        map[string]any{"text": "src/a.go"},
				"lines":       map[string]any{"text": "before line\n"},
				"line_number": 9,
			},
		}),
		mustJSONLine(t, map[string]any{
			"type": "match",
			"data": map[string]any{
				"path":            map[string]any{"text": "src/a.go"},
				"lines":           map[string]any{"text": "target line\n"},
				"line_number":     10,
				"absolute_offset": 0,
				"submatches": []map[string]any{
					{"match": map[string]any{"text": "target"}, "start": 0, "end": 6},
				},
			},
		}),
		mustJSONLine(t, map[string]any{
			"type": "context",
			"data": map[string]any{
				"path":        map[string]any{"text": "src/a.go"},
				"lines":       map[string]any{"text": "after line\n"},
				"line_number": 11,
			},
		}),
	}, "\n")

	results, err := engine.parseOutput([]byte(output))
	if err != nil {
		t.Fatalf("parseOutput failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ContextBefore != "before line" {
		t.Fatalf("ContextBefore = %q, want %q", results[0].ContextBefore, "before line")
	}
	if results[0].ContextAfter != "after line" {
		t.Fatalf("ContextAfter = %q, want %q", results[0].ContextAfter, "after line")
	}
}

func TestParseOutputSupportsLargeLines(t *testing.T) {
	engine := NewRipgrepEngine()
	large := strings.Repeat("x", 80*1024)
	output := mustJSONLine(t, map[string]any{
		"type": "match",
		"data": map[string]any{
			"path":            map[string]any{"text": "src/big.txt"},
			"lines":           map[string]any{"text": large + "\n"},
			"line_number":     1,
			"absolute_offset": 0,
			"submatches": []map[string]any{
				{"match": map[string]any{"text": "x"}, "start": 0, "end": 1},
			},
		},
	})

	results, err := engine.parseOutput([]byte(output))
	if err != nil {
		t.Fatalf("parseOutput failed for large line: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != large {
		t.Fatalf("large content was truncated: got %d bytes, want %d", len(results[0].Content), len(large))
	}
}
