package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClaudePlatform implements the Platform interface for Claude Code.
type ClaudePlatform struct{}

// NewClaudePlatform creates a new Claude Code platform instance.
func NewClaudePlatform() *ClaudePlatform {
	return &ClaudePlatform{}
}

// Name returns the platform name.
func (c *ClaudePlatform) Name() string {
	return "Claude Code"
}

// Detect checks if Claude Code is configured in the project.
// Returns true if .claude/ directory exists.
func (c *ClaudePlatform) Detect(projectRoot string) bool {
	claudeDir := filepath.Join(projectRoot, ".claude")
	info, err := os.Stat(claudeDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ConfigPath returns the path to Claude Code's settings file.
func (c *ClaudePlatform) ConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".claude", "settings.json")
}

// ReadConfig reads the existing Claude Code settings.
func (c *ClaudePlatform) ReadConfig(projectRoot string) (map[string]interface{}, error) {
	configPath := c.ConfigPath(projectRoot)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read settings.json: %w", err)
	}

	// Handle empty file
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse settings.json: %w", err)
	}

	return config, nil
}

// GenerateHookConfig merges floop hooks into existing config.
func (c *ClaudePlatform) GenerateHookConfig(existingConfig map[string]interface{}) (map[string]interface{}, error) {
	config := existingConfig
	if config == nil {
		config = make(map[string]interface{})
	}

	// Build the static behavior injection hook (floop prompt)
	promptHook := map[string]interface{}{
		"type":    "command",
		"command": c.InjectCommand(),
	}

	// Build the dynamic context activation hook (floop activate)
	activateHook := map[string]interface{}{
		"type":    "command",
		"command": c.ActivateCommand(),
	}

	// Get or create hooks section
	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
	}

	// Get or create PreToolUse array
	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		preToolUse = make([]interface{}, 0)
	}

	// Read matcher: static behavior injection via floop prompt
	readMatcher := map[string]interface{}{
		"matcher": "Read",
		"hooks":   []interface{}{promptHook},
	}

	// Bash matcher: dynamic context activation via floop activate
	bashMatcher := map[string]interface{}{
		"matcher": "Bash",
		"hooks":   []interface{}{activateHook},
	}

	// Add both matchers to PreToolUse array
	preToolUse = append(preToolUse, readMatcher, bashMatcher)

	// Update hooks
	hooks["PreToolUse"] = preToolUse
	config["hooks"] = hooks

	return config, nil
}

// WriteConfig writes the configuration to settings.json.
func (c *ClaudePlatform) WriteConfig(projectRoot string, config map[string]interface{}) error {
	configPath := c.ConfigPath(projectRoot)

	// Ensure .claude directory exists
	claudeDir := filepath.Dir(configPath)
	if err := os.MkdirAll(claudeDir, 0700); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Marshal with pretty printing
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write file
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// InjectCommand returns the command to inject behaviors.
func (c *ClaudePlatform) InjectCommand() string {
	return "floop prompt --format markdown"
}

// ActivateCommand returns the command for dynamic context activation.
func (c *ClaudePlatform) ActivateCommand() string {
	return `"$CLAUDE_PROJECT_DIR"/.claude/hooks/dynamic-context.sh`
}

// HasFloopHook checks if floop hooks are already configured.
func (c *ClaudePlatform) HasFloopHook(projectRoot string) (bool, error) {
	config, err := c.ReadConfig(projectRoot)
	if err != nil {
		return false, err
	}
	if config == nil {
		return false, nil
	}

	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		return false, nil
	}

	// Check PreToolUse for floop command
	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		return false, nil
	}

	for _, entry := range preToolUse {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}

		hooksList, ok := entryMap["hooks"].([]interface{})
		if !ok {
			continue
		}

		for _, hook := range hooksList {
			hookMap, ok := hook.(map[string]interface{})
			if !ok {
				continue
			}

			cmd, ok := hookMap["command"].(string)
			if ok && strings.Contains(cmd, "floop") {
				return true, nil
			}
		}
	}

	return false, nil
}
