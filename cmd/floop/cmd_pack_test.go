package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNewPackCmd(t *testing.T) {
	cmd := newPackCmd()

	if cmd.Use != "pack" {
		t.Errorf("Use = %q, want %q", cmd.Use, "pack")
	}

	// Verify subcommands exist
	subcommands := map[string]bool{
		"create":  false,
		"install": false,
		"list":    false,
		"info":    false,
		"update":  false,
		"remove":  false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := subcommands[sub.Name()]; ok {
			subcommands[sub.Name()] = true
		}
	}

	for name, found := range subcommands {
		if !found {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestNewPackCreateCmd_Flags(t *testing.T) {
	cmd := newPackCreateCmd()

	if cmd.Use != "create <output-path>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "create <output-path>")
	}

	requiredFlags := []string{"id", "version"}
	for _, flag := range requiredFlags {
		f := cmd.Flags().Lookup(flag)
		if f == nil {
			t.Errorf("missing --%s flag", flag)
			continue
		}
	}

	optionalFlags := []string{"description", "author", "tags", "source", "filter-tags", "filter-scope", "filter-kinds"}
	for _, flag := range optionalFlags {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestNewPackInstallCmd_Args(t *testing.T) {
	cmd := newPackInstallCmd()

	if cmd.Use != "install <source>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "install <source>")
	}

	if f := cmd.Flags().Lookup("derive-edges"); f == nil {
		t.Error("missing --derive-edges flag")
	}

	if f := cmd.Flags().Lookup("all-assets"); f == nil {
		t.Error("missing --all-assets flag")
	}
}

func TestNewPackListCmd(t *testing.T) {
	cmd := newPackListCmd()

	if cmd.Use != "list" {
		t.Errorf("Use = %q, want %q", cmd.Use, "list")
	}
}

func TestNewPackInfoCmd_Args(t *testing.T) {
	cmd := newPackInfoCmd()

	if cmd.Use != "info <pack-id>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "info <pack-id>")
	}
}

func TestNewPackUpdateCmd_Args(t *testing.T) {
	cmd := newPackUpdateCmd()

	if cmd.Use != "update [pack-id|source]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "update [pack-id|source]")
	}

	if f := cmd.Flags().Lookup("derive-edges"); f == nil {
		t.Error("missing --derive-edges flag")
	}

	if f := cmd.Flags().Lookup("all"); f == nil {
		t.Error("missing --all flag")
	}
}

func TestNewPackRemoveCmd_Args(t *testing.T) {
	cmd := newPackRemoveCmd()

	if cmd.Use != "remove <pack-id>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "remove <pack-id>")
	}
}

func TestPackCreateIntegration(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := filepath.Join(tmpDir, "test.fpack")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"pack", "create", outputPath,
		"--id", "test-org/test-pack",
		"--version", "1.0.0",
		"--description", "Test pack",
		"--root", tmpDir,
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack create failed: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("pack file not created: %v", err)
	}
}

func TestPackCreateJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := filepath.Join(tmpDir, "test.fpack")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"pack", "create", outputPath,
		"--id", "test-org/test-pack",
		"--version", "1.0.0",
		"--json",
		"--root", tmpDir,
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack create --json failed: %v", err)
	}
}

func TestPackCreateWithFilters(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := filepath.Join(tmpDir, "filtered.fpack")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"pack", "create", outputPath,
		"--id", "test-org/filtered-pack",
		"--version", "1.0.0",
		"--filter-tags", "go,testing",
		"--filter-kinds", "directive",
		"--root", tmpDir,
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack create with filters failed: %v", err)
	}
}

func TestPackListIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "list", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack list failed: %v", err)
	}
}

func TestPackListJSON(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "list", "--json", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack list --json failed: %v", err)
	}
}

func TestPackInfoNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "info", "nonexistent/pack", "--root", tmpDir})

	// Should not error — just prints not found message
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack info failed: %v", err)
	}
}

func TestPackInfoJSON(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "info", "nonexistent/pack", "--json", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack info --json failed: %v", err)
	}
}

func TestPackInstallFromFile(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	// Create a pack first
	packPath := filepath.Join(tmpDir, "install-test.fpack")
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"pack", "create", packPath,
		"--id", "test-org/install-test",
		"--version", "1.0.0",
		"--root", tmpDir,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack create failed: %v", err)
	}

	// Install the pack
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newPackCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"pack", "install", packPath, "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("pack install failed: %v", err)
	}
}

func TestPackInstallJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	packPath := filepath.Join(tmpDir, "install-json.fpack")
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"pack", "create", packPath,
		"--id", "test-org/install-json",
		"--version", "1.0.0",
		"--root", tmpDir,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack create failed: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newPackCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"pack", "install", packPath, "--json", "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("pack install --json failed: %v", err)
	}
}

func TestPackRemoveNotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "remove", "nonexistent/pack", "--root", tmpDir})

	// Remove succeeds even for nonexistent packs (reports 0 behaviors forgotten)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack remove failed: %v", err)
	}
}

func TestPackUpdateAllNotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "update", "--all", "--root", tmpDir})

	// Should succeed — just reports nothing to update
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack update --all failed: %v", err)
	}
}

func TestPackCreateThenInfo(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	// Create and install a pack
	packPath := filepath.Join(tmpDir, "info-test.fpack")
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"pack", "create", packPath,
		"--id", "test-org/info-test",
		"--version", "1.0.0",
		"--root", tmpDir,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack create failed: %v", err)
	}

	// Install
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newPackCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"pack", "install", packPath, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("pack install failed: %v", err)
	}

	// Info
	rootCmd3 := newTestRootCmd()
	rootCmd3.AddCommand(newPackCmd())
	rootCmd3.SetOut(&bytes.Buffer{})
	rootCmd3.SetArgs([]string{"pack", "info", "test-org/info-test", "--root", tmpDir})
	if err := rootCmd3.Execute(); err != nil {
		t.Fatalf("pack info failed: %v", err)
	}
}

func TestPackAddToPack(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "add", behaviorID, "--to", "test-org/my-pack", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack add failed: %v", err)
	}
}

func TestPackAddJSON(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "add", behaviorID, "--to", "test-org/my-pack", "--json", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack add --json failed: %v", err)
	}
}

func TestPackRemoveBehavior(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	// First add to pack
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "add", behaviorID, "--to", "test-org/my-pack", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack add failed: %v", err)
	}

	// Then remove from pack
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newPackCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"pack", "remove-behavior", behaviorID, "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("pack remove-behavior failed: %v", err)
	}
}

func TestPackRemoveBehaviorJSON(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "add", behaviorID, "--to", "test-org/my-pack", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack add failed: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newPackCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"pack", "remove-behavior", behaviorID, "--json", "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("pack remove-behavior --json failed: %v", err)
	}
}

func TestPackRemoveBehaviorForget(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"pack", "add", behaviorID, "--to", "test-org/my-pack", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack add failed: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newPackCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"pack", "remove-behavior", behaviorID, "--forget", "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("pack remove-behavior --forget failed: %v", err)
	}
}

func TestPackUpdateSpecificPack(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	// Create and install a pack
	packPath := filepath.Join(tmpDir, "update-test.fpack")
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"pack", "create", packPath,
		"--id", "test-org/update-test",
		"--version", "1.0.0",
		"--root", tmpDir,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack create failed: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newPackCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"pack", "install", packPath, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("pack install failed: %v", err)
	}

	// Update the specific pack
	rootCmd3 := newTestRootCmd()
	rootCmd3.AddCommand(newPackCmd())
	rootCmd3.SetOut(&bytes.Buffer{})
	rootCmd3.SetArgs([]string{"pack", "update", packPath, "--root", tmpDir})

	if err := rootCmd3.Execute(); err != nil {
		t.Fatalf("pack update specific failed: %v", err)
	}
}

func TestPackCreateInstallRemove(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	// Create
	packPath := filepath.Join(tmpDir, "remove-test.fpack")
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPackCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{
		"pack", "create", packPath,
		"--id", "test-org/remove-test",
		"--version", "1.0.0",
		"--root", tmpDir,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pack create failed: %v", err)
	}

	// Install
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newPackCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"pack", "install", packPath, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("pack install failed: %v", err)
	}

	// Remove
	rootCmd3 := newTestRootCmd()
	rootCmd3.AddCommand(newPackCmd())
	rootCmd3.SetOut(&bytes.Buffer{})
	rootCmd3.SetArgs([]string{"pack", "remove", "test-org/remove-test", "--root", tmpDir})
	if err := rootCmd3.Execute(); err != nil {
		t.Fatalf("pack remove failed: %v", err)
	}
}
