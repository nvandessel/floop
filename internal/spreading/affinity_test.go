package spreading

import (
	"math"
	"testing"
)

func TestVirtualAffinityEdges(t *testing.T) {
	config := DefaultAffinityConfig()

	allTags := map[string][]string{
		"b1": {"git", "worktree", "workflow"},
		"b2": {"git", "worktree"},
		"b3": {"testing", "tdd"},
		"b4": {"git", "pr"},
		"b5": {},
	}

	tests := []struct {
		name      string
		nodeID    string
		nodeTags  []string
		wantEdges int
	}{
		{
			name:      "high overlap finds neighbors",
			nodeID:    "b1",
			nodeTags:  allTags["b1"],
			wantEdges: 1, // b2 (Jaccard=2/3>0.3), not b4 (Jaccard=1/4<0.3), not b3, not b5
		},
		{
			name:      "no tags produces no edges",
			nodeID:    "b5",
			nodeTags:  nil,
			wantEdges: 0,
		},
		{
			name:      "disjoint tags produce no edges",
			nodeID:    "b3",
			nodeTags:  allTags["b3"],
			wantEdges: 0, // no overlap with git/worktree behaviors
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := virtualAffinityEdges(tt.nodeID, tt.nodeTags, allTags, config)
			if len(edges) != tt.wantEdges {
				t.Errorf("got %d edges, want %d", len(edges), tt.wantEdges)
				for _, e := range edges {
					t.Logf("  edge: %s → %s (weight=%.3f, kind=%s)", e.Source, e.Target, e.Weight, e.Kind)
				}
			}
		})
	}
}

func TestVirtualAffinityEdges_Weight(t *testing.T) {
	config := DefaultAffinityConfig()

	allTags := map[string][]string{
		"a": {"git", "worktree"},
		"b": {"git", "worktree"}, // identical tags → Jaccard 1.0
	}

	edges := virtualAffinityEdges("a", allTags["a"], allTags, config)
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}

	// Weight should be Jaccard(1.0) * MaxWeight(0.4) = 0.4
	if math.Abs(edges[0].Weight-0.4) > 1e-9 {
		t.Errorf("weight = %f, want 0.4", edges[0].Weight)
	}

	if edges[0].Kind != "feature-affinity" {
		t.Errorf("kind = %q, want %q", edges[0].Kind, "feature-affinity")
	}
}

func TestVirtualAffinityEdges_SelfExclusion(t *testing.T) {
	config := DefaultAffinityConfig()

	allTags := map[string][]string{
		"a": {"git", "worktree"},
	}

	edges := virtualAffinityEdges("a", allTags["a"], allTags, config)
	if len(edges) != 0 {
		t.Errorf("got %d edges, want 0 (self-exclusion)", len(edges))
	}
}

func TestVirtualAffinityEdges_MinJaccard(t *testing.T) {
	config := AffinityConfig{
		Enabled:    true,
		MaxWeight:  0.4,
		MinJaccard: 0.5, // stricter threshold
	}

	allTags := map[string][]string{
		"a": {"git", "worktree", "workflow"},
		"b": {"git"},                         // Jaccard = 1/3 ≈ 0.33 < 0.5
		"c": {"git", "worktree"},             // Jaccard = 2/3 ≈ 0.67 > 0.5
		"d": {"git", "worktree", "workflow"}, // Jaccard = 1.0 > 0.5
	}

	edges := virtualAffinityEdges("a", allTags["a"], allTags, config)
	if len(edges) != 2 { // c and d, not b
		t.Errorf("got %d edges, want 2", len(edges))
		for _, e := range edges {
			t.Logf("  edge: %s → %s (weight=%.3f)", e.Source, e.Target, e.Weight)
		}
	}
}

func TestDefaultAffinityConfig(t *testing.T) {
	c := DefaultAffinityConfig()
	if !c.Enabled {
		t.Error("Enabled should default to true")
	}
	if c.MaxWeight != 0.4 {
		t.Errorf("MaxWeight = %f, want 0.4", c.MaxWeight)
	}
	if c.MinJaccard != 0.3 {
		t.Errorf("MinJaccard = %f, want 0.3", c.MinJaccard)
	}
}
