package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func setupTestStores(t *testing.T) (localRoot, globalRoot string, cleanup func()) {
	t.Helper()

	// Create temp directories
	localRoot, err := os.MkdirTemp("", "floop-test-local-*")
	if err != nil {
		t.Fatalf("failed to create local temp dir: %v", err)
	}

	globalRoot, err = os.MkdirTemp("", "floop-test-global-*")
	if err != nil {
		os.RemoveAll(localRoot)
		t.Fatalf("failed to create global temp dir: %v", err)
	}

	cleanup = func() {
		os.RemoveAll(localRoot)
		os.RemoveAll(globalRoot)
	}

	return localRoot, globalRoot, cleanup
}

func TestNewMultiGraphStore(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	// Override global path for testing
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	tests := []struct {
		name       string
		writeScope StoreScope
		wantErr    bool
	}{
		{"create with ScopeLocal", ScopeLocal, false},
		{"create with ScopeGlobal", ScopeGlobal, false},
		{"create with ScopeBoth", ScopeBoth, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewMultiGraphStore(localRoot, tt.writeScope)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMultiGraphStore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && store == nil {
				t.Error("NewMultiGraphStore() returned nil store")
			}
			if store != nil {
				store.Close()
			}
		})
	}
}

func TestMultiGraphStore_AddNode_ScopeLocal(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeLocal)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	node := Node{
		ID:      "test-node-1",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "test"},
	}

	ctx := context.Background()
	id, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("AddNode() failed: %v", err)
	}
	if id != node.ID {
		t.Errorf("AddNode() returned id = %v, want %v", id, node.ID)
	}

	// Verify node is in local store
	localNode, err := store.localStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from local store: %v", err)
	}
	if localNode == nil {
		t.Error("node not found in local store")
	}

	// Verify node is NOT in global store
	globalNode, err := store.globalStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from global store: %v", err)
	}
	if globalNode != nil {
		t.Error("node should not be in global store")
	}

	// Verify metadata has scope set
	if localNode != nil && localNode.Metadata["scope"] != "local" {
		t.Errorf("node scope = %v, want local", localNode.Metadata["scope"])
	}
}

func TestMultiGraphStore_AddNode_ScopeGlobal(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeGlobal)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	node := Node{
		ID:      "test-node-2",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "test"},
	}

	ctx := context.Background()
	id, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("AddNode() failed: %v", err)
	}

	// Verify node is NOT in local store
	localNode, err := store.localStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from local store: %v", err)
	}
	if localNode != nil {
		t.Error("node should not be in local store")
	}

	// Verify node is in global store
	globalNode, err := store.globalStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from global store: %v", err)
	}
	if globalNode == nil {
		t.Error("node not found in global store")
	}

	// Verify metadata has scope set
	if globalNode != nil && globalNode.Metadata["scope"] != "global" {
		t.Errorf("node scope = %v, want global", globalNode.Metadata["scope"])
	}
}

func TestMultiGraphStore_AddNode_ScopeBoth(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeBoth)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	node := Node{
		ID:      "test-node-3",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "test"},
	}

	ctx := context.Background()
	id, err := store.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("AddNode() failed: %v", err)
	}

	// Verify node is in both stores
	localNode, err := store.localStore.GetNode(ctx, id)
	if err != nil || localNode == nil {
		t.Error("node not found in local store")
	}

	globalNode, err := store.globalStore.GetNode(ctx, id)
	if err != nil || globalNode == nil {
		t.Error("node not found in global store")
	}

	// Verify metadata has scope set
	if localNode != nil && localNode.Metadata["scope"] != "both" {
		t.Errorf("local node scope = %v, want both", localNode.Metadata["scope"])
	}
	if globalNode != nil && globalNode.Metadata["scope"] != "both" {
		t.Errorf("global node scope = %v, want both", globalNode.Metadata["scope"])
	}
}

func TestMultiGraphStore_GetNode_PreferLocal(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeLocal)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add node to global store
	globalNode := Node{
		ID:      "shared-id",
		Kind:    "behavior",
		Content: map[string]interface{}{"source": "global"},
	}
	store.globalStore.AddNode(ctx, globalNode)

	// Add same ID to local store with different content
	localNode := Node{
		ID:      "shared-id",
		Kind:    "behavior",
		Content: map[string]interface{}{"source": "local"},
	}
	store.localStore.AddNode(ctx, localNode)

	// GetNode should return local version
	result, err := store.GetNode(ctx, "shared-id")
	if err != nil {
		t.Fatalf("GetNode() failed: %v", err)
	}
	if result == nil {
		t.Fatal("GetNode() returned nil")
	}
	if result.Content["source"] != "local" {
		t.Errorf("GetNode() returned source = %v, want local", result.Content["source"])
	}
}

func TestMultiGraphStore_QueryNodes_LocalWins(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeLocal)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add nodes to global
	store.globalStore.AddNode(ctx, Node{
		ID:      "node-1",
		Kind:    "behavior",
		Content: map[string]interface{}{"source": "global"},
	})
	store.globalStore.AddNode(ctx, Node{
		ID:      "node-2",
		Kind:    "behavior",
		Content: map[string]interface{}{"source": "global"},
	})

	// Override node-1 in local
	store.localStore.AddNode(ctx, Node{
		ID:      "node-1",
		Kind:    "behavior",
		Content: map[string]interface{}{"source": "local"},
	})

	// Add node-3 only in local
	store.localStore.AddNode(ctx, Node{
		ID:      "node-3",
		Kind:    "behavior",
		Content: map[string]interface{}{"source": "local"},
	})

	// Query for all behaviors
	results, err := store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("QueryNodes() failed: %v", err)
	}

	// Should have 3 nodes: local node-1, global node-2, local node-3
	if len(results) != 3 {
		t.Errorf("QueryNodes() returned %d nodes, want 3", len(results))
	}

	// Verify node-1 is from local (local wins)
	for _, node := range results {
		if node.ID == "node-1" && node.Content["source"] != "local" {
			t.Error("node-1 should be from local store (local wins)")
		}
	}
}

func TestMultiGraphStore_UpdateNode_FindsCorrectStore(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeLocal)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add node to global store
	store.globalStore.AddNode(ctx, Node{
		ID:      "global-node",
		Kind:    "behavior",
		Content: map[string]interface{}{"value": 1},
	})

	// Update the node
	err = store.UpdateNode(ctx, Node{
		ID:      "global-node",
		Kind:    "behavior",
		Content: map[string]interface{}{"value": 2},
	})
	if err != nil {
		t.Fatalf("UpdateNode() failed: %v", err)
	}

	// Verify update happened in global store
	updated, err := store.globalStore.GetNode(ctx, "global-node")
	if err != nil || updated == nil {
		t.Fatal("failed to get updated node from global store")
	}
	// Check value is 2 (could be int or float64 depending on JSON marshaling)
	val, ok := updated.Content["value"]
	if !ok {
		t.Fatal("value field not found in updated node")
	}
	// Convert to float64 for comparison
	var numVal float64
	switch v := val.(type) {
	case int:
		numVal = float64(v)
	case float64:
		numVal = v
	default:
		t.Fatalf("value has unexpected type: %T", v)
	}
	if numVal != 2.0 {
		t.Errorf("updated value = %v, want 2", numVal)
	}
}

func TestMultiGraphStore_DeleteNode(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeBoth)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add node to both stores
	node := Node{ID: "delete-me", Kind: "behavior"}
	store.AddNode(ctx, node)

	// Delete the node
	err = store.DeleteNode(ctx, "delete-me")
	if err != nil {
		t.Fatalf("DeleteNode() failed: %v", err)
	}

	// Verify deleted from both stores
	localNode, _ := store.localStore.GetNode(ctx, "delete-me")
	globalNode, _ := store.globalStore.GetNode(ctx, "delete-me")

	if localNode != nil {
		t.Error("node still exists in local store")
	}
	if globalNode != nil {
		t.Error("node still exists in global store")
	}
}

func TestMultiGraphStore_Sync_BothStores(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeBoth)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add node
	node := Node{ID: "sync-test", Kind: "behavior"}
	store.AddNode(ctx, node)

	// Sync
	err = store.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() failed: %v", err)
	}

	// Verify files exist
	localNodesFile := filepath.Join(localRoot, ".floop", "nodes.jsonl")
	globalNodesFile := filepath.Join(globalRoot, ".floop", "nodes.jsonl")

	if _, err := os.Stat(localNodesFile); os.IsNotExist(err) {
		t.Error("local nodes file not created")
	}
	if _, err := os.Stat(globalNodesFile); os.IsNotExist(err) {
		t.Error("global nodes file not created")
	}
}

func TestMultiGraphStore_AddEdge(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	store, err := NewMultiGraphStore(localRoot, ScopeLocal)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add nodes
	store.localStore.AddNode(ctx, Node{ID: "node-a", Kind: "behavior"})
	store.localStore.AddNode(ctx, Node{ID: "node-b", Kind: "behavior"})

	// Add edge
	edge := Edge{Source: "node-a", Target: "node-b", Kind: "requires"}
	err = store.AddEdge(ctx, edge)
	if err != nil {
		t.Fatalf("AddEdge() failed: %v", err)
	}

	// Verify edge exists
	edges, err := store.GetEdges(ctx, "node-a", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges() failed: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("GetEdges() returned %d edges, want 1", len(edges))
	}
}

func TestMergeNodes(t *testing.T) {
	tests := []struct {
		name   string
		local  []Node
		global []Node
		want   int // expected number of nodes
	}{
		{
			name:   "no conflicts",
			local:  []Node{{ID: "a"}, {ID: "b"}},
			global: []Node{{ID: "c"}, {ID: "d"}},
			want:   4,
		},
		{
			name:   "local wins on conflict",
			local:  []Node{{ID: "a", Content: map[string]interface{}{"source": "local"}}},
			global: []Node{{ID: "a", Content: map[string]interface{}{"source": "global"}}},
			want:   1,
		},
		{
			name:   "mixed conflicts and unique",
			local:  []Node{{ID: "a"}, {ID: "b"}},
			global: []Node{{ID: "a"}, {ID: "c"}},
			want:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeNodes(tt.local, tt.global)
			if len(result) != tt.want {
				t.Errorf("mergeNodes() returned %d nodes, want %d", len(result), tt.want)
			}

			// For conflict test, verify local wins
			if tt.name == "local wins on conflict" {
				if result[0].Content["source"] != "local" {
					t.Error("local node should win on conflict")
				}
			}
		})
	}
}

func TestMergeEdges(t *testing.T) {
	tests := []struct {
		name   string
		local  []Edge
		global []Edge
		want   int
	}{
		{
			name:   "no duplicates",
			local:  []Edge{{Source: "a", Target: "b", Kind: "requires"}},
			global: []Edge{{Source: "c", Target: "d", Kind: "requires"}},
			want:   2,
		},
		{
			name:   "deduplicates same edge",
			local:  []Edge{{Source: "a", Target: "b", Kind: "requires"}},
			global: []Edge{{Source: "a", Target: "b", Kind: "requires"}},
			want:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeEdges(tt.local, tt.global)
			if len(result) != tt.want {
				t.Errorf("mergeEdges() returned %d edges, want %d", len(result), tt.want)
			}
		})
	}
}
