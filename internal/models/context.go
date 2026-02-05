package models

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectType represents the type of project based on files present
type ProjectType string

const (
	ProjectTypeGo      ProjectType = "go"
	ProjectTypeNode    ProjectType = "node"
	ProjectTypePython  ProjectType = "python"
	ProjectTypeRust    ProjectType = "rust"
	ProjectTypeUnknown ProjectType = "unknown"
)

// ContextSnapshot captures the environment at a point in time
type ContextSnapshot struct {
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`

	// Repository info
	Repo        string      `json:"repo,omitempty" yaml:"repo,omitempty"`
	RepoRoot    string      `json:"repo_root,omitempty" yaml:"repo_root,omitempty"`
	Branch      string      `json:"branch,omitempty" yaml:"branch,omitempty"`
	ProjectType ProjectType `json:"project_type,omitempty" yaml:"project_type,omitempty"`

	// File info
	FilePath     string `json:"file_path,omitempty" yaml:"file_path,omitempty"`
	FileLanguage string `json:"file_language,omitempty" yaml:"file_language,omitempty"`
	FileExt      string `json:"file_ext,omitempty" yaml:"file_ext,omitempty"`

	// Task info
	Task string `json:"task,omitempty" yaml:"task,omitempty"`

	// User info
	User  string   `json:"user,omitempty" yaml:"user,omitempty"`
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Environment
	Environment string `json:"environment,omitempty" yaml:"environment,omitempty"` // dev, staging, prod, ci

	// Custom fields for extensibility
	Custom map[string]interface{} `json:"custom,omitempty" yaml:"custom,omitempty"`
}

// Matches checks if this context matches a 'when' predicate
func (c *ContextSnapshot) Matches(predicate map[string]interface{}) bool {
	for key, required := range predicate {
		actual := c.GetField(key)
		if !matchValue(actual, required) {
			return false
		}
	}
	return true
}

// GetField retrieves a field value by name (exported for use by activation package)
func (c *ContextSnapshot) GetField(key string) interface{} {
	switch key {
	case "repo":
		return c.Repo
	case "branch":
		return c.Branch
	case "project_type":
		return string(c.ProjectType)
	case "file_path", "file.path":
		return c.FilePath
	case "file_language", "file.language", "language":
		return c.FileLanguage
	case "file_ext", "file.ext", "ext":
		return c.FileExt
	case "task":
		return c.Task
	case "user":
		return c.User
	case "environment", "env":
		return c.Environment
	default:
		if c.Custom != nil {
			return c.Custom[key]
		}
		return nil
	}
}

// matchValue checks if an actual value matches a required value
// Supports: exact match, array membership, glob patterns
func matchValue(actual interface{}, required interface{}) bool {
	if actual == nil {
		return false
	}

	actualStr, actualIsStr := actual.(string)

	switch req := required.(type) {
	case string:
		if !actualIsStr {
			return false
		}
		// Support glob patterns
		if strings.Contains(req, "*") {
			matched, _ := filepath.Match(req, actualStr)
			return matched
		}
		return actualStr == req

	case []interface{}:
		// Value must be one of the options
		for _, option := range req {
			if optStr, ok := option.(string); ok && actualIsStr {
				if actualStr == optStr {
					return true
				}
			}
		}
		return false

	case []string:
		if !actualIsStr {
			return false
		}
		for _, option := range req {
			if actualStr == option {
				return true
			}
		}
		return false

	default:
		return actual == required
	}
}

// InferLanguage attempts to determine language from file extension
func InferLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp":
		return "cpp"
	case ".md":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	default:
		return ""
	}
}

// InferLanguageFromContent attempts to detect language from file content.
// It checks shebang lines and common language patterns.
func InferLanguageFromContent(content string) string {
	if content == "" {
		return ""
	}

	// Check shebang first
	if strings.HasPrefix(content, "#!") {
		firstLine := strings.Split(content, "\n")[0]
		if strings.Contains(firstLine, "python") {
			return "python"
		}
		if strings.Contains(firstLine, "node") {
			return "javascript"
		}
		// Check for shell: /bin/bash, /bin/sh, /usr/bin/env bash, /usr/bin/env sh
		if strings.Contains(firstLine, "bash") ||
			strings.Contains(firstLine, "/sh") ||
			strings.HasSuffix(firstLine, " sh") {
			return "shell"
		}
		if strings.Contains(firstLine, "ruby") {
			return "ruby"
		}
		if strings.Contains(firstLine, "perl") {
			return "perl"
		}
	}

	// Check patterns line by line
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments for pattern detection
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Go: package declaration
		if strings.HasPrefix(trimmed, "package ") {
			return "go"
		}

		// Rust: fn main or fn keyword with braces
		if strings.HasPrefix(trimmed, "fn ") {
			return "rust"
		}

		// Python: def or class
		if strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "class ") {
			return "python"
		}

		// JavaScript: function, const, let, var keywords
		if strings.HasPrefix(trimmed, "function ") ||
			strings.HasPrefix(trimmed, "const ") ||
			strings.HasPrefix(trimmed, "let ") ||
			strings.HasPrefix(trimmed, "var ") {
			return "javascript"
		}
	}

	return ""
}

// InferProjectType detects project type from root directory
func InferProjectType(rootDir string) ProjectType {
	// Check for go.mod (Go project)
	if _, err := os.Stat(filepath.Join(rootDir, "go.mod")); err == nil {
		return ProjectTypeGo
	}

	// Check for Cargo.toml (Rust project)
	if _, err := os.Stat(filepath.Join(rootDir, "Cargo.toml")); err == nil {
		return ProjectTypeRust
	}

	// Check for package.json (Node project)
	if _, err := os.Stat(filepath.Join(rootDir, "package.json")); err == nil {
		return ProjectTypeNode
	}

	// Check for Python project markers
	if _, err := os.Stat(filepath.Join(rootDir, "pyproject.toml")); err == nil {
		return ProjectTypePython
	}
	if _, err := os.Stat(filepath.Join(rootDir, "requirements.txt")); err == nil {
		return ProjectTypePython
	}
	if _, err := os.Stat(filepath.Join(rootDir, "setup.py")); err == nil {
		return ProjectTypePython
	}

	return ProjectTypeUnknown
}
