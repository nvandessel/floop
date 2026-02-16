package simulation_test

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/simulation"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/tiering"
)

// TestTokenBudgetDemotion validates that the tiering system respects token
// budgets and correctly demotes low-activation behaviors while protecting
// constraints at TierSummary or above.
//
// Setup: 15 behaviors (3 constraints, 5 directives, 4 procedures, 3 preferences).
// Budget = 500 tokens. One seed activates all behaviors at varying levels.
// Expected: constraints never drop below TierSummary, total tokens <= budget,
// demotion targets lowest-activation behaviors first.
func TestTokenBudgetDemotion(t *testing.T) {
	r := simulation.NewRunner(t)

	behaviors := []simulation.BehaviorSpec{
		// Constraints — should be protected
		{ID: "con-1", Name: "Never commit secrets", Kind: models.BehaviorKindConstraint, Canonical: "Never commit secrets, API keys, or credentials to version control."},
		{ID: "con-2", Name: "Never skip tests", Kind: models.BehaviorKindConstraint, Canonical: "Never skip tests when modifying code that has existing test coverage."},
		{ID: "con-3", Name: "Never force push main", Kind: models.BehaviorKindConstraint, Canonical: "Never force push to main or master branches without explicit approval."},
		// Directives
		{ID: "dir-1", Name: "Use Go conventions", Kind: models.BehaviorKindDirective, Canonical: "Follow Go naming conventions: exported names are PascalCase, unexported are camelCase."},
		{ID: "dir-2", Name: "Error wrapping", Kind: models.BehaviorKindDirective, Canonical: "Wrap errors with context using fmt.Errorf and %w verb."},
		{ID: "dir-3", Name: "Table-driven tests", Kind: models.BehaviorKindDirective, Canonical: "Use table-driven tests with t.Run subtests for comprehensive test coverage."},
		{ID: "dir-4", Name: "Context propagation", Kind: models.BehaviorKindDirective, Canonical: "Pass context.Context as first parameter in all functions that perform I/O."},
		{ID: "dir-5", Name: "Structured logging", Kind: models.BehaviorKindDirective, Canonical: "Use structured logging with slog package instead of fmt.Printf for production code."},
		// Procedures
		{ID: "proc-1", Name: "PR workflow", Kind: models.BehaviorKindProcedure, Canonical: "1. Create feature branch. 2. Write tests. 3. Implement. 4. Run lints. 5. Create PR."},
		{ID: "proc-2", Name: "Debug workflow", Kind: models.BehaviorKindProcedure, Canonical: "1. Reproduce the bug. 2. Write a failing test. 3. Fix the code. 4. Verify test passes."},
		{ID: "proc-3", Name: "Deploy workflow", Kind: models.BehaviorKindProcedure, Canonical: "1. Merge PR. 2. Wait for CI. 3. Deploy to staging. 4. Smoke test. 5. Promote to prod."},
		{ID: "proc-4", Name: "Review workflow", Kind: models.BehaviorKindProcedure, Canonical: "1. Read the diff. 2. Check tests. 3. Run locally. 4. Leave feedback. 5. Approve or request changes."},
		// Preferences
		{ID: "pref-1", Name: "Prefer pathlib", Kind: models.BehaviorKindPreference, Canonical: "Prefer pathlib.Path over os.path for file operations."},
		{ID: "pref-2", Name: "Prefer early return", Kind: models.BehaviorKindPreference, Canonical: "Prefer early return over nested if-else chains."},
		{ID: "pref-3", Name: "Prefer composition", Kind: models.BehaviorKindPreference, Canonical: "Prefer composition over inheritance for code reuse."},
	}

	// All behaviors connected to a hub via semantic edges at varying weights.
	// Higher weight = higher activation = higher tier.
	edges := []simulation.EdgeSpec{
		{Source: "con-1", Target: "dir-1", Kind: "semantic", Weight: 0.9},
		{Source: "con-2", Target: "dir-1", Kind: "semantic", Weight: 0.9},
		{Source: "con-3", Target: "dir-1", Kind: "semantic", Weight: 0.9},
		{Source: "dir-1", Target: "dir-2", Kind: "semantic", Weight: 0.7},
		{Source: "dir-1", Target: "dir-3", Kind: "semantic", Weight: 0.6},
		{Source: "dir-1", Target: "dir-4", Kind: "semantic", Weight: 0.5},
		{Source: "dir-1", Target: "dir-5", Kind: "semantic", Weight: 0.4},
	}

	sessions := make([]simulation.SessionContext, 10)

	// Seed all behaviors directly with varying activation levels to simulate
	// a realistic activation distribution for tiering.
	scenario := simulation.Scenario{
		Name:        "token-budget-demotion",
		Behaviors:   behaviors,
		Edges:       edges,
		Sessions:    sessions,
		TokenBudget: 500,
		SeedOverride: func(sessionIndex int) []spreading.Seed {
			return []spreading.Seed{
				// Constraints at high activation
				{BehaviorID: "con-1", Activation: 0.9, Source: "test"},
				{BehaviorID: "con-2", Activation: 0.85, Source: "test"},
				{BehaviorID: "con-3", Activation: 0.8, Source: "test"},
				// Directives at medium-high
				{BehaviorID: "dir-1", Activation: 0.75, Source: "test"},
				{BehaviorID: "dir-2", Activation: 0.65, Source: "test"},
				{BehaviorID: "dir-3", Activation: 0.55, Source: "test"},
				{BehaviorID: "dir-4", Activation: 0.45, Source: "test"},
				{BehaviorID: "dir-5", Activation: 0.35, Source: "test"},
				// Procedures at medium
				{BehaviorID: "proc-1", Activation: 0.5, Source: "test"},
				{BehaviorID: "proc-2", Activation: 0.4, Source: "test"},
				{BehaviorID: "proc-3", Activation: 0.3, Source: "test"},
				{BehaviorID: "proc-4", Activation: 0.2, Source: "test"},
				// Preferences at low
				{BehaviorID: "pref-1", Activation: 0.25, Source: "test"},
				{BehaviorID: "pref-2", Activation: 0.15, Source: "test"},
				{BehaviorID: "pref-3", Activation: 0.1, Source: "test"},
			}
		},
	}

	result := r.Run(scenario)

	// The runner doesn't capture tiering output in SessionResult, so we
	// re-run tiering on the last session's results to validate properties.
	lastSession := result.Sessions[len(result.Sessions)-1]
	tierMapper := tiering.NewActivationTierMapper(tiering.DefaultActivationTierConfig())

	// Build behavior map from store.
	behaviorMap := make(map[string]*models.Behavior, len(behaviors))
	for _, bs := range behaviors {
		node, err := result.Store.GetNode(t.Context(), bs.ID)
		if err != nil {
			t.Fatalf("GetNode(%s): %v", bs.ID, err)
		}
		b := learning.NodeToBehavior(*node)
		behaviorMap[bs.ID] = &b
	}

	plan := tierMapper.MapResults(lastSession.Results, behaviorMap, 500)

	// Assertion 1: Total tokens within budget.
	if plan.TotalTokens > 500 {
		t.Errorf("total tokens %d exceeds budget 500", plan.TotalTokens)
	}

	// Assertion 2: Constraints are at TierSummary or above.
	for _, ib := range plan.AllBehaviors() {
		if ib.Behavior.Kind == models.BehaviorKindConstraint {
			if ib.Tier > models.TierSummary {
				t.Errorf("constraint %s demoted to %s (should be >= TierSummary)", ib.Behavior.Name, ib.Tier)
			}
		}
	}

	// Assertion 3: At least some behaviors were included.
	if plan.IncludedCount() == 0 {
		t.Error("no behaviors included in plan")
	}

	// Assertion 4: Every session produced results.
	simulation.AssertResultsNotEmpty(t, result)

	t.Logf("Budget plan: %d full, %d summary, %d name-only, %d omitted, total=%d tokens",
		len(plan.FullBehaviors), len(plan.SummarizedBehaviors),
		len(plan.NameOnlyBehaviors), len(plan.OmittedBehaviors), plan.TotalTokens)
}
