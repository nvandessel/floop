//go:build cgo

package spreading

import (
	"context"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/store"
)

func newTestExtendedStore(t *testing.T) store.ExtendedGraphStore {
	t.Helper()
	tmpDir := t.TempDir()
	s, err := store.NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addTestNode(t *testing.T, s store.ExtendedGraphStore, ctx context.Context, id string) {
	t.Helper()
	_, err := s.AddNode(ctx, store.Node{
		ID:   id,
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"content": map[string]interface{}{"canonical": "node-" + id},
		},
	})
	if err != nil {
		t.Fatalf("AddNode(%s): %v", id, err)
	}
}

func addTestEdge(t *testing.T, s store.ExtendedGraphStore, ctx context.Context, src, tgt string, kind store.EdgeKind, weight float64) {
	t.Helper()
	now := time.Now()
	err := s.AddEdge(ctx, store.Edge{
		Source:        src,
		Target:        tgt,
		Kind:          kind,
		Weight:        weight,
		CreatedAt:     now,
		LastActivated: &now,
	})
	if err != nil {
		t.Fatalf("AddEdge(%s->%s): %v", src, tgt, err)
	}
}

func TestLoadGraph_ThreeEdges(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	addTestNode(t, s, ctx, "a")
	addTestNode(t, s, ctx, "b")
	addTestNode(t, s, ctx, "c")

	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.8)
	addTestEdge(t, s, ctx, "b", "c", store.EdgeKindSimilarTo, 0.5)
	addTestEdge(t, s, ctx, "a", "c", store.EdgeKindConflicts, 0.3)

	config := DefaultConfig()

	graph, idmap, err := loadGraph(ctx, s, config)
	if err != nil {
		t.Fatalf("loadGraph() error = %v", err)
	}
	defer sproinkGraphFree(graph)

	if graph == nil {
		t.Fatal("loadGraph() returned nil graph")
	}

	// All 3 nodes should be in the IDMap
	if idmap.Len() != 3 {
		t.Errorf("IDMap.Len() = %d, want 3", idmap.Len())
	}

	// Verify all node IDs are mapped
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := idmap.ToU32(id); !ok {
			t.Errorf("IDMap missing node %q", id)
		}
	}
}

func TestLoadGraph_IsolatedNodes(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	addTestNode(t, s, ctx, "a")
	addTestNode(t, s, ctx, "b")
	addTestNode(t, s, ctx, "isolated") // no edges

	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.8)

	config := DefaultConfig()

	graph, idmap, err := loadGraph(ctx, s, config)
	if err != nil {
		t.Fatalf("loadGraph() error = %v", err)
	}
	defer sproinkGraphFree(graph)

	// All 3 nodes including the isolated one should be in IDMap
	if idmap.Len() != 3 {
		t.Errorf("IDMap.Len() = %d, want 3", idmap.Len())
	}

	if _, ok := idmap.ToU32("isolated"); !ok {
		t.Error("IDMap missing isolated node")
	}
}

func TestLoadGraph_EmptyStore(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	config := DefaultConfig()

	graph, idmap, err := loadGraph(ctx, s, config)
	if err != nil {
		t.Fatalf("loadGraph() error = %v", err)
	}
	if graph != nil {
		defer sproinkGraphFree(graph)
	}

	// Empty store: no nodes, no edges, nil graph is acceptable
	if idmap.Len() != 0 {
		t.Errorf("IDMap.Len() = %d, want 0", idmap.Len())
	}
}

// staticTagProvider is a test TagProvider that returns a fixed tag map.
type staticTagProvider struct {
	tags map[string][]string
}

func (p *staticTagProvider) GetAllBehaviorTags(_ context.Context) map[string][]string {
	return p.tags
}

func TestLoadGraph_WithAffinityEdges(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	addTestNode(t, s, ctx, "a")
	addTestNode(t, s, ctx, "b")
	addTestNode(t, s, ctx, "c")

	// One real edge
	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.8)

	affinityConfig := DefaultAffinityConfig()
	config := DefaultConfig()
	config.Affinity = &affinityConfig
	// a and c share tags ("go", "testing") with Jaccard=1.0 -> should create affinity edge
	// b has different tags -> lower Jaccard with a and c
	config.TagProvider = &staticTagProvider{
		tags: map[string][]string{
			"a": {"go", "testing"},
			"c": {"go", "testing"},
			"b": {"python"},
		},
	}

	graph, idmap, err := loadGraph(ctx, s, config)
	if err != nil {
		t.Fatalf("loadGraph() error = %v", err)
	}
	defer sproinkGraphFree(graph)

	if graph == nil {
		t.Fatal("loadGraph() returned nil graph")
	}

	// All 3 nodes should be mapped (a, b from edge + c from isolated node query)
	if idmap.Len() != 3 {
		t.Errorf("IDMap.Len() = %d, want 3", idmap.Len())
	}

	// c should be in the map (included via affinity edges or isolated node query)
	if _, ok := idmap.ToU32("c"); !ok {
		t.Error("IDMap missing node c (should be included via affinity or isolated node)")
	}
}
