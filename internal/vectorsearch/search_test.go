package vectorsearch

import (
	"math"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
		want float64
	}{
		{
			name: "identical vectors",
			a:    []float32{1, 2, 3},
			b:    []float32{1, 2, 3},
			want: 1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1, 0},
			b:    []float32{0, 1},
			want: 0.0,
		},
		{
			name: "opposite vectors",
			a:    []float32{1, 2, 3},
			b:    []float32{-1, -2, -3},
			want: -1.0,
		},
		{
			name: "different lengths",
			a:    []float32{1, 2},
			b:    []float32{1, 2, 3},
			want: 0.0,
		},
		{
			name: "empty vectors",
			a:    []float32{},
			b:    []float32{},
			want: 0.0,
		},
		{
			name: "nil vectors",
			a:    nil,
			b:    nil,
			want: 0.0,
		},
		{
			name: "zero magnitude vector",
			a:    []float32{0, 0, 0},
			b:    []float32{1, 2, 3},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("cosineSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBruteForceSearch_Ordering(t *testing.T) {
	// Query vector points in the direction of [1, 0, 0]
	queryVec := []float32{1, 0, 0}

	candidates := []store.BehaviorEmbedding{
		{BehaviorID: "low", Embedding: []float32{0, 1, 0}},    // orthogonal = 0.0
		{BehaviorID: "high", Embedding: []float32{1, 0, 0}},   // identical = 1.0
		{BehaviorID: "medium", Embedding: []float32{1, 1, 0}}, // partial alignment ~0.707
	}

	results := BruteForceSearch(queryVec, candidates, 3)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].BehaviorID != "high" {
		t.Errorf("expected first result to be 'high', got %q", results[0].BehaviorID)
	}
	if results[1].BehaviorID != "medium" {
		t.Errorf("expected second result to be 'medium', got %q", results[1].BehaviorID)
	}
	if results[2].BehaviorID != "low" {
		t.Errorf("expected third result to be 'low', got %q", results[2].BehaviorID)
	}

	// Verify scores are descending
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted by descending score: [%d]=%f > [%d]=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestBruteForceSearch_TopK(t *testing.T) {
	queryVec := []float32{1, 0, 0}

	candidates := []store.BehaviorEmbedding{
		{BehaviorID: "a", Embedding: []float32{1, 0, 0}},
		{BehaviorID: "b", Embedding: []float32{0.9, 0.1, 0}},
		{BehaviorID: "c", Embedding: []float32{0.5, 0.5, 0}},
		{BehaviorID: "d", Embedding: []float32{0.1, 0.9, 0}},
		{BehaviorID: "e", Embedding: []float32{0, 1, 0}},
	}

	results := BruteForceSearch(queryVec, candidates, 2)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].BehaviorID != "a" {
		t.Errorf("expected first result to be 'a', got %q", results[0].BehaviorID)
	}
	if results[1].BehaviorID != "b" {
		t.Errorf("expected second result to be 'b', got %q", results[1].BehaviorID)
	}
}

func TestBruteForceSearch_EmptyInput(t *testing.T) {
	t.Run("empty candidates", func(t *testing.T) {
		results := BruteForceSearch([]float32{1, 0}, nil, 5)
		if len(results) != 0 {
			t.Errorf("expected empty results for nil candidates, got %d", len(results))
		}
	})

	t.Run("empty candidates slice", func(t *testing.T) {
		results := BruteForceSearch([]float32{1, 0}, []store.BehaviorEmbedding{}, 5)
		if len(results) != 0 {
			t.Errorf("expected empty results for empty candidates, got %d", len(results))
		}
	})

	t.Run("nil query vector", func(t *testing.T) {
		candidates := []store.BehaviorEmbedding{
			{BehaviorID: "a", Embedding: []float32{1, 0}},
		}
		results := BruteForceSearch(nil, candidates, 5)
		if len(results) != 0 {
			t.Errorf("expected empty results for nil query, got %d", len(results))
		}
	})

	t.Run("zero topK", func(t *testing.T) {
		candidates := []store.BehaviorEmbedding{
			{BehaviorID: "a", Embedding: []float32{1, 0}},
		}
		results := BruteForceSearch([]float32{1, 0}, candidates, 0)
		if len(results) != 0 {
			t.Errorf("expected empty results for topK=0, got %d", len(results))
		}
	})
}

func TestBruteForceSearch_SingleCandidate(t *testing.T) {
	queryVec := []float32{1, 0, 0}

	candidates := []store.BehaviorEmbedding{
		{BehaviorID: "only", Embedding: []float32{0.5, 0.5, 0}},
	}

	results := BruteForceSearch(queryVec, candidates, 10)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].BehaviorID != "only" {
		t.Errorf("expected result to be 'only', got %q", results[0].BehaviorID)
	}

	// Score should be cosine similarity of [1,0,0] and [0.5,0.5,0]
	expectedScore := cosineSimilarity(queryVec, candidates[0].Embedding)
	if math.Abs(results[0].Score-expectedScore) > 1e-6 {
		t.Errorf("expected score %f, got %f", expectedScore, results[0].Score)
	}
}
