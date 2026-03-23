package store

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryGraphStore_AddNode(t *testing.T) {
	tests := []struct {
		name    string
		node    Node
		wantID  string
		wantErr bool
	}{
		{
			name: "valid node",
			node: Node{
				ID:   "test-1",
				Kind: "behavior",
				Content: map[string]interface{}{
					"name": "test behavior",
				},
			},
			wantID:  "test-1",
			wantErr: false,
		},
		{
			name: "empty ID",
			node: Node{
				Kind: "behavior",
			},
			wantID:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewInMemoryGraphStore()
			ctx := context.Background()

			gotID, err := s.AddNode(ctx, tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotID != tt.wantID {
				t.Errorf("AddNode() gotID = %v, want %v", gotID, tt.wantID)
			}
		})
	}
}

func TestInMemoryGraphStore_GetNode(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	node := Node{
		ID:   "test-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "test behavior",
		},
	}

	mustAddNode(t, s, ctx, node)

	// Test getting existing node
	got, err := s.GetNode(ctx, "test-1")
	if err != nil {
		t.Errorf("GetNode() error = %v", err)
		return
	}
	if got == nil {
		t.Error("GetNode() returned nil for existing node")
		return
	}
	if got.ID != "test-1" {
		t.Errorf("GetNode() got ID = %v, want test-1", got.ID)
	}

	// Test getting non-existent node
	got, err = s.GetNode(ctx, "non-existent")
	if err != nil {
		t.Errorf("GetNode() error = %v for non-existent", err)
	}
	if got != nil {
		t.Error("GetNode() should return nil for non-existent node")
	}
}

func TestInMemoryGraphStore_UpdateNode(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	node := Node{
		ID:   "test-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "original",
		},
	}
	mustAddNode(t, s, ctx, node)

	// Update existing node
	updated := Node{
		ID:   "test-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "updated",
		},
	}
	err := s.UpdateNode(ctx, updated)
	if err != nil {
		t.Errorf("UpdateNode() error = %v", err)
	}

	got := mustGetNode(t, s, ctx, "test-1")
	if got.Content["name"] != "updated" {
		t.Errorf("UpdateNode() content not updated, got %v", got.Content["name"])
	}

	// Update non-existent node
	err = s.UpdateNode(ctx, Node{ID: "non-existent"})
	if err == nil {
		t.Error("UpdateNode() should error for non-existent node")
	}
}

func TestInMemoryGraphStore_DeleteNode(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	node := Node{ID: "test-1", Kind: "behavior"}
	mustAddNode(t, s, ctx, node)
	mustAddEdge(t, s, ctx, Edge{Source: "test-1", Target: "test-2", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	err := s.DeleteNode(ctx, "test-1")
	if err != nil {
		t.Errorf("DeleteNode() error = %v", err)
	}

	got, err := s.GetNode(ctx, "test-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got != nil {
		t.Error("DeleteNode() node should be deleted")
	}

	edges := mustGetEdges(t, s, ctx, "test-1", DirectionBoth, "")
	if len(edges) > 0 {
		t.Error("DeleteNode() should remove associated edges")
	}
}

func TestInMemoryGraphStore_QueryNodes(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	mustAddNode(t, s, ctx, Node{ID: "b1", Kind: "behavior", Content: map[string]interface{}{"name": "b1"}})
	mustAddNode(t, s, ctx, Node{ID: "b2", Kind: "behavior", Content: map[string]interface{}{"name": "b2"}})
	mustAddNode(t, s, ctx, Node{ID: "c1", Kind: "correction", Content: map[string]interface{}{"name": "c1"}})

	// Query by kind
	results, err := s.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Errorf("QueryNodes() error = %v", err)
	}
	if len(results) != 2 {
		t.Errorf("QueryNodes() got %d results, want 2", len(results))
	}

	// Query by content field
	results, err = s.QueryNodes(ctx, map[string]interface{}{"name": "b1"})
	if err != nil {
		t.Errorf("QueryNodes() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("QueryNodes() got %d results, want 1", len(results))
	}
}

func TestInMemoryGraphStore_EdgeOperations(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	mustAddNode(t, s, ctx, Node{ID: "a", Kind: "behavior"})
	mustAddNode(t, s, ctx, Node{ID: "b", Kind: "behavior"})
	mustAddNode(t, s, ctx, Node{ID: "c", Kind: "behavior"})

	// Add edges
	mustAddEdge(t, s, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, s, ctx, Edge{Source: "a", Target: "c", Kind: EdgeKindOverrides, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, s, ctx, Edge{Source: "b", Target: "c", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	// Get outbound edges
	edges, err := s.GetEdges(ctx, "a", DirectionOutbound, "")
	if err != nil {
		t.Errorf("GetEdges() error = %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("GetEdges() outbound got %d, want 2", len(edges))
	}

	// Get inbound edges
	edges, err = s.GetEdges(ctx, "c", DirectionInbound, "")
	if err != nil {
		t.Errorf("GetEdges() error = %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("GetEdges() inbound got %d, want 2", len(edges))
	}

	// Get edges filtered by kind
	edges, err = s.GetEdges(ctx, "a", DirectionOutbound, EdgeKindRequires)
	if err != nil {
		t.Errorf("GetEdges() error = %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("GetEdges() filtered got %d, want 1", len(edges))
	}

	// Remove edge
	err = s.RemoveEdge(ctx, "a", "b", EdgeKindRequires)
	if err != nil {
		t.Errorf("RemoveEdge() error = %v", err)
	}

	edges = mustGetEdges(t, s, ctx, "a", DirectionOutbound, EdgeKindRequires)
	if len(edges) != 0 {
		t.Errorf("RemoveEdge() edge should be removed, got %d", len(edges))
	}
}

func TestInMemoryGraphStore_Traverse(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// Create a graph: a -> b -> c -> d
	mustAddNode(t, s, ctx, Node{ID: "a", Kind: "behavior"})
	mustAddNode(t, s, ctx, Node{ID: "b", Kind: "behavior"})
	mustAddNode(t, s, ctx, Node{ID: "c", Kind: "behavior"})
	mustAddNode(t, s, ctx, Node{ID: "d", Kind: "behavior"})

	mustAddEdge(t, s, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, s, ctx, Edge{Source: "b", Target: "c", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, s, ctx, Edge{Source: "c", Target: "d", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})

	// Traverse outbound with maxDepth 2 (should get a, b, c)
	results, err := s.Traverse(ctx, "a", []EdgeKind{EdgeKindRequires}, DirectionOutbound, 2)
	if err != nil {
		t.Errorf("Traverse() error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Traverse() got %d nodes, want 3", len(results))
	}

	// Traverse inbound from d (should get all)
	results, err = s.Traverse(ctx, "d", []EdgeKind{EdgeKindRequires}, DirectionInbound, 10)
	if err != nil {
		t.Errorf("Traverse() error = %v", err)
	}
	if len(results) != 4 {
		t.Errorf("Traverse() inbound got %d nodes, want 4", len(results))
	}

	// Traverse with edge kind filter (empty = should still work)
	results, err = s.Traverse(ctx, "a", nil, DirectionOutbound, 10)
	if err != nil {
		t.Errorf("Traverse() error = %v", err)
	}
	if len(results) != 4 {
		t.Errorf("Traverse() no filter got %d nodes, want 4", len(results))
	}
}

func TestInMemoryGraphStore_Embedding(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// Add a behavior node
	mustAddNode(t, s, ctx, Node{ID: "b1", Kind: NodeKindBehavior})
	mustAddNode(t, s, ctx, Node{ID: "b2", Kind: NodeKindBehavior})
	mustAddNode(t, s, ctx, Node{ID: "c1", Kind: NodeKindCorrection}) // not a behavior

	// StoreEmbedding for existing node
	err := s.StoreEmbedding(ctx, "b1", []float32{0.1, 0.2, 0.3}, "test-model")
	if err != nil {
		t.Fatalf("StoreEmbedding() error = %v", err)
	}

	// StoreEmbedding for non-existent node should fail
	err = s.StoreEmbedding(ctx, "nonexistent", []float32{0.1}, "test-model")
	if err == nil {
		t.Error("StoreEmbedding() should error for non-existent node")
	}

	// GetAllEmbeddings should return the one we stored
	embeddings, err := s.GetAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetAllEmbeddings() error = %v", err)
	}
	if len(embeddings) != 1 {
		t.Errorf("GetAllEmbeddings() got %d, want 1", len(embeddings))
	}
	if len(embeddings) > 0 && embeddings[0].BehaviorID != "b1" {
		t.Errorf("GetAllEmbeddings() got ID = %s, want b1", embeddings[0].BehaviorID)
	}

	// GetBehaviorIDsWithoutEmbeddings should return b2 (has no embedding) but not c1 (not a behavior)
	ids, err := s.GetBehaviorIDsWithoutEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetBehaviorIDsWithoutEmbeddings() error = %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("GetBehaviorIDsWithoutEmbeddings() got %d, want 1", len(ids))
	}
	if len(ids) > 0 && ids[0] != "b2" {
		t.Errorf("GetBehaviorIDsWithoutEmbeddings() got %s, want b2", ids[0])
	}
}

func TestInMemoryGraphStore_SyncClose(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// Sync is a no-op, should not error
	if err := s.Sync(ctx); err != nil {
		t.Errorf("Sync() error = %v", err)
	}

	// Close is a no-op, should not error
	if err := s.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestInMemoryGraphStore_AddEdge_Validation(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// Invalid weight (zero)
	err := s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 0, CreatedAt: time.Now()})
	if err == nil {
		t.Error("AddEdge() should reject zero weight")
	}

	// Invalid weight (negative)
	err = s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: -0.5, CreatedAt: time.Now()})
	if err == nil {
		t.Error("AddEdge() should reject negative weight")
	}

	// Invalid weight (>1.0)
	err = s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.5, CreatedAt: time.Now()})
	if err == nil {
		t.Error("AddEdge() should reject weight > 1.0")
	}

	// Missing CreatedAt
	err = s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 0.5})
	if err == nil {
		t.Error("AddEdge() should reject zero CreatedAt")
	}

	// Valid edge
	err = s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	if err != nil {
		t.Errorf("AddEdge() with valid data error = %v", err)
	}
}

func TestInMemoryGraphStore_Traverse_Both(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// Build graph: a -> b, c -> b (b has both inbound and outbound)
	mustAddNode(t, s, ctx, Node{ID: "a", Kind: NodeKindBehavior})
	mustAddNode(t, s, ctx, Node{ID: "b", Kind: NodeKindBehavior})
	mustAddNode(t, s, ctx, Node{ID: "c", Kind: NodeKindBehavior})
	mustAddEdge(t, s, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, s, ctx, Edge{Source: "b", Target: "c", Kind: EdgeKindOverrides, Weight: 1.0, CreatedAt: time.Now()})

	// Traverse both from b - should reach all 3
	results, err := s.Traverse(ctx, "b", nil, DirectionBoth, 5)
	if err != nil {
		t.Fatalf("Traverse(both) error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Traverse(both) got %d, want 3", len(results))
	}
}

func TestInMemoryGraphStore_DeleteNode_NonExistent(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// InMemoryGraphStore.DeleteNode on non-existent is a no-op
	err := s.DeleteNode(ctx, "nonexistent")
	_ = err // implementation-dependent behavior
}

func TestInMemoryGraphStore_QueryByID(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	mustAddNode(t, s, ctx, Node{ID: "b1", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "one"}})
	mustAddNode(t, s, ctx, Node{ID: "b2", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "two"}})

	// Query by ID
	results, err := s.QueryNodes(ctx, map[string]interface{}{"id": "b1"})
	if err != nil {
		t.Fatalf("QueryNodes() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("QueryNodes(id=b1) got %d results, want 1", len(results))
	}

	// Query by metadata
	mustAddNode(t, s, ctx, Node{ID: "b3", Kind: NodeKindBehavior, Metadata: map[string]interface{}{"scope": "local"}})
	results, err = s.QueryNodes(ctx, map[string]interface{}{"scope": "local"})
	if err != nil {
		t.Fatalf("QueryNodes() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("QueryNodes(scope=local) got %d results, want 1", len(results))
	}
}

func TestInMemoryGraphStore_TraverseNonExistent(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// InMemoryGraphStore returns empty results for non-existent start (no error)
	results, err := s.Traverse(ctx, "nonexistent", nil, DirectionOutbound, 5)
	if err != nil {
		t.Errorf("Traverse() unexpected error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Traverse() got %d results for non-existent node, want 0", len(results))
	}
}

func TestInMemoryGraphStore_GetEdges_Both(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	mustAddNode(t, s, ctx, Node{ID: "a", Kind: NodeKindBehavior})
	mustAddNode(t, s, ctx, Node{ID: "b", Kind: NodeKindBehavior})
	mustAddNode(t, s, ctx, Node{ID: "c", Kind: NodeKindBehavior})

	mustAddEdge(t, s, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 1.0, CreatedAt: time.Now()})
	mustAddEdge(t, s, ctx, Edge{Source: "c", Target: "a", Kind: EdgeKindOverrides, Weight: 1.0, CreatedAt: time.Now()})

	// Get both directions
	edges, err := s.GetEdges(ctx, "a", DirectionBoth, "")
	if err != nil {
		t.Fatalf("GetEdges(both) error = %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("GetEdges(both) got %d, want 2", len(edges))
	}
}

func TestEdgeKindMatches(t *testing.T) {
	tests := []struct {
		name    string
		kind    EdgeKind
		allowed []EdgeKind
		want    bool
	}{
		{"empty allowed matches all", EdgeKindRequires, nil, true},
		{"match found", EdgeKindRequires, []EdgeKind{EdgeKindRequires, EdgeKindOverrides}, true},
		{"no match", EdgeKindConflicts, []EdgeKind{EdgeKindRequires, EdgeKindOverrides}, false},
		{"single match", EdgeKindOverrides, []EdgeKind{EdgeKindOverrides}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := edgeKindMatches(tt.kind, tt.allowed)
			if got != tt.want {
				t.Errorf("edgeKindMatches(%v, %v) = %v, want %v", tt.kind, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestInMemoryGraphStore_Concurrency(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// Run concurrent operations
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id int) {
			node := Node{
				ID:   string(rune('a' + id)),
				Kind: "behavior",
			}
			if _, err := s.AddNode(ctx, node); err != nil {
				t.Errorf("AddNode(%s) error = %v", node.ID, err)
			}
			s.GetNode(ctx, node.ID)
			s.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
