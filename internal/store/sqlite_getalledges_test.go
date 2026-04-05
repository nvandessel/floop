package store

import (
	"context"
	"testing"
	"time"
)

func TestSQLiteGraphStore_GetAllEdges(t *testing.T) {
	s := newTestSQLiteStore(t)
	defer s.Close()
	ctx := context.Background()

	// Add nodes for edge endpoints
	mustAddNode(t, s, ctx, Node{ID: "a", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-a"}}})
	mustAddNode(t, s, ctx, Node{ID: "b", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-b"}}})
	mustAddNode(t, s, ctx, Node{ID: "c", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-c"}}})
	mustAddNode(t, s, ctx, Node{ID: "d", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-d"}}})

	now := time.Now().Truncate(time.Second)

	// Add 3 edges with distinct kinds and weights
	edges := []Edge{
		{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 0.8, CreatedAt: now},
		{Source: "b", Target: "c", Kind: EdgeKindSimilarTo, Weight: 0.5, CreatedAt: now},
		{Source: "c", Target: "d", Kind: EdgeKindCoActivated, Weight: 0.3, CreatedAt: now},
	}
	for _, e := range edges {
		mustAddEdge(t, s, ctx, e)
	}

	// Call GetAllEdges
	got, err := s.GetAllEdges(ctx)
	if err != nil {
		t.Fatalf("GetAllEdges() error = %v", err)
	}

	// Verify count — each edge appears exactly once
	if len(got) != 3 {
		t.Fatalf("GetAllEdges() returned %d edges, want 3", len(got))
	}

	// Build a lookup by source->target for verification
	type edgeKey struct{ src, tgt string }
	byKey := make(map[edgeKey]Edge)
	for _, e := range got {
		k := edgeKey{e.Source, e.Target}
		if _, dup := byKey[k]; dup {
			t.Errorf("duplicate edge returned: %s -> %s", e.Source, e.Target)
		}
		byKey[k] = e
	}

	// Verify each expected edge is present with correct fields
	for _, want := range edges {
		k := edgeKey{want.Source, want.Target}
		e, ok := byKey[k]
		if !ok {
			t.Errorf("missing edge %s -> %s", want.Source, want.Target)
			continue
		}
		if e.Kind != want.Kind {
			t.Errorf("edge %s->%s kind = %s, want %s", want.Source, want.Target, e.Kind, want.Kind)
		}
		if e.Weight != want.Weight {
			t.Errorf("edge %s->%s weight = %f, want %f", want.Source, want.Target, e.Weight, want.Weight)
		}
	}
}

func TestSQLiteGraphStore_GetAllEdges_Empty(t *testing.T) {
	s := newTestSQLiteStore(t)
	defer s.Close()

	got, err := s.GetAllEdges(context.Background())
	if err != nil {
		t.Fatalf("GetAllEdges() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("GetAllEdges() on empty store returned %d edges, want 0", len(got))
	}
}

func TestSQLiteGraphStore_Version_IncrementOnMutations(t *testing.T) {
	s := newTestSQLiteStore(t)
	defer s.Close()
	ctx := context.Background()

	// Initial version should be 0
	if v := s.Version(); v != 0 {
		t.Fatalf("initial Version() = %d, want 0", v)
	}

	// AddNode should bump version
	mustAddNode(t, s, ctx, Node{ID: "a", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-a"}}})
	v1 := s.Version()
	if v1 == 0 {
		t.Fatal("Version() did not increment after AddNode")
	}

	// AddEdge should bump version
	mustAddNode(t, s, ctx, Node{ID: "b", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-b"}}})
	v2 := s.Version()
	now := time.Now()
	mustAddEdge(t, s, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 0.5, CreatedAt: now})
	v3 := s.Version()
	if v3 <= v2 {
		t.Errorf("Version() did not increment after AddEdge: before=%d after=%d", v2, v3)
	}

	// RemoveEdge should bump version
	if err := s.RemoveEdge(ctx, "a", "b", EdgeKindRequires); err != nil {
		t.Fatalf("RemoveEdge() error = %v", err)
	}
	v4 := s.Version()
	if v4 <= v3 {
		t.Errorf("Version() did not increment after RemoveEdge: before=%d after=%d", v3, v4)
	}

	// UpdateNode should bump version
	if err := s.UpdateNode(ctx, Node{ID: "a", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-a-updated"}}}); err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}
	v5 := s.Version()
	if v5 <= v4 {
		t.Errorf("Version() did not increment after UpdateNode: before=%d after=%d", v4, v5)
	}

	// DeleteNode should bump version
	if err := s.DeleteNode(ctx, "b"); err != nil {
		t.Fatalf("DeleteNode() error = %v", err)
	}
	v6 := s.Version()
	if v6 <= v5 {
		t.Errorf("Version() did not increment after DeleteNode: before=%d after=%d", v5, v6)
	}
}

func TestSQLiteGraphStore_Version_NoIncrementOnReads(t *testing.T) {
	s := newTestSQLiteStore(t)
	defer s.Close()
	ctx := context.Background()

	// Seed some data
	mustAddNode(t, s, ctx, Node{ID: "a", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-a"}}})
	mustAddNode(t, s, ctx, Node{ID: "b", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-b"}}})
	mustAddEdge(t, s, ctx, Edge{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 0.5, CreatedAt: time.Now()})

	vBefore := s.Version()

	// Read-only operations should NOT bump version
	_, _ = s.GetNode(ctx, "a")
	_, _ = s.GetEdges(ctx, "a", DirectionOutbound, "")
	_, _ = s.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	_, _ = s.GetAllEdges(ctx)

	vAfter := s.Version()
	if vAfter != vBefore {
		t.Errorf("Version() changed after read-only calls: before=%d after=%d", vBefore, vAfter)
	}
}

func TestMultiGraphStore_GetAllEdges_DelegatesToLocal(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	// Add nodes and edges to local store directly
	local := m.localStore
	mustAddNode(t, local, ctx, Node{ID: "x", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-x"}}})
	mustAddNode(t, local, ctx, Node{ID: "y", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-y"}}})
	mustAddEdge(t, local, ctx, Edge{Source: "x", Target: "y", Kind: EdgeKindRequires, Weight: 0.7, CreatedAt: time.Now()})

	// GetAllEdges on MultiGraphStore should delegate to localStore
	es, ok := m.localStore.(ExtendedGraphStore)
	if !ok {
		t.Fatal("localStore does not implement ExtendedGraphStore")
	}
	_ = es // just verify the type assertion works

	got, err := m.GetAllEdges(ctx)
	if err != nil {
		t.Fatalf("MultiGraphStore.GetAllEdges() error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("MultiGraphStore.GetAllEdges() returned %d edges, want 1", len(got))
	}
}

func TestMultiGraphStore_Version_DelegatesToLocal(t *testing.T) {
	m := newTestMultiStore(t)
	ctx := context.Background()

	v0 := m.Version()
	if v0 != 0 {
		t.Fatalf("initial MultiGraphStore.Version() = %d, want 0", v0)
	}

	// Mutate local store
	local := m.localStore
	mustAddNode(t, local, ctx, Node{ID: "z", Kind: NodeKindBehavior, Content: map[string]interface{}{"content": map[string]interface{}{"canonical": "node-z"}}})

	v1 := m.Version()
	if v1 == 0 {
		t.Error("MultiGraphStore.Version() did not reflect localStore mutation")
	}
}
