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
	DurationMs int64             `json:"duration_ms"`
	Status     string            `json:"status"` // "success" or "error"
	Error      string            `json:"error,omitempty"`
	Params     map[string]string `json:"params,omitempty"` // sanitized metadata only
}

// AuditLogger writes audit entries to a JSONL file.
// It is safe for concurrent use. A nil AuditLogger is safe to use;
// all methods are no-ops on nil receiver.
type AuditLogger struct {
	mu   sync.Mutex
	file *os.File
}

// NewAuditLogger creates an audit logger writing to .floop/audit.jsonl
// under the given directory. If the file cannot be created, it prints a
// warning to stderr and returns nil (non-fatal).
func NewAuditLogger(dir string) *AuditLogger {
	path := filepath.Join(dir, ".floop", "audit.jsonl")

	// Ensure directory exists
	auditDir := filepath.Dir(path)
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot create audit log directory: %v\n", err)
		return nil
	}

	// Open file with restricted permissions
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot open audit log: %v\n", err)
		return nil
	}

	return &AuditLogger{file: f}
}

// Log writes an audit entry as a single JSONL line. Safe to call on nil receiver.
func (a *AuditLogger) Log(entry AuditEntry) {
	if a == nil || a.file == nil {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return // silently skip malformed entries
	}

	data = append(data, '\n')
	_, _ = a.file.Write(data)
}

// Close closes the audit log file. Safe to call on nil receiver.
func (a *AuditLogger) Close() error {
	if a == nil || a.file == nil {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	return a.file.Close()
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

// auditTool logs a tool invocation to the audit log.
func (s *Server) auditTool(toolName string, start time.Time, err error, params map[string]string) {
	status := "success"
	errMsg := ""
	if err != nil {
		status = "error"
		errMsg = err.Error()
	}

	s.auditLogger.Log(AuditEntry{
		Timestamp:  start,
		Tool:       toolName,
		DurationMs: time.Since(start).Milliseconds(),
		Status:     status,
		Error:      errMsg,
		Params:     params,
	})
}
