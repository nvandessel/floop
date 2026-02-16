package simulation_test

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

// TestHebbianLearningBreadth validates that Hebbian co-activation learning
// spans beyond the top-7 behaviors. With 20 behaviors across 4 context groups,
// cycling through contexts should produce co-activation pairs involving more
// than 7 unique behaviors.
//
// Setup: 20 behaviors across 4 groups (go, python, testing, dev).
// Sessions cycle through the 4 contexts.
// Expected: co-activation pairs include >7 unique behaviors total,
// and cross-group edges may form when behaviors co-activate.
func TestHebbianLearningBreadth(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		// Go group (5)
		{ID: "go-1", Name: "Go conventions", Kind: models.BehaviorKindDirective, Canonical: "Follow Go conventions", Tags: []string{"go"}},
		{ID: "go-2", Name: "Go error handling", Kind: models.BehaviorKindDirective, Canonical: "Use error wrapping", Tags: []string{"go"}},
		{ID: "go-3", Name: "Go interfaces", Kind: models.BehaviorKindDirective, Canonical: "Accept interfaces return structs", Tags: []string{"go"}},
		{ID: "go-4", Name: "Go testing", Kind: models.BehaviorKindDirective, Canonical: "Table-driven tests", Tags: []string{"go", "testing"}},
		{ID: "go-5", Name: "Go context", Kind: models.BehaviorKindDirective, Canonical: "Pass context.Context", Tags: []string{"go"}},
		// Python group (5)
		{ID: "py-1", Name: "Python PEP8", Kind: models.BehaviorKindDirective, Canonical: "Follow PEP8 style", Tags: []string{"python"}},
		{ID: "py-2", Name: "Python typing", Kind: models.BehaviorKindDirective, Canonical: "Use type hints", Tags: []string{"python"}},
		{ID: "py-3", Name: "Python venv", Kind: models.BehaviorKindDirective, Canonical: "Use virtual environments", Tags: []string{"python"}},
		{ID: "py-4", Name: "Python pytest", Kind: models.BehaviorKindDirective, Canonical: "Use pytest for testing", Tags: []string{"python", "testing"}},
		{ID: "py-5", Name: "Python pathlib", Kind: models.BehaviorKindPreference, Canonical: "Prefer pathlib", Tags: []string{"python"}},
		// Testing group (5)
		{ID: "test-1", Name: "TDD workflow", Kind: models.BehaviorKindProcedure, Canonical: "Red green refactor", Tags: []string{"testing"}},
		{ID: "test-2", Name: "Test isolation", Kind: models.BehaviorKindConstraint, Canonical: "Tests must be isolated", Tags: []string{"testing"}},
		{ID: "test-3", Name: "No flaky tests", Kind: models.BehaviorKindConstraint, Canonical: "Fix flaky tests immediately", Tags: []string{"testing"}},
		{ID: "test-4", Name: "Coverage targets", Kind: models.BehaviorKindDirective, Canonical: "Maintain 80% coverage", Tags: []string{"testing"}},
		{ID: "test-5", Name: "Integration tests", Kind: models.BehaviorKindDirective, Canonical: "Write integration tests", Tags: []string{"testing"}},
		// Dev group (5)
		{ID: "dev-1", Name: "Git workflow", Kind: models.BehaviorKindProcedure, Canonical: "Feature branch workflow", Tags: []string{"dev"}},
		{ID: "dev-2", Name: "Code review", Kind: models.BehaviorKindProcedure, Canonical: "Review before merge", Tags: []string{"dev"}},
		{ID: "dev-3", Name: "Documentation", Kind: models.BehaviorKindDirective, Canonical: "Document public APIs", Tags: []string{"dev"}},
		{ID: "dev-4", Name: "Logging", Kind: models.BehaviorKindDirective, Canonical: "Use structured logging", Tags: []string{"dev"}},
		{ID: "dev-5", Name: "Error handling", Kind: models.BehaviorKindDirective, Canonical: "Handle errors explicitly", Tags: []string{"dev"}},
	}

	// Semantic edges within each group to enable spreading.
	edges := []simulation.EdgeSpec{
		// Go cluster
		{Source: "go-1", Target: "go-2", Kind: "semantic", Weight: 0.7},
		{Source: "go-1", Target: "go-3", Kind: "semantic", Weight: 0.6},
		{Source: "go-2", Target: "go-4", Kind: "semantic", Weight: 0.5},
		{Source: "go-3", Target: "go-5", Kind: "semantic", Weight: 0.5},
		// Python cluster
		{Source: "py-1", Target: "py-2", Kind: "semantic", Weight: 0.7},
		{Source: "py-1", Target: "py-3", Kind: "semantic", Weight: 0.6},
		{Source: "py-2", Target: "py-4", Kind: "semantic", Weight: 0.5},
		{Source: "py-3", Target: "py-5", Kind: "semantic", Weight: 0.5},
		// Testing cluster
		{Source: "test-1", Target: "test-2", Kind: "semantic", Weight: 0.7},
		{Source: "test-1", Target: "test-3", Kind: "semantic", Weight: 0.6},
		{Source: "test-2", Target: "test-4", Kind: "semantic", Weight: 0.5},
		{Source: "test-3", Target: "test-5", Kind: "semantic", Weight: 0.5},
		// Dev cluster
		{Source: "dev-1", Target: "dev-2", Kind: "semantic", Weight: 0.7},
		{Source: "dev-1", Target: "dev-3", Kind: "semantic", Weight: 0.6},
		{Source: "dev-2", Target: "dev-4", Kind: "semantic", Weight: 0.5},
		{Source: "dev-3", Target: "dev-5", Kind: "semantic", Weight: 0.5},
		// Cross-group bridges (testing touches go and python)
		{Source: "go-4", Target: "test-1", Kind: "semantic", Weight: 0.4},
		{Source: "py-4", Target: "test-1", Kind: "semantic", Weight: 0.4},
	}

	sessions := make([]simulation.SessionContext, 40)

	hebbianCfg := spreading.DefaultHebbianConfig()
	hebbianCfg.ActivationThreshold = 0.1 // Lower threshold to catch spreading-reached behaviors

	scenario := simulation.Scenario{
		Name:           "hebbian-breadth",
		Behaviors:      behaviors,
		Edges:          edges,
		Sessions:       sessions,
		HebbianConfig:  &hebbianCfg,
		HebbianEnabled: true,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			// Cycle through 4 context groups.
			switch sessionIndex % 4 {
			case 0: // Go context
				return []spreading.Seed{
					{BehaviorID: "go-1", Activation: 0.8, Source: "context:language=go"},
					{BehaviorID: "go-2", Activation: 0.7, Source: "context:language=go"},
				}
			case 1: // Python context
				return []spreading.Seed{
					{BehaviorID: "py-1", Activation: 0.8, Source: "context:language=python"},
					{BehaviorID: "py-2", Activation: 0.7, Source: "context:language=python"},
				}
			case 2: // Testing context
				return []spreading.Seed{
					{BehaviorID: "test-1", Activation: 0.8, Source: "context:task=testing"},
					{BehaviorID: "test-2", Activation: 0.7, Source: "context:task=testing"},
				}
			default: // Dev context
				return []spreading.Seed{
					{BehaviorID: "dev-1", Activation: 0.8, Source: "context:task=dev"},
					{BehaviorID: "dev-2", Activation: 0.7, Source: "context:task=dev"},
				}
			}
		},
	}

	result := r.Run(scenario)

	// Assertion 1: Co-activation pairs span more than 7 unique behaviors.
	simulation.AssertDiverseCoActivation(t, result, 8)

	// Assertion 2: No weight explosion.
	simulation.AssertNoWeightExplosion(t, result, 0.95)

	// Assertion 3: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	// Log diversity stats.
	unique := make(map[string]bool)
	for _, sr := range result.Sessions {
		for _, p := range sr.Pairs {
			unique[p.BehaviorA] = true
			unique[p.BehaviorB] = true
		}
	}
	t.Logf("Unique behaviors in co-activation pairs: %d", len(unique))
}
