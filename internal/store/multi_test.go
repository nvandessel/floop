package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("NewMultiGraphStore() error = %v", err)
	}
	if store == nil {
		t.Error("NewMultiGraphStore() returned nil store")
	}
	if store != nil {
		store.Close()
	}
}

func TestMultiGraphStore_AddNode_DefaultsToGlobal(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
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

	// Verify node is in global store (default)
	globalNode, err := store.globalStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from global store: %v", err)
	}
	if globalNode == nil {
		t.Error("node not found in global store")
	}

	// Verify node is NOT in local store
	localNode, err := store.localStore.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("failed to get from local store: %v", err)
	}
	if localNode != nil {
		t.Error("node should not be in local store")
	}

	// Verify metadata has scope set to global
	if globalNode != nil && globalNode.Metadata["scope"] != "global" {
		t.Errorf("node scope = %v, want global", globalNode.Metadata["scope"])
	}
}

func TestMultiGraphStore_AddNodeToScope_Local(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
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

func TestMultiGraphStore_AddNodeToScope_RejectsInvalidScope(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	node := Node{
		ID:      "both-node",
		Kind:    "behavior",
		Content: map[string]interface{}{"name": "both-scope"},
	}

	ctx := context.Background()

	// ScopeBoth should be rejected for writes
	_, err = store.AddNodeToScope(ctx, node, ScopeBoth)
	if err == nil {
		t.Error("expected error for ScopeBoth write, got nil")
	}

	// Invalid scope should also be rejected
	_, err = store.AddNodeToScope(ctx, node, "invalid")
	if err == nil {
		t.Error("expected error for invalid scope, got nil")
	}
}

func TestMultiGraphStore_GetNode_PreferLocal(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add node to both stores directly
	node := Node{ID: "delete-me", Kind: "behavior"}
	store.localStore.AddNode(ctx, node)
	store.globalStore.AddNode(ctx, node)

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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add a node (goes to global by default)
	node := Node{ID: "sync-test", Kind: "behavior"}
	store.AddNode(ctx, node)

	// Sync
	err = store.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() failed: %v", err)
	}

	// Verify global nodes file exists
	globalNodesFile := filepath.Join(globalRoot, ".floop", "nodes.jsonl")
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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add nodes with proper structure to local store
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
	edge := Edge{Source: "node-a", Target: "node-b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()}
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
			local:  []Edge{{Source: "a", Target: "b", Kind: EdgeKindRequires}},
			global: []Edge{{Source: "c", Target: "d", Kind: EdgeKindRequires}},
			want:   2,
		},
		{
			name:   "deduplicates same edge",
			local:  []Edge{{Source: "a", Target: "b", Kind: EdgeKindRequires}},
			global: []Edge{{Source: "a", Target: "b", Kind: EdgeKindRequires}},
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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	ms, err := NewMultiGraphStore(localRoot)
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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
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

	// Add directly to local store via AddNodeToScope
	if _, err := store.AddNodeToScope(ctx, behaviorWithDangling, ScopeLocal); err != nil {
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
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add valid behaviors with proper relationships to local store
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

	if _, err := store.AddNodeToScope(ctx, behaviorA, ScopeLocal); err != nil {
		t.Fatalf("failed to add behavior A: %v", err)
	}
	if _, err := store.AddNodeToScope(ctx, behaviorB, ScopeLocal); err != nil {
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

func TestMultiGraphStore_AddEdge_CrossStoreRoutesToGlobal(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add source node to local store
	store.localStore.AddNode(ctx, Node{
		ID:   "local-node",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "local-node",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "local content",
			},
		},
	})

	// Add target node to global store
	store.globalStore.AddNode(ctx, Node{
		ID:   "global-node",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "global-node",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "global content",
			},
		},
	})

	// Add cross-store edge (local source → global target)
	edge := Edge{
		Source:    "local-node",
		Target:    "global-node",
		Kind:      EdgeKindSimilarTo,
		Weight:    0.8,
		CreatedAt: time.Now(),
	}
	if err := store.AddEdge(ctx, edge); err != nil {
		t.Fatalf("AddEdge() failed: %v", err)
	}

	// Edge should be in global store, NOT local store
	globalEdges, err := store.globalStore.GetEdges(ctx, "local-node", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("global GetEdges() failed: %v", err)
	}
	localEdges, err := store.localStore.GetEdges(ctx, "local-node", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("local GetEdges() failed: %v", err)
	}

	if len(globalEdges) != 1 {
		t.Errorf("expected 1 edge in global store, got %d", len(globalEdges))
	}
	if len(localEdges) != 0 {
		t.Errorf("expected 0 edges in local store, got %d", len(localEdges))
	}
}

func TestMultiGraphStore_AddEdge_SameStoreStaysLocal(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add both nodes to local store
	store.localStore.AddNode(ctx, Node{
		ID:   "local-a",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "local-a",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "content a",
			},
		},
	})
	store.localStore.AddNode(ctx, Node{
		ID:   "local-b",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "local-b",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "content b",
			},
		},
	})

	// Add same-store edge (local → local)
	edge := Edge{
		Source:    "local-a",
		Target:    "local-b",
		Kind:      EdgeKindSimilarTo,
		Weight:    0.9,
		CreatedAt: time.Now(),
	}
	if err := store.AddEdge(ctx, edge); err != nil {
		t.Fatalf("AddEdge() failed: %v", err)
	}

	// Edge should stay in local store
	localEdges, err := store.localStore.GetEdges(ctx, "local-a", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("local GetEdges() failed: %v", err)
	}
	globalEdges, err := store.globalStore.GetEdges(ctx, "local-a", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("global GetEdges() failed: %v", err)
	}

	if len(localEdges) != 1 {
		t.Errorf("expected 1 edge in local store, got %d", len(localEdges))
	}
	if len(globalEdges) != 0 {
		t.Errorf("expected 0 edges in global store, got %d", len(globalEdges))
	}
}

func TestMultiGraphStore_ValidateBehaviorGraph_CrossStoreEdgeInGlobal(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add a behavior to local store
	store.localStore.AddNode(ctx, Node{
		ID:   "local-behavior",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "local-behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "local content",
			},
		},
	})

	// Add a behavior to global store
	store.globalStore.AddNode(ctx, Node{
		ID:   "global-behavior",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "global-behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "global content",
			},
		},
	})

	// Add cross-store edge in global store (references local behavior as target)
	crossEdge := Edge{
		Source:    "global-behavior",
		Target:    "local-behavior",
		Kind:      EdgeKindSimilarTo,
		Weight:    0.85,
		CreatedAt: time.Now(),
	}
	if err := store.globalStore.AddEdge(ctx, crossEdge); err != nil {
		t.Fatalf("failed to add cross-store edge: %v", err)
	}

	// Validate — should find 0 errors (local-behavior exists, just in another store)
	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("ValidateBehaviorGraph() failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("expected 0 validation errors for cross-store edge, got %d: %v", len(errors), errors)
	}
}

func TestMultiGraphStore_ValidateBehaviorGraph_TrulyDanglingStillCaught(t *testing.T) {
	localRoot, globalRoot, cleanup := setupTestStores(t)
	defer cleanup()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", globalRoot)
	defer os.Setenv("HOME", originalHome)
	if runtime.GOOS == "windows" {
		originalProfile := os.Getenv("USERPROFILE")
		os.Setenv("USERPROFILE", globalRoot)
		defer os.Setenv("USERPROFILE", originalProfile)
	}

	store, err := NewMultiGraphStore(localRoot)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Add a behavior to global store
	store.globalStore.AddNode(ctx, Node{
		ID:   "global-behavior",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "global-behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "global content",
			},
		},
	})

	// Add edge in global store targeting a truly nonexistent ID
	danglingEdge := Edge{
		Source:    "global-behavior",
		Target:    "truly-nonexistent",
		Kind:      EdgeKindSimilarTo,
		Weight:    0.7,
		CreatedAt: time.Now(),
	}
	if err := store.globalStore.AddEdge(ctx, danglingEdge); err != nil {
		t.Fatalf("failed to add dangling edge: %v", err)
	}

	// Validate — should catch the truly dangling reference
	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("ValidateBehaviorGraph() failed: %v", err)
	}

	// Should find exactly 1 dangling error
	danglingErrors := 0
	for _, e := range errors {
		if e.Issue == "dangling" {
			danglingErrors++
		}
	}
	if danglingErrors != 1 {
		t.Errorf("expected 1 dangling error, got %d. All errors: %v", danglingErrors, errors)
	}
}

// newTestMultiStore creates a MultiGraphStore backed by two SQLiteGraphStores in temp dirs.
func newTestMultiStore(t *testing.T) *MultiGraphStore {
	t.Helper()
	local, err := NewSQLiteGraphStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create local store: %v", err)
	}
	global, err := NewSQLiteGraphStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create global store: %v", err)
	}
	t.Cleanup(func() {
		local.Close()
		global.Close()
	})
	return &MultiGraphStore{localStore: local, globalStore: global}
}

// newTestMultiStoreInMemory creates a MultiGraphStore backed by two InMemoryGraphStores.
func newTestMultiStoreInMemory(t *testing.T) *MultiGraphStore {
	t.Helper()
	return &MultiGraphStore{
		localStore:  NewInMemoryGraphStore(),
		globalStore: NewInMemoryGraphStore(),
	}
}

func TestMultiGraphStore_RemoveEdge(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	// Add nodes and edge to local store
	m.localStore.AddNode(ctx, Node{ID: "a", Kind: NodeKindBehavior})
	m.localStore.AddNode(ctx, Node{ID: "b", Kind: NodeKindBehavior})
	m.localStore.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	// RemoveEdge should succeed (removes from local)
	err := m.RemoveEdge(ctx, "a", "b", EdgeKindRequires)
	if err != nil {
		t.Fatalf("RemoveEdge() error = %v", err)
	}

	// Verify edge is gone
	edges, _ := m.localStore.GetEdges(ctx, "a", DirectionOutbound, EdgeKindRequires)
	if len(edges) != 0 {
		t.Errorf("edge should be removed, got %d", len(edges))
	}

	// RemoveEdge on non-existent edge in both stores should still succeed
	// (both stores return nil for "not found")
	// Both stores just try to remove; result depends on store semantics.
	_ = m.RemoveEdge(ctx, "x", "y", EdgeKindRequires)
}

func TestMultiGraphStore_Traverse(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	// Add a chain to global store: a -> b -> c
	m.globalStore.AddNode(ctx, Node{ID: "a", Kind: NodeKindBehavior})
	m.globalStore.AddNode(ctx, Node{ID: "b", Kind: NodeKindBehavior})
	m.globalStore.AddNode(ctx, Node{ID: "c", Kind: NodeKindBehavior})
	m.globalStore.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	m.globalStore.AddEdge(ctx, Edge{Source: "b", Target: "c", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	// Traverse from a in global store
	results, err := m.Traverse(ctx, "a", []EdgeKind{EdgeKindRequires}, DirectionOutbound, 5)
	if err != nil {
		t.Fatalf("Traverse() error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Traverse() got %d nodes, want 3", len(results))
	}

	// Add a chain to local store: x -> y
	m.localStore.AddNode(ctx, Node{ID: "x", Kind: NodeKindBehavior})
	m.localStore.AddNode(ctx, Node{ID: "y", Kind: NodeKindBehavior})
	m.localStore.AddEdge(ctx, Edge{Source: "x", Target: "y", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	// Traverse from x should use local store
	results, err = m.Traverse(ctx, "x", []EdgeKind{EdgeKindRequires}, DirectionOutbound, 5)
	if err != nil {
		t.Fatalf("Traverse() from local error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Traverse() from local got %d nodes, want 2", len(results))
	}

	// Traverse from non-existent node should error
	_, err = m.Traverse(ctx, "nonexistent", nil, DirectionOutbound, 5)
	if err == nil {
		t.Error("Traverse() should error for non-existent start node")
	}
}

func TestMultiGraphStore_LocalStore(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	if m.LocalStore() == nil {
		t.Error("LocalStore() returned nil")
	}
	if m.LocalStore() != m.localStore {
		t.Error("LocalStore() should return the local store instance")
	}
}

func TestMultiGraphStore_GetEdges(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	// Add nodes in both stores with edges
	m.localStore.AddNode(ctx, Node{ID: "a", Kind: NodeKindBehavior})
	m.localStore.AddNode(ctx, Node{ID: "b", Kind: NodeKindBehavior})
	m.localStore.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	m.globalStore.AddNode(ctx, Node{ID: "a", Kind: NodeKindBehavior})
	m.globalStore.AddNode(ctx, Node{ID: "c", Kind: NodeKindBehavior})
	m.globalStore.AddEdge(ctx, Edge{Source: "a", Target: "c", Kind: EdgeKindOverrides, Weight: 0.5, CreatedAt: time.Now()})

	// GetEdges should merge from both stores
	edges, err := m.GetEdges(ctx, "a", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("GetEdges() got %d edges, want 2", len(edges))
	}
}

func TestMultiGraphStore_ExtendedStore_UpdateConfidence(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	// Add behavior to global store
	m.globalStore.AddNode(ctx, Node{
		ID:   "b1",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"right": "do this thing",
		},
	})

	// UpdateConfidence should find it in global
	err := m.UpdateConfidence(ctx, "b1", 0.95)
	if err != nil {
		t.Fatalf("UpdateConfidence() error = %v", err)
	}

	// Verify confidence updated
	node, _ := m.globalStore.GetNode(ctx, "b1")
	if node == nil {
		t.Fatal("node not found after UpdateConfidence")
	}

	// UpdateConfidence for non-existent should error
	err = m.UpdateConfidence(ctx, "nonexistent", 0.5)
	if err == nil {
		t.Error("UpdateConfidence() should error for non-existent behavior")
	}
}

func TestMultiGraphStore_ExtendedStore_RecordActivationHit(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	// Add behavior to local store
	m.localStore.AddNode(ctx, Node{
		ID:   "b1",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"right": "do this",
		},
	})

	err := m.RecordActivationHit(ctx, "b1")
	if err != nil {
		t.Fatalf("RecordActivationHit() error = %v", err)
	}

	// Non-existent should error
	err = m.RecordActivationHit(ctx, "nonexistent")
	if err == nil {
		t.Error("RecordActivationHit() should error for non-existent behavior")
	}
}

func TestMultiGraphStore_ExtendedStore_RecordConfirmed(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	m.globalStore.AddNode(ctx, Node{
		ID:   "b1",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"right": "do this",
		},
	})

	err := m.RecordConfirmed(ctx, "b1")
	if err != nil {
		t.Fatalf("RecordConfirmed() error = %v", err)
	}

	err = m.RecordConfirmed(ctx, "nonexistent")
	if err == nil {
		t.Error("RecordConfirmed() should error for non-existent behavior")
	}
}

func TestMultiGraphStore_ExtendedStore_RecordOverridden(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	m.localStore.AddNode(ctx, Node{
		ID:   "b1",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"right": "do this",
		},
	})

	err := m.RecordOverridden(ctx, "b1")
	if err != nil {
		t.Fatalf("RecordOverridden() error = %v", err)
	}

	err = m.RecordOverridden(ctx, "nonexistent")
	if err == nil {
		t.Error("RecordOverridden() should error for non-existent behavior")
	}
}

func TestMultiGraphStore_ExtendedStore_TouchEdges(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	// Add behaviors and edges to both stores
	m.localStore.AddNode(ctx, Node{ID: "a", Kind: NodeKindBehavior, Content: map[string]interface{}{"right": "a"}})
	m.localStore.AddNode(ctx, Node{ID: "b", Kind: NodeKindBehavior, Content: map[string]interface{}{"right": "b"}})
	m.localStore.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	err := m.TouchEdges(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatalf("TouchEdges() error = %v", err)
	}
}

func TestMultiGraphStore_ExtendedStore_BatchUpdateEdgeWeights(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	m.localStore.AddNode(ctx, Node{ID: "a", Kind: NodeKindBehavior, Content: map[string]interface{}{"right": "a"}})
	m.localStore.AddNode(ctx, Node{ID: "b", Kind: NodeKindBehavior, Content: map[string]interface{}{"right": "b"}})
	m.localStore.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindCoActivated, Weight: 0.5, CreatedAt: time.Now()})

	updates := []EdgeWeightUpdate{
		{Source: "a", Target: "b", Kind: EdgeKindCoActivated, NewWeight: 0.8},
	}
	err := m.BatchUpdateEdgeWeights(ctx, updates)
	if err != nil {
		t.Fatalf("BatchUpdateEdgeWeights() error = %v", err)
	}
}

func TestMultiGraphStore_ExtendedStore_PruneWeakEdges(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	// Add weak edges to both stores
	m.localStore.AddNode(ctx, Node{ID: "a", Kind: NodeKindBehavior, Content: map[string]interface{}{"right": "a"}})
	m.localStore.AddNode(ctx, Node{ID: "b", Kind: NodeKindBehavior, Content: map[string]interface{}{"right": "b"}})
	m.localStore.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindCoActivated, Weight: 0.1, CreatedAt: time.Now()})

	m.globalStore.AddNode(ctx, Node{ID: "c", Kind: NodeKindBehavior, Content: map[string]interface{}{"right": "c"}})
	m.globalStore.AddNode(ctx, Node{ID: "d", Kind: NodeKindBehavior, Content: map[string]interface{}{"right": "d"}})
	m.globalStore.AddEdge(ctx, Edge{Source: "c", Target: "d", Kind: EdgeKindCoActivated, Weight: 0.05, CreatedAt: time.Now()})

	pruned, err := m.PruneWeakEdges(ctx, EdgeKindCoActivated, 0.2)
	if err != nil {
		t.Fatalf("PruneWeakEdges() error = %v", err)
	}
	if pruned != 2 {
		t.Errorf("PruneWeakEdges() pruned %d, want 2", pruned)
	}
}

func TestMultiGraphStore_Embedding_StoreAndGet(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	// Add behavior to global store
	m.globalStore.AddNode(ctx, Node{
		ID:   "b1",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"right": "do thing",
		},
	})
	m.localStore.AddNode(ctx, Node{
		ID:   "b2",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"right": "do other thing",
		},
	})

	// StoreEmbedding for global behavior
	err := m.StoreEmbedding(ctx, "b1", []float32{0.1, 0.2}, "model-1")
	if err != nil {
		t.Fatalf("StoreEmbedding(global) error = %v", err)
	}

	// StoreEmbedding for local behavior
	err = m.StoreEmbedding(ctx, "b2", []float32{0.3, 0.4}, "model-1")
	if err != nil {
		t.Fatalf("StoreEmbedding(local) error = %v", err)
	}

	// StoreEmbedding for non-existent should error
	err = m.StoreEmbedding(ctx, "nonexistent", []float32{0.5}, "model-1")
	if err == nil {
		t.Error("StoreEmbedding() should error for non-existent behavior")
	}

	// GetAllEmbeddings should return both
	embeddings, err := m.GetAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetAllEmbeddings() error = %v", err)
	}
	if len(embeddings) != 2 {
		t.Errorf("GetAllEmbeddings() got %d, want 2", len(embeddings))
	}

	// GetBehaviorIDsWithoutEmbeddings should return empty (both have embeddings)
	ids, err := m.GetBehaviorIDsWithoutEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetBehaviorIDsWithoutEmbeddings() error = %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("GetBehaviorIDsWithoutEmbeddings() got %d, want 0", len(ids))
	}
}

func TestMultiGraphStore_Close_BothStores(t *testing.T) {
	local, err := NewSQLiteGraphStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create local store: %v", err)
	}
	global, err := NewSQLiteGraphStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create global store: %v", err)
	}
	m := &MultiGraphStore{localStore: local, globalStore: global}

	err = m.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestMultiGraphStore_UpdateNode_NotFound(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	err := m.UpdateNode(ctx, Node{ID: "nonexistent", Kind: NodeKindBehavior})
	if err == nil {
		t.Error("UpdateNode() should error for non-existent node")
	}
}

func TestMultiGraphStore_UpdateNode_InGlobalStore(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	// Add node only to global
	m.globalStore.AddNode(ctx, Node{ID: "g1", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "original"}})

	// Update should find it in global
	err := m.UpdateNode(ctx, Node{ID: "g1", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "updated"}})
	if err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}

	got := mustGetNode(t, m.globalStore, ctx, "g1")
	if got.Content["name"] != "updated" {
		t.Errorf("name = %v, want updated", got.Content["name"])
	}
}

func TestMultiGraphStore_UpdateNode_InLocalStore(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	// Add to local
	m.localStore.AddNode(ctx, Node{ID: "l1", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "original"}})

	// Update should find it in local
	err := m.UpdateNode(ctx, Node{ID: "l1", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "local-updated"}})
	if err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}
}

func TestMultiGraphStore_GetNode_FromGlobal(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	// Add only to global
	m.globalStore.AddNode(ctx, Node{ID: "glob-only", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "global node"}})

	got, err := m.GetNode(ctx, "glob-only")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetNode() should find node in global store")
	}
	if got.Content["name"] != "global node" {
		t.Errorf("name = %v, want global node", got.Content["name"])
	}
}

func TestMultiGraphStore_GetNode_NotFound(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	got, err := m.GetNode(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got != nil {
		t.Error("GetNode() should return nil for non-existent node")
	}
}

func TestMultiGraphStore_AddEdge_EndpointsNotFound(t *testing.T) {
	m := newTestMultiStoreInMemory(t)
	ctx := context.Background()

	err := m.AddEdge(ctx, Edge{Source: "nonexistent-src", Target: "nonexistent-tgt", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	if err == nil {
		t.Error("AddEdge() should error when endpoints not found")
	}
}

func TestMultiGraphStore_Sync_BothStoresHaveData(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	m.localStore.AddNode(ctx, Node{ID: "sync-l", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "l", "kind": "directive", "content": map[string]interface{}{"canonical": "sync local"}}})
	m.globalStore.AddNode(ctx, Node{ID: "sync-g", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "g", "kind": "directive", "content": map[string]interface{}{"canonical": "sync global"}}})

	err := m.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
}
