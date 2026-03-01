package store

import (
	"context"
	"testing"
)

func mustAddNode(t *testing.T, s GraphStore, ctx context.Context, node Node) string {
	t.Helper()
	id, err := s.AddNode(ctx, node)
	if err != nil {
		t.Fatalf("AddNode(%s) failed: %v", node.ID, err)
	}
	return id
}

func mustAddEdge(t *testing.T, s GraphStore, ctx context.Context, edge Edge) {
	t.Helper()
	if err := s.AddEdge(ctx, edge); err != nil {
		t.Fatalf("AddEdge(%s -> %s) failed: %v", edge.Source, edge.Target, err)
	}
}

func mustGetEdges(t *testing.T, s GraphStore, ctx context.Context, nodeID string, dir Direction, kind EdgeKind) []Edge {
	t.Helper()
	edges, err := s.GetEdges(ctx, nodeID, dir, kind)
	if err != nil {
		t.Fatalf("GetEdges(%s, %s, %s) failed: %v", nodeID, dir, kind, err)
	}
	return edges
}

func mustGetNode(t *testing.T, s GraphStore, ctx context.Context, id string) *Node {
	t.Helper()
	node, err := s.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("GetNode(%s) failed: %v", id, err)
	}
	return node
}
