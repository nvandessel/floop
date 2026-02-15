package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestMultiGraphStore_AddNodeToScope_ClampedByWriteScope(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	// Create with ScopeLocal — requests for ScopeGlobal should be clamped to local
	store, err := NewMultiGraphStore(localRoot, ScopeLocal)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	node := Node{
		ID:      "clamped-node",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "should-be-clamped-to-local"},
	}

	ctx := context.Background()
	// Request global, but store is configured for local
	id, err := store.AddNodeToScope(ctx, node, ScopeGlobal)
	if err != nil {
		t.Fatalf("AddNodeToScope(ScopeGlobal on ScopeLocal store) failed: %v", err)
	}
	if id != node.ID {
		t.Errorf("returned id = %v, want %v", id, node.ID)
	}

	// Verify node is in local store (clamped from global to local)
	localNode, err := store.localStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from local store: %v", err)
	}
	if localNode == nil {
		t.Error("node not found in local store — clamping failed")
	}

	// Verify node is NOT in global store
	globalNode, err := store.globalStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from global store: %v", err)
	}
	if globalNode != nil {
		t.Error("node should not be in global store when writeScope is ScopeLocal")
	}

	// Verify metadata shows local scope (not global)
	if localNode != nil && localNode.Metadata["scope"] != "local" {
		t.Errorf("node scope = %v, want local (clamped)", localNode.Metadata["scope"])
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
		ID:   "shared-id",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "global-node",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "global content",
			},
			"source": "global",
		},
	}
	store.globalStore.AddNode(ctx, globalNode)

	// Add same ID to local store with different content
	localNode := Node{
		ID:   "shared-id",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "local-node",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "local content",
			},
			"source": "local",
		},
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
	// Check name to verify we got the local version
	if result.Content["name"] != "local-node" {
		t.Errorf("GetNode() returned name = %v, want local-node", result.Content["name"])
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

	// Helper to create a behavior node with proper structure
	makeNode := func(id, namePrefix string) Node {
		return Node{
			ID:   id,
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": namePrefix + "-" + id,
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "content for " + id,
				},
			},
		}
	}

	// Add nodes to global
	store.globalStore.AddNode(ctx, makeNode("node-1", "global"))
	store.globalStore.AddNode(ctx, makeNode("node-2", "global"))

	// Override node-1 in local
	store.localStore.AddNode(ctx, makeNode("node-1", "local"))

	// Add node-3 only in local
	store.localStore.AddNode(ctx, makeNode("node-3", "local"))

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
		if node.ID == "node-1" && node.Content["name"] != "local-node-1" {
			t.Errorf("node-1 should be from local store (local wins), got name=%v", node.Content["name"])
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

	// Add node to global store with proper structure
	store.globalStore.AddNode(ctx, Node{
		ID:   "global-node",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "global-node",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "original content",
			},
		},
	})

	// Update the node
	err = store.UpdateNode(ctx, Node{
		ID:   "global-node",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "global-node-updated",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "updated content",
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateNode() failed: %v", err)
	}

	// Verify update happened in global store
	updated, err := store.globalStore.GetNode(ctx, "global-node")
	if err != nil || updated == nil {
		t.Fatal("failed to get updated node from global store")
	}
	// Check name was updated
	if updated.Content["name"] != "global-node-updated" {
		t.Errorf("updated name = %v, want global-node-updated", updated.Content["name"])
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

	// Add nodes with proper structure
	store.localStore.AddNode(ctx, Node{
		ID:   "node-a",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "node-a",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "content a",
			},
		},
	})
	store.localStore.AddNode(ctx, Node{
		ID:   "node-b",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "node-b",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "content b",
			},
		},
	})

	// Add edge
	edge := Edge{Source: "node-a", Target: "node-b", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()}
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

func TestMultiGraphStore_GlobalStore(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	ms, err := NewMultiGraphStore(localRoot, ScopeLocal)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer ms.Close()

	ctx := context.Background()
	gs := ms.GlobalStore()

	if gs == nil {
		t.Fatal("GlobalStore() returned nil")
	}

	// Write through GlobalStore accessor
	node := Node{
		ID:      "global-only-node",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "test"},
	}
	if _, err := gs.AddNode(ctx, node); err != nil {
		t.Fatalf("AddNode via GlobalStore() failed: %v", err)
	}

	// Verify it's in global, not local
	globalNode, err := ms.globalStore.GetNode(ctx, "global-only-node")
	if err != nil || globalNode == nil {
		t.Error("node not found in global store")
	}

	localNode, err := ms.localStore.GetNode(ctx, "global-only-node")
	if err != nil {
		t.Fatalf("error checking local store: %v", err)
	}
	if localNode != nil {
		t.Error("node should not be in local store")
	}
}

func TestMultiGraphStore_ValidateBehaviorGraph(t *testing.T) {
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

	// Add a behavior with dangling reference to local store
	behaviorWithDangling := Node{
		ID:   "behavior-with-dangling",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test Behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content",
			},
			"requires": []string{"non-existent"},
		},
		Metadata: map[string]interface{}{
			"scope": "local",
		},
	}

	if _, err := store.AddNode(ctx, behaviorWithDangling); err != nil {
		t.Fatalf("failed to add behavior: %v", err)
	}

	// Validate
	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("ValidateBehaviorGraph() failed: %v", err)
	}

	// Should find the dangling reference
	if len(errors) == 0 {
		t.Error("expected validation errors, got none")
	}

	// Check that at least one error is from local store
	foundLocalError := false
	for _, e := range errors {
		if len(e.BehaviorID) > 6 && e.BehaviorID[:6] == "local:" {
			foundLocalError = true
			break
		}
	}

	if !foundLocalError {
		t.Errorf("expected error with 'local:' prefix, got: %v", errors)
	}
}

func TestMultiGraphStore_ValidateBehaviorGraph_Valid(t *testing.T) {
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

	// Add valid behaviors with proper relationships
	behaviorA := Node{
		ID:   "behavior-a",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Behavior A",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content A",
			},
			"requires": []string{"behavior-b"},
		},
	}
	behaviorB := Node{
		ID:   "behavior-b",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Behavior B",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test content B",
			},
		},
	}

	if _, err := store.AddNode(ctx, behaviorA); err != nil {
		t.Fatalf("failed to add behavior A: %v", err)
	}
	if _, err := store.AddNode(ctx, behaviorB); err != nil {
		t.Fatalf("failed to add behavior B: %v", err)
	}

	// Validate
	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("ValidateBehaviorGraph() failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("expected no validation errors, got %d: %v", len(errors), errors)
	}
}

func TestMultiGraphStore_AddNodeToScope_Local(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	// Create with ScopeBoth as the default — AddNodeToScope should override
	store, err := NewMultiGraphStore(localRoot, ScopeBoth)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	node := Node{
		ID:      "scoped-local-node",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "local-only"},
	}

	ctx := context.Background()
	id, err := store.AddNodeToScope(ctx, node, ScopeLocal)
	if err != nil {
		t.Fatalf("AddNodeToScope(ScopeLocal) failed: %v", err)
	}
	if id != node.ID {
		t.Errorf("returned id = %v, want %v", id, node.ID)
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
		t.Error("node should not be in global store when scope is local")
	}

	// Verify metadata has scope set
	if localNode != nil && localNode.Metadata["scope"] != "local" {
		t.Errorf("node scope = %v, want local", localNode.Metadata["scope"])
	}
}

func TestMultiGraphStore_AddNodeToScope_Global(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)

	// Create with ScopeBoth as the default — AddNodeToScope should override
	store, err := NewMultiGraphStore(localRoot, ScopeBoth)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	node := Node{
		ID:      "scoped-global-node",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "global-only"},
	}

	ctx := context.Background()
	id, err := store.AddNodeToScope(ctx, node, ScopeGlobal)
	if err != nil {
		t.Fatalf("AddNodeToScope(ScopeGlobal) failed: %v", err)
	}
	if id != node.ID {
		t.Errorf("returned id = %v, want %v", id, node.ID)
	}

	// Verify node is NOT in local store
	localNode, err := store.localStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from local store: %v", err)
	}
	if localNode != nil {
		t.Error("node should not be in local store when scope is global")
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
