//go:build cgo

package vectorindex

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
)

func newTestLanceDB(t *testing.T) *LanceDBIndex {
	t.Helper()
	dir := t.TempDir()
	idx, err := NewLanceDBIndex(LanceDBConfig{Dir: dir, Dims: 8})
	if err != nil {
		t.Fatalf("NewLanceDBIndex: %v", err)
	}
	return idx
}

func TestLanceDBIndex_Create(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()
	if idx.Len() != 0 {
		t.Errorf("expected Len()=0, got %d", idx.Len())
	}
}

func TestLanceDBIndex_AddAndSearch(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	ctx := context.Background()
	v1 := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	v2 := []float32{0, 1, 0, 0, 0, 0, 0, 0}
	v3 := []float32{0, 0, 1, 0, 0, 0, 0, 0}

	mustAdd(t, idx, ctx, "b1", v1)
	mustAdd(t, idx, ctx, "b2", v2)
	mustAdd(t, idx, ctx, "b3", v3)

	if idx.Len() != 3 {
		t.Fatalf("expected Len()=3, got %d", idx.Len())
	}

	results := mustSearch(t, idx, ctx, v1, 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].BehaviorID != "b1" {
		t.Errorf("expected b1 first, got %s", results[0].BehaviorID)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for exact match, got %f", results[0].Score)
	}
}

func TestLanceDBIndex_ReplaceExisting(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	ctx := context.Background()
	v1 := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	v2 := []float32{0, 1, 0, 0, 0, 0, 0, 0}

	mustAdd(t, idx, ctx, "b1", v1)
	// Replace b1 with a different vector.
	mustAdd(t, idx, ctx, "b1", v2)

	if idx.Len() != 1 {
		t.Errorf("expected Len()=1 after replace, got %d", idx.Len())
	}

	results := mustSearch(t, idx, ctx, v2, 1)
	if len(results) != 1 || results[0].BehaviorID != "b1" {
		t.Fatalf("expected b1, got %v", results)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for replaced vector, got %f", results[0].Score)
	}
}

func TestLanceDBIndex_Remove(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	ctx := context.Background()
	v1 := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	v2 := []float32{0, 1, 0, 0, 0, 0, 0, 0}
	v3 := []float32{0, 0, 1, 0, 0, 0, 0, 0}

	mustAdd(t, idx, ctx, "b1", v1)
	mustAdd(t, idx, ctx, "b2", v2)
	mustAdd(t, idx, ctx, "b3", v3)

	if err := idx.Remove(ctx, "b2"); err != nil {
		t.Fatal(err)
	}

	if idx.Len() != 2 {
		t.Errorf("expected Len()=2 after remove, got %d", idx.Len())
	}

	results := mustSearch(t, idx, ctx, v2, 3)
	for _, r := range results {
		if r.BehaviorID == "b2" {
			t.Error("removed b2 should not appear in results")
		}
	}
}

func TestLanceDBIndex_RemoveNonexistent(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	err := idx.Remove(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected nil error for removing nonexistent, got %v", err)
	}
}

func TestLanceDBIndex_SearchEmpty(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	results, err := idx.Search(context.Background(), []float32{1, 0, 0, 0, 0, 0, 0, 0}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestLanceDBIndex_SearchTopKExceedsLen(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	ctx := context.Background()
	mustAdd(t, idx, ctx, "b1", []float32{1, 0, 0, 0, 0, 0, 0, 0})
	mustAdd(t, idx, ctx, "b2", []float32{0, 1, 0, 0, 0, 0, 0, 0})

	results := mustSearch(t, idx, ctx, []float32{1, 0, 0, 0, 0, 0, 0, 0}, 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results when topK > len, got %d", len(results))
	}
}

func TestLanceDBIndex_SearchTopKZero(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	ctx := context.Background()
	mustAdd(t, idx, ctx, "b1", []float32{1, 0, 0, 0, 0, 0, 0, 0})

	results, err := idx.Search(ctx, []float32{1, 0, 0, 0, 0, 0, 0, 0}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results for topK=0, got %d", len(results))
	}
}

func TestLanceDBIndex_ScoreRange(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	ctx := context.Background()
	vecs := [][]float32{
		{1, 0, 0, 0, 0, 0, 0, 0},
		{0.9, 0.1, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 1},
	}
	for i, v := range vecs {
		mustAdd(t, idx, ctx, fmt.Sprintf("b%d", i), v)
	}

	query := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	results, err := idx.Search(ctx, query, 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	for _, r := range results {
		if r.Score < -1.0 || r.Score > 1.0+1e-6 {
			t.Errorf("score %f out of expected range [-1, 1] for %s", r.Score, r.BehaviorID)
		}
	}

	// Exact match should be close to 1.0.
	if len(results) > 0 && results[0].BehaviorID == "b0" {
		if math.Abs(results[0].Score-1.0) > 0.01 {
			t.Errorf("exact match score should be ~1.0, got %f", results[0].Score)
		}
	}

	// Results should be sorted descending by score.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score+1e-6 {
			t.Errorf("results not sorted: score[%d]=%f > score[%d]=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestLanceDBIndex_Persistence(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create and populate an index.
	idx, err := NewLanceDBIndex(LanceDBConfig{Dir: dir, Dims: 8})
	if err != nil {
		t.Fatalf("NewLanceDBIndex: %v", err)
	}

	v1 := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	v2 := []float32{0, 1, 0, 0, 0, 0, 0, 0}
	v3 := []float32{0, 0, 1, 0, 0, 0, 0, 0}
	mustAdd(t, idx, ctx, "b1", v1)
	mustAdd(t, idx, ctx, "b2", v2)
	mustAdd(t, idx, ctx, "b3", v3)

	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen from the same directory.
	idx2, err := NewLanceDBIndex(LanceDBConfig{Dir: dir, Dims: 8})
	if err != nil {
		t.Fatalf("reload NewLanceDBIndex: %v", err)
	}
	defer idx2.Close()

	if idx2.Len() != 3 {
		t.Fatalf("expected Len()=3 after reload, got %d", idx2.Len())
	}

	results, err := idx2.Search(ctx, v1, 1)
	if err != nil {
		t.Fatalf("Search after reload: %v", err)
	}
	if len(results) != 1 || results[0].BehaviorID != "b1" {
		t.Errorf("expected b1 after reload, got %v", results)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 after reload, got %f", results[0].Score)
	}
}

func TestLanceDBIndex_ConcurrentAccess(t *testing.T) {
	idx := newTestLanceDB(t)
	defer idx.Close()

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("b%d", n)
			vec := make([]float32, 8)
			vec[n%8] = float32(n + 1)
			_ = idx.Add(ctx, id, vec)
			_, _ = idx.Search(ctx, vec, 3)
			_ = idx.Remove(ctx, id)
		}(i)
	}
	wg.Wait()
}

// Verify LanceDBIndex satisfies the VectorIndex interface at compile time.
var _ VectorIndex = (*LanceDBIndex)(nil)
