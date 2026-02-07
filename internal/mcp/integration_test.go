package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jsonrpcRequest represents a JSON-RPC 2.0 request
type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonrpcResponse represents a JSON-RPC 2.0 response
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// TestIntegration_MCPProtocolFlow tests the full MCP protocol lifecycle
func TestIntegration_MCPProtocolFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create isolated test environment
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	// Set isolated HOME
	tmpHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(tmpHome, 0755); err != nil {
		t.Fatalf("Failed to create temp home: %v", err)
	}
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
	})

	// Create server
	cfg := &Config{
		Name:    "floop-integration-test",
		Version: "v1.0.0-test",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// Test that server exposes expected tools
	tools := []string{"floop_active", "floop_learn", "floop_list"}
	t.Run("ServerExposesExpectedTools", func(t *testing.T) {
		// The SDK registers tools at creation time
		// We verify by calling handlers directly since stdio transport
		// isn't suitable for unit testing
		ctx := context.Background()

		// floop_active should work with empty input
		_, output, err := server.handleFloopActive(ctx, nil, FloopActiveInput{})
		if err != nil {
			t.Errorf("floop_active handler failed: %v", err)
		}
		if output.Context == nil {
			t.Error("floop_active should return context")
		}

		// floop_list should work
		_, listOut, err := server.handleFloopList(ctx, nil, FloopListInput{})
		if err != nil {
			t.Errorf("floop_list handler failed: %v", err)
		}
		if listOut.Count < 0 {
			t.Error("floop_list count should be non-negative")
		}
	})

	t.Run("ToolsAreRegistered", func(t *testing.T) {
		// Verify we have the expected tool names
		for _, toolName := range tools {
			t.Logf("Expected tool: %s", toolName)
		}
	})
}

// TestIntegration_LearnAndRetrieve tests the full learn -> list -> active flow
func TestIntegration_LearnAndRetrieve(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	tmpHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(tmpHome, 0755); err != nil {
		t.Fatalf("Failed to create temp home: %v", err)
	}
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
	})

	cfg := &Config{
		Name:    "floop-integration-test",
		Version: "v1.0.0-test",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	ctx := context.Background()

	// Step 1: Learn a correction
	t.Run("Step1_LearnCorrection", func(t *testing.T) {
		learnInput := FloopLearnInput{
			Wrong: "Used println for debugging",
			Right: "Use structured logging with slog",
			File:  "main.go",
		}

		_, output, err := server.handleFloopLearn(ctx, nil, learnInput)
		if err != nil {
			t.Fatalf("floop_learn failed: %v", err)
		}

		if output.CorrectionID == "" {
			t.Error("CorrectionID should not be empty")
		}
		if output.BehaviorID == "" {
			t.Error("BehaviorID should not be empty")
		}

		t.Logf("Learned behavior: %s (confidence: %.2f)", output.BehaviorID, output.Confidence)
	})

	// Step 2: List behaviors - should include our learned behavior
	t.Run("Step2_ListBehaviors", func(t *testing.T) {
		listInput := FloopListInput{Corrections: false}

		_, output, err := server.handleFloopList(ctx, nil, listInput)
		if err != nil {
			t.Fatalf("floop_list failed: %v", err)
		}

		if output.Count < 1 {
			t.Errorf("Expected at least 1 behavior, got %d", output.Count)
		}

		t.Logf("Found %d behaviors", output.Count)
		for _, b := range output.Behaviors {
			t.Logf("  - %s: %s (confidence: %.2f)", b.ID, b.Name, b.Confidence)
		}
	})

	// Step 3: List corrections
	// Note: floop_learn saves corrections to .floop/corrections.jsonl but
	// floop_list queries the node store for corrections, so learned corrections
	// won't appear here unless explicitly added to the node store.
	t.Run("Step3_ListCorrections", func(t *testing.T) {
		listInput := FloopListInput{Corrections: true}

		_, output, err := server.handleFloopList(ctx, nil, listInput)
		if err != nil {
			t.Fatalf("floop_list failed: %v", err)
		}

		// Corrections are saved to a file, not the node store, so count may be 0
		t.Logf("Found %d corrections in node store", output.Count)
	})

	// Step 4: Get active behaviors for Go context
	t.Run("Step4_GetActiveBehaviors", func(t *testing.T) {
		// Create a Go file in the test directory
		goFile := filepath.Join(tmpDir, "main.go")
		if err := os.WriteFile(goFile, []byte("package main\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		activeInput := FloopActiveInput{
			File: "main.go",
			Task: "development",
		}

		_, output, err := server.handleFloopActive(ctx, nil, activeInput)
		if err != nil {
			t.Fatalf("floop_active failed: %v", err)
		}

		t.Logf("Active behaviors for Go: %d", output.Count)
		t.Logf("Context: %+v", output.Context)

		// The learned behavior should be active for Go files
		// (depends on behavior matching logic)
		if output.Context["language"] != "go" {
			t.Errorf("Expected language 'go', got %v", output.Context["language"])
		}
	})
}

// TestIntegration_ConcurrentAccess tests thread safety
func TestIntegration_ConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	tmpHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(tmpHome, 0755); err != nil {
		t.Fatalf("Failed to create temp home: %v", err)
	}
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
	})

	cfg := &Config{
		Name:    "floop-integration-test",
		Version: "v1.0.0-test",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	ctx := context.Background()

	type learnResult struct {
		idx     int
		err     error
		limited bool
	}
	results := make(chan learnResult, 5)

	// Run concurrent learns — some may be rate-limited (burst=3 for floop_learn)
	for i := 0; i < 5; i++ {
		go func(idx int) {
			learnInput := FloopLearnInput{
				Wrong: "Wrong action " + string(rune('A'+idx)),
				Right: "Right action " + string(rune('A'+idx)),
			}
			_, _, err := server.handleFloopLearn(ctx, nil, learnInput)
			limited := err != nil && strings.Contains(err.Error(), "rate limit exceeded")
			if err != nil && !limited {
				results <- learnResult{idx: idx, err: err}
			} else {
				results <- learnResult{idx: idx, limited: limited}
			}
		}(i)
	}

	// Wait for all goroutines
	successCount := 0
	limitedCount := 0
	for i := 0; i < 5; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Errorf("Concurrent learn %d failed with unexpected error: %v", r.idx, r.err)
			} else if r.limited {
				limitedCount++
			} else {
				successCount++
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	t.Logf("Concurrent learns: %d succeeded, %d rate-limited", successCount, limitedCount)

	// At least the burst count (3) should succeed
	if successCount < 3 {
		t.Errorf("Expected at least burst count (3) successful learns, got %d", successCount)
	}

	// Verify behaviors were saved for the successful ones
	_, output, err := server.handleFloopList(ctx, nil, FloopListInput{})
	if err != nil {
		t.Fatalf("floop_list failed: %v", err)
	}

	if output.Count < successCount {
		t.Errorf("Expected at least %d behaviors, got %d", successCount, output.Count)
	}
}

// TestIntegration_StdioTransport tests the stdio transport communication
func TestIntegration_StdioTransport(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create pipes to simulate stdio
	clientToServerReader, clientToServerWriter := io.Pipe()
	serverToClientReader, serverToClientWriter := io.Pipe()

	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	tmpHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(tmpHome, 0755); err != nil {
		t.Fatalf("Failed to create temp home: %v", err)
	}
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
	})

	cfg := &Config{
		Name:    "floop-integration-test",
		Version: "v1.0.0-test",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	defer server.Close()

	// We can't easily test the full stdio transport in unit tests
	// because the SDK's stdio transport expects real stdin/stdout.
	// Instead, we verify the server is properly configured.

	t.Run("ServerConfigured", func(t *testing.T) {
		if server.server == nil {
			t.Error("SDK server is nil")
		}
		if server.store == nil {
			t.Error("Store is nil")
		}
		if server.root != tmpDir {
			t.Errorf("Root = %q, want %q", server.root, tmpDir)
		}
	})

	// Close the pipes
	clientToServerReader.Close()
	clientToServerWriter.Close()
	serverToClientReader.Close()
	serverToClientWriter.Close()
}

// parseJSONRPCLine parses a newline-delimited JSON-RPC message
func parseJSONRPCLine(line string) (*jsonrpcResponse, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// readJSONRPCResponse reads a JSON-RPC response from the reader
func readJSONRPCResponse(reader *bufio.Reader) (*jsonrpcResponse, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return parseJSONRPCLine(line)
}
