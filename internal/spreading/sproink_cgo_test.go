//go:build cgo

package spreading

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

// buildTriangleEngine creates a NativeEngine backed by a 3-node triangle graph:
//
//	a --requires(0.8)--> b
//	b --similar-to(0.5)--> c
//	a --requires(0.6)--> c
func buildTriangleEngine(t *testing.T) (*NativeEngine, store.ExtendedGraphStore) {
	t.Helper()
	s := newTestExtendedStore(t)
	ctx := context.Background()

	addTestNode(t, s, ctx, "a")
	addTestNode(t, s, ctx, "b")
	addTestNode(t, s, ctx, "c")

	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.8)
	addTestEdge(t, s, ctx, "b", "c", store.EdgeKindSimilarTo, 0.5)
	addTestEdge(t, s, ctx, "a", "c", store.EdgeKindRequires, 0.6)

	config := DefaultConfig()
	// Disable affinity for predictable tests.
	config.Affinity = nil

	engine, err := NewNativeEngine(s, config)
	if err != nil {
		t.Fatalf("NewNativeEngine: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine, s
}

func TestNativeEngine_BasicActivation(t *testing.T) {
	engine, _ := buildTriangleEngine(t)

	results, err := engine.Activate(context.Background(), []Seed{
		{BehaviorID: "a", Activation: 1.0, Source: "test"},
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}

	// Results should be sorted by activation descending.
	for i := 1; i < len(results); i++ {
		if results[i].Activation > results[i-1].Activation {
			t.Errorf("results not sorted: [%d].Activation=%f > [%d].Activation=%f",
				i, results[i].Activation, i-1, results[i-1].Activation)
		}
	}

	// All activations should be above MinActivation and in [0, 1].
	for _, r := range results {
		if r.Activation < engine.config.MinActivation {
			t.Errorf("result %s activation %f below MinActivation %f",
				r.BehaviorID, r.Activation, engine.config.MinActivation)
		}
		if r.Activation > 1.0 {
			t.Errorf("result %s activation %f > 1.0", r.BehaviorID, r.Activation)
		}
	}

	// Seed node "a" should be present with SeedSource = "test".
	found := false
	for _, r := range results {
		if r.BehaviorID == "a" {
			found = true
			if r.SeedSource != "test" {
				t.Errorf("seed node SeedSource = %q, want %q", r.SeedSource, "test")
			}
			if r.Distance != 0 {
				t.Errorf("seed node Distance = %d, want 0", r.Distance)
			}
		}
	}
	if !found {
		t.Error("seed node 'a' not found in results")
	}

	// Neighbors "b" and "c" should appear with distance > 0.
	neighborIDs := make(map[string]bool)
	for _, r := range results {
		if r.BehaviorID != "a" {
			neighborIDs[r.BehaviorID] = true
			if r.Distance == 0 {
				t.Errorf("non-seed %s has Distance=0", r.BehaviorID)
			}
		}
	}
	if len(neighborIDs) == 0 {
		t.Error("expected at least one non-seed result")
	}
}

func TestNativeEngine_EmptySeeds(t *testing.T) {
	engine, _ := buildTriangleEngine(t)

	results, err := engine.Activate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Activate with nil seeds: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}

	results, err = engine.Activate(context.Background(), []Seed{})
	if err != nil {
		t.Fatalf("Activate with empty seeds: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestNativeEngine_UnknownSeed(t *testing.T) {
	engine, _ := buildTriangleEngine(t)

	// Seed with unknown UUID should be silently dropped.
	results, err := engine.Activate(context.Background(), []Seed{
		{BehaviorID: "nonexistent-uuid", Activation: 1.0, Source: "test"},
	})
	if err != nil {
		t.Fatalf("Activate with unknown seed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results for unknown seed, got %d", len(results))
	}
}

func TestNativeEngine_VersionStaleness(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	addTestNode(t, s, ctx, "a")
	addTestNode(t, s, ctx, "b")
	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.8)

	config := DefaultConfig()
	config.Affinity = nil

	engine, err := NewNativeEngine(s, config)
	if err != nil {
		t.Fatalf("NewNativeEngine: %v", err)
	}
	t.Cleanup(func() { engine.Close() })

	// Record version before mutation.
	versionBefore := engine.version

	// Add a new node + edge, bumping the store version.
	addTestNode(t, s, ctx, "c")
	addTestEdge(t, s, ctx, "b", "c", store.EdgeKindSimilarTo, 0.7)

	if s.Version() <= versionBefore {
		t.Fatal("store version did not increase after mutation")
	}

	// Activate should trigger a rebuild (version check detects staleness).
	results, err := engine.Activate(ctx, []Seed{
		{BehaviorID: "a", Activation: 1.0, Source: "test"},
	})
	if err != nil {
		t.Fatalf("Activate after mutation: %v", err)
	}

	// After rebuild, the engine should see node "c" via b->c.
	ids := make(map[string]bool)
	for _, r := range results {
		ids[r.BehaviorID] = true
	}

	// "c" should now be reachable (through a->b->c).
	if !ids["c"] {
		t.Error("node 'c' not in results after rebuild; expected rebuild to pick up new edge")
	}

	// Engine version should now match the store.
	if engine.version != s.Version() {
		t.Errorf("engine.version=%d != store.Version()=%d after rebuild", engine.version, s.Version())
	}
}

func TestNativeEngine_ConcurrentActivation(t *testing.T) {
	engine, _ := buildTriangleEngine(t)

	// Launch 10 goroutines calling Activate simultaneously.
	// Run with -race to detect data races.
	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := engine.Activate(context.Background(), []Seed{
				{BehaviorID: "a", Activation: 1.0, Source: "test"},
			})
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Activate error: %v", err)
	}
}

func TestNativeEngine_Close(t *testing.T) {
	engine, _ := buildTriangleEngine(t)

	// First close should succeed.
	if err := engine.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second close should not panic or error (idempotent).
	if err := engine.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestNativeEngine_SeedSourceAttribution(t *testing.T) {
	engine, _ := buildTriangleEngine(t)

	results, err := engine.Activate(context.Background(), []Seed{
		{BehaviorID: "a", Activation: 1.0, Source: "ctx:a"},
		{BehaviorID: "b", Activation: 0.8, Source: "ctx:b"},
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	for _, r := range results {
		if r.SeedSource == "" {
			t.Errorf("result %s has empty SeedSource", r.BehaviorID)
		}
		// Seeds should have their own source.
		if r.BehaviorID == "a" && r.Distance == 0 {
			if r.SeedSource != "ctx:a" {
				t.Errorf("seed 'a' SeedSource = %q, want %q", r.SeedSource, "ctx:a")
			}
		}
		if r.BehaviorID == "b" && r.Distance == 0 {
			if r.SeedSource != "ctx:b" {
				t.Errorf("seed 'b' SeedSource = %q, want %q", r.SeedSource, "ctx:b")
			}
		}
	}

	// Non-seed results should have alphabetically first seed's source for tiebreak.
	for _, r := range results {
		if r.Distance > 0 {
			// With seeds "a" and "b", alphabetically first is "a".
			if r.SeedSource != "ctx:a" {
				t.Errorf("non-seed %s SeedSource = %q, want %q (alphabetical tiebreak)",
					r.BehaviorID, r.SeedSource, "ctx:a")
			}
		}
	}
}

func TestNativeEngine_ResultsSorted(t *testing.T) {
	engine, _ := buildTriangleEngine(t)

	results, err := engine.Activate(context.Background(), []Seed{
		{BehaviorID: "a", Activation: 1.0, Source: "test"},
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	isSorted := sort.SliceIsSorted(results, func(i, j int) bool {
		return results[i].Activation > results[j].Activation
	})
	if !isSorted {
		t.Error("results not sorted by activation descending")
		for i, r := range results {
			t.Logf("  [%d] %s activation=%f", i, r.BehaviorID, r.Activation)
		}
	}
}
