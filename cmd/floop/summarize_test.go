package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/summarization"
)

func TestCountUpdated(t *testing.T) {
	tests := []struct {
		name    string
		results []summaryResult
		want    int
	}{
		{"empty", nil, 0},
		{"none updated", []summaryResult{{Updated: false}, {Updated: false}}, 0},
		{"one updated", []summaryResult{{Updated: true}, {Updated: false}}, 1},
		{"all updated", []summaryResult{{Updated: true}, {Updated: true}}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countUpdated(tt.results)
			if got != tt.want {
				t.Errorf("countUpdated() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNewSummarizeCmd(t *testing.T) {
	cmd := newSummarizeCmd()
	if cmd.Use != "summarize [behavior-id]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "summarize [behavior-id]")
	}
	for _, flag := range []string{"all", "missing"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestSummarizeBehaviorWithInMemoryStore(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Add a behavior
	b := &models.Behavior{
		ID:         "b-test-1",
		Name:       "test-behavior",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.9,
		Content: models.BehaviorContent{
			Canonical: "Always use structured logging with slog",
		},
	}
	node := models.BehaviorToNode(b)
	if _, err := s.AddNode(ctx, node); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())

	result, err := summarizeBehavior(ctx, s, summarizer, "b-test-1")
	if err != nil {
		t.Fatalf("summarizeBehavior failed: %v", err)
	}

	if result.BehaviorID != "b-test-1" {
		t.Errorf("BehaviorID = %q, want %q", result.BehaviorID, "b-test-1")
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if !result.Updated {
		t.Error("expected Updated=true for new summary")
	}
}

func TestSummarizeBehaviorNotFound(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())

	_, err := summarizeBehavior(ctx, s, summarizer, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent behavior")
	}
}

func TestSummarizeBehaviorIdempotent(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	b := &models.Behavior{
		ID:         "b-idem",
		Name:       "idempotent-test",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.9,
		Content: models.BehaviorContent{
			Canonical: "Use context.Context as the first parameter",
		},
	}
	node := models.BehaviorToNode(b)
	if _, err := s.AddNode(ctx, node); err != nil {
		t.Fatalf("AddNode failed: %v", err)
	}

	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())

	// First call should update
	result1, err := summarizeBehavior(ctx, s, summarizer, "b-idem")
	if err != nil {
		t.Fatalf("first summarize failed: %v", err)
	}
	if !result1.Updated {
		t.Error("expected first call to update")
	}

	// Second call should not update (same summary)
	result2, err := summarizeBehavior(ctx, s, summarizer, "b-idem")
	if err != nil {
		t.Fatalf("second summarize failed: %v", err)
	}
	if result2.Updated {
		t.Error("expected second call to not update")
	}
	if result1.Summary != result2.Summary {
		t.Errorf("summaries differ: %q vs %q", result1.Summary, result2.Summary)
	}
}

func TestSummarizeCmdIntegration(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newSummarizeCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"summarize", behaviorID, "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("summarize failed: %v", err)
	}
}

func TestSummarizeCmdAll(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newSummarizeCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"summarize", "--all", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("summarize --all failed: %v", err)
	}
}

func TestSummarizeCmdMissing(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newSummarizeCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"summarize", "--missing", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("summarize --missing failed: %v", err)
	}
}

func TestSummarizeCmdJSON(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newSummarizeCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"summarize", "--json", behaviorID, "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("summarize --json failed: %v", err)
	}
}

func TestSummarizeCmdNoArgs(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newSummarizeCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"summarize", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when no args and no --all/--missing")
	}
}
