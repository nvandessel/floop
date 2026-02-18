package simulation_test

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
)

// TestSeedSelectionIntegration exercises the real Pipeline.Run() path — no
// SeedOverride. It validates that SeedSelector.SelectSeeds() correctly matches
// behavior When conditions against ContextSnapshot fields, and that specificity
// maps to the correct seed activation levels.
//
// Setup:
//   - 6 behaviors with varying When conditions and specificity levels
//   - 2 semantic edges to allow spreading from seeds to reachable neighbors
//   - 6 sessions cycling through different ContextSnapshots
//   - SeedOverride is nil — the scenario uses Pipeline.Run() (runner.go line 64-67)
//
// Expected:
//   - Each context snapshot selects only the behaviors whose When conditions
//     match the snapshot's fields
//   - Higher specificity → higher seed activation (0.3 / 0.4 / 0.6)
//   - always-lint (no When) appears in every session at activation 0.3
//   - Language/task-specific behaviors only appear when context matches
func TestSeedSelectionIntegration(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		{
			ID: "go-security", Name: "Go Security", Kind: models.BehaviorKindDirective,
			When:      map[string]interface{}{"language": "go", "task": "security"},
			Canonical: "Security checks for Go code",
		},
		{
			ID: "go-general", Name: "Go General", Kind: models.BehaviorKindDirective,
			When:      map[string]interface{}{"language": "go"},
			Canonical: "General Go practices",
		},
		{
			ID: "python-style", Name: "Python Style", Kind: models.BehaviorKindDirective,
			When:      map[string]interface{}{"language": "python"},
			Canonical: "Python style guidelines",
		},
		{
			ID: "ci-deploy", Name: "CI Deploy", Kind: models.BehaviorKindDirective,
			When:      map[string]interface{}{"environment": "ci", "task": "deploy"},
			Canonical: "CI deployment procedures",
		},
		{
			ID: "always-lint", Name: "Always Lint", Kind: models.BehaviorKindDirective,
			// When: nil — always-active, specificity=0 → activation 0.3
			Canonical: "Always run linter",
		},
		{
			ID: "rust-perf", Name: "Rust Performance", Kind: models.BehaviorKindDirective,
			When:      map[string]interface{}{"language": "rust", "task": "performance"},
			Canonical: "Rust performance optimization",
		},
	}

	// Edges so spreading from go-security can reach go-general.
	edges := []simulation.EdgeSpec{
		{Source: "go-security", Target: "go-general", Kind: "semantic", Weight: 0.8},
		{Source: "always-lint", Target: "go-general", Kind: "semantic", Weight: 0.5},
	}

	sessions := []simulation.SessionContext{
		// Session 0: Go + security → matches go-security (spec=2), go-general (spec=1), always-lint (spec=0)
		{ContextSnapshot: models.ContextSnapshot{FileLanguage: "go", Task: "security"}, Label: "go-security"},
		// Session 1: Go + refactor → matches go-general (spec=1), always-lint (spec=0); NOT go-security
		{ContextSnapshot: models.ContextSnapshot{FileLanguage: "go", Task: "refactor"}, Label: "go-refactor"},
		// Session 2: Python + review → matches python-style (spec=1), always-lint (spec=0)
		{ContextSnapshot: models.ContextSnapshot{FileLanguage: "python", Task: "review"}, Label: "python-review"},
		// Session 3: CI + deploy → matches ci-deploy (spec=2), always-lint (spec=0)
		{ContextSnapshot: models.ContextSnapshot{Environment: "ci", Task: "deploy"}, Label: "ci-deploy"},
		// Session 4: Rust + performance → matches rust-perf (spec=2), always-lint (spec=0)
		{ContextSnapshot: models.ContextSnapshot{FileLanguage: "rust", Task: "performance"}, Label: "rust-perf"},
		// Session 5: Repeat of session 0 for Hebbian reinforcement
		{ContextSnapshot: models.ContextSnapshot{FileLanguage: "go", Task: "security"}, Label: "go-security-repeat"},
	}

	// Use default spread config — inhibition enabled, normal propagation.
	// Disable Hebbian to isolate seed selection behavior.
	scenario := simulation.Scenario{
		Name:           "seed-selection-integration",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		HebbianEnabled: false,
		// SeedOverride: nil — uses real Pipeline.Run() path
	}

	result := r.Run(scenario)

	// Log all session results for debugging.
	for _, sr := range result.Sessions {
		t.Logf("%s", simulation.FormatSessionDebug(sr))
	}

	// --- Session 0: Go + security ---
	// go-security (spec=2, act=0.6) and go-general (spec=1, act=0.4) should both be seeds.
	// always-lint (spec=0, act=0.3) should also be a seed.
	s0 := result.Sessions[0]
	simulation.AssertBehaviorIsSeed(t, s0, "go-security")
	simulation.AssertBehaviorIsSeed(t, s0, "go-general")
	simulation.AssertBehaviorIsSeed(t, s0, "always-lint")
	// go-security should have higher activation than go-general (0.6 > 0.4 pre-sigmoid).
	assertActivationHigher(t, s0, "go-security", "go-general")

	// --- Session 1: Go + refactor ---
	// go-general should be a seed, go-security should NOT be (task != "security").
	s1 := result.Sessions[1]
	simulation.AssertBehaviorIsSeed(t, s1, "go-general")
	simulation.AssertBehaviorIsSeed(t, s1, "always-lint")
	assertNotSeed(t, s1, "go-security")

	// --- Session 2: Python + review ---
	// python-style should be a seed; Go behaviors should not be seeds.
	s2 := result.Sessions[2]
	simulation.AssertBehaviorIsSeed(t, s2, "python-style")
	simulation.AssertBehaviorIsSeed(t, s2, "always-lint")
	// go-security may appear via spreading (always-lint → go-general → go-security)
	// but should NOT be a seed.
	assertNotSeed(t, s2, "go-security")
	// go-general shouldn't be a seed (wrong language)
	assertNotSeed(t, s2, "go-general")

	// --- Session 3: CI + deploy ---
	// ci-deploy (spec=2) should be a seed.
	// With partial matching, go-general and python-style are also seeds because
	// their language condition is absent (not contradicted). They get low activation
	// via AbsentFloorActivation. go-security and rust-perf are excluded because
	// their task condition is contradicted (deploy != security/performance).
	s3 := result.Sessions[3]
	simulation.AssertBehaviorIsSeed(t, s3, "ci-deploy")
	simulation.AssertBehaviorIsSeed(t, s3, "always-lint")
	simulation.AssertBehaviorIsSeed(t, s3, "go-general")
	simulation.AssertBehaviorIsSeed(t, s3, "python-style")
	assertNotSeed(t, s3, "go-security")
	// ci-deploy should have higher activation than go-general/python-style
	// since ci-deploy is fully confirmed (score=1.0) while they are all-absent (score=0.0).
	assertActivationHigher(t, s3, "ci-deploy", "go-general")
	assertActivationHigher(t, s3, "ci-deploy", "python-style")

	// --- Session 4: Rust + performance ---
	// rust-perf (spec=2) should be a seed.
	s4 := result.Sessions[4]
	simulation.AssertBehaviorIsSeed(t, s4, "rust-perf")
	simulation.AssertBehaviorIsSeed(t, s4, "always-lint")
	assertNotSeed(t, s4, "go-security")

	// --- Session 5: Repeat of session 0 ---
	// Same as session 0: go-security and go-general should be seeds.
	s5 := result.Sessions[5]
	simulation.AssertBehaviorIsSeed(t, s5, "go-security")
	simulation.AssertBehaviorIsSeed(t, s5, "go-general")

	// --- Cross-session assertions ---
	// always-lint should appear in every session (always-active).
	for i, sr := range result.Sessions {
		found := false
		for _, r := range sr.Results {
			if r.BehaviorID == "always-lint" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("always-lint not present in session %d", i)
		}
	}

	// rust-perf should only appear as a seed in session 4.
	simulation.AssertBehaviorIsSeed(t, result.Sessions[4], "rust-perf")
	for _, i := range []int{0, 1, 2, 3, 5} {
		assertNotSeed(t, result.Sessions[i], "rust-perf")
	}

	// All sessions should produce results.
	simulation.AssertResultsNotEmpty(t, result)
}

// assertActivationHigher asserts that behaviorA has higher activation than
// behaviorB in the given session.
func assertActivationHigher(t *testing.T, sr simulation.SessionResult, higherID, lowerID string) {
	t.Helper()
	var higherAct, lowerAct float64
	var foundHigher, foundLower bool
	for _, r := range sr.Results {
		if r.BehaviorID == higherID {
			higherAct = r.Activation
			foundHigher = true
		}
		if r.BehaviorID == lowerID {
			lowerAct = r.Activation
			foundLower = true
		}
	}
	if !foundHigher {
		t.Errorf("session %d: %s not found in results", sr.Index, higherID)
		return
	}
	if !foundLower {
		t.Errorf("session %d: %s not found in results", sr.Index, lowerID)
		return
	}
	if higherAct <= lowerAct {
		t.Errorf("session %d: expected %s (%.4f) > %s (%.4f)", sr.Index, higherID, higherAct, lowerID, lowerAct)
	}
}

// assertNotSeed asserts that a behavior is NOT a seed (distance != 0 or absent)
// in the given session.
func assertNotSeed(t *testing.T, sr simulation.SessionResult, behaviorID string) {
	t.Helper()
	for _, r := range sr.Results {
		if r.BehaviorID == behaviorID && r.Distance == 0 {
			t.Errorf("session %d: behavior %s unexpectedly is a seed (distance=0, activation=%.4f)", sr.Index, behaviorID, r.Activation)
			return
		}
	}
}
