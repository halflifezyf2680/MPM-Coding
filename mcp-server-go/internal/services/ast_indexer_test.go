package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hasArg(args []string, key string) bool {
	for _, arg := range args {
		if arg == key {
			return true
		}
	}
	return false
}

func argValue(args []string, key string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}

func toSet(csv string) map[string]bool {
	set := make(map[string]bool)
	for _, item := range strings.Split(csv, ",") {
		v := strings.TrimSpace(item)
		if v != "" {
			set[v] = true
		}
	}
	return set
}

func TestDetectTechStackAndConfig_DetectsGoWithoutRootGoMod(t *testing.T) {
	root := t.TempDir()
	goFile := filepath.Join(root, "nested", "service", "handler.go")
	if err := os.MkdirAll(filepath.Dir(goFile), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(goFile, []byte("package service\n\nfunc Handle() {}\n"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	exts, _ := detectTechStackAndConfig(root)
	extSet := toSet(exts)

	if !extSet["go"] {
		t.Fatalf("expected go extension to be detected, got %q", exts)
	}
}

func TestDetectTechStackAndConfig_GoDoesNotIgnorePkgDir(t *testing.T) {
	root := t.TempDir()
	goMod := filepath.Join(root, "go.mod")
	if err := os.WriteFile(goMod, []byte("module example.com/test\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod failed: %v", err)
	}

	_, ignores := detectTechStackAndConfig(root)
	ignoreSet := toSet(ignores)

	if ignoreSet["pkg"] {
		t.Fatalf("pkg should not be ignored for go projects, got ignores %q", ignores)
	}
}

func TestDetectTechStackAndConfig_DetectsHtmlOnlyFrontend(t *testing.T) {
	root := t.TempDir()
	htmlFile := filepath.Join(root, "index.html")
	if err := os.WriteFile(htmlFile, []byte("<main id=\"app\"></main>\n"), 0644); err != nil {
		t.Fatalf("write html file failed: %v", err)
	}

	exts, _ := detectTechStackAndConfig(root)
	extSet := toSet(exts)

	if !extSet["html"] {
		t.Fatalf("expected html extension to be detected, got %q", exts)
	}
}

func TestDetectTechStackAndConfig_DetectsJsxFrontend(t *testing.T) {
	root := t.TempDir()
	jsxFile := filepath.Join(root, "App.jsx")
	if err := os.WriteFile(jsxFile, []byte("export function App() { return <main /> }\n"), 0644); err != nil {
		t.Fatalf("write jsx file failed: %v", err)
	}

	exts, _ := detectTechStackAndConfig(root)
	extSet := toSet(exts)

	if !extSet["jsx"] {
		t.Fatalf("expected jsx extension to be detected, got %q", exts)
	}
}

func TestDetectTechStackAndConfig_DoesNotAdvertiseUnsupportedVueSvelte(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "App.vue"), []byte("<template><div /></template>\n"), 0644); err != nil {
		t.Fatalf("write vue file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "App.svelte"), []byte("<script>let x = 1;</script>\n"), 0644); err != nil {
		t.Fatalf("write svelte file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.css"), []byte("body { color: red; }\n"), 0644); err != nil {
		t.Fatalf("write css file failed: %v", err)
	}

	exts, _ := detectTechStackAndConfig(root)
	extSet := toSet(exts)

	if extSet["vue"] || extSet["svelte"] {
		t.Fatalf("unsupported frontend extensions should not be advertised, got %q", exts)
	}
	// CSS is now supported via tree-sitter-css
	if !extSet["css"] {
		t.Fatalf("expected css extension to be detected, got %q", exts)
	}
}

func TestBuildIndexArgs_DefaultDoesNotPassExtensions(t *testing.T) {
	args := buildIndexArgs("D:/repo", "D:/repo/.mpm-data/symbols.db", "D:/repo/.mpm-data/.ast_result_index.json", "node_modules,.git", "go,py", "", false, false)

	if hasArg(args, "--extensions") {
		t.Fatalf("default args should not include --extensions, got %v", args)
	}
	if !hasArg(args, "--ignore-dirs") {
		t.Fatalf("expected --ignore-dirs in args, got %v", args)
	}
}

func TestBuildIndexArgs_RetryCanPassExtensions(t *testing.T) {
	args := buildIndexArgs("D:/repo", "D:/repo/.mpm-data/symbols.db", "D:/repo/.mpm-data/.ast_result_index.json", "node_modules,.git", "go,py", "", true, false)

	if !hasArg(args, "--extensions") {
		t.Fatalf("retry args should include --extensions, got %v", args)
	}
	if got := argValue(args, "--extensions"); got != "go,py" {
		t.Fatalf("unexpected --extensions value: got %q", got)
	}
}

func TestBuildIndexArgs_ForceFullAddsFlag(t *testing.T) {
	args := buildIndexArgs("D:/repo", "D:/repo/.mpm-data/symbols.db", "D:/repo/.mpm-data/.ast_result_index.json", "node_modules,.git", "go,py", "", false, true)

	if !hasArg(args, "--force-full") {
		t.Fatalf("expected --force-full in args, got %v", args)
	}
}

func TestGetOutputPath_ReturnsUniqueTempFiles(t *testing.T) {
	root := t.TempDir()
	first := getOutputPath(root, "query")
	second := getOutputPath(root, "query")

	if first == second {
		t.Fatalf("expected unique output paths, got same path %q", first)
	}
	if filepath.Dir(first) != filepath.Join(root, ".mpm-data") {
		t.Fatalf("expected first path under .mpm-data, got %q", first)
	}
	if filepath.Dir(second) != filepath.Join(root, ".mpm-data") {
		t.Fatalf("expected second path under .mpm-data, got %q", second)
	}
	if !strings.Contains(filepath.Base(first), ".ast_result_query_") {
		t.Fatalf("unexpected first file name: %q", filepath.Base(first))
	}
	if !strings.Contains(filepath.Base(second), ".ast_result_query_") {
		t.Fatalf("unexpected second file name: %q", filepath.Base(second))
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("temp output path should not leave a pre-created file, stat err=%v", err)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatalf("temp output path should not leave a pre-created file, stat err=%v", err)
	}
}
