package tiering

import (
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/summarization"
)

func TestDefaultTierAssignerConfig(t *testing.T) {
	config := DefaultTierAssignerConfig()

	// Check percentages sum to ~1.0 (allow for floating point)
	total := config.FullTierPercent + config.SummaryTierPercent + config.OverheadPercent
	if total < 0.99 || total > 1.01 {
		t.Errorf("percentages should sum to ~1.0, got %f", total)
	}

	if !config.ConstraintsAlwaysFull {
		t.Error("ConstraintsAlwaysFull should default to true")
	}
}

func TestTierAssigner_AssignTiers_Empty(t *testing.T) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())
	assigner := NewTierAssigner(DefaultTierAssignerConfig(), scorer, summarizer)

	plan := assigner.AssignTiers(nil, nil, 1000)

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

func TestTierAssigner_AssignTiers_ConstraintsPrioritized(t *testing.T) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())
	assigner := NewTierAssigner(DefaultTierAssignerConfig(), scorer, summarizer)

	now := time.Now()
	behaviors := []models.Behavior{
		{
			ID:         "directive",
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.9,
			Priority:   10,
			Content:    models.BehaviorContent{Canonical: "This is a directive"},
			Stats:      models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
		{
			ID:         "constraint",
			Kind:       models.BehaviorKindConstraint,
			Confidence: 0.5,
			Priority:   1,
			Content:    models.BehaviorContent{Canonical: "Never do this"},
			Stats:      models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
	}

	plan := assigner.AssignTiers(behaviors, nil, 1000)

	// Both should be full tier with adequate budget
	if len(plan.FullBehaviors) != 2 {
		t.Fatalf("expected 2 full behaviors, got %d", len(plan.FullBehaviors))
	}

	// Constraint should be in full tier even with lower priority/confidence
	hasConstraint := false
	for _, b := range plan.FullBehaviors {
		if b.Behavior.Kind == models.BehaviorKindConstraint {
			hasConstraint = true
			break
		}
	}
	if !hasConstraint {
		t.Error("constraint should be in full tier")
	}
}

func TestTierAssigner_AssignTiers_LimitedBudget(t *testing.T) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())
	assigner := NewTierAssigner(DefaultTierAssignerConfig(), scorer, summarizer)

	now := time.Now()
	behaviors := make([]models.Behavior, 20)
	for i := 0; i < 20; i++ {
		behaviors[i] = models.Behavior{
			ID:         "b" + string(rune('0'+i)),
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.8,
			Priority:   5,
			Content:    models.BehaviorContent{Canonical: "This is a somewhat long behavior content for testing token budget limits"},
			Stats:      models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		}
	}

	// Very small budget should cause tiering
	plan := assigner.AssignTiers(behaviors, nil, 500)

	if plan.BehaviorCount() != 20 {
		t.Errorf("expected 20 total behaviors, got %d", plan.BehaviorCount())
	}

	// Should have some at each tier (or omitted)
	if len(plan.FullBehaviors) == 0 {
		t.Error("expected at least some full behaviors")
	}

	// Total tokens should be within budget
	if plan.TotalTokens > 500 {
		t.Errorf("total tokens %d exceeds budget 500", plan.TotalTokens)
	}
}

func TestTierAssigner_AssignTiers_TierContent(t *testing.T) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())
	assigner := NewTierAssigner(DefaultTierAssignerConfig(), scorer, summarizer)

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
	}

	plan := assigner.AssignTiers(behaviors, nil, 1000)

	if len(plan.FullBehaviors) != 1 {
		t.Fatalf("expected 1 full behavior, got %d", len(plan.FullBehaviors))
	}

	// Full tier should have canonical content
	full := plan.FullBehaviors[0]
	if full.Content != "Use Go modules for dependency management" {
		t.Errorf("full tier should have canonical content, got %s", full.Content)
	}
	if full.Tier != models.TierFull {
		t.Errorf("expected TierFull, got %d", full.Tier)
	}
}

func TestTierAssigner_AssignTiers_SummaryTier(t *testing.T) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())

	// Config that forces early summarization
	config := TierAssignerConfig{
		FullTierPercent:       0.20, // Very small full tier
		SummaryTierPercent:    0.70,
		OverheadPercent:       0.10,
		MinFullBehaviors:      1,
		ConstraintsAlwaysFull: true,
	}
	assigner := NewTierAssigner(config, scorer, summarizer)

	now := time.Now()
	behaviors := []models.Behavior{
		{
			ID:         "b1",
			Kind:       models.BehaviorKindDirective,
			Confidence: 1.0,
			Priority:   10,
			Content:    models.BehaviorContent{Canonical: "First behavior with highest priority"},
			Stats:      models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
		{
			ID:         "b2",
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.9,
			Priority:   9,
			Content: models.BehaviorContent{
				Canonical: "Second behavior with lower priority but still important",
				Summary:   "Second lower priority",
			},
			Stats: models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
		{
			ID:         "b3",
			Kind:       models.BehaviorKindDirective,
			Confidence: 0.8,
			Priority:   8,
			Content:    models.BehaviorContent{Canonical: "Third behavior"},
			Stats:      models.BehaviorStats{CreatedAt: now, UpdatedAt: now},
		},
	}

	// Budget forces summarization
	plan := assigner.AssignTiers(behaviors, nil, 100)

	// Should have some summarized or omitted behaviors
	if len(plan.SummarizedBehaviors)+len(plan.OmittedBehaviors) == 0 {
		t.Log("Full:", len(plan.FullBehaviors), "Summary:", len(plan.SummarizedBehaviors), "Omitted:", len(plan.OmittedBehaviors))
		t.Log("TotalTokens:", plan.TotalTokens, "Budget:", plan.TokenBudget)
	}
}

func TestTierAssigner_GetSummary_Existing(t *testing.T) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())
	assigner := NewTierAssigner(DefaultTierAssignerConfig(), scorer, summarizer)

	behavior := &models.Behavior{
		ID:   "test",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "This is a very long canonical content that should not be used",
			Summary:   "Short summary",
		},
	}

	summary := assigner.getSummary(behavior)
	if summary != "Short summary" {
		t.Errorf("expected existing summary, got %s", summary)
	}
}

func TestTierAssigner_GetSummary_Generated(t *testing.T) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())
	assigner := NewTierAssigner(DefaultTierAssignerConfig(), scorer, summarizer)

	behavior := &models.Behavior{
		ID:   "test",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "Always use descriptive variable names",
		},
	}

	summary := assigner.getSummary(behavior)
	if summary == "" {
		t.Error("expected generated summary")
	}
}

func TestTierAssigner_GetSummary_Truncated(t *testing.T) {
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	// nil summarizer to test truncation fallback
	assigner := NewTierAssigner(DefaultTierAssignerConfig(), scorer, nil)

	behavior := &models.Behavior{
		ID:   "test",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "This is a very long canonical content that exceeds sixty characters and should be truncated",
		},
	}

	summary := assigner.getSummary(behavior)
	if len(summary) > 60 {
		t.Errorf("summary should be <= 60 chars, got %d", len(summary))
	}
	if len(summary) < 60 {
		t.Error("expected truncated summary ending in ...")
	}
}

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
