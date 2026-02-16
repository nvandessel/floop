package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/sanitize"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

func newDetectCorrectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detect-correction",
		Short: "Detect and capture corrections from user text",
		Long: `Analyze user text to detect corrections and automatically capture them.

This command is used by hooks to automatically detect when a user is correcting
the agent and capture the correction as a learned behavior.

It uses the MightBeCorrection() heuristic for fast pattern matching, then
falls back to LLM extraction if running in a CLI session.

Examples:
  floop detect-correction --prompt "No, don't use print, use logging instead"
  echo '{"prompt":"Actually, prefer pathlib over os.path"}' | floop detect-correction`,
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt, _ := cmd.Flags().GetString("prompt")
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			// If no prompt flag, try reading from stdin (for hook usage)
			if prompt == "" {
				var input struct {
					Prompt string `json:"prompt"`
				}
				if err := json.NewDecoder(os.Stdin).Decode(&input); err == nil {
					prompt = input.Prompt
				}
			}

			if prompt == "" {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"detected": false,
						"reason":   "no prompt provided",
						"captured": false,
					})
				}
				return nil
			}

			// Fast pattern check first
			capture := learning.NewCorrectionCapture()
			if !capture.MightBeCorrection(prompt) {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"detected": false,
						"reason":   "no correction patterns found",
						"captured": false,
					})
				}
				return nil
			}

			// Try LLM extraction if available
			var wrong, right string
			var confidence float64
			var extracted bool

			client := llm.DetectAndCreate()
			if client != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				result, err := client.ExtractCorrection(ctx, prompt)
				if err == nil && result.IsCorrection && result.Wrong != "" && result.Right != "" {
					wrong = result.Wrong
					right = result.Right
					confidence = result.Confidence
					extracted = true
				}
			}

			if !extracted {
				// Fallback: pattern detected but couldn't extract details
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"detected": true,
						"reason":   "correction pattern found but could not extract details",
						"captured": false,
						"hint":     "Use floop_learn manually to capture this correction",
					})
				}
				return nil
			}

			// Sanitize extracted values to prevent stored prompt injection
			wrong = sanitize.SanitizeBehaviorContent(wrong)
			right = sanitize.SanitizeBehaviorContent(right)

			// Dry run - just report what would be captured
			if dryRun {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"detected":   true,
						"wrong":      wrong,
						"right":      right,
						"confidence": confidence,
						"captured":   false,
						"dry_run":    true,
					})
				} else {
					fmt.Println("Correction detected (dry run):")
					fmt.Printf("  Wrong: %s\n", wrong)
					fmt.Printf("  Right: %s\n", right)
					fmt.Printf("  Confidence: %.2f\n", confidence)
				}
				return nil
			}

			// Skip low confidence corrections
			if confidence < 0.6 {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"detected":   true,
						"wrong":      wrong,
						"right":      right,
						"confidence": confidence,
						"captured":   false,
						"reason":     "confidence too low (< 0.6)",
					})
				}
				return nil
			}

			// Ensure .floop exists
			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"detected": true,
						"wrong":    wrong,
						"right":    right,
						"captured": false,
						"error":    ".floop not initialized",
					})
				}
				return nil
			}

			// Capture the correction
			now := time.Now()
			ctxSnapshot := models.ContextSnapshot{
				Timestamp: now,
			}

			correction := models.Correction{
				ID:              fmt.Sprintf("c-%d", now.UnixNano()),
				Timestamp:       now,
				Context:         ctxSnapshot,
				AgentAction:     wrong,
				CorrectedAction: right,
				Processed:       false,
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"detected": true,
						"wrong":    wrong,
						"right":    right,
						"captured": false,
						"error":    err.Error(),
					})
				}
				return nil
			}
			defer graphStore.Close()

			// Process through learning loop
			loop := learning.NewLearningLoop(graphStore, nil)
			ctx := context.Background()

			result, err := loop.ProcessCorrection(ctx, correction)
			if err != nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"detected": true,
						"wrong":    wrong,
						"right":    right,
						"captured": false,
						"error":    err.Error(),
					})
				}
				return nil
			}

			// Mark correction as processed
			correction.Processed = true
			processedAt := time.Now()
			correction.ProcessedAt = &processedAt

			// Append to corrections log
			correctionsPath := filepath.Join(floopDir, "corrections.jsonl")
			f, err := os.OpenFile(correctionsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			if err == nil {
				json.NewEncoder(f).Encode(correction)
				f.Close()
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"detected":    true,
					"wrong":       wrong,
					"right":       right,
					"confidence":  confidence,
					"captured":    true,
					"behavior_id": result.CandidateBehavior.ID,
				})
			} else {
				fmt.Printf("Correction captured: %s\n", result.CandidateBehavior.ID)
			}

			return nil
		},
	}

	cmd.Flags().String("prompt", "", "User prompt text to analyze")
	cmd.Flags().Bool("dry-run", false, "Detect only, don't capture")

	return cmd
}
