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
	localDir := t.TempDir()
	globalDir := t.TempDir()
	logger := NewAuditLogger(localDir, globalDir)
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
		Scope:      "local",
		Params:     map[string]string{"scope": "local"},
	})

	// Read and verify from local log
	data, err := os.ReadFile(filepath.Join(localDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading local audit log: %v", err)
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
	if entry.Scope != "local" {
		t.Errorf("scope = %q, want local", entry.Scope)
	}
	if entry.Params["scope"] != "local" {
		t.Errorf("params[scope] = %q, want local", entry.Params["scope"])
	}

	// Verify nothing was written to the global log
	globalPath := filepath.Join(globalDir, ".floop", "audit.jsonl")
	if _, err := os.Stat(globalPath); err == nil {
		globalData, _ := os.ReadFile(globalPath)
		if len(globalData) > 0 {
			t.Error("expected no data in global audit log for local-scoped entry")
		}
	}
}

func TestAuditLogger_GlobalScope(t *testing.T) {
	localDir := t.TempDir()
	globalDir := t.TempDir()
	logger := NewAuditLogger(localDir, globalDir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Timestamp:  time.Now(),
		Tool:       "floop_list",
		DurationMs: 10,
		Status:     "success",
		Scope:      "global",
	})

	// Should be written to global log
	globalData, err := os.ReadFile(filepath.Join(globalDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading global audit log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(globalData[:len(globalData)-1], &entry); err != nil {
		t.Fatalf("parsing global audit entry: %v", err)
	}
	if entry.Tool != "floop_list" {
		t.Errorf("tool = %q, want floop_list", entry.Tool)
	}
	if entry.Scope != "global" {
		t.Errorf("scope = %q, want global", entry.Scope)
	}

	// Should NOT be in local log
	localPath := filepath.Join(localDir, ".floop", "audit.jsonl")
	if _, err := os.Stat(localPath); err == nil {
		localData, _ := os.ReadFile(localPath)
		if len(localData) > 0 {
			t.Error("expected no data in local audit log for global-scoped entry")
		}
	}
}

func TestAuditLogger_DefaultScopeIsLocal(t *testing.T) {
	localDir := t.TempDir()
	globalDir := t.TempDir()
	logger := NewAuditLogger(localDir, globalDir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	// Log with empty scope -- should default to local
	logger.Log(AuditEntry{
		Timestamp:  time.Now(),
		Tool:       "floop_active",
		DurationMs: 5,
		Status:     "success",
		Scope:      "",
	})

	// Should be written to local log
	localData, err := os.ReadFile(filepath.Join(localDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading local audit log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(localData[:len(localData)-1], &entry); err != nil {
		t.Fatalf("parsing local audit entry: %v", err)
	}
	if entry.Tool != "floop_active" {
		t.Errorf("tool = %q, want floop_active", entry.Tool)
	}
}

func TestAuditLogger_MultipleEntries(t *testing.T) {
	localDir := t.TempDir()
	globalDir := t.TempDir()
	logger := NewAuditLogger(localDir, globalDir)
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
			Scope:      "local",
		})
	}

	data, err := os.ReadFile(filepath.Join(localDir, ".floop", "audit.jsonl"))
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

func TestAuditLogger_MixedScopes(t *testing.T) {
	localDir := t.TempDir()
	globalDir := t.TempDir()
	logger := NewAuditLogger(localDir, globalDir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	// Write entries to both scopes
	logger.Log(AuditEntry{
		Timestamp: time.Now(), Tool: "floop_learn", Status: "success", Scope: "local",
	})
	logger.Log(AuditEntry{
		Timestamp: time.Now(), Tool: "floop_list", Status: "success", Scope: "global",
	})
	logger.Log(AuditEntry{
		Timestamp: time.Now(), Tool: "floop_active", Status: "success", Scope: "local",
	})

	// Verify local has 2 entries
	localData, err := os.ReadFile(filepath.Join(localDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading local audit log: %v", err)
	}
	localLines := 0
	for _, b := range localData {
		if b == '\n' {
			localLines++
		}
	}
	if localLines != 2 {
		t.Errorf("local line count = %d, want 2", localLines)
	}

	// Verify global has 1 entry
	globalData, err := os.ReadFile(filepath.Join(globalDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading global audit log: %v", err)
	}
	globalLines := 0
	for _, b := range globalData {
		if b == '\n' {
			globalLines++
		}
	}
	if globalLines != 1 {
		t.Errorf("global line count = %d, want 1", globalLines)
	}
}

func TestAuditLogger_ErrorEntry(t *testing.T) {
	localDir := t.TempDir()
	globalDir := t.TempDir()
	logger := NewAuditLogger(localDir, globalDir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Timestamp:  time.Now(),
		Tool:       "floop_restore",
		DurationMs: 5,
		Status:     "error",
		Scope:      "local",
		Error:      "file not found",
	})

	data, err := os.ReadFile(filepath.Join(localDir, ".floop", "audit.jsonl"))
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
	localDir := t.TempDir()
	globalDir := t.TempDir()
	logger := NewAuditLogger(localDir, globalDir)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	defer logger.Close()

	logger.Log(AuditEntry{Tool: "test", Scope: "local"})
	logger.Log(AuditEntry{Tool: "test", Scope: "global"})

	// Check local file permissions
	info, err := os.Stat(filepath.Join(localDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("stat local: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("local permissions = %o, want 0600", perm)
	}

	// Check global file permissions
	info, err = os.Stat(filepath.Join(globalDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("stat global: %v", err)
	}
	perm = info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("global permissions = %o, want 0600", perm)
	}
}

func TestAuditLogger_ConcurrentWrites(t *testing.T) {
	localDir := t.TempDir()
	globalDir := t.TempDir()
	logger := NewAuditLogger(localDir, globalDir)
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
			scope := "local"
			if id%2 == 0 {
				scope = "global"
			}
			for i := 0; i < entriesPerGoroutine; i++ {
				logger.Log(AuditEntry{
					Timestamp:  time.Now(),
					Tool:       "floop_active",
					DurationMs: int64(id*100 + i),
					Status:     "success",
					Scope:      scope,
				})
			}
		}(g)
	}

	wg.Wait()

	// Count lines in local log (odd goroutine IDs: 1,3,5,7,9 = 5 goroutines)
	localData, err := os.ReadFile(filepath.Join(localDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading local audit log: %v", err)
	}
	localLines := 0
	for _, b := range localData {
		if b == '\n' {
			localLines++
		}
	}

	// Count lines in global log (even goroutine IDs: 0,2,4,6,8 = 5 goroutines)
	globalData, err := os.ReadFile(filepath.Join(globalDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading global audit log: %v", err)
	}
	globalLines := 0
	for _, b := range globalData {
		if b == '\n' {
			globalLines++
		}
	}

	total := localLines + globalLines
	want := goroutines * entriesPerGoroutine
	if total != want {
		t.Errorf("total line count = %d (local=%d, global=%d), want %d", total, localLines, globalLines, want)
	}
}

func TestAuditLogger_NonFatalOnBadPath(t *testing.T) {
	// Use a path that cannot be created (file as parent)
	dir := t.TempDir()
	blockPath := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blockPath, []byte("file"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// NewAuditLogger should NOT return nil -- the other logger should still work
	goodDir := t.TempDir()
	logger := NewAuditLogger(blockPath, goodDir)
	if logger == nil {
		t.Fatal("expected non-nil logger when at least one path is valid")
	}
	defer logger.Close()

	// Logging to the failed scope should be a no-op (not panic)
	logger.Log(AuditEntry{Tool: "test", Scope: "local"})

	// Logging to the working scope should succeed
	logger.Log(AuditEntry{Tool: "test", Scope: "global"})
	data, err := os.ReadFile(filepath.Join(goodDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading good audit log: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected data in working audit log")
	}
}

func TestAuditLogger_BothPathsBad(t *testing.T) {
	dir := t.TempDir()
	block1 := filepath.Join(dir, "block1")
	block2 := filepath.Join(dir, "block2")
	if err := os.WriteFile(block1, []byte("file"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(block2, []byte("file"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// When both paths are bad, should return nil
	logger := NewAuditLogger(block1, block2)
	if logger != nil {
		logger.Close()
		t.Error("expected nil logger when both paths are bad")
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
	server.auditTool("floop_test", start, nil, map[string]string{"scope": "local"}, "local")

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
	if entry.Scope != "local" {
		t.Errorf("scope = %q, want local", entry.Scope)
	}
}

func TestAuditTool_GlobalScope(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer server.Close()

	if server.auditLogger == nil {
		t.Fatal("expected auditLogger to be initialized")
	}

	start := time.Now()
	server.auditTool("floop_list", start, nil, map[string]string{"scope": "global"}, "global")

	// The global audit log is under the test's HOME directory
	homeDir := os.Getenv("HOME")
	globalData, err := os.ReadFile(filepath.Join(homeDir, ".floop", "audit.jsonl"))
	if err != nil {
		t.Fatalf("reading global audit log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(globalData[:len(globalData)-1], &entry); err != nil {
		t.Fatalf("parsing global audit entry: %v", err)
	}

	if entry.Tool != "floop_list" {
		t.Errorf("tool = %q, want floop_list", entry.Tool)
	}
	if entry.Scope != "global" {
		t.Errorf("scope = %q, want global", entry.Scope)
	}

	// Local log should not have this entry
	localPath := filepath.Join(tmpDir, ".floop", "audit.jsonl")
	if _, err := os.Stat(localPath); err == nil {
		localData, _ := os.ReadFile(localPath)
		if len(localData) > 0 {
			t.Error("expected no data in local audit log for global-scoped auditTool call")
		}
	}
}
