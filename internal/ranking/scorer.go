package ranking

import (
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// ScorerConfig configures the relevance scorer
type ScorerConfig struct {
	// Weight for context match specificity (0.0-1.0)
	ContextWeight float64

	// Weight for usage signals (followed/activated ratio) (0.0-1.0)
	UsageWeight float64

	// Weight for recency (0.0-1.0)
	RecencyWeight float64

	// Weight for confidence score (0.0-1.0)
	ConfidenceWeight float64

	// Weight for priority and kind (0.0-1.0)
	PriorityWeight float64

	// RecencyHalfLife is the time after which recency score is halved
	RecencyHalfLife time.Duration

	// KindBoosts are score multipliers for behavior kinds
	KindBoosts map[models.BehaviorKind]float64
}

// DefaultScorerConfig returns the default scoring configuration
// Weights: Context 25%, Usage 25%, Recency 15%, Confidence 15%, Priority 20%
func DefaultScorerConfig() ScorerConfig {
	return ScorerConfig{
		ContextWeight:    0.25,
		UsageWeight:      0.25,
		RecencyWeight:    0.15,
		ConfidenceWeight: 0.15,
		PriorityWeight:   0.20,
		RecencyHalfLife:  7 * 24 * time.Hour, // 1 week
		KindBoosts: map[models.BehaviorKind]float64{
			models.BehaviorKindConstraint: 2.0, // Constraints are safety-critical
			models.BehaviorKindDirective:  1.5,
			models.BehaviorKindProcedure:  1.2,
			models.BehaviorKindPreference: 1.0,
		},
	}
}

// RelevanceScorer calculates relevance scores for behaviors
type RelevanceScorer struct {
	config ScorerConfig
}

// NewRelevanceScorer creates a new relevance scorer with the given config
func NewRelevanceScorer(config ScorerConfig) *RelevanceScorer {
	// Validate and normalize weights
	totalWeight := config.ContextWeight + config.UsageWeight + config.RecencyWeight +
		config.ConfidenceWeight + config.PriorityWeight

	if totalWeight > 0 && totalWeight != 1.0 {
		// Normalize weights to sum to 1.0
		config.ContextWeight /= totalWeight
		config.UsageWeight /= totalWeight
		config.RecencyWeight /= totalWeight
		config.ConfidenceWeight /= totalWeight
		config.PriorityWeight /= totalWeight
	}

	if config.RecencyHalfLife <= 0 {
		config.RecencyHalfLife = 7 * 24 * time.Hour
	}

	if config.KindBoosts == nil {
		config.KindBoosts = DefaultScorerConfig().KindBoosts
	}

	return &RelevanceScorer{config: config}
}

// ScoredBehavior represents a behavior with its calculated relevance score
type ScoredBehavior struct {
	Behavior *models.Behavior
	Score    float64

	// Component scores for debugging/transparency
	ContextScore    float64
	UsageScore      float64
	RecencyScore    float64
	ConfidenceScore float64
	PriorityScore   float64
	KindBoost       float64
}

// Score calculates the relevance score for a single behavior
func (s *RelevanceScorer) Score(behavior *models.Behavior, ctx *models.ContextSnapshot) ScoredBehavior {
	if behavior == nil {
		return ScoredBehavior{}
	}

	scored := ScoredBehavior{
		Behavior:        behavior,
		ContextScore:    s.contextScore(behavior, ctx),
		UsageScore:      s.usageScore(behavior),
		RecencyScore:    s.recencyScore(behavior),
		ConfidenceScore: s.confidenceScore(behavior),
		PriorityScore:   s.priorityScore(behavior),
		KindBoost:       s.kindBoost(behavior.Kind),
	}

	// Calculate weighted score
	baseScore := scored.ContextScore*s.config.ContextWeight +
		scored.UsageScore*s.config.UsageWeight +
		scored.RecencyScore*s.config.RecencyWeight +
		scored.ConfidenceScore*s.config.ConfidenceWeight +
		scored.PriorityScore*s.config.PriorityWeight

	// Apply kind boost
	scored.Score = baseScore * scored.KindBoost

	return scored
}

// ScoreBatch calculates relevance scores for multiple behaviors
func (s *RelevanceScorer) ScoreBatch(behaviors []models.Behavior, ctx *models.ContextSnapshot) []ScoredBehavior {
	results := make([]ScoredBehavior, len(behaviors))
	for i := range behaviors {
		results[i] = s.Score(&behaviors[i], ctx)
	}
	return results
}

// contextScore calculates how specifically the behavior matches the context
func (s *RelevanceScorer) contextScore(behavior *models.Behavior, ctx *models.ContextSnapshot) float64 {
	if behavior.When == nil || len(behavior.When) == 0 {
		// Global behaviors (no when predicate) get a base score
		return 0.5
	}

	// More specific predicates = higher score
	// Count matching predicates
	matches := 0
	total := len(behavior.When)

	for key := range behavior.When {
		if ctx != nil && s.predicateMatches(key, ctx) {
			matches++
		}
	}

	if total == 0 {
		return 0.5
	}

	// Specificity bonus: more predicates = more specific
	specificityBonus := float64(total) * 0.1
	if specificityBonus > 0.3 {
		specificityBonus = 0.3
	}

	matchRatio := float64(matches) / float64(total)
	score := matchRatio + specificityBonus

	// Clamp to [0, 1]
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// predicateMatches checks if a predicate key matches the context
func (s *RelevanceScorer) predicateMatches(key string, ctx *models.ContextSnapshot) bool {
	switch key {
	case "file", "file_path":
		return ctx.FilePath != ""
	case "language":
		return ctx.FileLanguage != ""
	case "task":
		return ctx.Task != ""
	case "environment", "env":
		return ctx.Environment != ""
	case "repo", "repository":
		return ctx.RepoRoot != ""
	default:
		// Check custom fields
		if ctx.Custom != nil {
			_, ok := ctx.Custom[key]
			return ok
		}
		return false
	}
}

// usageScore calculates score based on usage statistics
func (s *RelevanceScorer) usageScore(behavior *models.Behavior) float64 {
	stats := behavior.Stats

	// Calculate followed/activated ratio
	if stats.TimesActivated == 0 {
		// New behavior, give it a fair chance
		return 0.5
	}

	// No follow/confirm/override feedback yet — stay neutral.
	// Without this, incrementing TimesActivated would produce 0/N = 0.0,
	// penalizing all behaviors as soon as activation tracking starts.
	if stats.TimesFollowed == 0 && stats.TimesConfirmed == 0 && stats.TimesOverridden == 0 {
		return 0.5
	}

	// TimesFollowed + TimesConfirmed = positive signals
	positiveSignals := stats.TimesFollowed + stats.TimesConfirmed
	totalActivations := stats.TimesActivated

	ratio := float64(positiveSignals) / float64(totalActivations)

	// Penalize behaviors that are frequently overridden
	if stats.TimesOverridden > 0 {
		overrideRatio := float64(stats.TimesOverridden) / float64(totalActivations)
		ratio -= overrideRatio * 0.5 // 50% penalty for overrides
	}

	// Clamp to [0, 1]
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	return ratio
}

// recencyScore calculates score based on how recently the behavior was used
func (s *RelevanceScorer) recencyScore(behavior *models.Behavior) float64 {
	// Use most recent of: LastActivated, LastConfirmed, UpdatedAt
	var lastUsed time.Time

	if behavior.Stats.LastConfirmed != nil && behavior.Stats.LastConfirmed.After(lastUsed) {
		lastUsed = *behavior.Stats.LastConfirmed
	}
	if behavior.Stats.LastActivated != nil && behavior.Stats.LastActivated.After(lastUsed) {
		lastUsed = *behavior.Stats.LastActivated
	}
	if behavior.Stats.UpdatedAt.After(lastUsed) {
		lastUsed = behavior.Stats.UpdatedAt
	}

	// If no activity, use creation time
	if lastUsed.IsZero() {
		lastUsed = behavior.Stats.CreatedAt
	}

	return ExponentialDecay(lastUsed, s.config.RecencyHalfLife)
}

// confidenceScore returns the behavior's confidence as a score
func (s *RelevanceScorer) confidenceScore(behavior *models.Behavior) float64 {
	return behavior.Confidence
}

// priorityScore normalizes priority to a 0-1 score
func (s *RelevanceScorer) priorityScore(behavior *models.Behavior) float64 {
	// Assume priority is typically 0-10, normalize to 0-1
	priority := behavior.Priority
	if priority < 0 {
		priority = 0
	}
	if priority > 10 {
		priority = 10
	}
	return float64(priority) / 10.0
}

// kindBoost returns the score multiplier for a behavior kind
func (s *RelevanceScorer) kindBoost(kind models.BehaviorKind) float64 {
	if boost, ok := s.config.KindBoosts[kind]; ok {
		return boost
	}
	return 1.0
}
