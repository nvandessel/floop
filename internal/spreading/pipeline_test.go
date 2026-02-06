package spreading

import (
	"context"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestPipeline_EndToEnd(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	now := time.Now()

	// Add behaviors as nodes in the store.
	addBehaviorNode(t, s, "go-directive", "go-directive", map[string]interface{}{
		"language": "go",
	})
	addBehaviorNode(t, s, "python-directive", "python-directive", map[string]interface{}{
		"language": "python",
	})
	addBehaviorNode(t, s, "testing-procedure", "testing-procedure", map[string]interface{}{
		"task": "testing",
	})
	addBehaviorNode(t, s, "always-active", "always-active", nil)

	// Add edge: go-directive is similar to testing-procedure.
	addEdge(t, s, "go-directive", "testing-procedure", "similar-to", 0.8, timePtr(now))

	// Create pipeline with default config.
	pipeline := NewPipeline(s, DefaultConfig())

	// Context: language=go, task=development.
	actCtx := models.ContextSnapshot{
		FileLanguage: "go",
		Task:         "development",
	}

	results, err := pipeline.Run(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}

	// go-directive should be activated (direct seed match).
	goResult := findResult(results, "go-directive")
	if goResult == nil {
		t.Fatal("expected go-directive in results")
	}

	// always-active should be activated (always-active seed).
	alwaysResult := findResult(results, "always-active")
	if alwaysResult == nil {
		t.Fatal("expected always-active in results")
	}

	// go-directive should have higher activation than always-active
	// because it has higher specificity (1 condition vs 0).
	if goResult.Activation <= alwaysResult.Activation {
		t.Errorf("expected go-directive activation (%f) > always-active activation (%f)",
			goResult.Activation, alwaysResult.Activation)
	}

	// testing-procedure should be activated via spread from go-directive
	// through the similar-to edge (even though task=testing doesn't match
	// the context task=development, it gets activation through propagation).
	testResult := findResult(results, "testing-procedure")
	if testResult == nil {
		t.Log("testing-procedure not in results (may have been filtered by MinActivation)")
	} else {
		// It should have lower activation than the seed go-directive.
		if testResult.Activation >= goResult.Activation {
			t.Errorf("expected testing-procedure activation (%f) < go-directive activation (%f)",
				testResult.Activation, goResult.Activation)
		}
		// It should have distance > 0 (spread from seed).
		if testResult.Distance < 1 {
			t.Errorf("expected testing-procedure distance >= 1, got %d", testResult.Distance)
		}
	}

	// python-directive should NOT be activated: it doesn't match the context
	// and has no edges connecting it to any seed.
	pythonResult := findResult(results, "python-directive")
	if pythonResult != nil {
		t.Errorf("expected python-directive NOT in results (no match, no edges), but got activation %f",
			pythonResult.Activation)
	}

	// Results should be sorted by activation descending.
	for i := 1; i < len(results); i++ {
		if results[i].Activation > results[i-1].Activation {
			t.Errorf("results not sorted: index %d (%f) > index %d (%f)",
				i, results[i].Activation, i-1, results[i-1].Activation)
		}
	}
}

func TestPipeline_NoMatchingContext(t *testing.T) {
	s := store.NewInMemoryGraphStore()

	// Only add a Go-specific behavior.
	addBehaviorNode(t, s, "go-directive", "go-directive", map[string]interface{}{
		"language": "go",
	})

	pipeline := NewPipeline(s, DefaultConfig())

	// Context is Rust -- nothing should match.
	actCtx := models.ContextSnapshot{
		FileLanguage: "rust",
	}

	results, err := pipeline.Run(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for non-matching context, got %d results", len(results))
	}
}

func TestPipeline_EmptyStore(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	pipeline := NewPipeline(s, DefaultConfig())

	actCtx := models.ContextSnapshot{
		FileLanguage: "go",
	}

	results, err := pipeline.Run(context.Background(), actCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty store, got %d results", len(results))
	}
}
