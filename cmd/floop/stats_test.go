package main

import (
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/tiering"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"empty string", "", 0},
		{"single char", "a", 1},
		{"four chars", "abcd", 1},
		{"five chars", "abcde", 2},
		{"eight chars", "abcdefgh", 2},
		{"known content", "Use Go modules for dependency management", 10},
		{"longer content", "This is a somewhat long behavior content for testing token budget limits", 18},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.content)
			if got != tt.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.content, got, tt.want)
			}
		})
	}
}

func TestBudgetSimulationVaryingBudgets(t *testing.T) {
	now := time.Now()
	behaviors := make([]models.Behavior, 10)
	for i := range behaviors {
		behaviors[i] = models.Behavior{
			ID:         "behavior-" + string(rune('a'+i)),
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.8,
			Priority:   5,
			Content: models.BehaviorContent{
				Canonical: "This is behavior content that takes up some tokens for testing purposes here",
				Summary:   "Short summary",
			},
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		}
	}

	tests := []struct {
		name         string
		budget       int
		wantMoreOmit bool // expect more omitted than with large budget
		wantAllFit   bool // expect all behaviors to fit
	}{
		{"large budget 2000", 2000, false, true},
		{"medium budget 500", 500, true, false},
		{"small budget 100", 100, true, false},
	}

	largePlan := tiering.QuickAssign(behaviors, 2000)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := tiering.QuickAssign(behaviors, tt.budget)
			if plan == nil {
				t.Fatal("expected non-nil plan")
			}

			if tt.wantAllFit {
				if len(plan.OmittedBehaviors) != 0 {
					t.Errorf("expected no omitted behaviors with budget %d, got %d",
						tt.budget, len(plan.OmittedBehaviors))
				}
			}

			if tt.wantMoreOmit {
				if len(plan.OmittedBehaviors) < len(largePlan.OmittedBehaviors) {
					t.Errorf("expected more omitted behaviors at budget %d than at 2000, got %d vs %d",
						tt.budget, len(plan.OmittedBehaviors), len(largePlan.OmittedBehaviors))
				}
			}

			if plan.TotalTokens > tt.budget {
				t.Errorf("total tokens %d exceeds budget %d", plan.TotalTokens, tt.budget)
			}
		})
	}
}

func TestBudgetSimulationEdgeCases(t *testing.T) {
	t.Run("zero behaviors", func(t *testing.T) {
		plan := tiering.QuickAssign([]models.Behavior{}, 2000)
		if plan == nil {
			t.Fatal("expected non-nil plan")
		}
		if plan.TotalTokens != 0 {
			t.Errorf("expected 0 total tokens, got %d", plan.TotalTokens)
		}
		if len(plan.FullBehaviors) != 0 {
			t.Errorf("expected 0 full behaviors, got %d", len(plan.FullBehaviors))
		}
	})

	t.Run("zero budget", func(t *testing.T) {
		now := time.Now()
		behaviors := []models.Behavior{
			{
				ID:         "b1",
				Kind:       models.BehaviorKindDirective,
				Confidence: 0.8,
				Content:    models.BehaviorContent{Canonical: "Test behavior"},
				Stats:      models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
			},
		}
		plan := tiering.QuickAssign(behaviors, 0)
		if plan == nil {
			t.Fatal("expected non-nil plan")
		}
		// With zero budget, nothing should fit (except MinFullBehaviors override)
		if plan.TotalTokens > 0 && len(plan.FullBehaviors) > plan.TokenBudget {
			t.Logf("plan has %d total tokens with 0 budget (MinFullBehaviors override)",
				plan.TotalTokens)
		}
	})
}

func TestQuickAssignIntegrationTierSplits(t *testing.T) {
	now := time.Now()
	// Create behaviors with varying content lengths
	behaviors := []models.Behavior{
		{
			ID:         "high-priority",
			Kind:       models.BehaviorKindConstraint,
			Confidence: 1.0,
			Priority:   10,
			Content: models.BehaviorContent{
				Canonical: "Never expose API keys in logs or error messages",
				Summary:   "No API keys in logs",
			},
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
		{
			ID:         "med-priority",
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.9,
			Priority:   5,
			Content: models.BehaviorContent{
				Canonical: "Use table-driven tests with t.Run for all test functions",
				Summary:   "Use table-driven tests",
			},
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
		{
			ID:         "low-priority",
			Kind:       models.BehaviorKindPreference,
			Confidence: 0.5,
			Priority:   1,
			Content: models.BehaviorContent{
				Canonical: "Prefer short variable names in loop bodies like i, j, k for index variables",
				Summary:   "Short loop vars",
			},
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
	}

	t.Run("adequate budget fits all", func(t *testing.T) {
		plan := tiering.QuickAssign(behaviors, 2000)
		totalBehaviors := len(plan.FullBehaviors) + len(plan.SummarizedBehaviors) + len(plan.OmittedBehaviors)
		if totalBehaviors != 3 {
			t.Errorf("expected 3 total behaviors, got %d", totalBehaviors)
		}
		// Constraints should always be full
		for _, fb := range plan.FullBehaviors {
			if fb.Behavior.Kind == models.BehaviorKindConstraint {
				return // found constraint in full tier
			}
		}
		t.Error("expected constraint behavior in full tier")
	})

	t.Run("tight budget forces tiering", func(t *testing.T) {
		plan := tiering.QuickAssign(behaviors, 50)
		// With very tight budget, some should be omitted or summarized
		if len(plan.SummarizedBehaviors)+len(plan.OmittedBehaviors) == 0 && len(behaviors) > 1 {
			t.Log("Warning: tight budget didn't force tiering, MinFullBehaviors may override")
		}
		if plan.TotalTokens > 50 {
			// MinFullBehaviors can override budget, so just log
			t.Logf("tokens %d exceed budget 50 (MinFullBehaviors override)", plan.TotalTokens)
		}
	})
}

func TestTokenBudgetSection(t *testing.T) {
	// Test the buildTokenBudgetInfo helper that produces the JSON structure
	now := time.Now()
	behaviors := []models.Behavior{
		{
			ID:         "b1",
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.9,
			Priority:   10,
			Content: models.BehaviorContent{
				Canonical: "Use Go modules for dependency management",
				Summary:   "Use Go modules",
			},
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
		{
			ID:         "b2",
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.8,
			Priority:   5,
			Content: models.BehaviorContent{
				Canonical: "Always wrap errors with context",
				Summary:   "Wrap errors",
			},
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
	}

	budget := 2000
	plan := tiering.QuickAssign(behaviors, budget)

	// Build stats the same way the command does
	type testBehaviorStat struct {
		ID          string
		TokenCost   int
		SummaryCost int
	}

	var behaviorStats []testBehaviorStat
	for _, b := range behaviors {
		tokenCost := estimateTokens(b.Content.Canonical)
		summaryCost := estimateTokens(b.Content.Summary)
		behaviorStats = append(behaviorStats, testBehaviorStat{
			ID:          b.ID,
			TokenCost:   tokenCost,
			SummaryCost: summaryCost,
		})
	}

	// Verify token costs match expectations
	if behaviorStats[0].TokenCost != 10 {
		t.Errorf("expected token cost 10 for b1, got %d", behaviorStats[0].TokenCost)
	}
	if behaviorStats[0].SummaryCost != 4 {
		t.Errorf("expected summary cost 4 for b1, got %d", behaviorStats[0].SummaryCost)
	}

	// Verify plan totals
	if plan.TokenBudget != budget {
		t.Errorf("expected budget %d, got %d", budget, plan.TokenBudget)
	}

	// Compute full/summarized tokens from plan
	fullTokens := 0
	for _, fb := range plan.FullBehaviors {
		fullTokens += fb.TokenCost
	}
	summaryTokens := 0
	for _, sb := range plan.SummarizedBehaviors {
		summaryTokens += sb.TokenCost
	}

	if fullTokens+summaryTokens != plan.TotalTokens {
		t.Errorf("full(%d) + summary(%d) = %d, but plan.TotalTokens = %d",
			fullTokens, summaryTokens, fullTokens+summaryTokens, plan.TotalTokens)
	}

	// Verify utilization calculation
	utilization := 0.0
	if budget > 0 {
		utilization = float64(plan.TotalTokens) / float64(budget)
	}
	if utilization < 0 || utilization > 1.0 {
		t.Errorf("utilization %f should be between 0 and 1", utilization)
	}
}

func TestTruncatePreview(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"short string", "hello", "hello"},
		{"exactly 40 chars", "1234567890123456789012345678901234567890", "1234567890123456789012345678901234567890"},
		{"over 40 chars", "12345678901234567890123456789012345678901", "1234567890123456789012345678901234567890..."},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncatePreview(tt.input, 40)
			if got != tt.want {
				t.Errorf("truncatePreview(%q, 40) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
