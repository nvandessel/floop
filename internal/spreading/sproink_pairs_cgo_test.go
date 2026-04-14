//go:build cgo

package spreading

import (
	"context"
	"math"
	"sort"
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

// buildPairsEngine creates a NativeEngine with a 4-node graph that produces
// multiple co-activation pairs when seeded from "a":
//
//	a --requires(0.8)--> b
//	a --requires(0.7)--> c
//	b --similar-to(0.6)--> d
//	c --similar-to(0.5)--> d
func buildPairsEngine(t *testing.T) *NativeEngine {
	t.Helper()
	s := newTestExtendedStore(t)
	ctx := context.Background()

	addTestNode(t, s, ctx, "a")
	addTestNode(t, s, ctx, "b")
	addTestNode(t, s, ctx, "c")
	addTestNode(t, s, ctx, "d")

	addTestEdge(t, s, ctx, "a", "b", store.EdgeKindRequires, 0.8)
	addTestEdge(t, s, ctx, "a", "c", store.EdgeKindRequires, 0.7)
	addTestEdge(t, s, ctx, "b", "d", store.EdgeKindSimilarTo, 0.6)
	addTestEdge(t, s, ctx, "c", "d", store.EdgeKindSimilarTo, 0.5)

	config := DefaultConfig()
	config.Affinity = nil

	engine, err := NewNativeEngine(s, config)
	if err != nil {
		t.Fatalf("NewNativeEngine: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine
}

// sortPairs sorts CoActivationPair slices for deterministic comparison.
func sortPairs(pairs []CoActivationPair) {
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].BehaviorA != pairs[j].BehaviorA {
			return pairs[i].BehaviorA < pairs[j].BehaviorA
		}
		return pairs[i].BehaviorB < pairs[j].BehaviorB
	})
}

func TestNativeExtractPairs_ParityWithGo(t *testing.T) {
	engine := buildPairsEngine(t)
	threshold := 0.15

	seeds := []Seed{
		{BehaviorID: "a", Activation: 1.0, Source: "test"},
	}
	seedIDs := map[string]bool{"a": true}

	nativePairs, goResults, err := engine.nativeActivateAndExtractPairs(seeds, threshold)
	if err != nil {
		t.Fatalf("nativeActivateAndExtractPairs: %v", err)
	}

	// Go path.
	cfg := HebbianConfig{ActivationThreshold: threshold}
	goPairs := ExtractCoActivationPairs(goResults, seedIDs, cfg)

	sortPairs(nativePairs)
	sortPairs(goPairs)

	if len(nativePairs) == 0 {
		t.Fatal("expected non-empty pairs from NativeExtractPairs")
	}

	if len(nativePairs) != len(goPairs) {
		t.Logf("native pairs (%d):", len(nativePairs))
		for i, p := range nativePairs {
			t.Logf("  [%d] %s-%s (%.4f, %.4f)", i, p.BehaviorA, p.BehaviorB, p.ActivationA, p.ActivationB)
		}
		t.Logf("go pairs (%d):", len(goPairs))
		for i, p := range goPairs {
			t.Logf("  [%d] %s-%s (%.4f, %.4f)", i, p.BehaviorA, p.BehaviorB, p.ActivationA, p.ActivationB)
		}
		t.Fatalf("pair count mismatch: native=%d, go=%d", len(nativePairs), len(goPairs))
	}

	for i := range nativePairs {
		np := nativePairs[i]
		gp := goPairs[i]

		if np.BehaviorA != gp.BehaviorA || np.BehaviorB != gp.BehaviorB {
			t.Errorf("pair %d: IDs differ: native=(%s,%s), go=(%s,%s)",
				i, np.BehaviorA, np.BehaviorB, gp.BehaviorA, gp.BehaviorB)
		}

		if math.Abs(np.ActivationA-gp.ActivationA) > 1e-12 {
			t.Errorf("pair %d (%s,%s): ActivationA differs: native=%f, go=%f",
				i, np.BehaviorA, np.BehaviorB, np.ActivationA, gp.ActivationA)
		}
		if math.Abs(np.ActivationB-gp.ActivationB) > 1e-12 {
			t.Errorf("pair %d (%s,%s): ActivationB differs: native=%f, go=%f",
				i, np.BehaviorA, np.BehaviorB, np.ActivationB, gp.ActivationB)
		}
	}
}

func TestNativeExtractPairs_MultipleSeeds(t *testing.T) {
	engine := buildPairsEngine(t)
	threshold := 0.15

	seeds := []Seed{
		{BehaviorID: "a", Activation: 1.0, Source: "test"},
		{BehaviorID: "b", Activation: 0.9, Source: "test"},
	}
	seedIDs := map[string]bool{"a": true, "b": true}

	nativePairs, goResults, err := engine.nativeActivateAndExtractPairs(seeds, threshold)
	if err != nil {
		t.Fatalf("nativeActivateAndExtractPairs: %v", err)
	}

	cfg := HebbianConfig{ActivationThreshold: threshold}
	goPairs := ExtractCoActivationPairs(goResults, seedIDs, cfg)

	sortPairs(nativePairs)
	sortPairs(goPairs)

	if len(nativePairs) != len(goPairs) {
		t.Fatalf("pair count mismatch: native=%d, go=%d", len(nativePairs), len(goPairs))
	}

	for i := range nativePairs {
		np := nativePairs[i]
		gp := goPairs[i]

		if np.BehaviorA != gp.BehaviorA || np.BehaviorB != gp.BehaviorB {
			t.Errorf("pair %d: IDs differ: native=(%s,%s), go=(%s,%s)",
				i, np.BehaviorA, np.BehaviorB, gp.BehaviorA, gp.BehaviorB)
		}
	}
}

func TestNativeOjaUpdate_ParityWithGo(t *testing.T) {
	cfg := DefaultHebbianConfig()

	tests := []struct {
		name   string
		weight float64
		actA   float64
		actB   float64
	}{
		{"basic strengthening", 0.5, 0.8, 0.8},
		{"weak activation", 0.9, 0.1, 0.5},
		{"zero activation", 0.5, 0.0, 0.0},
		{"full activation", 0.5, 1.0, 1.0},
		{"asymmetric", 0.3, 0.9, 0.2},
		{"near min weight", 0.02, 0.3, 0.3},
		{"near max weight", 0.94, 0.7, 0.7},
		{"at min weight", cfg.MinWeight, 0.5, 0.5},
		{"at max weight", cfg.MaxWeight, 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goResult := OjaUpdate(tt.weight, tt.actA, tt.actB, cfg)
			nativeResult := NativeOjaUpdate(tt.weight, tt.actA, tt.actB, cfg)

			if math.Abs(goResult-nativeResult) > 1e-15 {
				t.Errorf("parity failure: Go=%.*e, Native=%.*e, diff=%e",
					17, goResult, 17, nativeResult, math.Abs(goResult-nativeResult))
			}
		})
	}
}

func TestNativeOjaUpdate_ConvergenceParity(t *testing.T) {
	cfg := DefaultHebbianConfig()

	goW := 0.1
	nativeW := 0.1
	for i := 0; i < 1000; i++ {
		goW = OjaUpdate(goW, 0.8, 0.8, cfg)
		nativeW = NativeOjaUpdate(nativeW, 0.8, 0.8, cfg)
	}

	if math.Abs(goW-nativeW) > 1e-12 {
		t.Errorf("convergence diverged after 1000 iterations: Go=%f, Native=%f", goW, nativeW)
	}
}
