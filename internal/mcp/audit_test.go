package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAuditLogger_NilSafety(t *testing.T) {
	t.Run("nil logger Log is no-op", func(t *testing.T) {
		var logger *AuditLogger
		// Should not panic
		logger.Log(AuditEntry{Tool: "test"})
	})

	t.Run("nil logger Close is no-op", func(t *testing.T) {
		var logger *AuditLogger
		err := logger.Close()
		if err != nil {
			t.Errorf("Close() on nil logger returned error: %v", err)
		}
	})
}

func TestAuditLogger_WritesJSONL(t *testing.T) {
	dir := t.TempDir()
	logger := NewAuditLogger(dir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	now := time.Now()
	logger.Log(AuditEntry{
		Timestamp:  now,
		Tool:       "floop_learn",
		DurationMs: 42,
		Status:     "success",
		Params:     map[string]string{"scope": "local"},
	})

	// Read and verify
	data, err := os.ReadFile(filepath.Join(dir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("parsing audit entry: %v", err)
	}
	if entry.Tool != "floop_learn" {
		t.Errorf("tool = %q, want floop_learn", entry.Tool)
	}
	if entry.DurationMs != 42 {
		t.Errorf("duration_ms = %d, want 42", entry.DurationMs)
	}
	if entry.Status != "success" {
		t.Errorf("status = %q, want success", entry.Status)
	}
	if entry.Params["scope"] != "local" {
		t.Errorf("params[scope] = %q, want local", entry.Params["scope"])
	}
}

func TestAuditLogger_MultipleEntries(t *testing.T) {
	dir := t.TempDir()
	logger := NewAuditLogger(dir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	for i := 0; i < 3; i++ {
		logger.Log(AuditEntry{
			Timestamp:  time.Now(),
			Tool:       "floop_list",
			DurationMs: int64(i * 10),
			Status:     "success",
		})
	}

	data, err := os.ReadFile(filepath.Join(dir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}

	// Count lines (JSONL = one JSON object per line)
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("line count = %d, want 3", lines)
	}
}

func TestAuditLogger_ErrorEntry(t *testing.T) {
	dir := t.TempDir()
	logger := NewAuditLogger(dir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Timestamp:  time.Now(),
		Tool:       "floop_restore",
		DurationMs: 5,
		Status:     "error",
		Error:      "file not found",
	})

	data, err := os.ReadFile(filepath.Join(dir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("parsing audit entry: %v", err)
	}
	if entry.Status != "error" {
		t.Errorf("status = %q, want error", entry.Status)
	}
	if entry.Error != "file not found" {
		t.Errorf("error = %q, want 'file not found'", entry.Error)
	}
}

func TestAuditLogger_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	logger := NewAuditLogger(dir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	logger.Log(AuditEntry{Tool: "test"})

	info, err := os.Stat(filepath.Join(dir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}

func TestAuditLogger_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	logger := NewAuditLogger(dir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	const goroutines = 10
	const entriesPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < entriesPerGoroutine; i++ {
				logger.Log(AuditEntry{
					Timestamp:  time.Now(),
					Tool:       "floop_active",
					DurationMs: int64(id*100 + i),
					Status:     "success",
				})
			}
		}(g)
	}

	wg.Wait()

	data, err := os.ReadFile(filepath.Join(dir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}

	// Count newlines
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	want := goroutines * entriesPerGoroutine
	if lines != want {
		t.Errorf("line count = %d, want %d", lines, want)
	}
}

func TestAuditLogger_NonFatalOnBadPath(t *testing.T) {
	// Use a path that cannot be created (file as parent)
	dir := t.TempDir()
	blockPath := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blockPath, []byte("file"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// NewAuditLogger should return nil (non-fatal), not panic
	logger := NewAuditLogger(blockPath)
	if logger != nil {
		logger.Close()
		t.Error("expected nil logger for invalid path")
	}
}

func TestSanitizeToolParams(t *testing.T) {
	t.Run("safe values are included", func(t *testing.T) {
		params := map[string]interface{}{
			"scope":     "local",
			"threshold": 0.9,
			"dry_run":   true,
		}
		result := sanitizeToolParams("floop_deduplicate", params)
		if result["scope"] != "local" {
			t.Errorf("scope = %q, want local", result["scope"])
		}
		if result["threshold"] != "0.9" {
			t.Errorf("threshold = %q, want 0.9", result["threshold"])
		}
		if result["dry_run"] != "true" {
			t.Errorf("dry_run = %q, want true", result["dry_run"])
		}
		if result["_param_count"] != "3" {
			t.Errorf("_param_count = %q, want 3", result["_param_count"])
		}
	})

	t.Run("sensitive values are redacted", func(t *testing.T) {
		params := map[string]interface{}{
			"wrong": "user said something private",
			"right": "should do something else",
			"file":  "/home/user/secret.go",
			"task":  "development",
		}
		result := sanitizeToolParams("floop_learn", params)
		if result["wrong"] != "(set)" {
			t.Errorf("wrong = %q, want (set)", result["wrong"])
		}
		if result["right"] != "(set)" {
			t.Errorf("right = %q, want (set)", result["right"])
		}
		if result["file"] != "(set)" {
			t.Errorf("file = %q, want (set)", result["file"])
		}
		if result["task"] != "(set)" {
			t.Errorf("task = %q, want (set)", result["task"])
		}
	})

	t.Run("unknown params are excluded", func(t *testing.T) {
		params := map[string]interface{}{
			"malicious_param": "should not appear",
		}
		result := sanitizeToolParams("test", params)
		if _, ok := result["malicious_param"]; ok {
			t.Error("unknown param should not be included")
		}
		// But param count should still reflect it
		if result["_param_count"] != "1" {
			t.Errorf("_param_count = %q, want 1", result["_param_count"])
		}
	})

	t.Run("nil params returns nil", func(t *testing.T) {
		result := sanitizeToolParams("test", nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("connect params handled correctly", func(t *testing.T) {
		params := map[string]interface{}{
			"source":        "behavior-123",
			"target":        "behavior-456",
			"kind":          "similar-to",
			"weight":        0.8,
			"bidirectional": true,
		}
		result := sanitizeToolParams("floop_connect", params)
		if result["source"] != "(set)" {
			t.Errorf("source = %q, want (set)", result["source"])
		}
		if result["target"] != "(set)" {
			t.Errorf("target = %q, want (set)", result["target"])
		}
		if result["kind"] != "similar-to" {
			t.Errorf("kind = %q, want similar-to", result["kind"])
		}
		if result["bidirectional"] != "true" {
			t.Errorf("bidirectional = %q, want true", result["bidirectional"])
		}
		if result["weight"] != "(set)" {
			t.Errorf("weight = %q, want (set)", result["weight"])
		}
	})
}

func TestAuditTool_Integration(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer server.Close()

	if server.auditLogger == nil {
		t.Fatal("expected auditLogger to be initialized")
	}

	// Use the auditTool helper
	start := time.Now()
	time.Sleep(1 * time.Millisecond) // ensure non-zero duration
	server.auditTool("floop_test", start, nil, map[string]string{"scope": "local"})

	// Read the audit log
	data, err := os.ReadFile(filepath.Join(tmpDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("parsing audit entry: %v", err)
	}

	if entry.Tool != "floop_test" {
		t.Errorf("tool = %q, want floop_test", entry.Tool)
	}
	if entry.Status != "success" {
		t.Errorf("status = %q, want success", entry.Status)
	}
	if entry.DurationMs < 1 {
		t.Errorf("duration_ms = %d, want >= 1", entry.DurationMs)
	}
}
