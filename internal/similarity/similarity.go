package similarity

import (
	"strings"

	"github.com/nvandessel/feedback-loop/internal/constants"
)

// ComputeWhenOverlap calculates overlap between two when predicates.
// Returns 1.0 when both maps are empty, 0.0 when one is empty.
// For non-empty maps, computes a Dice-like coefficient based on matching keys and values.
func ComputeWhenOverlap(a, b map[string]interface{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0 // Both empty = perfect overlap
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0 // One empty = no overlap
	}

	matches := 0
	total := len(a) + len(b)

	for key, valueA := range a {
		if valueB, exists := b[key]; exists {
			if ValuesEqual(valueA, valueB) {
				matches += 2 // Count both sides as matched
			}
		}
	}

	return float64(matches) / float64(total)
}

// ComputeContentSimilarity calculates Jaccard similarity between two strings.
// Tokenizes both strings and computes the Jaccard index (intersection/union).
func ComputeContentSimilarity(a, b string) float64 {
	wordsA := Tokenize(a)
	wordsB := Tokenize(b)

	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	setA := make(map[string]bool)
	for _, w := range wordsA {
		setA[strings.ToLower(w)] = true
	}

	setB := make(map[string]bool)
	for _, w := range wordsB {
		setB[strings.ToLower(w)] = true
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// WeightedScore computes a weighted similarity score from when-overlap and content
// similarity using the standard weights (0.4 for when, 0.6 for content).
func WeightedScore(whenOverlap, contentSimilarity float64) float64 {
	return whenOverlap*constants.WhenOverlapWeight + contentSimilarity*constants.ContentSimilarityWeight
}
