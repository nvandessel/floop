package edges

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

func addBehaviorToStore(t *testing.T, ctx context.Context, s store.GraphStore, b models.Behavior) {
	t.Helper()
	node := models.BehaviorToNode(&b)
	if _, err := s.AddNode(ctx, node); err != nil {
		t.Fatalf("failed to add node %s: %v", b.ID, err)
	}
}

func TestDeriveEdgesForStore(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	behaviors := []models.Behavior{
		{
			ID:   "b-go-errors",
			Name: "Go error conventions",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "use error wrapping with fmt context propagation",
				Tags:      []string{"go", "errors"},
			},
			Confidence: 0.8,
		},
		{
			ID:   "b-go-api",
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
		addBehaviorToStore(t, ctx, s, b)
	}

	// Run derivation (not dry-run)
	result, err := DeriveEdgesForStore(ctx, s, "test", false, false)
	if err != nil {
		t.Fatalf("DeriveEdgesForStore() error = %v", err)
	}

	if result.Behaviors != 2 {
		t.Errorf("Behaviors = %d, want 2", result.Behaviors)
	}

	// Should have created edges (these behaviors have shared "go" tag and similar content)
	if result.CreatedEdges == 0 {
		t.Error("expected at least one created edge")
		for _, pe := range result.ProposedEdges {
			t.Logf("proposed: %s -> %s (%s, score=%.4f)", pe.Source, pe.Target, pe.Kind, pe.Score)
		}
	}
}

func TestDeriveEdgesForSubset_SkipsExistingExisting(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create 3 behaviors: 2 existing + 1 new
	existing1 := models.Behavior{
		ID:   "b-existing-1",
		Name: "Existing 1",
		When: map[string]interface{}{"language": "go"},
		Content: models.BehaviorContent{
			Canonical: "use error wrapping with fmt context propagation",
			Tags:      []string{"go", "errors"},
		},
		Confidence: 0.8,
	}
	existing2 := models.Behavior{
		ID:   "b-existing-2",
		Name: "Existing 2",
		When: map[string]interface{}{"language": "go"},
		Content: models.BehaviorContent{
			Canonical: "use error wrapping and custom error types for API context",
			Tags:      []string{"go", "api"},
		},
		Confidence: 0.8,
	}
	newBehavior := models.Behavior{
		ID:   "b-new-1",
		Name: "New behavior",
		When: map[string]interface{}{"language": "python"},
		Content: models.BehaviorContent{
			Canonical: "use type hints for all function parameters",
			Tags:      []string{"python", "typing"},
		},
		Confidence: 0.8,
	}

	for _, b := range []models.Behavior{existing1, existing2, newBehavior} {
		addBehaviorToStore(t, ctx, s, b)
	}

	allBehaviors := []models.Behavior{existing1, existing2, newBehavior}
	newIDs := []string{"b-new-1"}

	result, err := DeriveEdgesForSubset(ctx, s, newIDs, allBehaviors)
	if err != nil {
		t.Fatalf("DeriveEdgesForSubset() error = %v", err)
	}

	// Verify that no edges were created between the two existing behaviors
	for _, pe := range result.ProposedEdges {
		if (pe.Source == "b-existing-1" && pe.Target == "b-existing-2") ||
			(pe.Source == "b-existing-2" && pe.Target == "b-existing-1") {
			t.Errorf("unexpected edge between existing behaviors: %s -> %s (%s)", pe.Source, pe.Target, pe.Kind)
		}
	}

	// Pairs compared should reflect subset: new*existing + new*(new-1)/2 = 1*2 + 0 = 2
	if result.PairsCompared != 2 {
		t.Errorf("PairsCompared = %d, want 2", result.PairsCompared)
	}
}

func TestDeriveEdgesForSubset_CreatesNewExisting(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create an existing behavior and a new one that are similar
	existing := models.Behavior{
		ID:   "b-existing",
		Name: "Go error conventions",
		When: map[string]interface{}{"language": "go"},
		Content: models.BehaviorContent{
			Canonical: "use error wrapping with fmt context propagation",
			Tags:      []string{"go", "errors"},
		},
		Confidence: 0.8,
	}
	newBehavior := models.Behavior{
		ID:   "b-new",
		Name: "Go error API patterns",
		When: map[string]interface{}{"language": "go"},
		Content: models.BehaviorContent{
			Canonical: "use error wrapping and custom error types for API context",
			Tags:      []string{"go", "api"},
		},
		Confidence: 0.8,
	}

	for _, b := range []models.Behavior{existing, newBehavior} {
		addBehaviorToStore(t, ctx, s, b)
	}

	allBehaviors := []models.Behavior{existing, newBehavior}
	newIDs := []string{"b-new"}

	result, err := DeriveEdgesForSubset(ctx, s, newIDs, allBehaviors)
	if err != nil {
		t.Fatalf("DeriveEdgesForSubset() error = %v", err)
	}

	// These behaviors have shared "go" tag and overlapping content, should create edges
	if result.EdgesCreated == 0 {
		t.Error("expected at least one edge between new and existing behaviors")
	}

	// Verify at least one proposed edge involves both the new and existing
	foundNewExisting := false
	for _, pe := range result.ProposedEdges {
		if (pe.Source == "b-new" && pe.Target == "b-existing") ||
			(pe.Source == "b-existing" && pe.Target == "b-new") {
			foundNewExisting = true
		}
	}
	if !foundNewExisting {
		t.Error("expected edge between b-new and b-existing")
	}
}

func TestDeriveEdgesForSubset_CreatesNewNew(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Two new behaviors that are similar to each other
	new1 := models.Behavior{
		ID:   "b-new-1",
		Name: "Git branching",
		When: map[string]interface{}{},
		Content: models.BehaviorContent{
			Canonical: "always create feature branches for new work",
			Tags:      []string{"git", "worktree", "branching"},
		},
		Confidence: 0.8,
	}
	new2 := models.Behavior{
		ID:   "b-new-2",
		Name: "Worktree cleanup",
		When: map[string]interface{}{},
		Content: models.BehaviorContent{
			Canonical: "remove stale worktrees after merging pull requests",
			Tags:      []string{"git", "worktree", "cleanup"},
		},
		Confidence: 0.8,
	}

	for _, b := range []models.Behavior{new1, new2} {
		addBehaviorToStore(t, ctx, s, b)
	}

	allBehaviors := []models.Behavior{new1, new2}
	newIDs := []string{"b-new-1", "b-new-2"}

	result, err := DeriveEdgesForSubset(ctx, s, newIDs, allBehaviors)
	if err != nil {
		t.Fatalf("DeriveEdgesForSubset() error = %v", err)
	}

	// Both new behaviors share 2 tags ("git", "worktree") -> similar-to edge
	foundNewNew := false
	for _, pe := range result.ProposedEdges {
		if pe.Kind == store.EdgeKindSimilarTo &&
			((pe.Source == "b-new-1" && pe.Target == "b-new-2") ||
				(pe.Source == "b-new-2" && pe.Target == "b-new-1")) {
			foundNewNew = true
		}
	}
	if !foundNewNew {
		t.Error("expected similar-to edge between b-new-1 and b-new-2 (share 2+ tags)")
		for _, pe := range result.ProposedEdges {
			t.Logf("proposed: %s -> %s (%s, score=%.4f)", pe.Source, pe.Target, pe.Kind, pe.Score)
		}
	}
}

func TestDeriveEdgesForSubset_PerformanceGuard(t *testing.T) {
	// Capture stderr to verify the warning is printed
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// We need newIDs * existingCount > 10000
	// Create 2 new IDs and a list of 5002 total behaviors (5000 existing + 2 new)
	// 2 * 5000 = 10000, so we need > 10000 => 2 * 5001 = 10002
	allBehaviors := make([]models.Behavior, 0, 5003)
	for i := 0; i < 5001; i++ {
		b := models.Behavior{
			ID:   fmt.Sprintf("b-existing-%d", i),
			Name: fmt.Sprintf("Existing %d", i),
			Content: models.BehaviorContent{
				Canonical: fmt.Sprintf("unique content %d", i),
			},
			Confidence: 0.8,
		}
		allBehaviors = append(allBehaviors, b)
	}
	// Add 2 new behaviors
	for i := 0; i < 2; i++ {
		b := models.Behavior{
			ID:   fmt.Sprintf("b-new-%d", i),
			Name: fmt.Sprintf("New %d", i),
			Content: models.BehaviorContent{
				Canonical: fmt.Sprintf("new unique content %d", i),
			},
			Confidence: 0.8,
		}
		allBehaviors = append(allBehaviors, b)
	}

	newIDs := []string{"b-new-0", "b-new-1"}

	// We don't actually need to add all to the store for the warning check,
	// but we need the function to run
	_, _ = DeriveEdgesForSubset(ctx, s, newIDs, allBehaviors)

	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	os.Stderr = oldStderr

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("warning: large comparison set")) {
		t.Errorf("expected performance guard warning, got: %q", output)
	}
}

func TestClearDerivedEdges(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	now := time.Now()

	// Create two behaviors
	behaviors := []models.Behavior{
		{ID: "b-1", Name: "B1", Confidence: 0.8},
		{ID: "b-2", Name: "B2", Confidence: 0.8},
	}
	for _, b := range behaviors {
		addBehaviorToStore(t, ctx, s, b)
	}

	// Add edges of different kinds
	s.AddEdge(ctx, store.Edge{Source: "b-1", Target: "b-2", Kind: store.EdgeKindSimilarTo, Weight: 0.8, CreatedAt: now})
	s.AddEdge(ctx, store.Edge{Source: "b-1", Target: "b-2", Kind: store.EdgeKindOverrides, Weight: 1.0, CreatedAt: now})
	s.AddEdge(ctx, store.Edge{Source: "b-1", Target: "b-2", Kind: store.EdgeKindRequires, Weight: 0.5, CreatedAt: now})

	cleared := ClearDerivedEdges(ctx, s, behaviors)

	// Should clear similar-to and overrides (2), but NOT requires
	if cleared != 2 {
		t.Errorf("cleared = %d, want 2 (similar-to + overrides)", cleared)
	}

	// Verify requires edge still exists
	requiresEdges, _ := s.GetEdges(ctx, "b-1", store.DirectionOutbound, store.EdgeKindRequires)
	if len(requiresEdges) != 1 {
		t.Errorf("requires edges = %d, want 1 (should not be cleared)", len(requiresEdges))
	}

	// Verify similar-to and overrides are gone
	similarEdges, _ := s.GetEdges(ctx, "b-1", store.DirectionOutbound, store.EdgeKindSimilarTo)
	if len(similarEdges) != 0 {
		t.Errorf("similar-to edges = %d, want 0 (should be cleared)", len(similarEdges))
	}
	overridesEdges, _ := s.GetEdges(ctx, "b-1", store.DirectionOutbound, store.EdgeKindOverrides)
	if len(overridesEdges) != 0 {
		t.Errorf("overrides edges = %d, want 0 (should be cleared)", len(overridesEdges))
	}
}

func TestComputeConnectivity(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	now := time.Now()

	// Create 3 behaviors
	behaviors := []models.Behavior{
		{ID: "b-1", Name: "B1", Confidence: 0.8},
		{ID: "b-2", Name: "B2", Confidence: 0.8},
		{ID: "b-3", Name: "B3", Confidence: 0.8},
	}
	for _, b := range behaviors {
		addBehaviorToStore(t, ctx, s, b)
	}

	// Add edge between b-1 and b-2 only
	s.AddEdge(ctx, store.Edge{Source: "b-1", Target: "b-2", Kind: store.EdgeKindSimilarTo, Weight: 0.8, CreatedAt: now})

	info := ComputeConnectivity(ctx, s, behaviors)

	if info.TotalNodes != 3 {
		t.Errorf("TotalNodes = %d, want 3", info.TotalNodes)
	}
	if info.Connected != 2 {
		t.Errorf("Connected = %d, want 2", info.Connected)
	}
	if info.Islands != 1 {
		t.Errorf("Islands = %d, want 1", info.Islands)
	}
}
