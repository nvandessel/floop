package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry represents a single audit log entry for an MCP tool invocation.
// It captures metadata about the call without including sensitive content.
type AuditEntry struct {
	Timestamp  time.Time         `json:"timestamp"`
	Tool       string            `json:"tool"`
	Scope      string            `json:"scope"` // "local" or "global"
	DurationMs int64             `json:"duration_ms"`
	Status     string            `json:"status"` // "success" or "error"
	Error      string            `json:"error,omitempty"`
	Params     map[string]string `json:"params,omitempty"` // sanitized metadata only
}

// auditFile holds a mutex-protected file handle for writing audit entries.
type auditFile struct {
	mu   sync.Mutex
	file *os.File
}

// AuditLogger writes audit entries to JSONL files, routing to local or global
// log based on entry scope. It is safe for concurrent use. A nil AuditLogger
// is safe to use; all methods are no-ops on nil receiver.
type AuditLogger struct {
	local  *auditFile // project-local audit log (.floop/audit.jsonl under project root)
	global *auditFile // global audit log (.floop/audit.jsonl under home dir)
}

// openAuditFile creates an auditFile writing to .floop/audit.jsonl under
// the given directory. Returns nil if the file cannot be created.
func openAuditFile(dir string) *auditFile {
	path := filepath.Join(dir, ".floop", "audit.jsonl")

	// Ensure directory exists
	auditDir := filepath.Dir(path)
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot create audit log directory %s: %v\n", auditDir, err)
		return nil
	}

	// Open file with restricted permissions
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot open audit log %s: %v\n", path, err)
		return nil
	}

	return &auditFile{file: f}
}

// write appends a JSON-encoded entry as a single line. Safe to call on nil.
func (af *auditFile) write(entry AuditEntry) {
	if af == nil || af.file == nil {
		return
	}

	af.mu.Lock()
	defer af.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return // silently skip malformed entries
	}

	data = append(data, '\n')
	_, _ = af.file.Write(data)
}

// close closes the underlying file. Safe to call on nil.
func (af *auditFile) close() error {
	if af == nil || af.file == nil {
		return nil
	}

	af.mu.Lock()
	defer af.mu.Unlock()

	return af.file.Close()
}

// NewAuditLogger creates an audit logger with separate local and global log files.
// localDir is the project root (writes to localDir/.floop/audit.jsonl) and
// globalDir is the home directory (writes to globalDir/.floop/audit.jsonl).
//
// If either file cannot be created, a warning is printed to stderr and that
// scope's logger is nil (non-fatal). If both fail, returns nil.
func NewAuditLogger(localDir, globalDir string) *AuditLogger {
	local := openAuditFile(localDir)
	global := openAuditFile(globalDir)

	// If both failed, return nil (caller checks for nil)
	if local == nil && global == nil {
		return nil
	}

	return &AuditLogger{
		local:  local,
		global: global,
	}
}

// Log writes an audit entry to the appropriate log file based on the entry's
// Scope field. If Scope is empty or "local", it writes to the local log.
// If Scope is "global", it writes to the global log. Safe to call on nil receiver.
func (a *AuditLogger) Log(entry AuditEntry) {
	if a == nil {
		return
	}

	switch entry.Scope {
	case "global":
		a.global.write(entry)
	default:
		// Default to local for empty or "local" scope
		a.local.write(entry)
	}
}

// Close closes both audit log files. Safe to call on nil receiver.
func (a *AuditLogger) Close() error {
	if a == nil {
		return nil
	}

	var firstErr error
	if err := a.local.close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := a.global.close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// sanitizeToolParams extracts safe metadata from tool parameters.
// It returns key names and non-sensitive value summaries, never content.
//
// Parameters are classified into three categories:
//   - Safe-value params: both key and value are safe to log (e.g., "scope", "dry_run")
//   - Presence-only params: key is logged but value is replaced with "(set)"
//   - Unknown params: not logged at all
//
// A "_param_count" key is always included to indicate how many params were provided.
func sanitizeToolParams(toolName string, params map[string]interface{}) map[string]string {
	if params == nil {
		return nil
	}

	result := make(map[string]string)

	// Safe parameter names whose VALUES are safe to log
	safeValueParams := map[string]bool{
		"scope":         true,
		"threshold":     true,
		"dry_run":       true,
		"format":        true,
		"mode":          true,
		"bidirectional": true,
		"kind":          true,
		"tag":           true,
		"corrections":   true,
		"signal":        true,
		"language":      true,
	}

	// Parameters whose existence is safe to log but whose values may contain
	// sensitive information (file paths, behavior IDs, content, etc.)
	presenceOnlyParams := map[string]bool{
		"wrong":       true,
		"right":       true,
		"file":        true,
		"task":        true,
		"source":      true,
		"target":      true,
		"input_path":  true,
		"output_path": true,
		"weight":      true,
		"auto_merge":  true,
		"behavior_id": true,
	}

	for key, val := range params {
		if safeValueParams[key] {
			result[key] = fmt.Sprintf("%v", val)
		} else if presenceOnlyParams[key] {
			result[key] = "(set)"
		}
		// Other params are not logged at all
	}

	// Always log param count for audit visibility
	result["_param_count"] = fmt.Sprintf("%d", len(params))

	return result
}

// auditTool logs a tool invocation to the audit log with the given scope.
// The scope parameter determines whether the entry is written to the local
// or global audit log ("local" or "global"). Empty scope defaults to "local".
func (s *Server) auditTool(toolName string, start time.Time, err error, params map[string]string, scope string) {
	status := "success"
	errMsg := ""
	if err != nil {
		status = "error"
		errMsg = err.Error()
	}

	if scope == "" {
		scope = "local"
	}

	s.auditLogger.Log(AuditEntry{
		Timestamp:  start,
		Tool:       toolName,
		Scope:      scope,
		DurationMs: time.Since(start).Milliseconds(),
		Status:     status,
		Error:      errMsg,
		Params:     params,
	})
}
