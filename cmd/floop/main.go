package main

import (
	"bufio"
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
	"github.com/nvandessel/feedback-loop/internal/dedup"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var version = "0.1.0-dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "floop",
		Short: "Feedback loop - behavior learning for AI agents",
		Long: `floop manages learned behaviors and conventions for AI coding agents.

It captures corrections, extracts reusable behaviors, and provides
context-aware behavior activation for consistent agent operation.`,
	}

	// Global flags
	rootCmd.PersistentFlags().Bool("json", false, "Output as JSON (for agent consumption)")
	rootCmd.PersistentFlags().String("root", ".", "Project root directory")

	// Add subcommands
	rootCmd.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newLearnCmd(),
		newReprocessCmd(),
		newListCmd(),
		newActiveCmd(),
		newShowCmd(),
		newWhyCmd(),
		newPromptCmd(),
		newMCPServerCmd(),
		// Curation commands
		newForgetCmd(),
		newDeprecateCmd(),
		newRestoreCmd(),
		newMergeCmd(),
		// Management commands
		newDeduplicateCmd(),
		newConfigCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]string{"version": version})
			} else {
				fmt.Printf("floop version %s\n", version)
			}
		},
	}
}

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize feedback loop tracking in current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			globalInit, _ := cmd.Flags().GetBool("global")

			var floopDir string
			if globalInit {
				// Initialize global directory
				if err := store.EnsureGlobalFloopDir(); err != nil {
					return fmt.Errorf("failed to initialize global directory: %w", err)
				}
				var err error
				floopDir, err = store.GlobalFloopPath()
				if err != nil {
					return fmt.Errorf("failed to get global path: %w", err)
				}
			} else {
				// Initialize local directory (default)
				floopDir = filepath.Join(root, ".floop")
			}

			// Create .floop directory
			if err := os.MkdirAll(floopDir, 0755); err != nil {
				return fmt.Errorf("failed to create .floop directory: %w", err)
			}

			// Create manifest.yaml
			manifestPath := filepath.Join(floopDir, "manifest.yaml")
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				manifest := `# Feedback Loop Manifest
version: "1.0"
created: %s

# Behaviors learned from corrections are stored in this directory
# Run 'floop list' to see all behaviors
# Run 'floop active' to see behaviors active in current context
`
				content := fmt.Sprintf(manifest, time.Now().Format(time.RFC3339))
				if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
					return fmt.Errorf("failed to create manifest.yaml: %w", err)
				}
			}

			// Create corrections log for dogfooding
			correctionsPath := filepath.Join(floopDir, "corrections.jsonl")
			if _, err := os.Stat(correctionsPath); os.IsNotExist(err) {
				if err := os.WriteFile(correctionsPath, []byte{}, 0644); err != nil {
					return fmt.Errorf("failed to create corrections.jsonl: %w", err)
				}
			}

			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				result := map[string]string{
					"status": "initialized",
					"path":   floopDir,
				}
				if globalInit {
					result["scope"] = "global"
				}
				json.NewEncoder(os.Stdout).Encode(result)
			} else {
				if globalInit {
					fmt.Printf("Initialized global .floop/ at %s\n", floopDir)
				} else {
					fmt.Printf("Initialized .floop/ in %s\n", root)
				}
			}

			return nil
		},
	}

	cmd.Flags().Bool("global", false, "Initialize global user directory (~/.floop/) instead of local project directory")

	return cmd
}

func newLearnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "learn",
		Short: "Capture a correction and extract behavior",
		Long: `Capture a correction from a human-agent interaction and extract a behavior.

This command is called by agents when they receive a correction.
It records the correction, extracts a candidate behavior, and determines
whether the behavior can be auto-accepted or requires human review.

Example:
  floop learn --wrong "used os.path" --right "use pathlib.Path instead"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			wrong, _ := cmd.Flags().GetString("wrong")
			right, _ := cmd.Flags().GetString("right")
			file, _ := cmd.Flags().GetString("file")
			task, _ := cmd.Flags().GetString("task")
			root, _ := cmd.Flags().GetString("root")
			scope, _ := cmd.Flags().GetString("scope")

			// Build context snapshot
			now := time.Now()
			ctxSnapshot := models.ContextSnapshot{
				Timestamp: now,
				FilePath:  file,
				Task:      task,
			}
			if file != "" {
				ctxSnapshot.FileLanguage = models.InferLanguage(file)
				ctxSnapshot.FileExt = filepath.Ext(file)
			}

			// Create correction using models.Correction
			correction := models.Correction{
				ID:              fmt.Sprintf("c-%d", now.UnixNano()),
				Timestamp:       now,
				Context:         ctxSnapshot,
				AgentAction:     wrong,
				CorrectedAction: right,
				Processed:       false,
			}

			// Ensure .floop exists
			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Parse scope and convert to StoreScope
			storeScope := store.ScopeLocal
			switch scope {
			case "global":
				storeScope = store.ScopeGlobal
			case "both":
				storeScope = store.ScopeBoth
			case "local":
				storeScope = store.ScopeLocal
			default:
				return fmt.Errorf("invalid scope: %s (must be local, global, or both)", scope)
			}

			// Use persistent graph store with MultiGraphStore
			graphStore, err := store.NewMultiGraphStore(root, storeScope)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			// Process through learning loop
			loop := learning.NewLearningLoop(graphStore, nil)
			ctx := context.Background()

			result, err := loop.ProcessCorrection(ctx, correction)
			if err != nil {
				return fmt.Errorf("failed to process correction: %w", err)
			}

			// Mark correction as processed
			correction.Processed = true
			processedAt := time.Now()
			correction.ProcessedAt = &processedAt

			// Append to corrections log (after processing so Processed flag is correct)
			correctionsPath := filepath.Join(floopDir, "corrections.jsonl")
			f, err := os.OpenFile(correctionsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("failed to open corrections log: %w", err)
			}
			defer f.Close()

			if err := json.NewEncoder(f).Encode(correction); err != nil {
				return fmt.Errorf("failed to write correction: %w", err)
			}

			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":          "processed",
					"correction":      correction,
					"behavior":        result.CandidateBehavior,
					"placement":       result.Placement,
					"auto_accepted":   result.AutoAccepted,
					"requires_review": result.RequiresReview,
					"review_reasons":  result.ReviewReasons,
				})
			} else {
				fmt.Println("Correction captured and processed:")
				fmt.Printf("  Wrong: %s\n", correction.AgentAction)
				fmt.Printf("  Right: %s\n", correction.CorrectedAction)
				if correction.Context.FilePath != "" {
					fmt.Printf("  File:  %s\n", correction.Context.FilePath)
				}
				if correction.Context.Task != "" {
					fmt.Printf("  Task:  %s\n", correction.Context.Task)
				}
				fmt.Println()
				fmt.Println("Extracted behavior:")
				fmt.Printf("  ID:   %s\n", result.CandidateBehavior.ID)
				fmt.Printf("  Name: %s\n", result.CandidateBehavior.Name)
				fmt.Printf("  Kind: %s\n", result.CandidateBehavior.Kind)
				fmt.Println()
				if result.AutoAccepted {
					fmt.Println("Status: Auto-accepted")
				} else if result.RequiresReview {
					fmt.Println("Status: Requires review")
					for _, reason := range result.ReviewReasons {
						fmt.Printf("  - %s\n", reason)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().String("wrong", "", "What the agent did (required)")
	cmd.Flags().String("right", "", "What should have been done (required)")
	cmd.Flags().String("file", "", "Current file path")
	cmd.Flags().String("task", "", "Current task type")
	cmd.Flags().String("scope", "local", "Where to save: local (project), global (user), or both")
	cmd.MarkFlagRequired("wrong")
	cmd.MarkFlagRequired("right")

	return cmd
}

func newReprocessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reprocess",
		Short: "Reprocess orphaned corrections into behaviors",
		Long: `Reprocess corrections that were captured before behavior extraction was implemented.

This command reads all corrections from corrections.jsonl, identifies those that
haven't been processed (no corresponding behavior exists), and runs them through
the learning loop to extract behaviors.

Example:
  floop reprocess           # Reprocess local corrections
  floop reprocess --dry-run # Preview what would be processed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			scope, _ := cmd.Flags().GetString("scope")

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Read corrections file
			correctionsPath := filepath.Join(floopDir, "corrections.jsonl")
			data, err := os.ReadFile(correctionsPath)
			if err != nil {
				if os.IsNotExist(err) {
					if jsonOut {
						json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
							"status":    "no_corrections",
							"processed": 0,
							"skipped":   0,
						})
					} else {
						fmt.Println("No corrections file found.")
					}
					return nil
				}
				return fmt.Errorf("failed to read corrections: %w", err)
			}

			// Parse all corrections
			var corrections []models.Correction
			lines := splitLines(string(data))
			for _, line := range lines {
				if line == "" {
					continue
				}
				var c models.Correction
				if err := json.Unmarshal([]byte(line), &c); err != nil {
					continue
				}
				corrections = append(corrections, c)
			}

			// Filter to unprocessed corrections
			var unprocessed []models.Correction
			for _, c := range corrections {
				if !c.Processed {
					unprocessed = append(unprocessed, c)
				}
			}

			if len(unprocessed) == 0 {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"status":    "all_processed",
						"processed": 0,
						"skipped":   len(corrections),
					})
				} else {
					fmt.Printf("All %d corrections have already been processed.\n", len(corrections))
				}
				return nil
			}

			if dryRun {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"status":            "dry_run",
						"would_process":     len(unprocessed),
						"already_processed": len(corrections) - len(unprocessed),
						"corrections":       unprocessed,
					})
				} else {
					fmt.Printf("Dry run: would process %d unprocessed corrections (out of %d total)\n\n",
						len(unprocessed), len(corrections))
					for i, c := range unprocessed {
						fmt.Printf("%d. [%s]\n", i+1, c.Timestamp.Format(time.RFC3339))
						fmt.Printf("   Wrong: %s\n", c.AgentAction)
						fmt.Printf("   Right: %s\n", c.CorrectedAction)
						fmt.Println()
					}
				}
				return nil
			}

			// Parse scope
			storeScope := store.ScopeLocal
			switch scope {
			case "global":
				storeScope = store.ScopeGlobal
			case "both":
				storeScope = store.ScopeBoth
			case "local":
				storeScope = store.ScopeLocal
			default:
				return fmt.Errorf("invalid scope: %s (must be local, global, or both)", scope)
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, storeScope)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			// Process through learning loop
			loop := learning.NewLearningLoop(graphStore, nil)
			ctx := context.Background()

			var processed []models.Correction
			var results []map[string]interface{}

			for i := range corrections {
				c := &corrections[i]
				if c.Processed {
					continue
				}

				result, err := loop.ProcessCorrection(ctx, *c)
				if err != nil {
					if !jsonOut {
						fmt.Fprintf(os.Stderr, "Warning: failed to process correction %s: %v\n", c.ID, err)
					}
					continue
				}

				// Mark as processed
				c.Processed = true
				now := time.Now()
				c.ProcessedAt = &now
				processed = append(processed, *c)

				if jsonOut {
					results = append(results, map[string]interface{}{
						"correction_id": c.ID,
						"behavior_id":   result.CandidateBehavior.ID,
						"behavior_name": result.CandidateBehavior.Name,
						"auto_accepted": result.AutoAccepted,
					})
				} else {
					fmt.Printf("Processed: %s -> %s\n", c.CorrectedAction[:min(50, len(c.CorrectedAction))], result.CandidateBehavior.ID)
				}
			}

			// Rewrite corrections file with updated processed flags
			tmpPath := correctionsPath + ".tmp"
			tmpFile, err := os.Create(tmpPath)
			if err != nil {
				return fmt.Errorf("failed to create temp file: %w", err)
			}

			encoder := json.NewEncoder(tmpFile)
			for _, c := range corrections {
				if err := encoder.Encode(c); err != nil {
					tmpFile.Close()
					os.Remove(tmpPath)
					return fmt.Errorf("failed to write correction: %w", err)
				}
			}
			tmpFile.Close()

			if err := os.Rename(tmpPath, correctionsPath); err != nil {
				return fmt.Errorf("failed to update corrections file: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":    "completed",
					"processed": len(processed),
					"skipped":   len(corrections) - len(processed),
					"results":   results,
				})
			} else {
				fmt.Printf("\nReprocessed %d corrections into behaviors.\n", len(processed))
				fmt.Printf("Skipped %d already-processed corrections.\n", len(corrections)-len(unprocessed))
			}

			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show what would be processed without making changes")
	cmd.Flags().String("scope", "local", "Where to save behaviors: local, global, or both")

	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List behaviors or corrections",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			showCorrections, _ := cmd.Flags().GetBool("corrections")
			globalFlag, _ := cmd.Flags().GetBool("global")
			allFlag, _ := cmd.Flags().GetBool("all")

			// Validate flag combinations
			if globalFlag && allFlag {
				return fmt.Errorf("cannot specify both --global and --all")
			}

			// Determine scope
			scope := store.ScopeLocal
			if globalFlag {
				scope = store.ScopeGlobal
			} else if allFlag {
				scope = store.ScopeBoth
			}

			// Check initialization based on scope
			if scope == store.ScopeLocal || scope == store.ScopeBoth {
				floopDir := filepath.Join(root, ".floop")
				if _, err := os.Stat(floopDir); os.IsNotExist(err) {
					if jsonOut {
						json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]interface{}{
							"error": "local .floop not initialized",
						})
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), "Local .floop not initialized. Run 'floop init' first.")
					}
					return nil
				}
			}

			if scope == store.ScopeGlobal || scope == store.ScopeBoth {
				globalPath, err := store.GlobalFloopPath()
				if err == nil {
					if _, err := os.Stat(globalPath); os.IsNotExist(err) {
						if scope == store.ScopeGlobal {
							if jsonOut {
								json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]interface{}{
									"error": "global .floop not initialized",
								})
							} else {
								fmt.Fprintln(cmd.OutOrStdout(), "Global .floop not initialized. Run 'floop init --global' first.")
							}
							return nil
						}
					}
				}
			}

			if showCorrections {
				return listCorrections(root, jsonOut)
			}

			// Load behaviors from appropriate store(s)
			behaviors, err := loadBehaviorsWithScope(root, scope)
			if err != nil {
				return fmt.Errorf("failed to load behaviors: %w", err)
			}

			if jsonOut {
				result := map[string]interface{}{
					"behaviors": behaviors,
					"count":     len(behaviors),
				}
				if globalFlag {
					result["scope"] = "global"
				} else if allFlag {
					result["scope"] = "all"
				} else {
					result["scope"] = "local"
				}
				json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				if len(behaviors) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No behaviors learned yet.")
					fmt.Fprintln(cmd.OutOrStdout(), "\nUse 'floop learn --wrong \"X\" --right \"Y\"' to capture corrections.")
					return nil
				}

				// Show scope in header
				scopeStr := "local"
				if globalFlag {
					scopeStr = "global"
				} else if allFlag {
					scopeStr = "all (local + global)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Learned behaviors - %s (%d):\n\n", scopeStr, len(behaviors))

				for i, b := range behaviors {
					fmt.Fprintf(cmd.OutOrStdout(), "%d. [%s] %s\n", i+1, b.Kind, b.Name)
					fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", b.Content.Canonical)
					if len(b.When) > 0 {
						fmt.Fprintf(cmd.OutOrStdout(), "   When: %v\n", b.When)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "   Confidence: %.2f\n", b.Confidence)
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}

			return nil
		},
	}

	cmd.Flags().Bool("corrections", false, "Show captured corrections instead of behaviors")
	cmd.Flags().Bool("global", false, "Show behaviors from global user store (~/.floop/) only")
	cmd.Flags().Bool("all", false, "Show behaviors from both local and global stores")

	return cmd
}

func listCorrections(root string, jsonOut bool) error {
	correctionsPath := filepath.Join(root, ".floop", "corrections.jsonl")

	data, err := os.ReadFile(correctionsPath)
	if err != nil {
		if os.IsNotExist(err) {
			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"corrections": []models.Correction{},
					"count":       0,
				})
			} else {
				fmt.Println("No corrections captured yet.")
			}
			return nil
		}
		return err
	}

	// Parse JSONL into models.Correction
	var corrections []models.Correction
	lines := splitLines(string(data))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var c models.Correction
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		corrections = append(corrections, c)
	}

	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"corrections": corrections,
			"count":       len(corrections),
		})
	} else {
		if len(corrections) == 0 {
			fmt.Println("No corrections captured yet.")
			return nil
		}
		fmt.Printf("Captured corrections (%d):\n\n", len(corrections))
		for i, c := range corrections {
			fmt.Printf("%d. [%s]\n", i+1, c.Timestamp.Format(time.RFC3339))
			fmt.Printf("   Wrong: %s\n", c.AgentAction)
			fmt.Printf("   Right: %s\n", c.CorrectedAction)
			if c.Context.FilePath != "" {
				fmt.Printf("   File:  %s\n", c.Context.FilePath)
			}
			fmt.Println()
		}
	}

	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// loadBehaviors loads behaviors from the persistent graph store.
func loadBehaviors(floopDir string) ([]models.Behavior, error) {
	// Get the project root from the floop directory
	projectRoot := filepath.Dir(floopDir)

	// Open the graph store
	graphStore, err := store.NewBeadsGraphStore(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to open graph store: %w", err)
	}
	defer graphStore.Close()

	// Query all behavior nodes
	ctx := context.Background()
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("failed to query behaviors: %w", err)
	}

	// Convert nodes to behaviors
	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		b := learning.NodeToBehavior(node)
		behaviors = append(behaviors, b)
	}

	return behaviors, nil
}

// loadBehaviorsWithScope loads behaviors from the specified scope (local, global, or both).
func loadBehaviorsWithScope(projectRoot string, scope store.StoreScope) ([]models.Behavior, error) {
	ctx := context.Background()
	var graphStore store.GraphStore
	var err error

	switch scope {
	case store.ScopeLocal:
		// Load from local store only
		graphStore, err = store.NewBeadsGraphStore(projectRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to open local store: %w", err)
		}

	case store.ScopeGlobal:
		// Load from global store only
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		graphStore, err = store.NewBeadsGraphStore(homeDir)
		if err != nil {
			return nil, fmt.Errorf("failed to open global store: %w", err)
		}

	case store.ScopeBoth:
		// Load from both stores using MultiGraphStore
		graphStore, err = store.NewMultiGraphStore(projectRoot, store.ScopeLocal)
		if err != nil {
			return nil, fmt.Errorf("failed to open multi-store: %w", err)
		}

	default:
		return nil, fmt.Errorf("invalid scope: %s", scope)
	}

	defer graphStore.Close()

	// Query all behavior nodes
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("failed to query behaviors: %w", err)
	}

	// Convert nodes to behaviors
	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		b := learning.NodeToBehavior(node)
		behaviors = append(behaviors, b)
	}

	return behaviors, nil
}

func newActiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "active",
		Short: "Show behaviors active in current context",
		Long: `List all behaviors that are currently active based on the
current context (file, task, language, etc.).

Use --json for machine-readable output suitable for agent consumption.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			file, _ := cmd.Flags().GetString("file")
			task, _ := cmd.Flags().GetString("task")
			env, _ := cmd.Flags().GetString("env")
			jsonOut, _ := cmd.Flags().GetBool("json")

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": ".floop not initialized",
					})
				} else {
					fmt.Println("Not initialized. Run 'floop init' first.")
				}
				return nil
			}

			// Load all behaviors from both local and global stores
			behaviors, err := loadBehaviorsWithScope(root, store.ScopeBoth)
			if err != nil {
				return fmt.Errorf("failed to load behaviors: %w", err)
			}

			// Build context
			ctxBuilder := activation.NewContextBuilder().
				WithFile(file).
				WithTask(task).
				WithEnvironment(env).
				WithRepoRoot(root)
			ctx := ctxBuilder.Build()

			// Evaluate which behaviors are active
			evaluator := activation.NewEvaluator()
			matches := evaluator.Evaluate(ctx, behaviors)

			// Resolve conflicts
			resolver := activation.NewResolver()
			result := resolver.Resolve(matches)

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"context":    ctx,
					"active":     result.Active,
					"overridden": result.Overridden,
					"excluded":   result.Excluded,
					"count":      len(result.Active),
				})
			} else {
				fmt.Printf("Context:\n")
				if ctx.FilePath != "" {
					fmt.Printf("  File: %s\n", ctx.FilePath)
				}
				if ctx.FileLanguage != "" {
					fmt.Printf("  Language: %s\n", ctx.FileLanguage)
				}
				if ctx.Task != "" {
					fmt.Printf("  Task: %s\n", ctx.Task)
				}
				if ctx.Branch != "" {
					fmt.Printf("  Branch: %s\n", ctx.Branch)
				}
				fmt.Println()

				if len(result.Active) == 0 {
					fmt.Println("No active behaviors for this context.")
					if len(behaviors) > 0 {
						fmt.Printf("\n(%d behaviors exist but none match current context)\n", len(behaviors))
					}
					return nil
				}

				fmt.Printf("Active behaviors (%d):\n\n", len(result.Active))
				for i, b := range result.Active {
					fmt.Printf("%d. [%s] %s\n", i+1, b.Kind, b.Name)
					fmt.Printf("   %s\n", b.Content.Canonical)
					if len(b.When) > 0 {
						fmt.Printf("   When: %v\n", b.When)
					}
					fmt.Println()
				}

				if len(result.Overridden) > 0 {
					fmt.Printf("Overridden behaviors (%d):\n", len(result.Overridden))
					for _, o := range result.Overridden {
						fmt.Printf("  - %s (by %s)\n", o.Behavior.Name, o.OverrideBy)
					}
					fmt.Println()
				}

				if len(result.Excluded) > 0 {
					fmt.Printf("Excluded due to conflicts (%d):\n", len(result.Excluded))
					for _, e := range result.Excluded {
						fmt.Printf("  - %s (conflicts with %s)\n", e.Behavior.Name, e.ConflictsWith)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().String("file", "", "Current file path")
	cmd.Flags().String("task", "", "Current task type")
	cmd.Flags().String("env", "", "Environment (dev, staging, prod)")

	return cmd
}

func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show [behavior-id]",
		Short: "Show details of a behavior",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			id := args[0]

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": ".floop not initialized",
					})
				} else {
					fmt.Println("Not initialized. Run 'floop init' first.")
				}
				return nil
			}

			// Load all behaviors from both local and global stores
			behaviors, err := loadBehaviorsWithScope(root, store.ScopeBoth)
			if err != nil {
				return fmt.Errorf("failed to load behaviors: %w", err)
			}

			// Find the behavior
			var found *models.Behavior
			for _, b := range behaviors {
				if b.ID == id || b.Name == id {
					found = &b
					break
				}
			}

			if found == nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "behavior not found",
						"id":    id,
					})
				} else {
					fmt.Printf("Behavior not found: %s\n", id)
				}
				return nil
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(found)
			} else {
				fmt.Printf("Behavior: %s\n", found.ID)
				fmt.Printf("Name: %s\n", found.Name)
				fmt.Printf("Kind: %s\n", found.Kind)
				fmt.Printf("Confidence: %.2f\n", found.Confidence)
				fmt.Printf("Priority: %d\n", found.Priority)
				fmt.Println()

				fmt.Println("Content:")
				fmt.Printf("  Canonical: %s\n", found.Content.Canonical)
				if found.Content.Expanded != "" {
					fmt.Printf("  Expanded: %s\n", found.Content.Expanded)
				}
				if len(found.Content.Structured) > 0 {
					fmt.Printf("  Structured: %v\n", found.Content.Structured)
				}
				fmt.Println()

				if len(found.When) > 0 {
					fmt.Println("Activation conditions:")
					for k, v := range found.When {
						fmt.Printf("  %s: %v\n", k, v)
					}
					fmt.Println()
				}

				fmt.Println("Provenance:")
				fmt.Printf("  Source: %s\n", found.Provenance.SourceType)
				fmt.Printf("  Created: %s\n", found.Provenance.CreatedAt.Format(time.RFC3339))
				if found.Provenance.CorrectionID != "" {
					fmt.Printf("  Correction: %s\n", found.Provenance.CorrectionID)
				}
				if found.Provenance.ApprovedBy != "" {
					fmt.Printf("  Approved by: %s\n", found.Provenance.ApprovedBy)
				}
				fmt.Println()

				if len(found.Requires) > 0 {
					fmt.Printf("Requires: %v\n", found.Requires)
				}
				if len(found.Overrides) > 0 {
					fmt.Printf("Overrides: %v\n", found.Overrides)
				}
				if len(found.Conflicts) > 0 {
					fmt.Printf("Conflicts: %v\n", found.Conflicts)
				}
			}

			return nil
		},
	}

	return cmd
}

func newWhyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "why [behavior-id]",
		Short: "Explain why a behavior is or isn't active",
		Long: `Show the activation status of a behavior and explain why.

This helps debug when a behavior isn't being applied as expected.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			file, _ := cmd.Flags().GetString("file")
			task, _ := cmd.Flags().GetString("task")
			env, _ := cmd.Flags().GetString("env")
			jsonOut, _ := cmd.Flags().GetBool("json")
			id := args[0]

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": ".floop not initialized",
					})
				} else {
					fmt.Println("Not initialized. Run 'floop init' first.")
				}
				return nil
			}

			// Load all behaviors from both local and global stores
			behaviors, err := loadBehaviorsWithScope(root, store.ScopeBoth)
			if err != nil {
				return fmt.Errorf("failed to load behaviors: %w", err)
			}

			// Find the behavior
			var found *models.Behavior
			for _, b := range behaviors {
				if b.ID == id || b.Name == id {
					found = &b
					break
				}
			}

			if found == nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "behavior not found",
						"id":    id,
					})
				} else {
					fmt.Printf("Behavior not found: %s\n", id)
				}
				return nil
			}

			// Build context
			ctxBuilder := activation.NewContextBuilder().
				WithFile(file).
				WithTask(task).
				WithEnvironment(env).
				WithRepoRoot(root)
			ctx := ctxBuilder.Build()

			// Get explanation
			evaluator := activation.NewEvaluator()
			explanation := evaluator.WhyActive(ctx, *found)

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"behavior":    found,
					"context":     ctx,
					"explanation": explanation,
				})
			} else {
				fmt.Printf("Behavior: %s\n", found.Name)
				fmt.Printf("ID: %s\n", found.ID)
				fmt.Println()

				if explanation.IsActive {
					fmt.Println("Status: ACTIVE")
				} else {
					fmt.Println("Status: NOT ACTIVE")
				}
				fmt.Printf("Reason: %s\n", explanation.Reason)
				fmt.Println()

				if len(explanation.Conditions) > 0 {
					fmt.Println("Condition evaluation:")
					for _, c := range explanation.Conditions {
						status := "✓"
						if !c.Matched {
							status = "✗"
						}
						fmt.Printf("  %s %s: required=%v, actual=%v\n",
							status, c.Field, c.Required, c.Actual)
					}
					fmt.Println()
				}

				fmt.Println("Current context:")
				if ctx.FilePath != "" {
					fmt.Printf("  file_path: %s\n", ctx.FilePath)
				}
				if ctx.FileLanguage != "" {
					fmt.Printf("  language: %s\n", ctx.FileLanguage)
				}
				if ctx.Task != "" {
					fmt.Printf("  task: %s\n", ctx.Task)
				}
				if ctx.Branch != "" {
					fmt.Printf("  branch: %s\n", ctx.Branch)
				}
				if ctx.Environment != "" {
					fmt.Printf("  environment: %s\n", ctx.Environment)
				}
			}

			return nil
		},
	}

	cmd.Flags().String("file", "", "Current file path")
	cmd.Flags().String("task", "", "Current task type")
	cmd.Flags().String("env", "", "Environment (dev, staging, prod)")

	return cmd
}

func newPromptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Generate prompt section from active behaviors",
		Long: `Generate a prompt section containing all active behaviors for the current context.

This command compiles active behaviors into a format suitable for injection into
agent system prompts. Use --max-tokens to limit output size.

Examples:
  floop prompt --file main.go
  floop prompt --file main.go --format xml --max-tokens 500
  floop prompt --file main.go --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			file, _ := cmd.Flags().GetString("file")
			task, _ := cmd.Flags().GetString("task")
			env, _ := cmd.Flags().GetString("env")
			format, _ := cmd.Flags().GetString("format")
			maxTokens, _ := cmd.Flags().GetInt("max-tokens")
			expanded, _ := cmd.Flags().GetBool("expanded")
			jsonOut, _ := cmd.Flags().GetBool("json")

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": ".floop not initialized",
					})
				} else {
					fmt.Println("Not initialized. Run 'floop init' first.")
				}
				return nil
			}

			// Load all behaviors from both local and global stores
			behaviors, err := loadBehaviorsWithScope(root, store.ScopeBoth)
			if err != nil {
				return fmt.Errorf("failed to load behaviors: %w", err)
			}

			// Build context
			ctxBuilder := activation.NewContextBuilder().
				WithFile(file).
				WithTask(task).
				WithEnvironment(env).
				WithRepoRoot(root)
			ctx := ctxBuilder.Build()

			// Evaluate which behaviors are active
			evaluator := activation.NewEvaluator()
			matches := evaluator.Evaluate(ctx, behaviors)

			// Resolve conflicts
			resolver := activation.NewResolver()
			resolved := resolver.Resolve(matches)

			// Optimize if token limit specified
			var activeBehaviors []models.Behavior
			var excluded []models.Behavior

			if maxTokens > 0 {
				optimizer := assembly.NewOptimizer(maxTokens)
				optResult := optimizer.Optimize(resolved.Active)
				activeBehaviors = optResult.Included
				excluded = optResult.Excluded
			} else {
				activeBehaviors = resolved.Active
			}

			// Compile into prompt format
			var outputFormat assembly.Format
			switch format {
			case "xml":
				outputFormat = assembly.FormatXML
			case "plain":
				outputFormat = assembly.FormatPlain
			default:
				outputFormat = assembly.FormatMarkdown
			}

			compiler := assembly.NewCompiler().
				WithFormat(outputFormat).
				WithExpanded(expanded)

			compiled := compiler.Compile(activeBehaviors)

			// Add excluded behaviors info
			for _, e := range excluded {
				compiled.ExcludedBehaviors = append(compiled.ExcludedBehaviors, e.ID)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"context":            ctx,
					"prompt":             compiled.Text,
					"format":             compiled.Format,
					"total_tokens":       compiled.TotalTokens,
					"included_behaviors": compiled.IncludedBehaviors,
					"excluded_behaviors": compiled.ExcludedBehaviors,
					"sections":           compiled.Sections,
				})
			} else {
				if len(activeBehaviors) == 0 {
					fmt.Println("No active behaviors for this context.")
					return nil
				}

				// Print the prompt text directly (for easy copy/paste)
				fmt.Println(compiled.Text)

				// Print stats to stderr so they don't interfere with prompt output
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "---\n")
				fmt.Fprintf(os.Stderr, "Behaviors: %d included", len(compiled.IncludedBehaviors))
				if len(compiled.ExcludedBehaviors) > 0 {
					fmt.Fprintf(os.Stderr, ", %d excluded (token limit)", len(compiled.ExcludedBehaviors))
				}
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Tokens: ~%d\n", compiled.TotalTokens)
			}

			return nil
		},
	}

	cmd.Flags().String("file", "", "Current file path")
	cmd.Flags().String("task", "", "Current task type")
	cmd.Flags().String("env", "", "Environment (dev, staging, prod)")
	cmd.Flags().String("format", "markdown", "Output format (markdown, xml, plain)")
	cmd.Flags().Int("max-tokens", 0, "Maximum tokens (0 = unlimited)")
	cmd.Flags().Bool("expanded", false, "Use expanded content when available")

	return cmd
}

// ============================================================================
// Curation Commands
// ============================================================================

func newForgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forget <behavior-id>",
		Short: "Soft-delete a behavior from active use",
		Long: `Mark a behavior as forgotten, removing it from active use.

The behavior is not deleted, just marked with kind "forgotten-behavior".
Use 'floop restore' to undo this action.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			force, _ := cmd.Flags().GetBool("force")
			reason, _ := cmd.Flags().GetString("reason")
			id := args[0]

			// JSON mode implies force (no interactive prompts)
			if jsonOut {
				force = true
			}

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, store.ScopeLocal)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Find the behavior by ID
			node, err := graphStore.GetNode(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to get behavior: %w", err)
			}
			if node == nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "behavior not found",
						"id":    id,
					})
					return nil
				}
				return fmt.Errorf("behavior not found: %s", id)
			}

			// Verify it's an active behavior
			if node.Kind != "behavior" {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error":        "not an active behavior",
						"id":           id,
						"current_kind": node.Kind,
					})
					return nil
				}
				return fmt.Errorf("not an active behavior (current kind: %s)", node.Kind)
			}

			// Get behavior name for display
			name := id
			if n, ok := node.Content["name"].(string); ok {
				name = n
			}

			// Confirm unless --force
			if !force {
				fmt.Printf("Forget behavior: %s\n", name)
				if reason != "" {
					fmt.Printf("Reason: %s\n", reason)
				}
				fmt.Print("\nConfirm? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				response, _ := reader.ReadString('\n')
				response = strings.TrimSpace(strings.ToLower(response))
				if response != "y" && response != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			// Update node to forgotten state
			now := time.Now()
			if node.Metadata == nil {
				node.Metadata = make(map[string]interface{})
			}
			node.Metadata["original_kind"] = node.Kind
			node.Metadata["forgotten_at"] = now.Format(time.RFC3339)
			node.Metadata["forgotten_by"] = os.Getenv("USER")
			if reason != "" {
				node.Metadata["forget_reason"] = reason
			}
			node.Kind = "forgotten-behavior"

			if err := graphStore.UpdateNode(ctx, *node); err != nil {
				return fmt.Errorf("failed to update behavior: %w", err)
			}

			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync changes: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":     "forgotten",
					"id":         id,
					"name":       name,
					"reason":     reason,
					"restorable": true,
				})
			} else {
				fmt.Printf("Behavior '%s' has been forgotten.\n", name)
				fmt.Println("Use 'floop restore' to undo this action.")
			}

			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Skip confirmation prompt")
	cmd.Flags().String("reason", "", "Reason for forgetting")

	return cmd
}

func newDeprecateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deprecate <behavior-id>",
		Short: "Mark a behavior as deprecated",
		Long: `Mark a behavior as deprecated but keep it visible.

Deprecated behaviors are not active but can be restored.
Use --replacement to link to a newer behavior.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			reason, _ := cmd.Flags().GetString("reason")
			replacement, _ := cmd.Flags().GetString("replacement")
			id := args[0]

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Reason is required
			if reason == "" {
				return fmt.Errorf("--reason is required for deprecation")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, store.ScopeLocal)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Find the behavior by ID
			node, err := graphStore.GetNode(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to get behavior: %w", err)
			}
			if node == nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "behavior not found",
						"id":    id,
					})
					return nil
				}
				return fmt.Errorf("behavior not found: %s", id)
			}

			// Verify it's an active behavior
			if node.Kind != "behavior" {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error":        "not an active behavior",
						"id":           id,
						"current_kind": node.Kind,
					})
					return nil
				}
				return fmt.Errorf("not an active behavior (current kind: %s)", node.Kind)
			}

			// Verify replacement exists if specified
			if replacement != "" {
				replNode, err := graphStore.GetNode(ctx, replacement)
				if err != nil {
					return fmt.Errorf("failed to get replacement behavior: %w", err)
				}
				if replNode == nil {
					return fmt.Errorf("replacement behavior not found: %s", replacement)
				}
			}

			// Get behavior name for display
			name := id
			if n, ok := node.Content["name"].(string); ok {
				name = n
			}

			// Update node to deprecated state
			now := time.Now()
			if node.Metadata == nil {
				node.Metadata = make(map[string]interface{})
			}
			node.Metadata["original_kind"] = node.Kind
			node.Metadata["deprecated_at"] = now.Format(time.RFC3339)
			node.Metadata["deprecated_by"] = os.Getenv("USER")
			node.Metadata["deprecation_reason"] = reason
			if replacement != "" {
				node.Metadata["replacement_id"] = replacement
			}
			node.Kind = "deprecated-behavior"

			if err := graphStore.UpdateNode(ctx, *node); err != nil {
				return fmt.Errorf("failed to update behavior: %w", err)
			}

			// Add deprecated-to edge if replacement specified
			if replacement != "" {
				edge := store.Edge{
					Source: id,
					Target: replacement,
					Kind:   "deprecated-to",
					Metadata: map[string]interface{}{
						"created_at": now.Format(time.RFC3339),
					},
				}
				if err := graphStore.AddEdge(ctx, edge); err != nil {
					return fmt.Errorf("failed to add deprecation edge: %w", err)
				}
			}

			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync changes: %w", err)
			}

			if jsonOut {
				result := map[string]interface{}{
					"status":     "deprecated",
					"id":         id,
					"name":       name,
					"reason":     reason,
					"restorable": true,
				}
				if replacement != "" {
					result["replacement"] = replacement
				}
				json.NewEncoder(os.Stdout).Encode(result)
			} else {
				fmt.Printf("Behavior '%s' has been deprecated.\n", name)
				fmt.Printf("Reason: %s\n", reason)
				if replacement != "" {
					fmt.Printf("Replacement: %s\n", replacement)
				}
				fmt.Println("Use 'floop restore' to undo this action.")
			}

			return nil
		},
	}

	cmd.Flags().String("reason", "", "Reason for deprecation (required)")
	cmd.Flags().String("replacement", "", "ID of behavior that replaces this one")

	return cmd
}

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <behavior-id>",
		Short: "Restore a deprecated or forgotten behavior",
		Long: `Restore a behavior that was previously deprecated or forgotten.

This undoes 'floop forget' or 'floop deprecate'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			id := args[0]

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, store.ScopeLocal)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Find the behavior by ID
			node, err := graphStore.GetNode(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to get behavior: %w", err)
			}
			if node == nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "behavior not found",
						"id":    id,
					})
					return nil
				}
				return fmt.Errorf("behavior not found: %s", id)
			}

			// Verify it's restorable (deprecated or forgotten)
			if node.Kind != "deprecated-behavior" && node.Kind != "forgotten-behavior" {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error":        "behavior is not deprecated or forgotten",
						"id":           id,
						"current_kind": node.Kind,
					})
					return nil
				}
				return fmt.Errorf("behavior is not deprecated or forgotten (current kind: %s)", node.Kind)
			}

			// Get behavior name for display
			name := id
			if n, ok := node.Content["name"].(string); ok {
				name = n
			}

			previousKind := node.Kind

			// Restore original kind
			originalKind := "behavior"
			if origKind, ok := node.Metadata["original_kind"].(string); ok {
				originalKind = origKind
			}
			node.Kind = originalKind

			// Record restoration
			now := time.Now()
			node.Metadata["restored_at"] = now.Format(time.RFC3339)
			node.Metadata["restored_by"] = os.Getenv("USER")

			// Clean up curation metadata
			delete(node.Metadata, "original_kind")
			delete(node.Metadata, "forgotten_at")
			delete(node.Metadata, "forgotten_by")
			delete(node.Metadata, "forget_reason")
			delete(node.Metadata, "deprecated_at")
			delete(node.Metadata, "deprecated_by")
			delete(node.Metadata, "deprecation_reason")
			delete(node.Metadata, "replacement_id")

			if err := graphStore.UpdateNode(ctx, *node); err != nil {
				return fmt.Errorf("failed to update behavior: %w", err)
			}

			// Remove deprecated-to edges if this was deprecated
			if previousKind == "deprecated-behavior" {
				edges, err := graphStore.GetEdges(ctx, id, store.DirectionOutbound, "deprecated-to")
				if err == nil {
					for _, e := range edges {
						graphStore.RemoveEdge(ctx, e.Source, e.Target, e.Kind)
					}
				}
			}

			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync changes: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":        "restored",
					"id":            id,
					"name":          name,
					"previous_kind": previousKind,
					"current_kind":  originalKind,
				})
			} else {
				fmt.Printf("Behavior '%s' has been restored.\n", name)
			}

			return nil
		},
	}

	return cmd
}

func newMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge <source-id> <target-id>",
		Short: "Merge two behaviors into one",
		Long: `Combine two similar behaviors into one.

The source behavior is marked as merged and linked to the target.
Use --into to specify which behavior survives (default: target).

This action cannot be undone with restore.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			force, _ := cmd.Flags().GetBool("force")
			into, _ := cmd.Flags().GetString("into")
			sourceID := args[0]
			targetID := args[1]

			// JSON mode implies force (no interactive prompts)
			if jsonOut {
				force = true
			}

			// Handle --into flag to swap source/target
			if into == sourceID {
				sourceID, targetID = targetID, sourceID
			} else if into != "" && into != targetID {
				return fmt.Errorf("--into must be one of the provided behavior IDs")
			}

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, store.ScopeLocal)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Load both behaviors
			sourceNode, err := graphStore.GetNode(ctx, sourceID)
			if err != nil {
				return fmt.Errorf("failed to get source behavior: %w", err)
			}
			if sourceNode == nil {
				return fmt.Errorf("source behavior not found: %s", sourceID)
			}

			targetNode, err := graphStore.GetNode(ctx, targetID)
			if err != nil {
				return fmt.Errorf("failed to get target behavior: %w", err)
			}
			if targetNode == nil {
				return fmt.Errorf("target behavior not found: %s", targetID)
			}

			// Verify both are active behaviors
			if sourceNode.Kind != "behavior" {
				return fmt.Errorf("source is not an active behavior (kind: %s)", sourceNode.Kind)
			}
			if targetNode.Kind != "behavior" {
				return fmt.Errorf("target is not an active behavior (kind: %s)", targetNode.Kind)
			}

			// Get names for display
			sourceName := sourceID
			if n, ok := sourceNode.Content["name"].(string); ok {
				sourceName = n
			}
			targetName := targetID
			if n, ok := targetNode.Content["name"].(string); ok {
				targetName = n
			}

			// Confirm unless --force
			if !force {
				fmt.Printf("Merge behaviors:\n")
				fmt.Printf("  Source (will be merged): %s\n", sourceName)
				fmt.Printf("  Target (will survive):   %s\n", targetName)
				fmt.Println("\nThis action cannot be undone.")
				fmt.Print("\nConfirm? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				response, _ := reader.ReadString('\n')
				response = strings.TrimSpace(strings.ToLower(response))
				if response != "y" && response != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			now := time.Now()

			// Merge when conditions (union)
			sourceWhen, _ := sourceNode.Content["when"].(map[string]interface{})
			targetWhen, _ := targetNode.Content["when"].(map[string]interface{})
			if targetWhen == nil {
				targetWhen = make(map[string]interface{})
			}
			for k, v := range sourceWhen {
				if _, exists := targetWhen[k]; !exists {
					targetWhen[k] = v
				}
			}
			targetNode.Content["when"] = targetWhen

			// Keep higher confidence
			sourceConf, _ := sourceNode.Metadata["confidence"].(float64)
			targetConf, _ := targetNode.Metadata["confidence"].(float64)
			if sourceConf > targetConf {
				targetNode.Metadata["confidence"] = sourceConf
			}

			// Keep higher priority
			sourcePrio, _ := sourceNode.Metadata["priority"].(int)
			targetPrio, _ := targetNode.Metadata["priority"].(int)
			if sourcePrio > targetPrio {
				targetNode.Metadata["priority"] = sourcePrio
			}

			// Track merge in target metadata
			mergedFrom, _ := targetNode.Metadata["merged_from"].([]interface{})
			mergedFrom = append(mergedFrom, sourceID)
			targetNode.Metadata["merged_from"] = mergedFrom
			targetNode.Metadata["last_merge_at"] = now.Format(time.RFC3339)

			// Update target
			if err := graphStore.UpdateNode(ctx, *targetNode); err != nil {
				return fmt.Errorf("failed to update target behavior: %w", err)
			}

			// Mark source as merged
			if sourceNode.Metadata == nil {
				sourceNode.Metadata = make(map[string]interface{})
			}
			sourceNode.Metadata["original_kind"] = sourceNode.Kind
			sourceNode.Metadata["merged_into"] = targetID
			sourceNode.Metadata["merged_at"] = now.Format(time.RFC3339)
			sourceNode.Metadata["merged_by"] = os.Getenv("USER")
			sourceNode.Kind = "merged-behavior"

			if err := graphStore.UpdateNode(ctx, *sourceNode); err != nil {
				return fmt.Errorf("failed to update source behavior: %w", err)
			}

			// Add merged-into edge
			edge := store.Edge{
				Source: sourceID,
				Target: targetID,
				Kind:   "merged-into",
				Metadata: map[string]interface{}{
					"merged_at": now.Format(time.RFC3339),
				},
			}
			if err := graphStore.AddEdge(ctx, edge); err != nil {
				return fmt.Errorf("failed to add merge edge: %w", err)
			}

			// Redirect edges that pointed to source to point to target
			inboundEdges, err := graphStore.GetEdges(ctx, sourceID, store.DirectionInbound, "")
			if err == nil {
				for _, e := range inboundEdges {
					if e.Kind != "merged-into" { // Don't redirect the edge we just added
						// Remove old edge
						graphStore.RemoveEdge(ctx, e.Source, e.Target, e.Kind)
						// Add redirected edge
						e.Target = targetID
						graphStore.AddEdge(ctx, e)
					}
				}
			}

			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync changes: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":       "merged",
					"source_id":    sourceID,
					"source_name":  sourceName,
					"target_id":    targetID,
					"target_name":  targetName,
					"surviving_id": targetID,
				})
			} else {
				fmt.Printf("Behaviors merged successfully.\n")
				fmt.Printf("  '%s' has been merged into '%s'\n", sourceName, targetName)
			}

			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Skip confirmation prompt")
	cmd.Flags().String("into", "", "ID of behavior that should survive (default: second argument)")

	return cmd
}

// ============================================================================
// Management Commands
// ============================================================================

func newDeduplicateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deduplicate",
		Short: "Find and merge duplicate behaviors",
		Long: `Find duplicate behaviors and optionally merge them.

This command analyzes all behaviors in the store, identifies duplicates based on
semantic similarity, and can automatically merge them.

Examples:
  floop deduplicate                  # Find duplicates in local store
  floop deduplicate --dry-run        # Show what would be merged
  floop deduplicate --threshold 0.8  # Use lower similarity threshold
  floop deduplicate --scope global   # Deduplicate global store only
  floop deduplicate --scope both     # Cross-store deduplication`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			threshold, _ := cmd.Flags().GetFloat64("threshold")
			scope, _ := cmd.Flags().GetString("scope")

			// Validate scope
			var storeScope store.StoreScope
			switch scope {
			case "local":
				storeScope = store.ScopeLocal
			case "global":
				storeScope = store.ScopeGlobal
			case "both":
				storeScope = store.ScopeBoth
			default:
				return fmt.Errorf("invalid scope: %s (must be local, global, or both)", scope)
			}

			// Check local initialization if needed
			if storeScope == store.ScopeLocal || storeScope == store.ScopeBoth {
				floopDir := filepath.Join(root, ".floop")
				if _, err := os.Stat(floopDir); os.IsNotExist(err) {
					return fmt.Errorf(".floop not initialized. Run 'floop init' first")
				}
			}

			// Check global initialization if needed
			if storeScope == store.ScopeGlobal || storeScope == store.ScopeBoth {
				globalPath, err := store.GlobalFloopPath()
				if err != nil {
					return fmt.Errorf("failed to get global path: %w", err)
				}
				if _, err := os.Stat(globalPath); os.IsNotExist(err) {
					return fmt.Errorf("global .floop not initialized. Run 'floop init --global' first")
				}
			}

			ctx := context.Background()

			// Configure deduplication
			dedupConfig := dedup.DeduplicatorConfig{
				SimilarityThreshold: threshold,
				AutoMerge:           !dryRun,
				UseLLM:              false,
				MaxBatchSize:        100,
			}

			// Handle cross-store deduplication
			if storeScope == store.ScopeBoth {
				return runCrossStoreDedup(ctx, root, dedupConfig, dryRun, jsonOut)
			}

			// Single store deduplication
			return runSingleStoreDedup(ctx, root, storeScope, dedupConfig, dryRun, jsonOut)
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show duplicates without merging")
	cmd.Flags().Float64("threshold", 0.9, "Similarity threshold for duplicate detection (0.0-1.0)")
	cmd.Flags().String("scope", "local", "Store scope: local, global, or both")

	return cmd
}

// runSingleStoreDedup runs deduplication on a single store.
func runSingleStoreDedup(ctx context.Context, root string, scope store.StoreScope, cfg dedup.DeduplicatorConfig, dryRun, jsonOut bool) error {
	// Open the appropriate store
	var graphStore store.GraphStore
	var err error

	switch scope {
	case store.ScopeLocal:
		graphStore, err = store.NewBeadsGraphStore(root)
	case store.ScopeGlobal:
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return fmt.Errorf("failed to get home directory: %w", homeErr)
		}
		graphStore, err = store.NewBeadsGraphStore(homeDir)
	}

	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer graphStore.Close()

	// Load all behaviors
	behaviors, err := loadBehaviorsFromStore(ctx, graphStore)
	if err != nil {
		return fmt.Errorf("failed to load behaviors: %w", err)
	}

	if len(behaviors) == 0 {
		if jsonOut {
			json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"status":           "no_behaviors",
				"total_behaviors":  0,
				"duplicates_found": 0,
			})
		} else {
			fmt.Println("No behaviors found to deduplicate.")
		}
		return nil
	}

	// Find duplicates using pairwise comparison
	type duplicatePair struct {
		BehaviorA  *models.Behavior
		BehaviorB  *models.Behavior
		Similarity float64
	}

	var duplicates []duplicatePair
	deduplicator := &crossStoreHelper{threshold: cfg.SimilarityThreshold}

	for i := 0; i < len(behaviors); i++ {
		for j := i + 1; j < len(behaviors); j++ {
			similarity := deduplicator.computeSimilarity(&behaviors[i], &behaviors[j])
			if similarity >= cfg.SimilarityThreshold {
				duplicates = append(duplicates, duplicatePair{
					BehaviorA:  &behaviors[i],
					BehaviorB:  &behaviors[j],
					Similarity: similarity,
				})
			}
		}
	}

	if len(duplicates) == 0 {
		if jsonOut {
			json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"status":           "no_duplicates",
				"total_behaviors":  len(behaviors),
				"duplicates_found": 0,
			})
		} else {
			fmt.Printf("Analyzed %d behaviors. No duplicates found.\n", len(behaviors))
		}
		return nil
	}

	if dryRun {
		if jsonOut {
			var pairs []map[string]interface{}
			for _, dup := range duplicates {
				pairs = append(pairs, map[string]interface{}{
					"behavior_a": dup.BehaviorA.ID,
					"name_a":     dup.BehaviorA.Name,
					"behavior_b": dup.BehaviorB.ID,
					"name_b":     dup.BehaviorB.Name,
					"similarity": dup.Similarity,
				})
			}
			json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"status":           "dry_run",
				"total_behaviors":  len(behaviors),
				"duplicates_found": len(duplicates),
				"duplicates":       pairs,
			})
		} else {
			fmt.Printf("Dry run: Found %d duplicate pairs among %d behaviors.\n\n", len(duplicates), len(behaviors))
			for i, dup := range duplicates {
				fmt.Printf("%d. Similarity: %.2f\n", i+1, dup.Similarity)
				fmt.Printf("   A: [%s] %s\n", dup.BehaviorA.ID, dup.BehaviorA.Name)
				fmt.Printf("   B: [%s] %s\n", dup.BehaviorB.ID, dup.BehaviorB.Name)
				fmt.Println()
			}
		}
		return nil
	}

	// Perform merges
	mergeCount := 0
	merged := make(map[string]bool) // Track already-merged behavior IDs

	for _, dup := range duplicates {
		// Skip if either behavior has already been merged
		if merged[dup.BehaviorA.ID] || merged[dup.BehaviorB.ID] {
			continue
		}

		// Use the existing merge command logic (behavior A survives, B is merged into A)
		merger := dedup.NewBehaviorMerger(dedup.MergerConfig{UseLLM: false})
		mergedBehavior, err := merger.Merge(ctx, []*models.Behavior{dup.BehaviorA, dup.BehaviorB})
		if err != nil {
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "Warning: failed to merge %s and %s: %v\n",
					dup.BehaviorA.ID, dup.BehaviorB.ID, err)
			}
			continue
		}

		// Mark B as merged
		merged[dup.BehaviorB.ID] = true
		mergeCount++

		if !jsonOut {
			fmt.Printf("Merged: %s <- %s (similarity: %.2f)\n",
				mergedBehavior.Name, dup.BehaviorB.Name, dup.Similarity)
		}
	}

	if err := graphStore.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync changes: %w", err)
	}

	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"status":           "completed",
			"total_behaviors":  len(behaviors),
			"duplicates_found": len(duplicates),
			"merges_performed": mergeCount,
		})
	} else {
		fmt.Printf("\nDeduplication complete: %d merges performed.\n", mergeCount)
	}

	return nil
}

// runCrossStoreDedup runs deduplication across local and global stores.
func runCrossStoreDedup(ctx context.Context, root string, cfg dedup.DeduplicatorConfig, dryRun, jsonOut bool) error {
	// Open local store
	localStore, err := store.NewBeadsGraphStore(root)
	if err != nil {
		return fmt.Errorf("failed to open local store: %w", err)
	}
	defer localStore.Close()

	// Open global store
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	globalStore, err := store.NewBeadsGraphStore(homeDir)
	if err != nil {
		return fmt.Errorf("failed to open global store: %w", err)
	}
	defer globalStore.Close()

	// Create merger
	merger := dedup.NewBehaviorMerger(dedup.MergerConfig{UseLLM: cfg.UseLLM})

	// Configure auto-merge based on dry-run
	crossCfg := cfg
	crossCfg.AutoMerge = !dryRun

	// Create cross-store deduplicator
	deduplicator := dedup.NewCrossStoreDeduplicatorWithConfig(localStore, globalStore, merger, crossCfg)

	// Run deduplication
	results, err := deduplicator.DeduplicateAcrossStores(ctx)
	if err != nil {
		return fmt.Errorf("cross-store deduplication failed: %w", err)
	}

	// Count results by action
	var skipped, merged, none int
	for _, r := range results {
		switch r.Action {
		case "skip":
			skipped++
		case "merge":
			merged++
		case "none":
			none++
		}
	}

	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"status":         "completed",
			"dry_run":        dryRun,
			"total_compared": len(results),
			"skipped":        skipped,
			"merged":         merged,
			"no_duplicate":   none,
			"results":        results,
		})
	} else {
		if dryRun {
			fmt.Printf("Dry run: Cross-store deduplication analysis\n\n")
		} else {
			fmt.Printf("Cross-store deduplication complete\n\n")
		}
		fmt.Printf("Total local behaviors compared: %d\n", len(results))
		fmt.Printf("  Skipped (same ID in global):  %d\n", skipped)
		fmt.Printf("  Semantic duplicates found:    %d\n", merged)
		fmt.Printf("  No duplicate found:           %d\n", none)

		// Show details of duplicates
		if merged > 0 {
			fmt.Println("\nDuplicate details:")
			for _, r := range results {
				if r.Action == "merge" {
					fmt.Printf("  - Local: %s (%.2f similar to global: %s)\n",
						r.LocalBehavior.Name, r.Similarity, r.GlobalMatch.Name)
				}
			}
		}
	}

	return nil
}

// loadBehaviorsFromStore loads all behaviors from a graph store.
func loadBehaviorsFromStore(ctx context.Context, graphStore store.GraphStore) ([]models.Behavior, error) {
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, err
	}

	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		b := learning.NodeToBehavior(node)
		behaviors = append(behaviors, b)
	}

	return behaviors, nil
}

// crossStoreHelper provides similarity computation for deduplication.
type crossStoreHelper struct {
	threshold float64
}

// computeSimilarity calculates similarity between two behaviors.
func (h *crossStoreHelper) computeSimilarity(a, b *models.Behavior) float64 {
	score := 0.0

	// Check 'when' overlap (40% weight)
	whenOverlap := h.computeWhenOverlap(a.When, b.When)
	score += whenOverlap * 0.4

	// Check content similarity using Jaccard word overlap (60% weight)
	contentSim := h.computeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
	score += contentSim * 0.6

	return score
}

// computeWhenOverlap calculates overlap between two when predicates.
func (h *crossStoreHelper) computeWhenOverlap(a, b map[string]interface{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	matches := 0
	total := len(a) + len(b)

	for key, valueA := range a {
		if valueB, exists := b[key]; exists {
			if fmt.Sprintf("%v", valueA) == fmt.Sprintf("%v", valueB) {
				matches += 2
			}
		}
	}

	if total == 0 {
		return 0.0
	}
	return float64(matches) / float64(total)
}

// computeContentSimilarity calculates Jaccard similarity between two strings.
func (h *crossStoreHelper) computeContentSimilarity(a, b string) float64 {
	wordsA := tokenizeContent(a)
	wordsB := tokenizeContent(b)

	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	setA := make(map[string]bool)
	for _, w := range wordsA {
		setA[strings.ToLower(w)] = true
	}

	setB := make(map[string]bool)
	for _, w := range wordsB {
		setB[strings.ToLower(w)] = true
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// tokenizeContent splits a string into word tokens.
func tokenizeContent(s string) []string {
	words := make([]string, 0)
	current := ""
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			current += string(r)
		} else if current != "" {
			words = append(words, current)
			current = ""
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage floop configuration",
		Long: `View and modify floop configuration settings.

Configuration is stored in ~/.floop/config.yaml.

Examples:
  floop config list                            # Show all settings
  floop config get llm.provider                # Get a specific setting
  floop config set llm.provider anthropic      # Set a setting
  floop config set llm.api_key $ANTHROPIC_API_KEY`,
	}

	cmd.AddCommand(
		newConfigListCmd(),
		newConfigGetCmd(),
		newConfigSetCmd(),
	)

	return cmd
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configuration settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(cfg)
			} else {
				fmt.Println("Configuration (~/.floop/config.yaml):")
				fmt.Println()
				fmt.Println("LLM Settings:")
				fmt.Printf("  llm.provider:          %s\n", valueOrDefault(cfg.LLM.Provider, "(not set)"))
				fmt.Printf("  llm.enabled:           %v\n", cfg.LLM.Enabled)
				if cfg.LLM.APIKey != "" {
					fmt.Printf("  llm.api_key:           %s\n", maskAPIKey(cfg.LLM.APIKey))
				} else {
					fmt.Printf("  llm.api_key:           (not set)\n")
				}
				fmt.Printf("  llm.comparison_model:  %s\n", valueOrDefault(cfg.LLM.ComparisonModel, "(default)"))
				fmt.Printf("  llm.merge_model:       %s\n", valueOrDefault(cfg.LLM.MergeModel, "(default)"))
				fmt.Printf("  llm.timeout:           %v\n", cfg.LLM.Timeout)
				fmt.Printf("  llm.fallback_to_rules: %v\n", cfg.LLM.FallbackToRules)
				fmt.Println()
				fmt.Println("Deduplication Settings:")
				fmt.Printf("  deduplication.auto_merge:            %v\n", cfg.Deduplication.AutoMerge)
				fmt.Printf("  deduplication.similarity_threshold:  %.2f\n", cfg.Deduplication.SimilarityThreshold)
			}

			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			key := args[0]

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			value, found := getConfigValue(cfg, key)
			if !found {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "key not found",
						"key":   key,
					})
				} else {
					fmt.Printf("Unknown configuration key: %s\n", key)
				}
				return nil
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"key":   key,
					"value": value,
				})
			} else {
				fmt.Printf("%s = %v\n", key, value)
			}

			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			key := args[0]
			value := args[1]

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if err := setConfigValue(cfg, key, value); err != nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": err.Error(),
						"key":   key,
					})
				} else {
					fmt.Printf("Error: %v\n", err)
				}
				return nil
			}

			// Save the config
			if err := saveConfig(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status": "updated",
					"key":    key,
					"value":  value,
				})
			} else {
				fmt.Printf("Set %s = %s\n", key, value)
			}

			return nil
		},
	}
}

// getConfigValue retrieves a configuration value by dot-notation key.
func getConfigValue(cfg *config.FloopConfig, key string) (interface{}, bool) {
	switch key {
	case "llm.provider":
		return cfg.LLM.Provider, true
	case "llm.api_key":
		return cfg.LLM.APIKey, true
	case "llm.comparison_model":
		return cfg.LLM.ComparisonModel, true
	case "llm.merge_model":
		return cfg.LLM.MergeModel, true
	case "llm.timeout":
		return cfg.LLM.Timeout.String(), true
	case "llm.enabled":
		return cfg.LLM.Enabled, true
	case "llm.fallback_to_rules":
		return cfg.LLM.FallbackToRules, true
	case "deduplication.auto_merge":
		return cfg.Deduplication.AutoMerge, true
	case "deduplication.similarity_threshold":
		return cfg.Deduplication.SimilarityThreshold, true
	default:
		return nil, false
	}
}

// setConfigValue sets a configuration value by dot-notation key.
func setConfigValue(cfg *config.FloopConfig, key, value string) error {
	switch key {
	case "llm.provider":
		validProviders := map[string]bool{"": true, "anthropic": true, "openai": true, "subagent": true}
		if !validProviders[value] {
			return fmt.Errorf("invalid provider: %s (valid: anthropic, openai, subagent, or empty)", value)
		}
		cfg.LLM.Provider = value
	case "llm.api_key":
		cfg.LLM.APIKey = value
	case "llm.comparison_model":
		cfg.LLM.ComparisonModel = value
	case "llm.merge_model":
		cfg.LLM.MergeModel = value
	case "llm.timeout":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration: %s", value)
		}
		cfg.LLM.Timeout = d
	case "llm.enabled":
		cfg.LLM.Enabled = value == "true" || value == "1"
	case "llm.fallback_to_rules":
		cfg.LLM.FallbackToRules = value == "true" || value == "1"
	case "deduplication.auto_merge":
		cfg.Deduplication.AutoMerge = value == "true" || value == "1"
	case "deduplication.similarity_threshold":
		var f float64
		if _, err := fmt.Sscanf(value, "%f", &f); err != nil {
			return fmt.Errorf("invalid threshold: %s (must be a number between 0 and 1)", value)
		}
		if f < 0 || f > 1 {
			return fmt.Errorf("threshold must be between 0 and 1, got %f", f)
		}
		cfg.Deduplication.SimilarityThreshold = f
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}
	return nil
}

// saveConfig writes the configuration to ~/.floop/config.yaml.
func saveConfig(cfg *config.FloopConfig) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	floopDir := filepath.Join(homeDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		return fmt.Errorf("failed to create .floop directory: %w", err)
	}

	configPath := filepath.Join(floopDir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// valueOrDefault returns the value if non-empty, otherwise the default.
func valueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// maskAPIKey masks an API key for display (shows first and last 4 chars).
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
