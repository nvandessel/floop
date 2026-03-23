package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/dedup"
	"github.com/nvandessel/floop/internal/models"
)

func TestNewDeduplicateCmd(t *testing.T) {
	cmd := newDeduplicateCmd()
	if cmd.Use != "deduplicate" {
		t.Errorf("Use = %q, want %q", cmd.Use, "deduplicate")
	}

	for _, flag := range []string{"dry-run", "threshold", "embedding-threshold", "scope"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}

	// Check default scope
	scope, _ := cmd.Flags().GetString("scope")
	if scope != "both" {
		t.Errorf("default scope = %q, want %q", scope, "both")
	}
}

func TestDeduplicateCmdInvalidScope(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeduplicateCmd())
	rootCmd.SetArgs([]string{"deduplicate", "--scope", "invalid", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for invalid scope")
	}
	if !strings.Contains(err.Error(), "invalid scope") {
		t.Errorf("expected 'invalid scope' error, got: %v", err)
	}
}

func TestDeduplicateCmdLocalNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeduplicateCmd())
	rootCmd.SetArgs([]string{"deduplicate", "--scope", "local", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when local .floop not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestFindDuplicatePairsNoDuplicates(t *testing.T) {
	behaviors := []models.Behavior{
		{
			ID:   "b-go",
			Name: "Go conventions",
			Content: models.BehaviorContent{
				Canonical: "use error wrapping with fmt.Errorf",
				Tags:      []string{"go", "errors"},
			},
		},
		{
			ID:   "b-python",
			Name: "Python conventions",
			Content: models.BehaviorContent{
				Canonical: "use type hints for function signatures",
				Tags:      []string{"python", "typing"},
			},
		},
	}

	cfg := dedup.DeduplicatorConfig{
		SimilarityThreshold: 0.9,
	}

	duplicates := findDuplicatePairs(behaviors, cfg, nil)
	if len(duplicates) != 0 {
		t.Errorf("expected 0 duplicates, got %d", len(duplicates))
	}
}

func TestFindDuplicatePairsIdentical(t *testing.T) {
	behaviors := []models.Behavior{
		{
			ID:   "b-1",
			Name: "Use error wrapping",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "use error wrapping with fmt.Errorf for context",
				Tags:      []string{"go", "errors"},
			},
		},
		{
			ID:   "b-2",
			Name: "Use error wrapping",
			When: map[string]interface{}{"language": "go"},
			Content: models.BehaviorContent{
				Canonical: "use error wrapping with fmt.Errorf for context",
				Tags:      []string{"go", "errors"},
			},
		},
	}

	cfg := dedup.DeduplicatorConfig{
		SimilarityThreshold: 0.5,
	}

	duplicates := findDuplicatePairs(behaviors, cfg, nil)
	if len(duplicates) == 0 {
		t.Error("expected at least 1 duplicate pair for identical behaviors")
	}
}

func TestFindDuplicatePairsEmpty(t *testing.T) {
	var behaviors []models.Behavior
	cfg := dedup.DeduplicatorConfig{SimilarityThreshold: 0.9}

	duplicates := findDuplicatePairs(behaviors, cfg, nil)
	if len(duplicates) != 0 {
		t.Errorf("expected 0 duplicates for empty input, got %d", len(duplicates))
	}
}

func TestFindDuplicatePairsSingle(t *testing.T) {
	behaviors := []models.Behavior{
		{
			ID:   "b-only",
			Name: "Single behavior",
			Content: models.BehaviorContent{
				Canonical: "only one behavior",
			},
		},
	}
	cfg := dedup.DeduplicatorConfig{SimilarityThreshold: 0.9}

	duplicates := findDuplicatePairs(behaviors, cfg, nil)
	if len(duplicates) != 0 {
		t.Errorf("expected 0 duplicates for single behavior, got %d", len(duplicates))
	}
}

func TestDeduplicateCmdDryRunLocal(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeduplicateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deduplicate", "--dry-run", "--scope", "local", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deduplicate --dry-run failed: %v", err)
	}
}

func TestDeduplicateCmdDryRunJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeduplicateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deduplicate", "--dry-run", "--json", "--scope", "local", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deduplicate --dry-run --json failed: %v", err)
	}
}

func TestDeduplicateCmdEmptyStore(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newDeduplicateCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"deduplicate", "--dry-run", "--scope", "local", "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("deduplicate empty store failed: %v", err)
	}
}

func TestDeduplicateCmdMerge(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeduplicateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deduplicate", "--scope", "local", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deduplicate failed: %v", err)
	}
}
