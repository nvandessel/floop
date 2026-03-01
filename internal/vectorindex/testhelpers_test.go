package vectorindex

import (
	"context"
	"testing"
)

func mustAdd(t *testing.T, idx VectorIndex, ctx context.Context, id string, vec []float32) {
	t.Helper()
	if err := idx.Add(ctx, id, vec); err != nil {
		t.Fatalf("Add(%s) failed: %v", id, err)
	}
}

func mustSearch(t *testing.T, idx VectorIndex, ctx context.Context, vec []float32, k int) []SearchResult {
	t.Helper()
	results, err := idx.Search(ctx, vec, k)
	if err != nil {
		t.Fatalf("Search(topK=%d) failed: %v", k, err)
	}
	return results
}
