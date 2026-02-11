package tagging

import (
	"math"
	"testing"
)

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want float64
	}{
		{"both empty", nil, nil, 0.0},
		{"a empty", nil, []string{"go"}, 0.0},
		{"b empty", []string{"go"}, nil, 0.0},
		{"identical", []string{"go", "testing"}, []string{"go", "testing"}, 1.0},
		{"disjoint", []string{"go"}, []string{"python"}, 0.0},
		{"partial overlap", []string{"go", "testing", "git"}, []string{"go", "testing", "linting"}, 0.5},
		{"subset", []string{"go"}, []string{"go", "testing"}, 0.5},
		{"single shared", []string{"go", "git"}, []string{"go", "python"}, 1.0 / 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JaccardSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("JaccardSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIntersectTags(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{"both empty", nil, nil, nil},
		{"no overlap", []string{"go"}, []string{"python"}, nil},
		{"full overlap", []string{"go", "testing"}, []string{"go", "testing"}, []string{"go", "testing"}},
		{"partial overlap preserves a order", []string{"testing", "go", "git"}, []string{"go", "testing"}, []string{"testing", "go"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IntersectTags(tt.a, tt.b)
			if len(got) == 0 && len(tt.want) == 0 {
				return // both nil/empty is fine
			}
			if len(got) != len(tt.want) {
				t.Fatalf("IntersectTags() len = %d, want %d: got %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("IntersectTags()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
