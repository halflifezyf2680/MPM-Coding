package core

import (
	"context"
	"database/sql"
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

func TestMemoryLayer_KnownFactsPersistence(t *testing.T) {
	projectTempRoot := filepath.Join(".", ".tmp-tests")
	if err := os.MkdirAll(projectTempRoot, 0755); err != nil {
		t.Fatalf("Failed to create test root dir: %v", err)
	}
	tempDir, err := os.MkdirTemp(projectTempRoot, "known-facts-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ml, err := NewMemoryLayer(tempDir)
	if err != nil {
		t.Fatalf("Failed to create MemoryLayer: %v", err)
	}

	ctx := context.Background()
	legacyID, err := ml.SaveFact(ctx, "避坑", "修改 schema 后必须保持旧字段兼容")
	if err != nil {
		t.Fatalf("SaveFact failed: %v", err)
	}
	if legacyID == 0 {
		t.Fatalf("expected legacy fact id")
	}

	structuredID, err := ml.SaveKnownFact(ctx, KnownFact{
		Type:       "success_pattern",
		Summarize:  "行动前先定位持久化写入点",
		Scope:      "path:mcp-server-go/internal/core",
		Keywords:   "known_facts persistence",
		Confidence: 0.45,
		Status:     "candidate",
		SourceType: "observation",
		SourceID:   "test",
	})
	if err != nil {
		t.Fatalf("SaveKnownFact failed: %v", err)
	}

	facts, err := ml.QueryFacts(ctx, "schema known_facts", 10)
	if err != nil {
		t.Fatalf("QueryFacts failed: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}

	var foundLegacy, foundStructured bool
	for _, fact := range facts {
		switch fact.ID {
		case legacyID:
			foundLegacy = true
			if fact.Scope != "global" || fact.Status != "active" || fact.SourceType != "manual" {
				t.Fatalf("legacy defaults not preserved: %+v", fact)
			}
			if fact.Confidence <= 0 {
				t.Fatalf("legacy confidence should be set: %+v", fact)
			}
		case structuredID:
			foundStructured = true
			if fact.Status != "candidate" || fact.Scope != "path:mcp-server-go/internal/core" {
				t.Fatalf("structured fact fields not preserved: %+v", fact)
			}
		}
	}
	if !foundLegacy || !foundStructured {
		t.Fatalf("expected both legacy and structured facts, got %+v", facts)
	}

	eventID, err := ml.RecordFactEvent(ctx, FactEvent{
		EventType:        "exposure",
		FactID:           structuredID,
		TaskID:           "task_known_fact",
		Phase:            "p2",
		ContextSignature: "known_facts|persistence",
		PayloadJSON:      `{"rank":1,"score":0.9}`,
	})
	if err != nil {
		t.Fatalf("RecordFactEvent failed: %v", err)
	}
	if eventID == 0 {
		t.Fatalf("expected fact event id")
	}

	events, err := ml.QueryFactEvents(ctx, structuredID, 10)
	if err != nil {
		t.Fatalf("QueryFactEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != "exposure" || events[0].TaskID != "task_known_fact" {
		t.Fatalf("unexpected event: %+v", events[0])
	}

	if err := ml.MarkFactExposed(ctx, structuredID); err != nil {
		t.Fatalf("MarkFactExposed failed: %v", err)
	}
	if err := ml.ApplyFactOutcome(ctx, structuredID, true, "success"); err != nil {
		t.Fatalf("ApplyFactOutcome failed: %v", err)
	}
	updated, err := ml.QueryFacts(ctx, "known_facts", 10)
	if err != nil {
		t.Fatalf("QueryFacts after outcome failed: %v", err)
	}
	var updatedStructured *KnownFact
	for i := range updated {
		if updated[i].ID == structuredID {
			updatedStructured = &updated[i]
			break
		}
	}
	if updatedStructured == nil {
		t.Fatalf("structured fact not found after outcome")
	}
	if updatedStructured.HitCount != 1 {
		t.Fatalf("HitCount = %d, want 1", updatedStructured.HitCount)
	}
	if updatedStructured.AdoptCount != 1 || updatedStructured.SupportCount != 1 {
		t.Fatalf("outcome counters not updated: %+v", updatedStructured)
	}
	if updatedStructured.Confidence <= 0.45 {
		t.Fatalf("confidence should increase after success: %+v", updatedStructured)
	}
}

func TestDatabaseManager_HealsLegacyKnownFactsBeforeIndexes(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "mcp_memory.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE known_facts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT,
		summarize TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create legacy known_facts: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO known_facts(type, summarize) VALUES (?, ?)`, "避坑", "旧库迁移必须先补列再建索引"); err != nil {
		t.Fatalf("insert legacy fact: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	mgr, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("NewDatabaseManager failed: %v", err)
	}
	defer mgr.Close()

	var status string
	if err := mgr.QueryRow(`SELECT status FROM known_facts WHERE id = 1`).Scan(&status); err != nil {
		t.Fatalf("status column not healed: %v", err)
	}
	if status != "active" {
		t.Fatalf("status = %q, want active", status)
	}

	var updatedAt string
	if err := mgr.QueryRow(`SELECT updated_at FROM known_facts WHERE id = 1`).Scan(&updatedAt); err != nil {
		t.Fatalf("updated_at column not healed: %v", err)
	}
	if updatedAt == "" {
		t.Fatalf("updated_at should be backfilled")
	}

	var indexName string
	if err := mgr.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_known_facts_status'`).Scan(&indexName); err != nil {
		t.Fatalf("status index not created after migration: %v", err)
	}
}
