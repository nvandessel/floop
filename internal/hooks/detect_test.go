package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAll(t *testing.T) {
	tmpDir := t.TempDir()

	// No platforms - should return empty
	results := DetectAll(tmpDir)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}

	// Create .claude directory
	if err := os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}

	results = DetectAll(tmpDir)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "Claude Code" {
		t.Errorf("expected 'Claude Code', got '%s'", results[0].Name)
	}
	if results[0].Platform == nil {
		t.Error("expected Platform to be set")
	}
}

func TestDetectAllWithStatus(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .claude directory
	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	reg.Register(NewClaudePlatform())

	// Without hooks
	results := reg.DetectAllWithStatus(tmpDir)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].HasHooks {
		t.Error("expected HasHooks=false")
	}

	// Add floop hooks
	configPath := filepath.Join(claudeDir, "settings.json")
	floopConfig := `{
		"hooks": {
			"PreToolUse": [
				{
					"matcher": "Read",
					"hooks": [{"type": "command", "command": "floop prompt"}]
				}
			]
		}
	}`
	if err := os.WriteFile(configPath, []byte(floopConfig), 0600); err != nil {
		t.Fatal(err)
	}

	results = reg.DetectAllWithStatus(tmpDir)
	if !results[0].HasHooks {
		t.Error("expected HasHooks=true")
	}
}

func TestEnsureClaudeDir(t *testing.T) {
	tmpDir := t.TempDir()

	claudeDir := filepath.Join(tmpDir, ".claude")

	// Should not exist yet
	if _, err := os.Stat(claudeDir); !os.IsNotExist(err) {
		t.Error("expected .claude to not exist")
	}

	// Create it
	if err := EnsureClaudeDir(tmpDir); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should exist now
	info, err := os.Stat(claudeDir)
	if err != nil {
		t.Errorf("expected .claude to exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected .claude to be a directory")
	}

	// Should be idempotent
	if err := EnsureClaudeDir(tmpDir); err != nil {
		t.Errorf("unexpected error on second call: %v", err)
	}
}

func TestPlatformNames(t *testing.T) {
	names := PlatformNames()

	// Should include Claude Code
	found := false
	for _, name := range names {
		if name == "Claude Code" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'Claude Code' in platform names")
	}
}

func TestGetPlatformByName(t *testing.T) {
	p := GetPlatformByName("Claude Code")
	if p == nil {
		t.Error("expected to find Claude Code platform")
	}

	p = GetPlatformByName("NonExistent")
	if p != nil {
		t.Error("expected nil for non-existent platform")
	}
}
