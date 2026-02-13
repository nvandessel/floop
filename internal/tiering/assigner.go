package tiering

import (
	"github.com/nvandessel/feedback-loop/internal/models"
)

// QuickAssign creates a tiered injection plan using the canonical ActivationTierMapper
// path. Behaviors are scored with the default relevance scorer, converted via bridge,
// and tiered using activation thresholds with budget demotion.
func QuickAssign(behaviors []models.Behavior, tokenBudget int) *models.InjectionPlan {
	results, behaviorMap := BehaviorsToResults(behaviors)
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())
	return mapper.MapResults(results, behaviorMap, tokenBudget)
}
