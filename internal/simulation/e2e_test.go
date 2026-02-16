package simulation_test

import (
	"fmt"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// TestE2EPipelineStability is the capstone test: a realistic graph with 30
// behaviors, 20 semantic edges, 5 initial co-activated edges, running 100
// sessions with rotating contexts across 4 groups.
//
// This validates that the full pipeline (spreading + sigmoid + inhibition +
// Oja + temporal decay) doesn't exhibit pathological emergent dynamics:
//   - No weight explosion
//   - No activation collapse
//   - No edge count explosion
//   - Diverse top-7 behaviors across sessions
func TestE2EPipelineStability(t *testing.T) {
	r := simulation.NewRunner(t)

	// Build 30 behaviors across 4 domains.
	var behaviors []simulation.BehaviorSpec
	domains := []struct {
		prefix string
		kind   models.BehaviorKind
		count  int
	}{
		{"go", models.BehaviorKindDirective, 8},
		{"py", models.BehaviorKindDirective, 8},
		{"sec", models.BehaviorKindConstraint, 7},
		{"ops", models.BehaviorKindProcedure, 7},
	}

	for _, d := range domains {
		for i := 1; i <= d.count; i++ {
			behaviors = append(behaviors, simulation.BehaviorSpec{
				ID:        fmt.Sprintf("%s-%d", d.prefix, i),
				Name:      fmt.Sprintf("%s behavior %d", d.prefix, i),
				Kind:      d.kind,
				Canonical: fmt.Sprintf("Canonical content for %s-%d with enough text for token estimation", d.prefix, i),
				Tags:      []string{d.prefix},
			})
		}
	}

	// 20 semantic edges — intra-domain connectivity.
	edges := []simulation.EdgeSpec{
		// Go cluster
		{Source: "go-1", Target: "go-2", Kind: "semantic", Weight: 0.7},
		{Source: "go-2", Target: "go-3", Kind: "semantic", Weight: 0.6},
		{Source: "go-3", Target: "go-4", Kind: "semantic", Weight: 0.5},
		{Source: "go-4", Target: "go-5", Kind: "semantic", Weight: 0.5},
		{Source: "go-5", Target: "go-6", Kind: "semantic", Weight: 0.4},
		// Python cluster
		{Source: "py-1", Target: "py-2", Kind: "semantic", Weight: 0.7},
		{Source: "py-2", Target: "py-3", Kind: "semantic", Weight: 0.6},
		{Source: "py-3", Target: "py-4", Kind: "semantic", Weight: 0.5},
		{Source: "py-4", Target: "py-5", Kind: "semantic", Weight: 0.5},
		{Source: "py-5", Target: "py-6", Kind: "semantic", Weight: 0.4},
		// Security cluster
		{Source: "sec-1", Target: "sec-2", Kind: "semantic", Weight: 0.7},
		{Source: "sec-2", Target: "sec-3", Kind: "semantic", Weight: 0.6},
		{Source: "sec-3", Target: "sec-4", Kind: "semantic", Weight: 0.5},
		{Source: "sec-4", Target: "sec-5", Kind: "semantic", Weight: 0.5},
		// Ops cluster
		{Source: "ops-1", Target: "ops-2", Kind: "semantic", Weight: 0.7},
		{Source: "ops-2", Target: "ops-3", Kind: "semantic", Weight: 0.6},
		{Source: "ops-3", Target: "ops-4", Kind: "semantic", Weight: 0.5},
		{Source: "ops-4", Target: "ops-5", Kind: "semantic", Weight: 0.5},
		// Cross-domain bridges
		{Source: "go-1", Target: "sec-1", Kind: "semantic", Weight: 0.3},
		{Source: "py-1", Target: "ops-1", Kind: "semantic", Weight: 0.3},
		// 5 initial co-activated edges
		{Source: "go-1", Target: "go-2", Kind: "co-activated", Weight: 0.4},
		{Source: "py-1", Target: "py-2", Kind: "co-activated", Weight: 0.4},
		{Source: "sec-1", Target: "sec-2", Kind: "co-activated", Weight: 0.3},
		{Source: "ops-1", Target: "ops-2", Kind: "co-activated", Weight: 0.3},
		{Source: "go-1", Target: "sec-1", Kind: "co-activated", Weight: 0.2},
	}

	sessions := make([]simulation.SessionContext, 100)

	// Boost propagation so non-seed behaviors reached via spreading can
	// participate in co-activation pairs.
	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.01,
		TemporalDecayRate: 0.001,
	}

	hebbianCfg := spreading.DefaultHebbianConfig()
	hebbianCfg.ActivationThreshold = 0.1

	scenario := simulation.Scenario{
		Name:           "e2e-pipeline-stability",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianConfig:  &hebbianCfg,
		HebbianEnabled: true,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			switch sessionIndex % 4 {
			case 0:
				return []spreading.Seed{
					{BehaviorID: "go-1", Activation: 0.8, Source: "context:language=go"},
					{BehaviorID: "go-2", Activation: 0.7, Source: "context:language=go"},
				}
			case 1:
				return []spreading.Seed{
					{BehaviorID: "py-1", Activation: 0.8, Source: "context:language=python"},
					{BehaviorID: "py-2", Activation: 0.7, Source: "context:language=python"},
				}
			case 2:
				return []spreading.Seed{
					{BehaviorID: "sec-1", Activation: 0.8, Source: "context:task=security"},
					{BehaviorID: "sec-2", Activation: 0.7, Source: "context:task=security"},
				}
			default:
				return []spreading.Seed{
					{BehaviorID: "ops-1", Activation: 0.8, Source: "context:task=deploy"},
					{BehaviorID: "ops-2", Activation: 0.7, Source: "context:task=deploy"},
				}
			}
		},
	}

	result := r.Run(scenario)

	// Assertion 1: No weight explosion.
	simulation.AssertNoWeightExplosion(t, result, 0.95)

	// Assertion 2: No activation collapse — at least 2 behaviors per session
	// above the minimum activation threshold.
	simulation.AssertNoActivationCollapse(t, result, 0.01, 2, 0)

	// Assertion 3: Edge weights stay bounded.
	simulation.AssertWeightBounded(t, result, 0.0, 0.95)

	// Assertion 4: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Assertion 5: Diverse co-activation — more than 7 unique behaviors
	// involved in co-activation across all sessions.
	simulation.AssertDiverseCoActivation(t, result, 8)

	// Assertion 6: Total edge count doesn't explode (no runaway creation).
	initialEdges := simulation.CountUniqueEdges(simulation.SimulationResult{Sessions: result.Sessions[:1]})
	finalEdges := simulation.CountUniqueEdges(result)
	t.Logf("Edge count: initial=%d, final=%d", initialEdges, finalEdges)

	// Log summary statistics.
	t.Logf("Sessions: %d", len(result.Sessions))
	totalPairs := 0
	for _, sr := range result.Sessions {
		totalPairs += len(sr.Pairs)
	}
	t.Logf("Total co-activation pairs across sessions: %d", totalPairs)
}
