package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvandessel/floop/internal/activation"
	"github.com/nvandessel/floop/internal/constants"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List behaviors or corrections",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			showCorrections, _ := cmd.Flags().GetBool("corrections")
			globalFlag, _ := cmd.Flags().GetBool("global")
			localFlag, _ := cmd.Flags().GetBool("local")
			allFlag, _ := cmd.Flags().GetBool("all")
			tagFilter, _ := cmd.Flags().GetString("tag")

			// Validate flag combinations
			if globalFlag && localFlag {
				return fmt.Errorf("cannot specify both --global and --local")
			}
			if globalFlag && allFlag {
				return fmt.Errorf("cannot specify both --global and --all")
			}
			if localFlag && allFlag {
				return fmt.Errorf("cannot specify both --local and --all")
			}

			// Determine scope
			scope := constants.ScopeBoth
			if globalFlag {
				scope = constants.ScopeGlobal
			} else if localFlag {
				scope = constants.ScopeLocal
			}
			if allFlag {
				fmt.Fprintln(cmd.ErrOrStderr(), "Warning: --all is deprecated; 'both' is now the default scope")
			}

			// Check initialization — for ScopeBoth, degrade gracefully if one store is missing
			hasLocal := true
			hasGlobal := true

			if scope == constants.ScopeLocal || scope == constants.ScopeBoth {
				floopDir := filepath.Join(root, ".floop")
				if _, err := os.Stat(floopDir); os.IsNotExist(err) {
					hasLocal = false
					if scope == constants.ScopeLocal {
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
			}

			if scope == constants.ScopeGlobal || scope == constants.ScopeBoth {
				globalPath, err := store.GlobalFloopPath()
				if err != nil {
					hasGlobal = false
					if scope == constants.ScopeGlobal {
						return fmt.Errorf("failed to get global path: %w", err)
					}
				} else if _, err := os.Stat(globalPath); os.IsNotExist(err) {
					hasGlobal = false
					if scope == constants.ScopeGlobal {
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

			// For ScopeBoth, degrade to whichever store is available
			if scope == constants.ScopeBoth {
				if !hasLocal && !hasGlobal {
					if jsonOut {
						json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]interface{}{
							"error": "no .floop stores initialized",
						})
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), "No .floop stores initialized. Run 'floop init' first.")
					}
					return nil
				}
				if !hasLocal {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning: local .floop not initialized, showing global behaviors only")
					scope = constants.ScopeGlobal
				} else if !hasGlobal {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning: global .floop not initialized, showing local behaviors only")
					scope = constants.ScopeLocal
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

			// Filter by tag if specified
			if tagFilter != "" {
				var filtered []models.Behavior
				for _, b := range behaviors {
					for _, t := range b.Content.Tags {
						if t == tagFilter {
							filtered = append(filtered, b)
							break
						}
					}
				}
				behaviors = filtered
			}

			if jsonOut {
				result := map[string]interface{}{
					"behaviors": behaviors,
					"count":     len(behaviors),
					"scope":     string(scope),
				}
				json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				// Show scope in header
				scopeStr := string(scope)
				if scope == constants.ScopeBoth {
					scopeStr = "all (local + global)"
				}

				if len(behaviors) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "No behaviors learned yet (%s scope).\n", scopeStr)
					fmt.Fprintln(cmd.OutOrStdout(), "\nUse 'floop learn --right \"Y\"' to capture corrections.")
					return nil
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Learned behaviors - %s (%d):\n\n", scopeStr, len(behaviors))

				for i, b := range behaviors {
					fmt.Fprintf(cmd.OutOrStdout(), "%d. [%s] %s\n", i+1, b.Kind, b.Name)
					fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", b.Content.Canonical)
					if len(b.Content.Tags) > 0 {
						fmt.Fprintf(cmd.OutOrStdout(), "   Tags: %v\n", b.Content.Tags)
					}
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
	cmd.Flags().Bool("local", false, "Show behaviors from local project store only")
	cmd.Flags().Bool("all", false, "Show behaviors from both local and global stores (now the default; this flag is deprecated)")
	cmd.Flags().String("tag", "", "Filter behaviors by tag (exact match)")

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
			fmt.Printf("%d. [%s]\n", i+1, c.Timestamp.Format("2006-01-02T15:04:05Z07:00"))
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

// splitLines splits a string into lines without using strings.Split for efficiency.
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

// loadBehaviorsWithScope loads behaviors from the specified scope (local, global, or both).
func loadBehaviorsWithScope(projectRoot string, scope constants.Scope) ([]models.Behavior, error) {
	ctx := context.Background()
	var graphStore store.GraphStore
	var err error

	switch scope {
	case constants.ScopeLocal:
		// Load from local store only
		graphStore, err = store.NewSQLiteGraphStore(projectRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to open local store: %w", err)
		}

	case constants.ScopeGlobal:
		// Load from global store only
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		graphStore, err = store.NewSQLiteGraphStore(homeDir)
		if err != nil {
			return nil, fmt.Errorf("failed to open global store: %w", err)
		}

	case constants.ScopeBoth:
		// Load from both stores using MultiGraphStore
		graphStore, err = store.NewMultiGraphStore(projectRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to open multi-store: %w", err)
		}

	default:
		return nil, fmt.Errorf("invalid scope: %s", scope)
	}

	defer graphStore.Close()

	// Query all behavior nodes
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": string(store.NodeKindBehavior)})
	if err != nil {
		return nil, fmt.Errorf("failed to query behaviors: %w", err)
	}

	// Convert nodes to behaviors
	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		b := models.NodeToBehavior(node)
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
			behaviors, err := loadBehaviorsWithScope(root, constants.ScopeBoth)
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
