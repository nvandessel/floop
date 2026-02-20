package learning

import (
	"context"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/similarity"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// GraphPlacerConfig configures optional LLM-based similarity for GraphPlacer.
type GraphPlacerConfig struct {
	// LLMClient is the optional LLM client for semantic comparison.
	LLMClient llm.Client

	// UseLLMForSimilarity enables LLM-based semantic comparison.
	UseLLMForSimilarity bool

	// LLMSimilarityThreshold is the minimum rule-based score required
	// before invoking LLM for semantic comparison. Default: 0.5
	LLMSimilarityThreshold float64
}

// DefaultGraphPlacerConfig returns a GraphPlacerConfig with sensible defaults.
func DefaultGraphPlacerConfig() *GraphPlacerConfig {
	return &GraphPlacerConfig{
		LLMClient:              nil,
		UseLLMForSimilarity:    false,
		LLMSimilarityThreshold: 0.5,
	}
}

// PlacementDecision describes where a new behavior should go in the graph.
type PlacementDecision struct {
	// Action indicates what to do: "create", "merge", or "specialize"
	Action string

	// TargetID is set for merge/specialize actions to indicate the existing behavior
	TargetID string

	// ProposedEdges are the edges to add when placing the behavior
	ProposedEdges []ProposedEdge

	// SimilarBehaviors lists existing behaviors that are similar
	SimilarBehaviors []SimilarityMatch

	// Confidence indicates how confident the placer is in this decision (0.0-1.0)
	Confidence float64
}

// ProposedEdge represents a proposed edge to add to the graph.
type ProposedEdge struct {
	From string
	To   string
	Kind string // "requires", "overrides", "conflicts", "similar-to"
}

// SimilarityMatch represents a similar existing behavior.
type SimilarityMatch struct {
	ID    string
	Score float64
}

// GraphPlacer determines where a new behavior fits in the graph.
// It analyzes existing behaviors to find relationships and detect
// potential duplicates or merge opportunities.
type GraphPlacer interface {
	// Place determines where a behavior should be placed in the graph.
	// It returns a PlacementDecision indicating whether to create a new
	// behavior, merge with an existing one, or specialize an existing one.
	Place(ctx context.Context, behavior *models.Behavior) (*PlacementDecision, error)
}

// graphPlacer is the concrete implementation of GraphPlacer.
type graphPlacer struct {
	store  store.GraphStore
	config *GraphPlacerConfig
}

// NewGraphPlacer creates a new GraphPlacer with the given store and no LLM support.
func NewGraphPlacer(s store.GraphStore) GraphPlacer {
	return &graphPlacer{store: s, config: nil}
}

// NewGraphPlacerWithConfig creates a new GraphPlacer with optional LLM-based similarity.
func NewGraphPlacerWithConfig(s store.GraphStore, cfg *GraphPlacerConfig) GraphPlacer {
	return &graphPlacer{store: s, config: cfg}
}

// Place determines where a behavior should be placed in the graph.
func (p *graphPlacer) Place(ctx context.Context, behavior *models.Behavior) (*PlacementDecision, error) {
	decision := &PlacementDecision{
		Action:           "create",
		ProposedEdges:    make([]ProposedEdge, 0),
		SimilarBehaviors: make([]SimilarityMatch, 0),
		Confidence:       0.7, // Default confidence for new behaviors
	}

	// Find existing behaviors with overlapping 'when' conditions
	existingBehaviors, err := p.findRelatedBehaviors(ctx, behavior)
	if err != nil {
		return nil, err
	}

	// If no existing behaviors, create with high confidence
	if len(existingBehaviors) == 0 {
		decision.Confidence = 0.9
		return decision, nil
	}

	// Track the most similar behavior for potential merge
	var mostSimilar *models.Behavior
	var highestSimilarity float64

	// Check for high similarity (potential duplicates or merges)
	for i := range existingBehaviors {
		existing := &existingBehaviors[i]
		similarity := p.computeSimilarity(ctx, behavior, existing)

		if similarity > constants.SimilarToThreshold {
			decision.SimilarBehaviors = append(decision.SimilarBehaviors, SimilarityMatch{
				ID:    existing.ID,
				Score: similarity,
			})
		}

		if similarity > highestSimilarity {
			highestSimilarity = similarity
			mostSimilar = existing
		}
	}

	// Decide action based on similarity
	if highestSimilarity > constants.SimilarToUpperBound && mostSimilar != nil {
		// Very high similarity - suggest merge
		decision.Action = "merge"
		decision.TargetID = mostSimilar.ID
		decision.Confidence = 0.5 // Lower confidence for merges (needs review)
	} else if highestSimilarity > constants.SpecializeThreshold && mostSimilar != nil {
		// High similarity but not duplicate - check if we should specialize
		if p.isMoreSpecific(behavior.When, mostSimilar.When) {
			decision.Action = "specialize"
			decision.TargetID = mostSimilar.ID
			decision.Confidence = 0.6
		}
	}

	// Determine edges based on relationships with existing behaviors
	decision.ProposedEdges = p.determineEdges(ctx, behavior, existingBehaviors)

	return decision, nil
}

// findRelatedBehaviors finds behaviors with overlapping activation conditions.
func (p *graphPlacer) findRelatedBehaviors(ctx context.Context, behavior *models.Behavior) ([]models.Behavior, error) {
	// Query for all behavior nodes
	nodes, err := p.store.QueryNodes(ctx, map[string]interface{}{
		"kind": "behavior",
	})
	if err != nil {
		return nil, err
	}

	related := make([]models.Behavior, 0)
	for _, node := range nodes {
		// Skip self if somehow present
		if node.ID == behavior.ID {
			continue
		}

		// Check for overlapping conditions
		if p.hasOverlappingConditions(behavior.When, node.Content) {
			b := models.NodeToBehavior(node)
			related = append(related, b)
		}
	}

	return related, nil
}

// computeSimilarity calculates similarity between two behaviors.
// Uses rule-based Jaccard similarity, optionally enhanced with LLM semantic comparison.
func (p *graphPlacer) computeSimilarity(ctx context.Context, a, b *models.Behavior) float64 {
	// First compute fast rule-based score
	ruleScore := p.computeRuleBasedSimilarity(a, b)

	// Check if we should enhance with LLM
	if !p.shouldUseLLM(ruleScore) {
		return ruleScore
	}

	// Try LLM-based semantic comparison
	result, err := p.config.LLMClient.CompareBehaviors(ctx, a, b)
	if err != nil {
		// Fallback to rule-based on error
		return ruleScore
	}

	// Blend scores: 30% rule-based + 70% semantic
	return (ruleScore * 0.3) + (result.SemanticSimilarity * 0.7)
}

// shouldUseLLM determines if LLM should be used for similarity comparison.
func (p *graphPlacer) shouldUseLLM(ruleScore float64) bool {
	if p.config == nil {
		return false
	}
	if !p.config.UseLLMForSimilarity {
		return false
	}
	if p.config.LLMClient == nil || !p.config.LLMClient.Available() {
		return false
	}
	return ruleScore > p.config.LLMSimilarityThreshold
}

// computeRuleBasedSimilarity calculates similarity using Jaccard word overlap.
// Uses canonical content combined with when-condition overlap and tag similarity.
// Missing signals (empty when, no tags) get -1.0 sentinel so their weight
// is redistributed to present signals.
func (p *graphPlacer) computeRuleBasedSimilarity(a, b *models.Behavior) float64 {
	whenOverlap := similarity.ComputeWhenOverlap(a.When, b.When)
	contentSim := similarity.ComputeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
	tagSim := similarity.ComputeTagSimilarity(a.Content.Tags, b.Content.Tags)
	return similarity.WeightedScoreWithTags(whenOverlap, contentSim, tagSim)
}

// hasOverlappingConditions checks if a behavior's when conditions overlap with node content.
// Returns true when either side has empty/missing when conditions (they could still
// be similar via content or tags), or when any condition values match.
func (p *graphPlacer) hasOverlappingConditions(when map[string]interface{}, content map[string]interface{}) bool {
	existingWhen, ok := content["when"].(map[string]interface{})
	if !ok {
		// No when in content: always consider for comparison
		return true
	}

	if len(when) == 0 || len(existingWhen) == 0 {
		// Either side is unscoped: always consider for comparison
		return true
	}

	// Check if any conditions match
	for key, value := range when {
		if existingValue, exists := existingWhen[key]; exists {
			if similarity.ValuesEqual(value, existingValue) {
				return true
			}
		}
	}

	return false
}

// determineEdges proposes edges for the new behavior based on relationships with existing behaviors.
func (p *graphPlacer) determineEdges(ctx context.Context, behavior *models.Behavior, existing []models.Behavior) []ProposedEdge {
	edges := make([]ProposedEdge, 0)

	for _, e := range existing {
		// If new behavior has more specific 'when' conditions, it overrides the existing one
		if p.isMoreSpecific(behavior.When, e.When) {
			edges = append(edges, ProposedEdge{
				From: behavior.ID,
				To:   e.ID,
				Kind: "overrides",
			})
		}

		// If existing behavior has more specific 'when' conditions,
		// the new behavior might be overridden by it (inverse relationship)
		if p.isMoreSpecific(e.When, behavior.When) {
			edges = append(edges, ProposedEdge{
				From: e.ID,
				To:   behavior.ID,
				Kind: "overrides",
			})
		}

		// Add similar-to edges for behaviors with moderate similarity
		similarity := p.computeSimilarity(ctx, behavior, &e)
		if similarity >= constants.SimilarToThreshold && similarity < constants.SimilarToUpperBound {
			edges = append(edges, ProposedEdge{
				From: behavior.ID,
				To:   e.ID,
				Kind: "similar-to",
			})
		}
	}

	return edges
}

// isMoreSpecific returns true if a has all of b's conditions plus additional ones.
// Delegates to the public similarity.IsMoreSpecific for reuse by other packages.
func (p *graphPlacer) isMoreSpecific(a, b map[string]interface{}) bool {
	return similarity.IsMoreSpecific(a, b)
}
