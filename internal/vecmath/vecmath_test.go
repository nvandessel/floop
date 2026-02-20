package vecmath

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
			a:    []float32{1, 0},
			b:    []float32{0, 1},
			want: 0.0,
		},
		{
			name: "opposite vectors",
			a:    []float32{1, 2, 3},
			b:    []float32{-1, -2, -3},
			want: -1.0,
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
			name: "zero magnitude vector",
			a:    []float32{0, 0, 0},
			b:    []float32{1, 2, 3},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("CosineSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
		want float64 // expected L2 norm after normalization
	}{
		{
			name: "standard vector",
			vec:  []float32{3, 4},
			want: 1.0,
		},
		{
			name: "already normalized",
			vec:  []float32{1, 0, 0},
			want: 1.0,
		},
		{
			name: "zero vector unchanged",
			vec:  []float32{0, 0, 0},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Normalize(tt.vec)
			var norm float64
			for _, v := range tt.vec {
				norm += float64(v) * float64(v)
			}
			norm = math.Sqrt(norm)
			if math.Abs(norm-tt.want) > 1e-6 {
				t.Errorf("Normalize() resulting norm = %v, want %v", norm, tt.want)
			}
		})
	}
}
