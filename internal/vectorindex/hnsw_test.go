package vectorindex

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sync"
	"testing"
)

func newTestHNSW(t *testing.T) *HNSWIndex {
	t.Helper()
	idx, err := NewHNSWIndex(HNSWConfig{})
	if err != nil {
		t.Fatalf("NewHNSWIndex: %v", err)
	}
	return idx
}

func TestHNSWIndex_AddAndSearch(t *testing.T) {
	idx := newTestHNSW(t)
	ctx := context.Background()

	// 8-dim vectors: axis-aligned unit vectors for clarity.
	v1 := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	v2 := []float32{0, 1, 0, 0, 0, 0, 0, 0}
	v3 := []float32{0, 0, 1, 0, 0, 0, 0, 0}

	if err := idx.Add(ctx, "b1", v1); err != nil {
		t.Fatalf("Add b1: %v", err)
	}
	if err := idx.Add(ctx, "b2", v2); err != nil {
		t.Fatalf("Add b2: %v", err)
	}
	if err := idx.Add(ctx, "b3", v3); err != nil {
		t.Fatalf("Add b3: %v", err)
	}

	if idx.Len() != 3 {
		t.Fatalf("expected Len()=3, got %d", idx.Len())
	}

	results, err := idx.Search(ctx, v1, 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// b1 should be first (exact match, score ~1.0).
	if results[0].BehaviorID != "b1" {
		t.Errorf("expected b1 first, got %s", results[0].BehaviorID)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for exact match, got %f", results[0].Score)
	}
}

func TestHNSWIndex_ReplaceExisting(t *testing.T) {
	idx := newTestHNSW(t)
	ctx := context.Background()

	v1 := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	v2 := []float32{0, 1, 0, 0, 0, 0, 0, 0}

	if err := idx.Add(ctx, "b1", v1); err != nil {
		t.Fatal(err)
	}
	// Replace b1 with a different vector.
	if err := idx.Add(ctx, "b1", v2); err != nil {
		t.Fatal(err)
	}

	if idx.Len() != 1 {
		t.Errorf("expected Len()=1 after replace, got %d", idx.Len())
	}

	results, err := idx.Search(ctx, v2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].BehaviorID != "b1" {
		t.Fatalf("expected b1, got %v", results)
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for replaced vector, got %f", results[0].Score)
	}
}

func TestHNSWIndex_Remove(t *testing.T) {
	idx := newTestHNSW(t)
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

func TestHNSWIndex_RemoveNonexistent(t *testing.T) {
	idx := newTestHNSW(t)
	err := idx.Remove(context.Background(), "nonexistent")
	if err != nil {
		t.Errorf("expected nil error for removing nonexistent, got %v", err)
	}
}

func TestHNSWIndex_SearchEmpty(t *testing.T) {
	idx := newTestHNSW(t)
	results, err := idx.Search(context.Background(), []float32{1, 0, 0, 0, 0, 0, 0, 0}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestHNSWIndex_SearchTopKExceedsLen(t *testing.T) {
	idx := newTestHNSW(t)
	ctx := context.Background()

	mustAdd(t, idx, ctx, "b1", []float32{1, 0, 0, 0, 0, 0, 0, 0})
	mustAdd(t, idx, ctx, "b2", []float32{0, 1, 0, 0, 0, 0, 0, 0})

	results := mustSearch(t, idx, ctx, []float32{1, 0, 0, 0, 0, 0, 0, 0}, 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results when topK > len, got %d", len(results))
	}
}

func TestHNSWIndex_SearchTopKZero(t *testing.T) {
	idx := newTestHNSW(t)
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

func TestHNSWIndex_Persistence(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create and populate an index.
	idx, err := NewHNSWIndex(HNSWConfig{Dir: dir})
	if err != nil {
		t.Fatalf("NewHNSWIndex: %v", err)
	}

	v1 := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	v2 := []float32{0, 1, 0, 0, 0, 0, 0, 0}
	mustAdd(t, idx, ctx, "b1", v1)
	mustAdd(t, idx, ctx, "b2", v2)

	if err := idx.Save(ctx); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reload from the same directory.
	idx2, err := NewHNSWIndex(HNSWConfig{Dir: dir})
	if err != nil {
		t.Fatalf("reload NewHNSWIndex: %v", err)
	}
	defer idx2.Close()

	if idx2.Len() != 2 {
		t.Fatalf("expected Len()=2 after reload, got %d", idx2.Len())
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

func TestHNSWIndex_PersistenceLargeGraph(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create an index with more vectors than the default EfSearch (100)
	// to verify shadow-map reconstruction recovers all nodes.
	idx, err := NewHNSWIndex(HNSWConfig{Dir: dir})
	if err != nil {
		t.Fatalf("NewHNSWIndex: %v", err)
	}

	// Insert more vectors than the default EfSearch (100) to verify
	// shadow-map reconstruction recovers all entries on reload.
	const numVectors = 150
	const dims = 8
	for i := 0; i < numVectors; i++ {
		key := fmt.Sprintf("b%d", i)
		v := make([]float32, dims)
		for d := 0; d < dims; d++ {
			v[d] = float32(i*dims+d) / float32(numVectors*dims)
		}
		mustAdd(t, idx, ctx, key, v)
	}

	if err := idx.Save(ctx); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reload and verify all vectors survived the round-trip.
	idx2, err := NewHNSWIndex(HNSWConfig{Dir: dir})
	if err != nil {
		t.Fatalf("reload NewHNSWIndex: %v", err)
	}
	defer idx2.Close()

	if idx2.Len() != numVectors {
		t.Fatalf("expected Len()=%d after reload, got %d", numVectors, idx2.Len())
	}

	// Trigger a rebuild (via Remove) to confirm the shadow map drove the
	// reconstruction — if any entries were missing, Len would drop.
	if err := idx2.Remove(ctx, "b0"); err != nil {
		t.Fatalf("Remove after reload: %v", err)
	}
	if idx2.Len() != numVectors-1 {
		t.Fatalf("expected Len()=%d after remove, got %d", numVectors-1, idx2.Len())
	}
}

func TestHNSWIndex_PersistenceFileCreated(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	idx, err := NewHNSWIndex(HNSWConfig{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, idx, ctx, "b1", []float32{1, 0, 0, 0, 0, 0, 0, 0})
	if err := idx.Save(ctx); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, hnswFileName)
	// Verify file exists by attempting to load.
	idx2, err := NewHNSWIndex(HNSWConfig{Dir: dir})
	if err != nil {
		t.Fatalf("failed to reload from %s: %v", path, err)
	}
	if idx2.Len() != 1 {
		t.Errorf("expected 1 node after reload, got %d", idx2.Len())
	}
}

func TestHNSWIndex_ConcurrentAccess(t *testing.T) {
	idx := newTestHNSW(t)
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

func TestHNSWIndex_ScoreRange(t *testing.T) {
	idx := newTestHNSW(t)
	ctx := context.Background()

	// Add several 8-dim vectors.
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

// Verify HNSWIndex satisfies the VectorIndex interface at compile time.
var _ VectorIndex = (*HNSWIndex)(nil)
