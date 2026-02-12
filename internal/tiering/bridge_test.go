package tiering

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/spreading"
)

func TestScoredBehaviorsToResults(t *testing.T) {
	b1 := &models.Behavior{ID: "b1", Name: "test-1", Kind: models.BehaviorKindConstraint}
	b2 := &models.Behavior{ID: "b2", Name: "test-2", Kind: models.BehaviorKindDirective}

	scored := []ranking.ScoredBehavior{
		{Behavior: b1, Score: 0.9},
		{Behavior: b2, Score: 0.4},
	}

	results, behaviorMap := ScoredBehaviorsToResults(scored)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(behaviorMap) != 2 {
		t.Fatalf("expected 2 behaviors in map, got %d", len(behaviorMap))
	}

	// Verify result fields
	if results[0].BehaviorID != "b1" {
		t.Errorf("expected first result ID 'b1', got '%s'", results[0].BehaviorID)
	}
	if results[0].Activation != 0.9 {
		t.Errorf("expected first result activation 0.9, got %f", results[0].Activation)
	}
	if results[1].BehaviorID != "b2" {
		t.Errorf("expected second result ID 'b2', got '%s'", results[1].BehaviorID)
	}

	// Verify behavior map
	if behaviorMap["b1"] != b1 {
		t.Error("behavior map should contain b1")
	}
	if behaviorMap["b2"] != b2 {
		t.Error("behavior map should contain b2")
	}
}

func TestScoredBehaviorsToResults_Empty(t *testing.T) {
	results, behaviorMap := ScoredBehaviorsToResults(nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
	if len(behaviorMap) != 0 {
		t.Errorf("expected 0 behaviors, got %d", len(behaviorMap))
	}
}

func TestBehaviorsToResults(t *testing.T) {
	behaviors := []models.Behavior{
		{
			ID:         "b1",
			Name:       "test-1",
			Kind:       models.BehaviorKindConstraint,
			Confidence: 0.8,
			Priority:   5,
		},
		{
			ID:         "b2",
			Name:       "test-2",
			Kind:       models.BehaviorKindPreference,
			Confidence: 0.6,
			Priority:   3,
		},
	}

	results, behaviorMap := BehaviorsToResults(behaviors)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Results should be scored and converted
	if results[0].BehaviorID != "b1" {
		t.Errorf("expected first result ID 'b1', got '%s'", results[0].BehaviorID)
	}
	// Activation should be derived from scoring
	if results[0].Activation <= 0 {
		t.Errorf("expected positive activation for b1, got %f", results[0].Activation)
	}

	if behaviorMap["b1"] == nil || behaviorMap["b2"] == nil {
		t.Error("behavior map should contain both behaviors")
	}
}

func TestBehaviorsToResults_ProducesValidActivationTierInput(t *testing.T) {
	behaviors := []models.Behavior{
		{
			ID:   "b1",
			Name: "test-constraint",
			Kind: models.BehaviorKindConstraint,
			Content: models.BehaviorContent{
				Canonical: "Always validate input parameters before processing",
			},
			Confidence: 0.8,
		},
	}

	results, behaviorMap := BehaviorsToResults(behaviors)

	// Should work with ActivationTierMapper
	mapper := NewActivationTierMapper(DefaultActivationTierConfig())
	plan := mapper.MapResults(results, behaviorMap, 2000)

	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	// The behavior should be included somewhere in the plan
	total := len(plan.FullBehaviors) + len(plan.SummarizedBehaviors) +
		len(plan.NameOnlyBehaviors) + len(plan.OmittedBehaviors)
	if total != 1 {
		t.Errorf("expected 1 behavior in plan, got %d", total)
	}
}

func TestScoredBehaviorsToResults_SkipsNilBehavior(t *testing.T) {
	scored := []ranking.ScoredBehavior{
		{Behavior: nil, Score: 0.9},
		{Behavior: &models.Behavior{ID: "b1"}, Score: 0.5},
	}

	results, behaviorMap := ScoredBehaviorsToResults(scored)

	if len(results) != 1 {
		t.Fatalf("expected 1 result (nil skipped), got %d", len(results))
	}
	if results[0].BehaviorID != "b1" {
		t.Errorf("expected result ID 'b1', got '%s'", results[0].BehaviorID)
	}
	if len(behaviorMap) != 1 {
		t.Errorf("expected 1 behavior in map, got %d", len(behaviorMap))
	}
}

// Verify the type signatures compile (spreading.Result is used correctly)
var _ []spreading.Result = func() []spreading.Result {
	r, _ := ScoredBehaviorsToResults(nil)
	return r
}()
