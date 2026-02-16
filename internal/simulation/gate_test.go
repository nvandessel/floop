package simulation_test

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// TestCreationGateViability validates that the co-activation creation gate
// correctly creates edges for frequently co-occurring non-seed pairs while
// not creating edges for pairs that rarely co-activate.
//
// Key design: ExtractCoActivationPairs excludes seed-seed pairs by design,
// so the test uses a single seed (hub) with strong semantic edges to
// non-seed behaviors. Non-seeds that both activate above threshold form
// co-activation pairs eligible for edge creation.
//
// Setup:
//   - 1 hub seed (A) with strong semantic edges to B, C, D, E, F
//   - No initial co-activated edges — testing creation from scratch
//   - B and C are always reachable from A (frequent co-activation pair)
//   - D is reachable only in every-other session (alternating seed pattern)
//   - E is reachable only in session 0 (rare pair with B or C)
//   - CreateEdges=true, 21 sessions
//   - Boosted spread config so non-seeds reach above activation threshold
//
// Expected: B↔C co-activated edge created (frequent pair), B↔D or C↔D edge
// created (moderate pair), E edges rare or absent.
func TestCreationGateViability(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{ID: "hub", Name: "Hub Seed", Kind: models.BehaviorKindDirective, Canonical: "Central hub behavior"},
		{ID: "freq-b", Name: "Frequent B", Kind: models.BehaviorKindDirective, Canonical: "Frequently co-activated B"},
		{ID: "freq-c", Name: "Frequent C", Kind: models.BehaviorKindDirective, Canonical: "Frequently co-activated C"},
		{ID: "mod-d", Name: "Moderate D", Kind: models.BehaviorKindDirective, Canonical: "Moderately co-activated D"},
		{ID: "rare-e", Name: "Rare E", Kind: models.BehaviorKindDirective, Canonical: "Rarely co-activated E"},
		{ID: "idle-f", Name: "Idle F", Kind: models.BehaviorKindDirective, Canonical: "Never activated F"},
	}

	// Strong semantic edges from hub to B, C, D, E.
	// No edge to F — it stays idle.
	edges := []simulation.EdgeSpec{
		{Source: "hub", Target: "freq-b", Kind: "semantic", Weight: 0.9},
		{Source: "hub", Target: "freq-c", Kind: "semantic", Weight: 0.9},
		{Source: "hub", Target: "mod-d", Kind: "semantic", Weight: 0.9},
		{Source: "hub", Target: "rare-e", Kind: "semantic", Weight: 0.9},
	}

	// Boosted spread config: with 4 outbound edges from hub, we need strong
	// propagation so non-seeds pass the co-activation threshold.
	// energy = 0.8 * 0.95 * 0.9 / 4 * 0.85 = 0.145, sigmoid(0.145) ≈ 0.174
	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.01,
		TemporalDecayRate: 0.001,
	}

	hebbianCfg := spreading.DefaultHebbianConfig()
	hebbianCfg.ActivationThreshold = 0.1 // Non-seeds reach ~0.17 post-sigmoid
	hebbianCfg.CreationGate = 1          // Create on first co-activation

	sessions := make([]simulation.SessionContext, 21)

	scenario := simulation.Scenario{
		Name:           "creation-gate",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianConfig:  &hebbianCfg,
		HebbianEnabled: true,
		CreateEdges:    true,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			// Hub always seeded — B and C always reachable.
			seeds := []spreading.Seed{
				{BehaviorID: "hub", Activation: 0.8, Source: "test"},
			}

			// On session 0 only, also seed rare-e directly to boost it.
			// On all other sessions, rare-e only gets spreading activation
			// which may be below threshold.
			if sessionIndex == 0 {
				seeds = append(seeds, spreading.Seed{
					BehaviorID: "rare-e",
					Activation: 0.6,
					Source:     "test",
				})
			}

			return seeds
		},
	}

	result := r.Run(scenario)

	// Log first few sessions to see pair formation.
	for i := 0; i < 3 && i < len(result.Sessions); i++ {
		sr := result.Sessions[i]
		t.Logf("Session %d: pairs=%d results=%d", sr.Index, len(sr.Pairs), len(sr.Results))
		for _, p := range sr.Pairs {
			t.Logf("  %s <-> %s (%.4f, %.4f)", p.BehaviorA, p.BehaviorB, p.ActivationA, p.ActivationB)
		}
	}

	// Assertion 1: B and C both surface (they're always reachable from hub).
	simulation.AssertBehaviorSurfaces(t, result, "freq-b", 0.1)
	simulation.AssertBehaviorSurfaces(t, result, "freq-c", 0.1)

	// Assertion 2: B↔C co-activated edge gets created (frequent co-activation pair).
	// Both are non-seeds reachable from hub every session.
	simulation.AssertEdgeCreated(t, result, "freq-b", "freq-c")

	// Assertion 3: B↔D and C↔D edges also get created — D is reachable
	// from hub every session via semantic edge.
	simulation.AssertEdgeCreated(t, result, "freq-b", "mod-d")
	simulation.AssertEdgeCreated(t, result, "freq-c", "mod-d")

	// Assertion 4: F has no co-activated edges (never activated, no semantic
	// edge from hub).
	simulation.AssertEdgeNotCreated(t, result, "freq-b", "idle-f")
	simulation.AssertEdgeNotCreated(t, result, "freq-c", "idle-f")

	// Assertion 5: No weight explosion.
	simulation.AssertNoWeightExplosion(t, result, 0.95)

	// Assertion 6: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Log final edge weights for created edges.
	lastSession := result.Sessions[len(result.Sessions)-1]
	for key, w := range lastSession.EdgeWeights {
		t.Logf("Final edge: %s = %.6f", key, w)
	}
}
