package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

// newTestRootCmd creates a root command with persistent flags for testing subcommands
func newTestRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "floop",
	}
	rootCmd.PersistentFlags().Bool("json", false, "Output as JSON")
	rootCmd.PersistentFlags().String("root", ".", "Project root directory")
	return rootCmd
}

// isolateHome sets HOME to a temp directory to avoid touching real ~/.floop/
// MUST be called for any test that creates stores
func isolateHome(t *testing.T, tmpDir string) {
	t.Helper()
	tmpHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(tmpHome, 0700); err != nil {
		t.Fatalf("Failed to create temp home: %v", err)
	}
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
	})
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", []string{}},
		{"single line no newline", "foo", []string{"foo"}},
		{"single line with newline", "foo\n", []string{"foo"}},
		{"multiple lines", "foo\nbar\nbaz", []string{"foo", "bar", "baz"}},
		{"multiple lines with trailing", "foo\nbar\n", []string{"foo", "bar"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitLines(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNewVersionCmd(t *testing.T) {
	cmd := newVersionCmd()
	if cmd.Use != "version" {
		t.Errorf("Use = %q, want %q", cmd.Use, "version")
	}
}

func TestNewInitCmd(t *testing.T) {
	cmd := newInitCmd()
	if cmd.Use != "init" {
		t.Errorf("Use = %q, want %q", cmd.Use, "init")
	}
}

func TestNewLearnCmd(t *testing.T) {
	cmd := newLearnCmd()
	if cmd.Use != "learn" {
		t.Errorf("Use = %q, want %q", cmd.Use, "learn")
	}

	// Check required flags exist
	wrongFlag := cmd.Flags().Lookup("wrong")
	if wrongFlag == nil {
		t.Error("missing --wrong flag")
	}
	rightFlag := cmd.Flags().Lookup("right")
	if rightFlag == nil {
		t.Error("missing --right flag")
	}
}

func TestNewListCmd(t *testing.T) {
	cmd := newListCmd()
	if cmd.Use != "list" {
		t.Errorf("Use = %q, want %q", cmd.Use, "list")
	}

	// Check corrections flag exists
	correctionsFlag := cmd.Flags().Lookup("corrections")
	if correctionsFlag == nil {
		t.Error("missing --corrections flag")
	}

	// Check global flag exists
	globalFlag := cmd.Flags().Lookup("global")
	if globalFlag == nil {
		t.Error("missing --global flag")
	}

	// Check all flag exists
	allFlag := cmd.Flags().Lookup("all")
	if allFlag == nil {
		t.Error("missing --all flag")
	}
}

func TestInitCmdCreatesDirectory(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Run init command with root command context
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{}) // Suppress output
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify .floop directory was created
	floopDir := filepath.Join(tmpDir, ".floop")
	if _, err := os.Stat(floopDir); os.IsNotExist(err) {
		t.Error(".floop directory not created")
	}

	// Verify manifest.yaml was created
	manifestPath := filepath.Join(floopDir, "manifest.yaml")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("manifest.yaml not created")
	}

	// Verify hook scripts were extracted
	hookDir := filepath.Join(tmpDir, ".claude", "hooks")
	if _, err := os.Stat(filepath.Join(hookDir, "floop-session-start.sh")); os.IsNotExist(err) {
		t.Error("floop-session-start.sh not extracted")
	}

	// Verify settings.json was created
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Error("settings.json not created")
	}
}

func TestLearnCmdRequiresInit(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newLearnCmd())
	rootCmd.SetArgs([]string{"learn", "--wrong", "test", "--right", "test", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when .floop not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestLearnCmdCapturesCorrection(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize first
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Run learn command
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newLearnCmd())
	rootCmd2.SetArgs([]string{
		"learn",
		"--wrong", "used os.path",
		"--right", "use pathlib.Path",
		"--file", "script.py",
		"--task", "refactor",
		"--root", tmpDir,
	})
	rootCmd2.SetOut(&bytes.Buffer{})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("learn failed: %v", err)
	}

	// Verify correction was written
	correctionsPath := filepath.Join(tmpDir, ".floop", "corrections.jsonl")
	data, err := os.ReadFile(correctionsPath)
	if err != nil {
		t.Fatalf("failed to read corrections: %v", err)
	}

	var correction map[string]interface{}
	if err := json.Unmarshal(data, &correction); err != nil {
		t.Fatalf("failed to parse correction: %v", err)
	}

	if correction["agent_action"] != "used os.path" {
		t.Errorf("agent_action = %v, want %q", correction["agent_action"], "used os.path")
	}
	if correction["corrected_action"] != "use pathlib.Path" {
		t.Errorf("corrected_action = %v, want %q", correction["corrected_action"], "use pathlib.Path")
	}

	// Check context is present
	ctx, ok := correction["context"].(map[string]interface{})
	if !ok {
		t.Fatal("context not present or not a map")
	}
	if ctx["file_path"] != "script.py" {
		t.Errorf("context.file_path = %v, want %q", ctx["file_path"], "script.py")
	}
	if ctx["task"] != "refactor" {
		t.Errorf("context.task = %v, want %q", ctx["task"], "refactor")
	}
	if ctx["file_language"] != "python" {
		t.Errorf("context.file_language = %v, want %q", ctx["file_language"], "python")
	}
}

func TestListCorrectionsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// List should succeed with empty results
	err := listCorrections(tmpDir, false)
	if err != nil {
		t.Fatalf("listCorrections failed: %v", err)
	}
}

func TestListCorrectionsNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// List should succeed gracefully
	err := listCorrections(tmpDir, false)
	if err != nil {
		t.Fatalf("listCorrections failed: %v", err)
	}
}

func TestListCmdGlobalAndAllFlagsConflict(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize local
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Try to list with both --global and --all
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newListCmd())
	rootCmd2.SetArgs([]string{"list", "--global", "--all", "--root", tmpDir})
	rootCmd2.SetOut(&bytes.Buffer{})
	err := rootCmd2.Execute()
	if err == nil {
		t.Error("expected error when both --global and --all are specified")
	}
	if !strings.Contains(err.Error(), "cannot specify both") {
		t.Errorf("expected 'cannot specify both' error, got: %v", err)
	}
}

func TestListCmdWithGlobalFlag(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize local
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Initialize global
	rootCmd1 := newTestRootCmd()
	rootCmd1.AddCommand(newInitCmd())
	rootCmd1.SetArgs([]string{"init", "--global"})
	rootCmd1.SetOut(&bytes.Buffer{})
	if err := rootCmd1.Execute(); err != nil {
		t.Fatalf("global init failed: %v", err)
	}

	// List with --global should work
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newListCmd())
	rootCmd2.SetArgs([]string{"list", "--global", "--root", tmpDir})
	var out bytes.Buffer
	rootCmd2.SetOut(&out)
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("list --global failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "global") {
		t.Errorf("expected output to mention 'global', got: %s", output)
	}
}

func TestListCmdWithAllFlag(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize local
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Initialize global (optional for this test)
	rootCmd1 := newTestRootCmd()
	rootCmd1.AddCommand(newInitCmd())
	rootCmd1.SetArgs([]string{"init", "--global"})
	rootCmd1.SetOut(&bytes.Buffer{})
	_ = rootCmd1.Execute() // Ignore error if already exists

	// List with --all should work
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newListCmd())
	rootCmd2.SetArgs([]string{"list", "--all", "--root", tmpDir})
	var out bytes.Buffer
	rootCmd2.SetOut(&out)
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("list --all failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "all") {
		t.Errorf("expected output to mention 'all', got: %s", output)
	}
}

// ============================================================================
// Curation Command Tests
// ============================================================================

func TestNewForgetCmd(t *testing.T) {
	cmd := newForgetCmd()
	if cmd.Use != "forget <behavior-id>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "forget <behavior-id>")
	}

	// Check flags exist
	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Error("missing --force flag")
	}
	reasonFlag := cmd.Flags().Lookup("reason")
	if reasonFlag == nil {
		t.Error("missing --reason flag")
	}
}

func TestNewDeprecateCmd(t *testing.T) {
	cmd := newDeprecateCmd()
	if cmd.Use != "deprecate <behavior-id>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "deprecate <behavior-id>")
	}

	// Check flags exist
	reasonFlag := cmd.Flags().Lookup("reason")
	if reasonFlag == nil {
		t.Error("missing --reason flag")
	}
	replacementFlag := cmd.Flags().Lookup("replacement")
	if replacementFlag == nil {
		t.Error("missing --replacement flag")
	}
}

func TestNewRestoreCmd(t *testing.T) {
	cmd := newRestoreCmd()
	if cmd.Use != "restore <behavior-id>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "restore <behavior-id>")
	}
}

func TestNewMergeCmd(t *testing.T) {
	cmd := newMergeCmd()
	if cmd.Use != "merge <source-id> <target-id>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "merge <source-id> <target-id>")
	}

	// Check flags exist
	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Error("missing --force flag")
	}
	intoFlag := cmd.Flags().Lookup("into")
	if intoFlag == nil {
		t.Error("missing --into flag")
	}
}

func TestForgetCmdRequiresInit(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newForgetCmd())
	rootCmd.SetArgs([]string{"forget", "test-id", "--force", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when .floop not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestDeprecateCmdRequiresReason(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize first
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Try deprecate without reason
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newDeprecateCmd())
	rootCmd2.SetArgs([]string{"deprecate", "test-id", "--root", tmpDir})
	rootCmd2.SetOut(&bytes.Buffer{})
	err := rootCmd2.Execute()
	if err == nil {
		t.Error("expected error when --reason not provided")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("expected 'reason' error, got: %v", err)
	}
}

func TestCurationWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Create a behavior via learn command
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newLearnCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{
		"learn",
		"--wrong", "used print",
		"--right", "use logging",
		"--file", "test.py",
		"--root", tmpDir,
	})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("learn failed: %v", err)
	}

	// Get behavior ID from store
	graphStore, err := store.NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	ctx := context.Background()
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("failed to query behaviors: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("no behaviors found after learn")
	}
	behaviorID := nodes[0].ID
	graphStore.Close()

	// Test forget
	rootCmd3 := newTestRootCmd()
	rootCmd3.AddCommand(newForgetCmd())
	rootCmd3.SetOut(&bytes.Buffer{})
	rootCmd3.SetArgs([]string{
		"forget", behaviorID,
		"--force",
		"--reason", "testing",
		"--root", tmpDir,
	})
	if err := rootCmd3.Execute(); err != nil {
		t.Fatalf("forget failed: %v", err)
	}

	// Verify node kind changed
	graphStore, _ = store.NewFileGraphStore(tmpDir)
	node, _ := graphStore.GetNode(ctx, behaviorID)
	if node.Kind != "forgotten-behavior" {
		t.Errorf("after forget, kind = %q, want 'forgotten-behavior'", node.Kind)
	}
	if node.Metadata["forget_reason"] != "testing" {
		t.Errorf("forget_reason = %v, want 'testing'", node.Metadata["forget_reason"])
	}
	graphStore.Close()

	// Test restore
	rootCmd4 := newTestRootCmd()
	rootCmd4.AddCommand(newRestoreCmd())
	rootCmd4.SetOut(&bytes.Buffer{})
	rootCmd4.SetArgs([]string{
		"restore", behaviorID,
		"--root", tmpDir,
	})
	if err := rootCmd4.Execute(); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	// Verify restored
	graphStore, _ = store.NewFileGraphStore(tmpDir)
	node, _ = graphStore.GetNode(ctx, behaviorID)
	if node.Kind != "behavior" {
		t.Errorf("after restore, kind = %q, want 'behavior'", node.Kind)
	}
	if _, hasKey := node.Metadata["forget_reason"]; hasKey {
		t.Error("forget_reason should be cleaned up after restore")
	}
	graphStore.Close()

	// Test deprecate
	rootCmd5 := newTestRootCmd()
	rootCmd5.AddCommand(newDeprecateCmd())
	rootCmd5.SetOut(&bytes.Buffer{})
	rootCmd5.SetArgs([]string{
		"deprecate", behaviorID,
		"--reason", "outdated approach",
		"--root", tmpDir,
	})
	if err := rootCmd5.Execute(); err != nil {
		t.Fatalf("deprecate failed: %v", err)
	}

	// Verify deprecated
	graphStore, _ = store.NewFileGraphStore(tmpDir)
	node, _ = graphStore.GetNode(ctx, behaviorID)
	if node.Kind != "deprecated-behavior" {
		t.Errorf("after deprecate, kind = %q, want 'deprecated-behavior'", node.Kind)
	}
	if node.Metadata["deprecation_reason"] != "outdated approach" {
		t.Errorf("deprecation_reason = %v, want 'outdated approach'", node.Metadata["deprecation_reason"])
	}
	graphStore.Close()

	// Restore from deprecated state
	rootCmd6 := newTestRootCmd()
	rootCmd6.AddCommand(newRestoreCmd())
	rootCmd6.SetOut(&bytes.Buffer{})
	rootCmd6.SetArgs([]string{
		"restore", behaviorID,
		"--root", tmpDir,
	})
	if err := rootCmd6.Execute(); err != nil {
		t.Fatalf("restore from deprecated failed: %v", err)
	}

	// Verify restored again
	graphStore, _ = store.NewFileGraphStore(tmpDir)
	node, _ = graphStore.GetNode(ctx, behaviorID)
	if node.Kind != "behavior" {
		t.Errorf("after second restore, kind = %q, want 'behavior'", node.Kind)
	}
	graphStore.Close()
}

func TestMergeWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Create first behavior (distinctly different)
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newLearnCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{
		"learn",
		"--wrong", "hardcoded database credentials",
		"--right", "use environment variables for secrets",
		"--file", "config.py",
		"--root", tmpDir,
	})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("learn 1 failed: %v", err)
	}

	// Create second behavior (completely different topic)
	rootCmd3 := newTestRootCmd()
	rootCmd3.AddCommand(newLearnCmd())
	rootCmd3.SetOut(&bytes.Buffer{})
	rootCmd3.SetArgs([]string{
		"learn",
		"--wrong", "concatenating SQL strings",
		"--right", "use parameterized queries",
		"--file", "database.go",
		"--root", tmpDir,
	})
	if err := rootCmd3.Execute(); err != nil {
		t.Fatalf("learn 2 failed: %v", err)
	}

	// Get behavior IDs from store
	ctx := context.Background()
	graphStore, err := store.NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("failed to query behaviors: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("expected 2 behaviors, got %d", len(nodes))
	}
	id1 := nodes[0].ID
	id2 := nodes[1].ID
	graphStore.Close()

	// Merge behaviors
	rootCmd4 := newTestRootCmd()
	rootCmd4.AddCommand(newMergeCmd())
	rootCmd4.SetOut(&bytes.Buffer{})
	rootCmd4.SetArgs([]string{
		"merge", id1, id2,
		"--force",
		"--root", tmpDir,
	})
	if err := rootCmd4.Execute(); err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// Verify source is now merged-behavior
	graphStore, _ = store.NewFileGraphStore(tmpDir)
	sourceNode, _ := graphStore.GetNode(ctx, id1)
	if sourceNode.Kind != "merged-behavior" {
		t.Errorf("source kind = %q, want 'merged-behavior'", sourceNode.Kind)
	}
	if sourceNode.Metadata["merged_into"] != id2 {
		t.Errorf("merged_into = %v, want %v", sourceNode.Metadata["merged_into"], id2)
	}

	// Verify target is still a behavior
	targetNode, _ := graphStore.GetNode(ctx, id2)
	if targetNode.Kind != "behavior" {
		t.Errorf("target kind = %q, want 'behavior'", targetNode.Kind)
	}

	// Verify merged-into edge exists
	edges, _ := graphStore.GetEdges(ctx, id1, store.DirectionOutbound, "merged-into")
	if len(edges) != 1 {
		t.Errorf("expected 1 merged-into edge, got %d", len(edges))
	} else {
		if edges[0].Target != id2 {
			t.Errorf("merged-into edge target = %v, want %v", edges[0].Target, id2)
		}
		if edges[0].Weight <= 0 {
			t.Errorf("merged-into edge weight = %f, want > 0", edges[0].Weight)
		}
		if edges[0].CreatedAt.IsZero() {
			t.Error("merged-into edge CreatedAt should not be zero")
		}
	}
	graphStore.Close()

	// Try to restore merged behavior (should fail)
	rootCmd5 := newTestRootCmd()
	rootCmd5.AddCommand(newRestoreCmd())
	rootCmd5.SetOut(&bytes.Buffer{})
	rootCmd5.SetArgs([]string{
		"restore", id1,
		"--root", tmpDir,
	})
	// This should NOT fail the command but should output an error in JSON mode
	// In non-JSON mode, it returns an error
	err = rootCmd5.Execute()
	// Since we're not in JSON mode, it should return an error
	if err == nil {
		t.Error("expected error when restoring merged behavior")
	}
	if !strings.Contains(err.Error(), "not deprecated or forgotten") {
		t.Errorf("expected 'not deprecated or forgotten' error, got: %v", err)
	}
}

func TestForgetBehaviorNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Try to forget non-existent behavior (non-JSON mode returns error)
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newForgetCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{
		"forget", "non-existent-id",
		"--force",
		"--root", tmpDir,
	})
	err := rootCmd2.Execute()
	if err == nil {
		t.Error("expected error when behavior not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestRestoreActiveBehaviorFails(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Create a behavior
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newLearnCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{
		"learn",
		"--wrong", "test wrong",
		"--right", "test right",
		"--file", "test.py",
		"--root", tmpDir,
	})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("learn failed: %v", err)
	}

	// Get behavior ID from store
	ctx := context.Background()
	graphStore, err := store.NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("failed to query behaviors: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("no behaviors found after learn")
	}
	behaviorID := nodes[0].ID
	graphStore.Close()

	// Try to restore an already active behavior
	rootCmd3 := newTestRootCmd()
	rootCmd3.AddCommand(newRestoreCmd())
	rootCmd3.SetOut(&bytes.Buffer{})
	rootCmd3.SetArgs([]string{
		"restore", behaviorID,
		"--root", tmpDir,
	})
	err = rootCmd3.Execute()
	if err == nil {
		t.Error("expected error when restoring active behavior")
	}
	if !strings.Contains(err.Error(), "not deprecated or forgotten") {
		t.Errorf("expected 'not deprecated or forgotten' error, got: %v", err)
	}
}

func TestDeprecateWithReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Create first behavior (to be deprecated)
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newLearnCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{
		"learn",
		"--wrong", "used print statements for debugging",
		"--right", "use structured logging",
		"--file", "app.py",
		"--root", tmpDir,
	})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("learn 1 failed: %v", err)
	}

	// Create second behavior (replacement)
	rootCmd3a := newTestRootCmd()
	rootCmd3a.AddCommand(newLearnCmd())
	rootCmd3a.SetOut(&bytes.Buffer{})
	rootCmd3a.SetArgs([]string{
		"learn",
		"--wrong", "used fmt.Println for logging",
		"--right", "use slog package",
		"--file", "main.go",
		"--root", tmpDir,
	})
	if err := rootCmd3a.Execute(); err != nil {
		t.Fatalf("learn 2 failed: %v", err)
	}

	// Get behavior IDs from store
	ctx := context.Background()
	graphStore, err := store.NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("failed to query behaviors: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("expected 2 behaviors, got %d", len(nodes))
	}
	id1 := nodes[0].ID
	id2 := nodes[1].ID
	graphStore.Close()

	// Deprecate the first behavior with second as replacement
	rootCmd4 := newTestRootCmd()
	rootCmd4.AddCommand(newDeprecateCmd())
	rootCmd4.SetOut(&bytes.Buffer{})
	rootCmd4.SetArgs([]string{
		"deprecate", id1,
		"--reason", "superseded by slog approach",
		"--replacement", id2,
		"--root", tmpDir,
	})
	if err := rootCmd4.Execute(); err != nil {
		t.Fatalf("deprecate failed: %v", err)
	}

	// Verify deprecated-to edge exists with valid Weight and CreatedAt
	graphStore, err = store.NewFileGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	edges, err := graphStore.GetEdges(ctx, id1, store.DirectionOutbound, "deprecated-to")
	if err != nil {
		t.Fatalf("failed to get edges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 deprecated-to edge, got %d", len(edges))
	}
	if edges[0].Target != id2 {
		t.Errorf("deprecated-to edge target = %v, want %v", edges[0].Target, id2)
	}
	if edges[0].Weight <= 0 {
		t.Errorf("deprecated-to edge weight = %f, want > 0", edges[0].Weight)
	}
	if edges[0].CreatedAt.IsZero() {
		t.Error("deprecated-to edge CreatedAt should not be zero")
	}
	graphStore.Close()
}
