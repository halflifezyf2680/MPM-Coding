package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-server-go/internal/core"

	"github.com/mark3labs/mcp-go/mcp"
)

func callKnownFactsTool(t *testing.T, sm *SessionManager, args map[string]any) string {
	t.Helper()
	handler := wrapSaveFact(sm)
	result, err := handler(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "known_facts",
			Arguments: args,
		},
	})
	if err != nil {
		t.Fatalf("known_facts call failed: %v", err)
	}
	return getTextResult(t, result)
}

func newTestMemorySession(t *testing.T) *SessionManager {
	t.Helper()
	projectTempRoot := filepath.Join(".", ".tmp-tests")
	if err := os.MkdirAll(projectTempRoot, 0755); err != nil {
		t.Fatalf("failed to create test root dir: %v", err)
	}
	root, err := os.MkdirTemp(projectTempRoot, "known-facts-tool-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	mem, err := core.NewMemoryLayer(root)
	if err != nil {
		t.Fatalf("NewMemoryLayer failed: %v", err)
	}
	return &SessionManager{ProjectRoot: root, Memory: mem}
}

func TestKnownFactsTool_StrategyLifecycle(t *testing.T) {
	sm := newTestMemorySession(t)

	legacyText := callKnownFactsTool(t, sm, map[string]any{
		"type":      "避坑",
		"summarize": "修改 task_chain 持久化时必须保持事件 payload 可审计",
	})
	if !strings.Contains(legacyText, "事实已存入数据库") {
		t.Fatalf("legacy output unexpected: %s", legacyText)
	}

	beforeText := callKnownFactsTool(t, sm, map[string]any{
		"mode": "before_action",
		"context": map[string]any{
			"task":    "修改 task_chain 持久化",
			"task_id": "task_demo",
			"intent":  "develop",
			"phase":   "implementation",
			"risk":    "medium",
			"files":   []string{"mcp-server-go/internal/tools/task_chain_v3_api.go"},
			"symbols": []string{"completePhaseV3"},
			"tools":   []string{"code_impact", "apply_patch"},
		},
	})
	if !strings.Contains(beforeText, "Relevant Known Facts") || !strings.Contains(beforeText, "Strategy") {
		t.Fatalf("before_action output unexpected: %s", beforeText)
	}
	if !strings.Contains(beforeText, "payload 可审计") {
		t.Fatalf("before_action should recall saved fact: %s", beforeText)
	}

	facts, err := sm.Memory.QueryFacts(context.Background(), "task_chain", 10)
	if err != nil {
		t.Fatalf("QueryFacts failed: %v", err)
	}
	if len(facts) == 0 {
		t.Fatalf("expected facts after before_action")
	}
	if facts[0].HitCount == 0 {
		t.Fatalf("before_action should mark exposure: %+v", facts[0])
	}

	afterText := callKnownFactsTool(t, sm, map[string]any{
		"mode": "after_action",
		"context": map[string]any{
			"task":    "修改 task_chain 持久化",
			"task_id": "task_demo",
			"phase":   "implementation",
			"files":   []string{"mcp-server-go/internal/tools/task_chain_v3_api.go"},
		},
		"outcome": map[string]any{
			"result":        "success",
			"signal":        "test_passed",
			"summary":       "持久化结构已验证",
			"adopted_facts": []int64{facts[0].ID},
			"new_observations": []string{
				"known_facts after_action 应记录 outcome 事件",
			},
		},
	})
	if !strings.Contains(afterText, "KnownFact evolution updated") {
		t.Fatalf("after_action output unexpected: %s", afterText)
	}
	if !strings.Contains(afterText, "candidate fact") {
		t.Fatalf("after_action should create candidate fact: %s", afterText)
	}

	updated, err := sm.Memory.QueryFacts(context.Background(), "task_chain known_facts", 10)
	if err != nil {
		t.Fatalf("QueryFacts updated failed: %v", err)
	}
	var sawAdopted, sawCandidate bool
	for _, fact := range updated {
		if fact.ID == facts[0].ID && fact.AdoptCount > 0 && fact.SupportCount > 0 {
			sawAdopted = true
		}
		if strings.Contains(fact.Summarize, "after_action") && fact.Status == "candidate" {
			sawCandidate = true
		}
	}
	if !sawAdopted {
		t.Fatalf("adopted fact counters not updated: %+v", updated)
	}
	if !sawCandidate {
		t.Fatalf("candidate observation not created: %+v", updated)
	}

	statusText := callKnownFactsTool(t, sm, map[string]any{
		"mode":  "status",
		"limit": 5,
	})
	if !strings.Contains(statusText, "KnownFact status") {
		t.Fatalf("status output unexpected: %s", statusText)
	}

	maintainText := callKnownFactsTool(t, sm, map[string]any{
		"mode": "maintain",
	})
	if !strings.Contains(maintainText, "KnownFact maintenance snapshot") {
		t.Fatalf("maintain output unexpected: %s", maintainText)
	}
}

func TestKnownFactsLegacyAddPersistsGlobalFactToRootClaude(t *testing.T) {
	sm := newTestMemorySession(t)

	text := callKnownFactsTool(t, sm, map[string]any{
		"type":      "铁律",
		"summarize": "全局事实必须同步到根目录 CLAUDE.md",
	})
	if !strings.Contains(text, "事实已存入数据库") {
		t.Fatalf("legacy output unexpected: %s", text)
	}

	rootClaude := filepath.Join(sm.ProjectRoot, "CLAUDE.md")
	content, err := os.ReadFile(rootClaude)
	if err != nil {
		t.Fatalf("expected root CLAUDE.md to be written: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "MPM Known Facts") {
		t.Fatalf("expected Known Facts section in root CLAUDE.md, got:\n%s", got)
	}
	if !strings.Contains(got, "- [铁律] 全局事实必须同步到根目录 CLAUDE.md") {
		t.Fatalf("expected saved fact in root CLAUDE.md, got:\n%s", got)
	}
}
