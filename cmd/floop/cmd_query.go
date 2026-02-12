package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/feedback-loop/internal/activation"
	"github.com/nvandessel/feedback-loop/internal/assembly"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/summarization"
	"github.com/nvandessel/feedback-loop/internal/tiering"
	"github.com/spf13/cobra"
)

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
					"scope":       "local",
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
agent system prompts. Use --token-budget to limit output size with intelligent tiering.

Examples:
  floop prompt --file main.go
  floop prompt --file main.go --format xml --token-budget 500
  floop prompt --file main.go --tiered --token-budget 2000
  floop prompt --file main.go --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			file, _ := cmd.Flags().GetString("file")
			task, _ := cmd.Flags().GetString("task")
			env, _ := cmd.Flags().GetString("env")
			format, _ := cmd.Flags().GetString("format")
			maxTokens, _ := cmd.Flags().GetInt("max-tokens")
			tokenBudget, _ := cmd.Flags().GetInt("token-budget")
			tiered, _ := cmd.Flags().GetBool("tiered")
			expanded, _ := cmd.Flags().GetBool("expanded")
			jsonOut, _ := cmd.Flags().GetBool("json")

			// Support both --max-tokens and --token-budget for backwards compatibility
			if tokenBudget > 0 {
				maxTokens = tokenBudget
			}

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

			// Set output format
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

			// Use tiered injection if requested
			if tiered && maxTokens > 0 {
				// Create tiered injection plan
				scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
				summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())
				assigner := tiering.NewTierAssigner(tiering.DefaultTierAssignerConfig(), scorer, summarizer)

				plan := assigner.AssignTiers(resolved.Active, &ctx, maxTokens)
				tieredCompiled := compiler.CompileTiered(plan)

				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"context":              ctx,
						"prompt":               tieredCompiled.Text,
						"format":               tieredCompiled.Format,
						"total_tokens":         tieredCompiled.TotalTokens,
						"token_budget":         maxTokens,
						"full_behaviors":       tieredCompiled.IncludedBehaviors,
						"summarized_behaviors": tieredCompiled.SummarizedBehaviors,
						"omitted_behaviors":    tieredCompiled.OmittedBehaviors,
						"sections":             tieredCompiled.Sections,
						"tiered":               true,
					})
				} else {
					if plan.IncludedCount() == 0 {
						fmt.Println("No active behaviors for this context.")
						return nil
					}

					fmt.Println(tieredCompiled.Text)

					fmt.Fprintln(os.Stderr)
					fmt.Fprintf(os.Stderr, "---\n")
					fmt.Fprintf(os.Stderr, "Behaviors: %d full, %d summarized, %d omitted\n",
						len(plan.FullBehaviors), len(plan.SummarizedBehaviors), len(plan.OmittedBehaviors))
					fmt.Fprintf(os.Stderr, "Tokens: ~%d / %d budget\n", plan.TotalTokens, maxTokens)
				}
			} else {
				// Use standard optimization
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

				compiled := compiler.Compile(activeBehaviors)

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
						"tiered":             false,
					})
				} else {
					if len(activeBehaviors) == 0 {
						fmt.Println("No active behaviors for this context.")
						return nil
					}

					fmt.Println(compiled.Text)

					fmt.Fprintln(os.Stderr)
					fmt.Fprintf(os.Stderr, "---\n")
					fmt.Fprintf(os.Stderr, "Behaviors: %d included", len(compiled.IncludedBehaviors))
					if len(compiled.ExcludedBehaviors) > 0 {
						fmt.Fprintf(os.Stderr, ", %d excluded (token limit)", len(compiled.ExcludedBehaviors))
					}
					fmt.Fprintln(os.Stderr)
					fmt.Fprintf(os.Stderr, "Tokens: ~%d\n", compiled.TotalTokens)
				}
			}

			return nil
		},
	}

	cmd.Flags().String("file", "", "Current file path")
	cmd.Flags().String("task", "", "Current task type")
	cmd.Flags().String("env", "", "Environment (dev, staging, prod)")
	cmd.Flags().String("format", "markdown", "Output format (markdown, xml, plain)")
	cmd.Flags().Int("max-tokens", 0, "Maximum tokens (0 = unlimited, deprecated: use --token-budget)")
	cmd.Flags().Int("token-budget", 0, "Token budget for behavior injection (enables intelligent tiering)")
	cmd.Flags().Bool("tiered", false, "Use tiered injection (full/summary/omit) instead of simple truncation")
	cmd.Flags().Bool("expanded", false, "Use expanded content when available")

	return cmd
}
