package learning

import (
	"context"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
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
			name: "environment only is global",
			when: map[string]interface{}{
				"environment": "production",
			},
			want: constants.ScopeGlobal,
		},
		{
			name: "environment with other non-file keys is global",
			when: map[string]interface{}{
				"language":    "python",
				"task":        "deployment",
				"environment": "staging",
			},
			want: constants.ScopeGlobal,
		},
		{
			name: "file_path with environment is local",
			when: map[string]interface{}{
				"file_path":   "src/**",
				"environment": "dev",
			},
			want: constants.ScopeLocal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			behavior := &models.Behavior{
				When: tt.when,
			}
			got := ClassifyScope(behavior)
			if got != tt.want {
				t.Errorf("ClassifyScope() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIntegration_ProcessCorrection_ScopeLocal tests that corrections with file
// context produce a local scope in the learning result.
func TestIntegration_ProcessCorrection_ScopeLocal(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()
	cfg := &LearningLoopConfig{AutoAcceptThreshold: 0.5}
	loop := NewLearningLoop(s, cfg)

	// Correction with file path → extractor will set file_path in When → local scope
	correction := models.Correction{
		ID:              "scope-test-local",
		Timestamp:       time.Now(),
		AgentAction:     "used fmt.Println in handler",
		CorrectedAction: "use structured logging in handler",
		Context: models.ContextSnapshot{
			Timestamp:    time.Now(),
			FileLanguage: "go",
			FilePath:     "internal/mcp/handlers.go",
		},
	}

	result, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}

	if result.Scope != constants.ScopeLocal {
		t.Errorf("expected scope %q, got %q", constants.ScopeLocal, result.Scope)
	}
}

// TestIntegration_ProcessCorrection_ScopeGlobal tests that corrections without
// file context produce a global scope in the learning result.
func TestIntegration_ProcessCorrection_ScopeGlobal(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()
	cfg := &LearningLoopConfig{AutoAcceptThreshold: 0.5}
	loop := NewLearningLoop(s, cfg)

	// Correction with language only, no file path → global scope
	correction := models.Correction{
		ID:              "scope-test-global",
		Timestamp:       time.Now(),
		AgentAction:     "used fmt.Println",
		CorrectedAction: "use log.Printf for logging",
		Context: models.ContextSnapshot{
			Timestamp:    time.Now(),
			FileLanguage: "go",
		},
	}

	result, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		t.Fatalf("ProcessCorrection failed: %v", err)
	}

	if result.Scope != constants.ScopeGlobal {
		t.Errorf("expected scope %q, got %q", constants.ScopeGlobal, result.Scope)
	}
}
