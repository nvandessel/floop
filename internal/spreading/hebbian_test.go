package spreading

import (
	"math"
	"testing"
	"time"
)

func TestOjaUpdate_BasicStrengthening(t *testing.T) {
	cfg := DefaultHebbianConfig()

	// Two co-activated behaviors should strengthen their edge
	oldWeight := 0.5
	newWeight := OjaUpdate(oldWeight, 0.8, 0.8, cfg)

	if newWeight <= oldWeight {
		t.Errorf("co-activation should strengthen: old=%f, new=%f", oldWeight, newWeight)
	}
}

func TestOjaUpdate_SelfLimiting(t *testing.T) {
	cfg := DefaultHebbianConfig()

	// Oja's rule is self-limiting: weight should converge, not grow unbounded.
	// Run 1000 iterations with constant activation.
	w := 0.1
	for i := 0; i < 1000; i++ {
		w = OjaUpdate(w, 0.8, 0.8, cfg)
	}

	// Weight should converge to activationA/activationB = 1.0 (clamped to MaxWeight)
	// but should NOT exceed MaxWeight
	if w > cfg.MaxWeight {
		t.Errorf("weight %f exceeds MaxWeight %f after 1000 iterations", w, cfg.MaxWeight)
	}
	if w < 0.5 {
		t.Errorf("weight %f should converge to a high value, not stay low", w)
	}
}

func TestOjaUpdate_Convergence(t *testing.T) {
	cfg := DefaultHebbianConfig()
	cfg.MaxWeight = 1.0 // Remove ceiling to test pure Oja convergence

	// With equal activations (A=B=x), Oja converges to w = A/B = 1.0
	w := 0.1
	for i := 0; i < 2000; i++ {
		w = OjaUpdate(w, 0.6, 0.6, cfg)
	}

	// Should converge near 1.0 (the ratio A_i/A_j when A_i == A_j)
	if math.Abs(w-1.0) > 0.01 {
		t.Errorf("weight should converge to 1.0, got %f", w)
	}
}

func TestOjaUpdate_AsymmetricActivation(t *testing.T) {
	cfg := DefaultHebbianConfig()
	cfg.MaxWeight = 2.0 // High ceiling to test convergence target

	// With A_i=0.8, A_j=0.4, Oja converges to w = A_i/A_j = 2.0
	w := 0.1
	for i := 0; i < 5000; i++ {
		w = OjaUpdate(w, 0.8, 0.4, cfg)
	}

	// Should converge near A_i/A_j = 0.8/0.4 = 2.0
	if math.Abs(w-2.0) > 0.05 {
		t.Errorf("weight should converge to 2.0, got %f", w)
	}
}

func TestOjaUpdate_WeakensOnLowActivation(t *testing.T) {
	cfg := DefaultHebbianConfig()

	// When forgetting term dominates (A_j^2 * W > A_i * A_j), Oja weakens.
	// With A_i=0.1, A_j=0.5, W=0.9: forgetting=0.25*0.9=0.225 > hebbian=0.05
	oldWeight := 0.9
	newWeight := OjaUpdate(oldWeight, 0.1, 0.5, cfg)

	if newWeight >= oldWeight {
		t.Errorf("forgetting should dominate: old=%f, new=%f", oldWeight, newWeight)
	}
}

func TestOjaUpdate_Clamping(t *testing.T) {
	cfg := DefaultHebbianConfig()

	tests := []struct {
		name       string
		current    float64
		actA, actB float64
		wantMin    float64
		wantMax    float64
	}{
		{
			name:    "clamp to MinWeight",
			current: 0.01,
			actA:    0.0,
			actB:    0.0,
			wantMin: cfg.MinWeight,
			wantMax: cfg.MinWeight,
		},
		{
			name:    "clamp to MaxWeight",
			current: 0.96, // Above MaxWeight → will be clamped
			actA:    1.0,
			actB:    1.0,
			wantMin: cfg.MaxWeight,
			wantMax: cfg.MaxWeight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OjaUpdate(tt.current, tt.actA, tt.actB, cfg)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("OjaUpdate(%f, %f, %f) = %f, want in [%f, %f]",
					tt.current, tt.actA, tt.actB, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestOjaUpdate_NaN(t *testing.T) {
	cfg := DefaultHebbianConfig()
	got := OjaUpdate(math.NaN(), 0.5, 0.5, cfg)
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Errorf("NaN input should produce clamped output, got %f", got)
	}
}

func TestOjaUpdate_AntiAntMill(t *testing.T) {
	cfg := DefaultHebbianConfig()

	// Triangle stability test: 3 behaviors with equal activation.
	// No single edge should dominate after convergence.
	wAB := 0.3
	wBC := 0.3
	wAC := 0.3
	act := 0.7 // Equal activation for all

	for i := 0; i < 500; i++ {
		wAB = OjaUpdate(wAB, act, act, cfg)
		wBC = OjaUpdate(wBC, act, act, cfg)
		wAC = OjaUpdate(wAC, act, act, cfg)
	}

	// All edges should converge to similar values (within 1%)
	if math.Abs(wAB-wBC) > 0.01 {
		t.Errorf("anti-ant-mill: wAB=%f, wBC=%f should be equal", wAB, wBC)
	}
	if math.Abs(wAB-wAC) > 0.01 {
		t.Errorf("anti-ant-mill: wAB=%f, wAC=%f should be equal", wAB, wAC)
	}
}

func TestExtractCoActivationPairs_Basic(t *testing.T) {
	cfg := DefaultHebbianConfig()
	cfg.ActivationThreshold = 0.3

	results := []Result{
		{BehaviorID: "a", Activation: 0.8},
		{BehaviorID: "b", Activation: 0.6},
		{BehaviorID: "c", Activation: 0.1}, // Below threshold
	}

	pairs := ExtractCoActivationPairs(results, nil, cfg)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].BehaviorA != "a" || pairs[0].BehaviorB != "b" {
		t.Errorf("expected pair (a,b), got (%s,%s)", pairs[0].BehaviorA, pairs[0].BehaviorB)
	}
}

func TestExtractCoActivationPairs_CanonicalOrder(t *testing.T) {
	cfg := DefaultHebbianConfig()
	cfg.ActivationThreshold = 0.3

	// Results in reverse alphabetical order
	results := []Result{
		{BehaviorID: "z", Activation: 0.8},
		{BehaviorID: "a", Activation: 0.6},
	}

	pairs := ExtractCoActivationPairs(results, nil, cfg)

	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	// Should be canonical: a before z
	if pairs[0].BehaviorA != "a" || pairs[0].BehaviorB != "z" {
		t.Errorf("expected canonical order (a,z), got (%s,%s)", pairs[0].BehaviorA, pairs[0].BehaviorB)
	}
}

func TestExtractCoActivationPairs_ExcludeSeedSeedPairs(t *testing.T) {
	cfg := DefaultHebbianConfig()
	cfg.ActivationThreshold = 0.3

	results := []Result{
		{BehaviorID: "seed1", Activation: 0.9},
		{BehaviorID: "seed2", Activation: 0.8},
		{BehaviorID: "spread1", Activation: 0.7},
	}

	seedIDs := map[string]bool{
		"seed1": true,
		"seed2": true,
	}

	pairs := ExtractCoActivationPairs(results, seedIDs, cfg)

	// seed1-seed2 should be excluded (both seeds)
	// seed1-spread1 and seed2-spread1 should be included
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs (excluding seed-seed), got %d", len(pairs))
	}

	// Verify no seed-seed pair exists
	for _, p := range pairs {
		if seedIDs[p.BehaviorA] && seedIDs[p.BehaviorB] {
			t.Errorf("seed-seed pair should be excluded: (%s, %s)", p.BehaviorA, p.BehaviorB)
		}
	}
}

func TestExtractCoActivationPairs_TooFewActive(t *testing.T) {
	cfg := DefaultHebbianConfig()
	cfg.ActivationThreshold = 0.5

	results := []Result{
		{BehaviorID: "a", Activation: 0.8},
		{BehaviorID: "b", Activation: 0.2}, // Below threshold
	}

	pairs := ExtractCoActivationPairs(results, nil, cfg)

	if len(pairs) != 0 {
		t.Errorf("expected 0 pairs when only 1 active, got %d", len(pairs))
	}
}

func TestExtractCoActivationPairs_MultiplePairs(t *testing.T) {
	cfg := DefaultHebbianConfig()
	cfg.ActivationThreshold = 0.3

	results := []Result{
		{BehaviorID: "a", Activation: 0.8},
		{BehaviorID: "b", Activation: 0.7},
		{BehaviorID: "c", Activation: 0.6},
	}

	pairs := ExtractCoActivationPairs(results, nil, cfg)

	// 3 behaviors => 3 pairs: (a,b), (a,c), (b,c)
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(pairs))
	}
}

func TestDefaultHebbianConfig(t *testing.T) {
	cfg := DefaultHebbianConfig()

	if cfg.LearningRate != 0.05 {
		t.Errorf("LearningRate = %f, want 0.05", cfg.LearningRate)
	}
	if cfg.MinWeight != 0.01 {
		t.Errorf("MinWeight = %f, want 0.01", cfg.MinWeight)
	}
	if cfg.MaxWeight != 0.95 {
		t.Errorf("MaxWeight = %f, want 0.95", cfg.MaxWeight)
	}
	if cfg.ActivationThreshold != 0.3 {
		t.Errorf("ActivationThreshold = %f, want 0.3", cfg.ActivationThreshold)
	}
	if cfg.CreationGate != 3 {
		t.Errorf("CreationGate = %d, want 3", cfg.CreationGate)
	}
	if cfg.CreationWindow != 7*24*time.Hour {
		t.Errorf("CreationWindow = %v, want 7 days", cfg.CreationWindow)
	}
}

func TestClampWeight(t *testing.T) {
	tests := []struct {
		name string
		w    float64
		min  float64
		max  float64
		want float64
	}{
		{"in range", 0.5, 0.01, 0.95, 0.5},
		{"below min", -0.1, 0.01, 0.95, 0.01},
		{"above max", 1.5, 0.01, 0.95, 0.95},
		{"NaN", math.NaN(), 0.01, 0.95, 0.01},
		{"Inf", math.Inf(1), 0.01, 0.95, 0.01},
		{"-Inf", math.Inf(-1), 0.01, 0.95, 0.01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampWeight(tt.w, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("clampWeight(%f, %f, %f) = %f, want %f",
					tt.w, tt.min, tt.max, got, tt.want)
			}
		})
	}
}
