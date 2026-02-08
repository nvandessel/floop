package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/visualization"
	"github.com/spf13/cobra"
)

func newGraphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Visualize the behavior graph",
		Long:  `Output the behavior graph in DOT (Graphviz) or JSON format.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			format, _ := cmd.Flags().GetString("format")

			gs, err := openStoreForGraph(root)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer gs.Close()

			ctx := context.Background()

			switch visualization.Format(format) {
			case visualization.FormatDOT:
				dot, err := visualization.RenderDOT(ctx, gs)
				if err != nil {
					return fmt.Errorf("render DOT: %w", err)
				}
				fmt.Fprint(cmd.OutOrStdout(), dot)

			case visualization.FormatJSON:
				result, err := visualization.RenderJSON(ctx, gs)
				if err != nil {
					return fmt.Errorf("render JSON: %w", err)
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(result); err != nil {
					return fmt.Errorf("encode JSON: %w", err)
				}

			default:
				return fmt.Errorf("unsupported format %q (use 'dot' or 'json')", format)
			}

			return nil
		},
	}

	cmd.Flags().String("format", "dot", "Output format: dot or json")

	return cmd
}

// openStoreForGraph opens a multi-store for graph visualization.
func openStoreForGraph(projectRoot string) (store.GraphStore, error) {
	gs, err := store.NewMultiGraphStore(projectRoot, constants.ScopeLocal)
	if err != nil {
		return nil, fmt.Errorf("open multi-store: %w", err)
	}
	return gs, nil
}
