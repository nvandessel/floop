package tiering

import (
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestQuickAssign(t *testing.T) {
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

	plan := QuickAssign(behaviors, 1000)

	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if plan.TokenBudget != 1000 {
		t.Errorf("expected budget 1000, got %d", plan.TokenBudget)
	}
}

func TestQuickAssign_Empty(t *testing.T) {
	plan := QuickAssign(nil, 1000)

	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if plan.TokenBudget != 1000 {
		t.Errorf("expected TokenBudget 1000, got %d", plan.TokenBudget)
	}
	if len(plan.FullBehaviors) != 0 {
		t.Errorf("expected empty FullBehaviors, got %d", len(plan.FullBehaviors))
	}
}

func TestQuickAssign_BudgetEnforced(t *testing.T) {
	now := time.Now()
	behaviors := make([]models.Behavior, 20)
	for i := 0; i < 20; i++ {
		behaviors[i] = models.Behavior{
			ID:         "b" + string(rune('a'+i)),
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.8,
			Priority:   5,
			Content:    models.BehaviorContent{Canonical: "This is a somewhat long behavior content for testing token budget limits"},
			Stats:      models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		}
	}

	plan := QuickAssign(behaviors, 500)

	if plan.TotalTokens > 500 {
		t.Errorf("total tokens %d exceeds budget 500", plan.TotalTokens)
	}
}

func TestInjectionPlan_Methods(t *testing.T) {
	plan := &models.InjectionPlan{
		FullBehaviors:       make([]models.InjectedBehavior, 5),
		SummarizedBehaviors: make([]models.InjectedBehavior, 3),
		OmittedBehaviors:    make([]models.InjectedBehavior, 2),
	}

	if plan.BehaviorCount() != 10 {
		t.Errorf("BehaviorCount() = %d, want 10", plan.BehaviorCount())
	}

	if plan.IncludedCount() != 8 {
		t.Errorf("IncludedCount() = %d, want 8", plan.IncludedCount())
	}

	included := plan.IncludedBehaviors()
	if len(included) != 8 {
		t.Errorf("IncludedBehaviors() len = %d, want 8", len(included))
	}

	all := plan.AllBehaviors()
	if len(all) != 10 {
		t.Errorf("AllBehaviors() len = %d, want 10", len(all))
	}
}
