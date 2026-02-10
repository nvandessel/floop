package llm

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
		want float64
	}{
		{
			name: "identical vectors",
			a:    []float32{1, 2, 3},
			b:    []float32{1, 2, 3},
			want: 1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{0, 1, 0},
			want: 0.0,
		},
		{
			name: "anti-parallel vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{-1, 0, 0},
			want: -1.0,
		},
		{
			name: "scaled identical",
			a:    []float32{1, 2, 3},
			b:    []float32{2, 4, 6},
			want: 1.0,
		},
		{
			name: "partial similarity",
			a:    []float32{1, 1, 0},
			b:    []float32{1, 0, 0},
			want: 1.0 / math.Sqrt(2),
		},
		{
			name: "zero vector a",
			a:    []float32{0, 0, 0},
			b:    []float32{1, 2, 3},
			want: 0.0,
		},
		{
			name: "zero vector b",
			a:    []float32{1, 2, 3},
			b:    []float32{0, 0, 0},
			want: 0.0,
		},
		{
			name: "both zero vectors",
			a:    []float32{0, 0, 0},
			b:    []float32{0, 0, 0},
			want: 0.0,
		},
		{
			name: "different lengths",
			a:    []float32{1, 2},
			b:    []float32{1, 2, 3},
			want: 0.0,
		},
		{
			name: "empty vectors",
			a:    []float32{},
			b:    []float32{},
			want: 0.0,
		},
		{
			name: "nil vectors",
			a:    nil,
			b:    nil,
			want: 0.0,
		},
		{
			name: "single dimension identical",
			a:    []float32{5},
			b:    []float32{5},
			want: 1.0,
		},
		{
			name: "single dimension opposite",
			a:    []float32{5},
			b:    []float32{-3},
			want: -1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("CosineSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}
