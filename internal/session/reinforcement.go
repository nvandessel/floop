package session

import (
	"github.com/nvandessel/feedback-loop/internal/models"
)

// ReinforcementConfig controls when behaviors should be re-injected.
type ReinforcementConfig struct {
	// BackoffBase is the number of prompts to wait before first re-injection.
	// Subsequent re-injections wait BackoffBase * injection_count. Default: 5.
	BackoffBase int

	// MaxReinjections is the maximum times a behavior can be re-injected per session.
	// After this, it's considered internalized. Default: 3.
	MaxReinjections int

	// ConstraintMultiplier increases re-injection frequency for constraints.
	// Constraints wait BackoffBase / ConstraintMultiplier prompts. Default: 2.
	ConstraintMultiplier int

	// ViolationBoost: when a behavior was activated but not followed
	// (high times_activated, low times_followed), multiply re-injection urgency.
	// Default: true.
	ViolationBoost bool
}

// DefaultReinforcementConfig returns sensible defaults.
func DefaultReinforcementConfig() ReinforcementConfig {
	return ReinforcementConfig{
		BackoffBase:          5,
		MaxReinjections:      3,
		ConstraintMultiplier: 2,
		ViolationBoost:       true,
	}
}

// ReinforcementDecision represents whether and why to reinforce a behavior.
type ReinforcementDecision struct {
	ShouldReinforce bool
	Reason          string // "never_injected", "upgrade", "backoff_expired", "violation_detected", "suppressed", "max_reinjections"
}

// ShouldReinforce determines whether a behavior should be re-injected.
//
// Decision matrix:
//
//	Never injected                                    -> REINFORCE (reason: "never_injected")
//	Injected at lower tier + higher activation now    -> REINFORCE (reason: "upgrade")
//	Constraint + violated (low follow rate)           -> REINFORCE (reason: "violation_detected")
//	Injected + past backoff window                    -> REINFORCE (reason: "backoff_expired")
//	Injected MaxReinjections times                    -> SUPPRESS  (reason: "max_reinjections")
//	Injected + within backoff window                  -> SUPPRESS  (reason: "suppressed")
func (c ReinforcementConfig) ShouldReinforce(
	record *InjectionRecord,
	currentActivation float64,
	requestedTier models.InjectionTier,
	kind models.BehaviorKind,
	stats models.BehaviorStats,
	promptCount int,
) ReinforcementDecision {
	// Never injected: always reinforce.
	if record == nil {
		return ReinforcementDecision{ShouldReinforce: true, Reason: "never_injected"}
	}

	// Max reinjections cap (Count includes the initial injection,
	// so re-injections = Count - 1; cap when re-injections reach MaxReinjections).
	if record.Count > c.MaxReinjections {
		return ReinforcementDecision{ShouldReinforce: false, Reason: "max_reinjections"}
	}

	// Upgrade: requested tier is more detailed (lower numeric value) than previously injected.
	if requestedTier < record.Tier {
		return ReinforcementDecision{ShouldReinforce: true, Reason: "upgrade"}
	}

	// Violation detection: behavior has enough feedback data to conclude
	// it is being consistently overridden (positive rate < 40%).
	if c.ViolationBoost && isViolated(stats) {
		return ReinforcementDecision{ShouldReinforce: true, Reason: "violation_detected"}
	}

	// Backoff check.
	backoff := c.effectiveBackoff(record.Count, kind)
	promptsSinceInjection := promptCount - record.LastPrompt
	if promptsSinceInjection < backoff {
		return ReinforcementDecision{ShouldReinforce: false, Reason: "suppressed"}
	}

	// Past backoff window: reinforce.
	return ReinforcementDecision{ShouldReinforce: true, Reason: "backoff_expired"}
}

// effectiveBackoff computes the backoff window in prompts for a given injection count and kind.
// The window grows linearly: BackoffBase * injectionCount.
// For constraints, the window is divided by ConstraintMultiplier.
func (c ReinforcementConfig) effectiveBackoff(injectionCount int, kind models.BehaviorKind) int {
	backoff := c.BackoffBase * injectionCount

	if kind == models.BehaviorKindConstraint && c.ConstraintMultiplier > 0 {
		backoff = backoff / c.ConstraintMultiplier
	}

	// Minimum backoff of 1 to prevent zero-length windows.
	if backoff < 1 {
		return 1
	}
	return backoff
}

// isViolated returns true when a behavior has enough feedback data to
// conclude it is being consistently overridden (positive rate < 40%).
// Uses totalFeedback as the denominator (not TimesActivated) with a
// minimum sample size of 3 to avoid spurious violation signals.
func isViolated(stats models.BehaviorStats) bool {
	totalFeedback := stats.TimesFollowed + stats.TimesConfirmed + stats.TimesOverridden
	if totalFeedback < 3 {
		return false
	}

	positiveSignals := float64(stats.TimesFollowed + stats.TimesConfirmed)
	positiveRate := positiveSignals / float64(totalFeedback)
	return positiveRate < 0.4
}
