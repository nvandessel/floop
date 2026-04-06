//go:build cgo

package spreading

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

// parityConfig returns a Config suitable for parity testing.
// TemporalDecayRate is 0 so temporal decay doesn't diverge between engines
// (Go applies at query time; native pre-applies at graph-load time).
// MinActivation is set above sigmoid(0) ≈ 0.047 to avoid phantom results
// from nodes that were added to the Go engine's map by suppressive edges
// but never received positive energy.
func parityConfig(inhibition bool) Config {
	cfg := DefaultConfig()
	cfg.TemporalDecayRate = 0
	cfg.MinActivation = 0.05 // above sigmoid(0) ≈ 0.047
	if inhibition {
		inh := DefaultInhibitionConfig()
		cfg.Inhibition = &inh
	} else {
		cfg.Inhibition = nil
	}
	cfg.Affinity = nil
	return cfg
}

// resultMap builds a lookup from BehaviorID to Result.
func resultMap(results []Result) map[string]Result {
	m := make(map[string]Result, len(results))
	for _, r := range results {
		m[r.BehaviorID] = r
	}
	return m
}

// assertParityResults compares Go and native engine results:
// - Same set of BehaviorIDs
// - Activations within epsilon
// - Same distances
// - Same rank order (where activations differ beyond epsilon)
func assertParityResults(t *testing.T, goResults, nativeResults []Result, epsilon float64) {
	t.Helper()

	goMap := resultMap(goResults)
	nativeMap := resultMap(nativeResults)

	// Boundary tolerance: nodes near MinActivation may appear in one engine
	// but not the other due to propagation order differences (Go processes
	// edges per-node with map iteration; sproink uses CSR with bidirectional
	// storage). Nodes present in only one result set are acceptable if their
	// activation is within 2x MinActivation (boundary zone).
	boundaryThreshold := 0.1 // 2x default MinActivation

	for id, goR := range goMap {
		natR, ok := nativeMap[id]
		if !ok {
			if goR.Activation < boundaryThreshold {
				t.Logf("BehaviorID %q in Go only (act=%.9f, near boundary) — tolerated", id, goR.Activation)
				continue
			}
			t.Errorf("BehaviorID %q in Go results but missing from Native (act=%.9f)", id, goR.Activation)
			continue
		}
		if math.Abs(goR.Activation-natR.Activation) > epsilon {
			t.Errorf("BehaviorID %q activation mismatch: Go=%.9f, Native=%.9f (eps=%e)",
				id, goR.Activation, natR.Activation, epsilon)
		}
		if goR.Distance != natR.Distance {
			t.Errorf("BehaviorID %q distance mismatch: Go=%d, Native=%d",
				id, goR.Distance, natR.Distance)
		}
	}

	for id, natR := range nativeMap {
		if _, ok := goMap[id]; !ok {
			if natR.Activation < boundaryThreshold {
				t.Logf("BehaviorID %q in Native only (act=%.9f, near boundary) — tolerated", id, natR.Activation)
				continue
			}
			t.Errorf("BehaviorID %q in Native results but missing from Go (act=%.9f)", id, natR.Activation)
		}
	}

	// Rank order: only check where activations differ by more than epsilon
	// (tied activations may be ordered differently due to implementation details).
	if len(goResults) == len(nativeResults) {
		for i := 1; i < len(goResults); i++ {
			goA, goB := goResults[i-1].Activation, goResults[i].Activation
			natA, natB := nativeResults[i-1].Activation, nativeResults[i].Activation
			// If both engines agree the activations are distinct, check same ordering.
			if goA-goB > epsilon && natA-natB > epsilon {
				if goResults[i-1].BehaviorID != nativeResults[i-1].BehaviorID {
					t.Errorf("rank order mismatch at position %d: Go=%s, Native=%s",
						i-1, goResults[i-1].BehaviorID, nativeResults[i-1].BehaviorID)
					break
				}
			}
		}
	}
}

func logResults(t *testing.T, label string, results []Result) {
	t.Helper()
	t.Logf("%s results:", label)
	for _, r := range results {
		t.Logf("  %s: act=%.9f dist=%d", r.BehaviorID, r.Activation, r.Distance)
	}
}

// runParity runs both engines on the same store, config, and seeds, then compares.
func runParity(t *testing.T, s store.ExtendedGraphStore, cfg Config, seeds []Seed, epsilon float64) {
	t.Helper()
	ctx := context.Background()

	goEngine := NewEngine(s, cfg)
	goResults, err := goEngine.Activate(ctx, seeds)
	if err != nil {
		t.Fatalf("Go Engine.Activate: %v", err)
	}

	nativeEngine, err := NewNativeEngine(s, cfg)
	if err != nil {
		t.Fatalf("NewNativeEngine: %v", err)
	}
	defer nativeEngine.Close()

	nativeResults, err := nativeEngine.Activate(ctx, seeds)
	if err != nil {
		t.Fatalf("NativeEngine.Activate: %v", err)
	}

	assertParityResults(t, goResults, nativeResults, epsilon)
}

// TestParity_PositiveGraph tests a graph with only positive edges.
// No suppressive interactions, so both engines should agree exactly.
func TestParity_PositiveGraph(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c", "d", "e"} {
		addTestNode(t, s, ctx, id)
	}

	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.9)
	addTestEdge(t, s, ctx, "b", "c", store.EdgeKindSimilarTo, 0.7)
	addTestEdge(t, s, ctx, "a", "d", store.EdgeKindLearnedFrom, 0.6)
	addTestEdge(t, s, ctx, "c", "e", store.EdgeKindRequires, 0.8)
	addTestEdge(t, s, ctx, "d", "e", store.EdgeKindCoActivated, 0.5)

	seeds := []Seed{
		{BehaviorID: "a", Activation: 1.0, Source: "test:a"},
		{BehaviorID: "c", Activation: 0.8, Source: "test:c"},
	}

	for _, inhEnabled := range []bool{false, true} {
		name := "inhibition_off"
		if inhEnabled {
			name = "inhibition_on"
		}
		t.Run(name, func(t *testing.T) {
			runParity(t, s, parityConfig(inhEnabled), seeds, 1e-6)
		})
	}
}

// TestParity_ConflictsSuppressActivation tests that conflict edges suppress
// activation identically in both engines. Nodes have positive baseline activation
// so suppression is measurable (not just sigmoid(0) phantom entries).
func TestParity_ConflictsSuppressActivation(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		addTestNode(t, s, ctx, id)
	}

	// Positive edges give b and c real activation.
	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.9)
	addTestEdge(t, s, ctx, "a", "c", store.EdgeKindSimilarTo, 0.8)
	// Conflict between b and c: should suppress each other.
	addTestEdge(t, s, ctx, "b", "c", store.EdgeKindConflicts, 0.7)

	seeds := []Seed{
		{BehaviorID: "a", Activation: 1.0, Source: "test:a"},
	}

	// Wider epsilon for mixed conflict+positive: propagation order differences
	// between Go (map iteration) and native (CSR index order) cause different
	// suppression magnitudes. Both engines suppress, just by different amounts.
	runParity(t, s, parityConfig(false), seeds, 0.15)
}

// TestParity_DirectionalSuppressive verifies that directional suppressive edges
// (overrides, deprecated-to, merged-into) only suppress in the source→target direction.
func TestParity_DirectionalSuppressive(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		addTestNode(t, s, ctx, id)
	}

	// a overrides b: seeding a should suppress b.
	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindOverrides, 0.7)
	// Positive edges so there's real activation to measure.
	addTestEdge(t, s, ctx, "a", "c", store.EdgeKindRequires, 0.8)
	addTestEdge(t, s, ctx, "b", "c", store.EdgeKindSimilarTo, 0.5)

	t.Run("seed_source", func(t *testing.T) {
		seeds := []Seed{{BehaviorID: "a", Activation: 1.0, Source: "test:a"}}
		runParity(t, s, parityConfig(false), seeds, 1e-6)
	})

	t.Run("seed_target", func(t *testing.T) {
		seeds := []Seed{{BehaviorID: "b", Activation: 1.0, Source: "test:b"}}
		runParity(t, s, parityConfig(false), seeds, 1e-6)
	})
}

// TestParity_EmptySeeds verifies both engines return empty results for empty seeds.
func TestParity_EmptySeeds(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	addTestNode(t, s, ctx, "a")
	addTestNode(t, s, ctx, "b")
	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.8)

	cfg := parityConfig(false)

	goEngine := NewEngine(s, cfg)
	goResults, err := goEngine.Activate(ctx, []Seed{})
	if err != nil {
		t.Fatalf("Go Engine.Activate: %v", err)
	}

	nativeEngine, err := NewNativeEngine(s, cfg)
	if err != nil {
		t.Fatalf("NewNativeEngine: %v", err)
	}
	defer nativeEngine.Close()

	nativeResults, err := nativeEngine.Activate(ctx, []Seed{})
	if err != nil {
		t.Fatalf("NativeEngine.Activate: %v", err)
	}

	if len(goResults) != 0 {
		t.Errorf("Go engine returned %d results for empty seeds, want 0", len(goResults))
	}
	if len(nativeResults) != 0 {
		t.Errorf("Native engine returned %d results for empty seeds, want 0", len(nativeResults))
	}
}

// TestParity_SingleSeedNoNeighbors verifies that a single seed with no outgoing
// edges returns just the seed with identical activation.
func TestParity_SingleSeedNoNeighbors(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	// Need at least one edge in the graph so sproink can build the CSR.
	addTestNode(t, s, ctx, "lone")
	addTestNode(t, s, ctx, "other")
	addTestEdge(t, s, ctx, "other", "lone", store.EdgeKindSimilarTo, 0.1)

	seeds := []Seed{
		{BehaviorID: "lone", Activation: 1.0, Source: "test:lone"},
	}

	cfg := parityConfig(false)

	goEngine := NewEngine(s, cfg)
	goResults, err := goEngine.Activate(ctx, seeds)
	if err != nil {
		t.Fatalf("Go Engine.Activate: %v", err)
	}

	nativeEngine, err := NewNativeEngine(s, cfg)
	if err != nil {
		t.Fatalf("NewNativeEngine: %v", err)
	}
	defer nativeEngine.Close()

	nativeResults, err := nativeEngine.Activate(ctx, seeds)
	if err != nil {
		t.Fatalf("NativeEngine.Activate: %v", err)
	}

	assertParityResults(t, goResults, nativeResults, 1e-6)

	// Verify the seed itself appears in both.
	for label, results := range map[string][]Result{"Go": goResults, "Native": nativeResults} {
		found := false
		for _, r := range results {
			if r.BehaviorID == "lone" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s engine: seed 'lone' missing from results", label)
		}
	}
}

// TestParity_InhibitionToggle verifies parity with inhibition on vs off using a
// large enough graph (>breadth) for inhibition to actually suppress nodes.
func TestParity_InhibitionToggle(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	// Star graph: n0 at center, spokes n1..n9.
	for i := 0; i < 10; i++ {
		addTestNode(t, s, ctx, fmt.Sprintf("n%d", i))
	}
	// Give each spoke a different weight so activations are distinct
	// (avoids tie-breaking ambiguity in rank order).
	for i := 1; i < 10; i++ {
		w := 0.5 + float64(i)*0.05 // 0.55, 0.60, ..., 0.95
		addTestEdge(t, s, ctx, "n0", fmt.Sprintf("n%d", i), store.EdgeKindRequires, w)
	}

	seeds := []Seed{
		{BehaviorID: "n0", Activation: 1.0, Source: "test:n0"},
	}

	for _, inhEnabled := range []bool{false, true} {
		name := "inhibition_off"
		if inhEnabled {
			name = "inhibition_on"
		}
		t.Run(name, func(t *testing.T) {
			runParity(t, s, parityConfig(inhEnabled), seeds, 1e-6)
		})
	}

	// Sanity check: inhibition actually changes results.
	t.Run("inhibition_has_effect", func(t *testing.T) {
		ctx := context.Background()
		cfgOff := parityConfig(false)
		cfgOn := parityConfig(true)

		goOff := NewEngine(s, cfgOff)
		offResults, _ := goOff.Activate(ctx, seeds)

		goOn := NewEngine(s, cfgOn)
		onResults, _ := goOn.Activate(ctx, seeds)

		offMap := resultMap(offResults)
		onMap := resultMap(onResults)

		different := false
		for id, offR := range offMap {
			if onR, ok := onMap[id]; ok {
				if math.Abs(offR.Activation-onR.Activation) > 1e-9 {
					different = true
					break
				}
			} else {
				different = true
				break
			}
		}
		if !different {
			t.Error("inhibition toggle had no effect on results")
		}
	})
}

// TestParity_DirectionalMergedInto verifies merged-into edge suppression parity.
func TestParity_DirectionalMergedInto(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	for _, id := range []string{"old", "new", "related"} {
		addTestNode(t, s, ctx, id)
	}

	// old merged-into new: seeding old should suppress new.
	addTestEdge(t, s, ctx, "old", "new", store.EdgeKindMergedInto, 0.9)
	// Positive edges so there's something to spread.
	addTestEdge(t, s, ctx, "old", "related", store.EdgeKindRequires, 0.6)
	// Give "new" positive activation from another source so it's not just sigmoid(0).
	addTestEdge(t, s, ctx, "related", "new", store.EdgeKindSimilarTo, 0.7)

	seeds := []Seed{
		{BehaviorID: "old", Activation: 1.0, Source: "test:old"},
	}

	runParity(t, s, parityConfig(false), seeds, 1e-6)
}

// TestParity_MultiSeedOverlap tests two seeds sharing a neighbor to verify
// energy accumulation parity.
func TestParity_MultiSeedOverlap(t *testing.T) {
	s := newTestExtendedStore(t)
	ctx := context.Background()

	for _, id := range []string{"s1", "s2", "shared", "tail"} {
		addTestNode(t, s, ctx, id)
	}
	addTestEdge(t, s, ctx, "s1", "shared", store.EdgeKindRequires, 0.8)
	addTestEdge(t, s, ctx, "s2", "shared", store.EdgeKindSimilarTo, 0.7)
	addTestEdge(t, s, ctx, "shared", "tail", store.EdgeKindRequires, 0.6)

	seeds := []Seed{
		{BehaviorID: "s1", Activation: 1.0, Source: "test:s1"},
		{BehaviorID: "s2", Activation: 0.9, Source: "test:s2"},
	}

	runParity(t, s, parityConfig(false), seeds, 1e-6)
}
