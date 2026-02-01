package models

import (
	"path/filepath"
	"strings"
	"time"
)

// ContextSnapshot captures the environment at a point in time
type ContextSnapshot struct {
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`

	// Repository info
	Repo     string `json:"repo,omitempty" yaml:"repo,omitempty"`
	RepoRoot string `json:"repo_root,omitempty" yaml:"repo_root,omitempty"`
	Branch   string `json:"branch,omitempty" yaml:"branch,omitempty"`

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
	Environment string `json:"environment,omitempty" yaml:"environment,omitempty"` // dev, staging, prod

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
