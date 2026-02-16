package llm

import (
	"context"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/similarity"
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
	whenOverlap := similarity.ComputeWhenOverlap(a.When, b.When)
	contentSim := similarity.ComputeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
	sim := similarity.WeightedScore(whenOverlap, contentSim)

	return &ComparisonResult{
		SemanticSimilarity: sim,
		IntentMatch:        sim > 0.8,
		MergeCandidate:     sim > 0.7,
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

	first := behaviors[0]
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
		ID:   first.ID + "-merged",
		Name: first.Name + " (merged)",
		Kind: first.Kind,
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
