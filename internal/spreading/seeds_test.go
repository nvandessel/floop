package spreading

import (
	"context"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// addBehaviorNode is a test helper that adds a behavior node to the store
// with the given when conditions and returns the node ID.
func addBehaviorNode(t *testing.T, s store.GraphStore, id, name string, when map[string]interface{}) {
	t.Helper()
	content := map[string]interface{}{
		"name": name,
		"kind": "directive",
		"content": map[string]interface{}{
			"canonical": "Test behavior: " + name,
		},
	}
	if when != nil {
		content["when"] = when
	}
	_, err := s.AddNode(context.Background(), store.Node{
		ID:      id,
		Kind:    "behavior",
		Content: content,
		Metadata: map[string]interface{}{
			"confidence": 0.8,
		},
	})
	if err != nil {
		t.Fatalf("addBehaviorNode(%s): %v", id, err)
	}
}

// findSeed returns the Seed for the given behavior ID, or nil if absent.
func findSeed(seeds []Seed, id string) *Seed {
	for i := range seeds {
		if seeds[i].BehaviorID == id {
			return &seeds[i]
		}
	}
	return nil
}

func TestSeedSelector_EmptyStore(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	selector := NewSeedSelector(s)

	actCtx := models.ContextSnapshot{
		FileLanguage: "go",
		Task:         "development",
	}

	seeds, err := selector.SelectSeeds(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seeds) != 0 {
		t.Errorf("expected empty seeds for empty store, got %d", len(seeds))
	}
}

func TestSeedSelector_NoMatchingBehaviors(t *testing.T) {
	s := store.NewInMemoryGraphStore()

	// Add only Go-specific behaviors.
	addBehaviorNode(t, s, "go-directive", "go-directive", map[string]interface{}{
		"language": "go",
	})

	selector := NewSeedSelector(s)

	// Context is Rust -- the Go behavior should not match.
	actCtx := models.ContextSnapshot{
		FileLanguage: "rust",
	}

	seeds, err := selector.SelectSeeds(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seeds) != 0 {
		t.Errorf("expected no seeds for non-matching context, got %d", len(seeds))
	}
}

func TestSeedSelector_SpecificityScaling(t *testing.T) {
	s := store.NewInMemoryGraphStore()

	// Behavior with 1 matching condition.
	addBehaviorNode(t, s, "b1", "go-only", map[string]interface{}{
		"language": "go",
	})

	// Behavior with 2 matching conditions.
	addBehaviorNode(t, s, "b2", "go-dev", map[string]interface{}{
		"language": "go",
		"task":     "development",
	})

	// Behavior with 3 matching conditions.
	addBehaviorNode(t, s, "b3", "go-dev-main", map[string]interface{}{
		"language": "go",
		"task":     "development",
		"branch":   "main",
	})

	selector := NewSeedSelector(s)

	actCtx := models.ContextSnapshot{
		FileLanguage: "go",
		Task:         "development",
		Branch:       "main",
	}

	seeds, err := selector.SelectSeeds(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(seeds) != 3 {
		t.Fatalf("expected 3 seeds, got %d", len(seeds))
	}

	s1 := findSeed(seeds, "b1")
	s2 := findSeed(seeds, "b2")
	s3 := findSeed(seeds, "b3")

	if s1 == nil || s2 == nil || s3 == nil {
		t.Fatalf("expected all three behaviors as seeds, got %v", seeds)
	}

	// Verify specificity scaling: higher specificity -> higher activation.
	if s1.Activation != 0.4 {
		t.Errorf("expected activation 0.4 for specificity 1, got %f", s1.Activation)
	}
	if s2.Activation != 0.6 {
		t.Errorf("expected activation 0.6 for specificity 2, got %f", s2.Activation)
	}
	if s3.Activation != 0.8 {
		t.Errorf("expected activation 0.8 for specificity 3, got %f", s3.Activation)
	}

	// Verify ordering: highest specificity first.
	if seeds[0].BehaviorID != "b3" {
		t.Errorf("expected b3 first (highest activation), got %s", seeds[0].BehaviorID)
	}
}

func TestSeedSelector_AlwaysActiveBehaviors(t *testing.T) {
	s := store.NewInMemoryGraphStore()

	// Behavior with no 'when' conditions (always active).
	addBehaviorNode(t, s, "always", "always-active", nil)

	// Also add a matching behavior for comparison.
	addBehaviorNode(t, s, "go-specific", "go-directive", map[string]interface{}{
		"language": "go",
	})

	selector := NewSeedSelector(s)

	actCtx := models.ContextSnapshot{
		FileLanguage: "go",
	}

	seeds, err := selector.SelectSeeds(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(seeds) != 2 {
		t.Fatalf("expected 2 seeds, got %d", len(seeds))
	}

	alwaysSeed := findSeed(seeds, "always")
	goSeed := findSeed(seeds, "go-specific")

	if alwaysSeed == nil {
		t.Fatal("expected always-active behavior as seed")
	}
	if goSeed == nil {
		t.Fatal("expected go-specific behavior as seed")
	}

	// Always-active should have lower activation than a specific match.
	if alwaysSeed.Activation != 0.3 {
		t.Errorf("expected activation 0.3 for always-active, got %f", alwaysSeed.Activation)
	}
	if goSeed.Activation != 0.4 {
		t.Errorf("expected activation 0.4 for specificity-1 match, got %f", goSeed.Activation)
	}
	if alwaysSeed.Activation >= goSeed.Activation {
		t.Errorf("always-active activation (%f) should be less than specific match (%f)",
			alwaysSeed.Activation, goSeed.Activation)
	}
}

func TestSeedSelector_SourceLabels(t *testing.T) {
	s := store.NewInMemoryGraphStore()

	// Always-active behavior.
	addBehaviorNode(t, s, "always", "always-active", nil)

	// Single condition behavior.
	addBehaviorNode(t, s, "go-only", "go-directive", map[string]interface{}{
		"language": "go",
	})

	// Multi-condition behavior.
	addBehaviorNode(t, s, "go-test", "go-testing", map[string]interface{}{
		"language": "go",
		"task":     "testing",
	})

	selector := NewSeedSelector(s)

	actCtx := models.ContextSnapshot{
		FileLanguage: "go",
		Task:         "testing",
	}

	seeds, err := selector.SelectSeeds(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		id         string
		wantSource string
	}{
		{"always", "context:always"},
		{"go-only", "context:language=go"},
		{"go-test", "context:language=go,task=testing"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			seed := findSeed(seeds, tt.id)
			if seed == nil {
				t.Fatalf("seed %s not found", tt.id)
			}
			if seed.Source != tt.wantSource {
				t.Errorf("expected source %q, got %q", tt.wantSource, seed.Source)
			}
		})
	}
}

func TestSeedSelector_SortedByActivation(t *testing.T) {
	s := store.NewInMemoryGraphStore()

	// Create behaviors with varying specificity levels.
	addBehaviorNode(t, s, "always", "always-active", nil)
	addBehaviorNode(t, s, "one", "one-condition", map[string]interface{}{
		"language": "go",
	})
	addBehaviorNode(t, s, "two", "two-conditions", map[string]interface{}{
		"language": "go",
		"task":     "development",
	})
	addBehaviorNode(t, s, "three", "three-conditions", map[string]interface{}{
		"language": "go",
		"task":     "development",
		"branch":   "main",
	})

	selector := NewSeedSelector(s)

	actCtx := models.ContextSnapshot{
		FileLanguage: "go",
		Task:         "development",
		Branch:       "main",
	}

	seeds, err := selector.SelectSeeds(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(seeds) != 4 {
		t.Fatalf("expected 4 seeds, got %d", len(seeds))
	}

	// Verify sorted by activation descending.
	for i := 1; i < len(seeds); i++ {
		if seeds[i].Activation > seeds[i-1].Activation {
			t.Errorf("seeds not sorted: index %d (%f) > index %d (%f)",
				i, seeds[i].Activation, i-1, seeds[i-1].Activation)
		}
	}
}

func TestSpecificityToActivation(t *testing.T) {
	tests := []struct {
		name        string
		specificity int
		want        float64
	}{
		{"zero (always-active)", 0, 0.3},
		{"one condition", 1, 0.4},
		{"two conditions", 2, 0.6},
		{"three conditions", 3, 0.8},
		{"four conditions", 4, 0.9},
		{"five conditions", 5, 1.0},
		{"six conditions (clamped)", 6, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := specificityToActivation(tt.specificity)
			if got != tt.want {
				t.Errorf("specificityToActivation(%d) = %f, want %f", tt.specificity, got, tt.want)
			}
		})
	}
}

func TestBuildSourceLabel(t *testing.T) {
	tests := []struct {
		name       string
		conditions map[string]interface{}
		want       string
	}{
		{"nil conditions", nil, "context:always"},
		{"empty conditions", map[string]interface{}{}, "context:always"},
		{"single condition", map[string]interface{}{"language": "go"}, "context:language=go"},
		{"two conditions sorted", map[string]interface{}{
			"task":     "testing",
			"language": "go",
		}, "context:language=go,task=testing"},
		{"three conditions sorted", map[string]interface{}{
			"task":     "dev",
			"branch":   "main",
			"language": "go",
		}, "context:branch=main,language=go,task=dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSourceLabel(tt.conditions)
			if got != tt.want {
				t.Errorf("buildSourceLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
