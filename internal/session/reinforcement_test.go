package session

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestShouldReinforce_NeverInjected(t *testing.T) {
	cfg := DefaultReinforcementConfig()

	tests := []struct {
		name string
		kind models.BehaviorKind
	}{
		{"directive", models.BehaviorKindDirective},
		{"constraint", models.BehaviorKindConstraint},
		{"preference", models.BehaviorKindPreference},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := cfg.ShouldReinforce(
				nil, // never injected
				0.8,
				models.TierFull,
				tt.kind,
				models.BehaviorStats{},
				10,
			)
			if !decision.ShouldReinforce {
				t.Error("expected ShouldReinforce=true for never-injected behavior")
			}
			if decision.Reason != "never_injected" {
				t.Errorf("expected reason 'never_injected', got %q", decision.Reason)
			}
		})
	}
}

func TestShouldReinforce_Upgrade(t *testing.T) {
	cfg := DefaultReinforcementConfig()

	tests := []struct {
		name          string
		previousTier  models.InjectionTier
		requestedTier models.InjectionTier
	}{
		{"summary to full", models.TierSummary, models.TierFull},
		{"name-only to full", models.TierNameOnly, models.TierFull},
		{"name-only to summary", models.TierNameOnly, models.TierSummary},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := &InjectionRecord{
				Tier:       tt.previousTier,
				Count:      1,
				LastPrompt: 0,
			}
			decision := cfg.ShouldReinforce(
				record,
				0.9,
				tt.requestedTier,
				models.BehaviorKindDirective,
				models.BehaviorStats{},
				1, // just 1 prompt later
			)
			if !decision.ShouldReinforce {
				t.Error("expected ShouldReinforce=true for tier upgrade")
			}
			if decision.Reason != "upgrade" {
				t.Errorf("expected reason 'upgrade', got %q", decision.Reason)
			}
		})
	}
}

func TestShouldReinforce_BackoffWindow(t *testing.T) {
	cfg := DefaultReinforcementConfig()
	cfg.BackoffBase = 5

	record := &InjectionRecord{
		Tier:       models.TierFull,
		Count:      1,
		LastPrompt: 0,
	}

	// Injected 2 prompts ago, backoff = 5*1 = 5 prompts. Should suppress.
	decision := cfg.ShouldReinforce(
		record,
		0.8,
		models.TierFull,
		models.BehaviorKindDirective,
		models.BehaviorStats{},
		2,
	)
	if decision.ShouldReinforce {
		t.Error("expected ShouldReinforce=false within backoff window")
	}
	if decision.Reason != "suppressed" {
		t.Errorf("expected reason 'suppressed', got %q", decision.Reason)
	}
}

func TestShouldReinforce_BackoffExpired(t *testing.T) {
	cfg := DefaultReinforcementConfig()
	cfg.BackoffBase = 5

	record := &InjectionRecord{
		Tier:       models.TierFull,
		Count:      1,
		LastPrompt: 0,
	}

	// Injected 10 prompts ago, backoff = 5*1 = 5. Past the window.
	decision := cfg.ShouldReinforce(
		record,
		0.8,
		models.TierFull,
		models.BehaviorKindDirective,
		models.BehaviorStats{},
		10,
	)
	if !decision.ShouldReinforce {
		t.Error("expected ShouldReinforce=true past backoff window")
	}
	if decision.Reason != "backoff_expired" {
		t.Errorf("expected reason 'backoff_expired', got %q", decision.Reason)
	}
}

func TestShouldReinforce_ExponentialBackoff(t *testing.T) {
	cfg := DefaultReinforcementConfig()
	cfg.BackoffBase = 5

	tests := []struct {
		name        string
		count       int
		lastPrompt  int
		promptCount int
		wantReason  string
		wantDecide  bool
	}{
		{
			name:        "1st injection: wait 5 prompts, at prompt 4 -> suppress",
			count:       1,
			lastPrompt:  0,
			promptCount: 4,
			wantReason:  "suppressed",
			wantDecide:  false,
		},
		{
			name:        "1st injection: wait 5 prompts, at prompt 5 -> reinforce",
			count:       1,
			lastPrompt:  0,
			promptCount: 5,
			wantReason:  "backoff_expired",
			wantDecide:  true,
		},
		{
			name:        "2nd injection: wait 10 prompts, at prompt 15 (last=7) -> suppress",
			count:       2,
			lastPrompt:  7,
			promptCount: 15,
			wantReason:  "suppressed",
			wantDecide:  false,
		},
		{
			name:        "2nd injection: wait 10 prompts, at prompt 17 (last=7) -> reinforce",
			count:       2,
			lastPrompt:  7,
			promptCount: 17,
			wantReason:  "backoff_expired",
			wantDecide:  true,
		},
		{
			name:        "3rd injection: wait 15 prompts, at prompt 34 (last=20) -> suppress",
			count:       3,
			lastPrompt:  20,
			promptCount: 34,
			wantReason:  "suppressed",
			wantDecide:  false,
		},
		{
			name:        "3rd injection: wait 15 prompts, at prompt 35 (last=20) -> reinforce",
			count:       3,
			lastPrompt:  20,
			promptCount: 35,
			wantReason:  "backoff_expired",
			wantDecide:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := &InjectionRecord{
				Tier:       models.TierFull,
				Count:      tt.count,
				LastPrompt: tt.lastPrompt,
			}
			decision := cfg.ShouldReinforce(
				record,
				0.8,
				models.TierFull,
				models.BehaviorKindDirective,
				models.BehaviorStats{},
				tt.promptCount,
			)
			if decision.ShouldReinforce != tt.wantDecide {
				t.Errorf("ShouldReinforce = %v, want %v", decision.ShouldReinforce, tt.wantDecide)
			}
			if decision.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", decision.Reason, tt.wantReason)
			}
		})
	}
}

func TestShouldReinforce_ConstraintBoosted(t *testing.T) {
	cfg := DefaultReinforcementConfig()
	cfg.BackoffBase = 5
	cfg.ConstraintMultiplier = 2

	record := &InjectionRecord{
		Tier:       models.TierFull,
		Count:      1,
		LastPrompt: 0,
	}

	// For a constraint: effective backoff = 5/2 = 2 prompts.
	// At prompt 2, should be past the backoff.
	decision := cfg.ShouldReinforce(
		record,
		0.8,
		models.TierFull,
		models.BehaviorKindConstraint,
		models.BehaviorStats{},
		2,
	)
	// effectiveBackoff = 5*1 / 2 = 2; promptsSince = 2 - 0 = 2; 2 >= 2 -> reinforce
	if !decision.ShouldReinforce {
		t.Error("expected ShouldReinforce=true for constraint past boosted backoff")
	}
	if decision.Reason != "backoff_expired" {
		t.Errorf("expected reason 'backoff_expired', got %q", decision.Reason)
	}

	// A non-constraint at prompt 2 should still be suppressed (backoff=5).
	decisionDir := cfg.ShouldReinforce(
		record,
		0.8,
		models.TierFull,
		models.BehaviorKindDirective,
		models.BehaviorStats{},
		2,
	)
	if decisionDir.ShouldReinforce {
		t.Error("expected ShouldReinforce=false for directive within backoff window")
	}
	if decisionDir.Reason != "suppressed" {
		t.Errorf("expected reason 'suppressed', got %q", decisionDir.Reason)
	}
}

func TestShouldReinforce_ViolationDetected(t *testing.T) {
	cfg := DefaultReinforcementConfig()
	cfg.BackoffBase = 5
	cfg.ViolationBoost = true

	record := &InjectionRecord{
		Tier:       models.TierFull,
		Count:      1,
		LastPrompt: 0,
	}

	// Behavior activated 10 times with feedback: 1 followed, 4 overridden.
	// Positive rate = 1/5 = 20% < 40% threshold. Should reinforce regardless of backoff.
	stats := models.BehaviorStats{
		TimesActivated:  10,
		TimesFollowed:   1,
		TimesOverridden: 4,
	}

	decision := cfg.ShouldReinforce(
		record,
		0.8,
		models.TierFull,
		models.BehaviorKindDirective,
		stats,
		1, // only 1 prompt ago, within backoff
	)
	if !decision.ShouldReinforce {
		t.Error("expected ShouldReinforce=true for violated behavior")
	}
	if decision.Reason != "violation_detected" {
		t.Errorf("expected reason 'violation_detected', got %q", decision.Reason)
	}

	// With ViolationBoost disabled, same scenario should suppress.
	cfgNoBoost := cfg
	cfgNoBoost.ViolationBoost = false

	decisionNoBoost := cfgNoBoost.ShouldReinforce(
		record,
		0.8,
		models.TierFull,
		models.BehaviorKindDirective,
		stats,
		1,
	)
	if decisionNoBoost.ShouldReinforce {
		t.Error("expected ShouldReinforce=false with ViolationBoost disabled")
	}

	// Positive rate at 40% threshold should NOT trigger violation.
	// 2 followed + 1 overridden = 3 total feedback, 2/3 = 67% positive >= 40%.
	statsAboveThreshold := models.BehaviorStats{
		TimesActivated:  10,
		TimesFollowed:   2,
		TimesOverridden: 1,
	}
	decisionAbove := cfg.ShouldReinforce(
		record,
		0.8,
		models.TierFull,
		models.BehaviorKindDirective,
		statsAboveThreshold,
		1,
	)
	if decisionAbove.ShouldReinforce {
		t.Error("expected ShouldReinforce=false for positive rate above threshold (not a violation)")
	}
}

func TestShouldReinforce_MaxReinjections(t *testing.T) {
	cfg := DefaultReinforcementConfig()
	cfg.MaxReinjections = 3

	// Count > MaxReinjections means we've already re-injected too many times.
	record := &InjectionRecord{
		Tier:       models.TierFull,
		Count:      4, // initial + 3 re-injections = 4 total
		LastPrompt: 0,
	}

	decision := cfg.ShouldReinforce(
		record,
		0.8,
		models.TierFull,
		models.BehaviorKindDirective,
		models.BehaviorStats{},
		100, // well past any backoff
	)
	if decision.ShouldReinforce {
		t.Error("expected ShouldReinforce=false after max reinjections")
	}
	if decision.Reason != "max_reinjections" {
		t.Errorf("expected reason 'max_reinjections', got %q", decision.Reason)
	}

	// Count exactly at MaxReinjections should still allow (Count=3, max=3 -> 3 > 3 is false).
	recordAtMax := &InjectionRecord{
		Tier:       models.TierFull,
		Count:      3,
		LastPrompt: 0,
	}

	decisionAtMax := cfg.ShouldReinforce(
		recordAtMax,
		0.8,
		models.TierFull,
		models.BehaviorKindDirective,
		models.BehaviorStats{},
		100,
	)
	if !decisionAtMax.ShouldReinforce {
		t.Error("expected ShouldReinforce=true at exactly MaxReinjections count")
	}
}

func TestIsViolated(t *testing.T) {
	tests := []struct {
		name  string
		stats models.BehaviorStats
		want  bool
	}{
		{
			name:  "no feedback data at all",
			stats: models.BehaviorStats{TimesActivated: 10},
			want:  false,
		},
		{
			name: "below minimum sample size",
			stats: models.BehaviorStats{
				TimesActivated:  10,
				TimesFollowed:   1,
				TimesOverridden: 1,
			},
			want: false, // only 2 total feedback < 3 minimum
		},
		{
			name: "exactly at minimum sample — not violated",
			stats: models.BehaviorStats{
				TimesActivated: 10,
				TimesFollowed:  2,
				TimesConfirmed: 1,
			},
			want: false, // 3/3 = 100% positive
		},
		{
			name: "at minimum sample — violated",
			stats: models.BehaviorStats{
				TimesActivated:  10,
				TimesOverridden: 3,
			},
			want: true, // 0/3 = 0% positive
		},
		{
			name: "positive rate at 40% threshold — not violated",
			stats: models.BehaviorStats{
				TimesActivated:  10,
				TimesFollowed:   2,
				TimesOverridden: 3,
			},
			want: false, // 2/5 = 0.4, not < 0.4
		},
		{
			name: "positive rate below 40% — violated",
			stats: models.BehaviorStats{
				TimesActivated:  10,
				TimesFollowed:   1,
				TimesOverridden: 4,
			},
			want: true, // 1/5 = 0.2 < 0.4
		},
		{
			name: "confirmed signals count as positive",
			stats: models.BehaviorStats{
				TimesActivated:  10,
				TimesFollowed:   1,
				TimesConfirmed:  3,
				TimesOverridden: 1,
			},
			want: false, // (1+3)/5 = 0.8 >= 0.4
		},
		{
			name: "all overridden",
			stats: models.BehaviorStats{
				TimesActivated:  20,
				TimesOverridden: 5,
			},
			want: true, // 0/5 = 0.0 < 0.4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isViolated(tt.stats)
			if got != tt.want {
				t.Errorf("isViolated() = %v, want %v", got, tt.want)
			}
		})
	}
}
