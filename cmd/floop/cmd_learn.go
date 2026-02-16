package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/dedup"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/sanitize"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

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
			// Validate required parameters
			if wrong == "" {
				return fmt.Errorf("--wrong is required and cannot be empty")
			}
			if right == "" {
				return fmt.Errorf("--right is required and cannot be empty")
			}

			// Sanitize inputs to prevent stored prompt injection
			wrong = sanitize.SanitizeBehaviorContent(wrong)
			right = sanitize.SanitizeBehaviorContent(right)
			if task != "" {
				task = sanitize.SanitizeBehaviorContent(task)
			}
			if file != "" {
				file = sanitize.SanitizeFilePath(file)
			}

			// Validate that inputs are not empty after sanitization
			if wrong == "" {
				return fmt.Errorf("--wrong is empty after sanitization: input contained only unsafe content")
			}
			if right == "" {
				return fmt.Errorf("--right is empty after sanitization: input contained only unsafe content")
			}

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

			// Use persistent graph store with MultiGraphStore
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			// Process through learning loop with auto-merge support
			autoMerge, _ := cmd.Flags().GetBool("auto-merge")
			var loopConfig *learning.LearningLoopConfig
			if autoMerge {
				merger := dedup.NewBehaviorMerger(dedup.MergerConfig{})
				dedupConfig := dedup.DeduplicatorConfig{
					SimilarityThreshold: 0.9,
					AutoMerge:           true,
				}
				loopConfig = &learning.LearningLoopConfig{
					AutoAcceptThreshold: 0.8,
					AutoMerge:           true,
					AutoMergeThreshold:  0.9,
					Deduplicator:        dedup.NewStoreDeduplicator(graphStore, merger, dedupConfig),
				}
			}

			// Apply --scope override if explicitly set
			if cmd.Flags().Changed("scope") {
				scopeVal, _ := cmd.Flags().GetString("scope")
				s := constants.Scope(scopeVal)
				if s != constants.ScopeLocal && s != constants.ScopeGlobal {
					return fmt.Errorf("--scope must be 'local' or 'global'")
				}
				if loopConfig == nil {
					loopConfig = &learning.LearningLoopConfig{}
				}
				loopConfig.ScopeOverride = &s
			}

			loop := learning.NewLearningLoop(graphStore, loopConfig)
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
			f, err := os.OpenFile(correctionsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
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
	cmd.Flags().String("scope", "", "Override auto-classification: local (project) or global (user)")
	cmd.Flags().Bool("auto-merge", true, "Automatically merge similar behaviors (matches MCP behavior)")
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

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			// Process through learning loop with auto-merge support
			autoMerge, _ := cmd.Flags().GetBool("auto-merge")
			var loopConfig *learning.LearningLoopConfig
			if autoMerge {
				merger := dedup.NewBehaviorMerger(dedup.MergerConfig{})
				dedupConfig := dedup.DeduplicatorConfig{
					SimilarityThreshold: 0.9,
					AutoMerge:           true,
				}
				loopConfig = &learning.LearningLoopConfig{
					AutoAcceptThreshold: 0.8,
					AutoMerge:           true,
					AutoMergeThreshold:  0.9,
					Deduplicator:        dedup.NewStoreDeduplicator(graphStore, merger, dedupConfig),
				}
			}

			// Apply --scope override if explicitly set
			if cmd.Flags().Changed("scope") {
				scopeVal, _ := cmd.Flags().GetString("scope")
				s := constants.Scope(scopeVal)
				if s != constants.ScopeLocal && s != constants.ScopeGlobal {
					return fmt.Errorf("--scope must be 'local' or 'global'")
				}
				if loopConfig == nil {
					loopConfig = &learning.LearningLoopConfig{}
				}
				loopConfig.ScopeOverride = &s
			}

			loop := learning.NewLearningLoop(graphStore, loopConfig)
			ctx := context.Background()

			var processed []models.Correction
			var results []map[string]interface{}

			for i := range corrections {
				c := &corrections[i]
				if c.Processed {
					continue
				}

				// Sanitize correction fields before reprocessing
				c.AgentAction = sanitize.SanitizeBehaviorContent(c.AgentAction)
				c.CorrectedAction = sanitize.SanitizeBehaviorContent(c.CorrectedAction)
				if c.Context.FilePath != "" {
					c.Context.FilePath = sanitize.SanitizeFilePath(c.Context.FilePath)
				}
				if c.Context.Task != "" {
					c.Context.Task = sanitize.SanitizeBehaviorContent(c.Context.Task)
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
	cmd.Flags().String("scope", "", "Override auto-classification: local or global")
	cmd.Flags().Bool("auto-merge", true, "Automatically merge similar behaviors (matches MCP behavior)")

	return cmd
}
