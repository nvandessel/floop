package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvandessel/floop/internal/constants"
	"github.com/nvandessel/floop/internal/edges"
	"github.com/nvandessel/floop/internal/store"
	"github.com/spf13/cobra"
)

func newDeriveEdgesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "derive-edges",
		Short: "Derive similar-to and overrides edges from behavior similarity",
		Long: `Analyze all behaviors pairwise and create edges based on similarity scores.

For each pair of behaviors:
  - If similarity is in [0.5, 0.9): create a similar-to edge (weight 0.8)
  - If one behavior's when-conditions are a strict superset: create an overrides edge (weight 1.0)

Existing edges are preserved unless --clear is used.

Examples:
  floop derive-edges                        # Derive edges for both stores
  floop derive-edges --dry-run              # Preview without creating edges
  floop derive-edges --scope global         # Only process global store
  floop derive-edges --clear                # Remove existing derived edges first`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			clear, _ := cmd.Flags().GetBool("clear")
			scope, _ := cmd.Flags().GetString("scope")

			storeScope := constants.Scope(scope)
			if !storeScope.Valid() {
				return fmt.Errorf("invalid scope: %s (must be local, global, or both)", scope)
			}

			ctx := context.Background()
			var allResults []edges.DeriveResult

			// Check initialization — for ScopeBoth, degrade gracefully if one store is missing
			hasLocal := true
			hasGlobal := true

			if storeScope == constants.ScopeLocal || storeScope == constants.ScopeBoth {
				floopDir := filepath.Join(root, ".floop")
				if _, err := os.Stat(floopDir); os.IsNotExist(err) {
					hasLocal = false
					if storeScope == constants.ScopeLocal {
						return fmt.Errorf(".floop not initialized. Run 'floop init' first")
					}
				}
			}

			if storeScope == constants.ScopeGlobal || storeScope == constants.ScopeBoth {
				globalPath, err := store.GlobalFloopPath()
				if err != nil {
					return fmt.Errorf("failed to get global path: %w", err)
				}
				if _, err := os.Stat(globalPath); os.IsNotExist(err) {
					hasGlobal = false
					if storeScope == constants.ScopeGlobal {
						return fmt.Errorf("global .floop not initialized. Run 'floop init --global' first")
					}
				}
			}

			if storeScope == constants.ScopeBoth {
				if !hasLocal && !hasGlobal {
					return fmt.Errorf("no .floop stores initialized. Run 'floop init' first")
				}
				if !hasLocal {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning: local .floop not initialized, deriving edges from global store only")
					storeScope = constants.ScopeGlobal
				} else if !hasGlobal {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning: global .floop not initialized, deriving edges from local store only")
					storeScope = constants.ScopeLocal
				}
			}

			if hasLocal && (storeScope == constants.ScopeLocal || storeScope == constants.ScopeBoth) {
				graphStore, err := store.NewSQLiteGraphStore(root)
				if err != nil {
					return fmt.Errorf("failed to open local store: %w", err)
				}
				defer graphStore.Close()
				result, err := edges.DeriveEdgesForStore(ctx, graphStore, "local", dryRun, clear)
				if err != nil {
					return fmt.Errorf("local store: %w", err)
				}
				allResults = append(allResults, result)
			}

			if hasGlobal && (storeScope == constants.ScopeGlobal || storeScope == constants.ScopeBoth) {
				globalPath, err := store.GlobalFloopPath()
				if err != nil {
					return fmt.Errorf("failed to get global path: %w", err)
				}
				if _, err := os.Stat(globalPath); os.IsNotExist(err) {
					return fmt.Errorf("global .floop not initialized. Run 'floop init --global' first")
				}
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to get home directory: %w", err)
				}
				graphStore, err := store.NewSQLiteGraphStore(homeDir)
				if err != nil {
					return fmt.Errorf("failed to open global store: %w", err)
				}
				defer graphStore.Close()
				result, err := edges.DeriveEdgesForStore(ctx, graphStore, "global", dryRun, clear)
				if err != nil {
					return fmt.Errorf("global store: %w", err)
				}
				allResults = append(allResults, result)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"dry_run": dryRun,
					"clear":   clear,
					"stores":  allResults,
				})
			}

			for _, r := range allResults {
				printDeriveResult(r, dryRun)
			}
			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show proposed edges without creating them")
	cmd.Flags().Bool("clear", false, "Remove existing similar-to and overrides edges before deriving")
	cmd.Flags().String("scope", "both", "Store scope: local, global, or both")

	return cmd
}

func printDeriveResult(r edges.DeriveResult, dryRun bool) {
	if dryRun {
		fmt.Printf("\n=== %s store (dry run) ===\n", r.Scope)
	} else {
		fmt.Printf("\n=== %s store ===\n", r.Scope)
	}
	fmt.Printf("Behaviors: %d\n", r.Behaviors)

	if r.ClearedEdges > 0 {
		fmt.Printf("Cleared edges: %d\n", r.ClearedEdges)
	}

	// Score histogram
	fmt.Println("\nScore distribution:")
	bucketLabels := []string{
		"[0.0-0.1)", "[0.1-0.2)", "[0.2-0.3)", "[0.3-0.4)", "[0.4-0.5)",
		"[0.5-0.6)", "[0.6-0.7)", "[0.7-0.8)", "[0.8-0.9)", "[0.9-1.0]",
	}
	for i, count := range r.Histogram {
		if count > 0 {
			bar := ""
			for range count {
				if len(bar) < 50 {
					bar += "#"
				}
			}
			fmt.Printf("  %s %s (%d)\n", bucketLabels[i], bar, count)
		}
	}

	// Edge proposals
	fmt.Printf("\nProposed edges: %d\n", len(r.ProposedEdges))
	fmt.Printf("Skipped (already exist): %d\n", r.SkippedExisting)

	if !dryRun {
		fmt.Printf("Created edges: %d\n", r.CreatedEdges)
	}

	// Connectivity
	fmt.Printf("\nConnectivity:\n")
	fmt.Printf("  Total nodes: %d\n", r.Connectivity.TotalNodes)
	fmt.Printf("  Connected: %d\n", r.Connectivity.Connected)
	fmt.Printf("  Islands (0 edges): %d\n", r.Connectivity.Islands)
}
