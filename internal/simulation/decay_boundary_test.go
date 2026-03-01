package simulation_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/simulation"
	"github.com/nvandessel/floop/internal/spreading"
	"github.com/nvandessel/floop/internal/store"
)

// TestAncientEdgeDecayBoundary validates that edges untouched for extreme
// durations (30 days, 365 days) decay to effectively zero without causing
// numerical issues (NaN, Inf, panic).
//
// With TemporalDecayRate=0.01:
//   - Fresh edge: full weight (LastActivated touched each session)
//   - 30-day edge (720h): e^(-0.01*720) ≈ 0.00074 (near-zero)
//   - 365-day edge (8760h): e^(-0.01*8760) ≈ 1.27e-38 (underflow territory)
//
// The test creates a seed and three targets with co-activated edges.
// BeforeSession at session 0 backdates the old and ancient edges.
// The fresh edge is touched normally by the runner each session.
func TestAncientEdgeDecayBoundary(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{ID: "decay-seed", Name: "Seed", Kind: models.BehaviorKindDirective, Canonical: "Seed behavior for decay boundary test"},
		{ID: "fresh-target", Name: "Fresh Target", Kind: models.BehaviorKindDirective, Canonical: "Target with fresh edge"},
		{ID: "old-target", Name: "Old Target", Kind: models.BehaviorKindDirective, Canonical: "Target with 30-day-old edge"},
		{ID: "ancient-target", Name: "Ancient Target", Kind: models.BehaviorKindDirective, Canonical: "Target with 365-day-old edge"},
	}

	edges := []simulation.EdgeSpec{
		{Source: "decay-seed", Target: "fresh-target", Kind: "co-activated", Weight: 0.5},
		{Source: "decay-seed", Target: "old-target", Kind: "co-activated", Weight: 0.5},
		{Source: "decay-seed", Target: "ancient-target", Kind: "co-activated", Weight: 0.5},
	}

	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.001, // Low threshold to detect near-zero values
		TemporalDecayRate: 0.01,
	}

	sessions := make([]simulation.SessionContext, 10)

	scenario := simulation.Scenario{
		Name:           "ancient-edge-decay-boundary",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianEnabled: false,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			return []spreading.Seed{
				{BehaviorID: "decay-seed", Activation: 0.8, Source: "test"},
			}
		},
		BeforeSession: func(sessionIndex int, s *store.SQLiteGraphStore) {
			if sessionIndex != 0 {
				return
			}
			ctx := context.Background()

			// Backdate old-target edge to 30 days ago
			thirtyDaysAgo := time.Now().Add(-30 * 24 * time.Hour)
			if err := s.AddEdge(ctx, store.Edge{
				Source:        "decay-seed",
				Target:        "old-target",
				Kind:          store.EdgeKindCoActivated,
				Weight:        0.5,
				CreatedAt:     time.Now().Add(-31 * 24 * time.Hour),
				LastActivated: &thirtyDaysAgo,
			}); err != nil {
				t.Fatalf("BeforeSession: failed to backdate old edge: %v", err)
			}

			// Backdate ancient-target edge to 365 days ago
			yearAgo := time.Now().Add(-365 * 24 * time.Hour)
			if err := s.AddEdge(ctx, store.Edge{
				Source:        "decay-seed",
				Target:        "ancient-target",
				Kind:          store.EdgeKindCoActivated,
				Weight:        0.5,
				CreatedAt:     time.Now().Add(-366 * 24 * time.Hour),
				LastActivated: &yearAgo,
			}); err != nil {
				t.Fatalf("BeforeSession: failed to backdate ancient edge: %v", err)
			}
		},
	}

	result := r.Run(scenario)

	// Assertion 1: Fresh target gets meaningful activation.
	simulation.AssertBehaviorSurfaces(t, result, "fresh-target", 0.01)

	// Assertion 2: No NaN or Inf in any result across all sessions.
	for _, sr := range result.Sessions {
		for _, res := range sr.Results {
			if math.IsNaN(res.Activation) {
				t.Errorf("session %d: behavior %s has NaN activation", sr.Index, res.BehaviorID)
			}
			if math.IsInf(res.Activation, 0) {
				t.Errorf("session %d: behavior %s has Inf activation", sr.Index, res.BehaviorID)
			}
		}
	}

	// Assertion 3: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Assertion 4: Fresh target activation > old target activation in session 0.
	// The 30-day backdating causes massive decay (e^(-0.01*720) ≈ 0.00074).
	freshAct := getBehaviorActivation(result.Sessions[0], "fresh-target")
	oldAct := getBehaviorActivation(result.Sessions[0], "old-target")
	ancientAct := getBehaviorActivation(result.Sessions[0], "ancient-target")
	t.Logf("Session 0 activations: fresh=%.6f, old=%.6f, ancient=%.6f", freshAct, oldAct, ancientAct)

	if freshAct <= oldAct {
		t.Errorf("fresh activation (%.6f) should exceed old activation (%.6f)", freshAct, oldAct)
	}

	// Assertion 5: Ancient target should be at the sigmoid floor (~0.047).
	// The raw activation is effectively zero after 365 days of decay, but the
	// sigmoid function maps 0 → ~0.047 (the sigmoid baseline). The key signal
	// is that fresh >> ancient, and ancient ≈ old (both decayed to zero raw).
	sigmoidFloor := 0.06 // generous upper bound on sigmoid(0)
	if ancientAct > sigmoidFloor {
		t.Errorf("ancient activation (%.6f) should be at sigmoid floor (< %.3f)", ancientAct, sigmoidFloor)
	}

	// Assertion 6: Old and ancient activations should be approximately equal
	// (both at sigmoid floor — their raw activations are both effectively zero).
	diff := math.Abs(oldAct - ancientAct)
	if diff > 0.01 {
		t.Errorf("old (%.6f) and ancient (%.6f) should be approximately equal (diff=%.6f)", oldAct, ancientAct, diff)
	}
}
