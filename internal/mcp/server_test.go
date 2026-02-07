package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// isolateHome sets HOME to a temp directory to avoid touching real ~/.floop/
func isolateHome(t *testing.T, tmpDir string) {
	t.Helper()
	tmpHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(tmpHome, 0755); err != nil {
		t.Fatalf("Failed to create temp home: %v", err)
	}
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
	})
}

func TestNewServer(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize .floop directory
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	// Create server
	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	if server.server == nil {
		t.Error("Server.server is nil")
	}

	if server.store == nil {
		t.Error("Server.store is nil")
	}

	if server.root != tmpDir {
		t.Errorf("Server.root = %q, want %q", server.root, tmpDir)
	}
}

func TestNewServer_CreatesFloopDir(t *testing.T) {
	// Create temp directory WITHOUT .floop
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// Verify .floop directory was created
	floopDir := filepath.Join(tmpDir, ".floop")
	if _, err := os.Stat(floopDir); os.IsNotExist(err) {
		t.Error(".floop directory was not created")
	}
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Close should not error
	if err := server.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Multiple closes should be safe
	if err := server.Close(); err != nil {
		t.Errorf("Second Close() error = %v", err)
	}
}

func TestNewServer_HasRateLimiters(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	if server.toolLimiters == nil {
		t.Error("toolLimiters should be initialized")
	}

	expectedTools := []string{
		"floop_learn", "floop_active", "floop_backup",
		"floop_restore", "floop_connect", "floop_deduplicate",
		"floop_list", "floop_validate",
	}
	for _, tool := range expectedTools {
		if _, ok := server.toolLimiters[tool]; !ok {
			t.Errorf("missing rate limiter for %s", tool)
		}
	}
}

func TestNewServer_HasWorkerPool(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	if server.workerPool == nil {
		t.Error("workerPool should be initialized")
	}

	if cap(server.workerPool) != maxBackgroundWorkers {
		t.Errorf("workerPool capacity = %d, want %d", cap(server.workerPool), maxBackgroundWorkers)
	}
}

func TestRunBackground_BoundsGoroutines(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// Fill the worker pool
	block := make(chan struct{})
	for i := 0; i < maxBackgroundWorkers; i++ {
		server.runBackground("blocker", func() {
			<-block // Block until released
		})
	}

	// Next background task should be dropped (pool full)
	dropped := true
	server.runBackground("overflow", func() {
		dropped = false
	})

	// The overflow task should not execute
	if !dropped {
		t.Error("expected background task to be dropped when pool is full")
	}

	// Release blockers
	close(block)
}

func TestRun_CancelledContext(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Run should return quickly with cancelled context
	err = server.Run(ctx)
	// We expect an error since stdio transport won't work in test
	// but we're just verifying it doesn't hang
	if err == nil {
		t.Log("Run returned nil (expected in test environment)")
	}
}

func TestClose_GracefulShutdownStopsDebounce(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Trigger a debounced PageRank refresh (fires after 2s)
	server.debouncedRefreshPageRank()

	// Close the server immediately — before the 2s timer fires.
	// This should stop the debounce timer and signal shutdown via the done channel.
	// Without the fix, the timer callback would run against a closed store,
	// causing "sql: database is closed" errors.
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Wait long enough for the timer to have fired (if it wasn't stopped)
	time.Sleep(3 * time.Second)

	// If we get here without panic or error, graceful shutdown worked
	t.Log("Graceful shutdown completed without panic or database errors")
}

func TestRunBackground_SkipsAfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Close the server first
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Attempting to run a background task after close should be a no-op
	executed := false
	server.runBackground("post-close-task", func() {
		executed = true
	})

	// Give it a moment in case the goroutine was somehow started
	time.Sleep(50 * time.Millisecond)

	if executed {
		t.Error("expected background task to be skipped after Close()")
	}
}
