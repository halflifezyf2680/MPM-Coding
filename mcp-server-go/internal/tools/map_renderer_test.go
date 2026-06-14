package tools

import (
	"mcp-server-go/internal/services"
	"strings"
	"testing"
)

func TestRenderStandardLabelsSymbolsAsNavigationAnchors(t *testing.T) {
	result := &services.MapResult{
		Statistics: services.Stats{TotalFiles: 1, TotalSymbols: 1},
		Structure: map[string][]services.Node{
			"internal/app.go": {
				{
					NodeType:  "function",
					Name:      "Run",
					FilePath:  "internal/app.go",
					LineStart: 12,
					LineEnd:   20,
				},
			},
		},
	}

	out := NewMapRenderer(result, "").RenderStandard()
	if !strings.Contains(out, "导航锚点") {
		t.Fatalf("expected map output to describe symbols as navigation anchors, got:\n%s", out)
	}
	if !strings.Contains(out, "按需阅读关键源码片段") {
		t.Fatalf("expected map output to encourage selective follow-up reading, got:\n%s", out)
	}
	if strings.Contains(out, "不能替代打开源文件") || strings.Contains(out, "打开目标文件阅读完整上下文") {
		t.Fatalf("expected map output not to force broad source reads, got:\n%s", out)
	}
}
