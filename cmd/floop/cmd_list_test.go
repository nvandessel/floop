package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/models"
)

func TestListCorrectionsWithData(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Create .floop directory and write a correction
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("failed to create .floop: %v", err)
	}

	correction := models.Correction{
		AgentAction:     "used os.path",
		CorrectedAction: "use pathlib.Path",
		Timestamp:       time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Context: models.ContextSnapshot{
			FilePath: "script.py",
		},
	}
	data, _ := json.Marshal(correction)
	if err := os.WriteFile(filepath.Join(floopDir, "corrections.jsonl"), data, 0600); err != nil {
		t.Fatalf("failed to write corrections: %v", err)
	}

	// Test human output
	var buf bytes.Buffer
	err := listCorrections(&buf, tmpDir, false)
	if err != nil {
		t.Fatalf("listCorrections failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "used os.path") {
		t.Errorf("expected correction content in output, got: %s", output)
	}
	if !strings.Contains(output, "use pathlib.Path") {
		t.Errorf("expected corrected action in output, got: %s", output)
	}

	// Test JSON output
	var jsonBuf bytes.Buffer
	err = listCorrections(&jsonBuf, tmpDir, true)
	if err != nil {
		t.Fatalf("listCorrections JSON failed: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(jsonBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	countVal, ok := result["count"].(float64)
	if !ok {
		t.Fatal("expected 'count' field to be a number")
	}
	if countVal != 1 {
		t.Errorf("expected count=1, got %v", result["count"])
	}
}

func TestListCorrectionsEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	os.MkdirAll(floopDir, 0700)

	// Write empty corrections file
	os.WriteFile(filepath.Join(floopDir, "corrections.jsonl"), []byte(""), 0600)

	var buf bytes.Buffer
	err := listCorrections(&buf, tmpDir, false)
	if err != nil {
		t.Fatalf("listCorrections on empty file failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No corrections") {
		t.Errorf("expected 'No corrections' message, got: %s", buf.String())
	}
}

func TestListCmdLocalAndGlobalConflict(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())
	rootCmd.SetArgs([]string{"list", "--local", "--global", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for --local and --global conflict")
	}
	if !strings.Contains(err.Error(), "cannot specify both") {
		t.Errorf("expected conflict error, got: %v", err)
	}
}

func TestListCmdLocalAndAllConflict(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())
	rootCmd.SetArgs([]string{"list", "--local", "--all", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for --local and --all conflict")
	}
	if !strings.Contains(err.Error(), "cannot specify both") {
		t.Errorf("expected conflict error, got: %v", err)
	}
}

func TestListCmdCorrectionsIgnoresScope(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Create .floop dir
	floopDir := filepath.Join(tmpDir, ".floop")
	os.MkdirAll(floopDir, 0700)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())
	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)

	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)
	rootCmd.SetArgs([]string{"list", "--corrections", "--global", "--root", tmpDir})

	// Should succeed with a warning about scope flags being ignored
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list --corrections --global failed: %v", err)
	}
	if !strings.Contains(errBuf.String(), "scope flags are ignored") {
		t.Errorf("expected scope warning, got stderr: %s", errBuf.String())
	}
}

func TestListCmdLocalNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"list", "--local", "--root", tmpDir})

	// Should succeed gracefully with message
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list --local failed: %v", err)
	}
	if !strings.Contains(buf.String(), "not initialized") {
		t.Errorf("expected 'not initialized' message, got: %s", buf.String())
	}
}

func TestListCmdLocalNotInitializedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"list", "--local", "--json", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list --local --json failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["error"] == nil {
		t.Error("expected error field in JSON output")
	}
}

func TestListCmdBothNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"list", "--root", tmpDir})

	// Should succeed with message
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No .floop stores initialized") {
		t.Errorf("expected 'No .floop stores initialized' message, got: %s", buf.String())
	}
}

func TestListCmdWithInitializedLocalStore(t *testing.T) {
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
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newListCmd())
	var buf bytes.Buffer
	rootCmd2.SetOut(&buf)
	rootCmd2.SetArgs([]string{"list", "--local", "--root", tmpDir})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("list --local failed: %v", err)
	}
}
