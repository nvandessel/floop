package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// backupOutputPath returns a valid backup output path inside the tmpDir's .floop/backups/.
func backupOutputPath(t *testing.T, tmpDir, filename string) string {
	t.Helper()
	dir := filepath.Join(tmpDir, ".floop", "backups")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("failed to create backups dir: %v", err)
	}
	return filepath.Join(dir, filename)
}

func TestBackupCmdIntegration(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "test-backup.json")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--no-compress", "--output", outputPath, "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("backup file not created: %v", err)
	}
}

func TestBackupCmdJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "test-backup.json")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--no-compress", "--output", outputPath, "--json", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup --json failed: %v", err)
	}
}

func TestBackupCmdCompressed(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "test-backup.json.gz")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--output", outputPath, "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup compressed failed: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("compressed backup file not created: %v", err)
	}
}

func TestBackupListCmdIntegration(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	// Backup list command uses the default backup dir
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "list", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup list failed: %v", err)
	}
}

func TestBackupVerifyCmdIntegration(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "test-backup.json")

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
	rootCmd2.SetArgs([]string{"backup", "verify", outputPath, "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("backup verify failed: %v", err)
	}
}

func TestBackupThenRestore(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "test-backup.json")

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"backup", "--no-compress", "--output", outputPath, "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newRestoreFromBackupCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{"restore-backup", outputPath, "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("restore-backup failed: %v", err)
	}
}

func TestBackupVerifyCmdJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)
	outputPath := backupOutputPath(t, tmpDir, "test-backup.json")

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
		t.Fatalf("backup verify --json failed: %v", err)
	}
}

func TestRestoreFromBackupCmdNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newRestoreFromBackupCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"restore-backup", "/nonexistent/file.json", "--root", tmpDir})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent backup file")
	}
}
