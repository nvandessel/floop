package llm

import (
	"context"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// FallbackClient implements the Client interface using rule-based Jaccard
// similarity instead of LLM calls. It is used when no LLM provider is available.
type FallbackClient struct{}

// NewFallbackClient creates a new FallbackClient.
func NewFallbackClient() *FallbackClient {
	return &FallbackClient{}
}

// CompareBehaviors compares two behaviors using Jaccard similarity.
// It uses 40% weight on when-condition overlap and 60% weight on content similarity.
func (c *FallbackClient) CompareBehaviors(ctx context.Context, a, b *models.Behavior) (*ComparisonResult, error) {
	whenOverlap := computeWhenOverlap(a.When, b.When)
	contentSim := computeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
	similarity := whenOverlap*0.4 + contentSim*0.6

	return &ComparisonResult{
		SemanticSimilarity: similarity,
		IntentMatch:        similarity > 0.8,
		MergeCandidate:     similarity > 0.7,
		Reasoning:          "Rule-based comparison using Jaccard word similarity",
	}, nil
}

// MergeBehaviors combines multiple behaviors by concatenating their contents.
// This is a simple fallback that preserves all information without LLM synthesis.
func (c *FallbackClient) MergeBehaviors(ctx context.Context, behaviors []*models.Behavior) (*MergeResult, error) {
	if len(behaviors) == 0 {
		return &MergeResult{Merged: nil, SourceIDs: []string{}, Reasoning: "No behaviors to merge"}, nil
	}
	if len(behaviors) == 1 {
		return &MergeResult{Merged: behaviors[0], SourceIDs: []string{behaviors[0].ID}, Reasoning: "Single behavior, no merge needed"}, nil
	}

	sourceIDs := make([]string, len(behaviors))
	for i, b := range behaviors {
		sourceIDs[i] = b.ID
	}

	var canonicalParts, expandedParts []string
	for _, b := range behaviors {
		if b.Content.Canonical != "" {
			canonicalParts = append(canonicalParts, b.Content.Canonical)
		}
		if b.Content.Expanded != "" {
			expandedParts = append(expandedParts, b.Content.Expanded)
		}
	}

	mergedWhen := make(map[string]interface{})
	var maxConfidence float64
	var maxPriority int
	for _, b := range behaviors {
		for k, v := range b.When {
			mergedWhen[k] = v
		}
		if b.Confidence > maxConfidence {
			maxConfidence = b.Confidence
		}
		if b.Priority > maxPriority {
			maxPriority = b.Priority
		}
	}

	merged := &models.Behavior{
		ID:   behaviors[0].ID + "-merged",
		Name: behaviors[0].Name + " (merged)",
		Kind: behaviors[0].Kind,
		When: mergedWhen,
		Content: models.BehaviorContent{
			Canonical: strings.Join(canonicalParts, "\n\n"),
			Expanded:  strings.Join(expandedParts, "\n\n"),
		},
		Provenance: models.Provenance{SourceType: models.SourceTypeLearned, CreatedAt: time.Now()},
		Confidence: maxConfidence,
		Priority:   maxPriority,
	}

	return &MergeResult{Merged: merged, SourceIDs: sourceIDs, Reasoning: "Rule-based merge using content concatenation"}, nil
}

// Available returns false because this is a fallback client.
// This signals to selection logic that an LLM provider should be preferred.
func (c *FallbackClient) Available() bool {
	return false
}

// computeWhenOverlap calculates overlap between two when predicates.
// This mirrors the logic in learning/place.go.
func computeWhenOverlap(a, b map[string]interface{}) float64 {
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
			if valuesEqual(valueA, valueB) {
				matches += 2 // Count both sides as matched
			}
		}
	}

	if total == 0 {
		return 0.0
	}
	return float64(matches) / float64(total)
}

// computeContentSimilarity calculates Jaccard similarity between two strings.
// This mirrors the logic in learning/place.go.
func computeContentSimilarity(a, b string) float64 {
	wordsA := tokenize(a)
	wordsB := tokenize(b)

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

// tokenize splits a string into word tokens.
// This mirrors the logic in learning/place.go.
func tokenize(s string) []string {
	words := make([]string, 0)
	current := ""
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			current += string(r)
		} else if current != "" {
			words = append(words, current)
			current = ""
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

// valuesEqual compares two interface{} values for equality.
// This mirrors the logic in learning/place.go.
func valuesEqual(a, b interface{}) bool {
	// Handle string comparison
	aStr, aIsStr := a.(string)
	bStr, bIsStr := b.(string)
	if aIsStr && bIsStr {
		return aStr == bStr
	}

	// Handle slice comparison (both must contain at least one common element)
	aSlice, aIsSlice := a.([]interface{})
	bSlice, bIsSlice := b.([]interface{})
	if aIsSlice && bIsSlice {
		for _, av := range aSlice {
			for _, bv := range bSlice {
				if valuesEqual(av, bv) {
					return true
				}
			}
		}
		return false
	}

	// Handle string slice comparison
	aStrSlice, aIsStrSlice := a.([]string)
	bStrSlice, bIsStrSlice := b.([]string)
	if aIsStrSlice && bIsStrSlice {
		for _, av := range aStrSlice {
			for _, bv := range bStrSlice {
				if av == bv {
					return true
				}
			}
		}
		return false
	}

	// Fallback to direct equality
	return a == b
}
