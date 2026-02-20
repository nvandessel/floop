package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/similarity"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

// minSharedTagsForEdge is the minimum number of shared tags between two
// behaviors to create a similar-to edge, regardless of overall similarity
// score. Tag co-occurrence is a strong signal for conceptual relatedness —
// if two behaviors both have "git" and "worktree" tags, spreading activation
// needs that edge to associate related concepts.
const minSharedTagsForEdge = 2

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
			var allResults []deriveStoreResult

			if storeScope == constants.ScopeLocal || storeScope == constants.ScopeBoth {
				floopDir := filepath.Join(root, ".floop")
				if _, err := os.Stat(floopDir); os.IsNotExist(err) {
					return fmt.Errorf(".floop not initialized. Run 'floop init' first")
				}
				graphStore, err := store.NewFileGraphStore(root)
				if err != nil {
					return fmt.Errorf("failed to open local store: %w", err)
				}
				defer graphStore.Close()
				result, err := deriveEdgesForStore(ctx, graphStore, "local", dryRun, clear)
				if err != nil {
					return fmt.Errorf("local store: %w", err)
				}
				allResults = append(allResults, result)
			}

			if storeScope == constants.ScopeGlobal || storeScope == constants.ScopeBoth {
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
				graphStore, err := store.NewFileGraphStore(homeDir)
				if err != nil {
					return fmt.Errorf("failed to open global store: %w", err)
				}
				defer graphStore.Close()
				result, err := deriveEdgesForStore(ctx, graphStore, "global", dryRun, clear)
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

// deriveStoreResult holds the output for one store's edge derivation.
type deriveStoreResult struct {
	Scope           string           `json:"scope"`
	Behaviors       int              `json:"behaviors"`
	ExistingEdges   int              `json:"existing_edges"`
	ClearedEdges    int              `json:"cleared_edges"`
	ProposedEdges   []proposedEdge   `json:"proposed_edges"`
	CreatedEdges    int              `json:"created_edges"`
	SkippedExisting int              `json:"skipped_existing"`
	Histogram       [10]int          `json:"score_histogram"`
	Connectivity    connectivityInfo `json:"connectivity"`
}

// proposedEdge represents a single proposed edge.
type proposedEdge struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Kind   string  `json:"kind"`
	Weight float64 `json:"weight"`
	Score  float64 `json:"score"`
}

// connectivityInfo describes graph connectivity after edge derivation.
type connectivityInfo struct {
	TotalNodes int `json:"total_nodes"`
	Islands    int `json:"islands"`
	Connected  int `json:"connected"`
}

// deriveEdgesForStore runs the edge derivation algorithm on a single store.
func deriveEdgesForStore(ctx context.Context, graphStore store.GraphStore, scope string, dryRun, clear bool) (deriveStoreResult, error) {
	result := deriveStoreResult{Scope: scope}

	// Load all non-forgotten behaviors
	behaviors, err := loadBehaviorsFromStore(ctx, graphStore)
	if err != nil {
		return result, fmt.Errorf("failed to load behaviors: %w", err)
	}
	result.Behaviors = len(behaviors)

	if len(behaviors) == 0 {
		return result, nil
	}

	// Clear existing derived edges if requested
	if clear && !dryRun {
		result.ClearedEdges = clearDerivedEdges(ctx, graphStore, behaviors)
	}

	// Build existing edge set for dedup
	existingEdges := make(map[string]bool)
	for _, b := range behaviors {
		edges, err := graphStore.GetEdges(ctx, b.ID, store.DirectionOutbound, "")
		if err != nil {
			continue
		}
		for _, e := range edges {
			key := e.Source + ":" + e.Target + ":" + e.Kind
			existingEdges[key] = true
		}
		result.ExistingEdges += len(edges)
	}

	// All-pairs comparison
	now := time.Now()
	for i := 0; i < len(behaviors); i++ {
		for j := i + 1; j < len(behaviors); j++ {
			a := &behaviors[i]
			b := &behaviors[j]

			// Compute similarity (no LLM)
			score := computeBehaviorSimilarity(a, b, nil, false, nil)

			// Record in histogram (10 buckets: [0.0,0.1), [0.1,0.2), ..., [0.9,1.0])
			bucket := int(score * 10)
			if bucket >= 10 {
				bucket = 9
			}
			result.Histogram[bucket]++

			// Check for overrides edges (specificity)
			if similarity.IsMoreSpecific(a.When, b.When) {
				pe := proposedEdge{Source: a.ID, Target: b.ID, Kind: "overrides", Weight: 1.0, Score: score}
				key := a.ID + ":" + b.ID + ":overrides"
				if existingEdges[key] {
					result.SkippedExisting++
				} else {
					result.ProposedEdges = append(result.ProposedEdges, pe)
					existingEdges[key] = true
				}
			}
			if similarity.IsMoreSpecific(b.When, a.When) {
				pe := proposedEdge{Source: b.ID, Target: a.ID, Kind: "overrides", Weight: 1.0, Score: score}
				key := b.ID + ":" + a.ID + ":overrides"
				if existingEdges[key] {
					result.SkippedExisting++
				} else {
					result.ProposedEdges = append(result.ProposedEdges, pe)
					existingEdges[key] = true
				}
			}

			// Check for similar-to edges:
			// 1. Score-based: similarity in [0.5, 0.9)
			// 2. Tag-based: behaviors sharing >= 2 tags are conceptually related
			//    and need edges for spreading activation (git → branch, worktree, etc.)
			similarToKey := a.ID + ":" + b.ID + ":similar-to"
			shouldConnect := (score >= constants.SimilarToThreshold && score < constants.SimilarToUpperBound) ||
				similarity.CountSharedTags(a.Content.Tags, b.Content.Tags) >= minSharedTagsForEdge
			if shouldConnect {
				pe := proposedEdge{Source: a.ID, Target: b.ID, Kind: "similar-to", Weight: 0.8, Score: score}
				if existingEdges[similarToKey] {
					result.SkippedExisting++
				} else {
					result.ProposedEdges = append(result.ProposedEdges, pe)
					existingEdges[similarToKey] = true
				}
			}
		}
	}

	// Create proposed edges (unless dry-run)
	if !dryRun && len(result.ProposedEdges) > 0 {
		for _, pe := range result.ProposedEdges {
			edge := store.Edge{
				Source:    pe.Source,
				Target:    pe.Target,
				Kind:      pe.Kind,
				Weight:    pe.Weight,
				CreatedAt: now,
			}
			if err := graphStore.AddEdge(ctx, edge); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to add edge %s -> %s: %v\n", pe.Source, pe.Target, err)
				continue
			}
			result.CreatedEdges++
		}

		if err := graphStore.Sync(ctx); err != nil {
			return result, fmt.Errorf("failed to sync store: %w", err)
		}

		// Refresh PageRank
		if _, err := ranking.ComputePageRank(ctx, graphStore, ranking.DefaultPageRankConfig()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to refresh PageRank: %v\n", err)
		}
	}

	// Compute connectivity
	result.Connectivity = computeConnectivity(ctx, graphStore, behaviors)

	return result, nil
}

// clearDerivedEdges removes all similar-to and overrides outbound edges for behaviors.
// Returns the number of edges removed. Logs warnings on individual failures but
// continues clearing remaining edges.
func clearDerivedEdges(ctx context.Context, graphStore store.GraphStore, behaviors []models.Behavior) int {
	cleared := 0
	for _, b := range behaviors {
		for _, kind := range []string{"similar-to", "overrides"} {
			edges, err := graphStore.GetEdges(ctx, b.ID, store.DirectionOutbound, kind)
			if err != nil {
				continue
			}
			for _, e := range edges {
				if err := graphStore.RemoveEdge(ctx, e.Source, e.Target, e.Kind); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to remove edge %s -> %s (%s): %v\n", e.Source, e.Target, e.Kind, err)
					continue
				}
				cleared++
			}
		}
	}
	return cleared
}

// computeConnectivity counts how many behaviors have edges vs. are isolated islands.
func computeConnectivity(ctx context.Context, graphStore store.GraphStore, behaviors []models.Behavior) connectivityInfo {
	info := connectivityInfo{TotalNodes: len(behaviors)}

	for _, b := range behaviors {
		hasEdge := false
		// Check outbound edges
		outEdges, err := graphStore.GetEdges(ctx, b.ID, store.DirectionOutbound, "")
		if err == nil && len(outEdges) > 0 {
			hasEdge = true
		}
		// Check inbound edges
		if !hasEdge {
			inEdges, err := graphStore.GetEdges(ctx, b.ID, store.DirectionInbound, "")
			if err == nil && len(inEdges) > 0 {
				hasEdge = true
			}
		}
		if hasEdge {
			info.Connected++
		} else {
			info.Islands++
		}
	}

	return info
}

func printDeriveResult(r deriveStoreResult, dryRun bool) {
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
