package similarity

import (
	"strings"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/tagging"
)

// ComputeWhenOverlap calculates overlap between two when predicates.
// Returns -1.0 sentinel when either/both maps are empty/nil (missing signal),
// or when the maps share no keys (orthogonal scoping axes like file_path vs task).
// For maps with shared keys, computes a Dice-like coefficient based on matching values.
func ComputeWhenOverlap(a, b map[string]interface{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return -1.0 // Sentinel: missing signal, redistribute weight
	}

	// Check if any keys are shared between the two maps
	hasSharedKey := false
	for key := range a {
		if _, exists := b[key]; exists {
			hasSharedKey = true
			break
		}
	}
	if !hasSharedKey {
		return -1.0 // Sentinel: orthogonal axes, can't compare
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

// ComputeTagSimilarity computes tag Jaccard similarity with a -1.0 sentinel
// for missing signals. Returns -1.0 when either slice is empty or nil,
// indicating that the tag signal is absent and its weight should be
// redistributed to other signals by WeightedScoreWithTags.
func ComputeTagSimilarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return -1.0
	}
	return tagging.JaccardSimilarity(a, b)
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

// WeightedScoreWithTags computes a weighted similarity score from when-overlap,
// content similarity, and tag similarity. Signals with value < 0 (sentinel)
// are treated as missing, and their weight is redistributed proportionally
// to the remaining signals.
func WeightedScoreWithTags(whenOverlap, contentSimilarity, tagSimilarity float64) float64 {
	type signal struct {
		value  float64
		weight float64
	}
	signals := []signal{
		{whenOverlap, constants.WhenOverlapWeight},
		{contentSimilarity, constants.ContentSimilarityWeight},
		{tagSimilarity, constants.TagSimilarityWeight},
	}

	var totalWeight float64
	for _, s := range signals {
		if s.value >= 0 {
			totalWeight += s.weight
		}
	}
	if totalWeight == 0 {
		return 0.0
	}

	var score float64
	for _, s := range signals {
		if s.value >= 0 {
			score += s.value * (s.weight / totalWeight)
		}
	}
	return score
}

// WeightedScore computes a weighted similarity score from when-overlap and content
// similarity using the standard weights (0.4 for when, 0.6 for content).
// Tags are treated as missing (-1.0 sentinel) and weight is redistributed.
func WeightedScore(whenOverlap, contentSimilarity float64) float64 {
	return WeightedScoreWithTags(whenOverlap, contentSimilarity, -1.0)
}
