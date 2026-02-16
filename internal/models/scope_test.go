package models

import (
	"testing"

	"github.com/nvandessel/feedback-loop/internal/constants"
)

func TestClassifyScope(t *testing.T) {
	tests := []struct {
		name string
		when map[string]interface{}
		want constants.Scope
	}{
		{
			name: "nil When defaults to global",
			when: nil,
			want: constants.ScopeGlobal,
		},
		{
			name: "empty When defaults to global",
			when: map[string]interface{}{},
			want: constants.ScopeGlobal,
		},
		{
			name: "language only is global",
			when: map[string]interface{}{
				"language": "go",
			},
			want: constants.ScopeGlobal,
		},
		{
			name: "task only is global",
			when: map[string]interface{}{
				"task": "testing",
			},
			want: constants.ScopeGlobal,
		},
		{
			name: "language and task is global",
			when: map[string]interface{}{
				"language": "python",
				"task":     "refactoring",
			},
			want: constants.ScopeGlobal,
		},
		{
			name: "file_path present is local",
			when: map[string]interface{}{
				"file_path": "internal/store/**",
			},
			want: constants.ScopeLocal,
		},
		{
			name: "file_path with language is local",
			when: map[string]interface{}{
				"language":  "go",
				"file_path": "cmd/floop/**",
			},
			want: constants.ScopeLocal,
		},
		{
			name: "environment present is local",
			when: map[string]interface{}{
				"environment": "production",
			},
			want: constants.ScopeLocal,
		},
		{
			name: "environment with other keys is local",
			when: map[string]interface{}{
				"language":    "python",
				"task":        "deployment",
				"environment": "staging",
			},
			want: constants.ScopeLocal,
		},
		{
			name: "both file_path and environment is local",
			when: map[string]interface{}{
				"file_path":   "src/**",
				"environment": "dev",
			},
			want: constants.ScopeLocal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			behavior := &Behavior{
				When: tt.when,
			}
			got := ClassifyScope(behavior)
			if got != tt.want {
				t.Errorf("ClassifyScope() = %q, want %q", got, tt.want)
			}
		})
	}
}
