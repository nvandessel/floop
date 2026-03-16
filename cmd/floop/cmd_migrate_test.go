package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewMigrateCmd(t *testing.T) {
	cmd := newMigrateCmd()
	if cmd.Use != "migrate" {
		t.Errorf("Use = %q, want %q", cmd.Use, "migrate")
	}

	// Check flags exist
	if cmd.Flags().Lookup("merge-local-to-global") == nil {
		t.Error("missing --merge-local-to-global flag")
	}
}

func TestMigrateCmdNoAction(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newMigrateCmd())
	rootCmd.SetArgs([]string{"migrate"})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no action specified, got nil")
	}
	if !strings.Contains(err.Error(), "no migration action specified") {
		t.Errorf("error = %v, want containing %q", err, "no migration action specified")
	}
}

func TestMigrateCmdMergeLocalNoStore(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newMigrateCmd())
	rootCmd.SetArgs([]string{"migrate", "--merge-local-to-global", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no local store exists")
	}
}

func TestMigrateCmdMergeLocalToGlobal(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize local .floop
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Create a behavior via learn
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newLearnCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{
		"learn",
		"--wrong", "used raw SQL",
		"--right", "use parameterized queries",
		"--root", tmpDir,
	})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("learn failed: %v", err)
	}

	// Ensure global .floop dir exists
	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	// Run migrate
	rootCmd3 := newTestRootCmd()
	rootCmd3.AddCommand(newMigrateCmd())
	rootCmd3.SetArgs([]string{"migrate", "--merge-local-to-global", "--root", tmpDir, "--json"})
	var outBuf bytes.Buffer
	rootCmd3.SetOut(&outBuf)

	if err := rootCmd3.Execute(); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if result["status"] != "completed" {
		t.Errorf("status = %v, want %q", result["status"], "completed")
	}
}
