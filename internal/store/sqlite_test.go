package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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
	os.MkdirAll(floopDir, 0700)

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

func TestSQLiteGraphStore_IncrementalExport(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add two behaviors and sync
	store.AddNode(ctx, Node{
		ID:   "b-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Behavior 1",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "First behavior",
			},
		},
	})
	store.AddNode(ctx, Node{
		ID:   "b-2",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Behavior 2",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Second behavior",
			},
		},
	})

	// First sync - full export
	if err := store.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Get dirty IDs - should be empty after sync
	dirtyIDs, err := store.GetDirtyBehaviorIDs(ctx)
	if err != nil {
		t.Fatalf("GetDirtyBehaviorIDs() error = %v", err)
	}
	if len(dirtyIDs) != 0 {
		t.Errorf("After sync, dirty count = %d, want 0", len(dirtyIDs))
	}

	// Update only one behavior
	store.UpdateNode(ctx, Node{
		ID:   "b-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Behavior 1 Updated",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "First behavior updated",
			},
		},
	})

	// Only b-1 should be dirty
	dirtyIDs, err = store.GetDirtyBehaviorIDs(ctx)
	if err != nil {
		t.Fatalf("GetDirtyBehaviorIDs() error = %v", err)
	}
	if len(dirtyIDs) != 1 {
		t.Errorf("After update, dirty count = %d, want 1", len(dirtyIDs))
	}
	if len(dirtyIDs) == 1 && dirtyIDs[0] != "b-1" {
		t.Errorf("Dirty ID = %s, want b-1", dirtyIDs[0])
	}

	// Sync should use incremental export (only dirty behaviors)
	if err := store.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Dirty should be cleared
	dirtyIDs, err = store.GetDirtyBehaviorIDs(ctx)
	if err != nil {
		t.Fatalf("GetDirtyBehaviorIDs() error = %v", err)
	}
	if len(dirtyIDs) != 0 {
		t.Errorf("After incremental sync, dirty count = %d, want 0", len(dirtyIDs))
	}

	// Verify the file still contains both behaviors
	nodesFile := filepath.Join(tmpDir, ".floop", "nodes.jsonl")
	content, err := os.ReadFile(nodesFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	contentStr := string(content)
	if !contains(contentStr, "b-1") || !contains(contentStr, "b-2") {
		t.Error("nodes.jsonl should contain both behaviors")
	}
	if !contains(contentStr, "First behavior updated") {
		t.Error("nodes.jsonl should contain the updated content")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestValidateIntegrity(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Integrity check should pass on a fresh database
	if err := ValidateIntegrity(ctx, store.db); err != nil {
		t.Errorf("ValidateIntegrity() on fresh DB error = %v", err)
	}
}

func TestValidateIntegrity_WithData(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add some data
	store.AddNode(ctx, Node{
		ID:   "test-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content",
			},
		},
	})
	store.AddEdge(ctx, Edge{Source: "test-1", Target: "other", Kind: "requires"})

	// Integrity check should still pass
	if err := ValidateIntegrity(ctx, store.db); err != nil {
		t.Errorf("ValidateIntegrity() with data error = %v", err)
	}
}

func TestInitSchema_RunsIntegrityCheck(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first store to initialize schema
	store1, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	store1.Close()

	// Re-open - this should trigger InitSchema with integrity check
	store2, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() reopen error = %v", err)
	}
	defer store2.Close()

	// If we got here, integrity check passed during InitSchema
	// (The test would fail with an error if integrity check failed)
}

func TestSQLiteGraphStore_ContentHashCollision(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add first behavior with canonical content "Same content"
	node1 := Node{
		ID:   "behavior-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "First Behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Same content",
			},
		},
	}
	_, err = store.AddNode(ctx, node1)
	if err != nil {
		t.Fatalf("AddNode(node1) error = %v", err)
	}

	// Try to add a DIFFERENT behavior with the same canonical content
	// This should error because it would cause data loss
	node2 := Node{
		ID:   "behavior-2", // Different ID
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Second Behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Same content", // Same canonical content = same hash
			},
		},
	}
	_, err = store.AddNode(ctx, node2)
	if err == nil {
		t.Error("AddNode(node2) should error for duplicate content hash with different ID")
	}

	// Verify only one behavior exists
	behaviors, err := store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("QueryNodes() error = %v", err)
	}
	if len(behaviors) != 1 {
		t.Errorf("Expected 1 behavior, got %d", len(behaviors))
	}

	// Verify the first behavior was preserved (not silently replaced)
	got, err := store.GetNode(ctx, "behavior-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("Original behavior-1 was lost")
	}
	if got.Content["name"] != "First Behavior" {
		t.Errorf("behavior-1 name = %v, want First Behavior", got.Content["name"])
	}
}

func TestSQLiteGraphStore_ContentHashSameIDUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add behavior
	node := Node{
		ID:   "behavior-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test Behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content",
			},
		},
	}
	_, err = store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	// Re-adding the same behavior with same ID should succeed (it's an update)
	node.Content["name"] = "Updated Name"
	_, err = store.AddNode(ctx, node)
	if err != nil {
		t.Errorf("AddNode() with same ID should succeed: %v", err)
	}

	// Verify the update took effect
	got, err := store.GetNode(ctx, "behavior-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got.Content["name"] != "Updated Name" {
		t.Errorf("behavior name = %v, want Updated Name", got.Content["name"])
	}
}

func TestSQLiteGraphStore_EdgeWeights(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()
	activated := now.Add(-1 * time.Hour)

	tests := []struct {
		name          string
		edge          Edge
		wantWeight    float64
		wantCreatedAt bool
		wantActivated bool
	}{
		{
			name: "edge with explicit weight and timestamps",
			edge: Edge{
				Source:        "a",
				Target:        "b",
				Kind:          "requires",
				Weight:        0.75,
				CreatedAt:     now,
				LastActivated: &activated,
			},
			wantWeight:    0.75,
			wantCreatedAt: true,
			wantActivated: true,
		},
		{
			name: "edge with zero weight (caller did not set it)",
			edge: Edge{
				Source:    "a",
				Target:    "c",
				Kind:      "overrides",
				Weight:    0,
				CreatedAt: now,
			},
			wantWeight:    0,
			wantCreatedAt: true,
			wantActivated: false,
		},
		{
			name: "edge with zero CreatedAt (caller did not set it)",
			edge: Edge{
				Source: "a",
				Target: "d",
				Kind:   "similar-to",
				Weight: 0.5,
			},
			wantWeight:    0.5,
			wantCreatedAt: false,
			wantActivated: false,
		},
		{
			name: "edge with full weight",
			edge: Edge{
				Source:    "b",
				Target:    "c",
				Kind:      "requires",
				Weight:    1.0,
				CreatedAt: now,
			},
			wantWeight:    1.0,
			wantCreatedAt: true,
			wantActivated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.AddEdge(ctx, tt.edge)
			if err != nil {
				t.Fatalf("AddEdge() error = %v", err)
			}

			// Read back edges
			edges, err := store.GetEdges(ctx, tt.edge.Source, DirectionOutbound, tt.edge.Kind)
			if err != nil {
				t.Fatalf("GetEdges() error = %v", err)
			}

			// Find our specific edge
			var found *Edge
			for _, e := range edges {
				if e.Target == tt.edge.Target {
					found = &e
					break
				}
			}
			if found == nil {
				t.Fatal("edge not found after AddEdge")
			}

			// Check weight
			if found.Weight != tt.wantWeight {
				t.Errorf("Weight = %v, want %v", found.Weight, tt.wantWeight)
			}

			// Check created_at
			if tt.wantCreatedAt && found.CreatedAt.IsZero() {
				t.Error("CreatedAt is zero, want non-zero")
			}
			if !tt.wantCreatedAt && !found.CreatedAt.IsZero() {
				t.Errorf("CreatedAt = %v, want zero", found.CreatedAt)
			}

			// Check last_activated
			if tt.wantActivated && found.LastActivated == nil {
				t.Error("LastActivated is nil, want non-nil")
			}
			if !tt.wantActivated && found.LastActivated != nil {
				t.Errorf("LastActivated = %v, want nil", found.LastActivated)
			}
		})
	}
}

func TestSQLiteGraphStore_SchemaV3Migration(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	dbPath := filepath.Join(floopDir, "floop.db")

	// Create a v2 database manually
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	ctx := context.Background()

	// Create the v1 schema but with only the original edges columns
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS behaviors (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			behavior_type TEXT,
			content_canonical TEXT NOT NULL,
			content_expanded TEXT,
			content_summary TEXT,
			content_structured TEXT,
			content_tags TEXT,
			provenance_source_type TEXT,
			provenance_correction_id TEXT,
			provenance_created_at TEXT,
			requires TEXT,
			overrides TEXT,
			conflicts TEXT,
			confidence REAL DEFAULT 0.6,
			priority INTEGER DEFAULT 0,
			scope TEXT DEFAULT 'local',
			metadata_extra TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			content_hash TEXT UNIQUE
		)
	`)
	if err != nil {
		t.Fatalf("create behaviors table: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS edges (
			source TEXT NOT NULL,
			target TEXT NOT NULL,
			kind TEXT NOT NULL,
			metadata TEXT,
			PRIMARY KEY (source, target, kind)
		)
	`)
	if err != nil {
		t.Fatalf("create edges table: %v", err)
	}

	// Create other required tables
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS behavior_when (behavior_id TEXT NOT NULL, field TEXT NOT NULL, value TEXT NOT NULL, value_type TEXT DEFAULT 'string', PRIMARY KEY (behavior_id, field))`,
		`CREATE TABLE IF NOT EXISTS behavior_stats (behavior_id TEXT PRIMARY KEY, times_activated INTEGER DEFAULT 0, times_followed INTEGER DEFAULT 0, times_overridden INTEGER DEFAULT 0, times_confirmed INTEGER DEFAULT 0, last_activated TEXT, last_confirmed TEXT)`,
		`CREATE TABLE IF NOT EXISTS corrections (id TEXT PRIMARY KEY, timestamp TEXT NOT NULL, agent_action TEXT NOT NULL, corrected_action TEXT NOT NULL, human_response TEXT, context TEXT, conversation_id TEXT, turn_number INTEGER, corrector TEXT, processed INTEGER DEFAULT 0, processed_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS dirty_behaviors (behavior_id TEXT PRIMARY KEY, operation TEXT NOT NULL, dirty_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS export_state (id INTEGER PRIMARY KEY CHECK (id = 1), last_export_time TEXT, jsonl_hash TEXT)`,
		`CREATE TABLE IF NOT EXISTS config (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	// Create triggers
	for _, stmt := range []string{
		`CREATE TRIGGER IF NOT EXISTS behavior_insert_dirty AFTER INSERT ON behaviors BEGIN INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at) VALUES (NEW.id, 'insert', datetime('now')); END`,
		`CREATE TRIGGER IF NOT EXISTS behavior_update_dirty AFTER UPDATE ON behaviors BEGIN INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at) VALUES (NEW.id, 'update', datetime('now')); END`,
		`CREATE TRIGGER IF NOT EXISTS behavior_delete_dirty AFTER DELETE ON behaviors BEGIN INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at) VALUES (OLD.id, 'delete', datetime('now')); END`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
	}

	// Record as v2
	_, err = db.ExecContext(ctx, `INSERT INTO schema_version (version, applied_at) VALUES (2, datetime('now'))`)
	if err != nil {
		t.Fatalf("record version: %v", err)
	}

	// Insert some v2 edges (no weight, created_at, last_activated columns)
	_, err = db.ExecContext(ctx, `INSERT INTO edges (source, target, kind, metadata) VALUES ('node-a', 'node-b', 'requires', '{"note":"test"}')`)
	if err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO edges (source, target, kind) VALUES ('node-b', 'node-c', 'overrides')`)
	if err != nil {
		t.Fatalf("insert edge: %v", err)
	}

	db.Close()

	// Open with new code -- should trigger v2->v3 migration
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() after migration error = %v", err)
	}
	defer store.Close()

	// Verify schema version is now 3
	var version int
	err = store.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if version != 3 {
		t.Errorf("schema version = %d, want 3", version)
	}

	// Verify edges have been backfilled
	edges, err := store.GetEdges(ctx, "node-a", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("GetEdges() returned %d edges, want 1", len(edges))
	}

	// Weight should be backfilled to 1.0
	if edges[0].Weight != 1.0 {
		t.Errorf("migrated edge Weight = %v, want 1.0", edges[0].Weight)
	}

	// CreatedAt should be backfilled (non-zero)
	if edges[0].CreatedAt.IsZero() {
		t.Error("migrated edge CreatedAt is zero, want backfilled time")
	}

	// LastActivated should be nil (not backfilled)
	if edges[0].LastActivated != nil {
		t.Errorf("migrated edge LastActivated = %v, want nil", edges[0].LastActivated)
	}

	// Verify metadata was preserved
	if edges[0].Metadata == nil || edges[0].Metadata["note"] != "test" {
		t.Error("migrated edge metadata not preserved")
	}

	// Verify the second edge also got backfilled
	edges2, err := store.GetEdges(ctx, "node-b", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges(node-b) error = %v", err)
	}
	if len(edges2) != 1 {
		t.Fatalf("GetEdges(node-b) returned %d edges, want 1", len(edges2))
	}
	if edges2[0].Weight != 1.0 {
		t.Errorf("second migrated edge Weight = %v, want 1.0", edges2[0].Weight)
	}
}

func TestSQLiteGraphStore_EdgeJSONLRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}

	ctx := context.Background()
	now := time.Now().Truncate(time.Second) // Truncate for RFC3339 roundtrip
	activated := now.Add(-2 * time.Hour)

	// Add edges with weights and timestamps
	edges := []Edge{
		{
			Source:        "x",
			Target:        "y",
			Kind:          "requires",
			Weight:        0.8,
			CreatedAt:     now,
			LastActivated: &activated,
			Metadata:      map[string]interface{}{"note": "first"},
		},
		{
			Source:    "y",
			Target:    "z",
			Kind:      "similar-to",
			Weight:    0.5,
			CreatedAt: now,
		},
	}

	for _, e := range edges {
		if err := store.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge() error = %v", err)
		}
	}

	// Sync to JSONL
	if err := store.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Verify edges.jsonl exists
	edgesFile := filepath.Join(tmpDir, ".floop", "edges.jsonl")
	if _, err := os.Stat(edgesFile); os.IsNotExist(err) {
		t.Fatal("edges.jsonl was not created after Sync")
	}

	// Close and reopen
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store2, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() reopen error = %v", err)
	}
	defer store2.Close()

	// Read back first edge
	got, err := store2.GetEdges(ctx, "x", DirectionOutbound, "requires")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetEdges() returned %d edges, want 1", len(got))
	}

	// Verify weight preserved
	if got[0].Weight != 0.8 {
		t.Errorf("Weight = %v, want 0.8", got[0].Weight)
	}

	// Verify created_at preserved (within 1 second tolerance)
	if got[0].CreatedAt.Sub(now).Abs() > time.Second {
		t.Errorf("CreatedAt = %v, want ~%v", got[0].CreatedAt, now)
	}

	// Verify last_activated preserved
	if got[0].LastActivated == nil {
		t.Fatal("LastActivated is nil, want non-nil")
	}
	if got[0].LastActivated.Sub(activated).Abs() > time.Second {
		t.Errorf("LastActivated = %v, want ~%v", got[0].LastActivated, activated)
	}

	// Verify metadata preserved
	if got[0].Metadata == nil || got[0].Metadata["note"] != "first" {
		t.Error("metadata not preserved through round-trip")
	}

	// Read back second edge
	got2, err := store2.GetEdges(ctx, "y", DirectionOutbound, "similar-to")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("GetEdges() returned %d edges, want 1", len(got2))
	}
	if got2[0].Weight != 0.5 {
		t.Errorf("Weight = %v, want 0.5", got2[0].Weight)
	}
	if got2[0].LastActivated != nil {
		t.Errorf("LastActivated = %v, want nil", got2[0].LastActivated)
	}
}

func TestSQLiteGraphStore_ProvenanceCreatedAtRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now().Truncate(time.Second) // RFC3339 precision

	node := Node{
		ID:   "ts-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Timestamp Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test timestamp round-trip",
			},
			"provenance": map[string]interface{}{
				"source_type": "correction",
				"created_at":  now.Format(time.RFC3339),
			},
		},
	}

	_, err = s.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	got, err := s.GetNode(ctx, "ts-test")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetNode() returned nil")
	}

	provenance, ok := got.Content["provenance"].(map[string]interface{})
	if !ok {
		t.Fatal("provenance not found in content")
	}

	createdAt, ok := provenance["created_at"].(time.Time)
	if !ok {
		t.Fatalf("created_at is %T, want time.Time", provenance["created_at"])
	}

	if createdAt.Sub(now).Abs() > time.Second {
		t.Errorf("created_at = %v, want ~%v", createdAt, now)
	}
}

func TestSQLiteStore_RecordActivationHit(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Add a behavior
	_, err = s.AddNode(ctx, Node{
		ID:   "hit-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Hit Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test activation hit recording",
			},
		},
	})
	if err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	// Record first hit
	err = s.RecordActivationHit(ctx, "hit-test")
	if err != nil {
		t.Fatalf("RecordActivationHit() error = %v", err)
	}

	// Verify times_activated = 1 and last_activated is set
	var timesActivated int
	var lastActivated sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT times_activated, last_activated FROM behavior_stats WHERE behavior_id = ?`,
		"hit-test").Scan(&timesActivated, &lastActivated)
	if err != nil {
		t.Fatalf("query stats error = %v", err)
	}
	if timesActivated != 1 {
		t.Errorf("times_activated = %d, want 1", timesActivated)
	}
	if !lastActivated.Valid {
		t.Error("last_activated should be set after first hit")
	}

	// Record second hit
	err = s.RecordActivationHit(ctx, "hit-test")
	if err != nil {
		t.Fatalf("RecordActivationHit() second call error = %v", err)
	}

	err = s.db.QueryRowContext(ctx,
		`SELECT times_activated FROM behavior_stats WHERE behavior_id = ?`,
		"hit-test").Scan(&timesActivated)
	if err != nil {
		t.Fatalf("query stats error = %v", err)
	}
	if timesActivated != 2 {
		t.Errorf("times_activated = %d, want 2", timesActivated)
	}
}

func TestSQLiteStore_RecordActivationHit_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	err = s.RecordActivationHit(ctx, "nonexistent-id")
	if err == nil {
		t.Error("RecordActivationHit() expected error for nonexistent behavior")
	}
}

func TestSQLiteStore_TouchEdges(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Add nodes
	for _, id := range []string{"touch-a", "touch-b", "touch-c", "touch-d"} {
		s.AddNode(ctx, Node{
			ID:   id,
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": id,
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": id + " content",
				},
			},
		})
	}

	// Add edges: a->b, a->c, c->d (d is not connected to a)
	s.AddEdge(ctx, Edge{Source: "touch-a", Target: "touch-b", Kind: "requires", Weight: 0.8, CreatedAt: time.Now()})
	s.AddEdge(ctx, Edge{Source: "touch-a", Target: "touch-c", Kind: "similar-to", Weight: 0.6, CreatedAt: time.Now()})
	s.AddEdge(ctx, Edge{Source: "touch-c", Target: "touch-d", Kind: "requires", Weight: 0.5, CreatedAt: time.Now()})

	// Touch edges for seed ["touch-a"]
	err = s.TouchEdges(ctx, []string{"touch-a"})
	if err != nil {
		t.Fatalf("TouchEdges() error = %v", err)
	}

	// Edges a->b and a->c should have last_activated set
	edges, err := s.GetEdges(ctx, "touch-a", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	for _, e := range edges {
		if e.LastActivated == nil {
			t.Errorf("edge %s->%s: LastActivated should be set after TouchEdges", e.Source, e.Target)
		}
	}

	// Edge c->d should NOT have last_activated set (touch-c is not in seed list)
	edges, err = s.GetEdges(ctx, "touch-c", DirectionOutbound, "requires")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge from touch-c, got %d", len(edges))
	}
	if edges[0].LastActivated != nil {
		t.Error("edge touch-c->touch-d: LastActivated should be nil (not in seed list)")
	}
}

func TestSQLiteStore_TouchEdges_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Empty slice should not error
	err = s.TouchEdges(ctx, []string{})
	if err != nil {
		t.Errorf("TouchEdges(empty) error = %v, want nil", err)
	}
}

func TestSQLiteStore_RecordConfirmed(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Add a behavior
	_, err = s.AddNode(ctx, Node{
		ID:   "confirm-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Confirm Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test confirm recording",
			},
		},
	})
	if err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	// Record first confirmation
	err = s.RecordConfirmed(ctx, "confirm-test")
	if err != nil {
		t.Fatalf("RecordConfirmed() error = %v", err)
	}

	var timesConfirmed int
	var lastConfirmed sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT times_confirmed, last_confirmed FROM behavior_stats WHERE behavior_id = ?`,
		"confirm-test").Scan(&timesConfirmed, &lastConfirmed)
	if err != nil {
		t.Fatalf("query stats error = %v", err)
	}
	if timesConfirmed != 1 {
		t.Errorf("times_confirmed = %d, want 1", timesConfirmed)
	}
	if !lastConfirmed.Valid {
		t.Error("last_confirmed should be set after first confirmation")
	}

	// Record second confirmation
	err = s.RecordConfirmed(ctx, "confirm-test")
	if err != nil {
		t.Fatalf("RecordConfirmed() second call error = %v", err)
	}

	err = s.db.QueryRowContext(ctx,
		`SELECT times_confirmed FROM behavior_stats WHERE behavior_id = ?`,
		"confirm-test").Scan(&timesConfirmed)
	if err != nil {
		t.Fatalf("query stats error = %v", err)
	}
	if timesConfirmed != 2 {
		t.Errorf("times_confirmed = %d, want 2", timesConfirmed)
	}
}

func TestSQLiteStore_RecordConfirmed_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	err = s.RecordConfirmed(context.Background(), "nonexistent-id")
	if err == nil {
		t.Error("RecordConfirmed() expected error for nonexistent behavior")
	}
}

func TestSQLiteStore_RecordOverridden(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Add a behavior
	_, err = s.AddNode(ctx, Node{
		ID:   "override-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Override Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test override recording",
			},
		},
	})
	if err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	// Record first override
	err = s.RecordOverridden(ctx, "override-test")
	if err != nil {
		t.Fatalf("RecordOverridden() error = %v", err)
	}

	var timesOverridden int
	err = s.db.QueryRowContext(ctx,
		`SELECT times_overridden FROM behavior_stats WHERE behavior_id = ?`,
		"override-test").Scan(&timesOverridden)
	if err != nil {
		t.Fatalf("query stats error = %v", err)
	}
	if timesOverridden != 1 {
		t.Errorf("times_overridden = %d, want 1", timesOverridden)
	}

	// Record second override
	err = s.RecordOverridden(ctx, "override-test")
	if err != nil {
		t.Fatalf("RecordOverridden() second call error = %v", err)
	}

	err = s.db.QueryRowContext(ctx,
		`SELECT times_overridden FROM behavior_stats WHERE behavior_id = ?`,
		"override-test").Scan(&timesOverridden)
	if err != nil {
		t.Fatalf("query stats error = %v", err)
	}
	if timesOverridden != 2 {
		t.Errorf("times_overridden = %d, want 2", timesOverridden)
	}
}

func TestSQLiteStore_RecordOverridden_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	err = s.RecordOverridden(context.Background(), "nonexistent-id")
	if err == nil {
		t.Error("RecordOverridden() expected error for nonexistent behavior")
	}
}

func TestSQLiteGraphStore_BatchUpdateEdgeWeights(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create nodes and edges
	for _, id := range []string{"a", "b", "c"} {
		s.AddNode(ctx, Node{ID: id, Kind: "behavior", Content: map[string]interface{}{"name": id}})
	}
	s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: "co-activated", Weight: 0.5})
	s.AddEdge(ctx, Edge{Source: "b", Target: "c", Kind: "co-activated", Weight: 0.3})
	s.AddEdge(ctx, Edge{Source: "a", Target: "c", Kind: "requires", Weight: 1.0})

	// Batch update co-activated edges
	updates := []EdgeWeightUpdate{
		{Source: "a", Target: "b", Kind: "co-activated", NewWeight: 0.7},
		{Source: "b", Target: "c", Kind: "co-activated", NewWeight: 0.4},
	}
	if err := s.BatchUpdateEdgeWeights(ctx, updates); err != nil {
		t.Fatalf("BatchUpdateEdgeWeights() error = %v", err)
	}

	// Verify updates
	edges, _ := s.GetEdges(ctx, "a", DirectionOutbound, "co-activated")
	if len(edges) != 1 || edges[0].Weight != 0.7 {
		t.Errorf("edge a→b weight = %v, want 0.7", edges)
	}

	edges, _ = s.GetEdges(ctx, "b", DirectionOutbound, "co-activated")
	if len(edges) != 1 || edges[0].Weight != 0.4 {
		t.Errorf("edge b→c weight = %v, want 0.4", edges)
	}

	// Verify requires edge was NOT updated
	edges, _ = s.GetEdges(ctx, "a", DirectionOutbound, "requires")
	if len(edges) != 1 || edges[0].Weight != 1.0 {
		t.Errorf("requires edge should be unchanged, got %v", edges)
	}
}

func TestSQLiteGraphStore_BatchUpdateEdgeWeights_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	// Empty updates should be a no-op
	if err := s.BatchUpdateEdgeWeights(context.Background(), nil); err != nil {
		t.Errorf("BatchUpdateEdgeWeights(nil) error = %v", err)
	}
}

func TestSQLiteGraphStore_PruneWeakEdges(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create nodes and edges with various weights
	for _, id := range []string{"a", "b", "c", "d"} {
		s.AddNode(ctx, Node{ID: id, Kind: "behavior", Content: map[string]interface{}{"name": id}})
	}
	s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: "co-activated", Weight: 0.005}) // Below threshold
	s.AddEdge(ctx, Edge{Source: "a", Target: "c", Kind: "co-activated", Weight: 0.01})  // At threshold
	s.AddEdge(ctx, Edge{Source: "a", Target: "d", Kind: "co-activated", Weight: 0.5})   // Above threshold
	s.AddEdge(ctx, Edge{Source: "b", Target: "c", Kind: "requires", Weight: 0.001})     // Different kind

	// Prune co-activated edges at or below 0.01
	n, err := s.PruneWeakEdges(ctx, "co-activated", 0.01)
	if err != nil {
		t.Fatalf("PruneWeakEdges() error = %v", err)
	}
	if n != 2 {
		t.Errorf("PruneWeakEdges() pruned %d, want 2", n)
	}

	// Verify only the strong co-activated edge remains
	edges, _ := s.GetEdges(ctx, "a", DirectionOutbound, "co-activated")
	if len(edges) != 1 {
		t.Errorf("remaining co-activated edges = %d, want 1", len(edges))
	}
	if len(edges) == 1 && edges[0].Target != "d" {
		t.Errorf("remaining edge target = %s, want d", edges[0].Target)
	}

	// Verify requires edge was NOT pruned (different kind)
	edges, _ = s.GetEdges(ctx, "b", DirectionOutbound, "requires")
	if len(edges) != 1 {
		t.Errorf("requires edge should still exist, got %d", len(edges))
	}
}

func TestSQLiteGraphStore_PruneWeakEdges_NoneToRemove(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	n, err := s.PruneWeakEdges(context.Background(), "co-activated", 0.01)
	if err != nil {
		t.Fatalf("PruneWeakEdges() error = %v", err)
	}
	if n != 0 {
		t.Errorf("PruneWeakEdges() pruned %d, want 0", n)
	}
}
