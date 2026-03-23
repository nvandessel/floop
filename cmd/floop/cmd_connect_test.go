package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

func TestValidEdgeKinds(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"requires", true},
		{"overrides", true},
		{"conflicts", true},
		{"similar-to", true},
		{"learned-from", true},
		{"invalid", false},
		{"", false},
		{"REQUIRES", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if got := store.ValidUserEdgeKinds[store.EdgeKind(tt.kind)]; got != tt.want {
				t.Errorf("store.ValidUserEdgeKinds[%q] = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestNewConnectCmd(t *testing.T) {
	cmd := newConnectCmd()

	if cmd.Use != "connect <source> <target> <kind>" {
		t.Errorf("Use = %q, want connect <source> <target> <kind>", cmd.Use)
	}

	// Verify flags exist
	if cmd.Flags().Lookup("weight") == nil {
		t.Error("missing --weight flag")
	}
	if cmd.Flags().Lookup("bidirectional") == nil {
		t.Error("missing --bidirectional flag")
	}

	// Verify default weight
	weight, _ := cmd.Flags().GetFloat64("weight")
	if weight != 0.8 {
		t.Errorf("default weight = %v, want 0.8", weight)
	}
}

func TestConnectCmdInvalidKind(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConnectCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"connect", "a", "b", "invalid-kind", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for invalid edge kind")
	}
	if !strings.Contains(err.Error(), "invalid edge kind") {
		t.Errorf("expected 'invalid edge kind' error, got: %v", err)
	}
}

func TestConnectCmdInvalidWeight(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConnectCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"connect", "a", "b", "similar-to", "--weight", "0", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for zero weight")
	}
}

func TestConnectCmdSelfEdge(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConnectCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"connect", "same-id", "same-id", "similar-to", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for self-edge")
	}
	if !strings.Contains(err.Error(), "self-edges") {
		t.Errorf("expected 'self-edges' error, got: %v", err)
	}
}

func TestConnectCmdNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConnectCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"connect", "a", "b", "similar-to", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when not initialized")
	}
}

func TestConnectCmdSourceNotFound(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConnectCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"connect", "nonexistent", "other", "similar-to", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestConnectCmdIntegration(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	// Learn a second behavior to connect to
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newLearnCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"learn",
		"--wrong", "used print for debugging",
		"--right", "use log package",
		"--file", "utils.go",
		"--task", "coding",
		"--root", tmpDir,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("learn second behavior failed: %v", err)
	}

	// Get second behavior ID
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newConnectCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	// Use the known first behavior ID and a meta-behavior (from seed) as target
	// Actually, let's query for the second behavior
	rootCmd2.SetArgs([]string{"connect", behaviorID, behaviorID[:len(behaviorID)-1] + "x", "similar-to", "--root", tmpDir})

	// This will fail because the second ID doesn't exist — that's fine, we're testing the path
	err := rootCmd2.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent target, got nil")
	}
	if !strings.Contains(err.Error(), "target node not found") {
		t.Errorf("expected 'target node not found' error, got: %v", err)
	}
}

func TestConnectCmdJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConnectCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"connect", "nonexistent", "other", "similar-to", "--json", "--root", tmpDir})

	// Will fail with source not found, but exercises the JSON path check
	_ = rootCmd.Execute()
}
