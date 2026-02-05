package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show behavior usage statistics",
		Long: `Display usage statistics for learned behaviors.

Shows activation counts, follow rates, and ranking scores to help
understand which behaviors are most valuable and which may need review.

Examples:
  floop stats              # Show all stats
  floop stats --top 10     # Show top 10 by usage
  floop stats --sort score # Sort by ranking score`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			topN, _ := cmd.Flags().GetInt("top")
			sortBy, _ := cmd.Flags().GetString("sort")
			scope, _ := cmd.Flags().GetString("scope")

			// Parse scope
			storeScope := constants.Scope(scope)
			if !storeScope.Valid() {
				storeScope = constants.ScopeLocal
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, storeScope)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Query all behaviors
			nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
			if err != nil {
				return fmt.Errorf("failed to query behaviors: %w", err)
			}

			if len(nodes) == 0 {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"behaviors": []interface{}{},
						"summary":   map[string]int{},
					})
				} else {
					fmt.Println("No behaviors found.")
				}
				return nil
			}

			// Convert to behaviors and calculate stats
			type BehaviorStats struct {
				ID              string  `json:"id"`
				Name            string  `json:"name"`
				Kind            string  `json:"kind"`
				Confidence      float64 `json:"confidence"`
				Priority        int     `json:"priority"`
				TimesActivated  int     `json:"times_activated"`
				TimesFollowed   int     `json:"times_followed"`
				TimesConfirmed  int     `json:"times_confirmed"`
				TimesOverridden int     `json:"times_overridden"`
				FollowRate      float64 `json:"follow_rate"`
				HasSummary      bool    `json:"has_summary"`
			}

			stats := make([]BehaviorStats, 0, len(nodes))
			var totalActivations, totalFollowed, totalConfirmed, totalOverridden int
			kindCounts := make(map[string]int)

			for _, node := range nodes {
				behavior := learning.NodeToBehavior(node)

				followRate := 0.0
				if behavior.Stats.TimesActivated > 0 {
					positiveSignals := behavior.Stats.TimesFollowed + behavior.Stats.TimesConfirmed
					followRate = float64(positiveSignals) / float64(behavior.Stats.TimesActivated)
				}

				stats = append(stats, BehaviorStats{
					ID:              behavior.ID,
					Name:            behavior.Name,
					Kind:            string(behavior.Kind),
					Confidence:      behavior.Confidence,
					Priority:        behavior.Priority,
					TimesActivated:  behavior.Stats.TimesActivated,
					TimesFollowed:   behavior.Stats.TimesFollowed,
					TimesConfirmed:  behavior.Stats.TimesConfirmed,
					TimesOverridden: behavior.Stats.TimesOverridden,
					FollowRate:      followRate,
					HasSummary:      behavior.Content.Summary != "",
				})

				totalActivations += behavior.Stats.TimesActivated
				totalFollowed += behavior.Stats.TimesFollowed
				totalConfirmed += behavior.Stats.TimesConfirmed
				totalOverridden += behavior.Stats.TimesOverridden
				kindCounts[string(behavior.Kind)]++
			}

			// Sort by specified field
			switch sortBy {
			case "activations", "activated":
				sort.Slice(stats, func(i, j int) bool {
					return stats[i].TimesActivated > stats[j].TimesActivated
				})
			case "followed":
				sort.Slice(stats, func(i, j int) bool {
					return stats[i].TimesFollowed > stats[j].TimesFollowed
				})
			case "rate", "follow_rate":
				sort.Slice(stats, func(i, j int) bool {
					return stats[i].FollowRate > stats[j].FollowRate
				})
			case "confidence":
				sort.Slice(stats, func(i, j int) bool {
					return stats[i].Confidence > stats[j].Confidence
				})
			case "priority":
				sort.Slice(stats, func(i, j int) bool {
					return stats[i].Priority > stats[j].Priority
				})
			default: // score (combined)
				sort.Slice(stats, func(i, j int) bool {
					scoreI := stats[i].Confidence * stats[i].FollowRate * float64(stats[i].Priority+1)
					scoreJ := stats[j].Confidence * stats[j].FollowRate * float64(stats[j].Priority+1)
					return scoreI > scoreJ
				})
			}

			// Apply top N limit
			if topN > 0 && topN < len(stats) {
				stats = stats[:topN]
			}

			// Build summary
			summary := map[string]interface{}{
				"total_behaviors":   len(nodes),
				"total_activations": totalActivations,
				"total_followed":    totalFollowed,
				"total_confirmed":   totalConfirmed,
				"total_overridden":  totalOverridden,
				"by_kind":           kindCounts,
			}

			// Count summaries
			withSummary := 0
			for _, s := range stats {
				if s.HasSummary {
					withSummary++
				}
			}
			summary["with_summary"] = withSummary

			// Output
			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"behaviors": stats,
					"summary":   summary,
				})
			} else {
				fmt.Printf("Behavior Statistics\n")
				fmt.Printf("===================\n\n")

				fmt.Printf("Summary:\n")
				fmt.Printf("  Total behaviors:   %d\n", len(nodes))
				fmt.Printf("  With summaries:    %d\n", withSummary)
				fmt.Printf("  Total activations: %d\n", totalActivations)
				fmt.Printf("  Total followed:    %d\n", totalFollowed)
				fmt.Printf("  Total confirmed:   %d\n", totalConfirmed)
				fmt.Printf("  Total overridden:  %d\n", totalOverridden)
				fmt.Printf("\n")

				fmt.Printf("By kind:\n")
				for kind, count := range kindCounts {
					fmt.Printf("  %s: %d\n", kind, count)
				}
				fmt.Printf("\n")

				if len(stats) > 0 {
					fmt.Printf("Behaviors (sorted by %s):\n\n", sortBy)
					fmt.Printf("%-8s %-30s %-12s %6s %6s %6s %8s\n",
						"ID", "Name", "Kind", "Act", "Fol", "Conf", "Rate")
					fmt.Println(repeatChar('-', 86))

					for _, s := range stats {
						shortID := s.ID
						if len(shortID) > 8 {
							shortID = shortID[:8]
						}
						name := s.Name
						if len(name) > 30 {
							name = name[:27] + "..."
						}
						kind := s.Kind
						if len(kind) > 12 {
							kind = kind[:12]
						}

						fmt.Printf("%-8s %-30s %-12s %6d %6d %6d %7.0f%%\n",
							shortID, name, kind,
							s.TimesActivated, s.TimesFollowed, s.TimesConfirmed,
							s.FollowRate*100)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().Int("top", 0, "Show only top N behaviors")
	cmd.Flags().String("sort", "score", "Sort by: score, activations, followed, rate, confidence, priority")
	cmd.Flags().String("scope", "local", "Scope: local, global, or both")

	return cmd
}

func repeatChar(c rune, n int) string {
	result := make([]rune, n)
	for i := range result {
		result[i] = c
	}
	return string(result)
}
