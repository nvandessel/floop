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

	mustAddNode(t, store, ctx, node)

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
	mustAddNode(t, store, ctx, node)

	// Add an edge involving this node
	mustAddEdge(t, store, ctx, Edge{Source: "test-1", Target: "other", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

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
	mustAddNode(t, store, ctx, Node{ID: "b-1", Kind: "behavior"})
	mustAddNode(t, store, ctx, Node{ID: "c-1", Kind: "correction"})
	mustAddNode(t, store, ctx, Node{ID: "b-2", Kind: "behavior"})

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
	mustAddNode(t, store, ctx, Node{ID: "a", Kind: "behavior"})
	mustAddNode(t, store, ctx, Node{ID: "b", Kind: "behavior"})
	mustAddNode(t, store, ctx, Node{ID: "c", Kind: "behavior"})

	mustAddEdge(t, store, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, store, ctx, Edge{Source: "a", Target: "c", Kind: EdgeKindOverrides, Weight: 1.0, CreatedAt: time.Now()})

	// Test outbound
	edges := mustGetEdges(t, store, ctx, "a", DirectionOutbound, "")
	if len(edges) != 2 {
		t.Errorf("GetEdges(outbound) = %d, want 2", len(edges))
	}

	// Test inbound
	edges = mustGetEdges(t, store, ctx, "b", DirectionInbound, "")
	if len(edges) != 1 {
		t.Errorf("GetEdges(inbound) = %d, want 1", len(edges))
	}

	// Test with kind filter
	edges = mustGetEdges(t, store, ctx, "a", DirectionOutbound, EdgeKindRequires)
	if len(edges) != 1 {
		t.Errorf("GetEdges(requires) = %d, want 1", len(edges))
	}

	// Test remove
	if err := store.RemoveEdge(ctx, "a", "b", EdgeKindRequires); err != nil {
		t.Fatalf("RemoveEdge() error = %v", err)
	}
	edges = mustGetEdges(t, store, ctx, "a", DirectionOutbound, "")
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
	mustAddNode(t, store, ctx, Node{ID: "a", Kind: "behavior"})
	mustAddNode(t, store, ctx, Node{ID: "b", Kind: "behavior"})
	mustAddNode(t, store, ctx, Node{ID: "c", Kind: "behavior"})
	mustAddEdge(t, store, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, store, ctx, Edge{Source: "b", Target: "c", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	// Traverse from a with depth 2
	results, err := store.Traverse(ctx, "a", []EdgeKind{EdgeKindRequires}, DirectionOutbound, 2)
	if err != nil {
		t.Fatalf("Traverse(depth=2) error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Traverse() = %d nodes, want 3", len(results))
	}

	// Traverse from a with depth 1
	results, err = store.Traverse(ctx, "a", []EdgeKind{EdgeKindRequires}, DirectionOutbound, 1)
	if err != nil {
		t.Fatalf("Traverse(depth=1) error = %v", err)
	}
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
	store1.AddEdge(ctx, Edge{Source: "persist-1", Target: "other", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

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
	mustAddNode(t, store, ctx, Node{ID: "test", Kind: "behavior"})
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

func TestFileGraphStore_Traverse_InboundAndBoth(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Build graph: a -> b -> c
	mustAddNode(t, store, ctx, Node{ID: "a", Kind: NodeKindBehavior})
	mustAddNode(t, store, ctx, Node{ID: "b", Kind: NodeKindBehavior})
	mustAddNode(t, store, ctx, Node{ID: "c", Kind: NodeKindBehavior})
	mustAddEdge(t, store, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, store, ctx, Edge{Source: "b", Target: "c", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	// Traverse inbound from c
	results, err := store.Traverse(ctx, "c", []EdgeKind{EdgeKindRequires}, DirectionInbound, 5)
	if err != nil {
		t.Fatalf("Traverse(inbound) error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Traverse(inbound) got %d nodes, want 3", len(results))
	}

	// Traverse both from b (should reach a and c)
	results, err = store.Traverse(ctx, "b", []EdgeKind{EdgeKindRequires}, DirectionBoth, 5)
	if err != nil {
		t.Fatalf("Traverse(both) error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Traverse(both) got %d nodes, want 3", len(results))
	}
}

func TestFileGraphStore_AddEdge_Validation(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Invalid weight
	err = store.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 0, CreatedAt: time.Now()})
	if err == nil {
		t.Error("AddEdge() should reject zero weight")
	}

	err = store.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.5, CreatedAt: time.Now()})
	if err == nil {
		t.Error("AddEdge() should reject weight > 1.0")
	}

	// Missing CreatedAt
	err = store.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 0.5})
	if err == nil {
		t.Error("AddEdge() should reject zero CreatedAt")
	}
}

func TestFileGraphStore_LoadFromExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store, add data, close (writes to files)
	s1, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	ctx := context.Background()
	s1.AddNode(ctx, Node{ID: "persist-a", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "a"}})
	s1.AddNode(ctx, Node{ID: "persist-b", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "b"}})
	s1.AddEdge(ctx, Edge{Source: "persist-a", Target: "persist-b", Kind: EdgeKindRequires, Weight: 0.8, CreatedAt: time.Now()})
	s1.Close()

	// Reopen — should load from existing files
	s2, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() reopen error = %v", err)
	}
	defer s2.Close()

	got, _ := s2.GetNode(ctx, "persist-a")
	if got == nil {
		t.Error("node persist-a should exist after reload")
	}

	edges, _ := s2.GetEdges(ctx, "persist-a", DirectionOutbound, "")
	if len(edges) != 1 {
		t.Errorf("edges after reload = %d, want 1", len(edges))
	}
}

func TestFileGraphStore_WriteEdges(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}

	ctx := context.Background()
	mustAddNode(t, store, ctx, Node{ID: "a", Kind: NodeKindBehavior})
	mustAddNode(t, store, ctx, Node{ID: "b", Kind: NodeKindBehavior})
	mustAddEdge(t, store, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	// Sync to persist
	if err := store.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Verify edges file exists
	edgesFile := filepath.Join(tmpDir, ".floop", "edges.jsonl")
	if _, err := os.Stat(edgesFile); os.IsNotExist(err) {
		t.Error("edges.jsonl should exist after sync")
	}

	store.Close()
}

func TestTruncateForError(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"short string", "hello", "hello"},
		{"exactly 100", string(make([]byte, 100)), string(make([]byte, 100))},
		{"over 100", string(make([]byte, 150)), string(make([]byte, 100)) + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fill with 'a' for readability
			input := tt.input
			if tt.name == "exactly 100" {
				input = ""
				for i := 0; i < 100; i++ {
					input += "a"
				}
			}
			if tt.name == "over 100" {
				input = ""
				for i := 0; i < 150; i++ {
					input += "a"
				}
			}

			got := truncateForError(input)
			if tt.name == "short string" && got != "hello" {
				t.Errorf("truncateForError(%q) = %q, want %q", input, got, "hello")
			}
			if tt.name == "over 100" && len(got) != 103 { // 100 + "..."
				t.Errorf("truncateForError() len = %d, want 103", len(got))
			}
		})
	}
}

func TestFileGraphStore_GetEdges_BothDirection(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	mustAddNode(t, store, ctx, Node{ID: "a", Kind: NodeKindBehavior})
	mustAddNode(t, store, ctx, Node{ID: "b", Kind: NodeKindBehavior})
	mustAddNode(t, store, ctx, Node{ID: "c", Kind: NodeKindBehavior})
	mustAddEdge(t, store, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, store, ctx, Edge{Source: "c", Target: "a", Kind: EdgeKindOverrides, Weight: 1.0, CreatedAt: time.Now()})

	edges, err := store.GetEdges(ctx, "a", DirectionBoth, "")
	if err != nil {
		t.Fatalf("GetEdges(both) error = %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("GetEdges(both) got %d, want 2", len(edges))
	}
}

func TestFileGraphStore_LoadMalformedJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Write malformed nodes JSONL
	nodesContent := `{"id":"good","kind":"behavior","content":{"name":"ok"}}
not-valid-json
{"id":"good2","kind":"behavior","content":{"name":"ok2"}}
`
	if err := os.WriteFile(filepath.Join(floopDir, "nodes.jsonl"), []byte(nodesContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Write malformed edges JSONL
	edgesContent := `{"source":"a","target":"b","kind":"requires","weight":0.5}
{bad-edge-json
{"source":"c","target":"d","kind":"requires","weight":0.5}
`
	if err := os.WriteFile(filepath.Join(floopDir, "edges.jsonl"), []byte(edgesContent), 0600); err != nil {
		t.Fatal(err)
	}

	store, err := NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewFileGraphStore() error = %v", err)
	}
	defer store.Close()

	// Should have loaded the good nodes and edges
	ctx := context.Background()
	node, err := store.GetNode(ctx, "good")
	if err != nil || node == nil {
		t.Error("expected good node to be loaded")
	}
	node2, err := store.GetNode(ctx, "good2")
	if err != nil || node2 == nil {
		t.Error("expected good2 node to be loaded")
	}

	// Should have recorded load errors for bad lines
	if len(store.LoadErrors) != 2 {
		t.Errorf("expected 2 load errors, got %d", len(store.LoadErrors))
	}
}
