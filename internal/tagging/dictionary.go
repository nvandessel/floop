package tagging

import (
	"sort"
	"strings"
)

// Dictionary maps keywords (tokens found in behavior text) to normalized tags.
// Multiple keywords can map to the same tag (e.g., "golang" and "go" both map to "go").
type Dictionary struct {
	entries map[string]string // lowercase keyword → normalized tag
}

// NewDictionary creates a Dictionary with the default keyword→tag mappings.
func NewDictionary() *Dictionary {
	d := &Dictionary{
		entries: make(map[string]string, 200),
	}
	d.loadDefaults()
	return d
}

// Lookup returns the normalized tag for a token, if any.
// Matching is case-insensitive.
func (d *Dictionary) Lookup(token string) (string, bool) {
	tag, ok := d.entries[strings.ToLower(token)]
	return tag, ok
}

// AllTags returns a sorted, deduplicated list of all tag values in the dictionary.
func (d *Dictionary) AllTags() []string {
	seen := make(map[string]bool, len(d.entries))
	for _, tag := range d.entries {
		seen[tag] = true
	}
	tags := make([]string, 0, len(seen))
	for tag := range seen {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// add registers one or more keywords that map to the same tag.
func (d *Dictionary) add(tag string, keywords ...string) {
	for _, kw := range keywords {
		d.entries[strings.ToLower(kw)] = tag
	}
}

// loadDefaults populates the dictionary with the standard keyword→tag mappings.
func (d *Dictionary) loadDefaults() {
	// Languages
	d.add("go", "go", "golang", "go-lang")
	d.add("python", "python", "py", "pip")
	d.add("rust", "rust", "cargo", "rustc")
	d.add("javascript", "javascript", "js", "node", "nodejs")
	d.add("typescript", "typescript", "ts", "tsx")
	d.add("ruby", "ruby", "rb", "gem", "bundler")
	d.add("java", "java", "jvm", "maven", "gradle")
	d.add("sql", "sql", "sqlite", "postgres", "postgresql", "mysql")
	d.add("yaml", "yaml", "yml")
	d.add("json", "json", "jsonl")
	d.add("bash", "bash", "sh", "shell", "zsh")

	// Tools
	d.add("git", "git", "git-town", "gitignore")
	d.add("docker", "docker", "dockerfile", "container", "containers")
	d.add("make", "make", "makefile")
	d.add("npm", "npm", "yarn", "pnpm")
	d.add("cli", "cobra", "cli", "command-line")
	d.add("linting", "golangci-lint", "golangci", "lint", "linter", "linting", "eslint")

	// Testing concepts
	d.add("testing", "test", "tests", "testing", "unittest", "unit-test")
	d.add("tdd", "tdd", "red-green-refactor")

	// Development concepts
	d.add("security", "security", "vulnerability", "owasp", "injection", "xss", "sanitize", "sanitization")
	d.add("error-handling", "error-handling", "error-wrap", "error-wrapping", "errors")
	d.add("concurrency", "concurrency", "goroutine", "goroutines", "mutex", "channel", "channels")
	d.add("logging", "logging", "log", "logs", "logger")
	d.add("configuration", "configuration", "config", "settings", "env", "environment")
	d.add("debugging", "debugging", "debug", "debugger", "breakpoint")
	d.add("refactoring", "refactoring", "refactor", "restructure")

	// Workflow
	d.add("ci", "ci", "ci/cd", "pipeline", "github-actions", "workflow-file")
	d.add("pr", "pr", "pull-request", "pull-requests", "code-review")
	d.add("workflow", "workflow", "process", "procedure")
	d.add("worktree", "worktree", "worktrees")

	// Architecture
	d.add("api", "api", "rest", "endpoint", "endpoints", "http")
	d.add("mcp", "mcp", "mcp-server", "model-context-protocol")
	d.add("database", "database", "db", "schema", "migration", "migrations")
	d.add("filesystem", "filesystem", "file", "files", "directory", "path", "paths")
	d.add("serialization", "serialization", "marshal", "unmarshal", "encode", "decode")

	// Project-specific
	d.add("beads", "beads", "bead", "issue-tracking")
	d.add("floop", "floop", "floop_learn", "floop_active", "floop_list", "feedback-loop")
	d.add("behavior", "behavior", "behaviors", "behaviour")
	d.add("correction", "correction", "corrections")
	d.add("spreading-activation", "spreading", "activation", "spreading-activation")
}
