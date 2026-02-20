package similarity

import "testing"

func TestIsMoreSpecific(t *testing.T) {
	tests := []struct {
		name string
		a    map[string]interface{}
		b    map[string]interface{}
		want bool
	}{
		{
			name: "superset returns true",
			a:    map[string]interface{}{"language": "go", "task": "testing"},
			b:    map[string]interface{}{"language": "go"},
			want: true,
		},
		{
			name: "equal maps returns false",
			a:    map[string]interface{}{"language": "go"},
			b:    map[string]interface{}{"language": "go"},
			want: false,
		},
		{
			name: "subset returns false",
			a:    map[string]interface{}{"language": "go"},
			b:    map[string]interface{}{"language": "go", "task": "testing"},
			want: false,
		},
		{
			name: "empty b returns false",
			a:    map[string]interface{}{"language": "go"},
			b:    map[string]interface{}{},
			want: false,
		},
		{
			name: "both empty returns false",
			a:    map[string]interface{}{},
			b:    map[string]interface{}{},
			want: false,
		},
		{
			name: "value mismatch returns false",
			a:    map[string]interface{}{"language": "python", "task": "testing"},
			b:    map[string]interface{}{"language": "go"},
			want: false,
		},
		{
			name: "a has extra key but missing b key returns false",
			a:    map[string]interface{}{"task": "testing", "file_path": "*.go"},
			b:    map[string]interface{}{"language": "go"},
			want: false,
		},
		{
			name: "nil b returns false",
			a:    map[string]interface{}{"language": "go"},
			b:    nil,
			want: false,
		},
		{
			name: "nil a returns false",
			a:    nil,
			b:    map[string]interface{}{"language": "go"},
			want: false,
		},
		{
			name: "both nil returns false",
			a:    nil,
			b:    nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMoreSpecific(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("IsMoreSpecific() = %v, want %v", got, tt.want)
			}
		})
	}
}
