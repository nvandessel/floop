package tiering

import (
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// ScoredBehaviorsToResults converts RelevanceScorer output into the format
// expected by ActivationTierMapper.MapResults. The Score field maps directly
// to spreading.Result.Activation, enabling the tier mapper's threshold-based
// tier assignment and budget demotion logic.
func ScoredBehaviorsToResults(scored []ranking.ScoredBehavior) ([]spreading.Result, map[string]*models.Behavior) {
	results := make([]spreading.Result, 0, len(scored))
	behaviorMap := make(map[string]*models.Behavior, len(scored))

	for _, s := range scored {
		if s.Behavior == nil {
			continue
		}
		results = append(results, spreading.Result{
			BehaviorID: s.Behavior.ID,
			Activation: s.Score,
		})
		behaviorMap[s.Behavior.ID] = s.Behavior
	}

	return results, behaviorMap
}

// BehaviorsToResults scores a slice of behaviors using the default relevance
// scorer and converts them into ActivationTierMapper input format. This is
// the convenience path used by QuickAssign and other callers that start with
// []models.Behavior rather than pre-scored results.
func BehaviorsToResults(behaviors []models.Behavior) ([]spreading.Result, map[string]*models.Behavior) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	scored := scorer.ScoreBatch(behaviors, nil)
	return ScoredBehaviorsToResults(scored)
}
