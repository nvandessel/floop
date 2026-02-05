package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewSQLiteGraphStore(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	// Verify .floop directory was created
	floopDir := filepath.Join(tmpDir, ".floop")
	if _, err := os.Stat(floopDir); os.IsNotExist(err) {
		t.Error(".floop directory was not created")
	}

	// Verify database file was created
	dbPath := filepath.Join(floopDir, "floop.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("floop.db was not created")
	}
}

func TestSQLiteGraphStore_AddGetNode(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{
		ID:   "test-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test Behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test canonical content",
			},
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
	if got.Kind != "behavior" { // Node.Kind is always "behavior" for behavior nodes
		t.Errorf("GetNode() Kind = %v, want behavior", got.Kind)
	}
}

func TestSQLiteGraphStore_AddNodeRequiresID(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{Kind: "behavior"} // Missing ID

	_, err = store.AddNode(ctx, node)
	if err == nil {
		t.Error("AddNode() expected error for missing ID")
	}
}

func TestSQLiteGraphStore_UpdateNode(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{
		ID:   "test-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Original",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Original content",
			},
		},
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

func TestSQLiteGraphStore_UpdateNodeNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{ID: "nonexistent", Kind: "behavior"}

	err = store.UpdateNode(ctx, node)
	if err == nil {
		t.Error("UpdateNode() expected error for nonexistent node")
	}
}

func TestSQLiteGraphStore_DeleteNode(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{
		ID:   "test-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content",
			},
		},
	}
	store.AddNode(ctx, node)

	// Add an edge involving this node
	store.AddEdge(ctx, Edge{Source: "test-1", Target: "other", Kind: "requires"})

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

func TestSQLiteGraphStore_QueryNodes(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add behavior nodes
	store.AddNode(ctx, Node{
		ID:   "b-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "B1",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Behavior 1",
			},
		},
	})
	store.AddNode(ctx, Node{
		ID:   "b-2",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "B2",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Behavior 2",
			},
		},
	})
	// Add a different kind
	store.AddNode(ctx, Node{
		ID:   "c-1",
		Kind: "correction",
		Content: map[string]interface{}{
			"name": "Correction 1",
			"kind": "correction",
			"content": map[string]interface{}{
				"canonical": "",
			},
		},
	})

	// Query by kind (node type, not behavior type)
	results, err := store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("QueryNodes() error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("QueryNodes() returned %d results, want 2", len(results))
	}
}

func TestSQLiteGraphStore_Edges(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add nodes
	for _, id := range []string{"a", "b", "c"} {
		store.AddNode(ctx, Node{
			ID:   id,
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": id,
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": id,
				},
			},
		})
	}

	store.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: "requires"})
	store.AddEdge(ctx, Edge{Source: "a", Target: "c", Kind: "overrides"})

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

func TestSQLiteGraphStore_Traverse(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Build a graph: a -> b -> c
	for _, id := range []string{"a", "b", "c"} {
		store.AddNode(ctx, Node{
			ID:   id,
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": id,
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": id,
				},
			},
		})
	}
	store.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: "requires"})
	store.AddEdge(ctx, Edge{Source: "b", Target: "c", Kind: "requires"})

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

func TestSQLiteGraphStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store and add data
	store1, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}

	ctx := context.Background()
	store1.AddNode(ctx, Node{
		ID:   "persist-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content",
			},
		},
	})
	store1.AddEdge(ctx, Edge{Source: "persist-1", Target: "other", Kind: "requires"})

	// Close to persist
	if err := store1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Reopen and verify
	store2, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() reopen error = %v", err)
	}
	defer store2.Close()

	got, _ := store2.GetNode(ctx, "persist-1")
	if got == nil {
		t.Fatal("Persisted node not found after reopen")
	}
	if got.Content["name"] != "Test" {
		t.Errorf("Persisted node content name = %v, want Test", got.Content["name"])
	}

	edges, _ := store2.GetEdges(ctx, "persist-1", DirectionOutbound, "")
	if len(edges) != 1 {
		t.Errorf("Persisted edges = %d, want 1", len(edges))
	}
}

func TestSQLiteGraphStore_SyncCreatesJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}

	ctx := context.Background()

	// Add a node
	store.AddNode(ctx, Node{
		ID:   "test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content",
			},
		},
	})

	// Sync
	if err := store.Sync(ctx); err != nil {
		t.Errorf("Sync() error = %v", err)
	}

	// Verify files exist
	nodesFile := filepath.Join(tmpDir, ".floop", "nodes.jsonl")
	if _, err := os.Stat(nodesFile); os.IsNotExist(err) {
		t.Error("nodes.jsonl was not created after Sync")
	}

	store.Close()
}

func TestSQLiteGraphStore_GetNodeNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
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

func TestSQLiteGraphStore_ImportExistingJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	os.MkdirAll(floopDir, 0755)

	// Create a nodes.jsonl file
	nodesFile := filepath.Join(floopDir, "nodes.jsonl")
	f, _ := os.Create(nodesFile)
	f.WriteString(`{"id":"imported-1","kind":"behavior","content":{"name":"Imported","kind":"directive","content":{"canonical":"Imported content"}},"metadata":{"confidence":0.8}}`)
	f.WriteString("\n")
	f.Close()

	// Create store - should auto-import
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	got, err := store.GetNode(ctx, "imported-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("Imported node not found")
	}
	if got.Content["name"] != "Imported" {
		t.Errorf("Imported node name = %v, want Imported", got.Content["name"])
	}
}

func TestSQLiteGraphStore_WhenConditions(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	node := Node{
		ID:   "when-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "When Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content",
			},
			"when": map[string]interface{}{
				"task":     "development",
				"language": "go",
			},
		},
	}

	store.AddNode(ctx, node)

	got, err := store.GetNode(ctx, "when-test")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("Node not found")
	}

	when, ok := got.Content["when"].(map[string]interface{})
	if !ok {
		t.Fatal("when not found in content")
	}

	if when["task"] != "development" {
		t.Errorf("when.task = %v, want development", when["task"])
	}
	if when["language"] != "go" {
		t.Errorf("when.language = %v, want go", when["language"])
	}
}

func TestSQLiteGraphStore_DirtyTracking(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Initially not dirty
	dirty, err := store.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if dirty {
		t.Error("IsDirty() = true, want false for empty store")
	}

	// Add a node
	store.AddNode(ctx, Node{
		ID:   "dirty-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Dirty Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content",
			},
		},
	})

	// Should be dirty now
	dirty, err = store.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if !dirty {
		t.Error("IsDirty() = false, want true after adding node")
	}

	// Sync should clear dirty
	store.Sync(ctx)

	dirty, err = store.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if dirty {
		t.Error("IsDirty() = true, want false after Sync")
	}
}

func TestSQLiteGraphStore_ConnectionPoolSettings(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	// Get the pool stats
	stats := store.db.Stats()

	// Verify pool settings are applied
	// MaxOpenConns should be 25
	if stats.MaxOpenConnections != 25 {
		t.Errorf("MaxOpenConnections = %d, want 25", stats.MaxOpenConnections)
	}
}
