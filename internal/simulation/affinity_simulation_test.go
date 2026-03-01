package simulation_test

import (
	"context"
	"testing"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/simulation"
	"github.com/nvandessel/floop/internal/spreading"
)

// mapTagProvider is a test-only TagProvider that returns hardcoded tags.
// This allows testing affinity without access to the runner's internal store.
type mapTagProvider struct {
	tags map[string][]string
}

func (p *mapTagProvider) GetAllBehaviorTags(_ context.Context) map[string][]string {
	return p.tags
}

// TestAffinityVirtualEdgesInSpreading validates that the affinity subsystem
// generates virtual edges from shared tags and that these edges carry
// activation during spreading.
//
// Setup:
//   - 4 behaviors with tag patterns:
//     A (seed): tags [git, worktree, workflow]
//     B: tags [git, worktree] — Jaccard(A,B) = 2/3 ≈ 0.67 > MinJaccard(0.3)
//     C: tags [testing, ci] — Jaccard(A,C) = 0 < MinJaccard → no edge
//     D: tags [git] — Jaccard(A,D) = 1/3 ≈ 0.33 > MinJaccard(0.3), borderline
//   - No explicit edges — activation flows ONLY through virtual affinity edges
//   - AffinityConfig enabled in SpreadConfig
func TestAffinityVirtualEdgesInSpreading(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{ID: "affinity-a", Name: "Behavior A", Kind: models.BehaviorKindDirective, Canonical: "Git worktree workflow", Tags: []string{"git", "worktree", "workflow"}},
		{ID: "affinity-b", Name: "Behavior B", Kind: models.BehaviorKindDirective, Canonical: "Git worktree usage", Tags: []string{"git", "worktree"}},
		{ID: "affinity-c", Name: "Behavior C", Kind: models.BehaviorKindDirective, Canonical: "CI testing pipeline", Tags: []string{"testing", "ci"}},
		{ID: "affinity-d", Name: "Behavior D", Kind: models.BehaviorKindDirective, Canonical: "Git basics", Tags: []string{"git"}},
	}

	// NO explicit edges — all activation must flow through virtual affinity edges.
	var edges []simulation.EdgeSpec

	tagMap := map[string][]string{
		"affinity-a": {"git", "worktree", "workflow"},
		"affinity-b": {"git", "worktree"},
		"affinity-c": {"testing", "ci"},
		"affinity-d": {"git"},
	}

	affinityConfig := spreading.DefaultAffinityConfig()
	spreadCfg := spreading.Config{
		MaxSteps:          3,
		DecayFactor:       0.85,
		SpreadFactor:      0.95,
		MinActivation:     0.001,
		TemporalDecayRate: 0.01,
		Affinity:          &affinityConfig,
		TagProvider:       &mapTagProvider{tags: tagMap},
	}

	sessions := make([]simulation.SessionContext, 10)

	scenario := simulation.Scenario{
		Name:           "affinity-virtual-edges",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		SpreadConfig:   &spreadCfg,
		HebbianEnabled: false,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			return []spreading.Seed{
				{BehaviorID: "affinity-a", Activation: 0.8, Source: "test"},
			}
		},
	}

	result := r.Run(scenario)

	// Assertion 1: B receives activation (Jaccard 0.67 > MinJaccard 0.3).
	simulation.AssertBehaviorSurfaces(t, result, "affinity-b", 0.01)

	// Assertion 2: C receives NO meaningful activation (Jaccard 0.0).
	for _, sr := range result.Sessions {
		cAct := getBehaviorActivation(sr, "affinity-c")
		if cAct > 0.01 {
			t.Errorf("session %d: C activation (%.6f) should be near-zero (Jaccard=0, no virtual edge)", sr.Index, cAct)
		}
	}

	// Assertion 3: D receives some activation (Jaccard 0.33, borderline > 0.3).
	simulation.AssertBehaviorSurfaces(t, result, "affinity-d", 0.001)

	// Assertion 4: B's activation > D's activation (higher Jaccard → stronger virtual edge).
	bMax := 0.0
	dMax := 0.0
	for _, sr := range result.Sessions {
		bAct := getBehaviorActivation(sr, "affinity-b")
		dAct := getBehaviorActivation(sr, "affinity-d")
		if bAct > bMax {
			bMax = bAct
		}
		if dAct > dMax {
			dMax = dAct
		}
	}
	t.Logf("Max activations: B=%.6f, D=%.6f", bMax, dMax)
	if bMax <= dMax {
		t.Errorf("B max activation (%.6f) should exceed D max activation (%.6f)", bMax, dMax)
	}

	// Assertion 5: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)
}
