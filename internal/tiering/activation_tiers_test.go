package tiering

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

func TestDefaultActivationTierConfig(t *testing.T) {
	config := DefaultActivationTierConfig()

	if config.FullThreshold != 0.7 {
		t.Errorf("FullThreshold = %f, want 0.7", config.FullThreshold)
	}
	if config.SummaryThreshold != 0.3 {
		t.Errorf("SummaryThreshold = %f, want 0.3", config.SummaryThreshold)
	}
	if config.NameOnlyThreshold != 0.1 {
		t.Errorf("NameOnlyThreshold = %f, want 0.1", config.NameOnlyThreshold)
	}
	if config.ConstraintMinTier != models.TierSummary {
		t.Errorf("ConstraintMinTier = %d, want TierSummary (%d)", config.ConstraintMinTier, models.TierSummary)
	}
}

func TestActivationTierMapper_MapTier_Thresholds(t *testing.T) {
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())

	tests := []struct {
		name       string
		activation float64
		kind       models.BehaviorKind
		want       models.InjectionTier
	}{
		{"high activation directive", 0.9, models.BehaviorKindDirective, models.TierFull},
		{"at full threshold", 0.7, models.BehaviorKindDirective, models.TierFull},
		{"medium activation directive", 0.5, models.BehaviorKindDirective, models.TierSummary},
		{"at summary threshold", 0.3, models.BehaviorKindDirective, models.TierSummary},
		{"low activation directive", 0.15, models.BehaviorKindDirective, models.TierNameOnly},
		{"at name-only threshold", 0.1, models.BehaviorKindDirective, models.TierNameOnly},
		{"very low activation", 0.05, models.BehaviorKindDirective, models.TierOmitted},
		{"zero activation", 0.0, models.BehaviorKindDirective, models.TierOmitted},
		{"low activation constraint", 0.15, models.BehaviorKindConstraint, models.TierSummary},
		{"very low activation constraint", 0.05, models.BehaviorKindConstraint, models.TierSummary},
		{"high activation constraint", 0.9, models.BehaviorKindConstraint, models.TierFull},
		{"medium activation constraint", 0.5, models.BehaviorKindConstraint, models.TierSummary},
		{"low activation preference", 0.15, models.BehaviorKindPreference, models.TierNameOnly},
		{"low activation procedure", 0.15, models.BehaviorKindProcedure, models.TierNameOnly},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapper.MapTier(tt.activation, tt.kind)
			if got != tt.want {
				t.Errorf("MapTier(%f, %s) = %s, want %s", tt.activation, tt.kind, got, tt.want)
			}
		})
	}
}

func TestActivationTierMapper_MapTier_ConstraintEnforcement(t *testing.T) {
	// Custom config where constraint min is TierFull
	config := ActivationTierConfig{
		FullThreshold:     0.7,
		SummaryThreshold:  0.3,
		NameOnlyThreshold: 0.1,
		ConstraintMinTier: models.TierFull,
	}
	mapper := NewActivationTierMapper(config)

	// Even with low activation, constraint should get TierFull
	got := mapper.MapTier(0.15, models.BehaviorKindConstraint)
	if got != models.TierFull {
		t.Errorf("constraint with TierFull min: got %s, want %s", got, models.TierFull)
	}

	// Non-constraint should still get TierNameOnly
	got = mapper.MapTier(0.15, models.BehaviorKindDirective)
	if got != models.TierNameOnly {
		t.Errorf("directive at 0.15: got %s, want %s", got, models.TierNameOnly)
	}
}

func TestActivationTierMapper_MapResults_Empty(t *testing.T) {
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())

	plan := mapper.MapResults(nil, nil, 1000)
	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if plan.TokenBudget != 1000 {
		t.Errorf("TokenBudget = %d, want 1000", plan.TokenBudget)
	}
	if plan.BehaviorCount() != 0 {
		t.Errorf("BehaviorCount = %d, want 0", plan.BehaviorCount())
	}
}

func TestActivationTierMapper_MapResults_BasicTiering(t *testing.T) {
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())

	behaviors := map[string]*models.Behavior{
		"high": {
			ID: "high", Name: "high-behavior", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{Canonical: "Full content for high activation"},
		},
		"medium": {
			ID: "medium", Name: "medium-behavior", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Full content for medium activation",
				Summary:   "Medium summary",
			},
		},
		"low": {
			ID: "low", Name: "low-behavior", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Full content for low activation",
				Tags:      []string{"testing", "go"},
			},
		},
		"very-low": {
			ID: "very-low", Name: "very-low-behavior", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{Canonical: "Should be omitted"},
		},
	}

	results := []spreading.Result{
		{BehaviorID: "high", Activation: 0.9, Distance: 0},
		{BehaviorID: "medium", Activation: 0.5, Distance: 1},
		{BehaviorID: "low", Activation: 0.15, Distance: 2},
		{BehaviorID: "very-low", Activation: 0.05, Distance: 3},
	}

	plan := mapper.MapResults(results, behaviors, 10000) // Large budget, no demotion needed

	if len(plan.FullBehaviors) != 1 {
		t.Errorf("FullBehaviors = %d, want 1", len(plan.FullBehaviors))
	}
	if len(plan.SummarizedBehaviors) != 1 {
		t.Errorf("SummarizedBehaviors = %d, want 1", len(plan.SummarizedBehaviors))
	}
	if len(plan.NameOnlyBehaviors) != 1 {
		t.Errorf("NameOnlyBehaviors = %d, want 1", len(plan.NameOnlyBehaviors))
	}
	if len(plan.OmittedBehaviors) != 1 {
		t.Errorf("OmittedBehaviors = %d, want 1", len(plan.OmittedBehaviors))
	}

	// Check that full tier has canonical content
	if len(plan.FullBehaviors) == 1 {
		if plan.FullBehaviors[0].Content != "Full content for high activation" {
			t.Errorf("FullBehaviors[0].Content = %q, want canonical", plan.FullBehaviors[0].Content)
		}
	}

	// Check that summary tier uses summary field
	if len(plan.SummarizedBehaviors) == 1 {
		if plan.SummarizedBehaviors[0].Content != "Medium summary" {
			t.Errorf("SummarizedBehaviors[0].Content = %q, want summary", plan.SummarizedBehaviors[0].Content)
		}
	}

	// Check that name-only tier has name + kind + tags
	if len(plan.NameOnlyBehaviors) == 1 {
		content := plan.NameOnlyBehaviors[0].Content
		if content != "`low-behavior` [directive] #testing #go" {
			t.Errorf("NameOnlyBehaviors[0].Content = %q, want name-only format", content)
		}
	}
}

func TestActivationTierMapper_MapResults_BudgetDemotion(t *testing.T) {
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())

	behaviors := map[string]*models.Behavior{
		"b1": {
			ID: "b1", Name: "behavior-one", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "This is a very long canonical content that takes many tokens to represent in the prompt",
				Summary:   "Short summary for b1",
			},
		},
		"b2": {
			ID: "b2", Name: "behavior-two", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Another fairly long canonical content for testing budget demotion logic",
				Summary:   "Short summary for b2",
			},
		},
		"b3": {
			ID: "b3", Name: "behavior-three", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Yet another long content string that contributes to exceeding the token budget limit",
				Summary:   "Short summary for b3",
				Tags:      []string{"test"},
			},
		},
	}

	results := []spreading.Result{
		{BehaviorID: "b1", Activation: 0.9, Distance: 0},
		{BehaviorID: "b2", Activation: 0.8, Distance: 0},
		{BehaviorID: "b3", Activation: 0.7, Distance: 1},
	}

	// Very small budget should force demotion
	plan := mapper.MapResults(results, behaviors, 30)

	if plan.TotalTokens > 30 {
		t.Errorf("TotalTokens = %d, exceeds budget 30", plan.TotalTokens)
	}

	// At least some behaviors should have been demoted
	demotedCount := len(plan.SummarizedBehaviors) + len(plan.NameOnlyBehaviors) + len(plan.OmittedBehaviors)
	if demotedCount == 0 {
		t.Error("expected at least some behaviors to be demoted under tight budget")
	}
}

func TestActivationTierMapper_MapResults_ConstraintNeverDemotedBelowMin(t *testing.T) {
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())

	behaviors := map[string]*models.Behavior{
		"constraint": {
			ID: "constraint", Name: "safety-constraint", Kind: models.BehaviorKindConstraint,
			Content: models.BehaviorContent{
				Canonical: "Never delete user data without explicit confirmation from the administrator",
				Summary:   "No data deletion without admin confirmation",
			},
		},
		"directive": {
			ID: "directive", Name: "some-directive", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Use descriptive variable names in all code following the project naming conventions",
				Summary:   "Use descriptive names",
				Tags:      []string{"naming"},
			},
		},
	}

	results := []spreading.Result{
		{BehaviorID: "constraint", Activation: 0.5, Distance: 1},
		{BehaviorID: "directive", Activation: 0.5, Distance: 1},
	}

	// Very tight budget should demote directive but not constraint below TierSummary
	plan := mapper.MapResults(results, behaviors, 10)

	// Find the constraint in the plan
	var constraintTier models.InjectionTier = -1
	for _, ib := range plan.AllBehaviors() {
		if ib.Behavior != nil && ib.Behavior.ID == "constraint" {
			constraintTier = ib.Tier
			break
		}
	}

	if constraintTier > models.TierSummary {
		t.Errorf("constraint tier = %s, want at most TierSummary", constraintTier)
	}
}

func TestActivationTierMapper_MapResults_MissingBehavior(t *testing.T) {
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())

	behaviors := map[string]*models.Behavior{
		"exists": {
			ID: "exists", Name: "existing", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{Canonical: "I exist"},
		},
	}

	results := []spreading.Result{
		{BehaviorID: "exists", Activation: 0.9, Distance: 0},
		{BehaviorID: "missing", Activation: 0.8, Distance: 0},
	}

	plan := mapper.MapResults(results, behaviors, 10000)

	// Only the existing behavior should appear
	if plan.BehaviorCount() != 1 {
		t.Errorf("BehaviorCount = %d, want 1 (missing behavior should be skipped)", plan.BehaviorCount())
	}
}

func TestActivationTierMapper_MapResults_TotalTokensAccurate(t *testing.T) {
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())

	behaviors := map[string]*models.Behavior{
		"b1": {
			ID: "b1", Name: "behavior-one", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{Canonical: "Some content"},
		},
		"b2": {
			ID: "b2", Name: "behavior-two", Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Other content",
				Summary:   "Summary",
			},
		},
	}

	results := []spreading.Result{
		{BehaviorID: "b1", Activation: 0.9, Distance: 0},
		{BehaviorID: "b2", Activation: 0.5, Distance: 1},
	}

	plan := mapper.MapResults(results, behaviors, 10000)

	// TotalTokens should be sum of included behaviors only
	expectedTotal := 0
	for _, ib := range plan.FullBehaviors {
		expectedTotal += ib.TokenCost
	}
	for _, ib := range plan.SummarizedBehaviors {
		expectedTotal += ib.TokenCost
	}
	for _, ib := range plan.NameOnlyBehaviors {
		expectedTotal += ib.TokenCost
	}

	if plan.TotalTokens != expectedTotal {
		t.Errorf("TotalTokens = %d, want %d", plan.TotalTokens, expectedTotal)
	}
}

func TestFormatNameOnly(t *testing.T) {
	tests := []struct {
		name     string
		behavior *models.Behavior
		want     string
	}{
		{
			name: "with tags",
			behavior: &models.Behavior{
				Name: "learned/use-cobra-for-cli",
				Kind: models.BehaviorKindDirective,
				Content: models.BehaviorContent{
					Tags: []string{"cli", "cobra"},
				},
			},
			want: "`learned/use-cobra-for-cli` [directive] #cli #cobra",
		},
		{
			name: "no tags",
			behavior: &models.Behavior{
				Name:    "learned/prefer-short-names",
				Kind:    models.BehaviorKindPreference,
				Content: models.BehaviorContent{},
			},
			want: "`learned/prefer-short-names` [preference]",
		},
		{
			name: "constraint with single tag",
			behavior: &models.Behavior{
				Name: "learned/no-secrets-in-code",
				Kind: models.BehaviorKindConstraint,
				Content: models.BehaviorContent{
					Tags: []string{"security"},
				},
			},
			want: "`learned/no-secrets-in-code` [constraint] #security",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatNameOnly(tt.behavior)
			if got != tt.want {
				t.Errorf("formatNameOnly() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentForTier(t *testing.T) {
	b := &models.Behavior{
		Name: "test-behavior",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "Full canonical content here",
			Summary:   "Short summary",
			Tags:      []string{"go", "test"},
		},
	}

	tests := []struct {
		name string
		tier models.InjectionTier
		want string
	}{
		{"full tier", models.TierFull, "Full canonical content here"},
		{"summary tier", models.TierSummary, "Short summary"},
		{"name-only tier", models.TierNameOnly, "`test-behavior` [directive] #go #test"},
		{"omitted tier", models.TierOmitted, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentForTier(b, tt.tier)
			if got != tt.want {
				t.Errorf("contentForTier(%s) = %q, want %q", tt.tier, got, tt.want)
			}
		})
	}
}

func TestContentForTier_SummaryFallback(t *testing.T) {
	b := &models.Behavior{
		Name: "test",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "This is a very long canonical content that exceeds sixty characters and should be truncated for summary fallback",
		},
	}

	got := contentForTier(b, models.TierSummary)
	if len(got) > 60 {
		t.Errorf("summary fallback should be <= 60 chars, got %d", len(got))
	}
	if got != "This is a very long canonical content that exceeds sixty ..." {
		t.Errorf("unexpected fallback truncation: %q", got)
	}
}
