package simulation_test

import (
	"testing"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/simulation"
	"github.com/nvandessel/floop/internal/spreading"
)

// TestConflictEdgePropagation validates that conflict edges subtract energy
// during spreading and interact correctly with lateral inhibition.
//
// Graph:
//
//	seed → A (semantic, 0.9)
//	A → B (conflicts, 0.8) — B should be suppressed
//	A → C (semantic, 0.8) — C should activate normally
//	A → D (semantic, 0.8) — D should activate normally (control)
//
// With inhibition enabled (default Breadth=7), B faces double suppression:
// conflict subtraction AND potential inhibition from stronger neighbors.
func TestConflictEdgePropagation(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{ID: "conf-seed", Name: "Seed", Kind: models.BehaviorKindDirective, Canonical: "Seed behavior"},
		{ID: "conf-a", Name: "Hub A", Kind: models.BehaviorKindDirective, Canonical: "Hub that distributes activation"},
		{ID: "conf-b", Name: "Conflict Target B", Kind: models.BehaviorKindDirective, Canonical: "Target connected via conflict edge"},
		{ID: "conf-c", Name: "Normal Target C", Kind: models.BehaviorKindDirective, Canonical: "Normal semantic target"},
		{ID: "conf-d", Name: "Normal Target D", Kind: models.BehaviorKindDirective, Canonical: "Normal semantic target control"},
	}

	edges := []simulation.EdgeSpec{
		{Source: "conf-seed", Target: "conf-a", Kind: "semantic", Weight: 0.9},
		{Source: "conf-a", Target: "conf-b", Kind: "conflicts", Weight: 0.8},
		{Source: "conf-a", Target: "conf-c", Kind: "semantic", Weight: 0.8},
		{Source: "conf-a", Target: "conf-d", Kind: "semantic", Weight: 0.8},
	}

	// Use default config (includes inhibition) with standard temporal decay.
	inh := spreading.DefaultInhibitionConfig()
	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.001,
		TemporalDecayRate: 0.01,
		Inhibition:        &inh,
	}

	sessions := make([]simulation.SessionContext, 10)

	scenario := simulation.Scenario{
		Name:           "conflict-edge-propagation",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianEnabled: false,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			return []spreading.Seed{
				{BehaviorID: "conf-seed", Activation: 0.8, Source: "test"},
			}
		},
	}

	result := r.Run(scenario)

	// Assertion 1: C and D have meaningful activation (normal spreading).
	simulation.AssertBehaviorSurfaces(t, result, "conf-c", 0.01)
	simulation.AssertBehaviorSurfaces(t, result, "conf-d", 0.01)

	// Assertion 2: B's activation < C's activation (conflict suppression).
	// Check across all sessions.
	for _, sr := range result.Sessions {
		bAct := getBehaviorActivation(sr, "conf-b")
		cAct := getBehaviorActivation(sr, "conf-c")
		if bAct > cAct && cAct > 0 {
			t.Errorf("session %d: conflict target B (%.6f) should not exceed normal target C (%.6f)", sr.Index, bAct, cAct)
		}
	}

	// Assertion 3: B's activation is never negative (floor at zero).
	for _, sr := range result.Sessions {
		bAct := getBehaviorActivation(sr, "conf-b")
		if bAct < 0 {
			t.Errorf("session %d: conflict target B has negative activation (%.6f)", sr.Index, bAct)
		}
	}

	// Assertion 4: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Log summary for debugging.
	lastSession := result.Sessions[len(result.Sessions)-1]
	t.Logf("Final session activations: B=%.6f, C=%.6f, D=%.6f",
		getBehaviorActivation(lastSession, "conf-b"),
		getBehaviorActivation(lastSession, "conf-c"),
		getBehaviorActivation(lastSession, "conf-d"))
}
