package vectorsearch

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// mockEmbedder implements llm.EmbeddingComparer for testing.
type mockEmbedder struct {
	embedFn func(ctx context.Context, text string) ([]float32, error)
}

func (m *mockEmbedder) embedCall(ctx context.Context, text string) ([]float32, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

// mockEmbeddingStore implements store.EmbeddingStore for testing.
type mockEmbeddingStore struct {
	mu         sync.Mutex
	embeddings map[string]embeddingRecord
	nodes      map[string]store.Node
}

type embeddingRecord struct {
	embedding []float32
	model     string
}

func newMockEmbeddingStore() *mockEmbeddingStore {
	return &mockEmbeddingStore{
		embeddings: make(map[string]embeddingRecord),
		nodes:      make(map[string]store.Node),
	}
}

func (m *mockEmbeddingStore) StoreEmbedding(_ context.Context, behaviorID string, embedding []float32, modelName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.embeddings[behaviorID] = embeddingRecord{embedding: embedding, model: modelName}
	return nil
}

func (m *mockEmbeddingStore) GetAllEmbeddings(_ context.Context) ([]store.BehaviorEmbedding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []store.BehaviorEmbedding
	for id, rec := range m.embeddings {
		result = append(result, store.BehaviorEmbedding{BehaviorID: id, Embedding: rec.embedding})
	}
	return result, nil
}

func (m *mockEmbeddingStore) GetBehaviorIDsWithoutEmbeddings(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []string
	for id := range m.nodes {
		if _, ok := m.embeddings[id]; !ok {
			result = append(result, id)
		}
	}
	return result, nil
}

func (m *mockEmbeddingStore) GetNode(_ context.Context, id string) (*store.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	node, ok := m.nodes[id]
	if !ok {
		return nil, errors.New("node not found")
	}
	return &node, nil
}

func TestNewEmbedder(t *testing.T) {
	t.Run("returns embedder for valid embed function", func(t *testing.T) {
		mock := &mockEmbedder{}
		e := NewEmbedder(mock.embedCall, "test-model")
		if e == nil {
			t.Fatal("expected non-nil Embedder")
		}
		if !e.Available() {
			t.Error("expected Available() to return true")
		}
	})

	t.Run("returns nil for nil embed function", func(t *testing.T) {
		e := NewEmbedder(nil, "test-model")
		if e != nil {
			t.Fatal("expected nil Embedder for nil embed function")
		}
	})
}

func TestEmbedder_EmbedAndStore(t *testing.T) {
	t.Run("embeds text with search_document prefix and stores", func(t *testing.T) {
		var capturedText string
		mock := &mockEmbedder{
			embedFn: func(_ context.Context, text string) ([]float32, error) {
				capturedText = text
				return []float32{0.1, 0.2, 0.3}, nil
			},
		}
		es := newMockEmbeddingStore()
		e := NewEmbedder(mock.embedCall, "nomic-embed-text")

		err := e.EmbedAndStore(context.Background(), es, "b1", "always use snake_case")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify search_document prefix was added
		expected := "search_document: always use snake_case"
		if capturedText != expected {
			t.Errorf("expected text %q, got %q", expected, capturedText)
		}

		// Verify stored in store
		rec, ok := es.embeddings["b1"]
		if !ok {
			t.Fatal("embedding not stored")
		}
		if rec.model != "nomic-embed-text" {
			t.Errorf("expected model %q, got %q", "nomic-embed-text", rec.model)
		}
		if len(rec.embedding) != 3 {
			t.Errorf("expected 3 dims, got %d", len(rec.embedding))
		}
	})

	t.Run("returns error on embed failure", func(t *testing.T) {
		mock := &mockEmbedder{
			embedFn: func(_ context.Context, _ string) ([]float32, error) {
				return nil, errors.New("model unavailable")
			},
		}
		es := newMockEmbeddingStore()
		e := NewEmbedder(mock.embedCall, "test-model")

		err := e.EmbedAndStore(context.Background(), es, "b1", "some text")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestEmbedder_EmbedQuery(t *testing.T) {
	t.Run("embeds query with search_query prefix", func(t *testing.T) {
		var capturedText string
		mock := &mockEmbedder{
			embedFn: func(_ context.Context, text string) ([]float32, error) {
				capturedText = text
				return []float32{0.4, 0.5, 0.6}, nil
			},
		}
		e := NewEmbedder(mock.embedCall, "test-model")

		vec, err := e.EmbedQuery(context.Background(), "go development editing main.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expected := "search_query: go development editing main.go"
		if capturedText != expected {
			t.Errorf("expected text %q, got %q", expected, capturedText)
		}
		if len(vec) != 3 {
			t.Errorf("expected 3 dims, got %d", len(vec))
		}
	})
}

// nodeGetter is the interface that BackfillMissing needs from the store.
// Defined here to match the production interface in embedder.go.
type testNodeGetter interface {
	store.EmbeddingStore
	GetNode(ctx context.Context, id string) (*store.Node, error)
}

func TestEmbedder_BackfillMissing(t *testing.T) {
	t.Run("embeds behaviors without embeddings", func(t *testing.T) {
		var embedded []string
		mock := &mockEmbedder{
			embedFn: func(_ context.Context, text string) ([]float32, error) {
				embedded = append(embedded, text)
				return []float32{0.1, 0.2}, nil
			},
		}
		es := newMockEmbeddingStore()
		// Add two behaviors: one with embedding, one without
		es.nodes["b1"] = store.Node{
			ID:   "b1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"canonical": "use snake_case for variables",
			},
		}
		es.nodes["b2"] = store.Node{
			ID:   "b2",
			Kind: "behavior",
			Content: map[string]interface{}{
				"canonical": "prefer composition over inheritance",
			},
		}
		// b1 already has an embedding
		es.embeddings["b1"] = embeddingRecord{embedding: []float32{0.1, 0.2}, model: "test"}

		e := NewEmbedder(mock.embedCall, "test-model")
		count, err := e.BackfillMissing(context.Background(), es)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 backfilled, got %d", count)
		}

		// Verify b2 was embedded with search_document prefix
		if len(embedded) != 1 {
			t.Fatalf("expected 1 embed call, got %d", len(embedded))
		}
		if embedded[0] != "search_document: prefer composition over inheritance" {
			t.Errorf("unexpected text: %s", embedded[0])
		}

		// Verify b2 is now stored
		if _, ok := es.embeddings["b2"]; !ok {
			t.Error("b2 embedding not stored")
		}
	})

	t.Run("skips behaviors without canonical text", func(t *testing.T) {
		mock := &mockEmbedder{}
		es := newMockEmbeddingStore()
		es.nodes["b1"] = store.Node{
			ID:      "b1",
			Kind:    "behavior",
			Content: map[string]interface{}{},
		}

		e := NewEmbedder(mock.embedCall, "test-model")
		count, err := e.BackfillMissing(context.Background(), es)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Errorf("expected 0 backfilled, got %d", count)
		}
	})

	t.Run("returns zero when all behaviors have embeddings", func(t *testing.T) {
		mock := &mockEmbedder{}
		es := newMockEmbeddingStore()
		es.nodes["b1"] = store.Node{
			ID:   "b1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"canonical": "some text",
			},
		}
		es.embeddings["b1"] = embeddingRecord{embedding: []float32{0.1}, model: "test"}

		e := NewEmbedder(mock.embedCall, "test-model")
		count, err := e.BackfillMissing(context.Background(), es)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Errorf("expected 0, got %d", count)
		}
	})
}

func TestExtractCanonical(t *testing.T) {
	tests := []struct {
		name    string
		node    store.Node
		want    string
		wantOK  bool
	}{
		{
			name: "has canonical string",
			node: store.Node{
				Content: map[string]interface{}{"canonical": "use snake_case"},
			},
			want:   "use snake_case",
			wantOK: true,
		},
		{
			name: "nil content",
			node: store.Node{
				Content: nil,
			},
			want:   "",
			wantOK: false,
		},
		{
			name: "no canonical key",
			node: store.Node{
				Content: map[string]interface{}{"other": "value"},
			},
			want:   "",
			wantOK: false,
		},
		{
			name: "canonical is not string",
			node: store.Node{
				Content: map[string]interface{}{"canonical": 42},
			},
			want:   "",
			wantOK: false,
		},
		{
			name: "canonical is empty string",
			node: store.Node{
				Content: map[string]interface{}{"canonical": ""},
			},
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractCanonical(&tt.node)
			if ok != tt.wantOK {
				t.Errorf("extractCanonical() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("extractCanonical() = %q, want %q", got, tt.want)
			}
		})
	}
}
