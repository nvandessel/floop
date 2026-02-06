package tiering

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// ActivationTierConfig maps activation levels to tiers.
type ActivationTierConfig struct {
	// FullThreshold: activation >= this gets TierFull. Default: 0.7.
	FullThreshold float64

	// SummaryThreshold: activation >= this gets TierSummary. Default: 0.3.
	SummaryThreshold float64

	// NameOnlyThreshold: activation >= this gets TierNameOnly. Default: 0.1.
	// Below this -> TierOmitted (filtered out by engine already).
	NameOnlyThreshold float64

	// ConstraintMinTier ensures constraints never go below this tier.
	// Default: TierSummary (constraints are safety-critical).
	ConstraintMinTier models.InjectionTier
}

// DefaultActivationTierConfig returns the default tier thresholds.
func DefaultActivationTierConfig() ActivationTierConfig {
	return ActivationTierConfig{
		FullThreshold:     0.7,
		SummaryThreshold:  0.3,
		NameOnlyThreshold: 0.1,
		ConstraintMinTier: models.TierSummary,
	}
}

// ActivationTierMapper maps spreading activation results to injection tiers.
type ActivationTierMapper struct {
	config ActivationTierConfig
}

// NewActivationTierMapper creates a new mapper with the given configuration.
func NewActivationTierMapper(config ActivationTierConfig) *ActivationTierMapper {
	return &ActivationTierMapper{config: config}
}

// MapTier returns the appropriate tier for a given activation level and behavior kind.
func (m *ActivationTierMapper) MapTier(activation float64, kind models.BehaviorKind) models.InjectionTier {
	tier := models.TierOmitted

	if activation >= m.config.FullThreshold {
		tier = models.TierFull
	} else if activation >= m.config.SummaryThreshold {
		tier = models.TierSummary
	} else if activation >= m.config.NameOnlyThreshold {
		tier = models.TierNameOnly
	}

	// Enforce constraint minimum tier
	if kind == models.BehaviorKindConstraint && tier > m.config.ConstraintMinTier {
		tier = m.config.ConstraintMinTier
	}

	return tier
}

// tierEntry is an internal bookkeeping record used during budget demotion.
type tierEntry struct {
	result   spreading.Result
	behavior *models.Behavior
	tier     models.InjectionTier
	tokens   int
}

// MapResults converts spreading activation results into an InjectionPlan.
// It respects both activation-based tiers AND token budget as a hard ceiling.
func (m *ActivationTierMapper) MapResults(
	results []spreading.Result,
	behaviors map[string]*models.Behavior,
	tokenBudget int,
) *models.InjectionPlan {
	plan := &models.InjectionPlan{
		TokenBudget:         tokenBudget,
		FullBehaviors:       make([]models.InjectedBehavior, 0),
		SummarizedBehaviors: make([]models.InjectedBehavior, 0),
		NameOnlyBehaviors:   make([]models.InjectedBehavior, 0),
		OmittedBehaviors:    make([]models.InjectedBehavior, 0),
	}

	if len(results) == 0 {
		return plan
	}

	// Step 1: Build entries with initial tier assignments and token costs.
	entries := make([]tierEntry, 0, len(results))
	for _, r := range results {
		b, ok := behaviors[r.BehaviorID]
		if !ok || b == nil {
			continue
		}
		tier := m.MapTier(r.Activation, b.Kind)
		tokens := estimateTokensForTier(b, tier)
		entries = append(entries, tierEntry{
			result:   r,
			behavior: b,
			tier:     tier,
			tokens:   tokens,
		})
	}

	// Step 2: Check total tokens against budget and demote if necessary.
	totalTokens := sumTokens(entries)
	if totalTokens > tokenBudget {
		// Sort by activation ascending so we demote lowest first.
		sort.SliceStable(entries, func(i, j int) bool {
			return entries[i].result.Activation < entries[j].result.Activation
		})

		for totalTokens > tokenBudget {
			demoted := false
			for i := range entries {
				if entries[i].tier == models.TierOmitted {
					continue
				}
				// Never demote constraints below ConstraintMinTier.
				if entries[i].behavior.Kind == models.BehaviorKindConstraint &&
					entries[i].tier >= m.config.ConstraintMinTier {
					continue
				}
				// Demote one level.
				newTier := entries[i].tier + 1
				newTokens := estimateTokensForTier(entries[i].behavior, newTier)
				totalTokens -= entries[i].tokens - newTokens
				entries[i].tier = newTier
				entries[i].tokens = newTokens
				demoted = true
				if totalTokens <= tokenBudget {
					break
				}
			}
			if !demoted {
				break // Cannot demote further; budget exceeded by constraints.
			}
		}
	}

	// Step 3: Build the InjectionPlan from the final entries.
	for _, e := range entries {
		ib := models.InjectedBehavior{
			Behavior:  e.behavior,
			Tier:      e.tier,
			Content:   contentForTier(e.behavior, e.tier),
			TokenCost: e.tokens,
			Score:     e.result.Activation,
		}
		switch e.tier {
		case models.TierFull:
			plan.FullBehaviors = append(plan.FullBehaviors, ib)
		case models.TierSummary:
			plan.SummarizedBehaviors = append(plan.SummarizedBehaviors, ib)
		case models.TierNameOnly:
			plan.NameOnlyBehaviors = append(plan.NameOnlyBehaviors, ib)
		case models.TierOmitted:
			plan.OmittedBehaviors = append(plan.OmittedBehaviors, ib)
		}
	}

	plan.TotalTokens = sumPlanTokens(plan)

	return plan
}

// estimateTokensForTier estimates the token cost for a behavior at a given tier.
func estimateTokensForTier(b *models.Behavior, tier models.InjectionTier) int {
	content := contentForTier(b, tier)
	if content == "" {
		return 0
	}
	// Rough estimate: 1 token ~ 4 characters.
	return (len(content) + 3) / 4
}

// contentForTier returns the content string for a behavior at a given tier.
func contentForTier(b *models.Behavior, tier models.InjectionTier) string {
	switch tier {
	case models.TierFull:
		return b.Content.Canonical
	case models.TierSummary:
		if b.Content.Summary != "" {
			return b.Content.Summary
		}
		// Fallback: truncate canonical.
		canonical := b.Content.Canonical
		if len(canonical) > 60 {
			return canonical[:57] + "..."
		}
		return canonical
	case models.TierNameOnly:
		return formatNameOnly(b)
	default:
		return ""
	}
}

// formatNameOnly produces a compact name-only representation of a behavior.
// Format: `{name}` [{kind}] #tag1 #tag2
func formatNameOnly(b *models.Behavior) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("`%s` [%s]", b.Name, b.Kind))

	for _, tag := range b.Content.Tags {
		sb.WriteString(fmt.Sprintf(" #%s", tag))
	}

	return sb.String()
}

// sumTokens returns the total token cost across all entries.
func sumTokens(entries []tierEntry) int {
	total := 0
	for _, e := range entries {
		total += e.tokens
	}
	return total
}

// sumPlanTokens returns the total token cost of included behaviors in a plan.
func sumPlanTokens(plan *models.InjectionPlan) int {
	total := 0
	for _, ib := range plan.FullBehaviors {
		total += ib.TokenCost
	}
	for _, ib := range plan.SummarizedBehaviors {
		total += ib.TokenCost
	}
	for _, ib := range plan.NameOnlyBehaviors {
		total += ib.TokenCost
	}
	return total
}
