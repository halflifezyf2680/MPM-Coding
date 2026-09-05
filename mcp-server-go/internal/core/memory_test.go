package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryLayer_AddMemos(t *testing.T) {
	projectTempRoot := filepath.Join(".", ".tmp-tests")
	if err := os.MkdirAll(projectTempRoot, 0755); err != nil {
		t.Fatalf("Failed to create test root dir: %v", err)
	}
	tempDir, err := os.MkdirTemp(projectTempRoot, "mcp-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ml, err := NewMemoryLayer(tempDir)
	if err != nil {
		t.Fatalf("Failed to create MemoryLayer: %v", err)
	}

	ctx := context.Background()
	memos := []Memo{
		{
			Category: "测试",
			Entity:   "Unit Test",
			Act:      "Execute",
			Path:     "internal/core/memory_test.go",
			Content:  "Verification of memo logic",
		},
	}

	ids, err := ml.AddMemos(ctx, memos)
	if err != nil {
		t.Fatalf("AddMemos failed: %v", err)
	}

	if len(ids) != 1 {
		t.Errorf("Expected 1 memo ID, got %d", len(ids))
	}

	// 验证日志同步
	devLogPath := filepath.Join(tempDir, "dev-log.md")
	created := false
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(devLogPath); err == nil {
			created = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !created {
		t.Errorf("dev-log.md was not created")
	}

	// 验证查询功能
	results, err := ml.QueryMemos(ctx, "Verification", "", 10)
	if err != nil {
		t.Fatalf("QueryMemos failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result from QueryMemos, got %d", len(results))
	}

	if results[0].Entity != "Unit Test" {
		t.Errorf("Expected Entity 'Unit Test', got %s", results[0].Entity)
	}
}

func TestMemoryLayer_SearchMemosReranksAndPrunesLowQualityORMatches(t *testing.T) {
	projectTempRoot := filepath.Join(".", ".tmp-tests")
	if err := os.MkdirAll(projectTempRoot, 0755); err != nil {
		t.Fatalf("Failed to create test root dir: %v", err)
	}
	tempDir, err := os.MkdirTemp(projectTempRoot, "memo-rerank-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ml, err := NewMemoryLayer(tempDir)
	if err != nil {
		t.Fatalf("Failed to create MemoryLayer: %v", err)
	}

	ctx := context.Background()
	_, err = ml.AddMemos(ctx, []Memo{
		{
			Category: "开发",
			Entity:   "PythonDispatch",
			Act:      "修复",
			Path:     "internal/services/ast_indexer_rust/src/main.rs",
			Content:  "python dispatch argparse set_defaults func 解析必须生成动态调用边",
		},
		{
			Category: "开发",
			Entity:   "Docs",
			Act:      "整理",
			Path:     "docs/manual.md",
			Content:  "python 文档说明",
		},
		{
			Category: "开发",
			Entity:   "Readme",
			Act:      "整理",
			Path:     "README.md",
			Content:  "dispatch 单词示例",
		},
	})
	if err != nil {
		t.Fatalf("AddMemos failed: %v", err)
	}

	results, err := ml.SearchMemos(ctx, "python dispatch argparse set_defaults", "", 10)
	if err != nil {
		t.Fatalf("SearchMemos failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected strict output to prune weak OR matches, got %d: %+v", len(results), results)
	}
	if results[0].Entity != "PythonDispatch" {
		t.Fatalf("expected strongest multi-keyword match first, got %+v", results[0])
	}
}

