package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/activation"
	"github.com/nvandessel/feedback-loop/internal/assembly"
	"github.com/nvandessel/feedback-loop/internal/config"
	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/sanitize"
	"github.com/nvandessel/feedback-loop/internal/session"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/tiering"
	"github.com/spf13/cobra"
)

// newHookCmd creates the parent 'hook' command with subcommands for each
// Claude Code hook event. These replace the previously extracted .sh scripts.
func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Hook subcommands for Claude Code integration",
		Long: `Native Go implementations of Claude Code hook handlers.

These subcommands read JSON from stdin (as provided by Claude Code hooks)
and perform the appropriate action. They replace the previously extracted
shell scripts, eliminating bash/jq dependencies for Windows support.`,
	}

	cmd.AddCommand(
		newHookSessionStartCmd(),
		newHookFirstPromptCmd(),
		newHookDynamicContextCmd(),
		newHookDetectCorrectionCmd(),
	)

	return cmd
}

// newHookSessionStartCmd creates the 'hook session-start' subcommand.
// It generates a prompt with all active behaviors for session injection.
func newHookSessionStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-start",
		Short: "Inject behaviors at session start",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return runHookPrompt(cmd, root)
		},
	}
}

// newHookFirstPromptCmd creates the 'hook first-prompt' subcommand.
// It uses atomic mkdir for dedup so behaviors are injected only once per session.
func newHookFirstPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "first-prompt",
		Short: "Fallback behavior injection on first prompt",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")

			// Read session_id from stdin JSON
			var input struct {
				SessionID string `json:"session_id"`
			}
			if err := json.NewDecoder(cmd.InOrStdin()).Decode(&input); err != nil {
				// Invalid input — exit silently (hook context)
				return nil
			}

			if input.SessionID == "" {
				input.SessionID = "unknown"
			}

			// Atomic dedup: mkdir fails if dir already exists (TOCTOU-safe, cross-platform)
			marker := filepath.Join(os.TempDir(), fmt.Sprintf("floop-session-%s", input.SessionID))
			if err := os.Mkdir(marker, 0700); err != nil {
				// Already exists — this session was already handled
				return nil
			}

			return runHookPrompt(cmd, root)
		},
	}
}

// newHookDynamicContextCmd creates the 'hook dynamic-context' subcommand.
// It reads tool_name and tool_input from stdin and routes to the appropriate
// activation pipeline.
func newHookDynamicContextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dynamic-context",
		Short: "Dynamic context injection based on tool use",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")

			// Read stdin JSON
			var input struct {
				ToolName  string                 `json:"tool_name"`
				ToolInput map[string]interface{} `json:"tool_input"`
				SessionID string                 `json:"session_id"`
			}
			if err := json.NewDecoder(cmd.InOrStdin()).Decode(&input); err != nil {
				return nil // invalid input — exit silently
			}

			if input.ToolName == "" {
				return nil
			}
			if input.SessionID == "" {
				input.SessionID = "default"
			}

			// Load dynamic context token budget from config
			cfg, err := config.Load()
			if err != nil {
				cfg = config.Default()
			}
			tokenBudget := cfg.TokenBudget.DynamicContext

			switch input.ToolName {
			case "Read", "Edit", "Write":
				filePath := extractFilePath(input.ToolInput)
				if filePath == "" {
					return nil
				}
				return runHookActivate(cmd, root, filePath, "", tokenBudget, input.SessionID)

			case "Bash":
				command, _ := input.ToolInput["command"].(string)
				if command == "" {
					return nil
				}
				task := detectTaskFromCommand(command)
				if task == "" {
					return nil
				}
				return runHookActivate(cmd, root, "", task, tokenBudget, input.SessionID)

			default:
				return nil // unknown tool — exit silently
			}
		},
	}
}

// newHookDetectCorrectionCmd creates the 'hook detect-correction' subcommand.
// It reads the user prompt from stdin and runs correction detection with a timeout.
func newHookDetectCorrectionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detect-correction",
		Short: "Detect and capture corrections from user prompts",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")

			// Read prompt from stdin JSON
			var input struct {
				Prompt string `json:"prompt"`
			}
			if err := json.NewDecoder(cmd.InOrStdin()).Decode(&input); err != nil {
				return nil
			}

			if input.Prompt == "" {
				return nil
			}

			// Fast pattern check
			capture := learning.NewCorrectionCapture()
			if !capture.MightBeCorrection(input.Prompt) {
				return nil
			}

			// Try LLM extraction with timeout
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			client := llm.DetectAndCreate()
			if client == nil {
				return nil
			}

			result, err := client.ExtractCorrection(ctx, input.Prompt)
			if err != nil || !result.IsCorrection || result.Wrong == "" || result.Right == "" {
				return nil
			}

			if result.Confidence < 0.6 {
				return nil
			}

			// Sanitize extracted values
			wrong := sanitize.SanitizeBehaviorContent(result.Wrong)
			right := sanitize.SanitizeBehaviorContent(result.Right)

			// Ensure .floop exists
			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return nil
			}

			// Open graph store and process
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return nil
			}
			defer graphStore.Close()

			now := time.Now()
			correction := models.Correction{
				ID:              fmt.Sprintf("c-%d", now.UnixNano()),
				Timestamp:       now,
				Context:         models.ContextSnapshot{Timestamp: now},
				AgentAction:     wrong,
				CorrectedAction: right,
				Processed:       false,
			}

			loop := learning.NewLearningLoop(graphStore, nil)
			_, processErr := loop.ProcessCorrection(ctx, correction)
			if processErr != nil {
				return nil
			}

			// Mark processed and append to corrections log
			correction.Processed = true
			processedAt := time.Now()
			correction.ProcessedAt = &processedAt

			correctionsPath := filepath.Join(floopDir, "corrections.jsonl")
			f, err := os.OpenFile(correctionsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err == nil {
				json.NewEncoder(f).Encode(correction)
				f.Close()
			}

			return nil
		},
	}
}

// runHookPrompt generates a markdown prompt with all active behaviors.
// Used by session-start and first-prompt hooks.
func runHookPrompt(cmd *cobra.Command, root string) error {
	// Check initialization silently
	floopDir := filepath.Join(root, ".floop")
	if _, err := os.Stat(floopDir); os.IsNotExist(err) {
		return nil
	}

	// Load config for token budget
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	tokenBudget := cfg.TokenBudget.Default

	// Load all behaviors from both scopes
	behaviors, err := loadBehaviorsWithScope(root, constants.ScopeBoth)
	if err != nil {
		return nil // silent in hook context
	}

	if len(behaviors) == 0 {
		return nil
	}

	// Evaluate which behaviors are active (no specific context for session start)
	ctx := activation.NewContextBuilder().
		WithRepoRoot(root).
		Build()

	evaluator := activation.NewEvaluator()
	matches := evaluator.Evaluate(ctx, behaviors)

	resolver := activation.NewResolver()
	resolved := resolver.Resolve(matches)

	if len(resolved.Active) == 0 {
		return nil
	}

	// Use tiered injection with markdown format
	results, behaviorMap := tiering.BehaviorsToResults(resolved.Active)
	mapper := tiering.NewActivationTierMapper(tiering.DefaultActivationTierConfig())
	plan := mapper.MapResults(results, behaviorMap, tokenBudget)

	compiler := assembly.NewCompiler().
		WithFormat(assembly.FormatMarkdown)
	compiled := compiler.CompileTiered(plan)

	if compiled.Text == "" {
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), compiled.Text)
	return nil
}

// runHookActivate runs the spreading activation pipeline for dynamic context.
// This mirrors the logic in cmd_activate.go's runActivate but is streamlined
// for hook usage (always markdown, silent on errors).
func runHookActivate(cmd *cobra.Command, root, file, task string, tokenBudget int, sessionID string) error {
	// Check initialization silently
	floopDir := filepath.Join(root, ".floop")
	if _, err := os.Stat(floopDir); os.IsNotExist(err) {
		return nil
	}

	// Build context
	ctxBuilder := activation.NewContextBuilder().
		WithRepoRoot(root)
	if file != "" {
		ctxBuilder.WithFile(file)
	}
	if task != "" {
		ctxBuilder.WithTask(task)
	}
	actCtx := ctxBuilder.Build()

	// Open store
	graphStore, err := store.NewMultiGraphStore(root)
	if err != nil {
		return nil
	}
	defer graphStore.Close()

	// Load or create session state
	sessionDir := sessionStateDir(sessionID)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return nil
	}
	sessState, err := session.LoadState(sessionDir)
	if err != nil {
		// On error, create fresh state (don't block the hook)
		sessState = session.NewState(session.DefaultConfig())
	}

	sessState.IncrementPromptCount()

	// Run spreading activation
	ctx := context.Background()
	pipeline := spreading.NewPipeline(graphStore, spreading.DefaultConfig())
	results, err := pipeline.Run(ctx, actCtx)
	if err != nil {
		_ = session.SaveState(sessState, sessionDir)
		return nil
	}

	if len(results) == 0 {
		_ = session.SaveState(sessState, sessionDir)
		return nil
	}

	// Filter through session state
	filtered := sessState.FilterResults(results, activationToTier, estimateTokenCost)
	if len(filtered) == 0 {
		_ = session.SaveState(sessState, sessionDir)
		return nil
	}

	// Load behaviors for output rendering
	behaviorMap, err := loadBehaviorMap(ctx, graphStore)
	if err != nil {
		_ = session.SaveState(sessState, sessionDir)
		return nil
	}

	// Apply token budget
	budgeted := applyTokenBudget(filtered, tokenBudget)
	if len(budgeted) == 0 {
		_ = session.SaveState(sessState, sessionDir)
		return nil
	}

	// Record injections
	for _, fr := range budgeted {
		cost := estimateTokenCost(fr.BehaviorID, fr.Tier)
		sessState.RecordInjection(fr.BehaviorID, fr.Tier, fr.Activation, cost)
	}
	_ = session.SaveState(sessState, sessionDir)

	// Build trigger reason and output markdown
	triggerReason := buildTriggerReason(file, task)
	return outputMarkdown(cmd, budgeted, behaviorMap, triggerReason)
}

// detectTaskFromCommand detects the task type from a bash command string.
func detectTaskFromCommand(command string) string {
	switch {
	case strings.HasPrefix(command, "git commit"),
		strings.HasPrefix(command, "git push"),
		strings.HasPrefix(command, "git merge"):
		return "committing"
	case strings.HasPrefix(command, "git "):
		return "git-operations"
	case strings.HasPrefix(command, "go test"),
		strings.HasPrefix(command, "pytest"),
		strings.HasPrefix(command, "npm test"),
		strings.HasPrefix(command, "jest"):
		return "testing"
	case strings.HasPrefix(command, "go build"),
		strings.HasPrefix(command, "npm run build"),
		strings.HasPrefix(command, "make"):
		return "building"
	case strings.HasPrefix(command, "docker"),
		strings.HasPrefix(command, "kubectl"):
		return "deployment"
	default:
		return ""
	}
}

// extractFilePath extracts the file path from tool input, trying both
// "file_path" and "path" keys (matching Claude Code's tool schemas).
func extractFilePath(toolInput map[string]interface{}) string {
	if fp, ok := toolInput["file_path"].(string); ok && fp != "" {
		return fp
	}
	if p, ok := toolInput["path"].(string); ok && p != "" {
		return p
	}
	return ""
}
