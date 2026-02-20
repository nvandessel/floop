package vectorsearch

import (
	"sort"

	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/vecmath"
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
		score := vecmath.CosineSimilarity(queryVec, c.Embedding)
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
