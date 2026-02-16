package llm

import (
	"context"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestNewFallbackClient(t *testing.T) {
	client := NewFallbackClient()
	if client == nil {
		t.Error("NewFallbackClient() returned nil")
	}
}

func TestFallbackClient_Available(t *testing.T) {
	client := NewFallbackClient()
	if client.Available() {
		t.Error("FallbackClient.Available() should return false")
	}
}

func TestFallbackClient_CompareBehaviors(t *testing.T) {
	client := NewFallbackClient()
	ctx := context.Background()

	tests := []struct {
		name            string
		a               *models.Behavior
		b               *models.Behavior
		wantHighSim     bool
		wantIntentMatch bool
	}{
		{
			name: "identical behaviors",
			a: &models.Behavior{
				Content: models.BehaviorContent{Canonical: "use pathlib.Path instead of os.path"},
				When:    map[string]interface{}{"language": "python"},
			},
			b: &models.Behavior{
				Content: models.BehaviorContent{Canonical: "use pathlib.Path instead of os.path"},
				When:    map[string]interface{}{"language": "python"},
			},
			wantHighSim:     true,
			wantIntentMatch: true,
		},
		{
			name: "similar behaviors",
			a: &models.Behavior{
				Content: models.BehaviorContent{Canonical: "use pathlib for file paths"},
				When:    map[string]interface{}{"language": "python"},
			},
			b: &models.Behavior{
				Content: models.BehaviorContent{Canonical: "prefer pathlib over os.path for file paths"},
				When:    map[string]interface{}{"language": "python"},
			},
			wantHighSim:     true,
			wantIntentMatch: false, // Jaccard similarity may not be high enough for intent match
		},
		{
			name: "different behaviors",
			a: &models.Behavior{
				Content: models.BehaviorContent{Canonical: "use pathlib.Path"},
				When:    map[string]interface{}{"language": "python"},
			},
			b: &models.Behavior{
				Content: models.BehaviorContent{Canonical: "always run tests before committing"},
				When:    map[string]interface{}{"task": "commit"},
			},
			wantHighSim:     false,
			wantIntentMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.CompareBehaviors(ctx, tt.a, tt.b)
			if err != nil {
				t.Fatalf("CompareBehaviors() error = %v", err)
			}
			if result == nil {
				t.Fatal("CompareBehaviors() returned nil result")
			}

			if tt.wantHighSim && result.SemanticSimilarity < 0.5 {
				t.Errorf("expected high similarity, got %.2f", result.SemanticSimilarity)
			}
			if !tt.wantHighSim && result.SemanticSimilarity > 0.5 {
				t.Errorf("expected low similarity, got %.2f", result.SemanticSimilarity)
			}
			if result.IntentMatch != tt.wantIntentMatch {
				t.Errorf("IntentMatch = %v, want %v", result.IntentMatch, tt.wantIntentMatch)
			}
		})
	}
}

func TestFallbackClient_MergeBehaviors(t *testing.T) {
	client := NewFallbackClient()
	ctx := context.Background()

	t.Run("empty input", func(t *testing.T) {
		result, err := client.MergeBehaviors(ctx, []*models.Behavior{})
		if err != nil {
			t.Fatalf("MergeBehaviors() error = %v", err)
		}
		if result.Merged != nil {
			t.Error("expected nil merged for empty input")
		}
	})

	t.Run("single behavior", func(t *testing.T) {
		b := &models.Behavior{ID: "b1", Name: "test", Content: models.BehaviorContent{Canonical: "test"}}
		result, err := client.MergeBehaviors(ctx, []*models.Behavior{b})
		if err != nil {
			t.Fatalf("MergeBehaviors() error = %v", err)
		}
		if result.Merged != b {
			t.Error("expected same behavior returned for single input")
		}
	})

	t.Run("multiple behaviors", func(t *testing.T) {
		b1 := &models.Behavior{
			ID:         "b1",
			Name:       "first",
			Kind:       models.BehaviorKindDirective,
			Content:    models.BehaviorContent{Canonical: "first content"},
			Confidence: 0.8,
			Priority:   1,
		}
		b2 := &models.Behavior{
			ID:         "b2",
			Name:       "second",
			Kind:       models.BehaviorKindDirective,
			Content:    models.BehaviorContent{Canonical: "second content"},
			Confidence: 0.9,
			Priority:   2,
		}

		result, err := client.MergeBehaviors(ctx, []*models.Behavior{b1, b2})
		if err != nil {
			t.Fatalf("MergeBehaviors() error = %v", err)
		}
		if result.Merged == nil {
			t.Fatal("MergeBehaviors() returned nil merged")
		}
		if len(result.SourceIDs) != 2 {
			t.Errorf("expected 2 source IDs, got %d", len(result.SourceIDs))
		}
		if result.Merged.Confidence != 0.9 {
			t.Errorf("expected max confidence 0.9, got %.2f", result.Merged.Confidence)
		}
		if result.Merged.Priority != 2 {
			t.Errorf("expected max priority 2, got %d", result.Merged.Priority)
		}
	})
}
