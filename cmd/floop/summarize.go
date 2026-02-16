package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/summarization"
	"github.com/spf13/cobra"
)

func newSummarizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "summarize [behavior-id]",
		Short: "Generate or regenerate summaries for behaviors",
		Long: `Generate compressed summaries for behaviors to optimize token usage.

Summaries are used in tiered injection when the full behavior content
would exceed the token budget. Each summary is ~60 characters.

Examples:
  floop summarize abc123      # Generate summary for specific behavior
  floop summarize --all       # Generate summaries for all behaviors
  floop summarize --missing   # Only generate for behaviors without summaries`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			allBehaviors, _ := cmd.Flags().GetBool("all")
			missingOnly, _ := cmd.Flags().GetBool("missing")

			// Validate flags
			if len(args) == 0 && !allBehaviors && !missingOnly {
				return fmt.Errorf("specify a behavior ID or use --all/--missing")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Create summarizer
			summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())

			// Results tracking
			var results []summaryResult

			if len(args) > 0 {
				// Process single behavior
				behaviorID := args[0]
				res, err := summarizeBehavior(ctx, graphStore, summarizer, behaviorID)
				if err != nil {
					return err
				}
				results = append(results, res)
			} else {
				// Process all or missing
				nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
				if err != nil {
					return fmt.Errorf("failed to query behaviors: %w", err)
				}

				for _, node := range nodes {
					behavior := learning.NodeToBehavior(node)

					// Skip if missingOnly and summary exists
					if missingOnly && behavior.Content.Summary != "" {
						continue
					}

					res, err := summarizeBehavior(ctx, graphStore, summarizer, behavior.ID)
					if err != nil {
						res = summaryResult{
							BehaviorID: behavior.ID,
							Name:       behavior.Name,
							Error:      err.Error(),
						}
					}
					results = append(results, res)
				}
			}

			// Sync changes
			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync store: %w", err)
			}

			// Output results
			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"results": results,
					"count":   len(results),
					"updated": countUpdated(results),
				})
			} else {
				if len(results) == 0 {
					fmt.Println("No behaviors to summarize.")
					return nil
				}

				fmt.Printf("Summarized %d behavior(s):\n\n", len(results))
				for _, r := range results {
					shortID := r.BehaviorID
					if len(shortID) > 8 {
						shortID = shortID[:8]
					}
					if r.Error != "" {
						fmt.Printf("  %s: ERROR - %s\n", shortID, r.Error)
					} else if r.Updated {
						fmt.Printf("  %s: %s\n", shortID, r.Summary)
					} else {
						fmt.Printf("  %s: (unchanged) %s\n", shortID, r.Summary)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().Bool("all", false, "Generate summaries for all behaviors")
	cmd.Flags().Bool("missing", false, "Only generate for behaviors without summaries")

	return cmd
}

// summaryResult holds the result of summarizing a behavior
type summaryResult struct {
	BehaviorID string `json:"behavior_id"`
	Name       string `json:"name"`
	Summary    string `json:"summary"`
	Updated    bool   `json:"updated"`
	Error      string `json:"error,omitempty"`
}

// summarizeBehavior generates a summary for a single behavior
func summarizeBehavior(ctx context.Context, graphStore store.GraphStore, summarizer summarization.Summarizer, behaviorID string) (summaryResult, error) {
	// Query for the behavior
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{
		"kind": "behavior",
		"id":   behaviorID,
	})
	if err != nil {
		return summaryResult{BehaviorID: behaviorID, Error: err.Error()}, err
	}
	if len(nodes) == 0 {
		return summaryResult{BehaviorID: behaviorID, Error: "not found"}, fmt.Errorf("behavior not found: %s", behaviorID)
	}

	behavior := learning.NodeToBehavior(nodes[0])
	oldSummary := behavior.Content.Summary

	// Generate summary
	summary, err := summarizer.Summarize(&behavior)
	if err != nil {
		return summaryResult{BehaviorID: behaviorID, Name: behavior.Name, Error: err.Error()}, err
	}

	// Check if changed
	updated := summary != oldSummary
	if updated {
		// Update the behavior node
		node := nodes[0]
		if content, ok := node.Content["content"].(map[string]interface{}); ok {
			content["summary"] = summary
		} else {
			node.Content["content"] = map[string]interface{}{
				"canonical": behavior.Content.Canonical,
				"summary":   summary,
			}
		}

		if err := graphStore.UpdateNode(ctx, node); err != nil {
			return summaryResult{BehaviorID: behaviorID, Name: behavior.Name, Error: err.Error()}, err
		}
	}

	return summaryResult{
		BehaviorID: behaviorID,
		Name:       behavior.Name,
		Summary:    summary,
		Updated:    updated,
	}, nil
}

func countUpdated(results []summaryResult) int {
	count := 0
	for _, r := range results {
		if r.Updated {
			count++
		}
	}
	return count
}
