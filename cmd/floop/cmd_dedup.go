package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/feedback-loop/internal/config"
	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/dedup"
	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/similarity"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

func newDeduplicateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deduplicate",
		Short: "Find and merge duplicate behaviors",
		Long: `Find duplicate behaviors and optionally merge them.

This command analyzes all behaviors in the store, identifies duplicates based on
semantic similarity, and can automatically merge them.

Examples:
  floop deduplicate                  # Find duplicates in local store
  floop deduplicate --dry-run        # Show what would be merged
  floop deduplicate --threshold 0.8  # Use lower similarity threshold
  floop deduplicate --scope global   # Deduplicate global store only
  floop deduplicate --scope both     # Cross-store deduplication`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			threshold, _ := cmd.Flags().GetFloat64("threshold")
			scope, _ := cmd.Flags().GetString("scope")

			// Validate scope
			storeScope := constants.Scope(scope)
			if !storeScope.Valid() {
				return fmt.Errorf("invalid scope: %s (must be local, global, or both)", scope)
			}

			// Check local initialization if needed
			if storeScope == store.ScopeLocal || storeScope == store.ScopeBoth {
				floopDir := filepath.Join(root, ".floop")
				if _, err := os.Stat(floopDir); os.IsNotExist(err) {
					return fmt.Errorf(".floop not initialized. Run 'floop init' first")
				}
			}

			// Check global initialization if needed
			if storeScope == store.ScopeGlobal || storeScope == store.ScopeBoth {
				globalPath, err := store.GlobalFloopPath()
				if err != nil {
					return fmt.Errorf("failed to get global path: %w", err)
				}
				if _, err := os.Stat(globalPath); os.IsNotExist(err) {
					return fmt.Errorf("global .floop not initialized. Run 'floop init --global' first")
				}
			}

			ctx := context.Background()

			// Load config to check for LLM settings
			floopCfg, _ := config.Load()
			useLLM := floopCfg != nil && floopCfg.LLM.Enabled && floopCfg.LLM.Provider != ""

			// Configure deduplication
			dedupConfig := dedup.DeduplicatorConfig{
				SimilarityThreshold: threshold,
				AutoMerge:           !dryRun,
				UseLLM:              useLLM,
				MaxBatchSize:        100,
			}

			// Handle cross-store deduplication
			if storeScope == store.ScopeBoth {
				return runCrossStoreDedup(ctx, root, dedupConfig, dryRun, jsonOut)
			}

			// Single store deduplication
			return runSingleStoreDedup(ctx, root, storeScope, dedupConfig, dryRun, jsonOut)
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show duplicates without merging")
	cmd.Flags().Float64("threshold", 0.9, "Similarity threshold for duplicate detection (0.0-1.0)")
	cmd.Flags().String("scope", "local", "Store scope: local, global, or both")

	return cmd
}

// runSingleStoreDedup runs deduplication on a single store.
func runSingleStoreDedup(ctx context.Context, root string, scope store.StoreScope, cfg dedup.DeduplicatorConfig, dryRun, jsonOut bool) error {
	// Open the appropriate store
	var graphStore store.GraphStore
	var err error

	switch scope {
	case store.ScopeLocal:
		graphStore, err = store.NewFileGraphStore(root)
	case store.ScopeGlobal:
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return fmt.Errorf("failed to get home directory: %w", homeErr)
		}
		graphStore, err = store.NewFileGraphStore(homeDir)
	}

	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer graphStore.Close()

	// Load all behaviors
	behaviors, err := loadBehaviorsFromStore(ctx, graphStore)
	if err != nil {
		return fmt.Errorf("failed to load behaviors: %w", err)
	}

	if len(behaviors) == 0 {
		if jsonOut {
			json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"status":           "no_behaviors",
				"total_behaviors":  0,
				"duplicates_found": 0,
			})
		} else {
			fmt.Println("No behaviors found to deduplicate.")
		}
		return nil
	}

	// Find duplicates using pairwise comparison
	type duplicatePair struct {
		BehaviorA  *models.Behavior
		BehaviorB  *models.Behavior
		Similarity float64
	}

	// Create LLM client for similarity comparison
	floopCfg, _ := config.Load()
	llmClient := createLLMClient(floopCfg)
	useLLM := cfg.UseLLM && llmClient != nil

	var duplicates []duplicatePair

	for i := 0; i < len(behaviors); i++ {
		for j := i + 1; j < len(behaviors); j++ {
			sim := computeBehaviorSimilarity(&behaviors[i], &behaviors[j], llmClient, useLLM)
			if sim >= cfg.SimilarityThreshold {
				duplicates = append(duplicates, duplicatePair{
					BehaviorA:  &behaviors[i],
					BehaviorB:  &behaviors[j],
					Similarity: sim,
				})
			}
		}
	}

	if len(duplicates) == 0 {
		if jsonOut {
			json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"status":           "no_duplicates",
				"total_behaviors":  len(behaviors),
				"duplicates_found": 0,
			})
		} else {
			fmt.Printf("Analyzed %d behaviors. No duplicates found.\n", len(behaviors))
		}
		return nil
	}

	if dryRun {
		if jsonOut {
			var pairs []map[string]interface{}
			for _, dup := range duplicates {
				pairs = append(pairs, map[string]interface{}{
					"behavior_a": dup.BehaviorA.ID,
					"name_a":     dup.BehaviorA.Name,
					"behavior_b": dup.BehaviorB.ID,
					"name_b":     dup.BehaviorB.Name,
					"similarity": dup.Similarity,
				})
			}
			json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"status":           "dry_run",
				"total_behaviors":  len(behaviors),
				"duplicates_found": len(duplicates),
				"duplicates":       pairs,
			})
		} else {
			fmt.Printf("Dry run: Found %d duplicate pairs among %d behaviors.\n\n", len(duplicates), len(behaviors))
			for i, dup := range duplicates {
				fmt.Printf("%d. Similarity: %.2f\n", i+1, dup.Similarity)
				fmt.Printf("   A: [%s] %s\n", dup.BehaviorA.ID, dup.BehaviorA.Name)
				fmt.Printf("   B: [%s] %s\n", dup.BehaviorB.ID, dup.BehaviorB.Name)
				fmt.Println()
			}
		}
		return nil
	}

	// Perform merges
	mergeCount := 0
	merged := make(map[string]bool) // Track already-merged behavior IDs

	for _, dup := range duplicates {
		// Skip if either behavior has already been merged
		if merged[dup.BehaviorA.ID] || merged[dup.BehaviorB.ID] {
			continue
		}

		// Use the existing merge command logic (behavior A survives, B is merged into A)
		// Create LLM client if configured
		floopCfg, _ := config.Load()
		llmClient := createLLMClient(floopCfg)
		merger := dedup.NewBehaviorMerger(dedup.MergerConfig{
			UseLLM:    llmClient != nil,
			LLMClient: llmClient,
		})
		mergedBehavior, err := merger.Merge(ctx, []*models.Behavior{dup.BehaviorA, dup.BehaviorB})
		if err != nil {
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "Warning: failed to merge %s and %s: %v\n",
					dup.BehaviorA.ID, dup.BehaviorB.ID, err)
			}
			continue
		}

		// Update behavior A with merged content
		mergedNode := models.BehaviorToNode(mergedBehavior)
		mergedNode.ID = dup.BehaviorA.ID // Keep original ID
		if err := graphStore.UpdateNode(ctx, mergedNode); err != nil {
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "Warning: failed to save merged behavior: %v\n", err)
			}
			continue
		}

		// Delete behavior B
		if err := graphStore.DeleteNode(ctx, dup.BehaviorB.ID); err != nil {
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "Warning: failed to delete merged behavior %s: %v\n",
					dup.BehaviorB.ID, err)
			}
			// Continue anyway - merge was successful even if cleanup failed
		}

		// Mark B as merged
		merged[dup.BehaviorB.ID] = true
		mergeCount++

		if !jsonOut {
			fmt.Printf("Merged: %s <- %s (similarity: %.2f)\n",
				mergedBehavior.Name, dup.BehaviorB.Name, dup.Similarity)
		}
	}

	if err := graphStore.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync changes: %w", err)
	}

	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"status":           "completed",
			"total_behaviors":  len(behaviors),
			"duplicates_found": len(duplicates),
			"merges_performed": mergeCount,
		})
	} else {
		fmt.Printf("\nDeduplication complete: %d merges performed.\n", mergeCount)
	}

	return nil
}

// runCrossStoreDedup runs deduplication across local and global stores.
func runCrossStoreDedup(ctx context.Context, root string, cfg dedup.DeduplicatorConfig, dryRun, jsonOut bool) error {
	// Open local store
	localStore, err := store.NewFileGraphStore(root)
	if err != nil {
		return fmt.Errorf("failed to open local store: %w", err)
	}
	defer localStore.Close()

	// Open global store
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	globalStore, err := store.NewFileGraphStore(homeDir)
	if err != nil {
		return fmt.Errorf("failed to open global store: %w", err)
	}
	defer globalStore.Close()

	// Create merger with LLM client if configured
	floopCfg, _ := config.Load()
	llmClient := createLLMClient(floopCfg)
	merger := dedup.NewBehaviorMerger(dedup.MergerConfig{
		UseLLM:    cfg.UseLLM && llmClient != nil,
		LLMClient: llmClient,
	})

	// Configure auto-merge based on dry-run
	crossCfg := cfg
	crossCfg.AutoMerge = !dryRun

	// Create cross-store deduplicator
	deduplicator := dedup.NewCrossStoreDeduplicatorWithConfig(localStore, globalStore, merger, crossCfg)

	// Run deduplication
	results, err := deduplicator.DeduplicateAcrossStores(ctx)
	if err != nil {
		return fmt.Errorf("cross-store deduplication failed: %w", err)
	}

	// Count results by action
	var skipped, mergedCount, none int
	for _, r := range results {
		switch r.Action {
		case "skip":
			skipped++
		case "merge":
			mergedCount++
		case "none":
			none++
		}
	}

	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"status":         "completed",
			"dry_run":        dryRun,
			"total_compared": len(results),
			"skipped":        skipped,
			"merged":         mergedCount,
			"no_duplicate":   none,
			"results":        results,
		})
	} else {
		if dryRun {
			fmt.Printf("Dry run: Cross-store deduplication analysis\n\n")
		} else {
			fmt.Printf("Cross-store deduplication complete\n\n")
		}
		fmt.Printf("Total local behaviors compared: %d\n", len(results))
		fmt.Printf("  Skipped (same ID in global):  %d\n", skipped)
		fmt.Printf("  Semantic duplicates found:    %d\n", mergedCount)
		fmt.Printf("  No duplicate found:           %d\n", none)

		// Show details of duplicates
		if mergedCount > 0 {
			fmt.Println("\nDuplicate details:")
			for _, r := range results {
				if r.Action == "merge" {
					fmt.Printf("  - Local: %s (%.2f similar to global: %s)\n",
						r.LocalBehavior.Name, r.Similarity, r.GlobalMatch.Name)
				}
			}
		}
	}

	return nil
}

// loadBehaviorsFromStore loads all behaviors from a graph store.
func loadBehaviorsFromStore(ctx context.Context, graphStore store.GraphStore) ([]models.Behavior, error) {
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, err
	}

	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		b := models.NodeToBehavior(node)
		behaviors = append(behaviors, b)
	}

	return behaviors, nil
}

// computeBehaviorSimilarity calculates similarity between two behaviors.
// Uses LLM-based comparison if available, otherwise falls back to Jaccard.
func computeBehaviorSimilarity(a, b *models.Behavior, llmClient llm.Client, useLLM bool) float64 {
	if useLLM && llmClient != nil && llmClient.Available() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := llmClient.CompareBehaviors(ctx, a, b)
		if err == nil && result != nil {
			return result.SemanticSimilarity
		}
		// Fall through to Jaccard on error
	}

	whenOverlap := similarity.ComputeWhenOverlap(a.When, b.When)
	contentSim := similarity.ComputeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
	return similarity.WeightedScore(whenOverlap, contentSim)
}
