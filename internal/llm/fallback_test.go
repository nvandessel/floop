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
		name           string
		a              *models.Behavior
		b              *models.Behavior
		wantHighSim    bool
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
			wantHighSim:    true,
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
			wantHighSim:    true,
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
			wantHighSim:    false,
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

func TestComputeWhenOverlap(t *testing.T) {
	tests := []struct {
		name string
		a    map[string]interface{}
		b    map[string]interface{}
		want float64
	}{
		{
			name: "both empty",
			a:    map[string]interface{}{},
			b:    map[string]interface{}{},
			want: 1.0,
		},
		{
			name: "one empty",
			a:    map[string]interface{}{"key": "value"},
			b:    map[string]interface{}{},
			want: 0.0,
		},
		{
			name: "identical",
			a:    map[string]interface{}{"key": "value"},
			b:    map[string]interface{}{"key": "value"},
			want: 1.0,
		},
		{
			name: "no overlap",
			a:    map[string]interface{}{"key1": "value1"},
			b:    map[string]interface{}{"key2": "value2"},
			want: 0.0,
		},
		{
			name: "partial overlap",
			a:    map[string]interface{}{"key1": "value1", "key2": "value2"},
			b:    map[string]interface{}{"key1": "value1", "key3": "value3"},
			want: 0.5, // 2 matches out of 4 total
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeWhenOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("computeWhenOverlap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeContentSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want float64
	}{
		{
			name: "identical",
			a:    "hello world",
			b:    "hello world",
			want: 1.0,
		},
		{
			name: "both empty",
			a:    "",
			b:    "",
			want: 1.0,
		},
		{
			name: "one empty",
			a:    "hello",
			b:    "",
			want: 0.0,
		},
		{
			name: "no overlap",
			a:    "hello world",
			b:    "foo bar",
			want: 0.0,
		},
		{
			name: "partial overlap",
			a:    "hello world foo",
			b:    "hello bar foo",
			want: 0.5, // 2 common words out of 4 unique
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeContentSimilarity(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("computeContentSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple words",
			input: "hello world",
			want:  []string{"hello", "world"},
		},
		{
			name:  "with punctuation",
			input: "hello, world!",
			want:  []string{"hello", "world"},
		},
		{
			name:  "with underscores",
			input: "hello_world foo_bar",
			want:  []string{"hello_world", "foo_bar"},
		},
		{
			name:  "with numbers",
			input: "test123 foo456",
			want:  []string{"test123", "foo456"},
		},
		{
			name:  "empty string",
			input: "",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("tokenize() len = %v, want %v", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestValuesEqual(t *testing.T) {
	tests := []struct {
		name string
		a    interface{}
		b    interface{}
		want bool
	}{
		{
			name: "equal strings",
			a:    "hello",
			b:    "hello",
			want: true,
		},
		{
			name: "different strings",
			a:    "hello",
			b:    "world",
			want: false,
		},
		{
			name: "equal slices with overlap",
			a:    []interface{}{"a", "b"},
			b:    []interface{}{"b", "c"},
			want: true, // has common element
		},
		{
			name: "slices without overlap",
			a:    []interface{}{"a", "b"},
			b:    []interface{}{"c", "d"},
			want: false,
		},
		{
			name: "string slices with overlap",
			a:    []string{"a", "b"},
			b:    []string{"b", "c"},
			want: true,
		},
		{
			name: "equal integers",
			a:    42,
			b:    42,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valuesEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("valuesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
