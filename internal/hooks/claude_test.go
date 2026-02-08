package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudePlatformName(t *testing.T) {
	p := NewClaudePlatform()
	if p.Name() != "Claude Code" {
		t.Errorf("expected 'Claude Code', got '%s'", p.Name())
	}
}

func TestClaudePlatformDetect(t *testing.T) {
	tmpDir := t.TempDir()
	p := NewClaudePlatform()

	// No .claude directory
	if p.Detect(tmpDir) {
		t.Error("expected Detect=false when .claude doesn't exist")
	}

	// Create .claude as file (not directory)
	filePath := filepath.Join(tmpDir, ".claude")
	if err := os.WriteFile(filePath, []byte("not a dir"), 0600); err != nil {
		t.Fatal(err)
	}
	if p.Detect(tmpDir) {
		t.Error("expected Detect=false when .claude is a file")
	}

	// Remove and create as directory
	os.Remove(filePath)
	if err := os.MkdirAll(filePath, 0700); err != nil {
		t.Fatal(err)
	}
	if !p.Detect(tmpDir) {
		t.Error("expected Detect=true when .claude is a directory")
	}
}

func TestClaudePlatformConfigPath(t *testing.T) {
	p := NewClaudePlatform()
	path := p.ConfigPath("/project")
	expected := filepath.Join("/project", ".claude", "settings.json")
	if path != expected {
		t.Errorf("expected '%s', got '%s'", expected, path)
	}
}

func TestClaudePlatformReadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	p := NewClaudePlatform()

	// No config file - should return nil
	config, err := p.ReadConfig(tmpDir)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if config != nil {
		t.Error("expected nil config when file doesn't exist")
	}

	// Create .claude directory and empty file
	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(configPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	// Empty file - should return nil
	config, err = p.ReadConfig(tmpDir)
	if err != nil {
		t.Errorf("unexpected error for empty file: %v", err)
	}
	if config != nil {
		t.Error("expected nil config for empty file")
	}

	// Valid JSON
	validJSON := `{"key": "value"}`
	if err := os.WriteFile(configPath, []byte(validJSON), 0600); err != nil {
		t.Fatal(err)
	}
	config, err = p.ReadConfig(tmpDir)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if config == nil {
		t.Error("expected non-nil config")
	}
	if config["key"] != "value" {
		t.Errorf("expected key='value', got '%v'", config["key"])
	}

	// Invalid JSON
	if err := os.WriteFile(configPath, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = p.ReadConfig(tmpDir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestClaudePlatformGenerateHookConfig(t *testing.T) {
	p := NewClaudePlatform()

	// From nil config
	config, err := p.GenerateHookConfig(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify structure
	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hooks to be a map")
	}

	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		t.Fatal("expected PreToolUse to be an array")
	}

	if len(preToolUse) != 2 {
		t.Errorf("expected 2 PreToolUse entries (Read + Bash), got %d", len(preToolUse))
	}

	// Verify Read matcher entry
	readEntry := preToolUse[0].(map[string]interface{})
	if readEntry["matcher"] != "Read" {
		t.Errorf("expected first matcher='Read', got '%v'", readEntry["matcher"])
	}

	readHooksList := readEntry["hooks"].([]interface{})
	readHook := readHooksList[0].(map[string]interface{})
	if readHook["type"] != "command" {
		t.Errorf("expected type='command', got '%v'", readHook["type"])
	}
	if !strings.Contains(readHook["command"].(string), "floop") {
		t.Errorf("expected Read command to contain 'floop', got '%v'", readHook["command"])
	}

	// Verify Bash matcher entry
	bashEntry := preToolUse[1].(map[string]interface{})
	if bashEntry["matcher"] != "Bash" {
		t.Errorf("expected second matcher='Bash', got '%v'", bashEntry["matcher"])
	}

	bashHooksList := bashEntry["hooks"].([]interface{})
	bashHook := bashHooksList[0].(map[string]interface{})
	if bashHook["type"] != "command" {
		t.Errorf("expected type='command', got '%v'", bashHook["type"])
	}
	if !strings.Contains(bashHook["command"].(string), "dynamic-context") {
		t.Errorf("expected Bash command to contain 'dynamic-context', got '%v'", bashHook["command"])
	}
}

func TestClaudePlatformGenerateHookConfigMerge(t *testing.T) {
	p := NewClaudePlatform()

	// Existing config with other settings
	existing := map[string]interface{}{
		"otherSetting": "preserved",
		"hooks": map[string]interface{}{
			"OtherHook": []interface{}{"existing"},
		},
	}

	config, err := p.GenerateHookConfig(existing)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Other setting should be preserved
	if config["otherSetting"] != "preserved" {
		t.Error("expected otherSetting to be preserved")
	}

	// Both hooks should exist
	hooks := config["hooks"].(map[string]interface{})
	if hooks["OtherHook"] == nil {
		t.Error("expected OtherHook to be preserved")
	}
	if hooks["PreToolUse"] == nil {
		t.Error("expected PreToolUse to be added")
	}
}

func TestClaudePlatformWriteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	p := NewClaudePlatform()

	config := map[string]interface{}{
		"key": "value",
	}

	// Should create .claude directory and write file
	err := p.WriteConfig(tmpDir, config)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify file exists and is valid JSON
	configPath := filepath.Join(tmpDir, ".claude", "settings.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Errorf("failed to read config: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("config is not valid JSON: %v", err)
	}

	if parsed["key"] != "value" {
		t.Errorf("expected key='value', got '%v'", parsed["key"])
	}
}

func TestClaudePlatformInjectCommand(t *testing.T) {
	p := NewClaudePlatform()
	cmd := p.InjectCommand()

	if !strings.Contains(cmd, "floop") {
		t.Error("expected command to contain 'floop'")
	}
	if !strings.Contains(cmd, "prompt") {
		t.Error("expected command to contain 'prompt'")
	}
}

func TestClaudePlatformActivateCommand(t *testing.T) {
	p := NewClaudePlatform()
	cmd := p.ActivateCommand()

	if !strings.Contains(cmd, "dynamic-context") {
		t.Error("expected activate command to contain 'dynamic-context'")
	}
	if !strings.Contains(cmd, "CLAUDE_PROJECT_DIR") {
		t.Error("expected activate command to reference CLAUDE_PROJECT_DIR")
	}
}

func TestClaudePlatformHasFloopHook(t *testing.T) {
	tmpDir := t.TempDir()
	p := NewClaudePlatform()

	// No config - should return false
	has, err := p.HasFloopHook(tmpDir)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if has {
		t.Error("expected false when no config exists")
	}

	// Create config without floop hooks
	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		t.Fatal(err)
	}

	noFloopConfig := `{
		"hooks": {
			"PreToolUse": [
				{
					"matcher": "Read",
					"hooks": [
						{"type": "command", "command": "other-tool"}
					]
				}
			]
		}
	}`
	configPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(configPath, []byte(noFloopConfig), 0600); err != nil {
		t.Fatal(err)
	}

	has, err = p.HasFloopHook(tmpDir)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if has {
		t.Error("expected false when floop hooks not present")
	}

	// Config with floop hooks
	floopConfig := `{
		"hooks": {
			"PreToolUse": [
				{
					"matcher": "Read",
					"hooks": [
						{"type": "command", "command": "floop prompt --format markdown"}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(configPath, []byte(floopConfig), 0600); err != nil {
		t.Fatal(err)
	}

	has, err = p.HasFloopHook(tmpDir)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !has {
		t.Error("expected true when floop hooks are present")
	}
}
