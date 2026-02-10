// Package logging provides leveled logging and decision tracing for floop.
// It offers two complementary outputs:
//   - A leveled slog.Logger for stderr (operational output)
//   - A DecisionLogger for structured JSONL decision traces (.floop/decisions.jsonl)
package logging

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LevelTrace is a custom slog level below Debug for full content logging.
// At this level, LLM prompts/responses and other verbose content are included.
const LevelTrace = slog.LevelDebug - 4

// ParseLevel maps a string level name to a slog.Level.
// Supported values: "info", "debug", "trace" (case-insensitive).
// Unknown values default to info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "trace":
		return LevelTrace
	default:
		return slog.LevelInfo
	}
}

// NewLogger creates a leveled slog.Logger writing to w.
func NewLogger(level string, w io.Writer) *slog.Logger {
	lvl := ParseLevel(level)
	opts := &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Label the custom trace level
			if a.Key == slog.LevelKey {
				if lvl, ok := a.Value.Any().(slog.Level); ok && lvl == LevelTrace {
					a.Value = slog.StringValue("TRACE")
				}
			}
			return a
		},
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

// DecisionLogger writes structured decision events to a JSONL file.
// It is safe for concurrent use. A nil DecisionLogger is safe to use;
// all methods are no-ops on nil receiver.
type DecisionLogger struct {
	mu   sync.Mutex
	file *os.File
}

// NewDecisionLogger creates a decision logger writing to dir/decisions.jsonl.
// At "info" level (the default), returns nil — no file is created.
// At "debug" or "trace" level, the file is opened for append.
// Returns nil if the file cannot be opened. All methods are nil-safe.
func NewDecisionLogger(dir string, level string) *DecisionLogger {
	lvl := ParseLevel(level)
	if lvl == slog.LevelInfo {
		return nil
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil
	}

	path := filepath.Join(dir, "decisions.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil
	}

	return &DecisionLogger{file: f}
}

// Log writes a decision event as a single JSONL line.
// A "time" field is added automatically. The caller's map is not mutated.
// Safe to call on nil receiver.
func (dl *DecisionLogger) Log(event map[string]any) {
	if dl == nil || dl.file == nil {
		return
	}

	// Copy to avoid mutating caller's map
	entry := make(map[string]any, len(event)+1)
	for k, v := range event {
		entry[k] = v
	}
	entry["time"] = time.Now().UTC().Format(time.RFC3339Nano)

	dl.mu.Lock()
	defer dl.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = dl.file.Write(data)
}

// Close closes the underlying file. Safe to call on nil receiver.
func (dl *DecisionLogger) Close() {
	if dl == nil || dl.file == nil {
		return
	}

	dl.mu.Lock()
	defer dl.mu.Unlock()

	dl.file.Close()
	dl.file = nil
}
