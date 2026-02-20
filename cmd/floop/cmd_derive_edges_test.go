package main

import (
	"context"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestNewDeriveEdgesCmd(t *testing.T) {
	cmd := newDeriveEdgesCmd()

	if cmd.Use != "derive-edges" {
		t.Errorf("Use = %q, want derive-edges", cmd.Use)
	}

	// Verify flags exist
	for _, flag := range []string{"dry-run", "clear", "scope"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}

	// Verify default scope
	scope, _ := cmd.Flags().GetString("scope")
	if scope != "both" {
		t.Errorf("default scope = %q, want both", scope)
	}
}

func TestDeriveEdgesProposals(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Weighted scoring: when(0.4) + content(0.6) + tags(0.2), normalized by
	// sum of present weights. All three present → totalWeight = 1.2.
	//
	// b-general-go and b-go-related:
	//   when: both {"language":"go"} → overlap = 1.0
	//   content: partial word overlap → ~0.5
	//   tags: Jaccard(["go","errors"], ["go","api"]) = 1/3 = 0.33
	//   score ≈ 1.0*0.333 + 0.5*0.5 + 0.33*0.167 ≈ 0.64 → in [0.5, 0.9) ✓
	//
	// b-specific-go-test overrides b-general-go (superset when):
	//   when={language:go, task:testing} ⊃ {language:go}
	behaviors := []models.Behavior{
		{
			ID:   "b-general-go",
			Name: "Go error conventions",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "use error wrapping with fmt context propagation",
				Tags:      []string{"go", "errors"},
			},
			Confidence: 0.8,
		},
		{
			ID:   "b-specific-go-test",
			Name: "Go test conventions",
			When: map[string]interface{}{"language": "go", "task": "testing"},
			Content: models.BehaviorContent{
				Canonical: "use table driven tests with subtests and parallel execution",
				Tags:      []string{"go", "testing"},
			},
			Confidence: 0.8,
		},
		{
			ID:   "b-python",
			Name: "Python conventions",
			When: map[string]interface{}{"language": "python"},
			Content: models.BehaviorContent{
				Canonical: "use type hints and dataclasses for data models",
				Tags:      []string{"python", "typing"},
			},
			Confidence: 0.8,
		},
		{
			ID:   "b-go-related",
			Name: "Go error API patterns",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "use error wrapping and custom error types for API context",
				Tags:      []string{"go", "api"},
			},
			Confidence: 0.8,
		},
	}

	for _, b := range behaviors {
		node := models.BehaviorToNode(&b)
		_, err := s.AddNode(ctx, node)
		if err != nil {
			t.Fatalf("failed to add node %s: %v", b.ID, err)
		}
	}

	// Run derive-edges in dry-run mode
	result, err := deriveEdgesForStore(ctx, s, "test", true, false)
	if err != nil {
		t.Fatalf("deriveEdgesForStore failed: %v", err)
	}

	if result.Behaviors != 4 {
		t.Errorf("behaviors = %d, want 4", result.Behaviors)
	}

	// b-specific-go-test has when={language:go, task:testing} which is a
	// strict superset of b-general-go's when={language:go}, so overrides.
	foundOverrides := false
	for _, pe := range result.ProposedEdges {
		if pe.Source == "b-specific-go-test" && pe.Target == "b-general-go" && pe.Kind == "overrides" {
			foundOverrides = true
			if pe.Weight != 1.0 {
				t.Errorf("overrides weight = %v, want 1.0", pe.Weight)
			}
		}
	}
	if !foundOverrides {
		t.Error("expected overrides edge from b-specific-go-test -> b-general-go")
		for _, pe := range result.ProposedEdges {
			t.Logf("proposed: %s -> %s (%s, score=%.4f)", pe.Source, pe.Target, pe.Kind, pe.Score)
		}
	}

	// b-general-go and b-go-related have same when, partial content/tag overlap.
	// Score should be in [0.5, 0.9) → similar-to edge.
	foundSimilarTo := false
	for _, pe := range result.ProposedEdges {
		if pe.Kind == "similar-to" &&
			((pe.Source == "b-general-go" && pe.Target == "b-go-related") ||
				(pe.Source == "b-go-related" && pe.Target == "b-general-go")) {
			foundSimilarTo = true
		}
	}
	if !foundSimilarTo {
		t.Error("expected similar-to edge between b-general-go and b-go-related")
		for _, pe := range result.ProposedEdges {
			t.Logf("proposed: %s -> %s (%s, score=%.4f)", pe.Source, pe.Target, pe.Kind, pe.Score)
		}
		t.Logf("histogram: %v", result.Histogram)
	}

	// Python behavior should NOT have similar-to edges to Go behaviors (different language)
	for _, pe := range result.ProposedEdges {
		if pe.Kind == "similar-to" &&
			(pe.Source == "b-python" || pe.Target == "b-python") {
			t.Errorf("unexpected similar-to edge involving python behavior: %+v", pe)
		}
	}

	// Verify no edges were created (dry-run)
	if result.CreatedEdges != 0 {
		t.Errorf("created edges in dry-run = %d, want 0", result.CreatedEdges)
	}
}

func TestDeriveEdgesSkipsExisting(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Two behaviors with same when and partial content overlap → score in [0.5, 0.9)
	behaviors := []models.Behavior{
		{
			ID:   "b-a",
			Name: "Behavior A",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "use error wrapping with fmt context propagation",
				Tags:      []string{"go", "errors"},
			},
			Confidence: 0.8,
		},
		{
			ID:   "b-b",
			Name: "Behavior B",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "use error wrapping and custom error types for API context",
				Tags:      []string{"go", "api"},
			},
			Confidence: 0.8,
		},
	}

	now := time.Now()
	for _, b := range behaviors {
		node := models.BehaviorToNode(&b)
		s.AddNode(ctx, node)
	}

	// Pre-create a similar-to edge
	s.AddEdge(ctx, store.Edge{
		Source:    "b-a",
		Target:    "b-b",
		Kind:      "similar-to",
		Weight:    0.8,
		CreatedAt: now,
	})

	// Run derive — should skip the existing edge
	result, err := deriveEdgesForStore(ctx, s, "test", true, false)
	if err != nil {
		t.Fatalf("deriveEdgesForStore failed: %v", err)
	}

	// The similar-to edge should be skipped (already exists)
	for _, pe := range result.ProposedEdges {
		if pe.Source == "b-a" && pe.Target == "b-b" && pe.Kind == "similar-to" {
			t.Error("expected similar-to edge b-a -> b-b to be skipped (already exists)")
		}
	}
	if result.SkippedExisting == 0 {
		t.Error("expected at least one skipped edge")
		t.Logf("histogram: %v", result.Histogram)
		for _, pe := range result.ProposedEdges {
			t.Logf("proposed: %s -> %s (%s, score=%.4f)", pe.Source, pe.Target, pe.Kind, pe.Score)
		}
	}
}

func TestDeriveEdgesConnectivity(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	now := time.Now()

	// Create 3 behaviors
	for _, id := range []string{"b-1", "b-2", "b-3"} {
		b := models.Behavior{
			ID:   id,
			Name: id,
			Content: models.BehaviorContent{
				Canonical: "unique content for " + id,
			},
			Confidence: 0.8,
		}
		node := models.BehaviorToNode(&b)
		s.AddNode(ctx, node)
	}

	// Add edge between b-1 and b-2 only
	s.AddEdge(ctx, store.Edge{
		Source:    "b-1",
		Target:    "b-2",
		Kind:      "similar-to",
		Weight:    0.8,
		CreatedAt: now,
	})

	behaviors := []models.Behavior{
		{ID: "b-1"},
		{ID: "b-2"},
		{ID: "b-3"},
	}

	info := computeConnectivity(ctx, s, behaviors)

	if info.TotalNodes != 3 {
		t.Errorf("total nodes = %d, want 3", info.TotalNodes)
	}
	if info.Connected != 2 {
		t.Errorf("connected = %d, want 2 (b-1 and b-2 have edges)", info.Connected)
	}
	if info.Islands != 1 {
		t.Errorf("islands = %d, want 1 (b-3 has no edges)", info.Islands)
	}
}

func TestDeriveEdgesHistogram(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create identical behaviors → score = 1.0 → bucket 9
	behaviors := []models.Behavior{
		{
			ID:   "b-identical-1",
			Name: "Identical A",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "identical content here",
				Tags:      []string{"go"},
			},
			Confidence: 0.8,
		},
		{
			ID:   "b-identical-2",
			Name: "Identical B",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "identical content here",
				Tags:      []string{"go"},
			},
			Confidence: 0.8,
		},
	}

	for _, b := range behaviors {
		node := models.BehaviorToNode(&b)
		s.AddNode(ctx, node)
	}

	result, err := deriveEdgesForStore(ctx, s, "test", true, false)
	if err != nil {
		t.Fatalf("deriveEdgesForStore failed: %v", err)
	}

	// Identical behaviors should score 1.0, landing in bucket 9 [0.9-1.0]
	if result.Histogram[9] != 1 {
		t.Errorf("histogram[9] = %d, want 1 (identical pair)", result.Histogram[9])
		t.Logf("full histogram: %v", result.Histogram)
	}

	// Score >= 0.9 means NO similar-to edge (above upper bound)
	for _, pe := range result.ProposedEdges {
		if pe.Kind == "similar-to" {
			t.Errorf("unexpected similar-to edge for identical behaviors (score >= 0.9): %+v", pe)
		}
	}
}

func TestDeriveEdgesClear(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	now := time.Now()

	// Create two behaviors with a pre-existing edge
	for _, id := range []string{"b-x", "b-y"} {
		b := models.Behavior{
			ID:   id,
			Name: id,
			Content: models.BehaviorContent{
				Canonical: "unique content for " + id,
			},
			Confidence: 0.8,
		}
		node := models.BehaviorToNode(&b)
		s.AddNode(ctx, node)
	}

	s.AddEdge(ctx, store.Edge{
		Source: "b-x", Target: "b-y", Kind: "similar-to",
		Weight: 0.8, CreatedAt: now,
	})
	s.AddEdge(ctx, store.Edge{
		Source: "b-x", Target: "b-y", Kind: "requires",
		Weight: 0.5, CreatedAt: now,
	})

	// Run with --clear (not dry-run)
	result, err := deriveEdgesForStore(ctx, s, "test", false, true)
	if err != nil {
		t.Fatalf("deriveEdgesForStore failed: %v", err)
	}

	// Should have cleared the similar-to edge but NOT the requires edge
	if result.ClearedEdges != 1 {
		t.Errorf("cleared edges = %d, want 1 (only similar-to)", result.ClearedEdges)
	}

	// Verify requires edge still exists
	edges, _ := s.GetEdges(ctx, "b-x", store.DirectionOutbound, "requires")
	if len(edges) != 1 {
		t.Errorf("requires edges remaining = %d, want 1", len(edges))
	}
}

func TestDeriveEdgesTagOverlap(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Two behaviors with completely different content but 2 shared tags.
	// Overall similarity score will be low (~0.1), but the tag-overlap rule
	// should still create a similar-to edge because they share "git" and "worktree".
	behaviors := []models.Behavior{
		{
			ID:   "b-git-basics",
			Name: "Git branching basics",
			When: map[string]interface{}{},
			Content: models.BehaviorContent{
				Canonical: "always create feature branches for new work",
				Tags:      []string{"git", "worktree", "branching"},
			},
			Confidence: 0.8,
		},
		{
			ID:   "b-worktree-cleanup",
			Name: "Worktree cleanup",
			When: map[string]interface{}{},
			Content: models.BehaviorContent{
				Canonical: "remove stale worktrees after merging pull requests",
				Tags:      []string{"git", "worktree", "cleanup"},
			},
			Confidence: 0.8,
		},
		{
			ID:   "b-python-typing",
			Name: "Python typing",
			When: map[string]interface{}{},
			Content: models.BehaviorContent{
				Canonical: "use type hints for function signatures",
				Tags:      []string{"python", "typing"},
			},
			Confidence: 0.8,
		},
	}

	for _, b := range behaviors {
		node := models.BehaviorToNode(&b)
		s.AddNode(ctx, node)
	}

	result, err := deriveEdgesForStore(ctx, s, "test", true, false)
	if err != nil {
		t.Fatalf("deriveEdgesForStore failed: %v", err)
	}

	// git-basics and worktree-cleanup share 2 tags ("git", "worktree")
	// → should get a similar-to edge even with low content similarity
	foundTagEdge := false
	for _, pe := range result.ProposedEdges {
		if pe.Kind == "similar-to" &&
			((pe.Source == "b-git-basics" && pe.Target == "b-worktree-cleanup") ||
				(pe.Source == "b-worktree-cleanup" && pe.Target == "b-git-basics")) {
			foundTagEdge = true
		}
	}
	if !foundTagEdge {
		t.Error("expected similar-to edge between b-git-basics and b-worktree-cleanup (share 2+ tags)")
		for _, pe := range result.ProposedEdges {
			t.Logf("proposed: %s -> %s (%s, score=%.4f)", pe.Source, pe.Target, pe.Kind, pe.Score)
		}
	}

	// python-typing shares 0 tags with git behaviors → no edge
	for _, pe := range result.ProposedEdges {
		if pe.Kind == "similar-to" &&
			(pe.Source == "b-python-typing" || pe.Target == "b-python-typing") {
			t.Errorf("unexpected similar-to edge involving python-typing: %+v", pe)
		}
	}
}
