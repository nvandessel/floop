package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	// Should be empty initially
	if len(reg.All()) != 0 {
		t.Errorf("expected empty registry, got %d platforms", len(reg.All()))
	}

	// Register a platform
	p := NewClaudePlatform()
	reg.Register(p)

	// Should have one platform
	all := reg.All()
	if len(all) != 1 {
		t.Errorf("expected 1 platform, got %d", len(all))
	}

	// Get by name
	got := reg.Get("Claude Code")
	if got == nil {
		t.Error("expected to find Claude Code platform")
	}
	if got.Name() != "Claude Code" {
		t.Errorf("expected name 'Claude Code', got '%s'", got.Name())
	}

	// Get non-existent
	notFound := reg.Get("NonExistent")
	if notFound != nil {
		t.Error("expected nil for non-existent platform")
	}
}

func TestRegistryDetectPlatforms(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	reg := NewRegistry()
	reg.Register(NewClaudePlatform())

	// No .claude directory - should not detect
	detected := reg.DetectPlatforms(tmpDir)
	if len(detected) != 0 {
		t.Errorf("expected 0 detected platforms, got %d", len(detected))
	}

	// Create .claude directory
	if err := os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}

	// Should now detect Claude Code
	detected = reg.DetectPlatforms(tmpDir)
	if len(detected) != 1 {
		t.Errorf("expected 1 detected platform, got %d", len(detected))
	}
	if detected[0].Name() != "Claude Code" {
		t.Errorf("expected 'Claude Code', got '%s'", detected[0].Name())
	}
}

func TestConfigurePlatform(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .claude directory
	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatal(err)
	}

	p := NewClaudePlatform()

	// First configuration - should create
	result := ConfigurePlatform(p, tmpDir)
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
	if !result.Created {
		t.Error("expected Created=true for new config")
	}
	if result.Skipped {
		t.Error("expected Skipped=false")
	}

	// Verify file was created
	configPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected settings.json to be created")
	}

	// Second configuration - should skip
	result2 := ConfigurePlatform(p, tmpDir)
	if result2.Error != nil {
		t.Errorf("unexpected error: %v", result2.Error)
	}
	if !result2.Skipped {
		t.Error("expected Skipped=true for second configure")
	}
	if result2.SkipReason != "floop hooks already configured" {
		t.Errorf("unexpected skip reason: %s", result2.SkipReason)
	}
}

func TestConfigureAllDetected(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .claude directory
	if err := os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	reg.Register(NewClaudePlatform())

	results := reg.ConfigureAllDetected(tmpDir)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if results[0].Platform != "Claude Code" {
		t.Errorf("expected platform 'Claude Code', got '%s'", results[0].Platform)
	}
	if results[0].Error != nil {
		t.Errorf("unexpected error: %v", results[0].Error)
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Default registry should have Claude Code registered
	p := DefaultRegistry.Get("Claude Code")
	if p == nil {
		t.Error("expected Claude Code to be registered in default registry")
	}
}
