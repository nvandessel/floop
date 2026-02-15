package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileGraphStore(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	// Verify .floop directory was created
	floopDir := filepath.Join(tmpDir, ".floop")
	if _, err := os.Stat(floopDir); os.IsNotExist(err) {
		t.Error(".floop directory was not created")
	}
}

func TestFileGraphStore_AddGetNode(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{
		ID:   "test-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test Behavior",
		},
	}

	// Add node
	id, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}
	if id != "test-1" {
		t.Errorf("AddNode() returned id = %v, want test-1", id)
	}

	// Get node
	got, err := store.GetNode(ctx, "test-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetNode() returned nil")
	}
	if got.ID != "test-1" {
		t.Errorf("GetNode() ID = %v, want test-1", got.ID)
	}
	if got.Kind != "behavior" {
		t.Errorf("GetNode() Kind = %v, want behavior", got.Kind)
	}
}

func TestFileGraphStore_AddNodeRequiresID(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{Kind: "behavior"} // Missing ID

	_, err = store.AddNode(ctx, node)
	if err == nil {
		t.Error("AddNode() expected error for missing ID")
	}
}

func TestFileGraphStore_UpdateNode(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{
		ID:      "test-1",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "Original"},
	}

	store.AddNode(ctx, node)

	// Update
	node.Content["name"] = "Updated"
	err = store.UpdateNode(ctx, node)
	if err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}

	// Verify
	got, _ := store.GetNode(ctx, "test-1")
	if got.Content["name"] != "Updated" {
		t.Errorf("UpdateNode() did not persist, got name = %v", got.Content["name"])
	}
}

func TestFileGraphStore_UpdateNodeNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{ID: "nonexistent", Kind: "behavior"}

	err = store.UpdateNode(ctx, node)
	if err == nil {
		t.Error("UpdateNode() expected error for nonexistent node")
	}
}

func TestFileGraphStore_DeleteNode(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{ID: "test-1", Kind: "behavior"}
	store.AddNode(ctx, node)

	// Add an edge involving this node
	store.AddEdge(ctx, Edge{Source: "test-1", Target: "other", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})

	// Delete
	err = store.DeleteNode(ctx, "test-1")
	if err != nil {
		t.Fatalf("DeleteNode() error = %v", err)
	}

	// Verify node is gone
	got, _ := store.GetNode(ctx, "test-1")
	if got != nil {
		t.Error("DeleteNode() did not remove node")
	}

	// Verify edge is also gone
	edges, _ := store.GetEdges(ctx, "test-1", DirectionBoth, "")
	if len(edges) != 0 {
		t.Error("DeleteNode() did not remove associated edges")
	}
}

func TestFileGraphStore_QueryNodes(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.AddNode(ctx, Node{ID: "b-1", Kind: "behavior"})
	store.AddNode(ctx, Node{ID: "c-1", Kind: "correction"})
	store.AddNode(ctx, Node{ID: "b-2", Kind: "behavior"})

	// Query by kind
	results, err := store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("QueryNodes() error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("QueryNodes() returned %d results, want 2", len(results))
	}
}

func TestFileGraphStore_Edges(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.AddNode(ctx, Node{ID: "a", Kind: "behavior"})
	store.AddNode(ctx, Node{ID: "b", Kind: "behavior"})
	store.AddNode(ctx, Node{ID: "c", Kind: "behavior"})

	store.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})
	store.AddEdge(ctx, Edge{Source: "a", Target: "c", Kind: "overrides", Weight: 1.0, CreatedAt: time.Now()})

	// Test outbound
	edges, _ := store.GetEdges(ctx, "a", DirectionOutbound, "")
	if len(edges) != 2 {
		t.Errorf("GetEdges(outbound) = %d, want 2", len(edges))
	}

	// Test inbound
	edges, _ = store.GetEdges(ctx, "b", DirectionInbound, "")
	if len(edges) != 1 {
		t.Errorf("GetEdges(inbound) = %d, want 1", len(edges))
	}

	// Test with kind filter
	edges, _ = store.GetEdges(ctx, "a", DirectionOutbound, "requires")
	if len(edges) != 1 {
		t.Errorf("GetEdges(requires) = %d, want 1", len(edges))
	}

	// Test remove
	store.RemoveEdge(ctx, "a", "b", "requires")
	edges, _ = store.GetEdges(ctx, "a", DirectionOutbound, "")
	if len(edges) != 1 {
		t.Errorf("After RemoveEdge, GetEdges = %d, want 1", len(edges))
	}
}

func TestFileGraphStore_Traverse(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Build a graph: a -> b -> c
	store.AddNode(ctx, Node{ID: "a", Kind: "behavior"})
	store.AddNode(ctx, Node{ID: "b", Kind: "behavior"})
	store.AddNode(ctx, Node{ID: "c", Kind: "behavior"})
	store.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})
	store.AddEdge(ctx, Edge{Source: "b", Target: "c", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})

	// Traverse from a with depth 2
	results, _ := store.Traverse(ctx, "a", []string{"requires"}, DirectionOutbound, 2)
	if len(results) != 3 {
		t.Errorf("Traverse() = %d nodes, want 3", len(results))
	}

	// Traverse from a with depth 1
	results, _ = store.Traverse(ctx, "a", []string{"requires"}, DirectionOutbound, 1)
	if len(results) != 2 {
		t.Errorf("Traverse(depth=1) = %d nodes, want 2", len(results))
	}
}

func TestFileGraphStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store and add data
	store1, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}

	ctx := context.Background()
	store1.AddNode(ctx, Node{ID: "persist-1", Kind: "behavior", Content: map[string]interface{}{"name": "Test"}})
	store1.AddEdge(ctx, Edge{Source: "persist-1", Target: "other", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})

	// Close to persist
	if err := store1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Reopen and verify
	store2, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() reopen error = %v", err)
	}
	defer store2.Close()

	got, _ := store2.GetNode(ctx, "persist-1")
	if got == nil {
		t.Fatal("Persisted node not found after reopen")
	}
	if got.Content["name"] != "Test" {
		t.Errorf("Persisted node content = %v, want Test", got.Content["name"])
	}

	edges, _ := store2.GetEdges(ctx, "persist-1", DirectionOutbound, "")
	if len(edges) != 1 {
		t.Errorf("Persisted edges = %d, want 1", len(edges))
	}
}

func TestFileGraphStore_SyncOnlyWhenDirty(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}

	ctx := context.Background()

	// Sync without changes should be no-op (no error)
	if err := store.Sync(ctx); err != nil {
		t.Errorf("Sync() with no changes error = %v", err)
	}

	// After adding, sync should write
	store.AddNode(ctx, Node{ID: "test", Kind: "behavior"})
	if err := store.Sync(ctx); err != nil {
		t.Errorf("Sync() after add error = %v", err)
	}

	// Verify files exist
	nodesFile := filepath.Join(tmpDir, ".floop", "nodes.jsonl")
	if _, err := os.Stat(nodesFile); os.IsNotExist(err) {
		t.Error("nodes.jsonl was not created after Sync")
	}

	store.Close()
}

func TestFileGraphStore_GetNodeNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	got, err := store.GetNode(ctx, "nonexistent")
	if err != nil {
		t.Errorf("GetNode() error = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("GetNode() = %v, want nil for nonexistent", got)
	}
}
