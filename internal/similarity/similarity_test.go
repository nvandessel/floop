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
			want: 1.0,
		},
		{
			name: "both nil",
			a:    nil,
			b:    nil,
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
