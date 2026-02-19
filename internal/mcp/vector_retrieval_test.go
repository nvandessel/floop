package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/vectorsearch"
)

// mockGraphStoreWithEmbeddings satisfies both store.GraphStore (partially) and
// store.EmbeddingStore for testing vector retrieval.
type mockGraphStoreWithEmbeddings struct {
	store.GraphStore // embed for interface satisfaction; only GetNode/QueryNodes used

	nodes      map[string]store.Node
	embeddings []store.BehaviorEmbedding
	unembedded []string
}

func (m *mockGraphStoreWithEmbeddings) GetNode(_ context.Context, id string) (*store.Node, error) {
	n, ok := m.nodes[id]
	if !ok {
		return nil, nil
	}
	return &n, nil
}

func (m *mockGraphStoreWithEmbeddings) QueryNodes(_ context.Context, _ map[string]interface{}) ([]store.Node, error) {
	var result []store.Node
	for _, n := range m.nodes {
		result = append(result, n)
	}
	return result, nil
}

func (m *mockGraphStoreWithEmbeddings) StoreEmbedding(_ context.Context, _ string, _ []float32, _ string) error {
	return nil
}

func (m *mockGraphStoreWithEmbeddings) GetAllEmbeddings(_ context.Context) ([]store.BehaviorEmbedding, error) {
	return m.embeddings, nil
}

func (m *mockGraphStoreWithEmbeddings) GetBehaviorIDsWithoutEmbeddings(_ context.Context) ([]string, error) {
	return m.unembedded, nil
}

func TestVectorRetrieve(t *testing.T) {
	t.Run("returns nodes from vector search plus unembedded behaviors", func(t *testing.T) {
		gs := &mockGraphStoreWithEmbeddings{
			nodes: map[string]store.Node{
				"b1": {ID: "b1", Kind: "behavior", Content: map[string]interface{}{"canonical": "use snake_case"}},
				"b2": {ID: "b2", Kind: "behavior", Content: map[string]interface{}{"canonical": "prefer composition"}},
				"b3": {ID: "b3", Kind: "behavior", Content: map[string]interface{}{"canonical": "new behavior no embedding"}},
			},
			embeddings: []store.BehaviorEmbedding{
				{BehaviorID: "b1", Embedding: []float32{1.0, 0.0, 0.0}},
				{BehaviorID: "b2", Embedding: []float32{0.0, 1.0, 0.0}},
			},
			unembedded: []string{"b3"},
		}

		embedder := vectorsearch.NewEmbedder(
			func(_ context.Context, text string) ([]float32, error) {
				// Return a vector close to b1
				return []float32{0.9, 0.1, 0.0}, nil
			},
			"test-model",
		)

		actCtx := models.ContextSnapshot{Task: "development"}
		nodes, err := vectorRetrieve(context.Background(), embedder, gs, actCtx, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should include both vector-matched (b1, b2) and unembedded (b3)
		if len(nodes) != 3 {
			t.Errorf("expected 3 nodes, got %d", len(nodes))
		}

		// Verify all node IDs are present
		found := make(map[string]bool)
		for _, n := range nodes {
			found[n.ID] = true
		}
		for _, id := range []string{"b1", "b2", "b3"} {
			if !found[id] {
				t.Errorf("expected node %s in results", id)
			}
		}
	})

	t.Run("topK limits vector search results but still includes unembedded", func(t *testing.T) {
		gs := &mockGraphStoreWithEmbeddings{
			nodes: map[string]store.Node{
				"b1": {ID: "b1", Kind: "behavior", Content: map[string]interface{}{"canonical": "a"}},
				"b2": {ID: "b2", Kind: "behavior", Content: map[string]interface{}{"canonical": "b"}},
				"b3": {ID: "b3", Kind: "behavior", Content: map[string]interface{}{"canonical": "c"}},
				"b4": {ID: "b4", Kind: "behavior", Content: map[string]interface{}{"canonical": "d"}},
			},
			embeddings: []store.BehaviorEmbedding{
				{BehaviorID: "b1", Embedding: []float32{1.0, 0.0}},
				{BehaviorID: "b2", Embedding: []float32{0.9, 0.1}},
				{BehaviorID: "b3", Embedding: []float32{0.0, 1.0}},
			},
			unembedded: []string{"b4"},
		}

		embedder := vectorsearch.NewEmbedder(
			func(_ context.Context, _ string) ([]float32, error) {
				return []float32{1.0, 0.0}, nil // closest to b1, then b2
			},
			"test-model",
		)

		actCtx := models.ContextSnapshot{Task: "development"}
		nodes, err := vectorRetrieve(context.Background(), embedder, gs, actCtx, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// topK=1 -> 1 from vector search + 1 unembedded = 2
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes (1 vector + 1 unembedded), got %d", len(nodes))
		}

		found := make(map[string]bool)
		for _, n := range nodes {
			found[n.ID] = true
		}
		// b1 should be the top vector match
		if !found["b1"] {
			t.Error("expected b1 (top vector match) in results")
		}
		// b4 should be included as unembedded
		if !found["b4"] {
			t.Error("expected b4 (unembedded) in results")
		}
	})

	t.Run("returns error when embed query fails", func(t *testing.T) {
		gs := &mockGraphStoreWithEmbeddings{
			nodes:      map[string]store.Node{},
			embeddings: []store.BehaviorEmbedding{},
		}

		embedder := vectorsearch.NewEmbedder(
			func(_ context.Context, _ string) ([]float32, error) {
				return nil, errors.New("embed failed")
			},
			"test-model",
		)

		actCtx := models.ContextSnapshot{Task: "development"}
		_, err := vectorRetrieve(context.Background(), embedder, gs, actCtx, 10)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("deduplicates when unembedded ID overlaps with vector result", func(t *testing.T) {
		gs := &mockGraphStoreWithEmbeddings{
			nodes: map[string]store.Node{
				"b1": {ID: "b1", Kind: "behavior", Content: map[string]interface{}{"canonical": "a"}},
			},
			embeddings: []store.BehaviorEmbedding{
				{BehaviorID: "b1", Embedding: []float32{1.0}},
			},
			// Claim b1 is also "unembedded" to simulate a race condition
			unembedded: []string{"b1"},
		}

		embedder := vectorsearch.NewEmbedder(
			func(_ context.Context, _ string) ([]float32, error) {
				return []float32{1.0}, nil
			},
			"test-model",
		)

		actCtx := models.ContextSnapshot{Task: "development"}
		nodes, err := vectorRetrieve(context.Background(), embedder, gs, actCtx, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should not duplicate b1
		if len(nodes) != 1 {
			t.Errorf("expected 1 node (deduplicated), got %d", len(nodes))
		}
	})

	t.Run("skips missing nodes gracefully", func(t *testing.T) {
		gs := &mockGraphStoreWithEmbeddings{
			nodes: map[string]store.Node{
				// b1 exists in embeddings but NOT in nodes (deleted)
				"b2": {ID: "b2", Kind: "behavior", Content: map[string]interface{}{"canonical": "b"}},
			},
			embeddings: []store.BehaviorEmbedding{
				{BehaviorID: "b1", Embedding: []float32{1.0, 0.0}},
				{BehaviorID: "b2", Embedding: []float32{0.0, 1.0}},
			},
			unembedded: []string{},
		}

		embedder := vectorsearch.NewEmbedder(
			func(_ context.Context, _ string) ([]float32, error) {
				return []float32{0.5, 0.5}, nil
			},
			"test-model",
		)

		actCtx := models.ContextSnapshot{Task: "development"}
		nodes, err := vectorRetrieve(context.Background(), embedder, gs, actCtx, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Only b2 should be returned (b1 is missing from store)
		if len(nodes) != 1 {
			t.Errorf("expected 1 node, got %d", len(nodes))
		}
		if nodes[0].ID != "b2" {
			t.Errorf("expected b2, got %s", nodes[0].ID)
		}
	})
}
