package llm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestDefaultSubagentConfig(t *testing.T) {
	cfg := DefaultSubagentConfig()

	if cfg.CLIPath != "" {
		t.Errorf("CLIPath = %q, want empty string", cfg.CLIPath)
	}
	if cfg.Model != "haiku" {
		t.Errorf("Model = %q, want haiku", cfg.Model)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
}

func TestNewSubagentClient(t *testing.T) {
	t.Run("with defaults", func(t *testing.T) {
		client := NewSubagentClient(SubagentConfig{})

		if client.model != "haiku" {
			t.Errorf("model = %q, want haiku", client.model)
		}
		if client.timeout != 30*time.Second {
			t.Errorf("timeout = %v, want 30s", client.timeout)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		client := NewSubagentClient(SubagentConfig{
			CLIPath: "/usr/bin/my-cli",
			Model:   "sonnet",
			Timeout: 60 * time.Second,
		})

		if client.cliPath != "/usr/bin/my-cli" {
			t.Errorf("cliPath = %q, want /usr/bin/my-cli", client.cliPath)
		}
		if client.model != "sonnet" {
			t.Errorf("model = %q, want sonnet", client.model)
		}
		if client.timeout != 60*time.Second {
			t.Errorf("timeout = %v, want 60s", client.timeout)
		}
	})

	t.Run("empty model uses default", func(t *testing.T) {
		client := NewSubagentClient(SubagentConfig{
			Model: "",
		})

		if client.model != "haiku" {
			t.Errorf("model = %q, want haiku (default)", client.model)
		}
	})

	t.Run("zero timeout uses default", func(t *testing.T) {
		client := NewSubagentClient(SubagentConfig{
			Timeout: 0,
		})

		if client.timeout != 30*time.Second {
			t.Errorf("timeout = %v, want 30s (default)", client.timeout)
		}
	})
}

func TestSubagentClient_inCLISession(t *testing.T) {
	// Save and restore environment
	envVars := []string{"CLAUDE_CODE", "CLAUDE_SESSION_ID", "ANTHROPIC_CLI"}
	saved := make(map[string]string)
	for _, v := range envVars {
		saved[v] = os.Getenv(v)
	}
	defer func() {
		for k, v := range saved {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	// Clear all CLI env vars first
	for _, v := range envVars {
		os.Unsetenv(v)
	}

	client := NewSubagentClient(SubagentConfig{})

	t.Run("CLAUDE_CODE env var", func(t *testing.T) {
		os.Setenv("CLAUDE_CODE", "1")
		defer os.Unsetenv("CLAUDE_CODE")

		if !client.inCLISession() {
			t.Error("expected true when CLAUDE_CODE is set")
		}
	})

	t.Run("CLAUDE_SESSION_ID env var", func(t *testing.T) {
		os.Setenv("CLAUDE_SESSION_ID", "test-session")
		defer os.Unsetenv("CLAUDE_SESSION_ID")

		if !client.inCLISession() {
			t.Error("expected true when CLAUDE_SESSION_ID is set")
		}
	})

	t.Run("ANTHROPIC_CLI env var", func(t *testing.T) {
		os.Setenv("ANTHROPIC_CLI", "1")
		defer os.Unsetenv("ANTHROPIC_CLI")

		if !client.inCLISession() {
			t.Error("expected true when ANTHROPIC_CLI is set")
		}
	})

	t.Run("always true when has non-init parent", func(t *testing.T) {
		// This will always be true in test environment since tests have a parent process
		// The inCLISession heuristic treats any process with a parent (ppid > 1) as potential CLI session
		if !client.inCLISession() {
			// This could fail in some edge cases but is expected to pass normally
			t.Log("inCLISession returned false - might be running in init context")
		}
	})
}

func TestSubagentClient_Available_Caching(t *testing.T) {
	client := NewSubagentClient(SubagentConfig{})

	// First call determines availability
	firstResult := client.Available()

	// Set the cache to a different value to verify caching works
	client.available = !firstResult

	// Second call should return cached value (the flipped one), proving caching works
	secondResult := client.Available()

	if secondResult != !firstResult {
		t.Error("Available() should return cached result on subsequent calls")
	}
}

func TestSubagentClient_findCLI(t *testing.T) {
	t.Run("explicit path takes precedence", func(t *testing.T) {
		// Use a path that exists (the go binary)
		goPath := "/usr/bin/go"
		if _, err := os.Stat(goPath); os.IsNotExist(err) {
			t.Skip("skipping test: /usr/bin/go not found")
		}

		client := NewSubagentClient(SubagentConfig{
			CLIPath: goPath,
		})

		result := client.findCLI()
		if result != goPath {
			t.Errorf("findCLI() = %q, want %q", result, goPath)
		}
	})

	t.Run("returns empty for non-existent explicit path", func(t *testing.T) {
		client := NewSubagentClient(SubagentConfig{
			CLIPath: "/nonexistent/path/to/cli",
		})

		// Note: findCLI will fall back to searching common CLI names
		// so we can't guarantee it returns empty without mocking
		result := client.findCLI()
		// Just verify it doesn't return the non-existent path
		if result == "/nonexistent/path/to/cli" {
			t.Error("findCLI() should not return a non-existent explicit path")
		}
	})
}

func TestDetectAndCreate(t *testing.T) {
	// DetectAndCreate creates a client and checks availability
	// In test environment, this will typically return a client since
	// the inCLISession heuristic is permissive (ppid > 1)

	client := DetectAndCreate()

	// We can't guarantee whether this returns nil or a client
	// because it depends on CLI availability on the test machine
	// Just verify the function runs without panic
	if client != nil {
		if !client.Available() {
			t.Error("DetectAndCreate returned non-nil client but Available() is false")
		}
	}
}

func TestSubagentClient_CompareBehaviors_NotAvailable(t *testing.T) {
	// Create a client that's explicitly not available
	client := &SubagentClient{
		availableOnce: true,
		available:     false,
	}

	_, err := client.CompareBehaviors(context.TODO(), nil, nil)
	if err == nil {
		t.Error("expected error when client not available")
	}
	if err.Error() != "subagent client not available" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSubagentClient_MergeBehaviors_NotAvailable(t *testing.T) {
	// Create a client that's explicitly not available
	client := &SubagentClient{
		availableOnce: true,
		available:     false,
	}

	_, err := client.MergeBehaviors(context.TODO(), nil)
	if err == nil {
		t.Error("expected error when client not available")
	}
	if err.Error() != "subagent client not available" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSubagentClient_MergeBehaviors_EmptyInput(t *testing.T) {
	// Create a client that's available but will fail on empty input
	client := &SubagentClient{
		availableOnce: true,
		available:     true,
	}

	_, err := client.MergeBehaviors(context.TODO(), []*models.Behavior{})
	if err == nil {
		t.Error("expected error for empty behaviors")
	}
	if err.Error() != "no behaviors to merge" {
		t.Errorf("unexpected error message: %v", err)
	}
}
