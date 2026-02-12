package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/visualization"
	"github.com/spf13/cobra"
)

func newGraphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Visualize the behavior graph",
		Long:  `Output the behavior graph in DOT (Graphviz), JSON, or interactive HTML format.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			format, _ := cmd.Flags().GetString("format")
			output, _ := cmd.Flags().GetString("output")
			noOpen, _ := cmd.Flags().GetBool("no-open")

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

			case visualization.FormatHTML:
				// Compute PageRank for node sizing
				pageRank, err := ranking.ComputePageRank(ctx, gs, ranking.DefaultPageRankConfig())
				if err != nil {
					return fmt.Errorf("compute PageRank: %w", err)
				}

				enrichment := &visualization.EnrichmentData{
					PageRank: pageRank,
				}

				htmlBytes, err := visualization.RenderHTML(ctx, gs, enrichment)
				if err != nil {
					return fmt.Errorf("render HTML: %w", err)
				}

				// Determine output path
				outPath := output
				if outPath == "" {
					tmpDir := os.TempDir()
					outPath = filepath.Join(tmpDir, "floop-graph.html")
				}

				if err := os.WriteFile(outPath, htmlBytes, 0644); err != nil {
					return fmt.Errorf("write HTML file: %w", err)
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Graph written to %s\n", outPath)

				if !noOpen {
					if err := visualization.OpenBrowser(outPath); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Could not open browser: %v\nOpen %s manually.\n", err, outPath)
					}
				}

			default:
				return fmt.Errorf("unsupported format %q (use 'dot', 'json', or 'html')", format)
			}

			return nil
		},
	}

	cmd.Flags().String("format", "dot", "Output format: dot, json, or html")
	cmd.Flags().StringP("output", "o", "", "Output file path (html format only)")
	cmd.Flags().Bool("no-open", false, "Don't open browser after generating HTML")

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
