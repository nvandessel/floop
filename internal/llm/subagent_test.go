package llm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if cfg.AllowedCLIDirs != nil {
		t.Errorf("AllowedCLIDirs = %v, want nil", cfg.AllowedCLIDirs)
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

	t.Run("with AllowedCLIDirs", func(t *testing.T) {
		dirs := []string{"/usr/bin", "/usr/local/bin"}
		client := NewSubagentClient(SubagentConfig{
			AllowedCLIDirs: dirs,
		})

		if len(client.allowedCLIDirs) != 2 {
			t.Errorf("allowedCLIDirs length = %d, want 2", len(client.allowedCLIDirs))
		}
		if client.allowedCLIDirs[0] != "/usr/bin" {
			t.Errorf("allowedCLIDirs[0] = %q, want /usr/bin", client.allowedCLIDirs[0])
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

	t.Run("no env vars returns false even with parent process", func(t *testing.T) {
		// After removing the ppid > 1 heuristic, inCLISession should return false
		// when no CLI environment variables are set, even though the test process
		// has a parent (ppid > 1).
		if client.inCLISession() {
			t.Error("expected false when no CLI env vars are set (ppid check removed)")
		}
	})
}

func TestSubagentClient_isAllowedPath(t *testing.T) {
	tests := []struct {
		name           string
		allowedCLIDirs []string
		cliPath        string
		want           bool
	}{
		{
			name:           "no restrictions allows any path",
			allowedCLIDirs: nil,
			cliPath:        "/usr/bin/claude",
			want:           true,
		},
		{
			name:           "empty restrictions allows any path",
			allowedCLIDirs: []string{},
			cliPath:        "/tmp/malicious/claude",
			want:           true,
		},
		{
			name:           "path in allowed directory",
			allowedCLIDirs: []string{"/usr/bin"},
			cliPath:        "/usr/bin/claude",
			want:           true,
		},
		{
			name:           "path in second allowed directory",
			allowedCLIDirs: []string{"/opt/bin", "/usr/local/bin"},
			cliPath:        "/usr/local/bin/claude",
			want:           true,
		},
		{
			name:           "path not in allowed directories",
			allowedCLIDirs: []string{"/usr/bin", "/usr/local/bin"},
			cliPath:        "/tmp/malicious/claude",
			want:           false,
		},
		{
			name:           "path prefix attack rejected",
			allowedCLIDirs: []string{"/usr/bin"},
			cliPath:        "/usr/bin-evil/claude",
			want:           false,
		},
		{
			name:           "nonexistent path rejected",
			allowedCLIDirs: []string{"/usr/bin"},
			cliPath:        "/nonexistent/path/claude",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &SubagentClient{
				allowedCLIDirs: tt.allowedCLIDirs,
			}

			got := client.isAllowedPath(tt.cliPath)

			// For paths that don't exist, EvalSymlinks will fail and return false.
			// We need to handle that in our expectations.
			if tt.cliPath == "/nonexistent/path/claude" {
				// EvalSymlinks will fail for nonexistent paths, so isAllowedPath returns false
				if got != false {
					t.Errorf("isAllowedPath(%q) = %v, want false (path doesn't exist)", tt.cliPath, got)
				}
				return
			}

			// For paths that exist on the filesystem, check normally
			if _, err := os.Stat(tt.cliPath); err != nil {
				// Path doesn't actually exist - EvalSymlinks will fail
				// isAllowedPath should return false when path can't be resolved
				if len(tt.allowedCLIDirs) > 0 && got != false {
					t.Errorf("isAllowedPath(%q) = %v, want false (path not resolvable)", tt.cliPath, got)
				}
				return
			}

			if got != tt.want {
				t.Errorf("isAllowedPath(%q) = %v, want %v", tt.cliPath, got, tt.want)
			}
		})
	}
}

func TestSubagentClient_isAllowedPath_WithRealPaths(t *testing.T) {
	// Create a temp directory structure with a real executable
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	blockedDir := filepath.Join(tmpDir, "blocked")

	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatalf("creating allowed dir: %v", err)
	}
	if err := os.MkdirAll(blockedDir, 0o755); err != nil {
		t.Fatalf("creating blocked dir: %v", err)
	}

	// Create a dummy executable in allowed dir
	allowedCLI := filepath.Join(allowedDir, "test-cli")
	if err := os.WriteFile(allowedCLI, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("creating allowed CLI: %v", err)
	}

	// Create a dummy executable in blocked dir
	blockedCLI := filepath.Join(blockedDir, "test-cli")
	if err := os.WriteFile(blockedCLI, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("creating blocked CLI: %v", err)
	}

	client := &SubagentClient{
		allowedCLIDirs: []string{allowedDir},
	}

	t.Run("allowed directory accepted", func(t *testing.T) {
		if !client.isAllowedPath(allowedCLI) {
			t.Errorf("isAllowedPath(%q) = false, want true", allowedCLI)
		}
	})

	t.Run("blocked directory rejected", func(t *testing.T) {
		if client.isAllowedPath(blockedCLI) {
			t.Errorf("isAllowedPath(%q) = true, want false", blockedCLI)
		}
	})

	t.Run("symlink resolved to real path", func(t *testing.T) {
		// Create a symlink from blocked dir pointing to allowed dir CLI
		symlinkPath := filepath.Join(blockedDir, "symlinked-cli")
		if err := os.Symlink(allowedCLI, symlinkPath); err != nil {
			t.Fatalf("creating symlink: %v", err)
		}

		// Even though symlink is in blocked dir, resolved path is in allowed dir
		if !client.isAllowedPath(symlinkPath) {
			t.Errorf("isAllowedPath(%q) = false, want true (symlink resolves to allowed dir)", symlinkPath)
		}
	})

	t.Run("exact directory match accepted", func(t *testing.T) {
		// When the resolved path equals the allowed directory exactly,
		// it should be accepted. This covers the case where the CLI binary
		// path resolves to exactly the allowed directory path.
		if !client.isAllowedPath(allowedDir) {
			t.Errorf("isAllowedPath(%q) = false, want true (exact directory match)", allowedDir)
		}
	})

	t.Run("symlink to blocked dir from allowed rejected", func(t *testing.T) {
		// Create a symlink from allowed dir pointing to blocked dir CLI
		symlinkPath := filepath.Join(allowedDir, "sneaky-link")
		if err := os.Symlink(blockedCLI, symlinkPath); err != nil {
			t.Fatalf("creating symlink: %v", err)
		}

		// Even though symlink is in allowed dir, resolved path is in blocked dir
		if client.isAllowedPath(symlinkPath) {
			t.Errorf("isAllowedPath(%q) = true, want false (symlink resolves to blocked dir)", symlinkPath)
		}
	})
}

func TestSubagentClient_validateCLI(t *testing.T) {
	client := &SubagentClient{}

	t.Run("valid CLI passes validation", func(t *testing.T) {
		// Use 'true' command which always exits 0 and supports --version (or at least exits 0)
		truePath, err := exec.LookPath("true")
		if err != nil {
			t.Skip("skipping: 'true' command not found")
		}

		if !client.validateCLI(context.Background(), truePath) {
			t.Errorf("validateCLI(%q) = false, want true", truePath)
		}
	})

	t.Run("nonexistent CLI fails validation", func(t *testing.T) {
		if client.validateCLI(context.Background(), "/nonexistent/binary") {
			t.Error("validateCLI(/nonexistent/binary) = true, want false")
		}
	})

	t.Run("CLI that exits non-zero fails validation", func(t *testing.T) {
		// Use 'false' command which always exits 1
		falsePath, err := exec.LookPath("false")
		if err != nil {
			t.Skip("skipping: 'false' command not found")
		}

		if client.validateCLI(context.Background(), falsePath) {
			t.Errorf("validateCLI(%q) = true, want false", falsePath)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// sleep would hang forever but context is cancelled
		sleepPath, err := exec.LookPath("sleep")
		if err != nil {
			t.Skip("skipping: 'sleep' command not found")
		}

		if client.validateCLI(ctx, sleepPath) {
			t.Error("validateCLI with cancelled context should return false")
		}
	})
}

func TestSubagentClient_runSubagent_StdinPrompt(t *testing.T) {
	// Create a mock CLI script that reads from stdin and echoes it back
	tmpDir := t.TempDir()
	mockCLI := filepath.Join(tmpDir, "mock-cli")

	// Script reads stdin and writes it to stdout, ignoring command-line args
	script := `#!/bin/sh
# Read stdin and echo it back (simulates reading prompt from stdin)
cat
`
	if err := os.WriteFile(mockCLI, []byte(script), 0o755); err != nil {
		t.Fatalf("creating mock CLI: %v", err)
	}

	client := &SubagentClient{
		cliPath: mockCLI,
		model:   "test",
		timeout: 5 * time.Second,
	}

	t.Run("prompt passed via stdin", func(t *testing.T) {
		testPrompt := "What is the meaning of life?"
		response, err := client.runSubagent(context.Background(), testPrompt)
		if err != nil {
			t.Fatalf("runSubagent() error: %v", err)
		}

		if response != testPrompt {
			t.Errorf("response = %q, want %q (prompt should be passed via stdin)", response, testPrompt)
		}
	})

	t.Run("prompt not in command args", func(t *testing.T) {
		// Create a script that dumps its args to stdout so we can verify
		// the prompt is NOT passed as an argument
		argCheckCLI := filepath.Join(tmpDir, "arg-check-cli")
		argCheckScript := `#!/bin/sh
# Print all args, one per line
for arg in "$@"; do
    echo "ARG: $arg"
done
`
		if err := os.WriteFile(argCheckCLI, []byte(argCheckScript), 0o755); err != nil {
			t.Fatalf("creating arg check CLI: %v", err)
		}

		argClient := &SubagentClient{
			cliPath: argCheckCLI,
			model:   "test",
			timeout: 5 * time.Second,
		}

		sensitivePrompt := "SECRET_API_KEY=sk-12345"
		response, err := argClient.runSubagent(context.Background(), sensitivePrompt)
		if err != nil {
			t.Fatalf("runSubagent() error: %v", err)
		}

		// The prompt should NOT appear in the args
		if strings.Contains(response, sensitivePrompt) {
			t.Error("prompt appears in command arguments - should be passed via stdin only")
		}

		// Verify the expected args are present
		if !strings.Contains(response, "ARG: --print") {
			t.Error("expected --print in args")
		}
		if !strings.Contains(response, "ARG: -p") {
			t.Error("expected -p in args")
		}
		if !strings.Contains(response, "ARG: -") {
			t.Error("expected '-' (stdin marker) in args")
		}
	})

	t.Run("empty response returns error", func(t *testing.T) {
		emptyCLI := filepath.Join(tmpDir, "empty-cli")
		emptyScript := `#!/bin/sh
# Output nothing
`
		if err := os.WriteFile(emptyCLI, []byte(emptyScript), 0o755); err != nil {
			t.Fatalf("creating empty CLI: %v", err)
		}

		emptyClient := &SubagentClient{
			cliPath: emptyCLI,
			model:   "test",
			timeout: 5 * time.Second,
		}

		_, err := emptyClient.runSubagent(context.Background(), "test")
		if err == nil {
			t.Error("expected error for empty response")
		}
		if !strings.Contains(err.Error(), "empty response") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("failed command returns error", func(t *testing.T) {
		failCLI := filepath.Join(tmpDir, "fail-cli")
		failScript := `#!/bin/sh
echo "something went wrong" >&2
exit 1
`
		if err := os.WriteFile(failCLI, []byte(failScript), 0o755); err != nil {
			t.Fatalf("creating fail CLI: %v", err)
		}

		failClient := &SubagentClient{
			cliPath: failCLI,
			model:   "test",
			timeout: 5 * time.Second,
		}

		_, err := failClient.runSubagent(context.Background(), "test")
		if err == nil {
			t.Error("expected error for failed command")
		}
		if !strings.Contains(err.Error(), "subagent failed") {
			t.Errorf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "something went wrong") {
			t.Errorf("error should contain stderr output: %v", err)
		}
	})
}

func TestSubagentClient_findCLI_WithAllowedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	blockedDir := filepath.Join(tmpDir, "blocked")

	if err := os.MkdirAll(allowedDir, 0o755); err != nil {
		t.Fatalf("creating allowed dir: %v", err)
	}
	if err := os.MkdirAll(blockedDir, 0o755); err != nil {
		t.Fatalf("creating blocked dir: %v", err)
	}

	// Create a mock CLI that responds to --version
	createMockCLI := func(dir, name string) string {
		path := filepath.Join(dir, name)
		script := `#!/bin/sh
if [ "$1" = "--version" ]; then
    echo "mock-cli 1.0.0"
    exit 0
fi
cat
`
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("creating mock CLI %s: %v", path, err)
		}
		return path
	}

	t.Run("explicit path in allowed dir accepted", func(t *testing.T) {
		cliPath := createMockCLI(allowedDir, "test-cli")
		client := &SubagentClient{
			cliPath:        cliPath,
			allowedCLIDirs: []string{allowedDir},
		}

		result := client.findCLI()
		if result != cliPath {
			t.Errorf("findCLI() = %q, want %q", result, cliPath)
		}
	})

	t.Run("explicit path in blocked dir rejected", func(t *testing.T) {
		cliPath := createMockCLI(blockedDir, "test-cli2")
		client := &SubagentClient{
			cliPath:        cliPath,
			allowedCLIDirs: []string{allowedDir},
		}

		result := client.findCLI()
		if result == cliPath {
			t.Errorf("findCLI() should not return CLI from blocked directory %q", cliPath)
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
	// Save and restore environment to ensure clean state
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

	// With the ppid check removed, DetectAndCreate will only detect availability
	// when CLI env vars are set. Without them, it should return nil.
	t.Run("returns nil without CLI env vars", func(t *testing.T) {
		for _, v := range envVars {
			os.Unsetenv(v)
		}

		client := DetectAndCreate()
		if client != nil {
			t.Error("DetectAndCreate should return nil without CLI env vars set")
		}
	})

	t.Run("runs without panic", func(t *testing.T) {
		// Just verify the function doesn't panic regardless of env
		client := DetectAndCreate()
		if client != nil {
			if !client.Available() {
				t.Error("DetectAndCreate returned non-nil client but Available() is false")
			}
		}
	})
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
