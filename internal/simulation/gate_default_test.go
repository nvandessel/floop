package simulation_test

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// TestCreationGateDefault validates that edges are NOT created until the
// creation gate threshold (default=3) is met. This exercises the co-activation
// counting logic in Runner.applyHebbian.
//
// Setup:
//   - 4 behaviors: hub (seed), freq-b, freq-c, rare-d
//   - Hub has semantic edges to all three non-seeds (weight 0.9)
//   - Hub is seeded every session; rare-d is additionally seeded only in
//     sessions 0, 5, and 10 (to test slow gate accumulation)
//   - HebbianConfig with default CreationGate=3 (NOT gate=1)
//   - CreateEdges=true, 10 sessions
//   - Boosted spread config so non-seeds reach above activation threshold
//
// Expected:
//   - After session 0: no co-activated edge between freq-b and freq-c (count=1)
//   - After session 1: no co-activated edge (count=2)
//   - After session 2: edge freq-b↔freq-c IS created (count=3, gate met)
//   - After session 9: edge has been strengthened via Oja
//   - No weight explosion
func TestCreationGateDefault(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{ID: "hub", Name: "Hub Seed", Kind: models.BehaviorKindDirective, Canonical: "Central hub behavior"},
		{ID: "freq-b", Name: "Frequent B", Kind: models.BehaviorKindDirective, Canonical: "Frequently co-activated B"},
		{ID: "freq-c", Name: "Frequent C", Kind: models.BehaviorKindDirective, Canonical: "Frequently co-activated C"},
		{ID: "rare-d", Name: "Rare D", Kind: models.BehaviorKindDirective, Canonical: "Rarely co-activated D"},
	}

	// Strong semantic edges from hub to all non-seeds.
	edges := []simulation.EdgeSpec{
		{Source: "hub", Target: "freq-b", Kind: "semantic", Weight: 0.9},
		{Source: "hub", Target: "freq-c", Kind: "semantic", Weight: 0.9},
		{Source: "hub", Target: "rare-d", Kind: "semantic", Weight: 0.9},
	}

	// Boosted spread config: with 3 outbound edges from hub, we need strong
	// propagation so non-seeds pass the co-activation threshold.
	// energy = 0.8 * 0.95 * 0.9 / 3 * 0.85 = 0.1938, sigmoid(0.1938) ≈ 0.256
	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.01,
		TemporalDecayRate: 0.001,
	}

	// Default Hebbian config: CreationGate=3.
	hebbianCfg := spreading.DefaultHebbianConfig()
	hebbianCfg.ActivationThreshold = 0.1 // Non-seeds reach ~0.26 post-sigmoid

	sessions := make([]simulation.SessionContext, 10)

	scenario := simulation.Scenario{
		Name:           "creation-gate-default",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianConfig:  &hebbianCfg,
		HebbianEnabled: true,
		CreateEdges:    true,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			seeds := []spreading.Seed{
				{BehaviorID: "hub", Activation: 0.8, Source: "test"},
			}
			// Rare-d additionally seeded only in sessions 0, 5 to boost
			// its activation (otherwise it gets spreading activation from hub).
			if sessionIndex == 0 || sessionIndex == 5 {
				seeds = append(seeds, spreading.Seed{
					BehaviorID: "rare-d",
					Activation: 0.6,
					Source:     "test",
				})
			}
			return seeds
		},
	}

	result := r.Run(scenario)

	// Log first few sessions for debugging.
	for i := 0; i < 5 && i < len(result.Sessions); i++ {
		sr := result.Sessions[i]
		t.Logf("Session %d: pairs=%d results=%d", sr.Index, len(sr.Pairs), len(sr.Results))
		for _, p := range sr.Pairs {
			t.Logf("  pair: %s <-> %s (%.4f, %.4f)", p.BehaviorA, p.BehaviorB, p.ActivationA, p.ActivationB)
		}
	}

	// --- Assertion 1: Edge absent after sessions 0 and 1 (gate count < 3) ---
	simulation.AssertEdgeAbsentAtSession(t, result, "freq-b", "freq-c", 0)
	simulation.AssertEdgeAbsentAtSession(t, result, "freq-b", "freq-c", 1)

	// --- Assertion 2: Edge present after session 2 (gate count = 3) ---
	simulation.AssertEdgePresentAtSession(t, result, "freq-b", "freq-c", 2)

	// --- Assertion 3: Edge strengthened by session 9 vs session 2 ---
	// Once created, Oja should strengthen the edge over subsequent sessions.
	getEdgeWeight := func(sessionIdx int, a, b string) float64 {
		sr := result.Sessions[sessionIdx]
		keyAB := simulation.EdgeKey(a, b, "co-activated")
		keyBA := simulation.EdgeKey(b, a, "co-activated")
		if w, ok := sr.EdgeWeights[keyAB]; ok {
			return w
		}
		if w, ok := sr.EdgeWeights[keyBA]; ok {
			return w
		}
		return -1
	}

	weightAtCreation := getEdgeWeight(2, "freq-b", "freq-c")
	weightAtEnd := getEdgeWeight(len(result.Sessions)-1, "freq-b", "freq-c")

	t.Logf("freq-b↔freq-c weight at creation (session 2): %.6f", weightAtCreation)
	t.Logf("freq-b↔freq-c weight at end (session %d): %.6f", len(result.Sessions)-1, weightAtEnd)

	if weightAtCreation >= 0 && weightAtEnd >= 0 {
		if weightAtEnd <= weightAtCreation {
			t.Errorf("expected edge weight to increase via Oja: creation=%.6f, end=%.6f", weightAtCreation, weightAtEnd)
		}
	}

	// --- Assertion 4: Verify co-activation counter state ---
	// freq-b:freq-c should have been counted 3+ times (once per session they co-activate).
	counts := r.CoActivationCounts()
	bcKey := simulation.CoActivationKeyFor("freq-b", "freq-c")
	t.Logf("Co-activation counts: %v", counts)
	if count, ok := counts[bcKey]; ok {
		// Counter should show 3 (the gate was met at 3, then subsequent
		// co-activations update existing edge instead of incrementing counter).
		if count < 3 {
			t.Errorf("expected co-activation count for freq-b:freq-c >= 3, got %d", count)
		}
	} else {
		t.Errorf("no co-activation count found for key %s", bcKey)
	}

	// --- Assertion 5: No weight explosion ---
	simulation.AssertNoWeightExplosion(t, result, 0.95)

	// --- Assertion 6: All sessions produce results ---
	simulation.AssertResultsNotEmpty(t, result)

	// Log final edge weights.
	lastSession := result.Sessions[len(result.Sessions)-1]
	for key, w := range lastSession.EdgeWeights {
		t.Logf("Final edge: %s = %.6f", key, w)
	}
}
