package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionCmdTextOutput(t *testing.T) {
	origVersion := version
	origCommit := commit
	origDate := date
	t.Cleanup(func() {
		version = origVersion
		commit = origCommit
		date = origDate
	})

	version = "v1.2.3"
	commit = "deadbeef"
	date = "2026-01-15T10:00:00Z"

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newVersionCmd())

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "v1.2.3") {
		t.Errorf("expected version in output, got: %s", output)
	}
	if !strings.Contains(output, "deadbeef") {
		t.Errorf("expected commit in output, got: %s", output)
	}
	if !strings.Contains(output, "2026-01-15T10:00:00Z") {
		t.Errorf("expected date in output, got: %s", output)
	}
}

func TestVersionCmdJSONOutput(t *testing.T) {
	// NOTE: This test mutates package-level vars (version, commit, date).
	// Do not use t.Parallel() — the save/restore via t.Cleanup is not goroutine-safe.
	origVersion := version
	origCommit := commit
	origDate := date
	t.Cleanup(func() {
		version = origVersion
		commit = origCommit
		date = origDate
	})

	version = "v2.0.0"
	commit = "abc1234"
	date = "2026-03-01T00:00:00Z"

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newVersionCmd())

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"version", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("version --json failed: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if result["version"] != "v2.0.0" {
		t.Errorf("version = %q, want %q", result["version"], "v2.0.0")
	}
	if result["commit"] != "abc1234" {
		t.Errorf("commit = %q, want %q", result["commit"], "abc1234")
	}
	if result["date"] != "2026-03-01T00:00:00Z" {
		t.Errorf("date = %q, want %q", result["date"], "2026-03-01T00:00:00Z")
	}
}
