package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  string
	}{
		{"zero bytes", 0, "0B"},
		{"small bytes", 512, "512B"},
		{"one KB", 1024, "1.0KB"},
		{"several KB", 5120, "5.0KB"},
		{"one MB", 1024 * 1024, "1.0MB"},
		{"several MB", 5 * 1024 * 1024, "5.0MB"},
		{"one GB", 1024 * 1024 * 1024, "1.0GB"},
		{"fractional KB", 1536, "1.5KB"},
		{"fractional MB", 1572864, "1.5MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewBackupListCmd(t *testing.T) {
	cmd := newBackupListCmd()
	if cmd.Use != "list" {
		t.Errorf("Use = %q, want %q", cmd.Use, "list")
	}
}

func TestNewBackupVerifyCmd(t *testing.T) {
	cmd := newBackupVerifyCmd()
	if cmd.Use != "verify <file>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "verify <file>")
	}
}

func TestBackupListWithBackups(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "list-test-backup.json")

	// Create a backup first
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--no-compress", "--output", outputPath, "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	// Override HOME to point backup list at the right default dir
	// The backup list uses backup.DefaultBackupDir() which is HOME-based
	// Create a backup in the global default dir too
	globalBackupDir := filepath.Join(tmpDir, "home", ".floop", "backups")
	if err := os.MkdirAll(globalBackupDir, 0700); err != nil {
		t.Fatalf("failed to create global backups dir: %v", err)
	}
	// Copy the backup to the global backup dir
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalBackupDir, "list-test-backup.json"), data, 0644); err != nil {
		t.Fatalf("failed to write backup: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newBackupCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"backup", "list", "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("backup list failed: %v", err)
	}
}

func TestBackupListJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "list", "--json", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup list --json failed: %v", err)
	}
}

func TestBackupVerifyV1(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "v1-test-backup.json")

	// Create an uncompressed (v1) backup
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--no-compress", "--output", outputPath, "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	// Verify v1 backup (text mode)
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newBackupCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"backup", "verify", outputPath, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("backup verify v1 failed: %v", err)
	}
}

func TestBackupVerifyV2(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "v2-test-backup.json.gz")

	// Create a compressed (v2) backup
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--output", outputPath, "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup compressed failed: %v", err)
	}

	// Verify v2 backup (text mode)
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newBackupCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"backup", "verify", outputPath, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("backup verify v2 failed: %v", err)
	}
}

func TestBackupVerifyV2JSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "v2-json-test-backup.json.gz")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--output", outputPath, "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup compressed failed: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newBackupCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"backup", "verify", "--json", outputPath, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("backup verify v2 --json failed: %v", err)
	}
}

func TestBackupVerifyV1JSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "v1-json-test-backup.json")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--no-compress", "--output", outputPath, "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newBackupCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"backup", "verify", "--json", outputPath, "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("backup verify v1 --json failed: %v", err)
	}
}

func TestNewBackupCmd(t *testing.T) {
	cmd := newBackupCmd()
	if cmd.Use != "backup" {
		t.Errorf("Use = %q, want %q", cmd.Use, "backup")
	}

	// Verify flags exist
	for _, flag := range []string{"output", "no-compress"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}

	// Verify subcommands exist
	subCmds := cmd.Commands()
	names := make(map[string]bool)
	for _, sub := range subCmds {
		names[sub.Use] = true
	}
	if !names["list"] {
		t.Error("missing 'list' subcommand")
	}
	if !names["verify <file>"] {
		t.Error("missing 'verify' subcommand")
	}
}
