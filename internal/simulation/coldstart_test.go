package simulation_test

import (
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// TestColdStartFairness validates that new behaviors (TimesActivated=0)
// aren't permanently disadvantaged by ACT-R base-level scoring.
//
// Setup:
//   - 5 established behaviors (TimesActivated=50, age=30d)
//   - 3 new behaviors (TimesActivated=0, age=0d)
//   - All match the same context via direct seeding
//
// Expected: new behaviors appear in results with meaningful activation,
// not starved by the established behaviors' history advantage.
func TestColdStartFairness(t *testing.T) {
	r := simulation.NewRunner(t)

	thirtyDaysAgo := simulation.TimeAgoPtr(30 * 24 * time.Hour)
	now := simulation.TimeAgoPtr(0)

	behaviors := []simulation.BehaviorSpec{
		// Established behaviors with history
		{ID: "est-1", Name: "Established 1", Kind: models.BehaviorKindDirective, Canonical: "Long-running behavior one",
			Stats: models.BehaviorStats{TimesActivated: 50, LastActivated: thirtyDaysAgo, CreatedAt: simulation.TimeAgo(60 * 24 * time.Hour)}},
		{ID: "est-2", Name: "Established 2", Kind: models.BehaviorKindDirective, Canonical: "Long-running behavior two",
			Stats: models.BehaviorStats{TimesActivated: 45, LastActivated: thirtyDaysAgo, CreatedAt: simulation.TimeAgo(60 * 24 * time.Hour)}},
		{ID: "est-3", Name: "Established 3", Kind: models.BehaviorKindDirective, Canonical: "Long-running behavior three",
			Stats: models.BehaviorStats{TimesActivated: 40, LastActivated: thirtyDaysAgo, CreatedAt: simulation.TimeAgo(60 * 24 * time.Hour)}},
		{ID: "est-4", Name: "Established 4", Kind: models.BehaviorKindConstraint, Canonical: "Long-running constraint",
			Stats: models.BehaviorStats{TimesActivated: 55, LastActivated: thirtyDaysAgo, CreatedAt: simulation.TimeAgo(60 * 24 * time.Hour)}},
		{ID: "est-5", Name: "Established 5", Kind: models.BehaviorKindPreference, Canonical: "Long-running preference",
			Stats: models.BehaviorStats{TimesActivated: 35, LastActivated: thirtyDaysAgo, CreatedAt: simulation.TimeAgo(60 * 24 * time.Hour)}},
		// New behaviors — cold start
		{ID: "new-1", Name: "New Behavior 1", Kind: models.BehaviorKindDirective, Canonical: "Brand new directive",
			Stats: models.BehaviorStats{TimesActivated: 0, CreatedAt: time.Now(), LastActivated: now}},
		{ID: "new-2", Name: "New Behavior 2", Kind: models.BehaviorKindDirective, Canonical: "Another new directive",
			Stats: models.BehaviorStats{TimesActivated: 0, CreatedAt: time.Now(), LastActivated: now}},
		{ID: "new-3", Name: "New Behavior 3", Kind: models.BehaviorKindProcedure, Canonical: "New procedure step by step",
			Stats: models.BehaviorStats{TimesActivated: 0, CreatedAt: time.Now(), LastActivated: now}},
	}

	// Semantic edges connecting established and new behaviors in one cluster.
	edges := []simulation.EdgeSpec{
		{Source: "est-1", Target: "new-1", Kind: "semantic", Weight: 0.7},
		{Source: "est-2", Target: "new-2", Kind: "semantic", Weight: 0.7},
		{Source: "est-3", Target: "new-3", Kind: "semantic", Weight: 0.7},
		{Source: "est-1", Target: "est-2", Kind: "semantic", Weight: 0.5},
		{Source: "est-2", Target: "est-3", Kind: "semantic", Weight: 0.5},
	}

	sessions := make([]simulation.SessionContext, 20)

	scenario := simulation.Scenario{
		Name:      "coldstart-fairness",
		Behaviors: behaviors,
		Edges:     edges,
		Sessions:  sessions,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			// Seed all established behaviors. New ones should be reached
			// via spreading through semantic edges.
			return []spreading.Seed{
				{BehaviorID: "est-1", Activation: 0.7, Source: "test"},
				{BehaviorID: "est-2", Activation: 0.7, Source: "test"},
				{BehaviorID: "est-3", Activation: 0.7, Source: "test"},
				{BehaviorID: "est-4", Activation: 0.6, Source: "test"},
				{BehaviorID: "est-5", Activation: 0.5, Source: "test"},
			}
		},
	}

	result := r.Run(scenario)

	// Assertion 1: New behaviors surface in at least one session.
	// The spreading engine should reach them via semantic edges.
	simulation.AssertBehaviorSurfaces(t, result, "new-1", 0.01)
	simulation.AssertBehaviorSurfaces(t, result, "new-2", 0.01)
	simulation.AssertBehaviorSurfaces(t, result, "new-3", 0.01)

	// Assertion 2: Established behaviors also appear (sanity check).
	simulation.AssertBehaviorSurfaces(t, result, "est-1", 0.3)
	simulation.AssertBehaviorSurfaces(t, result, "est-2", 0.3)
	simulation.AssertBehaviorSurfaces(t, result, "est-3", 0.3)

	// Assertion 3: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Log activation levels of new behaviors for manual inspection.
	for _, sr := range result.Sessions[:3] {
		for _, res := range sr.Results {
			if res.BehaviorID == "new-1" || res.BehaviorID == "new-2" || res.BehaviorID == "new-3" {
				t.Logf("Session %d: %s activation=%.4f", sr.Index, res.BehaviorID, res.Activation)
			}
		}
	}
}
