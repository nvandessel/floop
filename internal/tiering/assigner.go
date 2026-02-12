package tiering

import (
	"sort"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/summarization"
	"github.com/nvandessel/feedback-loop/internal/tokens"
)

// TierAssignerConfig configures the tier assigner
type TierAssignerConfig struct {
	// FullTierPercent is the percentage of budget for full-tier behaviors (default: 0.6)
	FullTierPercent float64

	// SummaryTierPercent is the percentage of budget for summary-tier behaviors (default: 0.3)
	SummaryTierPercent float64

	// OverheadPercent is reserved for formatting overhead (default: 0.1)
	OverheadPercent float64

	// MinFullBehaviors is the minimum number of behaviors at full tier
	MinFullBehaviors int

	// ConstraintsAlwaysFull ensures constraints always get full tier
	ConstraintsAlwaysFull bool
}

// DefaultTierAssignerConfig returns the default tier assigner configuration
func DefaultTierAssignerConfig() TierAssignerConfig {
	return TierAssignerConfig{
		FullTierPercent:       0.60,
		SummaryTierPercent:    0.30,
		OverheadPercent:       0.10,
		MinFullBehaviors:      3,
		ConstraintsAlwaysFull: true,
	}
}

// TierAssigner assigns injection tiers to behaviors based on budget
type TierAssigner struct {
	config     TierAssignerConfig
	scorer     *ranking.RelevanceScorer
	summarizer summarization.Summarizer
}

// NewTierAssigner creates a new tier assigner
func NewTierAssigner(config TierAssignerConfig, scorer *ranking.RelevanceScorer, summarizer summarization.Summarizer) *TierAssigner {
	// Validate and normalize percentages
	total := config.FullTierPercent + config.SummaryTierPercent + config.OverheadPercent
	if total > 0 && total != 1.0 {
		config.FullTierPercent /= total
		config.SummaryTierPercent /= total
		config.OverheadPercent /= total
	}

	if config.MinFullBehaviors < 0 {
		config.MinFullBehaviors = 0
	}

	return &TierAssigner{
		config:     config,
		scorer:     scorer,
		summarizer: summarizer,
	}
}

// AssignTiers creates an injection plan for the given behaviors within the token budget
func (a *TierAssigner) AssignTiers(behaviors []models.Behavior, ctx *models.ContextSnapshot, tokenBudget int) *models.InjectionPlan {
	if len(behaviors) == 0 {
		return &models.InjectionPlan{
			TokenBudget: tokenBudget,
		}
	}

	// Score all behaviors
	scored := a.scorer.ScoreBatch(behaviors, ctx)

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Calculate budget tiers
	fullBudget := int(float64(tokenBudget) * a.config.FullTierPercent)
	summaryBudget := int(float64(tokenBudget) * a.config.SummaryTierPercent)

	plan := &models.InjectionPlan{
		TokenBudget:         tokenBudget,
		FullBehaviors:       make([]models.InjectedBehavior, 0),
		SummarizedBehaviors: make([]models.InjectedBehavior, 0),
		OmittedBehaviors:    make([]models.InjectedBehavior, 0),
	}

	fullTokensUsed := 0
	summaryTokensUsed := 0

	// First pass: assign constraints to full tier (if configured)
	if a.config.ConstraintsAlwaysFull {
		for i, s := range scored {
			if s.Behavior.Kind != models.BehaviorKindConstraint {
				continue
			}
			tokenCost := a.estimateTokens(s.Behavior.Content.Canonical)
			injected := models.InjectedBehavior{
				Behavior:  s.Behavior,
				Tier:      models.TierFull,
				Content:   s.Behavior.Content.Canonical,
				TokenCost: tokenCost,
				Score:     s.Score,
			}
			plan.FullBehaviors = append(plan.FullBehaviors, injected)
			fullTokensUsed += tokenCost
			// Mark as processed
			scored[i].Score = -1
		}
	}

	// Second pass: fill remaining full budget
	for _, s := range scored {
		if s.Score < 0 {
			continue // Already processed
		}

		tokenCost := a.estimateTokens(s.Behavior.Content.Canonical)
		if fullTokensUsed+tokenCost <= fullBudget || len(plan.FullBehaviors) < a.config.MinFullBehaviors {
			injected := models.InjectedBehavior{
				Behavior:  s.Behavior,
				Tier:      models.TierFull,
				Content:   s.Behavior.Content.Canonical,
				TokenCost: tokenCost,
				Score:     s.Score,
			}
			plan.FullBehaviors = append(plan.FullBehaviors, injected)
			fullTokensUsed += tokenCost
		} else {
			// Try to fit as summary
			summary := a.getSummary(s.Behavior)
			summaryTokenCost := a.estimateTokens(summary)

			if summaryTokensUsed+summaryTokenCost <= summaryBudget {
				injected := models.InjectedBehavior{
					Behavior:  s.Behavior,
					Tier:      models.TierSummary,
					Content:   summary,
					TokenCost: summaryTokenCost,
					Score:     s.Score,
				}
				plan.SummarizedBehaviors = append(plan.SummarizedBehaviors, injected)
				summaryTokensUsed += summaryTokenCost
			} else {
				// Omit entirely
				injected := models.InjectedBehavior{
					Behavior:  s.Behavior,
					Tier:      models.TierOmitted,
					Content:   "",
					TokenCost: 0,
					Score:     s.Score,
				}
				plan.OmittedBehaviors = append(plan.OmittedBehaviors, injected)
			}
		}
	}

	plan.TotalTokens = fullTokensUsed + summaryTokensUsed

	return plan
}

// getSummary retrieves or generates a summary for a behavior
func (a *TierAssigner) getSummary(behavior *models.Behavior) string {
	// Use existing summary if available
	if behavior.Content.Summary != "" {
		return behavior.Content.Summary
	}

	// Generate summary using summarizer
	if a.summarizer != nil {
		summary, err := a.summarizer.Summarize(behavior)
		if err == nil && summary != "" {
			return summary
		}
	}

	// Fallback: truncate canonical content
	canonical := behavior.Content.Canonical
	if len(canonical) > 60 {
		return canonical[:57] + "..."
	}
	return canonical
}

// estimateTokens estimates token count for text
func (a *TierAssigner) estimateTokens(text string) int {
	return tokens.EstimateTokens(text)
}

// QuickAssign creates a tiered injection plan using the canonical ActivationTierMapper
// path. Behaviors are scored with the default relevance scorer, converted via bridge,
// and tiered using activation thresholds with budget demotion.
func QuickAssign(behaviors []models.Behavior, tokenBudget int) *models.InjectionPlan {
	results, behaviorMap := BehaviorsToResults(behaviors)
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())
	return mapper.MapResults(results, behaviorMap, tokenBudget)
}
