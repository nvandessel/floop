package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewInitCmdFlags(t *testing.T) {
	cmd := newInitCmd()
	if cmd.Use != "init" {
		t.Errorf("Use = %q, want %q", cmd.Use, "init")
	}

	for _, flag := range []string{"global", "project", "hooks", "token-budget", "embeddings", "no-embeddings"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestInitCmdNonInteractiveDefaultsToProject(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Running with --root but no scope flags defaults to project
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify .floop directory was created at project root
	floopDir := filepath.Join(tmpDir, ".floop")
	if _, err := os.Stat(floopDir); os.IsNotExist(err) {
		t.Error(".floop directory not created at project root")
	}
}

func TestInitCmdGlobalScope(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--global"})
	rootCmd.SetOut(&bytes.Buffer{})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init --global failed: %v", err)
	}

	// Verify global .floop was created
	globalFloopDir := filepath.Join(tmpDir, "home", ".floop")
	if _, err := os.Stat(globalFloopDir); os.IsNotExist(err) {
		t.Error("global .floop directory not created")
	}
}

func TestInitCmdBothScopes(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--global", "--project", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init --global --project failed: %v", err)
	}

	// Verify both directories were created
	localFloopDir := filepath.Join(tmpDir, ".floop")
	if _, err := os.Stat(localFloopDir); os.IsNotExist(err) {
		t.Error("local .floop directory not created")
	}
	globalFloopDir := filepath.Join(tmpDir, "home", ".floop")
	if _, err := os.Stat(globalFloopDir); os.IsNotExist(err) {
		t.Error("global .floop directory not created")
	}
}

func TestInitCmdJSONRequiresScope(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--json"})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when --json without scope flags")
	}
	if !strings.Contains(err.Error(), "explicit scope flags") {
		t.Errorf("expected scope error, got: %v", err)
	}
}

func TestInitCmdIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Run init twice — should not fail
	for i := 0; i < 2; i++ {
		rootCmd := newTestRootCmd()
		rootCmd.AddCommand(newInitCmd())
		rootCmd.SetArgs([]string{"init", "--root", tmpDir})
		rootCmd.SetOut(&bytes.Buffer{})

		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("init attempt %d failed: %v", i+1, err)
		}
	}
}

func TestReadLine(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("hello\n"))
	result := readLine(reader)
	if result != "hello" {
		t.Errorf("readLine() = %q, want %q", result, "hello")
	}
}

func TestReadLineEmpty(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	result := readLine(reader)
	if result != "" {
		t.Errorf("readLine() = %q, want empty string", result)
	}
}
