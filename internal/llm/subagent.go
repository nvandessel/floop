// Package llm provides interfaces and types for LLM-based behavior comparison and merging.
package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/logging"
	"github.com/nvandessel/feedback-loop/internal/models"
)

// SubagentClient implements the Client interface using the parent CLI's LLM session.
// When floop runs inside Claude Code, Codex, or similar tools, this client spawns
// lightweight subagents that share the parent session's authentication.
type SubagentClient struct {
	// cliPath is the path to the CLI executable (e.g., "claude")
	cliPath string

	// model specifies which model to use for subagent requests
	model string

	// timeout is the maximum duration to wait for a subagent response
	timeout time.Duration

	// allowedCLIDirs restricts CLI search to these directories when set.
	// If empty, any directory is allowed (permissive default).
	allowedCLIDirs []string

	// available caches the result of CLI detection
	available     bool
	availableOnce bool

	// logger is the structured logger for operational output
	logger *slog.Logger

	// decisions logs decision events to JSONL
	decisions *logging.DecisionLogger
}

// SubagentConfig configures the subagent client.
type SubagentConfig struct {
	// CLIPath overrides the default CLI path detection
	CLIPath string

	// Model specifies the model to use (default: "haiku")
	Model string

	// Timeout is the maximum duration for requests (default: 30s)
	Timeout time.Duration

	// AllowedCLIDirs restricts CLI search to these directories.
	// When set, only CLI executables found within these directories are accepted.
	// When empty, any directory is allowed (permissive default).
	AllowedCLIDirs []string

	// Logger is the optional structured logger for operational output.
	Logger *slog.Logger

	// DecisionLogger is the optional decision event logger.
	DecisionLogger *logging.DecisionLogger
}

// DefaultSubagentConfig returns a SubagentConfig with sensible defaults.
func DefaultSubagentConfig() SubagentConfig {
	return SubagentConfig{
		CLIPath: "",
		Model:   "haiku",
		Timeout: 30 * time.Second,
	}
}

// NewSubagentClient creates a new SubagentClient with the given configuration.
func NewSubagentClient(cfg SubagentConfig) *SubagentClient {
	if cfg.Model == "" {
		cfg.Model = "haiku"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &SubagentClient{
		cliPath:        cfg.CLIPath,
		model:          cfg.Model,
		timeout:        cfg.Timeout,
		allowedCLIDirs: cfg.AllowedCLIDirs,
		logger:         logger,
		decisions:      cfg.DecisionLogger,
	}
}

// CompareBehaviors semantically compares two behaviors using a subagent.
func (c *SubagentClient) CompareBehaviors(ctx context.Context, a, b *models.Behavior) (*ComparisonResult, error) {
	if !c.Available() {
		return nil, fmt.Errorf("subagent client not available")
	}

	prompt := ComparisonPrompt(a, b)
	response, err := c.runSubagent(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("running comparison subagent: %w", err)
	}

	result, err := ParseComparisonResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parsing comparison response: %w", err)
	}

	return result, nil
}

// MergeBehaviors combines multiple behaviors using a subagent.
func (c *SubagentClient) MergeBehaviors(ctx context.Context, behaviors []*models.Behavior) (*MergeResult, error) {
	if !c.Available() {
		return nil, fmt.Errorf("subagent client not available")
	}

	if len(behaviors) == 0 {
		return nil, fmt.Errorf("no behaviors to merge")
	}

	prompt := MergePrompt(behaviors)
	response, err := c.runSubagent(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("running merge subagent: %w", err)
	}

	result, err := ParseMergeResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parsing merge response: %w", err)
	}

	return result, nil
}

// Available returns true if the subagent client can be used.
// It checks if running inside a CLI session and if the CLI is accessible.
func (c *SubagentClient) Available() bool {
	if c.availableOnce {
		return c.available
	}

	c.availableOnce = true
	c.available = c.detectAvailability()
	return c.available
}

// detectAvailability checks if we're running inside a CLI session.
func (c *SubagentClient) detectAvailability() bool {
	c.ensureLogger()

	// Check for CLI session environment variables
	inSession := c.inCLISession()
	if !inSession {
		c.logger.Debug("subagent not available: no CLI session env vars")
		if c.decisions != nil {
			c.decisions.Log(map[string]any{
				"event":     "llm_availability",
				"provider":  "subagent",
				"available": false,
				"reason":    "no CLI session env vars",
			})
		}
		return false
	}

	// Find the CLI executable
	cliPath := c.findCLI()
	if cliPath == "" {
		c.logger.Debug("subagent not available: CLI not found")
		if c.decisions != nil {
			c.decisions.Log(map[string]any{
				"event":     "llm_availability",
				"provider":  "subagent",
				"available": false,
				"reason":    "CLI executable not found",
			})
		}
		return false
	}

	c.cliPath = cliPath
	c.logger.Debug("subagent available", "cli_path", cliPath)
	if c.decisions != nil {
		c.decisions.Log(map[string]any{
			"event":     "llm_availability",
			"provider":  "subagent",
			"available": true,
			"cli_path":  cliPath,
		})
	}
	return true
}

// inCLISession checks if we're running inside a CLI agent session.
func (c *SubagentClient) inCLISession() bool {
	// Check for common CLI environment indicators
	// These are set by various Claude-compatible CLIs when running subprocesses

	// Claude Code sets these
	if os.Getenv("CLAUDE_CODE") != "" {
		return true
	}

	// Generic MCP/Claude session indicators
	if os.Getenv("CLAUDE_SESSION_ID") != "" {
		return true
	}

	// Anthropic CLI indicators
	if os.Getenv("ANTHROPIC_CLI") != "" {
		return true
	}

	return false
}

// findCLI locates and validates the CLI executable.
// It checks that the CLI is in an allowed directory (if configured)
// and validates it by running --version.
func (c *SubagentClient) findCLI() string {
	// If explicitly configured, use that
	if c.cliPath != "" {
		path, err := exec.LookPath(c.cliPath)
		if err == nil && c.isAllowedPath(path) && c.validateCLI(context.Background(), path) {
			return path
		}
	}

	// Try common CLI names in order of preference
	cliNames := []string{
		"claude",    // Claude Code CLI
		"anthropic", // Anthropic CLI
		"opencode",  // OpenCode CLI
		"codex",     // Codex CLI
	}

	for _, name := range cliNames {
		if path, err := exec.LookPath(name); err == nil {
			if c.isAllowedPath(path) && c.validateCLI(context.Background(), path) {
				return path
			}
		}
	}

	return ""
}

// isAllowedPath checks if the CLI path is within allowed directories.
// Returns true if no AllowedCLIDirs are configured (permissive default).
func (c *SubagentClient) isAllowedPath(cliPath string) bool {
	if len(c.allowedCLIDirs) == 0 {
		return true
	}

	absPath, err := filepath.Abs(cliPath)
	if err != nil {
		return false
	}

	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return false
	}

	for _, dir := range c.allowedCLIDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if resolved == absDir || strings.HasPrefix(resolved, absDir+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// validateCLI checks that the CLI at the given path is a legitimate tool
// by running it with --version and verifying it exits successfully.
func (c *SubagentClient) validateCLI(ctx context.Context, cliPath string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, cliPath, "--version")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	return cmd.Run() == nil
}

// runSubagent executes a prompt using the CLI and returns the response.
// The prompt is passed via stdin rather than command-line arguments to avoid
// exposing it in process listings (e.g., ps aux).
func (c *SubagentClient) runSubagent(ctx context.Context, prompt string) (string, error) {
	c.ensureLogger()
	start := time.Now()

	c.logger.Debug("subagent request", "model", c.model, "prompt_len", len(prompt))
	if c.decisions != nil {
		c.decisions.Log(map[string]any{
			"event":      "llm_request",
			"operation":  "subagent",
			"model":      c.model,
			"prompt_len": len(prompt),
		})
	}

	// At trace level, log full prompt content
	c.logger.Log(ctx, logging.LevelTrace, "subagent prompt content", "prompt", prompt)

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Build the command
	// Using --print for non-interactive output
	// Using "-p -" to read prompt from stdin instead of command args
	args := []string{
		"--print",
		"--model", c.model,
		"-p", "-",
	}

	cmd := exec.CommandContext(ctx, c.cliPath, args...)

	// Pass prompt via stdin to avoid exposure in process listings
	cmd.Stdin = strings.NewReader(prompt)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	if err := cmd.Run(); err != nil {
		duration := time.Since(start)
		c.logger.Debug("subagent failed", "duration_ms", duration.Milliseconds(), "error", err)
		if c.decisions != nil {
			c.decisions.Log(map[string]any{
				"event":       "llm_response",
				"operation":   "subagent",
				"duration_ms": duration.Milliseconds(),
				"success":     false,
				"error":       err.Error(),
			})
		}

		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("subagent timed out after %v", c.timeout)
		}
		return "", fmt.Errorf("subagent failed: %w (stderr: %s)", err, stderr.String())
	}

	response := strings.TrimSpace(stdout.String())
	duration := time.Since(start)

	if response == "" {
		c.logger.Debug("subagent empty response", "duration_ms", duration.Milliseconds())
		if c.decisions != nil {
			c.decisions.Log(map[string]any{
				"event":       "llm_response",
				"operation":   "subagent",
				"duration_ms": duration.Milliseconds(),
				"success":     false,
				"error":       "empty response",
			})
		}
		return "", fmt.Errorf("subagent returned empty response")
	}

	c.logger.Debug("subagent response", "duration_ms", duration.Milliseconds(), "response_len", len(response))
	if c.decisions != nil {
		c.decisions.Log(map[string]any{
			"event":        "llm_response",
			"operation":    "subagent",
			"duration_ms":  duration.Milliseconds(),
			"response_len": len(response),
			"success":      true,
		})
	}

	// At trace level, log full response content
	c.logger.Log(ctx, logging.LevelTrace, "subagent response content", "response", response)

	return response, nil
}

// ExtractCorrection analyzes user text to determine if it contains a correction.
// Returns the extraction result with wrong/right if a correction is detected.
func (c *SubagentClient) ExtractCorrection(ctx context.Context, userText string) (*CorrectionExtractionResult, error) {
	if !c.Available() {
		return nil, fmt.Errorf("subagent client not available")
	}

	prompt := CorrectionExtractionPrompt(userText)
	response, err := c.runSubagent(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("running extraction subagent: %w", err)
	}

	result, err := ParseCorrectionExtractionResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parsing extraction response: %w", err)
	}

	return result, nil
}

// ensureLogger initializes the logger if it was not set (e.g., when
// SubagentClient is constructed directly in tests without NewSubagentClient).
func (c *SubagentClient) ensureLogger() {
	if c.logger == nil {
		c.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
}

// DetectAndCreate attempts to create a SubagentClient if running in a CLI session.
// Returns nil if not in a CLI session or if detection fails.
func DetectAndCreate() *SubagentClient {
	client := NewSubagentClient(DefaultSubagentConfig())
	if client.Available() {
		return client
	}
	return nil
}
