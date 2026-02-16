package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/hooks"
)

func TestUpgradeScopeNoInstallation(t *testing.T) {
	dir := t.TempDir()

	result, err := upgradeScope("test", dir, hooks.ScopeProject, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when no hooks installed")
	}
}

func TestUpgradeScopeNativeUpToDate(t *testing.T) {
	dir := t.TempDir()

	// Create settings.json with native floop hook commands
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatal(err)
	}

	p := hooks.NewClaudePlatform()
	config, _ := p.GenerateHookConfig(nil, hooks.ScopeProject, "")
	if err := p.WriteConfig(dir, config); err != nil {
		t.Fatal(err)
	}

	result, err := upgradeScope("test", dir, hooks.ScopeProject, false, true)
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

func TestUpgradeScopeMigratesOldScripts(t *testing.T) {
	dir := t.TempDir()
	hookDir := filepath.Join(dir, ".claude", "hooks")

	// Create old-style .sh scripts to simulate a pre-migration installation
	if err := os.MkdirAll(hookDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"floop-session-start.sh", "floop-first-prompt.sh", "floop-detect-correction.sh", "floop-dynamic-context.sh"} {
		content := "#!/bin/bash\n# version: 0.0.1\nexit 0\n"
		if err := os.WriteFile(filepath.Join(hookDir, name), []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Upgrade should migrate
	result, err := upgradeScope("test", dir, hooks.ScopeProject, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["status"] != "migrated" {
		t.Errorf("expected status=migrated, got %v", result["status"])
	}

	// Verify old .sh scripts were removed
	installed, _ := hooks.InstalledScripts(hookDir)
	if len(installed) != 0 {
		t.Errorf("expected 0 remaining .sh scripts, got %d", len(installed))
	}

	// Verify settings.json now has native commands
	p := hooks.NewClaudePlatform()
	config, err := p.ReadConfig(dir)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	configJSON, _ := json.Marshal(config)
	configStr := string(configJSON)
	if configStr == "" {
		t.Fatal("expected non-empty config")
	}

	has, _ := p.HasFloopHook(dir)
	if !has {
		t.Error("expected HasFloopHook=true after migration")
	}
}

func TestUpgradeScopeForce(t *testing.T) {
	dir := t.TempDir()

	// Create settings.json with native floop hooks
	p := hooks.NewClaudePlatform()
	config, _ := p.GenerateHookConfig(nil, hooks.ScopeProject, "")
	if err := p.WriteConfig(dir, config); err != nil {
		t.Fatal(err)
	}

	// Force should reconfigure even when native hooks exist
	result, err := upgradeScope("test", dir, hooks.ScopeProject, true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["status"] != "reconfigured" {
		t.Errorf("expected status=reconfigured with --force, got %v", result["status"])
	}
}
