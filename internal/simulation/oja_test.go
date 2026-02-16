package simulation_test

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// TestOjaConvergence validates that Oja's self-limiting Hebbian rule causes
// co-activated edge weights to converge to a stable equilibrium rather than
// exploding to MaxWeight.
//
// Setup:
//   - 1 seed behavior (A) activated at 0.8
//   - 4 non-seed behaviors (B, C, D, E) activated via spreading
//   - Strong semantic edges A→B, A→C (weight 0.9) so B,C get high activation
//   - Pre-existing co-activated edges B↔C, B↔D, C↔D at 0.3
//   - Custom spread config with no inhibition for clean convergence signal
//
// Expected: co-activated edges converge to stable equilibrium, variance < 0.01
// in last 10 sessions. Oja's forgetting term prevents runaway to MaxWeight.
func TestOjaConvergence(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{ID: "beh-a", Name: "Behavior A", Kind: models.BehaviorKindDirective, Canonical: "Always do A"},
		{ID: "beh-b", Name: "Behavior B", Kind: models.BehaviorKindDirective, Canonical: "Always do B"},
		{ID: "beh-c", Name: "Behavior C", Kind: models.BehaviorKindDirective, Canonical: "Always do C"},
		{ID: "beh-d", Name: "Behavior D", Kind: models.BehaviorKindDirective, Canonical: "Always do D"},
		{ID: "beh-e", Name: "Behavior E", Kind: models.BehaviorKindDirective, Canonical: "Always do E"},
	}

	edges := []simulation.EdgeSpec{
		// Strong semantic edges so B, C, D get activated via spreading from A.
		{Source: "beh-a", Target: "beh-b", Kind: "semantic", Weight: 0.9},
		{Source: "beh-a", Target: "beh-c", Kind: "semantic", Weight: 0.9},
		{Source: "beh-a", Target: "beh-d", Kind: "semantic", Weight: 0.9},
		// Pre-existing co-activated edges we want to test convergence on.
		{Source: "beh-b", Target: "beh-c", Kind: "co-activated", Weight: 0.3},
		{Source: "beh-b", Target: "beh-d", Kind: "co-activated", Weight: 0.3},
		{Source: "beh-c", Target: "beh-d", Kind: "co-activated", Weight: 0.3},
	}

	// Custom spread config: boost propagation strength, disable inhibition
	// so non-seed behaviors reach meaningful activation levels.
	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.01,
		TemporalDecayRate: 0.001, // Low decay — edges were just touched
	}

	// Lower activation threshold for Hebbian pairs — with 3 semantic edges
	// from A, the out-degree division means B/C/D land at ~0.26 post-sigmoid,
	// just below the default 0.3 threshold. The threshold is configurable
	// per scenario by design.
	hebbianCfg := spreading.DefaultHebbianConfig()
	hebbianCfg.ActivationThreshold = 0.15

	// 50 identical sessions with A as the only seed.
	sessions := make([]simulation.SessionContext, 50)

	scenario := simulation.Scenario{
		Name:           "oja-convergence",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianConfig:  &hebbianCfg,
		HebbianEnabled: true,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			return []spreading.Seed{
				{BehaviorID: "beh-a", Activation: 0.8, Source: "test"},
			}
		},
	}

	result := r.Run(scenario)

	// Debug: log first and last session to understand activation levels.
	if len(result.Sessions) > 0 {
		t.Logf("Session 0:\n%s", simulation.FormatSessionDebug(result.Sessions[0]))
		t.Logf("Session 49:\n%s", simulation.FormatSessionDebug(result.Sessions[49]))
	}

	// Assertion 1: No weight explosion — all weights stay bounded.
	simulation.AssertNoWeightExplosion(t, result, 0.95)

	// Assertion 2: Co-activated edges converge to a stable range.
	// The exact equilibrium depends on the post-sigmoid activation levels
	// of B, C, D. We allow a wide range and check stability separately.
	simulation.AssertWeightConverges(t, result, "beh-b", "beh-c", "co-activated", 0.3, 0.95, 30)
	simulation.AssertWeightConverges(t, result, "beh-b", "beh-d", "co-activated", 0.3, 0.95, 30)
	simulation.AssertWeightConverges(t, result, "beh-c", "beh-d", "co-activated", 0.3, 0.95, 30)

	// Assertion 3: Stability — low variance in the last 10 sessions.
	simulation.AssertWeightStable(t, result, "beh-b", "beh-c", "co-activated", 0.01, 10)
	simulation.AssertWeightStable(t, result, "beh-b", "beh-d", "co-activated", 0.01, 10)
	simulation.AssertWeightStable(t, result, "beh-c", "beh-d", "co-activated", 0.01, 10)

	// Assertion 4: Weights changed from 0.3 initial value.
	simulation.AssertWeightIncreased(t, result, "beh-b", "beh-c", "co-activated", 0, 49)
	simulation.AssertWeightIncreased(t, result, "beh-b", "beh-d", "co-activated", 0, 49)
	simulation.AssertWeightIncreased(t, result, "beh-c", "beh-d", "co-activated", 0, 49)

	// Assertion 5: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)
}
