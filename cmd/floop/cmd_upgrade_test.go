package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/hooks"
)

func TestUpgradeScopeNoInstallation(t *testing.T) {
	dir := t.TempDir()
	hookDir := filepath.Join(dir, ".claude", "hooks")

	result, err := upgradeScope("test", hookDir, dir, hooks.ScopeProject, false, 2000, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when no scripts installed")
	}
}

func TestUpgradeScopeUpToDate(t *testing.T) {
	dir := t.TempDir()
	hookDir := filepath.Join(dir, ".claude", "hooks")

	// Extract scripts with current version
	_, err := hooks.ExtractScripts(hookDir, version, 2000)
	if err != nil {
		t.Fatalf("failed to extract scripts: %v", err)
	}

	// Also create settings.json so ConfigurePlatform works
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}

	result, err := upgradeScope("test", hookDir, dir, hooks.ScopeProject, false, 2000, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["status"] != "up_to_date" {
		t.Errorf("expected status=up_to_date, got %v", result["status"])
	}
}

func TestUpgradeScopeStale(t *testing.T) {
	dir := t.TempDir()
	hookDir := filepath.Join(dir, ".claude", "hooks")

	// Extract scripts with an old version
	_, err := hooks.ExtractScripts(hookDir, "0.0.1", 2000)
	if err != nil {
		t.Fatalf("failed to extract scripts: %v", err)
	}

	// Create .claude dir for settings
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}

	result, err := upgradeScope("test", hookDir, dir, hooks.ScopeProject, false, 2000, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["status"] != "upgraded" {
		t.Errorf("expected status=upgraded, got %v", result["status"])
	}

	// Verify scripts now have current version
	installed, _ := hooks.InstalledScripts(hookDir)
	for _, script := range installed {
		ver, _ := hooks.ScriptVersion(script)
		if ver != version {
			t.Errorf("script %s has version %s, expected %s", filepath.Base(script), ver, version)
		}
	}
}

func TestUpgradeScopeForce(t *testing.T) {
	dir := t.TempDir()
	hookDir := filepath.Join(dir, ".claude", "hooks")

	// Extract scripts with current version
	_, err := hooks.ExtractScripts(hookDir, version, 2000)
	if err != nil {
		t.Fatalf("failed to extract scripts: %v", err)
	}

	// Create .claude dir for settings
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}

	// Force should upgrade even when versions match
	result, err := upgradeScope("test", hookDir, dir, hooks.ScopeProject, true, 2000, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["status"] != "upgraded" {
		t.Errorf("expected status=upgraded with --force, got %v", result["status"])
	}
}

func TestUpgradeScopeTokenBudget(t *testing.T) {
	dir := t.TempDir()
	hookDir := filepath.Join(dir, ".claude", "hooks")

	// Extract with old budget
	_, err := hooks.ExtractScripts(hookDir, "0.0.1", 1000)
	if err != nil {
		t.Fatalf("failed to extract scripts: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}

	// Upgrade with new budget
	_, err = upgradeScope("test", hookDir, dir, hooks.ScopeProject, false, 3000, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify new budget in script
	content, err := os.ReadFile(filepath.Join(hookDir, "floop-session-start.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "--token-budget 3000") {
		t.Error("expected token budget 3000 in upgraded script")
	}
}
