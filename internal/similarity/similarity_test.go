package similarity

import (
	"testing"
)

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
			want: -1.0,
		},
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: -1.0,
		},
		{
			name: "one empty",
			a:    map[string]interface{}{"key": "value"},
			b:    map[string]interface{}{},
			want: -1.0,
		},
		{
			name: "identical",
			a:    map[string]interface{}{"key": "value"},
			b:    map[string]interface{}{"key": "value"},
			want: 1.0,
		},
		{
			name: "no shared keys (orthogonal axes)",
			a:    map[string]interface{}{"file_path": "store/*"},
			b:    map[string]interface{}{"task": "refactor"},
			want: -1.0, // Different dimensions = missing signal
		},
		{
			name: "shared key different value",
			a:    map[string]interface{}{"language": "go"},
			b:    map[string]interface{}{"language": "python"},
			want: 0.0, // Same dimension, genuinely different
		},
		{
			name: "partial overlap",
			a:    map[string]interface{}{"key1": "value1", "key2": "value2"},
			b:    map[string]interface{}{"key1": "value1", "key3": "value3"},
			want: 0.5, // 2 matches out of 4 total
		},
		{
			name: "mixed keys shared and orthogonal",
			a:    map[string]interface{}{"language": "go", "file_path": "store/*"},
			b:    map[string]interface{}{"language": "go", "task": "refactor"},
			want: 0.5, // shared key matches, orthogonal keys dilute total
		},
		{
			name: "multiple matching keys",
			a:    map[string]interface{}{"language": "go", "task": "refactor"},
			b:    map[string]interface{}{"language": "go", "task": "refactor"},
			want: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeWhenOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("ComputeWhenOverlap() = %v, want %v", got, tt.want)
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
		{
			name: "case insensitive",
			a:    "Hello World",
			b:    "hello world",
			want: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeContentSimilarity(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("ComputeContentSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWeightedScore(t *testing.T) {
	tests := []struct {
		name              string
		whenOverlap       float64
		contentSimilarity float64
		want              float64
	}{
		{
			name:              "both zero",
			whenOverlap:       0.0,
			contentSimilarity: 0.0,
			want:              0.0,
		},
		{
			name:              "both one",
			whenOverlap:       1.0,
			contentSimilarity: 1.0,
			want:              1.0,
		},
		{
			name:              "only when overlap",
			whenOverlap:       1.0,
			contentSimilarity: 0.0,
			want:              0.4,
		},
		{
			name:              "only content similarity",
			whenOverlap:       0.0,
			contentSimilarity: 1.0,
			want:              0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WeightedScore(tt.whenOverlap, tt.contentSimilarity)
			if got != tt.want {
				t.Errorf("WeightedScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWeightedScoreWithTags(t *testing.T) {
	tests := []struct {
		name              string
		whenOverlap       float64
		contentSimilarity float64
		tagSimilarity     float64
		want              float64
	}{
		{
			name:              "all signals present",
			whenOverlap:       1.0,
			contentSimilarity: 1.0,
			tagSimilarity:     1.0,
			want:              1.0,
		},
		{
			name:              "all signals zero",
			whenOverlap:       0.0,
			contentSimilarity: 0.0,
			tagSimilarity:     0.0,
			want:              0.0,
		},
		{
			name:              "tags missing sentinel falls back to old weights",
			whenOverlap:       1.0,
			contentSimilarity: 1.0,
			tagSimilarity:     -1.0,
			want:              1.0,
		},
		{
			name:              "tags missing only content",
			whenOverlap:       0.0,
			contentSimilarity: 1.0,
			tagSimilarity:     -1.0,
			want:              0.6,
		},
		{
			name:              "when missing sentinel redistributes",
			whenOverlap:       -1.0,
			contentSimilarity: 1.0,
			tagSimilarity:     1.0,
			want:              1.0,
		},
		{
			name:              "when missing only content",
			whenOverlap:       -1.0,
			contentSimilarity: 1.0,
			tagSimilarity:     0.0,
			want:              0.75,
		},
		{
			name:              "when+tags missing = content only",
			whenOverlap:       -1.0,
			contentSimilarity: 0.8,
			tagSimilarity:     -1.0,
			want:              0.8,
		},
		{
			name:              "content missing",
			whenOverlap:       1.0,
			contentSimilarity: -1.0,
			tagSimilarity:     1.0,
			want:              1.0,
		},
		{
			name:              "all signals missing returns zero",
			whenOverlap:       -1.0,
			contentSimilarity: -1.0,
			tagSimilarity:     -1.0,
			want:              0.0,
		},
		{
			name:              "only tags present",
			whenOverlap:       -1.0,
			contentSimilarity: -1.0,
			tagSimilarity:     0.5,
			want:              0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WeightedScoreWithTags(tt.whenOverlap, tt.contentSimilarity, tt.tagSimilarity)
			if diff := got - tt.want; diff > 0.001 || diff < -0.001 {
				t.Errorf("WeightedScoreWithTags() = %v, want %v", got, tt.want)
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
		{
			name:  "only punctuation",
			input: "!@#$%",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("Tokenize() len = %v, want %v", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Tokenize()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestComputeTagSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want float64
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: -1.0,
		},
		{
			name: "both empty",
			a:    []string{},
			b:    []string{},
			want: -1.0,
		},
		{
			name: "a empty",
			a:    []string{},
			b:    []string{"go", "cli"},
			want: -1.0,
		},
		{
			name: "b empty",
			a:    []string{"go", "cli"},
			b:    []string{},
			want: -1.0,
		},
		{
			name: "a nil",
			a:    nil,
			b:    []string{"go", "cli"},
			want: -1.0,
		},
		{
			name: "b nil",
			a:    []string{"go", "cli"},
			b:    nil,
			want: -1.0,
		},
		{
			name: "identical tags",
			a:    []string{"go", "cli"},
			b:    []string{"go", "cli"},
			want: 1.0,
		},
		{
			name: "no overlap",
			a:    []string{"go", "cli"},
			b:    []string{"python", "web"},
			want: 0.0,
		},
		{
			name: "partial overlap",
			a:    []string{"go", "cli", "testing"},
			b:    []string{"go", "testing", "web"},
			want: 0.5, // 2 common out of 4 unique
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeTagSimilarity(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("ComputeTagSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCountSharedTags(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want int
	}{
		{"both nil", nil, nil, 0},
		{"both empty", []string{}, []string{}, 0},
		{"a empty", []string{}, []string{"go"}, 0},
		{"b empty", []string{"go"}, []string{}, 0},
		{"no overlap", []string{"go", "cli"}, []string{"python", "web"}, 0},
		{"one shared", []string{"go", "cli"}, []string{"go", "web"}, 1},
		{"two shared", []string{"go", "cli", "errors"}, []string{"go", "errors", "web"}, 2},
		{"identical", []string{"go", "cli"}, []string{"go", "cli"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountSharedTags(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("CountSharedTags() = %d, want %d", got, tt.want)
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
			name: "string slices without overlap",
			a:    []string{"a", "b"},
			b:    []string{"c", "d"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValuesEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("ValuesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
