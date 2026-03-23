package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

func TestNewValidateCmd(t *testing.T) {
	cmd := newValidateCmd()
	if cmd.Use != "validate" {
		t.Errorf("Use = %q, want %q", cmd.Use, "validate")
	}

	scopeFlag := cmd.Flags().Lookup("scope")
	if scopeFlag == nil {
		t.Fatal("missing --scope flag")
	}
	if scopeFlag.DefValue != "both" {
		t.Errorf("scope default = %q, want %q", scopeFlag.DefValue, "both")
	}
}

func TestValidateCmdInvalidScope(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.SetArgs([]string{"validate", "--scope", "invalid", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for invalid scope")
	}
	if !strings.Contains(err.Error(), "invalid scope") {
		t.Errorf("expected 'invalid scope' error, got: %v", err)
	}
}

func TestValidateCmdLocalNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.SetArgs([]string{"validate", "--scope", "local", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when local .floop not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestOutputValidationResultsValid(t *testing.T) {
	// Valid graph — no errors
	var errs []store.ValidationError
	err := outputValidationResults(errs, store.ScopeLocal, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOutputValidationResultsWithErrors(t *testing.T) {
	errs := []store.ValidationError{
		{
			BehaviorID: "b-test",
			Field:      "requires",
			RefID:      "b-missing",
			Issue:      "dangling",
		},
	}

	err := outputValidationResults(errs, store.ScopeLocal, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOutputValidationResultsValidText(t *testing.T) {
	var errs []store.ValidationError
	// Non-JSON mode — writes to stdout
	err := outputValidationResults(errs, store.ScopeLocal, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOutputValidationResultsWithErrorsText(t *testing.T) {
	errs := []store.ValidationError{
		{
			BehaviorID: "b-test",
			Field:      "requires",
			RefID:      "b-missing",
			Issue:      "dangling",
		},
		{
			BehaviorID: "b-test2",
			Field:      "overrides",
			RefID:      "b-test2",
			Issue:      "self-reference",
		},
	}

	err := outputValidationResults(errs, store.ScopeBoth, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCmdBothNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.SetArgs([]string{"validate", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when no stores initialized")
	}
	if !strings.Contains(err.Error(), "no .floop stores initialized") {
		t.Errorf("expected 'no stores' error, got: %v", err)
	}
}

func TestValidateCmdWithInitializedStore(t *testing.T) {
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

	// Validate local scope
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newValidateCmd())
	rootCmd2.SetArgs([]string{"validate", "--scope", "local", "--root", tmpDir})
	rootCmd2.SetOut(&bytes.Buffer{})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("validate --scope local failed: %v", err)
	}
}

func TestValidateCmdWithInitializedStoreJSON(t *testing.T) {
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
	rootCmd2.AddCommand(newValidateCmd())
	rootCmd2.SetArgs([]string{"validate", "--scope", "local", "--json", "--root", tmpDir})
	rootCmd2.SetOut(&bytes.Buffer{})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("validate --scope local --json failed: %v", err)
	}
}

func TestValidateCmdGlobalNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.SetArgs([]string{"validate", "--scope", "global", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when global .floop not initialized")
	}
}
