package simulation_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/simulation"
	"github.com/nvandessel/floop/internal/spreading"
)

// TestBoundsInvariants runs a stress scenario with high activations and
// extended sessions to validate system invariants hold under pressure.
//
// Invariants tested:
//  1. Edge weights never exceed MaxWeight (0.95) despite Oja clamping
//  2. Edge weights are never negative
//  3. Activations are never negative
//  4. All activations are finite (no NaN/Inf)
//  5. Every session produces results
func TestBoundsInvariants(t *testing.T) {
	r := simulation.NewRunner(t)

	// 8 behaviors: mix of directive, constraint, and procedure kinds.
	behaviors := []simulation.BehaviorSpec{
		{ID: "bound-1", Name: "Alpha", Kind: models.BehaviorKindDirective, Canonical: "Alpha behavior for stress test", Tags: []string{"core"}},
		{ID: "bound-2", Name: "Beta", Kind: models.BehaviorKindDirective, Canonical: "Beta behavior for stress test", Tags: []string{"core"}},
		{ID: "bound-3", Name: "Gamma", Kind: models.BehaviorKindConstraint, Canonical: "Gamma constraint behavior", Tags: []string{"safety"}},
		{ID: "bound-4", Name: "Delta", Kind: models.BehaviorKindProcedure, Canonical: "Delta procedure behavior", Tags: []string{"workflow"}},
		{ID: "bound-5", Name: "Epsilon", Kind: models.BehaviorKindDirective, Canonical: "Epsilon behavior", Tags: []string{"core"}},
		{ID: "bound-6", Name: "Zeta", Kind: models.BehaviorKindDirective, Canonical: "Zeta behavior", Tags: []string{"extra"}},
		{ID: "bound-7", Name: "Eta", Kind: models.BehaviorKindConstraint, Canonical: "Eta constraint", Tags: []string{"safety"}},
		{ID: "bound-8", Name: "Theta", Kind: models.BehaviorKindProcedure, Canonical: "Theta procedure", Tags: []string{"workflow"}},
	}

	// Dense edge graph: semantic, co-activated, and conflict edges.
	edges := []simulation.EdgeSpec{
		// Semantic backbone
		{Source: "bound-1", Target: "bound-2", Kind: "semantic", Weight: 0.8},
		{Source: "bound-2", Target: "bound-3", Kind: "semantic", Weight: 0.7},
		{Source: "bound-3", Target: "bound-4", Kind: "semantic", Weight: 0.6},
		{Source: "bound-4", Target: "bound-5", Kind: "semantic", Weight: 0.7},
		{Source: "bound-5", Target: "bound-6", Kind: "semantic", Weight: 0.6},
		{Source: "bound-6", Target: "bound-7", Kind: "semantic", Weight: 0.5},
		{Source: "bound-7", Target: "bound-8", Kind: "semantic", Weight: 0.5},
		{Source: "bound-8", Target: "bound-1", Kind: "semantic", Weight: 0.4},
		// Cross connections
		{Source: "bound-1", Target: "bound-5", Kind: "semantic", Weight: 0.5},
		{Source: "bound-3", Target: "bound-7", Kind: "semantic", Weight: 0.6},
		// Initial co-activated edges
		{Source: "bound-1", Target: "bound-2", Kind: "co-activated", Weight: 0.4},
		{Source: "bound-3", Target: "bound-4", Kind: "co-activated", Weight: 0.3},
		// Conflict edge: 6 conflicts with 3
		{Source: "bound-6", Target: "bound-3", Kind: "conflicts", Weight: 0.7},
	}

	inh := spreading.DefaultInhibitionConfig()
	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.01,
		TemporalDecayRate: 0.01,
		Inhibition:        &inh,
	}

	hebbianCfg := spreading.DefaultHebbianConfig()
	hebbianCfg.ActivationThreshold = 0.05

	sessions := make([]simulation.SessionContext, 100)

	scenario := simulation.Scenario{
		Name:           "bounds-invariants-stress",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianConfig:  &hebbianCfg,
		HebbianEnabled: true,
		CreateEdges:    true,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			// Rotate seeds across behaviors to maximize co-activation diversity.
			seedID := fmt.Sprintf("bound-%d", (sessionIndex%8)+1)
			return []spreading.Seed{
				{BehaviorID: seedID, Activation: 0.95, Source: "test"},
			}
		},
	}

	result := r.Run(scenario)

	// Invariant 1: No weight explosion — weights never exceed MaxWeight.
	simulation.AssertNoWeightExplosion(t, result, 0.95)

	// Invariant 2: All edge weights bounded [0, 0.95].
	simulation.AssertWeightBounded(t, result, 0.0, 0.95)

	// Invariant 3: All activations are non-negative and finite.
	for _, sr := range result.Sessions {
		for _, res := range sr.Results {
			if res.Activation < 0 {
				t.Errorf("session %d: behavior %s has negative activation %.6f",
					sr.Index, res.BehaviorID, res.Activation)
			}
			if math.IsNaN(res.Activation) {
				t.Errorf("session %d: behavior %s has NaN activation",
					sr.Index, res.BehaviorID)
			}
			if math.IsInf(res.Activation, 0) {
				t.Errorf("session %d: behavior %s has Inf activation",
					sr.Index, res.BehaviorID)
			}
		}
	}

	// Invariant 4: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Log summary statistics.
	initialEdges := simulation.CountUniqueEdges(simulation.SimulationResult{Sessions: result.Sessions[:1]})
	finalEdges := simulation.CountUniqueEdges(result)
	t.Logf("Edge count: initial=%d, final=%d", initialEdges, finalEdges)
	t.Logf("Sessions: %d", len(result.Sessions))
}
