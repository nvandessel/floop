package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

// validEdgeKinds defines the allowed edge kinds for floop connect.
var validEdgeKinds = map[string]bool{
	"requires":     true,
	"overrides":    true,
	"conflicts":    true,
	"similar-to":   true,
	"learned-from": true,
}

func newConnectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect <source> <target> <kind>",
		Short: "Create an edge between two behaviors",
		Long: `Create a semantic edge between two behaviors in the graph.

This allows manual wiring of the behavior graph for spreading activation.

Edge kinds:
  requires      - Source depends on target
  overrides     - Source replaces target in matching context
  conflicts     - Source and target cannot both be active
  similar-to    - Behaviors are related/similar
  learned-from  - Source was derived from target

Examples:
  floop connect behavior-abc behavior-xyz similar-to
  floop connect behavior-abc behavior-xyz requires --weight 0.9
  floop connect behavior-abc behavior-xyz similar-to --bidirectional`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			target := args[1]
			kind := args[2]

			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			weight, _ := cmd.Flags().GetFloat64("weight")
			bidirectional, _ := cmd.Flags().GetBool("bidirectional")

			// Validate kind
			if !validEdgeKinds[kind] {
				return fmt.Errorf("invalid edge kind: %s (must be one of: requires, overrides, conflicts, similar-to, learned-from)", kind)
			}

			// Validate weight
			if weight <= 0 || weight > 1.0 {
				return fmt.Errorf("weight must be in (0.0, 1.0], got %f", weight)
			}

			// No self-edges
			if source == target {
				return fmt.Errorf("self-edges are not allowed: source and target are both %s", source)
			}

			// Check local initialization
			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			// Validate source exists
			sourceNode, err := graphStore.GetNode(ctx, source)
			if err != nil {
				return fmt.Errorf("failed to check source node: %w", err)
			}
			if sourceNode == nil {
				return fmt.Errorf("source node not found: %s", source)
			}

			// Validate target exists
			targetNode, err := graphStore.GetNode(ctx, target)
			if err != nil {
				return fmt.Errorf("failed to check target node: %w", err)
			}
			if targetNode == nil {
				return fmt.Errorf("target node not found: %s", target)
			}

			// Check for duplicate edge
			existing, err := graphStore.GetEdges(ctx, source, store.DirectionOutbound, kind)
			if err != nil {
				return fmt.Errorf("failed to check existing edges: %w", err)
			}
			for _, e := range existing {
				if e.Target == target {
					if !jsonOut {
						fmt.Fprintf(os.Stderr, "warning: edge %s -[%s]-> %s already exists (weight: %.2f)\n", source, kind, target, e.Weight)
					}
				}
			}

			// Create edge
			now := time.Now()
			edge := store.Edge{
				Source:    source,
				Target:    target,
				Kind:      kind,
				Weight:    weight,
				CreatedAt: now,
			}

			if err := graphStore.AddEdge(ctx, edge); err != nil {
				return fmt.Errorf("failed to add edge: %w", err)
			}

			// Create reverse edge if bidirectional
			if bidirectional {
				reverseEdge := store.Edge{
					Source:    target,
					Target:    source,
					Kind:      kind,
					Weight:    weight,
					CreatedAt: now,
				}
				if err := graphStore.AddEdge(ctx, reverseEdge); err != nil {
					return fmt.Errorf("failed to add reverse edge: %w", err)
				}
			}

			// Sync
			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync store: %w", err)
			}

			// Refresh PageRank
			if _, err := ranking.ComputePageRank(ctx, graphStore, ranking.DefaultPageRankConfig()); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to refresh PageRank: %v\n", err)
			}

			// Output
			result := map[string]interface{}{
				"source":        source,
				"target":        target,
				"kind":          kind,
				"weight":        weight,
				"bidirectional": bidirectional,
				"message":       fmt.Sprintf("Edge created: %s -[%s (%.2f)]-> %s", source, kind, weight, target),
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(result)
			}

			fmt.Printf("✓ Edge created: %s -[%s (%.2f)]-> %s\n", source, kind, weight, target)
			if bidirectional {
				fmt.Printf("✓ Reverse edge: %s -[%s (%.2f)]-> %s\n", target, kind, weight, source)
			}
			return nil
		},
	}

	cmd.Flags().Float64("weight", 0.8, "Edge weight (0.0-1.0)")
	cmd.Flags().Bool("bidirectional", false, "Create edges in both directions")

	return cmd
}
