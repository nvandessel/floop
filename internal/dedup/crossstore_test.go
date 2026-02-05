package dedup

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestDeduplicationResult_Instantiation(t *testing.T) {
	result := DeduplicationResult{
		LocalBehavior: &models.Behavior{ID: "local-1"},
		Action:        "merge",
		GlobalMatch:   &models.Behavior{ID: "global-1"},
		Similarity:    0.95,
	}

	if result.LocalBehavior.ID != "local-1" {
		t.Errorf("LocalBehavior.ID = %q, want local-1", result.LocalBehavior.ID)
	}
	if result.Action != "merge" {
		t.Errorf("Action = %q, want merge", result.Action)
	}
	if result.Similarity != 0.95 {
		t.Errorf("Similarity = %v, want 0.95", result.Similarity)
	}
}

func TestNewCrossStoreDeduplicator(t *testing.T) {
	merger := NewBehaviorMerger(MergerConfig{})
	dedup := NewCrossStoreDeduplicator(nil, nil, merger)

	if dedup.merger != merger {
		t.Error("expected merger to be set")
	}
	if dedup.config.SimilarityThreshold != DefaultConfig().SimilarityThreshold {
		t.Error("expected default config")
	}
}

func TestNewCrossStoreDeduplicatorWithConfig(t *testing.T) {
	merger := NewBehaviorMerger(MergerConfig{})
	config := DeduplicatorConfig{
		SimilarityThreshold: 0.8,
		AutoMerge:           true,
	}
	dedup := NewCrossStoreDeduplicatorWithConfig(nil, nil, merger, config)

	if dedup.config.SimilarityThreshold != 0.8 {
		t.Errorf("SimilarityThreshold = %v, want 0.8", dedup.config.SimilarityThreshold)
	}
	if !dedup.config.AutoMerge {
		t.Error("expected AutoMerge to be true")
	}
}

func TestNodeToBehavior(t *testing.T) {
	t.Run("basic conversion", func(t *testing.T) {
		node := store.Node{
			ID: "b1",
			Content: map[string]interface{}{
				"kind": "directive",
				"name": "Test Behavior",
				"when": map[string]interface{}{
					"language": "python",
				},
				"content": map[string]interface{}{
					"canonical": "use pathlib",
					"expanded":  "Use pathlib.Path for file operations",
					"summary":   "pathlib usage",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 0.85,
				"priority":   5,
			},
		}

		b := nodeToBehavior(node)

		if b.ID != "b1" {
			t.Errorf("ID = %q, want b1", b.ID)
		}
		if b.Kind != models.BehaviorKindDirective {
			t.Errorf("Kind = %q, want directive", b.Kind)
		}
		if b.Name != "Test Behavior" {
			t.Errorf("Name = %q, want Test Behavior", b.Name)
		}
		if b.Content.Canonical != "use pathlib" {
			t.Errorf("Content.Canonical = %q, want use pathlib", b.Content.Canonical)
		}
		if b.Content.Expanded != "Use pathlib.Path for file operations" {
			t.Errorf("Content.Expanded = %q", b.Content.Expanded)
		}
		if b.Content.Summary != "pathlib usage" {
			t.Errorf("Content.Summary = %q", b.Content.Summary)
		}
		if b.Confidence != 0.85 {
			t.Errorf("Confidence = %v, want 0.85", b.Confidence)
		}
		if b.When["language"] != "python" {
			t.Errorf("When[language] = %v, want python", b.When["language"])
		}
	})

	t.Run("empty node", func(t *testing.T) {
		node := store.Node{
			ID:       "b1",
			Content:  map[string]interface{}{},
			Metadata: map[string]interface{}{},
		}

		b := nodeToBehavior(node)

		if b.ID != "b1" {
			t.Errorf("ID = %q, want b1", b.ID)
		}
		// Other fields should be zero values
		if b.Kind != "" {
			t.Errorf("Kind should be empty, got %q", b.Kind)
		}
		if b.Confidence != 0 {
			t.Errorf("Confidence should be 0, got %v", b.Confidence)
		}
	})

	t.Run("with stats", func(t *testing.T) {
		node := store.Node{
			ID:      "b1",
			Content: map[string]interface{}{},
			Metadata: map[string]interface{}{
				"stats": map[string]interface{}{
					"times_activated":  10,
					"times_followed":   8,
					"times_confirmed":  5,
					"times_overridden": 2,
				},
			},
		}

		b := nodeToBehavior(node)

		if b.Stats.TimesActivated != 10 {
			t.Errorf("Stats.TimesActivated = %d, want 10", b.Stats.TimesActivated)
		}
		if b.Stats.TimesFollowed != 8 {
			t.Errorf("Stats.TimesFollowed = %d, want 8", b.Stats.TimesFollowed)
		}
		if b.Stats.TimesConfirmed != 5 {
			t.Errorf("Stats.TimesConfirmed = %d, want 5", b.Stats.TimesConfirmed)
		}
		if b.Stats.TimesOverridden != 2 {
			t.Errorf("Stats.TimesOverridden = %d, want 2", b.Stats.TimesOverridden)
		}
	})
}

func TestCrossStoreDeduplicator_ComputeWhenOverlap(t *testing.T) {
	dedup := &CrossStoreDeduplicator{}

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
			name: "a empty",
			a:    map[string]interface{}{},
			b:    map[string]interface{}{"key": "value"},
			want: 0.0,
		},
		{
			name: "b empty",
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
			got := dedup.computeWhenOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("computeWhenOverlap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCrossStoreDeduplicator_ComputeContentSimilarity(t *testing.T) {
	dedup := &CrossStoreDeduplicator{}

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
			name: "a empty",
			a:    "",
			b:    "hello",
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
			want: 0.5, // 2 common words (hello, foo) out of 4 unique
		},
		{
			name: "case insensitive",
			a:    "HELLO World",
			b:    "hello WORLD",
			want: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedup.computeContentSimilarity(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("computeContentSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCrossStoreDeduplicator_ComputeSimilarity(t *testing.T) {
	dedup := &CrossStoreDeduplicator{}

	t.Run("identical behaviors", func(t *testing.T) {
		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}

		sim := dedup.computeSimilarity(a, b)
		if sim != 1.0 {
			t.Errorf("similarity = %v, want 1.0 for identical behaviors", sim)
		}
	})

	t.Run("different behaviors", func(t *testing.T) {
		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "run tests"},
			When:    map[string]interface{}{"task": "commit"},
		}

		sim := dedup.computeSimilarity(a, b)
		if sim > 0.5 {
			t.Errorf("similarity = %v, expected < 0.5 for different behaviors", sim)
		}
	})
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
		{
			name:  "only punctuation",
			input: ".,!?",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("tokenize() returned %d tokens, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize()[%d] = %q, want %q", i, got[i], tt.want[i])
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
			name: "equal integers",
			a:    42,
			b:    42,
			want: true,
		},
		{
			name: "different integers",
			a:    42,
			b:    43,
			want: false,
		},
		{
			name: "interface slices with overlap",
			a:    []interface{}{"a", "b"},
			b:    []interface{}{"b", "c"},
			want: true, // has common element "b"
		},
		{
			name: "interface slices without overlap",
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
			name: "string slices without overlap",
			a:    []string{"a", "b"},
			b:    []string{"c", "d"},
			want: false,
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

func TestNewCrossStoreDeduplicatorWithLLM(t *testing.T) {
	localStore := store.NewInMemoryGraphStore()
	globalStore := store.NewInMemoryGraphStore()
	merger := NewBehaviorMerger(MergerConfig{})
	config := DeduplicatorConfig{
		SimilarityThreshold: 0.9,
		UseLLM:              true,
	}
	mockClient := llm.NewMockClient()

	dedup := NewCrossStoreDeduplicatorWithLLM(localStore, globalStore, merger, config, mockClient)

	if dedup.localStore != localStore {
		t.Error("expected local store to be set")
	}
	if dedup.globalStore != globalStore {
		t.Error("expected global store to be set")
	}
	if dedup.llmClient != mockClient {
		t.Error("expected LLM client to be set")
	}
	if !dedup.config.UseLLM {
		t.Error("expected UseLLM to be true")
	}
}

func TestCrossStoreDeduplicator_ComputeSimilarity_WithLLM(t *testing.T) {
	t.Run("uses LLM when available and configured", func(t *testing.T) {
		mockClient := llm.NewMockClient().
			WithComparisonResult(&llm.ComparisonResult{
				SemanticSimilarity: 0.92,
				IntentMatch:        true,
			})

		dedup := &CrossStoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: mockClient,
		}

		a := &models.Behavior{Content: models.BehaviorContent{Canonical: "use pathlib"}}
		b := &models.Behavior{Content: models.BehaviorContent{Canonical: "prefer pathlib"}}

		sim := dedup.computeSimilarity(a, b)

		if sim != 0.92 {
			t.Errorf("expected LLM similarity 0.92, got %f", sim)
		}

		if mockClient.CompareCallCount() != 1 {
			t.Errorf("expected 1 LLM call, got %d", mockClient.CompareCallCount())
		}
	})

	t.Run("falls back to Jaccard when LLM disabled", func(t *testing.T) {
		mockClient := llm.NewMockClient()

		dedup := &CrossStoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: false},
			llmClient: mockClient,
		}

		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}

		sim := dedup.computeSimilarity(a, b)

		if sim != 1.0 {
			t.Errorf("expected Jaccard similarity 1.0, got %f", sim)
		}

		if mockClient.CompareCallCount() != 0 {
			t.Errorf("expected 0 LLM calls, got %d", mockClient.CompareCallCount())
		}
	})

	t.Run("handles nil LLM client gracefully", func(t *testing.T) {
		dedup := &CrossStoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: nil,
		}

		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "hello"},
			When:    map[string]interface{}{},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "hello"},
			When:    map[string]interface{}{},
		}

		// Should not panic
		sim := dedup.computeSimilarity(a, b)

		if sim != 1.0 {
			t.Errorf("expected Jaccard similarity 1.0, got %f", sim)
		}
	})
}
