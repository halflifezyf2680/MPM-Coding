package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryLayerPersistTaskChain(t *testing.T) {
	projectTempRoot := filepath.Join(".", ".tmp-tests")
	if err := os.MkdirAll(projectTempRoot, 0755); err != nil {
		t.Fatalf("failed to create test root dir: %v", err)
	}
	root, err := os.MkdirTemp(projectTempRoot, "task-chain-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	ml, err := NewMemoryLayer(root)
	if err != nil {
		t.Fatalf("NewMemoryLayer failed: %v", err)
	}

	ctx := context.Background()
	rec := &TaskChainRecord{
		TaskID:       "task_demo",
		Description:  "demo",
		Protocol:     "linear",
		Status:       "running",
		PhasesJSON:   `[{"id":"main","name":"main","type":"execute","status":"pending"}]`,
		CurrentPhase: "main",
	}
	evt := &TaskChainEvent{
		TaskID:    "task_demo",
		PhaseID:   "main",
		EventType: "init",
		Payload:   "created",
	}

	if err := ml.PersistTaskChain(ctx, rec, evt); err != nil {
		t.Fatalf("PersistTaskChain failed: %v", err)
	}

	loaded, err := ml.LoadTaskChain(ctx, "task_demo")
	if err != nil {
		t.Fatalf("LoadTaskChain failed: %v", err)
	}
	if loaded == nil {
		t.Fatalf("expected task chain to be persisted")
	}
	if loaded.CurrentPhase != "main" {
		t.Fatalf("CurrentPhase = %q, want %q", loaded.CurrentPhase, "main")
	}

	events, err := ml.QueryTaskChainEvents(ctx, "task_demo", 10)
	if err != nil {
		t.Fatalf("QueryTaskChainEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != "init" {
		t.Fatalf("EventType = %q, want %q", events[0].EventType, "init")
	}
}
