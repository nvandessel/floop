package spreading

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestApplyInhibition_TopMPreserved(t *testing.T) {
	// 10 nodes with linearly decreasing activation. M=3.
	// The top 3 should retain their activation unchanged.
	// The bottom 7 should be suppressed.
	activations := map[string]float64{
		"n1":  1.0,
		"n2":  0.9,
		"n3":  0.8,
		"n4":  0.7,
		"n5":  0.6,
		"n6":  0.5,
		"n7":  0.4,
		"n8":  0.3,
		"n9":  0.2,
		"n10": 0.1,
	}

	config := InhibitionConfig{
		Strength: 0.15,
		Breadth:  3,
		Enabled:  true,
	}

	result := ApplyInhibition(activations, config)

	// Top 3 winners should be unchanged.
	for _, id := range []string{"n1", "n2", "n3"} {
		if result[id] != activations[id] {
			t.Errorf("winner %s: got %f, want %f (unchanged)", id, result[id], activations[id])
		}
	}

	// Bottom 7 losers should be suppressed (lower than original).
	for _, id := range []string{"n4", "n5", "n6", "n7", "n8", "n9", "n10"} {
		if result[id] >= activations[id] {
			t.Errorf("loser %s: got %f, want less than %f (suppressed)", id, result[id], activations[id])
		}
	}
}

func TestApplyInhibition_Suppression(t *testing.T) {
	// All nodes at 0.5 except one at 0.9. M=1.
	// The 0.9 node should suppress all 0.5 nodes.
	activations := map[string]float64{
		"strong": 0.9,
		"weak1":  0.5,
		"weak2":  0.5,
		"weak3":  0.5,
	}

	config := InhibitionConfig{
		Strength: 0.15,
		Breadth:  1,
		Enabled:  true,
	}

	result := ApplyInhibition(activations, config)

	// Strong node is the sole winner — unchanged.
	if result["strong"] != 0.9 {
		t.Errorf("strong: got %f, want 0.9", result["strong"])
	}

	// Weak nodes should be suppressed below 0.5.
	for _, id := range []string{"weak1", "weak2", "weak3"} {
		if result[id] >= 0.5 {
			t.Errorf("suppressed %s: got %f, want < 0.5", id, result[id])
		}
		// suppression = 0.15 * (0.9 - 0.5) = 0.06, so expect ~0.44
		expected := 0.44
		tolerance := 0.001
		if math.Abs(result[id]-expected) > tolerance {
			t.Errorf("suppressed %s: got %f, want ~%f", id, result[id], expected)
		}
	}
}

func TestApplyInhibition_Disabled(t *testing.T) {
	activations := map[string]float64{
		"a": 0.8,
		"b": 0.3,
		"c": 0.1,
	}

	config := InhibitionConfig{
		Strength: 0.15,
		Breadth:  1,
		Enabled:  false,
	}

	result := ApplyInhibition(activations, config)

	// Activations should be unchanged.
	for id, act := range activations {
		if result[id] != act {
			t.Errorf("%s: got %f, want %f (unchanged when disabled)", id, result[id], act)
		}
	}
}

func TestApplyInhibition_EmptyMap(t *testing.T) {
	result := ApplyInhibition(map[string]float64{}, DefaultInhibitionConfig())
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}

	// nil map should also not panic.
	resultNil := ApplyInhibition(nil, DefaultInhibitionConfig())
	if resultNil != nil {
		t.Errorf("expected nil result for nil input, got %v", resultNil)
	}
}

func TestApplyInhibition_FewerThanM(t *testing.T) {
	// Only 3 nodes but M=7. All nodes are winners; no suppression.
	activations := map[string]float64{
		"a": 0.9,
		"b": 0.5,
		"c": 0.2,
	}

	config := InhibitionConfig{
		Strength: 0.15,
		Breadth:  7,
		Enabled:  true,
	}

	result := ApplyInhibition(activations, config)

	for id, act := range activations {
		if result[id] != act {
			t.Errorf("%s: got %f, want %f (all winners when fewer than M)", id, result[id], act)
		}
	}
}

func TestApplyInhibition_StrengthEffect(t *testing.T) {
	// Same activations with different beta values.
	// Higher beta should produce more suppression.
	activations := map[string]float64{
		"winner": 0.9,
		"loser1": 0.4,
		"loser2": 0.3,
	}

	tests := []struct {
		name     string
		strength float64
	}{
		{"low strength", 0.05},
		{"medium strength", 0.15},
		{"high strength", 0.50},
	}

	var prevSuppression float64
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := InhibitionConfig{
				Strength: tt.strength,
				Breadth:  1,
				Enabled:  true,
			}
			result := ApplyInhibition(activations, config)

			// Calculate total suppression (original - result) for losers.
			suppression := 0.0
			for _, id := range []string{"loser1", "loser2"} {
				suppression += activations[id] - result[id]
			}

			if suppression < 0 {
				t.Errorf("expected non-negative suppression, got %f", suppression)
			}

			// Each test should produce more suppression than the last.
			if prevSuppression > 0 && suppression <= prevSuppression {
				t.Errorf("expected increasing suppression: %f <= previous %f", suppression, prevSuppression)
			}
			prevSuppression = suppression
		})
	}
}

func TestApplyInhibition_FocusEffect(t *testing.T) {
	// Two clusters: Cluster A (highly activated), Cluster B (weakly activated).
	// After inhibition, A should stay bright and B should fade.
	activations := map[string]float64{
		// Cluster A: strongly activated (related to current context).
		"a1": 0.95,
		"a2": 0.90,
		"a3": 0.85,
		// Cluster B: weakly activated (tangentially related).
		"b1": 0.30,
		"b2": 0.25,
		"b3": 0.20,
	}

	config := InhibitionConfig{
		Strength: 0.20,
		Breadth:  3,
		Enabled:  true,
	}

	result := ApplyInhibition(activations, config)

	// Cluster A nodes are winners — unchanged.
	for _, id := range []string{"a1", "a2", "a3"} {
		if result[id] != activations[id] {
			t.Errorf("cluster A %s: got %f, want %f (winner unchanged)", id, result[id], activations[id])
		}
	}

	// Cluster B nodes should be significantly suppressed.
	for _, id := range []string{"b1", "b2", "b3"} {
		if result[id] >= activations[id] {
			t.Errorf("cluster B %s: got %f, want < %f (suppressed)", id, result[id], activations[id])
		}
	}

	// Verify the contrast is amplified: ratio of mean(A) to mean(B) should
	// increase after inhibition.
	meanA := (activations["a1"] + activations["a2"] + activations["a3"]) / 3
	meanB := (activations["b1"] + activations["b2"] + activations["b3"]) / 3
	origRatio := meanA / meanB

	meanAResult := (result["a1"] + result["a2"] + result["a3"]) / 3
	meanBResult := (result["b1"] + result["b2"] + result["b3"]) / 3

	if meanBResult <= 0 {
		// B cluster was fully suppressed — maximum focus achieved.
		return
	}
	resultRatio := meanAResult / meanBResult
	if resultRatio <= origRatio {
		t.Errorf("expected ratio to increase after inhibition: original=%f, after=%f", origRatio, resultRatio)
	}
}

func TestApplyInhibition_PureFunction(t *testing.T) {
	// Verify that the input map is not mutated.
	activations := map[string]float64{
		"a": 0.9,
		"b": 0.3,
		"c": 0.1,
	}

	// Save original values.
	origA := activations["a"]
	origB := activations["b"]
	origC := activations["c"]

	config := InhibitionConfig{
		Strength: 0.15,
		Breadth:  1,
		Enabled:  true,
	}

	_ = ApplyInhibition(activations, config)

	if activations["a"] != origA || activations["b"] != origB || activations["c"] != origC {
		t.Error("input map was mutated by ApplyInhibition")
	}
}

func TestDefaultInhibitionConfig(t *testing.T) {
	cfg := DefaultInhibitionConfig()

	if cfg.Strength != 0.15 {
		t.Errorf("expected Strength=0.15, got %f", cfg.Strength)
	}
	if cfg.Breadth != 7 {
		t.Errorf("expected Breadth=7, got %d", cfg.Breadth)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
}

// Engine integration tests for inhibition.

func TestEngine_WithInhibition(t *testing.T) {
	// Two clusters connected by a weak edge.
	// Cluster A: seed -> a1 -> a2 (strong edges)
	// Cluster B: b1 -> b2 (strong edges)
	// Cross-link: a2 -> b1 (weak edge)
	// With inhibition, the seeded cluster A should stay bright while
	// cluster B fades.
	s := store.NewInMemoryGraphStore()
	now := time.Now()

	for _, id := range []string{"seed", "a1", "a2", "b1", "b2"} {
		addNode(t, s, id)
	}

	addEdge(t, s, "seed", "a1", "requires", 1.0, timePtr(now))
	addEdge(t, s, "a1", "a2", "requires", 1.0, timePtr(now))
	addEdge(t, s, "a2", "b1", "related", 0.3, timePtr(now))
	addEdge(t, s, "b1", "b2", "requires", 1.0, timePtr(now))

	cfg := DefaultConfig()
	inh := InhibitionConfig{
		Strength: 0.20,
		Breadth:  3,
		Enabled:  true,
	}
	cfg.Inhibition = &inh

	eng := NewEngine(s, cfg)
	seeds := []Seed{{BehaviorID: "seed", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rSeed := findResult(results, "seed")
	rA1 := findResult(results, "a1")

	if rSeed == nil {
		t.Fatal("expected seed in results")
	}
	if rA1 == nil {
		t.Fatal("expected a1 in results")
	}

	// The seeded cluster should dominate.
	if rSeed.Activation < 0.5 {
		t.Errorf("expected seed activation > 0.5, got %f", rSeed.Activation)
	}
	if rA1.Activation < 0.1 {
		t.Errorf("expected a1 activation > 0.1, got %f", rA1.Activation)
	}

	// If b2 is present, it should have lower activation than the seed cluster.
	rB2 := findResult(results, "b2")
	if rB2 != nil && rB2.Activation >= rA1.Activation {
		t.Errorf("expected b2 activation (%f) < a1 activation (%f) due to inhibition",
			rB2.Activation, rA1.Activation)
	}
}

func TestEngine_WithoutInhibition(t *testing.T) {
	// Same graph as TestEngine_WithInhibition, but with inhibition disabled.
	// Both clusters should have activation (less focused).
	s := store.NewInMemoryGraphStore()
	now := time.Now()

	for _, id := range []string{"seed", "a1", "a2", "b1", "b2"} {
		addNode(t, s, id)
	}

	addEdge(t, s, "seed", "a1", "requires", 1.0, timePtr(now))
	addEdge(t, s, "a1", "a2", "requires", 1.0, timePtr(now))
	addEdge(t, s, "a2", "b1", "related", 0.3, timePtr(now))
	addEdge(t, s, "b1", "b2", "requires", 1.0, timePtr(now))

	cfg := DefaultConfig()
	cfg.Inhibition = nil // Disabled.

	eng := NewEngine(s, cfg)
	seeds := []Seed{{BehaviorID: "seed", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without inhibition, activation should spread to the second cluster.
	rSeed := findResult(results, "seed")
	if rSeed == nil {
		t.Fatal("expected seed in results")
	}

	// Count how many nodes have activation — without inhibition
	// there should be reasonable spread.
	if len(results) < 2 {
		t.Errorf("expected at least 2 results without inhibition, got %d", len(results))
	}
}

func TestEngine_InhibitionBackwardCompatible(t *testing.T) {
	// Config with nil Inhibition should work exactly as before (no inhibition).
	s := store.NewInMemoryGraphStore()
	addNode(t, s, "A")

	cfg := Config{
		MaxSteps:          3,
		DecayFactor:       0.5,
		SpreadFactor:      0.8,
		MinActivation:     0.01,
		TemporalDecayRate: 0.01,
		Inhibition:        nil, // explicitly nil
	}

	eng := NewEngine(s, cfg)
	seeds := []Seed{{BehaviorID: "A", Activation: 1.0, Source: "test"}}

	results, err := eng.Activate(context.Background(), seeds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BehaviorID != "A" {
		t.Errorf("expected behavior A, got %s", results[0].BehaviorID)
	}
	if results[0].Activation < 0.99 {
		t.Errorf("expected activation near 1.0, got %f", results[0].Activation)
	}
}
