package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HookScope determines how script paths are generated in the config.
type HookScope int

const (
	// ScopeGlobal generates absolute paths (for ~/.claude/settings.json).
	ScopeGlobal HookScope = iota
	// ScopeProject generates $CLAUDE_PROJECT_DIR-relative paths (for .claude/settings.json).
	ScopeProject
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

// GenerateHookConfig merges floop hooks into existing config, generating entries
// for all 3 event types (SessionStart, UserPromptSubmit, PreToolUse).
// It removes any existing floop entries first (idempotent merge).
//
// Hook commands are native Go subcommands ("floop hook <name>") that read
// JSON from stdin and call internal packages directly — no bash/jq required.
// The hookDir parameter is unused (retained for interface compatibility).
func (c *ClaudePlatform) GenerateHookConfig(existingConfig map[string]interface{}, scope HookScope, hookDir string) (map[string]interface{}, error) {
	config := existingConfig
	if config == nil {
		config = make(map[string]interface{})
	}

	// Get or create hooks section
	hooksSection, ok := config["hooks"].(map[string]interface{})
	if !ok {
		hooksSection = make(map[string]interface{})
	}

	// Remove existing floop entries for idempotent merge
	hooksSection = removeFloopEntries(hooksSection)

	// SessionStart: inject behaviors at session start
	sessionStartEntry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "floop hook session-start",
			},
		},
	}
	sessionStart := getOrCreateEventArray(hooksSection, "SessionStart")
	hooksSection["SessionStart"] = append(sessionStart, sessionStartEntry)

	// UserPromptSubmit: first-prompt fallback + correction detection
	userPromptEntry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "floop hook first-prompt",
			},
			map[string]interface{}{
				"type":    "command",
				"command": "floop hook detect-correction",
			},
		},
	}
	userPrompt := getOrCreateEventArray(hooksSection, "UserPromptSubmit")
	hooksSection["UserPromptSubmit"] = append(userPrompt, userPromptEntry)

	// PreToolUse: dynamic context injection for Read and Bash
	readMatcher := map[string]interface{}{
		"matcher": "Read",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "floop hook dynamic-context",
			},
		},
	}
	bashMatcher := map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "floop hook dynamic-context",
			},
		},
	}
	preToolUse := getOrCreateEventArray(hooksSection, "PreToolUse")
	hooksSection["PreToolUse"] = append(preToolUse, readMatcher, bashMatcher)

	config["hooks"] = hooksSection
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

	// Append newline for POSIX compliance
	data = append(data, '\n')

	// Write file
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	return nil
}

// HasFloopHook checks if floop hooks are already configured by scanning
// all event types for commands containing "floop".
func (c *ClaudePlatform) HasFloopHook(projectRoot string) (bool, error) {
	config, err := c.ReadConfig(projectRoot)
	if err != nil {
		return false, err
	}
	if config == nil {
		return false, nil
	}

	hooksSection, ok := config["hooks"].(map[string]interface{})
	if !ok {
		return false, nil
	}

	// Check all event types
	for _, eventType := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse"} {
		if containsFloopCommand(hooksSection, eventType) {
			return true, nil
		}
	}

	return false, nil
}

// containsFloopCommand checks if any hook command in the given event type contains "floop".
func containsFloopCommand(hooksSection map[string]interface{}, eventType string) bool {
	entries, ok := hooksSection[eventType].([]interface{})
	if !ok {
		return false
	}

	for _, entry := range entries {
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
				return true
			}
		}
	}

	return false
}

// removeFloopEntries removes all floop-related entries from the hooks config.
// Non-floop entries are preserved.
func removeFloopEntries(hooksSection map[string]interface{}) map[string]interface{} {
	for _, eventType := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse"} {
		entries, ok := hooksSection[eventType].([]interface{})
		if !ok {
			continue
		}

		var kept []interface{}
		for _, entry := range entries {
			entryMap, ok := entry.(map[string]interface{})
			if !ok {
				kept = append(kept, entry)
				continue
			}

			if !entryHasFloopCommand(entryMap) {
				kept = append(kept, entry)
			}
		}

		if len(kept) > 0 {
			hooksSection[eventType] = kept
		} else {
			delete(hooksSection, eventType)
		}
	}

	return hooksSection
}

// entryHasFloopCommand checks if a hook entry contains any floop commands.
func entryHasFloopCommand(entry map[string]interface{}) bool {
	hooksList, ok := entry["hooks"].([]interface{})
	if !ok {
		return false
	}

	for _, hook := range hooksList {
		hookMap, ok := hook.(map[string]interface{})
		if !ok {
			continue
		}

		cmd, ok := hookMap["command"].(string)
		if ok && strings.Contains(cmd, "floop") {
			return true
		}
	}

	return false
}

// getOrCreateEventArray gets or creates an event array from the hooks section.
func getOrCreateEventArray(hooksSection map[string]interface{}, eventType string) []interface{} {
	arr, ok := hooksSection[eventType].([]interface{})
	if !ok {
		return make([]interface{}, 0)
	}
	return arr
}
