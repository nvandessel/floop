package main

import (
	"bytes"
	"testing"
)

func TestNewDetectCorrectionCmd(t *testing.T) {
	cmd := newDetectCorrectionCmd()
	if cmd.Use != "detect-correction" {
		t.Errorf("Use = %q, want %q", cmd.Use, "detect-correction")
	}

	// Check required flags exist
	promptFlag := cmd.Flags().Lookup("prompt")
	if promptFlag == nil {
		t.Error("missing --prompt flag")
	}
	dryRunFlag := cmd.Flags().Lookup("dry-run")
	if dryRunFlag == nil {
		t.Error("missing --dry-run flag")
	}
}

func TestDetectCorrectionCmdNoPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Running detect-correction with no prompt should return without error
	// and output detected=false in JSON mode
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDetectCorrectionCmd())
	rootCmd.SetArgs([]string{"detect-correction", "--json", "--root", tmpDir})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetectCorrectionCmdNonCorrectionPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// A prompt that doesn't match correction patterns should return detected=false
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDetectCorrectionCmd())
	rootCmd.SetArgs([]string{
		"detect-correction",
		"--prompt", "Hello, how are you today?",
		"--json",
		"--root", tmpDir,
	})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetectCorrectionCmdCorrectionPrompt(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	// A prompt that matches correction patterns
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDetectCorrectionCmd())
	rootCmd.SetArgs([]string{
		"detect-correction",
		"--prompt", "No, don't use fmt.Println, use slog instead",
		"--json",
		"--root", tmpDir,
	})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("detect-correction failed: %v", err)
	}
}

func TestDetectCorrectionCmdDryRun(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDetectCorrectionCmd())
	rootCmd.SetArgs([]string{
		"detect-correction",
		"--prompt", "No, don't use fmt.Println, use slog instead",
		"--dry-run",
		"--json",
		"--root", tmpDir,
	})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("detect-correction --dry-run failed: %v", err)
	}
}

func TestDetectCorrectionCmdWithLongPrompt(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDetectCorrectionCmd())
	rootCmd.SetArgs([]string{
		"detect-correction",
		"--prompt", "No, don't use os.path, use pathlib.Path instead. Always prefer pathlib for file operations.",
		"--json",
		"--root", tmpDir,
	})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("detect-correction with long prompt failed: %v", err)
	}
}

func TestDetectCorrectionCmdText(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newDetectCorrectionCmd())
	rootCmd.SetArgs([]string{
		"detect-correction",
		"--prompt", "No, don't use fmt.Println, use slog instead",
		"--root", tmpDir,
	})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("detect-correction text failed: %v", err)
	}
}
