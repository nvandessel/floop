package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/activation"
	"github.com/nvandessel/feedback-loop/internal/config"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/session"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

// newActivateCmd creates the 'activate' command.
// This command runs the spreading activation pipeline for the given context
// and outputs behaviors that should be injected, respecting session state.
func newActivateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activate",
		Short: "Run spreading activation for dynamic context injection",
		Long: `Evaluates the behavior graph using spreading activation and returns new
or upgraded behaviors for injection. Respects session state to prevent
re-injection spam.

This command is designed to be called from Claude Code hooks on
PreToolUse events (Read, Bash) to dynamically surface relevant
behaviors as the agent's work context evolves.

Examples:
  floop activate --file main.go
  floop activate --task testing --token-budget 500
  floop activate --file main.py --session-id abc123 --json`,
		RunE: runActivate,
	}

	cmd.Flags().String("file", "", "File path for context")
	cmd.Flags().String("task", "", "Task type for context")
	cmd.Flags().String("format", "markdown", "Output format: markdown, json")
	cmd.Flags().Int("token-budget", config.Default().TokenBudget.DynamicContext, "Token budget for this injection")
	cmd.Flags().String("session-id", "default", "Session ID for state tracking")

	return cmd
}

// runActivate executes the activate command logic.
func runActivate(cmd *cobra.Command, args []string) error {
	root, _ := cmd.Flags().GetString("root")
	file, _ := cmd.Flags().GetString("file")
	task, _ := cmd.Flags().GetString("task")
	format, _ := cmd.Flags().GetString("format")
	tokenBudget, _ := cmd.Flags().GetInt("token-budget")
	sessionID, _ := cmd.Flags().GetString("session-id")
	jsonOut, _ := cmd.Flags().GetBool("json")

	// Must have at least one context signal
	if file == "" && task == "" {
		return nil // nothing to activate on
	}

	// Check initialization
	floopDir := filepath.Join(root, ".floop")
	if _, err := os.Stat(floopDir); os.IsNotExist(err) {
		return nil // silently exit if not initialized (hook context)
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

	// Open store (both local and global)
	graphStore, err := store.NewMultiGraphStore(root)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer graphStore.Close()

	// Load or create session state
	sessionDir := sessionStateDir(sessionID)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return fmt.Errorf("creating session state dir: %w", err)
	}
	sessState, err := session.LoadState(sessionDir)
	if err != nil {
		// On error, create fresh state (don't block the hook)
		sessState = session.NewState(session.DefaultConfig())
	}

	sessState.IncrementPromptCount()

	// Run spreading activation pipeline
	ctx := context.Background()
	pipeline := spreading.NewPipeline(graphStore, spreading.DefaultConfig())
	results, err := pipeline.Run(ctx, actCtx)
	if err != nil {
		return fmt.Errorf("spreading activation: %w", err)
	}

	if len(results) == 0 {
		// Save state (prompt count) even if no results
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
		return fmt.Errorf("loading behaviors: %w", err)
	}

	// Apply token budget
	budgeted := applyTokenBudget(filtered, tokenBudget)
	if len(budgeted) == 0 {
		_ = session.SaveState(sessState, sessionDir)
		return nil
	}

	// Record injections in session state
	for _, fr := range budgeted {
		cost := estimateTokenCost(fr.BehaviorID, fr.Tier)
		sessState.RecordInjection(fr.BehaviorID, fr.Tier, fr.Activation, cost)
	}

	// Save session state
	_ = session.SaveState(sessState, sessionDir)

	// Build trigger reason
	triggerReason := buildTriggerReason(file, task)

	// Output
	if jsonOut || format == "json" {
		return outputJSON(cmd, budgeted, behaviorMap, triggerReason)
	}
	return outputMarkdown(cmd, budgeted, behaviorMap, triggerReason)
}

// sessionStateDir returns the directory for persisting session state.
// Session state is stored under ~/.floop/sessions/ with owner-only permissions
// to avoid predictable temp paths in world-readable directories.
func sessionStateDir(sessionID string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fall back to os.TempDir if home is unavailable
		return filepath.Join(os.TempDir(), fmt.Sprintf("floop-session-%s", sessionID))
	}
	return filepath.Join(homeDir, ".floop", "sessions", fmt.Sprintf("floop-session-%s", sessionID))
}

// activationToTier maps an activation level to an injection tier.
func activationToTier(activation float64) models.InjectionTier {
	switch {
	case activation >= 0.7:
		return models.TierFull
	case activation >= 0.4:
		return models.TierSummary
	default:
		return models.TierNameOnly
	}
}

// estimateTokenCost provides a rough token estimate for a behavior at a given tier.
func estimateTokenCost(behaviorID string, tier models.InjectionTier) int {
	switch tier {
	case models.TierFull:
		return 80
	case models.TierSummary:
		return 30
	case models.TierNameOnly:
		return 10
	default:
		return 0
	}
}

// loadBehaviorMap loads all behaviors into a map keyed by ID.
func loadBehaviorMap(ctx context.Context, graphStore store.GraphStore) (map[string]models.Behavior, error) {
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("querying behaviors: %w", err)
	}

	bMap := make(map[string]models.Behavior, len(nodes))
	for _, node := range nodes {
		b := learning.NodeToBehavior(node)
		bMap[b.ID] = b
	}
	return bMap, nil
}

// applyTokenBudget trims the filtered results to fit within the token budget.
func applyTokenBudget(filtered []session.FilteredResult, budget int) []session.FilteredResult {
	if budget <= 0 {
		return filtered
	}

	var result []session.FilteredResult
	used := 0
	for _, fr := range filtered {
		cost := estimateTokenCost(fr.BehaviorID, fr.Tier)
		if used+cost > budget {
			break
		}
		result = append(result, fr)
		used += cost
	}
	return result
}

// buildTriggerReason creates a human-readable reason for the activation.
func buildTriggerReason(file, task string) string {
	if file != "" {
		ext := filepath.Ext(file)
		if ext != "" {
			return fmt.Sprintf("file change to `*%s`", ext)
		}
		return fmt.Sprintf("file `%s`", filepath.Base(file))
	}
	if task != "" {
		return fmt.Sprintf("task: `%s`", task)
	}
	return "context change"
}

// outputJSON writes the activation results as JSON.
func outputJSON(cmd *cobra.Command, results []session.FilteredResult, behaviorMap map[string]models.Behavior, triggerReason string) error {
	type jsonBehavior struct {
		BehaviorID  string               `json:"behavior_id"`
		Name        string               `json:"name"`
		Kind        string               `json:"kind"`
		Activation  float64              `json:"activation"`
		Tier        models.InjectionTier `json:"tier"`
		TierName    string               `json:"tier_name"`
		Content     string               `json:"content"`
		IsUpgrade   bool                 `json:"is_upgrade,omitempty"`
		IsReinforce bool                 `json:"is_reinforce,omitempty"`
	}

	behaviors := make([]jsonBehavior, 0, len(results))
	for _, fr := range results {
		jb := jsonBehavior{
			BehaviorID:  fr.BehaviorID,
			Activation:  fr.Activation,
			Tier:        fr.Tier,
			TierName:    fr.Tier.String(),
			IsUpgrade:   fr.IsUpgrade,
			IsReinforce: fr.IsReinforce,
		}
		if b, ok := behaviorMap[fr.BehaviorID]; ok {
			jb.Name = b.Name
			jb.Kind = string(b.Kind)
			jb.Content = behaviorContent(b, fr.Tier)
		}
		behaviors = append(behaviors, jb)
	}

	output := map[string]interface{}{
		"trigger":   triggerReason,
		"behaviors": behaviors,
		"count":     len(behaviors),
		"scope":     "local",
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// outputMarkdown writes the activation results as markdown.
func outputMarkdown(cmd *cobra.Command, results []session.FilteredResult, behaviorMap map[string]models.Behavior, triggerReason string) error {
	var sb strings.Builder

	sb.WriteString("## Dynamic Context Update\n\n")
	sb.WriteString(fmt.Sprintf("_Activated by: %s_\n\n", triggerReason))

	// Group by kind
	directives := make([]session.FilteredResult, 0)
	constraints := make([]session.FilteredResult, 0)
	procedures := make([]session.FilteredResult, 0)
	preferences := make([]session.FilteredResult, 0)

	for _, fr := range results {
		b, ok := behaviorMap[fr.BehaviorID]
		if !ok {
			continue
		}
		switch b.Kind {
		case models.BehaviorKindDirective:
			directives = append(directives, fr)
		case models.BehaviorKindConstraint:
			constraints = append(constraints, fr)
		case models.BehaviorKindProcedure:
			procedures = append(procedures, fr)
		case models.BehaviorKindPreference:
			preferences = append(preferences, fr)
		default:
			directives = append(directives, fr)
		}
	}

	writeSection(&sb, "Directives", directives, behaviorMap)
	writeSection(&sb, "Constraints", constraints, behaviorMap)
	writeSection(&sb, "Procedures", procedures, behaviorMap)
	writeSection(&sb, "Preferences", preferences, behaviorMap)

	output := sb.String()
	if strings.TrimSpace(output) == "## Dynamic Context Update\n\n_Activated by: "+triggerReason+"_" {
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), output)
	return nil
}

// writeSection writes a markdown section for a group of behaviors.
func writeSection(sb *strings.Builder, title string, results []session.FilteredResult, behaviorMap map[string]models.Behavior) {
	if len(results) == 0 {
		return
	}

	sb.WriteString(fmt.Sprintf("### %s\n", title))
	for _, fr := range results {
		b, ok := behaviorMap[fr.BehaviorID]
		if !ok {
			continue
		}
		content := behaviorContent(b, fr.Tier)
		sb.WriteString(fmt.Sprintf("- %s\n", content))
	}
	sb.WriteString("\n")
}

// behaviorContent returns the appropriate content for a behavior at the given tier.
func behaviorContent(b models.Behavior, tier models.InjectionTier) string {
	switch tier {
	case models.TierFull:
		if b.Content.Canonical != "" {
			return b.Content.Canonical
		}
		return b.Name
	case models.TierSummary:
		if b.Content.Summary != "" {
			return b.Content.Summary
		}
		if b.Content.Canonical != "" {
			return b.Content.Canonical
		}
		return b.Name
	case models.TierNameOnly:
		return b.Name
	default:
		return b.Name
	}
}
