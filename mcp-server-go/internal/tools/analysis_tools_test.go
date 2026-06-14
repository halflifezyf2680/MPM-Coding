package tools

import (
	"mcp-server-go/internal/services"
	"strings"
	"testing"
)

func TestRenderImpactAlarmFramesDirectCallersAsPreChangeRisk(t *testing.T) {
	result := &services.ImpactResult{
		RiskLevel:       "medium",
		ComplexityScore: 18,
		AffectedNodes:   2,
		DirectCallers: []services.CallerInfo{
			{
				Node: services.Node{
					Name:      "RegisterAnalysisTools",
					FilePath:  "internal/tools/analysis_tools.go",
					LineStart: 39,
				},
			},
		},
	}

	out := renderImpactAlarm("wrapImpact", result)
	if !strings.Contains(out, "改动风险报警") {
		t.Fatalf("expected impact output to be framed as a change-risk alarm, got:\n%s", out)
	}
	if !strings.Contains(out, "有明显风险时，改之前先看这些点") {
		t.Fatalf("expected impact output to tell the LLM to inspect obvious risks before editing, got:\n%s", out)
	}
	if !strings.Contains(out, "明显风险点（改前优先检查）") {
		t.Fatalf("expected direct callers to be labeled as pre-change risk points, got:\n%s", out)
	}
	if strings.Contains(out, "候选影响锚点") || strings.Contains(out, "完整影响证明") {
		t.Fatalf("expected impact output not to imply a complete impact proof or old anchor framing, got:\n%s", out)
	}
}

func TestRenderImpactAlarmDoesNotTreatNoStaticCallersAsSafe(t *testing.T) {
	result := &services.ImpactResult{
		RiskLevel:       "low",
		ComplexityScore: 3,
		AffectedNodes:   0,
	}

	out := renderImpactAlarm("Run", result)
	if !strings.Contains(out, "未发现明显静态调用者") {
		t.Fatalf("expected no-caller output to say no obvious static callers were found, got:\n%s", out)
	}
	if !strings.Contains(out, "不等于修改一定安全") {
		t.Fatalf("expected no-caller output not to imply safety, got:\n%s", out)
	}
}
