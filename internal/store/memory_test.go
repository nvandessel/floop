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

	s.AddNode(ctx, node)

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
	s.AddNode(ctx, node)

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

	got, _ := s.GetNode(ctx, "test-1")
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
	s.AddNode(ctx, node)
	s.AddEdge(ctx, Edge{Source: "test-1", Target: "test-2", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})

	err := s.DeleteNode(ctx, "test-1")
	if err != nil {
		t.Errorf("DeleteNode() error = %v", err)
	}

	got, _ := s.GetNode(ctx, "test-1")
	if got != nil {
		t.Error("DeleteNode() node should be deleted")
	}

	edges, _ := s.GetEdges(ctx, "test-1", DirectionBoth, "")
	if len(edges) > 0 {
		t.Error("DeleteNode() should remove associated edges")
	}
}

func TestInMemoryGraphStore_QueryNodes(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	s.AddNode(ctx, Node{ID: "b1", Kind: "behavior", Content: map[string]interface{}{"name": "b1"}})
	s.AddNode(ctx, Node{ID: "b2", Kind: "behavior", Content: map[string]interface{}{"name": "b2"}})
	s.AddNode(ctx, Node{ID: "c1", Kind: "correction", Content: map[string]interface{}{"name": "c1"}})

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

	s.AddNode(ctx, Node{ID: "a", Kind: "behavior"})
	s.AddNode(ctx, Node{ID: "b", Kind: "behavior"})
	s.AddNode(ctx, Node{ID: "c", Kind: "behavior"})

	// Add edges
	s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})
	s.AddEdge(ctx, Edge{Source: "a", Target: "c", Kind: "overrides", Weight: 1.0, CreatedAt: time.Now()})
	s.AddEdge(ctx, Edge{Source: "b", Target: "c", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})

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
	edges, err = s.GetEdges(ctx, "a", DirectionOutbound, "requires")
	if err != nil {
		t.Errorf("GetEdges() error = %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("GetEdges() filtered got %d, want 1", len(edges))
	}

	// Remove edge
	err = s.RemoveEdge(ctx, "a", "b", "requires")
	if err != nil {
		t.Errorf("RemoveEdge() error = %v", err)
	}

	edges, _ = s.GetEdges(ctx, "a", DirectionOutbound, "requires")
	if len(edges) != 0 {
		t.Errorf("RemoveEdge() edge should be removed, got %d", len(edges))
	}
}

func TestInMemoryGraphStore_Traverse(t *testing.T) {
	s := NewInMemoryGraphStore()
	ctx := context.Background()

	// Create a graph: a -> b -> c -> d
	s.AddNode(ctx, Node{ID: "a", Kind: "behavior"})
	s.AddNode(ctx, Node{ID: "b", Kind: "behavior"})
	s.AddNode(ctx, Node{ID: "c", Kind: "behavior"})
	s.AddNode(ctx, Node{ID: "d", Kind: "behavior"})

	s.AddEdge(ctx, Edge{Source: "a", Target: "b", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})
	s.AddEdge(ctx, Edge{Source: "b", Target: "c", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})
	s.AddEdge(ctx, Edge{Source: "c", Target: "d", Kind: "requires", Weight: 1.0, CreatedAt: time.Now()})

	// Traverse outbound with maxDepth 2 (should get a, b, c)
	results, err := s.Traverse(ctx, "a", []string{"requires"}, DirectionOutbound, 2)
	if err != nil {
		t.Errorf("Traverse() error = %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Traverse() got %d nodes, want 3", len(results))
	}

	// Traverse inbound from d (should get all)
	results, err = s.Traverse(ctx, "d", []string{"requires"}, DirectionInbound, 10)
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
			s.AddNode(ctx, node)
			s.GetNode(ctx, node.ID)
			s.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
