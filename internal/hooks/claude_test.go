package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestClaudePlatformGenerateHookConfigProject(t *testing.T) {
	p := NewClaudePlatform()
	hookDir := "/project/.claude/hooks"

	config, err := p.GenerateHookConfig(nil, ScopeProject, hookDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hooks to be a map")
	}

	// Verify SessionStart uses native Go command
	sessionStart, ok := hooks["SessionStart"].([]interface{})
	if !ok {
		t.Fatal("expected SessionStart to be an array")
	}
	if len(sessionStart) != 1 {
		t.Errorf("expected 1 SessionStart entry, got %d", len(sessionStart))
	}
	ssEntry := sessionStart[0].(map[string]interface{})
	ssHooks := ssEntry["hooks"].([]interface{})
	ssCmd := ssHooks[0].(map[string]interface{})["command"].(string)
	if ssCmd != "floop hook session-start" {
		t.Errorf("expected 'floop hook session-start', got: %s", ssCmd)
	}

	// Verify UserPromptSubmit
	userPrompt, ok := hooks["UserPromptSubmit"].([]interface{})
	if !ok {
		t.Fatal("expected UserPromptSubmit to be an array")
	}
	if len(userPrompt) != 1 {
		t.Errorf("expected 1 UserPromptSubmit entry, got %d", len(userPrompt))
	}
	upEntry := userPrompt[0].(map[string]interface{})
	upHooks := upEntry["hooks"].([]interface{})
	if len(upHooks) != 2 {
		t.Errorf("expected 2 UserPromptSubmit hooks (first-prompt + detect-correction), got %d", len(upHooks))
	}
	fpCmd := upHooks[0].(map[string]interface{})["command"].(string)
	if fpCmd != "floop hook first-prompt" {
		t.Errorf("expected 'floop hook first-prompt', got: %s", fpCmd)
	}
	dcCmd := upHooks[1].(map[string]interface{})["command"].(string)
	if dcCmd != "floop hook detect-correction" {
		t.Errorf("expected 'floop hook detect-correction', got: %s", dcCmd)
	}

	// Verify PreToolUse
	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		t.Fatal("expected PreToolUse to be an array")
	}
	if len(preToolUse) != 2 {
		t.Errorf("expected 2 PreToolUse entries (Read + Bash), got %d", len(preToolUse))
	}

	readEntry := preToolUse[0].(map[string]interface{})
	if readEntry["matcher"] != "Read" {
		t.Errorf("expected first matcher='Read', got '%v'", readEntry["matcher"])
	}
	readHooks := readEntry["hooks"].([]interface{})
	readCmd := readHooks[0].(map[string]interface{})["command"].(string)
	if readCmd != "floop hook dynamic-context" {
		t.Errorf("expected 'floop hook dynamic-context', got: %s", readCmd)
	}

	bashEntry := preToolUse[1].(map[string]interface{})
	if bashEntry["matcher"] != "Bash" {
		t.Errorf("expected second matcher='Bash', got '%v'", bashEntry["matcher"])
	}
}

func TestClaudePlatformGenerateHookConfigGlobal(t *testing.T) {
	p := NewClaudePlatform()
	hookDir := "/home/user/.claude/hooks"

	config, err := p.GenerateHookConfig(nil, ScopeGlobal, hookDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := config["hooks"].(map[string]interface{})
	sessionStart := hooks["SessionStart"].([]interface{})
	ssEntry := sessionStart[0].(map[string]interface{})
	ssHooks := ssEntry["hooks"].([]interface{})
	ssCmd := ssHooks[0].(map[string]interface{})["command"].(string)

	// Both scopes now use the same native Go commands (no path differences)
	if ssCmd != "floop hook session-start" {
		t.Errorf("expected 'floop hook session-start', got: %s", ssCmd)
	}
}

func TestClaudePlatformGenerateHookConfigMerge(t *testing.T) {
	p := NewClaudePlatform()

	existing := map[string]interface{}{
		"otherSetting": "preserved",
		"hooks": map[string]interface{}{
			"OtherHook": []interface{}{"existing"},
		},
	}

	config, err := p.GenerateHookConfig(existing, ScopeProject, "/project/.claude/hooks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Other setting should be preserved
	if config["otherSetting"] != "preserved" {
		t.Error("expected otherSetting to be preserved")
	}

	// All hook types should exist
	hooks := config["hooks"].(map[string]interface{})
	if hooks["OtherHook"] == nil {
		t.Error("expected OtherHook to be preserved")
	}
	if hooks["SessionStart"] == nil {
		t.Error("expected SessionStart to be added")
	}
	if hooks["UserPromptSubmit"] == nil {
		t.Error("expected UserPromptSubmit to be added")
	}
	if hooks["PreToolUse"] == nil {
		t.Error("expected PreToolUse to be added")
	}
}

func TestClaudePlatformGenerateHookConfigIdempotent(t *testing.T) {
	p := NewClaudePlatform()
	hookDir := "/project/.claude/hooks"

	// Generate config twice
	config1, err := p.GenerateHookConfig(nil, ScopeProject, hookDir)
	if err != nil {
		t.Fatalf("first generate failed: %v", err)
	}

	config2, err := p.GenerateHookConfig(config1, ScopeProject, hookDir)
	if err != nil {
		t.Fatalf("second generate failed: %v", err)
	}

	hooks := config2["hooks"].(map[string]interface{})

	// Should not duplicate entries
	sessionStart := hooks["SessionStart"].([]interface{})
	if len(sessionStart) != 1 {
		t.Errorf("idempotent merge failed: expected 1 SessionStart entry, got %d", len(sessionStart))
	}

	preToolUse := hooks["PreToolUse"].([]interface{})
	if len(preToolUse) != 2 {
		t.Errorf("idempotent merge failed: expected 2 PreToolUse entries, got %d", len(preToolUse))
	}
}

func TestClaudePlatformGenerateHookConfigPreservesNonFloop(t *testing.T) {
	p := NewClaudePlatform()

	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Read",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "other-tool check",
						},
					},
				},
			},
		},
	}

	config, err := p.GenerateHookConfig(existing, ScopeProject, "/project/.claude/hooks")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := config["hooks"].(map[string]interface{})
	preToolUse := hooks["PreToolUse"].([]interface{})

	// Should have the original non-floop entry + 2 floop entries
	if len(preToolUse) != 3 {
		t.Errorf("expected 3 PreToolUse entries (1 non-floop + 2 floop), got %d", len(preToolUse))
	}

	// First entry should be the preserved non-floop one
	firstEntry := preToolUse[0].(map[string]interface{})
	firstHooks := firstEntry["hooks"].([]interface{})
	firstCmd := firstHooks[0].(map[string]interface{})["command"].(string)
	if firstCmd != "other-tool check" {
		t.Errorf("expected preserved non-floop command, got: %s", firstCmd)
	}
}

func TestClaudePlatformWriteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	p := NewClaudePlatform()

	config := map[string]interface{}{
		"key": "value",
	}

	err := p.WriteConfig(tmpDir, config)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

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

	// Config with floop hooks in SessionStart
	floopConfig := `{
		"hooks": {
			"SessionStart": [
				{
					"hooks": [
						{"type": "command", "command": "/home/user/.claude/hooks/floop-session-start.sh"}
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
		t.Error("expected true when floop hooks are present in SessionStart")
	}

	// Config with native floop hook commands
	floopConfig2 := `{
		"hooks": {
			"PreToolUse": [
				{
					"matcher": "Read",
					"hooks": [
						{"type": "command", "command": "floop hook dynamic-context"}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(configPath, []byte(floopConfig2), 0600); err != nil {
		t.Fatal(err)
	}

	has, err = p.HasFloopHook(tmpDir)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !has {
		t.Error("expected true when floop hooks are present in PreToolUse")
	}
}
