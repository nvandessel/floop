package assembly

import (
	"sort"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// Optimizer handles token budget management for behavior compilation
type Optimizer struct {
	maxTokens int
}

// OptimizationResult contains the result of token optimization
type OptimizationResult struct {
	// Behaviors that fit within the token budget
	Included []models.Behavior `json:"included"`

	// Behaviors excluded due to token limits
	Excluded []models.Behavior `json:"excluded"`

	// Tokens used by included behaviors
	TokensUsed int `json:"tokens_used"`

	// Token budget
	TokensAvailable int `json:"tokens_available"`

	// Whether any behaviors were excluded
	Truncated bool `json:"truncated"`
}

// NewOptimizer creates a new token optimizer
func NewOptimizer(maxTokens int) *Optimizer {
	return &Optimizer{
		maxTokens: maxTokens,
	}
}

// Optimize selects behaviors that fit within the token budget
// Prioritizes by: constraints first, then by priority, then by confidence
func (o *Optimizer) Optimize(behaviors []models.Behavior) OptimizationResult {
	if o.maxTokens <= 0 {
		// No limit - include all
		return OptimizationResult{
			Included:        behaviors,
			Excluded:        nil,
			TokensUsed:      o.estimateTotalTokens(behaviors),
			TokensAvailable: 0,
			Truncated:       false,
		}
	}

	// Sort behaviors by importance
	sorted := make([]models.Behavior, len(behaviors))
	copy(sorted, behaviors)
	o.sortByImportance(sorted)

	var included []models.Behavior
	var excluded []models.Behavior
	tokensUsed := 0

	// Estimate overhead for formatting (headers, etc.)
	overhead := 50 // Rough estimate for markdown headers

	for _, b := range sorted {
		tokenCost := o.estimateBehaviorTokens(b)

		if tokensUsed+tokenCost+overhead <= o.maxTokens {
			included = append(included, b)
			tokensUsed += tokenCost
		} else {
			excluded = append(excluded, b)
		}
	}

	return OptimizationResult{
		Included:        included,
		Excluded:        excluded,
		TokensUsed:      tokensUsed,
		TokensAvailable: o.maxTokens,
		Truncated:       len(excluded) > 0,
	}
}

// sortByImportance orders behaviors by importance for inclusion
func (o *Optimizer) sortByImportance(behaviors []models.Behavior) {
	sort.Slice(behaviors, func(i, j int) bool {
		bi, bj := behaviors[i], behaviors[j]

		// Constraints always come first (most important for safety)
		if bi.Kind == models.BehaviorKindConstraint && bj.Kind != models.BehaviorKindConstraint {
			return true
		}
		if bj.Kind == models.BehaviorKindConstraint && bi.Kind != models.BehaviorKindConstraint {
			return false
		}

		// Higher priority wins
		if bi.Priority != bj.Priority {
			return bi.Priority > bj.Priority
		}

		// Higher confidence wins
		if bi.Confidence != bj.Confidence {
			return bi.Confidence > bj.Confidence
		}

		// Directives before preferences
		kindOrder := map[models.BehaviorKind]int{
			models.BehaviorKindConstraint: 0,
			models.BehaviorKindDirective:  1,
			models.BehaviorKindPreference: 2,
			models.BehaviorKindProcedure:  3,
		}
		return kindOrder[bi.Kind] < kindOrder[bj.Kind]
	})
}

// estimateBehaviorTokens estimates tokens for a single behavior
func (o *Optimizer) estimateBehaviorTokens(b models.Behavior) int {
	// Use canonical content for estimation
	text := b.Content.Canonical
	if text == "" {
		return 0
	}

	// Add some overhead for formatting (bullet point, newlines)
	return estimateTokens(text) + 5
}

// estimateTotalTokens estimates total tokens for all behaviors
func (o *Optimizer) estimateTotalTokens(behaviors []models.Behavior) int {
	total := 0
	for _, b := range behaviors {
		total += o.estimateBehaviorTokens(b)
	}
	// Add formatting overhead
	return total + 50
}

// OptimizeWithPriorities allows custom priority ordering
func (o *Optimizer) OptimizeWithPriorities(behaviors []models.Behavior, priorityOrder []string) OptimizationResult {
	// Build priority map from order
	priorityMap := make(map[string]int)
	for i, id := range priorityOrder {
		priorityMap[id] = len(priorityOrder) - i // Higher priority for earlier items
	}

	// Sort by custom priority
	sorted := make([]models.Behavior, len(behaviors))
	copy(sorted, behaviors)

	sort.Slice(sorted, func(i, j int) bool {
		pi := priorityMap[sorted[i].ID]
		pj := priorityMap[sorted[j].ID]

		// If in priority list, use that order
		if pi != 0 || pj != 0 {
			return pi > pj
		}

		// Fall back to default importance
		return o.compareImportance(sorted[i], sorted[j])
	})

	// Use standard optimization with sorted list
	var included []models.Behavior
	var excluded []models.Behavior
	tokensUsed := 0
	overhead := 50

	for _, b := range sorted {
		tokenCost := o.estimateBehaviorTokens(b)

		if o.maxTokens <= 0 || tokensUsed+tokenCost+overhead <= o.maxTokens {
			included = append(included, b)
			tokensUsed += tokenCost
		} else {
			excluded = append(excluded, b)
		}
	}

	return OptimizationResult{
		Included:        included,
		Excluded:        excluded,
		TokensUsed:      tokensUsed,
		TokensAvailable: o.maxTokens,
		Truncated:       len(excluded) > 0,
	}
}

// compareImportance compares two behaviors by importance (for sorting)
func (o *Optimizer) compareImportance(bi, bj models.Behavior) bool {
	// Constraints first
	if bi.Kind == models.BehaviorKindConstraint && bj.Kind != models.BehaviorKindConstraint {
		return true
	}
	if bj.Kind == models.BehaviorKindConstraint && bi.Kind != models.BehaviorKindConstraint {
		return false
	}

	// Higher priority
	if bi.Priority != bj.Priority {
		return bi.Priority > bj.Priority
	}

	// Higher confidence
	return bi.Confidence > bj.Confidence
}

