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

// OptimizeWithTiers creates a tiered injection plan using ranking, scoring, and summarization
// This is the main entry point for intelligent token optimization with tiering
func (o *Optimizer) OptimizeWithTiers(behaviors []models.Behavior, ctx *models.ContextSnapshot) *models.InjectionPlan {
	// Import the tiering package's QuickAssign for a simple implementation
	// For more control, callers should use the tiering package directly
	return optimizeWithTiersInternal(behaviors, ctx, o.maxTokens)
}

// optimizeWithTiersInternal implements tiered optimization without circular imports
// This is a simplified version; for full control use the tiering package
func optimizeWithTiersInternal(behaviors []models.Behavior, ctx *models.ContextSnapshot, budget int) *models.InjectionPlan {
	if len(behaviors) == 0 {
		return &models.InjectionPlan{
			TokenBudget: budget,
		}
	}

	// Sort by importance (constraints first, then priority/confidence)
	sorted := make([]models.Behavior, len(behaviors))
	copy(sorted, behaviors)
	sortBehaviorsByImportance(sorted)

	// Budget allocation: 60% full, 30% summary, 10% overhead
	fullBudget := int(float64(budget) * 0.60)
	summaryBudget := int(float64(budget) * 0.30)

	plan := &models.InjectionPlan{
		TokenBudget:         budget,
		FullBehaviors:       make([]models.InjectedBehavior, 0),
		SummarizedBehaviors: make([]models.InjectedBehavior, 0),
		OmittedBehaviors:    make([]models.InjectedBehavior, 0),
	}

	fullTokensUsed := 0
	summaryTokensUsed := 0

	for i := range sorted {
		b := &sorted[i]
		tokenCost := estimateTokens(b.Content.Canonical) + 5

		// Constraints always get full tier
		if b.Kind == models.BehaviorKindConstraint {
			injected := models.InjectedBehavior{
				Behavior:  b,
				Tier:      models.TierFull,
				Content:   b.Content.Canonical,
				TokenCost: tokenCost,
				Score:     float64(b.Priority) * b.Confidence,
			}
			plan.FullBehaviors = append(plan.FullBehaviors, injected)
			fullTokensUsed += tokenCost
			continue
		}

		// Try full tier
		if fullTokensUsed+tokenCost <= fullBudget {
			injected := models.InjectedBehavior{
				Behavior:  b,
				Tier:      models.TierFull,
				Content:   b.Content.Canonical,
				TokenCost: tokenCost,
				Score:     float64(b.Priority) * b.Confidence,
			}
			plan.FullBehaviors = append(plan.FullBehaviors, injected)
			fullTokensUsed += tokenCost
		} else {
			// Try summary tier
			summary := getSummaryContent(b)
			summaryTokenCost := estimateTokens(summary) + 3

			if summaryTokensUsed+summaryTokenCost <= summaryBudget {
				injected := models.InjectedBehavior{
					Behavior:  b,
					Tier:      models.TierSummary,
					Content:   summary,
					TokenCost: summaryTokenCost,
					Score:     float64(b.Priority) * b.Confidence,
				}
				plan.SummarizedBehaviors = append(plan.SummarizedBehaviors, injected)
				summaryTokensUsed += summaryTokenCost
			} else {
				// Omit
				injected := models.InjectedBehavior{
					Behavior:  b,
					Tier:      models.TierOmitted,
					Content:   "",
					TokenCost: 0,
					Score:     float64(b.Priority) * b.Confidence,
				}
				plan.OmittedBehaviors = append(plan.OmittedBehaviors, injected)
			}
		}
	}

	plan.TotalTokens = fullTokensUsed + summaryTokensUsed

	return plan
}

// sortBehaviorsByImportance sorts behaviors for tiered optimization
func sortBehaviorsByImportance(behaviors []models.Behavior) {
	sort.Slice(behaviors, func(i, j int) bool {
		bi, bj := behaviors[i], behaviors[j]

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
	})
}

// getSummaryContent returns summary content for a behavior
func getSummaryContent(b *models.Behavior) string {
	if b.Content.Summary != "" {
		return b.Content.Summary
	}

	// Fallback: truncate canonical
	canonical := b.Content.Canonical
	if len(canonical) > 60 {
		// Try to truncate at word boundary
		truncated := canonical[:57]
		lastSpace := len(truncated) - 1
		for lastSpace > 30 && truncated[lastSpace] != ' ' {
			lastSpace--
		}
		if lastSpace > 30 {
			truncated = truncated[:lastSpace]
		}
		return truncated + "..."
	}
	return canonical
}
