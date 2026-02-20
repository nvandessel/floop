package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
			serve, _ := cmd.Flags().GetBool("serve")

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

				if serve {
					if err := runGraphServer(cmd, ctx, gs, enrichment, noOpen); err != nil {
						return err
					}
				} else {
					if err := writeStaticHTML(cmd, ctx, gs, enrichment, output, noOpen); err != nil {
						return err
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
	cmd.Flags().Bool("serve", false, "Start a local server with electric mode (spreading activation visualization)")

	return cmd
}

// writeStaticHTML renders the graph to a self-contained HTML file.
func writeStaticHTML(cmd *cobra.Command, ctx context.Context, gs store.GraphStore, enrichment *visualization.EnrichmentData, output string, noOpen bool) error {
	htmlBytes, err := visualization.RenderHTML(ctx, gs, enrichment)
	if err != nil {
		return fmt.Errorf("render HTML: %w", err)
	}

	outPath := output
	if outPath == "" {
		outPath = filepath.Join(os.TempDir(), "floop-graph.html")
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
	return nil
}

// runGraphServer starts a local HTTP server with electric mode and blocks until Ctrl-C.
func runGraphServer(cmd *cobra.Command, ctx context.Context, gs store.GraphStore, enrichment *visualization.EnrichmentData, noOpen bool) error {
	srv := visualization.NewServer(gs, enrichment)

	srvCtx, srvCancel := context.WithCancel(ctx)
	defer srvCancel()

	// Handle SIGINT/SIGTERM for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		select {
		case <-sigCh:
			srvCancel()
		case <-srvCtx.Done():
		}
	}()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(srvCtx) }()

	// Wait for server to start
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if srv.Addr() != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	addr := srv.Addr()
	if addr == "" {
		return fmt.Errorf("server failed to start")
	}

	url := "http://" + addr
	fmt.Fprintf(cmd.OutOrStdout(), "Graph server running at %s\n", url)
	fmt.Fprintf(cmd.OutOrStdout(), "Press Ctrl-C to stop.\n")

	if !noOpen {
		if err := visualization.OpenBrowser(url); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Could not open browser: %v\nOpen %s manually.\n", err, url)
		}
	}

	// Block until server exits
	if err := <-errCh; err != nil {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// openStoreForGraph opens a multi-store for graph visualization.
func openStoreForGraph(projectRoot string) (store.GraphStore, error) {
	gs, err := store.NewMultiGraphStore(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("open multi-store: %w", err)
	}
	return gs, nil
}
