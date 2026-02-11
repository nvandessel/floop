package learning

import (
	"context"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
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

		if similarity > 0.5 {
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
	if highestSimilarity > 0.9 && mostSimilar != nil {
		// Very high similarity - suggest merge
		decision.Action = "merge"
		decision.TargetID = mostSimilar.ID
		decision.Confidence = 0.5 // Lower confidence for merges (needs review)
	} else if highestSimilarity > 0.7 && mostSimilar != nil {
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
			b := NodeToBehavior(node)
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
// Uses canonical content combined with when-condition overlap.
func (p *graphPlacer) computeRuleBasedSimilarity(a, b *models.Behavior) float64 {
	score := 0.0

	// Check 'when' overlap (40% weight)
	whenOverlap := p.computeWhenOverlap(a.When, b.When)
	score += whenOverlap * 0.4

	// Check content similarity using Jaccard word overlap (60% weight)
	contentSim := p.computeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
	score += contentSim * 0.6

	return score
}

// computeWhenOverlap calculates overlap between two when predicates.
func (p *graphPlacer) computeWhenOverlap(a, b map[string]interface{}) float64 {
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
func (p *graphPlacer) computeContentSimilarity(a, b string) float64 {
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

// hasOverlappingConditions checks if a behavior's when conditions overlap with node content.
func (p *graphPlacer) hasOverlappingConditions(when map[string]interface{}, content map[string]interface{}) bool {
	existingWhen, ok := content["when"].(map[string]interface{})
	if !ok {
		// If the existing behavior has no when conditions, it applies everywhere
		// so there is overlap with any new behavior
		return len(when) == 0
	}

	// Check if any conditions match
	for key, value := range when {
		if existingValue, exists := existingWhen[key]; exists {
			if valuesEqual(value, existingValue) {
				return true
			}
		}
	}

	return false
}

// valuesEqual compares two interface{} values for equality.
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
		if similarity >= 0.5 && similarity < 0.9 {
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
func (p *graphPlacer) isMoreSpecific(a, b map[string]interface{}) bool {
	// a is more specific than b if:
	// 1. a has more conditions than b
	// 2. a includes all of b's conditions with the same values
	if len(a) <= len(b) {
		return false
	}

	for key, valueB := range b {
		valueA, exists := a[key]
		if !exists {
			return false
		}
		if !valuesEqual(valueA, valueB) {
			return false
		}
	}

	return true
}

// NodeToBehavior converts a store.Node to a models.Behavior.
// This is exported for use by CLI commands that need to load behaviors from the store.
func NodeToBehavior(node store.Node) models.Behavior {
	b := models.Behavior{
		ID: node.ID,
	}

	// Extract kind
	if kind, ok := node.Content["kind"].(string); ok {
		b.Kind = models.BehaviorKind(kind)
	}

	// Extract name
	if name, ok := node.Content["name"].(string); ok {
		b.Name = name
	}

	// Extract when conditions
	if when, ok := node.Content["when"].(map[string]interface{}); ok {
		b.When = when
	}

	// Extract content
	if content, ok := node.Content["content"].(map[string]interface{}); ok {
		if canonical, ok := content["canonical"].(string); ok {
			b.Content.Canonical = canonical
		}
		if expanded, ok := content["expanded"].(string); ok {
			b.Content.Expanded = expanded
		}
		if summary, ok := content["summary"].(string); ok {
			b.Content.Summary = summary
		}
		if structured, ok := content["structured"].(map[string]interface{}); ok {
			b.Content.Structured = structured
		}
		if tags, ok := content["tags"].([]interface{}); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					b.Content.Tags = append(b.Content.Tags, s)
				}
			}
		}
	} else if content, ok := node.Content["content"].(models.BehaviorContent); ok {
		b.Content = content
	}

	// Extract confidence from metadata
	if confidence, ok := node.Metadata["confidence"].(float64); ok {
		b.Confidence = confidence
	}

	// Extract priority from metadata
	if priority, ok := node.Metadata["priority"].(int); ok {
		b.Priority = priority
	}

	// Extract provenance from metadata
	if provenance, ok := node.Metadata["provenance"].(map[string]interface{}); ok {
		if sourceType, ok := provenance["source_type"].(string); ok {
			b.Provenance.SourceType = models.SourceType(sourceType)
		}
		if createdAt, ok := provenance["created_at"].(time.Time); ok {
			b.Provenance.CreatedAt = createdAt
		} else if createdAtStr, ok := provenance["created_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
				b.Provenance.CreatedAt = t
			}
		}
		if author, ok := provenance["author"].(string); ok {
			b.Provenance.Author = author
		}
	}

	// Extract stats from metadata
	if stats, ok := node.Metadata["stats"].(map[string]interface{}); ok {
		if activated, ok := stats["times_activated"].(int); ok {
			b.Stats.TimesActivated = activated
		}
		if followed, ok := stats["times_followed"].(int); ok {
			b.Stats.TimesFollowed = followed
		}
		if confirmed, ok := stats["times_confirmed"].(int); ok {
			b.Stats.TimesConfirmed = confirmed
		}
		if overridden, ok := stats["times_overridden"].(int); ok {
			b.Stats.TimesOverridden = overridden
		}
	}

	return b
}
