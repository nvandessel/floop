package activation

import (
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestEvaluator_Evaluate(t *testing.T) {
	evaluator := NewEvaluator()

	tests := []struct {
		name       string
		ctx        models.ContextSnapshot
		behaviors  []models.Behavior
		wantActive int
	}{
		{
			name: "no behaviors",
			ctx: models.ContextSnapshot{
				FileLanguage: "go",
			},
			behaviors:  []models.Behavior{},
			wantActive: 0,
		},
		{
			name: "behavior with no conditions always matches",
			ctx: models.ContextSnapshot{
				FileLanguage: "go",
			},
			behaviors: []models.Behavior{
				{ID: "b1", Name: "always-active", When: nil},
			},
			wantActive: 1,
		},
		{
			name: "behavior matches language",
			ctx: models.ContextSnapshot{
				FileLanguage: "go",
			},
			behaviors: []models.Behavior{
				{ID: "b1", Name: "go-specific", When: map[string]interface{}{"language": "go"}},
			},
			wantActive: 1,
		},
		{
			name: "behavior does not match language",
			ctx: models.ContextSnapshot{
				FileLanguage: "python",
			},
			behaviors: []models.Behavior{
				{ID: "b1", Name: "go-specific", When: map[string]interface{}{"language": "go"}},
			},
			wantActive: 0,
		},
		{
			name: "multiple conditions all must match",
			ctx: models.ContextSnapshot{
				FileLanguage: "go",
				Task:         "refactor",
			},
			behaviors: []models.Behavior{
				{ID: "b1", Name: "go-refactor", When: map[string]interface{}{
					"language": "go",
					"task":     "refactor",
				}},
			},
			wantActive: 1,
		},
		{
			name: "contradiction excludes behavior",
			ctx: models.ContextSnapshot{
				FileLanguage: "go",
				Task:         "debug",
			},
			behaviors: []models.Behavior{
				{ID: "b1", Name: "go-refactor", When: map[string]interface{}{
					"language": "go",
					"task":     "refactor",
				}},
			},
			wantActive: 0,
		},
		{
			name: "multiple behaviors some match",
			ctx: models.ContextSnapshot{
				FileLanguage: "python",
			},
			behaviors: []models.Behavior{
				{ID: "b1", Name: "go-specific", When: map[string]interface{}{"language": "go"}},
				{ID: "b2", Name: "python-specific", When: map[string]interface{}{"language": "python"}},
				{ID: "b3", Name: "rust-specific", When: map[string]interface{}{"language": "rust"}},
			},
			wantActive: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := evaluator.Evaluate(tt.ctx, tt.behaviors)
			if len(results) != tt.wantActive {
				t.Errorf("Evaluate() returned %d active, want %d", len(results), tt.wantActive)
			}
		})
	}
}

func TestEvaluator_Specificity(t *testing.T) {
	evaluator := NewEvaluator()

	ctx := models.ContextSnapshot{
		FileLanguage: "go",
		Task:         "refactor",
		Branch:       "main",
	}

	behaviors := []models.Behavior{
		{ID: "b1", Name: "general", When: nil},                                      // 0 conditions
		{ID: "b2", Name: "go-only", When: map[string]interface{}{"language": "go"}}, // 1 condition
		{ID: "b3", Name: "go-refactor", When: map[string]interface{}{
			"language": "go",
			"task":     "refactor",
		}}, // 2 conditions
	}

	results := evaluator.Evaluate(ctx, behaviors)

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Should be sorted by specificity (most specific first)
	if results[0].Behavior.ID != "b3" {
		t.Errorf("First result should be b3 (most specific), got %s", results[0].Behavior.ID)
	}
	if results[1].Behavior.ID != "b2" {
		t.Errorf("Second result should be b2, got %s", results[1].Behavior.ID)
	}
	if results[2].Behavior.ID != "b1" {
		t.Errorf("Third result should be b1 (least specific), got %s", results[2].Behavior.ID)
	}
}

func TestEvaluator_WhyActive(t *testing.T) {
	evaluator := NewEvaluator()

	behavior := models.Behavior{
		ID:   "b1",
		Name: "go-refactor",
		When: map[string]interface{}{
			"language": "go",
			"task":     "refactor",
		},
	}

	tests := []struct {
		name       string
		ctx        models.ContextSnapshot
		wantActive bool
	}{
		{
			name: "all conditions match",
			ctx: models.ContextSnapshot{
				FileLanguage: "go",
				Task:         "refactor",
			},
			wantActive: true,
		},
		{
			name: "language mismatch",
			ctx: models.ContextSnapshot{
				FileLanguage: "python",
				Task:         "refactor",
			},
			wantActive: false,
		},
		{
			name: "task mismatch",
			ctx: models.ContextSnapshot{
				FileLanguage: "go",
				Task:         "debug",
			},
			wantActive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explanation := evaluator.WhyActive(tt.ctx, behavior)

			if explanation.IsActive != tt.wantActive {
				t.Errorf("WhyActive() IsActive = %v, want %v", explanation.IsActive, tt.wantActive)
			}

			if len(explanation.Conditions) != 2 {
				t.Errorf("Expected 2 conditions in explanation, got %d", len(explanation.Conditions))
			}
		})
	}
}

func TestEvaluator_PartialMatching(t *testing.T) {
	evaluator := NewEvaluator()

	tests := []struct {
		name       string
		ctx        models.ContextSnapshot
		behavior   models.Behavior
		wantActive bool
		wantScore  float64
	}{
		{
			name: "full match - all confirmed",
			ctx:  models.ContextSnapshot{FileLanguage: "go", Task: "testing"},
			behavior: models.Behavior{ID: "b1", When: map[string]interface{}{
				"language": "go", "task": "testing",
			}},
			wantActive: true,
			wantScore:  1.0,
		},
		{
			name: "partial match - task absent",
			ctx:  models.ContextSnapshot{FileLanguage: "go"},
			behavior: models.Behavior{ID: "b2", When: map[string]interface{}{
				"language": "go", "task": "development",
			}},
			wantActive: true,
			wantScore:  0.5,
		},
		{
			name: "contradiction excludes behavior",
			ctx:  models.ContextSnapshot{FileLanguage: "go", Task: "debug"},
			behavior: models.Behavior{ID: "b3", When: map[string]interface{}{
				"language": "go", "task": "refactor",
			}},
			wantActive: false,
			wantScore:  0.0,
		},
		{
			name: "all conditions absent",
			ctx:  models.ContextSnapshot{},
			behavior: models.Behavior{ID: "b4", When: map[string]interface{}{
				"task": "development",
			}},
			wantActive: true,
			wantScore:  0.0,
		},
		{
			name:       "no when conditions - always matches",
			ctx:        models.ContextSnapshot{FileLanguage: "go"},
			behavior:   models.Behavior{ID: "b5", When: nil},
			wantActive: true,
			wantScore:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := evaluator.Evaluate(tt.ctx, []models.Behavior{tt.behavior})
			if tt.wantActive {
				if len(results) != 1 {
					t.Fatalf("expected 1 result, got %d", len(results))
				}
				if results[0].MatchScore != tt.wantScore {
					t.Errorf("MatchScore = %f, want %f", results[0].MatchScore, tt.wantScore)
				}
			} else {
				if len(results) != 0 {
					t.Errorf("expected 0 results (excluded), got %d", len(results))
				}
			}
		})
	}
}

func TestEvaluator_NoConditions(t *testing.T) {
	evaluator := NewEvaluator()

	behavior := models.Behavior{
		ID:   "b1",
		Name: "always-active",
		When: nil,
	}

	ctx := models.ContextSnapshot{
		Timestamp: time.Now(),
	}

	explanation := evaluator.WhyActive(ctx, behavior)

	if !explanation.IsActive {
		t.Error("Behavior with no conditions should always be active")
	}

	if explanation.Reason != "No activation conditions - always active" {
		t.Errorf("Unexpected reason: %s", explanation.Reason)
	}
}
