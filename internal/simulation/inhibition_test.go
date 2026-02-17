package simulation_test

import (
	"sort"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// TestLateralInhibitionIsolation validates that lateral inhibition creates
// sharp winner/loser separation in a multi-session scenario using default
// production config (Strength=0.15, Breadth=7).
//
// Setup:
//   - 1 hub seed with 11 non-seed neighbors connected via semantic edges
//   - 4 "strong" neighbors: semantic weight 0.9 (should become winners)
//   - 3 "medium" neighbors: semantic weight 0.6 (2 winners, 1 loser at boundary)
//   - 4 "weak" neighbors: semantic weight 0.2 (all losers, suppressed)
//   - Default spread config with Inhibition enabled (Strength=0.15, Breadth=7)
//   - HebbianEnabled=false to isolate inhibition dynamics
//   - 10 sessions, same seed every time
//
// Key insight: Breadth=7 means 7 total winners INCLUDING hub. So 6 non-hub
// nodes are winners (4 strong + 2 medium), and 5 are losers (1 medium + 4 weak).
// Inhibition equalizes loser activations downward, creating a visible cliff
// between the last winner and first loser.
//
// At default spread settings with 11 outbound edges, out-degree dilution
// keeps post-sigmoid activations in a narrow range (~0.05-0.06). The test
// validates relative ordering and the cliff property rather than absolute values.
func TestLateralInhibitionIsolation(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{ID: "hub", Name: "Hub Seed", Kind: models.BehaviorKindDirective, Canonical: "Central hub"},
	}

	strongIDs := []string{"strong-1", "strong-2", "strong-3", "strong-4"}
	mediumIDs := []string{"medium-1", "medium-2", "medium-3"}
	weakIDs := []string{"weak-1", "weak-2", "weak-3", "weak-4"}

	for _, id := range strongIDs {
		behaviors = append(behaviors, simulation.BehaviorSpec{
			ID: id, Name: "Strong " + id, Kind: models.BehaviorKindDirective, Canonical: "Strong neighbor " + id,
		})
	}
	for _, id := range mediumIDs {
		behaviors = append(behaviors, simulation.BehaviorSpec{
			ID: id, Name: "Medium " + id, Kind: models.BehaviorKindDirective, Canonical: "Medium neighbor " + id,
		})
	}
	for _, id := range weakIDs {
		behaviors = append(behaviors, simulation.BehaviorSpec{
			ID: id, Name: "Weak " + id, Kind: models.BehaviorKindDirective, Canonical: "Weak neighbor " + id,
		})
	}

	// Hub → all 11 neighbors via semantic edges with varying weights.
	var edges []simulation.EdgeSpec
	for _, id := range strongIDs {
		edges = append(edges, simulation.EdgeSpec{Source: "hub", Target: id, Kind: "semantic", Weight: 0.9})
	}
	for _, id := range mediumIDs {
		edges = append(edges, simulation.EdgeSpec{Source: "hub", Target: id, Kind: "semantic", Weight: 0.6})
	}
	for _, id := range weakIDs {
		edges = append(edges, simulation.EdgeSpec{Source: "hub", Target: id, Kind: "semantic", Weight: 0.2})
	}

	sessions := make([]simulation.SessionContext, 10)

	// Default spread config includes inhibition (Strength=0.15, Breadth=7).
	spreadCfg := spreading.DefaultConfig()

	scenario := simulation.Scenario{
		Name:           "lateral-inhibition-isolation",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianEnabled: false,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			return []spreading.Seed{
				{BehaviorID: "hub", Activation: 0.8, Source: "test"},
			}
		},
	}

	result := r.Run(scenario)

	// Log session results for debugging.
	for _, sr := range result.Sessions {
		t.Logf("%s", simulation.FormatSessionDebug(sr))
	}

	// --- Assertion 1: Strong neighbors consistently outrank weak neighbors ---
	for _, sr := range result.Sessions {
		simulation.AssertInhibitionGap(t, sr, strongIDs, weakIDs, 0.005)
	}

	// --- Assertion 2: Cliff at the winner/loser boundary ---
	// Breadth=7 includes hub, so 6 non-hub nodes are winners.
	// The cliff should be between the 6th and 7th ranked non-hub behaviors.
	lastSession := result.Sessions[len(result.Sessions)-1]
	type rankedResult struct {
		id         string
		activation float64
	}
	var ranked []rankedResult
	for _, res := range lastSession.Results {
		if res.BehaviorID != "hub" {
			ranked = append(ranked, rankedResult{id: res.BehaviorID, activation: res.Activation})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].activation > ranked[j].activation
	})

	t.Logf("Ranked activations (excluding hub):")
	for i, r := range ranked {
		t.Logf("  rank %d: %s = %.4f", i+1, r.id, r.activation)
	}

	// With Breadth=7 (hub + 6 non-hub winners), the cliff is between
	// ranked[5] (last winner) and ranked[6] (first loser).
	if len(ranked) >= 8 {
		// Gap between adjacent winners (e.g., ranked[4] → ranked[5])
		adjacentWinnerGap := ranked[4].activation - ranked[5].activation
		// Gap at the cliff (ranked[5] → ranked[6])
		cliffGap := ranked[5].activation - ranked[6].activation

		t.Logf("Adjacent winner gap (rank 5→6): %.4f", adjacentWinnerGap)
		t.Logf("Cliff gap (rank 6→7): %.4f", cliffGap)

		if cliffGap <= adjacentWinnerGap {
			t.Errorf("expected cliff gap (%.4f) > adjacent winner gap (%.4f) — inhibition should create a sharper boundary at rank 6→7",
				cliffGap, adjacentWinnerGap)
		}
	}

	// --- Assertion 3: Inhibition equalizes losers ---
	// medium-3 (a natural "medium" that falls outside Breadth) should be
	// suppressed to approximately the same level as weak nodes, demonstrating
	// that inhibition pushes losers down regardless of their natural weight.
	activationMap := make(map[string]float64)
	for _, res := range lastSession.Results {
		activationMap[res.BehaviorID] = res.Activation
	}

	// medium-1 and medium-2 should be at winner level (like each other)
	// medium-3 should be at loser level (like weak nodes)
	if actM1, ok1 := activationMap["medium-1"]; ok1 {
		if actM3, ok3 := activationMap["medium-3"]; ok3 {
			if actW1, okW := activationMap["weak-1"]; okW {
				// medium-3 should be closer to weak-1 than to medium-1
				distToWinner := actM1 - actM3
				distToLoser := actM3 - actW1
				t.Logf("medium-3 distance to winner (medium-1): %.4f, to loser (weak-1): %.4f", distToWinner, distToLoser)
				if distToLoser > distToWinner {
					t.Errorf("medium-3 (%.4f) closer to winners (%.4f) than losers (%.4f) — inhibition should push it to loser level",
						actM3, actM1, actW1)
				}
			}
		}
	}

	// --- Assertion 4: All sessions produce results ---
	simulation.AssertResultsNotEmpty(t, result)
}
