package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTimelineOpenCommands(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		target  string
		wantLen int
		first   openCommandSpec
	}{
		{
			name:    "windows uses native path with start + rundll32 first (robust for spaces/unicode project paths)",
			goos:    "windows",
			target:  `D:\My Projects\中文路径\project_timeline.html`,
			wantLen: 4,
			first: openCommandSpec{
				name: "cmd",
				args: []string{"/c", "start", "", `D:\My Projects\中文路径\project_timeline.html`},
			},
		},
		{
			name:    "darwin uses open",
			goos:    "darwin",
			target:  "/tmp/project_timeline.html",
			wantLen: 1,
			first: openCommandSpec{
				name: "open",
				args: []string{"/tmp/project_timeline.html"},
			},
		},
		{
			name:    "linux uses xdg-open first",
			goos:    "linux",
			target:  "/tmp/project_timeline.html",
			wantLen: 2,
			first: openCommandSpec{
				name: "xdg-open",
				args: []string{"/tmp/project_timeline.html"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTimelineOpenCommands(tt.goos, tt.target)
			if len(got) != tt.wantLen {
				t.Fatalf("len(commands) = %d, want %d", len(got), tt.wantLen)
			}
			if got[0].name != tt.first.name {
				t.Fatalf("first command name = %q, want %q", got[0].name, tt.first.name)
			}
			if len(got[0].args) != len(tt.first.args) {
				t.Fatalf("first command args len = %d, want %d", len(got[0].args), len(tt.first.args))
			}
			for i := range tt.first.args {
				if got[0].args[i] != tt.first.args[i] {
					t.Fatalf("first command args[%d] = %q, want %q", i, got[0].args[i], tt.first.args[i])
				}
			}
		})
	}
}

func TestInjectProjectRules(t *testing.T) {
	readAgents := func(root string) string {
		data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
		if err != nil {
			t.Fatalf("read AGENTS.md: %v", err)
		}
		return string(data)
	}

	t.Run("creates AGENTS.md with marker block when absent", func(t *testing.T) {
		root := t.TempDir()
		if err := injectProjectRules(root); err != nil {
			t.Fatalf("injectProjectRules: %v", err)
		}
		content := readAgents(root)
		if !strings.HasPrefix(content, mpmRulesBeginMarker) {
			t.Fatalf("content should start with begin marker, got prefix %.60q", content)
		}
		if !strings.Contains(content, mpmRulesEndMarker) {
			t.Fatal("content should contain end marker")
		}
	})

	t.Run("prepends block above existing content", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# User Notes\n\nmy rule\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := injectProjectRules(root); err != nil {
			t.Fatalf("injectProjectRules: %v", err)
		}
		content := readAgents(root)
		if !strings.HasPrefix(content, mpmRulesBeginMarker) {
			t.Fatal("block should be at top of file")
		}
		if !strings.Contains(content, "# User Notes") || !strings.Contains(content, "my rule") {
			t.Fatal("existing content must be preserved below the block")
		}
	})

	t.Run("re-inject is idempotent and keeps outside edits", func(t *testing.T) {
		root := t.TempDir()
		if err := injectProjectRules(root); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(readAgents(root)+"\n# user added later\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := injectProjectRules(root); err != nil {
			t.Fatal(err)
		}
		content := readAgents(root)
		if got := strings.Count(content, mpmRulesBeginMarker); got != 1 {
			t.Fatalf("begin marker count = %d, want 1", got)
		}
		if !strings.Contains(content, "# user added later") {
			t.Fatal("edits outside the block must survive re-injection")
		}
		if strings.Index(content, mpmRulesEndMarker) > strings.Index(content, "# user added later") {
			t.Fatal("user content appended after the block must stay after it")
		}
	})

	t.Run("rebuilds block when end marker was removed by hand", func(t *testing.T) {
		root := t.TempDir()
		if err := injectProjectRules(root); err != nil {
			t.Fatal(err)
		}
		content := readAgents(root)
		content = strings.Replace(content, mpmRulesEndMarker, "", 1)
		if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if err := injectProjectRules(root); err != nil {
			t.Fatal(err)
		}
		content = readAgents(root)
		if got := strings.Count(content, mpmRulesBeginMarker); got != 1 {
			t.Fatalf("begin marker count = %d, want 1", got)
		}
		if !strings.Contains(content, mpmRulesEndMarker) {
			t.Fatal("end marker should be restored")
		}
	})

	t.Run("coexists with arbitrary bottom sections", func(t *testing.T) {
		root := t.TempDir()
		existing := "# Project\n\n## Legacy Section\n- manual note kept by user\n"
		if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(existing), 0644); err != nil {
			t.Fatal(err)
		}
		if err := injectProjectRules(root); err != nil {
			t.Fatal(err)
		}
		content := readAgents(root)
		if !strings.Contains(content, "- manual note kept by user") {
			t.Fatal("bottom section must be preserved")
		}
		if strings.Index(content, mpmRulesBeginMarker) > strings.Index(content, "## Legacy Section") {
			t.Fatal("MPM protocol block must sit above bottom sections")
		}
	})
}
