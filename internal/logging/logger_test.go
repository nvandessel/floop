package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{"info", "info", slog.LevelInfo},
		{"debug", "debug", slog.LevelDebug},
		{"trace", "trace", LevelTrace},
		{"uppercase INFO", "INFO", slog.LevelInfo},
		{"uppercase DEBUG", "DEBUG", slog.LevelDebug},
		{"uppercase TRACE", "TRACE", LevelTrace},
		{"mixed case Debug", "Debug", slog.LevelDebug},
		{"unknown defaults to info", "unknown", slog.LevelInfo},
		{"empty defaults to info", "", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLevel(tt.input)
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{"info level", "info"},
		{"debug level", "debug"},
		{"trace level", "trace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(tt.level, &buf)
			if logger == nil {
				t.Fatal("NewLogger returned nil")
			}
		})
	}
}

func TestNewLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		logAtDebug bool
		logAtInfo  bool
	}{
		{"info filters debug", "info", false, true},
		{"debug passes debug", "debug", true, true},
		{"trace passes debug", "trace", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(tt.level, &buf)

			logger.Debug("debug message")
			hasDebug := strings.Contains(buf.String(), "debug message")
			if hasDebug != tt.logAtDebug {
				t.Errorf("debug message visible = %v, want %v (buf: %q)", hasDebug, tt.logAtDebug, buf.String())
			}

			buf.Reset()
			logger.Info("info message")
			hasInfo := strings.Contains(buf.String(), "info message")
			if hasInfo != tt.logAtInfo {
				t.Errorf("info message visible = %v, want %v (buf: %q)", hasInfo, tt.logAtInfo, buf.String())
			}
		})
	}
}

func TestLevelTrace(t *testing.T) {
	// Trace should be below debug (more verbose)
	if LevelTrace >= slog.LevelDebug {
		t.Errorf("LevelTrace (%d) should be less than LevelDebug (%d)", LevelTrace, slog.LevelDebug)
	}
}

func TestNewDecisionLogger_InfoLevel(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(dir, "info")

	// At info level, decision logger should be nil
	if dl != nil {
		t.Error("expected nil DecisionLogger at info level")
	}

	// Nil logger should still be safe to use
	dl.Log(map[string]any{"event": "test"})

	path := filepath.Join(dir, "decisions.jsonl")
	if _, err := os.Stat(path); err == nil {
		t.Error("decisions.jsonl should not exist at info level")
	}
}

func TestNewDecisionLogger_DebugLevel(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(dir, "debug")
	defer dl.Close()

	dl.Log(map[string]any{"event": "test_event", "score": 0.87})

	path := filepath.Join(dir, "decisions.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read decisions.jsonl: %v", err)
	}

	// Parse the JSONL line
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse JSONL entry: %v", err)
	}

	if entry["event"] != "test_event" {
		t.Errorf("event = %v, want test_event", entry["event"])
	}
	if entry["score"] != 0.87 {
		t.Errorf("score = %v, want 0.87", entry["score"])
	}
	if _, ok := entry["time"]; !ok {
		t.Error("expected 'time' field in decision log entry")
	}
}

func TestNewDecisionLogger_TraceLevel(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(dir, "trace")
	defer dl.Close()

	dl.Log(map[string]any{"event": "trace_event"})

	path := filepath.Join(dir, "decisions.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read decisions.jsonl: %v", err)
	}

	if !strings.Contains(string(data), "trace_event") {
		t.Error("expected trace_event in decisions.jsonl")
	}
}

func TestNewDecisionLogger_MultipleWrites(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(dir, "debug")
	defer dl.Close()

	dl.Log(map[string]any{"event": "first"})
	dl.Log(map[string]any{"event": "second"})

	path := filepath.Join(dir, "decisions.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read decisions.jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(data))
	}

	var first, second map[string]any
	json.Unmarshal([]byte(lines[0]), &first)
	json.Unmarshal([]byte(lines[1]), &second)

	if first["event"] != "first" {
		t.Errorf("first event = %v, want 'first'", first["event"])
	}
	if second["event"] != "second" {
		t.Errorf("second event = %v, want 'second'", second["event"])
	}
}

func TestDecisionLogger_NilSafety(t *testing.T) {
	// nil DecisionLogger should not panic
	var dl *DecisionLogger
	dl.Log(map[string]any{"event": "should_not_panic"})
	dl.Close()
}

func TestDecisionLogger_DoesNotMutateCallerMap(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(dir, "debug")
	defer dl.Close()

	event := map[string]any{"event": "test"}
	dl.Log(event)

	if _, hasTime := event["time"]; hasTime {
		t.Error("Log() should not mutate caller's map, but 'time' was injected")
	}
}

func TestDecisionLogger_LogAfterClose(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(dir, "debug")

	dl.Log(map[string]any{"event": "before_close"})
	dl.Close()

	// Should be a no-op, not panic or error
	dl.Log(map[string]any{"event": "after_close"})
}

func TestNewDecisionLogger_CreatesDir(t *testing.T) {
	base := t.TempDir()
	nestedDir := filepath.Join(base, "sub", "dir")

	dl := NewDecisionLogger(nestedDir, "debug")
	if dl == nil {
		t.Fatal("expected non-nil DecisionLogger when dir needs creation")
	}
	defer dl.Close()

	dl.Log(map[string]any{"event": "dir_create_test"})

	path := filepath.Join(nestedDir, "decisions.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("decisions.jsonl should exist after dir creation: %v", err)
	}
}

func TestDecisionLogger_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	dl := NewDecisionLogger(dir, "debug")
	defer dl.Close()

	dl.Log(map[string]any{"event": "perm_test"})

	path := filepath.Join(dir, "decisions.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat decisions.jsonl: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}
