package vectorindex

import (
	"context"
	"sync"
	"testing"

	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/vectorsearch"
)

func TestBruteForceIndex_AddAndSearch(t *testing.T) {
	idx := NewBruteForceIndex()
	ctx := context.Background()

	mustAdd(t, idx, ctx, "b1", []float32{1, 0, 0})
	mustAdd(t, idx, ctx, "b2", []float32{0, 1, 0})
	mustAdd(t, idx, ctx, "b3", []float32{0, 0, 1})

	results, err := idx.Search(ctx, []float32{1, 0, 0}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// b1 should be first (exact match, score=1.0)
	if results[0].BehaviorID != "b1" {
		t.Errorf("expected b1 first, got %s", results[0].BehaviorID)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for exact match, got %f", results[0].Score)
	}
	// b2 and b3 should have score 0.0 (orthogonal)
	if results[1].Score > 0.01 {
		t.Errorf("expected score ~0.0 for orthogonal, got %f", results[1].Score)
	}
}

func TestBruteForceIndex_ReplaceExisting(t *testing.T) {
	idx := NewBruteForceIndex()
	ctx := context.Background()

	mustAdd(t, idx, ctx, "b1", []float32{1, 0, 0})
	mustAdd(t, idx, ctx, "b1", []float32{0, 1, 0}) // replace

	if idx.Len() != 1 {
		t.Errorf("expected Len()=1 after replace, got %d", idx.Len())
	}

	results := mustSearch(t, idx, ctx, []float32{0, 1, 0}, 1)
	if len(results) != 1 || results[0].BehaviorID != "b1" {
		t.Fatalf("expected b1 result")
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for replaced vector, got %f", results[0].Score)
	}
}

func TestBruteForceIndex_Remove(t *testing.T) {
	idx := NewBruteForceIndex()
	ctx := context.Background()

	mustAdd(t, idx, ctx, "b1", []float32{1, 0, 0})
	mustAdd(t, idx, ctx, "b2", []float32{0, 1, 0})
	mustAdd(t, idx, ctx, "b3", []float32{0, 0, 1})

	if err := idx.Remove(ctx, "b2"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if idx.Len() != 2 {
		t.Errorf("expected Len()=2 after remove, got %d", idx.Len())
	}

	results := mustSearch(t, idx, ctx, []float32{0, 1, 0}, 3)
	for _, r := range results {
		if r.BehaviorID == "b2" {
			t.Error("removed b2 should not appear in results")
		}
	}
}

func TestBruteForceIndex_RemoveNonexistent(t *testing.T) {
	idx := NewBruteForceIndex()
	err := idx.Remove(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected nil error for removing nonexistent, got %v", err)
	}
}

func TestBruteForceIndex_SearchEmpty(t *testing.T) {
	idx := NewBruteForceIndex()
	results, err := idx.Search(context.Background(), []float32{1, 0}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestBruteForceIndex_SearchTopKExceedsLen(t *testing.T) {
	idx := NewBruteForceIndex()
	ctx := context.Background()

	mustAdd(t, idx, ctx, "b1", []float32{1, 0})
	mustAdd(t, idx, ctx, "b2", []float32{0, 1})

	results := mustSearch(t, idx, ctx, []float32{1, 0}, 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results when topK > len, got %d", len(results))
	}
}

func TestBruteForceIndex_SearchTopKZero(t *testing.T) {
	idx := NewBruteForceIndex()
	ctx := context.Background()

	mustAdd(t, idx, ctx, "b1", []float32{1, 0})

	results, err := idx.Search(ctx, []float32{1, 0}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results for topK=0, got %d", len(results))
	}
}

func TestBruteForceIndex_ConcurrentAccess(t *testing.T) {
	idx := NewBruteForceIndex()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := string(rune('a' + n))
			vec := []float32{float32(n), float32(n + 1), float32(n + 2)}
			_ = idx.Add(ctx, id, vec)
			_, _ = idx.Search(ctx, vec, 3)
			_ = idx.Remove(ctx, id)
		}(i)
	}
	wg.Wait()
}

func TestBruteForceIndex_Save(t *testing.T) {
	idx := NewBruteForceIndex()
	if err := idx.Save(context.Background()); err != nil {
		t.Errorf("Save() error = %v, want nil", err)
	}
}

func TestBruteForceIndex_Close(t *testing.T) {
	idx := NewBruteForceIndex()
	if err := idx.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestBruteForceIndex_SearchEmptyQuery(t *testing.T) {
	idx := NewBruteForceIndex()
	ctx := context.Background()
	mustAdd(t, idx, ctx, "b1", []float32{1, 0})

	results, err := idx.Search(ctx, []float32{}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results for empty query, got %d", len(results))
	}
}

func TestBruteForceIndex_MatchesBruteForceSearch(t *testing.T) {
	// Verify that BruteForceIndex produces the same ordering as the
	// existing vectorsearch.BruteForceSearch for identical inputs.
	idx := NewBruteForceIndex()
	ctx := context.Background()

	vecs := []struct {
		id  string
		vec []float32
	}{
		{"b1", []float32{1, 0, 0}},
		{"b2", []float32{0.9, 0.1, 0}},
		{"b3", []float32{0, 0, 1}},
	}
	candidates := make([]store.BehaviorEmbedding, len(vecs))
	for i, v := range vecs {
		mustAdd(t, idx, ctx, v.id, v.vec)
		candidates[i] = store.BehaviorEmbedding{BehaviorID: v.id, Embedding: v.vec}
	}

	query := []float32{1, 0, 0}
	idxResults, err := idx.Search(ctx, query, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bfResults := vectorsearch.BruteForceSearch(query, candidates, 3)

	if len(idxResults) != len(bfResults) {
		t.Fatalf("length mismatch: index=%d, brute-force=%d", len(idxResults), len(bfResults))
	}
	for i := range idxResults {
		if idxResults[i].BehaviorID != bfResults[i].BehaviorID {
			t.Errorf("position %d: index=%s, brute-force=%s", i, idxResults[i].BehaviorID, bfResults[i].BehaviorID)
		}
	}
}
