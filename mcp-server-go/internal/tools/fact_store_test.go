package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFactFile(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, factFileName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFactTable(t *testing.T) {
	content := `# Known Facts

| 状态 | 类型 | 内容 | scope |
|---|---|---|---|
| active | 铁律 | 测试必须先失败 | project |
| candidate | success_pattern | 待验证经验 | symbol:get_db |
| rejected | 避坑 | 已证伪 | project |
| bogus | note | 非法状态行 | project |
| 列数不足
| active | note | 缺 scope 列 |
`
	rows, warnings := parseFactTable(content)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (active/candidate/rejected), got %+v", len(rows), rows)
	}
	if rows[0].Type != "铁律" || rows[1].Scope != "symbol:get_db" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "非法状态") {
		t.Fatalf("warnings = %v, want 1 illegal-status warning", warnings)
	}
}

func TestAppendFactRow(t *testing.T) {
	root := t.TempDir()

	// 首次写入自动建表头
	if err := appendFactRow(root, "candidate", "observation", "在CI下应该缓存go build缓存", "project"); err != nil {
		t.Fatalf("appendFactRow: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, factFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "| 状态 | 类型 | 内容 | scope |") {
		t.Fatalf("header missing:\n%s", data)
	}
	if !strings.Contains(string(data), "| candidate | observation | 在CI下应该缓存go build缓存 | project |") {
		t.Fatalf("row missing:\n%s", data)
	}

	// 追加；内容中的竖线被转义
	if err := appendFactRow(root, "active", "避坑", "路径含 a|b 时会破坏表格", ""); err != nil {
		t.Fatalf("appendFactRow second: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(root, factFileName))
	if !strings.Contains(string(data), "a∣b") {
		t.Fatalf("pipe not escaped:\n%s", data)
	}
	if got := strings.Count(string(data), "| active |"); got != 1 {
		t.Fatalf("appended row count = %d, want 1", got)
	}
}

func TestRenderRecallFacts(t *testing.T) {
	t.Run("missing file returns empty", func(t *testing.T) {
		if got := renderRecallFacts(t.TempDir(), "cache", 10); got != "" {
			t.Fatalf("want empty, got %q", got)
		}
	})

	t.Run("filters status and non-matching, orders pitfall first", func(t *testing.T) {
		root := t.TempDir()
		var sb strings.Builder
		sb.WriteString(factHeader)
		sb.WriteString("| active | success_pattern | cache the go build artifacts | project |\n")
		sb.WriteString("| active | 避坑 | cache must be invalidated on config change | project |\n")
		sb.WriteString("| candidate | success_pattern | cache everything aggressively | project |\n")
		sb.WriteString("| rejected | 避坑 | cache is unnecessary | project |\n")
		writeFactFile(t, root, sb.String())

		got := renderRecallFacts(root, "cache", 10)
		if !strings.Contains(got, "cache must be invalidated") || !strings.Contains(got, "cache the go build") {
			t.Fatalf("expected both matching active rows:\n%s", got)
		}
		if strings.Contains(got, "aggressively") || strings.Contains(got, "unnecessary") {
			t.Fatalf("candidate/rejected must be filtered:\n%s", got)
		}
		// 避坑优先于 success_pattern
		if strings.Index(got, "cache must be invalidated") > strings.Index(got, "cache the go build") {
			t.Fatalf("pitfall should rank first:\n%s", got)
		}
		if !strings.Contains(got, factFileName) {
			t.Fatalf("should mention fact file path:\n%s", got)
		}
	})

	t.Run("no keyword hit returns empty", func(t *testing.T) {
		root := t.TempDir()
		writeFactFile(t, root, factHeader+"| active | note | totally unrelated content | project |\n")
		if got := renderRecallFacts(root, "cache", 10); got != "" {
			t.Fatalf("want empty on no hit, got %q", got)
		}
	})

	t.Run("warns when active exceeds threshold", func(t *testing.T) {
		root := t.TempDir()
		var sb strings.Builder
		sb.WriteString(factHeader)
		for i := 0; i < factActiveThreshold+1; i++ {
			sb.WriteString("| active | note | cache row whatever | project |\n")
		}
		writeFactFile(t, root, sb.String())

		got := renderRecallFacts(root, "cache", 10)
		if !strings.Contains(got, "建议人工去重") {
			t.Fatalf("threshold warning missing:\n%s", got)
		}
		if got := strings.Count(got, "- **[note]**"); got > 10 {
			t.Fatalf("limit not applied: %d rows", got)
		}
	})
}

func TestAppendFactObservations(t *testing.T) {
	root := t.TempDir()
	written, err := appendFactObservations(root, []string{"  ", "在XX条件下应该YY", ""})
	if err != nil {
		t.Fatalf("appendFactObservations: %v", err)
	}
	if written != 1 {
		t.Fatalf("written = %d, want 1", written)
	}
	rows, warnings := parseFactTable(mustRead(t, root))
	if len(warnings) != 0 || len(rows) != 1 {
		t.Fatalf("rows=%d warnings=%v", len(rows), warnings)
	}
	if rows[0].Status != "candidate" || rows[0].Type != "observation" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}

func mustRead(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, factFileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
