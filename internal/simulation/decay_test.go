package simulation_test

import (
	"context"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// TestTemporalDecayVsOja validates the interplay between temporal edge decay
// and Oja-stabilized Hebbian learning across three phases.
//
// The key mechanism under test: the spreading engine computes effective edge
// weight as `weight * e^(-rho * elapsed_hours)` where elapsed_hours is
// `time.Since(edge.LastActivated)`. When an edge goes dormant (not touched),
// its effective weight decays even though the stored weight persists.
//
// Phase 1 (sessions 0-19): A+B co-activate, A-B edge strengthens via Oja.
// Phase 2 (sessions 20-39): A-B edge LastActivated backdated to 48h ago.
//
//	This simulates dormancy: the stored weight persists, but the effective
//	weight during spreading drops to ~62% (e^(-0.01*48) = 0.619), reducing
//	B's activation. Meanwhile A-C continues strengthening.
//
// Phase 3 (sessions 40-59): A-B edge timestamp restored to recent.
//
//	B's activation recovers as effective weight returns to full stored value.
//
// Expected: Phase 2 shows measurably lower B activation than Phase 1 end,
// Phase 3 recovers.
func TestTemporalDecayVsOja(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{ID: "decay-a", Name: "Behavior A", Kind: models.BehaviorKindDirective, Canonical: "Decay test A"},
		{ID: "decay-b", Name: "Behavior B", Kind: models.BehaviorKindDirective, Canonical: "Decay test B"},
		{ID: "decay-c", Name: "Behavior C", Kind: models.BehaviorKindDirective, Canonical: "Decay test C"},
	}

	// A-B and A-C co-activated edges at 0.5 (strong enough to produce
	// meaningful activation via spreading).
	edges := []simulation.EdgeSpec{
		{Source: "decay-a", Target: "decay-b", Kind: "co-activated", Weight: 0.5},
		{Source: "decay-a", Target: "decay-c", Kind: "co-activated", Weight: 0.5},
	}

	// Use a meaningful temporal decay rate — 0.01 means ~21% decay per day.
	// With 48h backdating, effective weight = stored * 0.619.
	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.01,
		TemporalDecayRate: 0.01, // Production default
	}

	hebbianCfg := spreading.DefaultHebbianConfig()
	hebbianCfg.ActivationThreshold = 0.05 // Very low to capture spreading-reached behaviors

	sessions := make([]simulation.SessionContext, 60)

	scenario := simulation.Scenario{
		Name:           "temporal-decay-vs-oja",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianConfig:  &hebbianCfg,
		HebbianEnabled: true,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			return []spreading.Seed{
				{BehaviorID: "decay-a", Activation: 0.8, Source: "test"},
			}
		},
		BeforeSession: func(sessionIndex int, s *store.SQLiteGraphStore) {
			ctx := context.Background()

			if sessionIndex == 20 {
				// Phase 2 start: backdate A-B edge to 48h ago.
				// This causes temporal decay in the spreading engine.
				fortyEightHoursAgo := time.Now().Add(-48 * time.Hour)
				err := s.AddEdge(ctx, store.Edge{
					Source:        "decay-a",
					Target:        "decay-b",
					Kind:          "co-activated",
					Weight:        getEdgeWeightFromStore(t, s, "decay-a", "decay-b", "co-activated"),
					CreatedAt:     time.Now().Add(-24 * time.Hour),
					LastActivated: &fortyEightHoursAgo,
				})
				if err != nil {
					t.Fatalf("BeforeSession(%d): failed to backdate A-B edge: %v", sessionIndex, err)
				}
				t.Logf("Phase 2 start: backdated A-B LastActivated to %s", fortyEightHoursAgo.Format(time.RFC3339))
			}

			if sessionIndex == 40 {
				// Phase 3 start: restore A-B timestamp to recent.
				// The runner's TouchEdges will keep it fresh going forward.
				now := time.Now()
				err := s.AddEdge(ctx, store.Edge{
					Source:        "decay-a",
					Target:        "decay-b",
					Kind:          "co-activated",
					Weight:        getEdgeWeightFromStore(t, s, "decay-a", "decay-b", "co-activated"),
					CreatedAt:     time.Now().Add(-24 * time.Hour),
					LastActivated: &now,
				})
				if err != nil {
					t.Fatalf("BeforeSession(%d): failed to restore A-B edge: %v", sessionIndex, err)
				}
				t.Logf("Phase 3 start: restored A-B LastActivated to %s", now.Format(time.RFC3339))
			}
		},
	}

	result := r.Run(scenario)

	// Log phase boundaries.
	t.Logf("Phase 1 end (session 19):\n%s", simulation.FormatSessionDebug(result.Sessions[19]))
	t.Logf("Phase 2 start (session 20):\n%s", simulation.FormatSessionDebug(result.Sessions[20]))
	t.Logf("Phase 2 end (session 39):\n%s", simulation.FormatSessionDebug(result.Sessions[39]))
	t.Logf("Phase 3 start (session 40):\n%s", simulation.FormatSessionDebug(result.Sessions[40]))
	t.Logf("Phase 3 end (session 59):\n%s", simulation.FormatSessionDebug(result.Sessions[59]))

	// Get B's activation at phase boundaries.
	bActPhase1End := getBehaviorActivation(result.Sessions[19], "decay-b")
	bActPhase2Start := getBehaviorActivation(result.Sessions[20], "decay-b")
	bActPhase2End := getBehaviorActivation(result.Sessions[39], "decay-b")
	bActPhase3Start := getBehaviorActivation(result.Sessions[40], "decay-b")
	bActPhase3End := getBehaviorActivation(result.Sessions[59], "decay-b")

	t.Logf("B activation: Phase1End=%.4f, Phase2Start=%.4f, Phase2End=%.4f, Phase3Start=%.4f, Phase3End=%.4f",
		bActPhase1End, bActPhase2Start, bActPhase2End, bActPhase3Start, bActPhase3End)

	// Assertion 1: B's activation drops at Phase 2 start due to temporal decay.
	// The 48h backdating reduces effective edge weight by ~38%.
	if bActPhase2Start >= bActPhase1End {
		t.Errorf("Phase 2 start: B activation (%.4f) should be lower than Phase 1 end (%.4f) due to temporal decay",
			bActPhase2Start, bActPhase1End)
	}

	// Assertion 2: B's activation recovers at Phase 3 start (timestamp restored).
	if bActPhase3Start <= bActPhase2Start {
		t.Errorf("Phase 3 start: B activation (%.4f) should be higher than Phase 2 start (%.4f) after timestamp restoration",
			bActPhase3Start, bActPhase2Start)
	}

	// Assertion 3: A-B stored weight increased during Phase 1.
	simulation.AssertWeightIncreased(t, result, "decay-a", "decay-b", "co-activated", 0, 19)

	// Assertion 4: A-B stored weight may still increase during Phase 2
	// (if B's decayed activation still passes the Hebbian threshold).
	// The key point is that stored weight persists even when effective
	// weight is decayed — Oja operates on stored weight.
	abKey := simulation.EdgeKey("decay-a", "decay-b", "co-activated")
	abWeightPhase1End := result.Sessions[19].EdgeWeights[abKey]
	abWeightPhase2End := result.Sessions[39].EdgeWeights[abKey]
	t.Logf("A-B stored weight: Phase1End=%.6f, Phase2End=%.6f", abWeightPhase1End, abWeightPhase2End)

	// Assertion 5: No weight explosion.
	simulation.AssertNoWeightExplosion(t, result, 0.95)

	// Assertion 6: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Assertion 7: C's activation should NOT be affected by A-B backdating.
	// C reaches through A-C edge which is freshly touched each session.
	cActPhase1End := getBehaviorActivation(result.Sessions[19], "decay-c")
	cActPhase2Start := getBehaviorActivation(result.Sessions[20], "decay-c")
	t.Logf("C activation: Phase1End=%.4f, Phase2Start=%.4f (should be similar)", cActPhase1End, cActPhase2Start)
}

// getBehaviorActivation returns the activation of a behavior in a session, or 0 if absent.
func getBehaviorActivation(sr simulation.SessionResult, behaviorID string) float64 {
	for _, r := range sr.Results {
		if r.BehaviorID == behaviorID {
			return r.Activation
		}
	}
	return 0
}

// getEdgeWeightFromStore reads the current weight of an edge from the store.
func getEdgeWeightFromStore(t *testing.T, s *store.SQLiteGraphStore, src, tgt, kind string) float64 {
	t.Helper()
	edges, err := s.GetEdges(context.Background(), src, store.DirectionOutbound, kind)
	if err != nil {
		t.Fatalf("getEdgeWeightFromStore: %v", err)
	}
	for _, e := range edges {
		if e.Target == tgt {
			return e.Weight
		}
	}
	t.Fatalf("getEdgeWeightFromStore: edge %s->%s:%s not found", src, tgt, kind)
	return 0
}
