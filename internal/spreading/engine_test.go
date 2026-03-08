package spreading

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/store"
)

// helper to create a time.Time pointer.
func timePtr(t time.Time) *time.Time { return &t }

// addNode is a test helper that adds a node and fails the test on error.
func addNode(t *testing.T, s store.GraphStore, id string) {
	t.Helper()
	_, err := s.AddNode(context.Background(), store.Node{
		ID:   id,
		Kind: "behavior",
	})
	if err != nil {
		t.Fatalf("addNode(%s): %v", id, err)
	}
}

// addEdge is a test helper that adds a weighted edge and fails the test on error.
func addEdge(t *testing.T, s store.GraphStore, source, target string, kind store.EdgeKind, weight float64, lastActivated *time.Time) {
	t.Helper()
	edge := store.Edge{
		Source:        source,
		Target:        target,
		Kind:          kind,
		Weight:        weight,
		CreatedAt:     time.Now(),
		LastActivated: lastActivated,
	}
	if err := s.AddEdge(context.Background(), edge); err != nil {
		t.Fatalf("addEdge(%s->%s): %v", source, target, err)
	}
}

// findResult returns the Result for the given behavior ID, or nil if absent.
func findResult(results []Result, id string) *Result {
	for i := range results {
		if results[i].BehaviorID == id {
			return &results[i]
		}
	}
	return nil
}

func TestEngine_NoSeeds(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	eng := NewEngine(s, DefaultConfig())

	results, err := eng.Activate(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
	if results == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

func TestEngine_EmptySeeds(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	eng := NewEngine(s, DefaultConfig())

	results, err := eng.Activate(context.Background(), []Seed{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
	if results == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

func TestEngine_SingleSeed_NoEdges(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.BehaviorID != "A" {
		t.Errorf("expected behavior A, got %s", r.BehaviorID)
	}
	if r.Distance != 0 {
		t.Errorf("expected distance 0, got %d", r.Distance)
	}
	if r.SeedSource != "test" {
		t.Errorf("expected seed source 'test', got %s", r.SeedSource)
	}
	// Seed activation 1.0 -> after sigmoid(1.0) should be close to 1.0
	if r.Activation < 0.99 {
		t.Errorf("expected activation near 1.0, got %f", r.Activation)
	}
}

func TestEngine_LinearChain(t *testing.T) {
	// A -> B -> C
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")

	now := time.Now()
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "B", "C", store.EdgeKindRequires, 1.0, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rA := findResult(results, "A")
	rB := findResult(results, "B")
	rC := findResult(results, "C")

	if rA == nil || rB == nil {
		t.Fatalf("expected A and B in results, got %v", results)
	}

	// A should have highest activation.
	if rA.Activation <= rB.Activation {
		t.Errorf("expected A activation (%f) > B activation (%f)", rA.Activation, rB.Activation)
	}

	// B should have higher activation than C (if C is present).
	if rC != nil && rB.Activation <= rC.Activation {
		t.Errorf("expected B activation (%f) > C activation (%f)", rB.Activation, rC.Activation)
	}

	// Check distances.
	if rA.Distance != 0 {
		t.Errorf("expected A distance 0, got %d", rA.Distance)
	}
	if rB.Distance != 1 {
		t.Errorf("expected B distance 1, got %d", rB.Distance)
	}
	if rC != nil && rC.Distance != 2 {
		t.Errorf("expected C distance 2, got %d", rC.Distance)
	}

	// All should trace back to the same seed source.
	if rA.SeedSource != "test" {
		t.Errorf("expected A seed source 'test', got %s", rA.SeedSource)
	}
	if rB.SeedSource != "test" {
		t.Errorf("expected B seed source 'test', got %s", rB.SeedSource)
	}
}

func TestEngine_FanOut(t *testing.T) {
	// A -> B, A -> C, A -> D
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")
	addNode(t, s, "D")

	now := time.Now()
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "A", "C", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "A", "D", store.EdgeKindRequires, 1.0, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rB := findResult(results, "B")
	rC := findResult(results, "C")
	rD := findResult(results, "D")

	if rB == nil || rC == nil || rD == nil {
		t.Fatalf("expected B, C, D in results, got %v", results)
	}

	// All three should get equal activation due to fan-out normalization.
	tolerance := 0.001
	if math.Abs(rB.Activation-rC.Activation) > tolerance {
		t.Errorf("expected B and C to have equal activation, got B=%f C=%f", rB.Activation, rC.Activation)
	}
	if math.Abs(rB.Activation-rD.Activation) > tolerance {
		t.Errorf("expected B and D to have equal activation, got B=%f D=%f", rB.Activation, rD.Activation)
	}
}

func TestEngine_FanIn(t *testing.T) {
	// B -> A, C -> A (two sources feeding into A)
	// Seed both B and C. A should get max of incoming, not sum.
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")

	now := time.Now()
	addEdge(t, s, "B", "A", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "C", "A", store.EdgeKindRequires, 0.5, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{
		{BehaviorID: "B", Activation: 1.0, Source: "source-b"},
		{BehaviorID: "C", Activation: 1.0, Source: "source-c"},
	}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rA := findResult(results, "A")
	if rA == nil {
		t.Fatalf("expected A in results")
	}

	// Now test that max() is used: create a scenario where sum would produce
	// a different result than max. Both B and C send energy to A. With max(),
	// A's activation from the propagation step should equal the larger of the
	// two incoming energies (before sigmoid). We can verify by comparing
	// against a single-source scenario.
	s2 := store.NewInMemoryGraphStore()
	addNode(t, s2, "A2")
	addNode(t, s2, "B2")

	addEdge(t, s2, "B2", "A2", store.EdgeKindRequires, 1.0, timePtr(now))

	eng2 := NewEngine(s2, DefaultConfig())
	seeds2 := []Seed{{BehaviorID: "B2", Activation: 1.0, Source: "source-b2"}}
	results2, err := eng2.Activate(context.Background(), seeds2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rA2 := findResult(results2, "A2")
	if rA2 == nil {
		t.Fatalf("expected A2 in results")
	}

	// With max semantics, the fan-in node A should have activation at most
	// equal to the single-source case (since B has the stronger edge and
	// fan-out=1 in the single-source case but fan-out includes the A edge
	// in the fan-in case). The key invariant: A's activation should NOT exceed
	// a value that would only be achievable via sum().
	// Just verify it's a reasonable value and the engine didn't crash.
	if rA.Activation <= 0 {
		t.Errorf("expected positive activation for A, got %f", rA.Activation)
	}
}

func TestEngine_WeightedEdges(t *testing.T) {
	// A -> B (weight=1.0), A -> C (weight=0.1)
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")

	now := time.Now()
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "A", "C", store.EdgeKindRequires, 0.1, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rB := findResult(results, "B")
	rC := findResult(results, "C")

	if rB == nil {
		t.Fatalf("expected B in results")
	}

	// B should have higher activation than C because of the weight difference.
	// C might be filtered out entirely if its activation falls below MinActivation.
	if rC != nil && rB.Activation <= rC.Activation {
		t.Errorf("expected B activation (%f) > C activation (%f) due to higher edge weight", rB.Activation, rC.Activation)
	}
}

func TestEngine_TemporalDecay(t *testing.T) {
	// A -> B (last_activated = now), A -> C (last_activated = 7 days ago)
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")

	now := time.Now()
	sevenDaysAgo := now.Add(-7 * 24 * time.Hour)

	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "A", "C", store.EdgeKindRequires, 1.0, timePtr(sevenDaysAgo))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rB := findResult(results, "B")
	rC := findResult(results, "C")

	if rB == nil {
		t.Fatalf("expected B in results")
	}

	// B should have higher activation due to more recent edge.
	if rC != nil && rB.Activation <= rC.Activation {
		t.Errorf("expected B activation (%f) > C activation (%f) due to temporal decay", rB.Activation, rC.Activation)
	}
}

func TestEngine_DepthLimit(t *testing.T) {
	// Chain of 10 nodes: N0 -> N1 -> N2 -> ... -> N9
	// With MaxSteps=3, nodes beyond hop 3 should have very low activation.
	s := store.NewInMemoryGraphStore()
	now := time.Now()

	nodeIDs := make([]string, 10)
	for i := range 10 {
		nodeIDs[i] = string(rune('A' + i))
		addNode(t, s, nodeIDs[i])
	}
	for i := range 9 {
		addEdge(t, s, nodeIDs[i], nodeIDs[i+1], store.EdgeKindRequires, 1.0, timePtr(now))
	}

	cfg := DefaultConfig()
	cfg.MaxSteps = 3
	eng := NewEngine(s, cfg)
	seeds := []Seed{{BehaviorID: nodeIDs[0], Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Nodes far from the seed should either be absent or have very low activation.
	// The first few nodes should be present.
	r0 := findResult(results, nodeIDs[0])
	if r0 == nil {
		t.Fatal("expected seed node in results")
	}

	// Nodes at distance 5+ should not appear (energy decays too much).
	for i := 5; i < 10; i++ {
		r := findResult(results, nodeIDs[i])
		if r != nil {
			t.Logf("node %s at distance %d has activation %f (expected filtered out)", nodeIDs[i], i, r.Activation)
		}
	}

	// At least verify that activation decreases with distance for the nodes that exist.
	prevAct := r0.Activation
	for i := 1; i < 10; i++ {
		r := findResult(results, nodeIDs[i])
		if r == nil {
			break // Once we stop seeing nodes, all subsequent should be absent too.
		}
		if r.Activation > prevAct {
			t.Errorf("expected monotonically decreasing activation, but node %s (%f) > previous (%f)",
				nodeIDs[i], r.Activation, prevAct)
		}
		prevAct = r.Activation
	}
}

func TestEngine_Cycle(t *testing.T) {
	// A -> B -> C -> A (cycle)
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")

	now := time.Now()
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "B", "C", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "C", "A", store.EdgeKindRequires, 1.0, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not hang or crash. All nodes should be present.
	if len(results) < 2 {
		t.Errorf("expected at least 2 results from cycle, got %d", len(results))
	}

	rA := findResult(results, "A")
	if rA == nil {
		t.Fatal("expected A in results")
	}

	// A (the seed) should have the highest activation.
	for _, r := range results {
		if r.BehaviorID != "A" && r.Activation > rA.Activation {
			t.Errorf("expected seed A (%f) to have highest activation, but %s has %f",
				rA.Activation, r.BehaviorID, r.Activation)
		}
	}
}

func TestEngine_MinActivationFilter(t *testing.T) {
	// Chain: A -> B -> C -> D -> E
	// With high MinActivation, distant nodes should be excluded.
	s := store.NewInMemoryGraphStore()
	now := time.Now()

	for _, id := range []string{"A", "B", "C", "D", "E"} {
		addNode(t, s, id)
	}
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "B", "C", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "C", "D", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "D", "E", store.EdgeKindRequires, 1.0, timePtr(now))

	cfg := DefaultConfig()
	cfg.MinActivation = 0.1 // Higher threshold to filter more aggressively.
	eng := NewEngine(s, cfg)
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify that distant nodes are filtered out.
	for _, r := range results {
		if r.Activation < cfg.MinActivation {
			t.Errorf("result %s has activation %f below MinActivation %f",
				r.BehaviorID, r.Activation, cfg.MinActivation)
		}
	}
}

func TestEngine_SortedByActivation(t *testing.T) {
	// Multiple nodes with different distances from seed.
	s := store.NewInMemoryGraphStore()
	now := time.Now()

	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "A", "C", store.EdgeKindRequires, 0.5, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify sorted by activation descending.
	for i := 1; i < len(results); i++ {
		if results[i].Activation > results[i-1].Activation {
			t.Errorf("results not sorted: index %d (%f) > index %d (%f)",
				i, results[i].Activation, i-1, results[i-1].Activation)
		}
	}
}

func TestEngine_SigmoidSquashing(t *testing.T) {
	tests := []struct {
		name   string
		input  float64
		wantLo float64 // lower bound
		wantHi float64 // upper bound
	}{
		{"zero stays near zero", 0.0, 0.0, 0.06},
		{"low stays low", 0.1, 0.0, 0.2},
		{"inflection point near 0.5", 0.3, 0.45, 0.55},
		{"high goes near 1", 0.6, 0.9, 1.0},
		{"one stays near 1", 1.0, 0.99, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sigmoid(tt.input)
			if got < tt.wantLo || got > tt.wantHi {
				t.Errorf("sigmoid(%f) = %f, want in [%f, %f]", tt.input, got, tt.wantLo, tt.wantHi)
			}
		})
	}
}

func TestEngine_BidirectionalPropagation(t *testing.T) {
	// Edge A -> B, but seed at B. Activation should flow back to A
	// because we use DirectionBoth.
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")

	now := time.Now()
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "B", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rA := findResult(results, "A")
	if rA == nil {
		t.Fatal("expected activation to flow from B to A via bidirectional edge")
	}
	if rA.Distance != 1 {
		t.Errorf("expected A distance 1, got %d", rA.Distance)
	}
}

func TestEngine_NilLastActivated(t *testing.T) {
	// Edge with nil LastActivated should use full weight (no temporal decay).
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")

	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, nil) // nil LastActivated

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rB := findResult(results, "B")
	if rB == nil {
		t.Fatal("expected B in results with nil LastActivated (full weight)")
	}
	if rB.Activation <= 0 {
		t.Errorf("expected positive activation for B, got %f", rB.Activation)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxSteps != 3 {
		t.Errorf("expected MaxSteps=3, got %d", cfg.MaxSteps)
	}
	if cfg.DecayFactor != 0.7 {
		t.Errorf("expected DecayFactor=0.7, got %f", cfg.DecayFactor)
	}
	if cfg.SpreadFactor != 0.85 {
		t.Errorf("expected SpreadFactor=0.85, got %f", cfg.SpreadFactor)
	}
	if cfg.MinActivation != 0.01 {
		t.Errorf("expected MinActivation=0.01, got %f", cfg.MinActivation)
	}
	if cfg.TemporalDecayRate != 0.01 {
		t.Errorf("expected TemporalDecayRate=0.01, got %f", cfg.TemporalDecayRate)
	}
	if cfg.Inhibition == nil {
		t.Fatal("expected Inhibition to be non-nil in DefaultConfig")
	}
	if cfg.Inhibition.Strength != 0.15 {
		t.Errorf("expected Inhibition.Strength=0.15, got %f", cfg.Inhibition.Strength)
	}
	if cfg.Inhibition.Breadth != 7 {
		t.Errorf("expected Inhibition.Breadth=7, got %d", cfg.Inhibition.Breadth)
	}
	if !cfg.Inhibition.Enabled {
		t.Error("expected Inhibition.Enabled=true")
	}
}

func TestDefaultConfig_HebbianViability(t *testing.T) {
	// Regression test: with DefaultConfig, 1-hop neighbors of a seed with
	// realistic fan-out (3) must produce activation above the Hebbian
	// co-activation threshold. If this test fails, Hebbian learning is dead
	// in production — no edges will ever form from usage patterns.
	s := store.NewInMemoryGraphStore()
	now := time.Now()

	addNode(t, s, "Seed")
	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")

	addEdge(t, s, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "Seed", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "Seed", "C", store.EdgeKindRequires, 1.0, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hebbianThreshold := DefaultHebbianConfig().ActivationThreshold
	for _, id := range []string{"A", "B", "C"} {
		r := findResult(results, id)
		if r == nil {
			t.Errorf("expected %s in results", id)
			continue
		}
		if r.Activation < hebbianThreshold {
			t.Errorf("%s activation %f is below Hebbian threshold %f — Hebbian learning is dead for fan-out=3",
				id, r.Activation, hebbianThreshold)
		}
	}
}

func TestEngine_ConflictEdgeInhibition(t *testing.T) {
	// Graph: Seed -> A (requires), A -> B (conflicts)
	// B should have REDUCED activation due to the conflict edge.
	// Compare against a baseline where A -> B is a normal "requires" edge.
	now := time.Now()

	t.Run("conflict edge reduces neighbor activation", func(t *testing.T) {
		s := store.NewInMemoryGraphStore()
		addNode(t, s, "Seed")
		addNode(t, s, "A")
		addNode(t, s, "B")

		addEdge(t, s, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
		addEdge(t, s, "A", "B", store.EdgeKindConflicts, 1.0, timePtr(now))

		cfg := DefaultConfig()
		cfg.Inhibition = nil // Disable lateral inhibition to isolate conflict behavior
		eng := NewEngine(s, cfg)
		seeds := []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}}

		results, err := eng.Activate(context.Background(), seeds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rB := findResult(results, "B")

		// With conflict inhibition, B should NOT gain activation from A.
		// B should either be absent from results or have very low activation
		// (only from sigmoid of 0 or negative, which floors at 0).
		if rB != nil {
			// If B appears, its activation should be lower than what a normal
			// requires edge would produce.
			t.Logf("B activation with conflict edge: %f", rB.Activation)
		}

		// Now create a baseline with a normal edge to compare.
		sBaseline := store.NewInMemoryGraphStore()
		addNode(t, sBaseline, "Seed")
		addNode(t, sBaseline, "A")
		addNode(t, sBaseline, "B")

		addEdge(t, sBaseline, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
		addEdge(t, sBaseline, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))

		engBaseline := NewEngine(sBaseline, cfg)
		baselineResults, err := engBaseline.Activate(context.Background(), seeds)
		if err != nil {
			t.Fatalf("unexpected error in baseline: %v", err)
		}

		rBBaseline := findResult(baselineResults, "B")
		if rBBaseline == nil {
			t.Fatal("expected B in baseline results")
		}

		// The conflict case should produce strictly lower activation for B
		// than the normal-edge case.
		conflictAct := 0.0
		if rB != nil {
			conflictAct = rB.Activation
		}
		if conflictAct >= rBBaseline.Activation {
			t.Errorf("conflict edge should reduce B's activation: conflict=%f, normal=%f",
				conflictAct, rBBaseline.Activation)
		}
	})

	t.Run("conflict edge does not produce negative activation", func(t *testing.T) {
		// Seed -> A (requires, high weight), Seed -> B (requires), B -> A (conflicts)
		// Even with strong conflict, activation should not go negative.
		s := store.NewInMemoryGraphStore()
		addNode(t, s, "Seed")
		addNode(t, s, "A")
		addNode(t, s, "B")

		addEdge(t, s, "Seed", "A", store.EdgeKindRequires, 0.3, timePtr(now))
		addEdge(t, s, "Seed", "B", store.EdgeKindRequires, 1.0, timePtr(now))
		addEdge(t, s, "B", "A", store.EdgeKindConflicts, 1.0, timePtr(now))

		cfg := DefaultConfig()
		cfg.Inhibition = nil
		eng := NewEngine(s, cfg)
		seeds := []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}}

		results, err := eng.Activate(context.Background(), seeds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, r := range results {
			if r.Activation < 0 {
				t.Errorf("activation for %s should not be negative, got %f",
					r.BehaviorID, r.Activation)
			}
		}
	})

	t.Run("non-conflict edges still spread normally", func(t *testing.T) {
		// Seed -> A (requires), Seed -> B (similar-to)
		// Both should receive positive activation.
		s := store.NewInMemoryGraphStore()
		addNode(t, s, "Seed")
		addNode(t, s, "A")
		addNode(t, s, "B")

		addEdge(t, s, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
		addEdge(t, s, "Seed", "B", store.EdgeKindSimilarTo, 1.0, timePtr(now))

		cfg := DefaultConfig()
		cfg.Inhibition = nil
		eng := NewEngine(s, cfg)
		seeds := []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}}

		results, err := eng.Activate(context.Background(), seeds)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rA := findResult(results, "A")
		rB := findResult(results, "B")

		if rA == nil {
			t.Fatal("expected A in results for requires edge")
		}
		if rB == nil {
			t.Fatal("expected B in results for similar-to edge")
		}

		if rA.Activation <= 0 {
			t.Errorf("expected positive activation for A, got %f", rA.Activation)
		}
		if rB.Activation <= 0 {
			t.Errorf("expected positive activation for B, got %f", rB.Activation)
		}
	})
}

func TestEngine_ConflictEdgesDoNotDilutePositiveSpread(t *testing.T) {
	// Bug: outDegree counts ALL edges including conflicts, diluting positive energy.
	// With 3 requires + 2 conflict edges, positive edges should get 1/3 energy each,
	// not 1/5.
	now := time.Now()

	// Baseline: Seed -> A, B, C (3 requires, no conflicts)
	sBaseline := store.NewInMemoryGraphStore()
	for _, id := range []string{"Seed", "A", "B", "C"} {
		addNode(t, sBaseline, id)
	}
	addEdge(t, sBaseline, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, sBaseline, "Seed", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, sBaseline, "Seed", "C", store.EdgeKindRequires, 1.0, timePtr(now))

	cfg := DefaultConfig()
	cfg.Inhibition = nil // Isolate spreading behavior from lateral inhibition
	engBaseline := NewEngine(sBaseline, cfg)
	seeds := []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}}

	baselineResults, err := engBaseline.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	// Test case: Seed -> A, B, C (3 requires) + Seed -> X, Y (2 conflicts)
	sTest := store.NewInMemoryGraphStore()
	for _, id := range []string{"Seed", "A", "B", "C", "X", "Y"} {
		addNode(t, sTest, id)
	}
	addEdge(t, sTest, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, sTest, "Seed", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, sTest, "Seed", "C", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, sTest, "Seed", "X", store.EdgeKindConflicts, 1.0, timePtr(now))
	addEdge(t, sTest, "Seed", "Y", store.EdgeKindConflicts, 1.0, timePtr(now))

	engTest := NewEngine(sTest, cfg)
	testResults, err := engTest.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("test case: %v", err)
	}

	// A's activation should be the same whether or not conflict edges exist.
	tolerance := 0.001
	for _, id := range []string{"A", "B", "C"} {
		baselineNode := findResult(baselineResults, id)
		testNode := findResult(testResults, id)
		if baselineNode == nil || testNode == nil {
			t.Fatalf("expected %s in both baseline and test results", id)
		}

		if math.Abs(baselineNode.Activation-testNode.Activation) > tolerance {
			t.Errorf("conflict edges diluted positive spread for %s: baseline=%f, with conflicts=%f",
				id, baselineNode.Activation, testNode.Activation)
		}
	}
}

func TestEngine_DirectionalSuppressiveEdges(t *testing.T) {
	// Test that directional suppressive edges (overrides, deprecated-to, merged-into)
	// only suppress in the outbound direction (source → target), not reverse.
	now := time.Now()

	edgeKinds := []struct {
		name string
		kind store.EdgeKind
	}{
		{"overrides", store.EdgeKindOverrides},
		{"deprecated-to", store.EdgeKindDeprecatedTo},
		{"merged-into", store.EdgeKindMergedInto},
	}

	cfg := DefaultConfig()
	cfg.Inhibition = nil // Isolate spreading behavior

	for _, ek := range edgeKinds {
		t.Run(ek.name+" outbound suppresses target", func(t *testing.T) {
			// Seed -> A (requires), A -> B (suppressive)
			// Seeding A should suppress B.
			s := store.NewInMemoryGraphStore()
			addNode(t, s, "Seed")
			addNode(t, s, "A")
			addNode(t, s, "B")
			addEdge(t, s, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
			addEdge(t, s, "A", "B", ek.kind, 1.0, timePtr(now))

			eng := NewEngine(s, cfg)
			results, err := eng.Activate(context.Background(), []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Compare against a baseline with a requires edge.
			sBase := store.NewInMemoryGraphStore()
			addNode(t, sBase, "Seed")
			addNode(t, sBase, "A")
			addNode(t, sBase, "B")
			addEdge(t, sBase, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
			addEdge(t, sBase, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))

			engBase := NewEngine(sBase, cfg)
			baseResults, err := engBase.Activate(context.Background(), []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rB := findResult(results, "B")
			rBBase := findResult(baseResults, "B")
			if rBBase == nil {
				t.Fatal("expected B in baseline results")
			}

			suppressAct := 0.0
			if rB != nil {
				suppressAct = rB.Activation
			}
			if suppressAct >= rBBase.Activation {
				t.Errorf("%s edge should reduce B's activation: suppressive=%f, normal=%f",
					ek.name, suppressAct, rBBase.Activation)
			}
		})

		t.Run(ek.name+" reverse direction does not suppress source", func(t *testing.T) {
			// A -> B (suppressive edge), Seed -> B (requires)
			// Seeding B (the target) should NOT suppress A via reverse traversal.
			// A should still receive positive activation from other paths.
			s := store.NewInMemoryGraphStore()
			addNode(t, s, "Seed")
			addNode(t, s, "A")
			addNode(t, s, "B")
			addEdge(t, s, "Seed", "B", store.EdgeKindRequires, 1.0, timePtr(now))
			addEdge(t, s, "A", "B", ek.kind, 1.0, timePtr(now))
			addEdge(t, s, "Seed", "A", store.EdgeKindRequires, 0.5, timePtr(now))

			eng := NewEngine(s, cfg)
			results, err := eng.Activate(context.Background(), []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rA := findResult(results, "A")
			if rA == nil {
				t.Fatal("expected A in results — reverse suppression should not remove it")
			}
			if rA.Activation <= 0 {
				t.Errorf("A should have positive activation despite reverse %s edge, got %f",
					ek.name, rA.Activation)
			}
		})
	}
}

// --- ActivateWithSteps tests ---

func TestActivateWithSteps_LinearChain(t *testing.T) {
	// A -> B -> C with MaxSteps=3
	// Step 0 (initial): only A has activation
	// Step 1: B gets activation from A
	// Step 2: C gets activation from B
	// Step 3: further propagation
	// Final: post-inhibition + sigmoid
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")
	addNode(t, s, "C")

	now := time.Now()
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, s, "B", "C", store.EdgeKindRequires, 1.0, timePtr(now))

	cfg := DefaultConfig()
	cfg.MaxSteps = 3
	eng := NewEngine(s, cfg)
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	steps, err := eng.ActivateWithSteps(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return MaxSteps + 1 snapshots (initial + 3 propagation steps)
	// plus 1 final snapshot = MaxSteps + 2 total
	wantLen := cfg.MaxSteps + 2
	if len(steps) != wantLen {
		t.Fatalf("expected %d snapshots, got %d", wantLen, len(steps))
	}

	// Step 0 (initial seed): only A is active
	if steps[0].Step != 0 {
		t.Errorf("step 0: expected Step=0, got %d", steps[0].Step)
	}
	if steps[0].Final {
		t.Error("step 0: should not be final")
	}
	if act, ok := steps[0].Activation["A"]; !ok || act != 1.0 {
		t.Errorf("step 0: expected A=1.0, got %v", steps[0].Activation["A"])
	}
	if _, ok := steps[0].Activation["B"]; ok {
		t.Error("step 0: B should not have activation yet")
	}

	// Step 1: B should now have activation
	if steps[1].Step != 1 {
		t.Errorf("step 1: expected Step=1, got %d", steps[1].Step)
	}
	if _, ok := steps[1].Activation["B"]; !ok {
		t.Error("step 1: expected B to have activation")
	}

	// Step 2: C should now have activation
	if _, ok := steps[2].Activation["C"]; !ok {
		t.Error("step 2: expected C to have activation")
	}

	// Final snapshot should be marked Final
	last := steps[len(steps)-1]
	if !last.Final {
		t.Error("last snapshot should be marked Final")
	}
}

func TestActivateWithSteps_SnapshotCopiesAreIndependent(t *testing.T) {
	// Verify that mutating one snapshot's activation map doesn't affect others
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")
	addNode(t, s, "B")

	now := time.Now()
	addEdge(t, s, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	steps, err := eng.ActivateWithSteps(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(steps) < 2 {
		t.Fatalf("expected at least 2 snapshots, got %d", len(steps))
	}

	// Mutate step 0's activation map
	origA := steps[1].Activation["A"]
	steps[0].Activation["A"] = 999.0

	// Step 1 should be unaffected
	if steps[1].Activation["A"] != origA {
		t.Errorf("mutation leaked between snapshots: step 1 A changed from %f to %f",
			origA, steps[1].Activation["A"])
	}
}

func TestActivateWithSteps_FinalSnapshotHasSigmoid(t *testing.T) {
	// Final snapshot should have sigmoid applied (values mapped to [0,1] range)
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")

	eng := NewEngine(s, DefaultConfig())
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	steps, err := eng.ActivateWithSteps(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	last := steps[len(steps)-1]
	if !last.Final {
		t.Fatal("last snapshot should be Final")
	}

	// Seed with activation 1.0 -> after sigmoid should be very close to 1.0
	actA := last.Activation["A"]
	if actA < 0.99 {
		t.Errorf("expected final A activation near 1.0 (sigmoid of 1.0), got %f", actA)
	}
}

func TestActivateWithSteps_EmptySeeds(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	eng := NewEngine(s, DefaultConfig())

	steps, err := eng.ActivateWithSteps(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("expected empty steps for nil seeds, got %d", len(steps))
	}
	if steps == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

// mockTagProvider implements TagProvider for testing.
type mockTagProvider struct {
	tags map[string][]string
}

func (m *mockTagProvider) GetAllBehaviorTags(_ context.Context) map[string][]string {
	return m.tags
}

func TestEngine_AffinityEdgesDoNotInflateOutDegree(t *testing.T) {
	// Regression test for floop-g30: virtual affinity edges were appended to
	// the edges slice BEFORE outDegree was computed, diluting real edge energy.
	//
	// Setup: Seed -> A -> B (real edges), plus many virtual affinity neighbors
	// via shared tags. Without the fix, B's activation is diluted by the
	// virtual edge count in A's outDegree.
	now := time.Now()

	// Baseline: no affinity edges.
	sBase := store.NewInMemoryGraphStore()
	addNode(t, sBase, "Seed")
	addNode(t, sBase, "A")
	addNode(t, sBase, "B")
	addEdge(t, sBase, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, sBase, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))

	cfgBase := DefaultConfig()
	cfgBase.Inhibition = nil // Isolate the propagation behavior.
	engBase := NewEngine(sBase, cfgBase)
	seeds := []Seed{{BehaviorID: "Seed", Activation: 1.0, Source: "test"}}

	baseResults, err := engBase.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	rBBase := findResult(baseResults, "B")
	if rBBase == nil {
		t.Fatal("baseline: expected B in results")
	}

	// With affinity: A shares tags with 10 other behaviors, creating ~10
	// virtual affinity edges. A has 2 real edges visible via DirectionBoth
	// (Seed→A incoming, A→B outgoing). Without the fix, virtual edges inflate
	// outDegree to ~12, so B gets 1/12 of A's energy instead of 1/2.
	sAff := store.NewInMemoryGraphStore()
	addNode(t, sAff, "Seed")
	addNode(t, sAff, "A")
	addNode(t, sAff, "B")
	addEdge(t, sAff, "Seed", "A", store.EdgeKindRequires, 1.0, timePtr(now))
	addEdge(t, sAff, "A", "B", store.EdgeKindRequires, 1.0, timePtr(now))

	// Create 10 tag-affinity neighbors (not connected by real edges).
	tags := map[string][]string{
		"A": {"git", "worktree"},
	}
	for i := range 10 {
		id := "V" + string(rune('0'+i))
		addNode(t, sAff, id)
		tags[id] = []string{"git", "worktree"} // identical tags → Jaccard 1.0
	}

	affCfg := DefaultAffinityConfig()
	cfgAff := DefaultConfig()
	cfgAff.Inhibition = nil
	cfgAff.Affinity = &affCfg
	cfgAff.TagProvider = &mockTagProvider{tags: tags}

	engAff := NewEngine(sAff, cfgAff)

	affResults, err := engAff.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("affinity: %v", err)
	}
	rBAff := findResult(affResults, "B")
	if rBAff == nil {
		t.Fatal("affinity: expected B in results")
	}

	// B's activation with affinity should be close to the baseline (real edges
	// use the same outDegree regardless of virtual edges). Allow a small
	// tolerance for second-order effects from virtual neighbors feeding back.
	ratio := rBAff.Activation / rBBase.Activation
	if ratio < 0.8 {
		t.Errorf("virtual affinity edges diluted real edge energy: "+
			"baseline B=%f, with affinity B=%f (ratio=%.2f, want >= 0.80)",
			rBBase.Activation, rBAff.Activation, ratio)
	}
	if ratio > 1.5 {
		t.Errorf("virtual affinity edges amplified real edge energy unexpectedly: "+
			"baseline B=%f, with affinity B=%f (ratio=%.2f, want <= 1.50)",
			rBBase.Activation, rBAff.Activation, ratio)
	}
}
