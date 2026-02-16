package simulation_test

import (
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// TestAttentionMonopolyPrevention validates that a clique of well-established
// behaviors with strong mutual edges cannot permanently monopolize attention,
// crowding out newcomer behaviors.
//
// Setup:
//   - 3 "old guard" behaviors with high stats and strong mutual edges (0.8)
//   - 5 newcomer behaviors with no edges and no stats
//   - Hebbian learning enabled, 100 sessions
//   - Seeds rotate: some sessions include newcomers
//
// Expected: newcomers appear in at least 30% of sessions, old guard weights
// don't all stay at max, system doesn't collapse to only the old guard.
func TestAttentionMonopolyPrevention(t *testing.T) {
	r := simulation.NewRunner(t)

	thirtyDaysAgo := simulation.TimeAgoPtr(30 * 24 * time.Hour)

	behaviors := []simulation.BehaviorSpec{
		// Old guard — well-established with history
		{ID: "old-1", Name: "Old Guard 1", Kind: models.BehaviorKindDirective, Canonical: "Established behavior one",
			Stats: models.BehaviorStats{TimesActivated: 100, LastActivated: thirtyDaysAgo, CreatedAt: simulation.TimeAgo(90 * 24 * time.Hour)}},
		{ID: "old-2", Name: "Old Guard 2", Kind: models.BehaviorKindDirective, Canonical: "Established behavior two",
			Stats: models.BehaviorStats{TimesActivated: 95, LastActivated: thirtyDaysAgo, CreatedAt: simulation.TimeAgo(90 * 24 * time.Hour)}},
		{ID: "old-3", Name: "Old Guard 3", Kind: models.BehaviorKindDirective, Canonical: "Established behavior three",
			Stats: models.BehaviorStats{TimesActivated: 90, LastActivated: thirtyDaysAgo, CreatedAt: simulation.TimeAgo(90 * 24 * time.Hour)}},
		// Newcomers — fresh, no history
		{ID: "new-1", Name: "Newcomer 1", Kind: models.BehaviorKindDirective, Canonical: "Fresh directive one"},
		{ID: "new-2", Name: "Newcomer 2", Kind: models.BehaviorKindDirective, Canonical: "Fresh directive two"},
		{ID: "new-3", Name: "Newcomer 3", Kind: models.BehaviorKindDirective, Canonical: "Fresh directive three"},
		{ID: "new-4", Name: "Newcomer 4", Kind: models.BehaviorKindPreference, Canonical: "Fresh preference"},
		{ID: "new-5", Name: "Newcomer 5", Kind: models.BehaviorKindProcedure, Canonical: "Fresh procedure"},
	}

	// Old guard has strong mutual co-activated edges.
	edges := []simulation.EdgeSpec{
		{Source: "old-1", Target: "old-2", Kind: "co-activated", Weight: 0.8},
		{Source: "old-1", Target: "old-3", Kind: "co-activated", Weight: 0.8},
		{Source: "old-2", Target: "old-3", Kind: "co-activated", Weight: 0.8},
		// Some semantic edges connecting old to new for spreading reach.
		{Source: "old-1", Target: "new-1", Kind: "semantic", Weight: 0.5},
		{Source: "old-2", Target: "new-2", Kind: "semantic", Weight: 0.5},
		{Source: "old-3", Target: "new-3", Kind: "semantic", Weight: 0.5},
	}

	sessions := make([]simulation.SessionContext, 100)

	hebbianCfg := spreading.DefaultHebbianConfig()
	hebbianCfg.ActivationThreshold = 0.1

	scenario := simulation.Scenario{
		Name:           "monopoly-prevention",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		HebbianConfig:  &hebbianCfg,
		HebbianEnabled: true,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			// Old guard always seeded.
			seeds := []spreading.Seed{
				{BehaviorID: "old-1", Activation: 0.8, Source: "test"},
				{BehaviorID: "old-2", Activation: 0.75, Source: "test"},
				{BehaviorID: "old-3", Activation: 0.7, Source: "test"},
			}
			// Every 5th session, also seed a newcomer directly.
			if sessionIndex%5 == 0 {
				newcomerIdx := (sessionIndex / 5) % 5
				newcomerID := []string{"new-1", "new-2", "new-3", "new-4", "new-5"}[newcomerIdx]
				seeds = append(seeds, spreading.Seed{
					BehaviorID: newcomerID,
					Activation: 0.6,
					Source:     "test",
				})
			}
			return seeds
		},
	}

	result := r.Run(scenario)

	// Assertion 1: At least some newcomers appear via spreading (via semantic edges).
	for _, id := range []string{"new-1", "new-2", "new-3"} {
		simulation.AssertBehaviorSurfaces(t, result, id, 0.01)
	}

	// Assertion 2: No weight explosion in old guard edges.
	simulation.AssertNoWeightExplosion(t, result, 0.95)

	// Assertion 3: No activation collapse — system should maintain diversity.
	simulation.AssertNoActivationCollapse(t, result, 0.01, 3, 10)

	// Assertion 4: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Log newcomer appearances.
	for _, id := range []string{"new-1", "new-2", "new-3", "new-4", "new-5"} {
		count := simulation.CountSessionsWithBehavior(result, id, 0.01)
		t.Logf("Newcomer %s appeared in %d/%d sessions", id, count, len(result.Sessions))
	}
}
