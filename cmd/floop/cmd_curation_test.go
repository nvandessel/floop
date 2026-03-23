package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestForgetCmdNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newForgetCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"forget", "any-id", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestForgetCmdForce(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newForgetCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"forget", behaviorID, "--force", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("forget --force failed: %v", err)
	}
}

func TestForgetCmdNotFound(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newForgetCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"forget", "nonexistent-id", "--force", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent behavior")
	}
}

func TestForgetCmdJSON(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newForgetCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"forget", behaviorID, "--json", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("forget --json failed: %v", err)
	}
}

func TestForgetCmdWithReason(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newForgetCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"forget", behaviorID, "--force", "--reason", "outdated", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("forget --reason failed: %v", err)
	}
}

func TestDeprecateCmdNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeprecateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deprecate", "any-id", "--reason", "test", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when not initialized")
	}
}

func TestDeprecateCmdNoReason(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeprecateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deprecate", behaviorID, "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when no reason")
	}
	if !strings.Contains(err.Error(), "--reason is required") {
		t.Errorf("expected '--reason is required' error, got: %v", err)
	}
}

func TestDeprecateCmdWithReason(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeprecateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deprecate", behaviorID, "--reason", "replaced by new pattern", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deprecate failed: %v", err)
	}
}

func TestDeprecateCmdJSON(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeprecateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deprecate", behaviorID, "--reason", "test", "--json", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deprecate --json failed: %v", err)
	}
}

func TestDeprecateCmdNotFound(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeprecateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deprecate", "nonexistent-id", "--reason", "test", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent behavior")
	}
}

func TestRestoreCmdNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newRestoreCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"restore", "any-id", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when not initialized")
	}
}

func TestRestoreCmdNotFound(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newRestoreCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"restore", "nonexistent-id", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent behavior")
	}
}

func TestRestoreCmdNotRestorable(t *testing.T) {
	// Restore an active behavior (not forgotten/deprecated) should fail
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newRestoreCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"restore", behaviorID, "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for non-restorable behavior")
	}
	if !strings.Contains(err.Error(), "not deprecated or forgotten") {
		t.Errorf("expected 'not deprecated or forgotten' error, got: %v", err)
	}
}

func TestForgetThenRestore(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	// Forget
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newForgetCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"forget", behaviorID, "--force", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("forget failed: %v", err)
	}

	// Restore
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newRestoreCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"restore", behaviorID, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("restore after forget failed: %v", err)
	}
}

func TestDeprecateThenRestore(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	// Deprecate
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDeprecateCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"deprecate", behaviorID, "--reason", "outdated", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deprecate failed: %v", err)
	}

	// Restore
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newRestoreCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"restore", behaviorID, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("restore after deprecate failed: %v", err)
	}
}

func TestRestoreCmdJSON(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	// Forget first
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newForgetCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"forget", behaviorID, "--force", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("forget failed: %v", err)
	}

	// Restore with JSON
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newRestoreCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"restore", behaviorID, "--json", "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("restore --json failed: %v", err)
	}
}

func TestMergeCmdNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newMergeCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"merge", "id1", "id2", "--force", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error when not initialized")
	}
}

func TestMergeCmdSourceNotFound(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newMergeCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"merge", "nonexistent", "other", "--force", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestMergeCmdInvalidInto(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newMergeCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"merge", "id1", "id2", "--force", "--into", "id3", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for invalid --into")
	}
	if !strings.Contains(err.Error(), "--into must be one of") {
		t.Errorf("expected '--into must be one of' error, got: %v", err)
	}
}
