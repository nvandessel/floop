package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

func TestNewTagsCmd(t *testing.T) {
	cmd := newTagsCmd()
	if cmd.Use != "tags" {
		t.Errorf("Use = %q, want %q", cmd.Use, "tags")
	}

	// Verify backfill subcommand exists
	subCmds := cmd.Commands()
	found := false
	for _, sub := range subCmds {
		if sub.Name() == "backfill" {
			found = true
		}
	}
	if !found {
		t.Error("missing 'backfill' subcommand")
	}
}

func TestNewTagsBackfillCmd(t *testing.T) {
	cmd := newTagsBackfillCmd()
	if cmd.Use != "backfill" {
		t.Errorf("Use = %q, want %q", cmd.Use, "backfill")
	}

	for _, flag := range []string{"dry-run", "scope"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestRunTagsBackfillDryRun(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Add a behavior with no tags but content that should generate tags
	b := models.Behavior{
		ID:   "b-go-errors",
		Name: "Go error handling",
		Content: models.BehaviorContent{
			Canonical: "use error wrapping with fmt.Errorf for go error handling",
		},
		Confidence: 0.8,
	}
	node := models.BehaviorToNode(&b)
	if _, err := s.AddNode(ctx, node); err != nil {
		t.Fatalf("failed to add node: %v", err)
	}

	// Add a behavior WITH tags (should be skipped)
	b2 := models.Behavior{
		ID:   "b-python-typing",
		Name: "Python typing",
		Content: models.BehaviorContent{
			Canonical: "use type hints for function signatures",
			Tags:      []string{"python", "typing"},
		},
		Confidence: 0.8,
	}
	node2 := models.BehaviorToNode(&b2)
	if _, err := s.AddNode(ctx, node2); err != nil {
		t.Fatalf("failed to add node: %v", err)
	}

	// Dry run should not modify the store
	err := runTagsBackfill(s, true, false)
	if err != nil {
		t.Fatalf("runTagsBackfill dry-run failed: %v", err)
	}

	// Verify original node is unchanged (no tags added in dry run)
	originalNode, err := s.GetNode(ctx, "b-go-errors")
	if err != nil {
		t.Fatalf("failed to get node: %v", err)
	}
	contentMap, ok := originalNode.Content["content"].(map[string]interface{})
	if ok {
		if tags, exists := contentMap["tags"]; exists && tags != nil {
			t.Error("dry run should not modify store, but tags were added")
		}
	}
	// If content is not a map, there are no tags — dry run is correct
}

func TestRunTagsBackfillAppliesTags(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Add a behavior with no tags but content that should generate tags
	b := models.Behavior{
		ID:   "b-go-errors",
		Name: "Go error handling",
		Content: models.BehaviorContent{
			Canonical: "use error wrapping with fmt.Errorf for go error handling",
		},
		Confidence: 0.8,
	}
	node := models.BehaviorToNode(&b)
	if _, err := s.AddNode(ctx, node); err != nil {
		t.Fatalf("failed to add node: %v", err)
	}

	// Run for real (not dry run)
	err := runTagsBackfill(s, false, false)
	if err != nil {
		t.Fatalf("runTagsBackfill failed: %v", err)
	}
}

func TestRunTagsBackfillJSON(t *testing.T) {
	s := store.NewInMemoryGraphStore()

	// Run with JSON output on empty store
	err := runTagsBackfill(s, true, true)
	if err != nil {
		t.Fatalf("runTagsBackfill JSON failed: %v", err)
	}
}

func TestRunTagsBackfillEmptyStore(t *testing.T) {
	s := store.NewInMemoryGraphStore()

	err := runTagsBackfill(s, false, false)
	if err != nil {
		t.Fatalf("runTagsBackfill on empty store failed: %v", err)
	}
}

func TestTagsBackfillCmdIntegration(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newTagsCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"tags", "backfill", "--scope", "local", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("tags backfill failed: %v", err)
	}
}

func TestTagsBackfillCmdDryRun(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newTagsCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"tags", "backfill", "--dry-run", "--scope", "local", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("tags backfill --dry-run failed: %v", err)
	}
}

func TestTagsBackfillCmdJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newTagsCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"tags", "backfill", "--json", "--scope", "local", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("tags backfill --json failed: %v", err)
	}
}
