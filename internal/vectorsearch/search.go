package vectorsearch

import (
	"math"
	"sort"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// SearchResult pairs a behavior ID with its similarity score.
type SearchResult struct {
	BehaviorID string
	Score      float64
}

// BruteForceSearch finds the topK most similar behaviors to queryVec using cosine similarity.
// Returns results sorted by descending score.
func BruteForceSearch(queryVec []float32, candidates []store.BehaviorEmbedding, topK int) []SearchResult {
	if len(queryVec) == 0 || len(candidates) == 0 || topK <= 0 {
		return nil
	}

	results := make([]SearchResult, 0, len(candidates))
	for _, c := range candidates {
		score := cosineSimilarity(queryVec, c.Embedding)
		results = append(results, SearchResult{
			BehaviorID: c.BehaviorID,
			Score:      score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK > len(results) {
		topK = len(results)
	}

	return results[:topK]
}

// cosineSimilarity computes cosine similarity between two float32 vectors.
// Returns 0.0 for zero-magnitude or mismatched-length vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}

	magA = math.Sqrt(magA)
	magB = math.Sqrt(magB)

	if magA == 0 || magB == 0 {
		return 0.0
	}

	return dot / (magA * magB)
}
