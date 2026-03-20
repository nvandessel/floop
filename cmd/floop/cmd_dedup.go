package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/constants"
	"github.com/nvandessel/floop/internal/dedup"
	"github.com/nvandessel/floop/internal/edges"
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
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
  floop deduplicate                  # Find duplicates across both stores (default)
  floop deduplicate --dry-run        # Show what would be merged
  floop deduplicate --threshold 0.8  # Use lower similarity threshold
  floop deduplicate --scope global   # Deduplicate global store only
  floop deduplicate --scope local    # Deduplicate local store only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			threshold, _ := cmd.Flags().GetFloat64("threshold")
			embeddingThreshold, _ := cmd.Flags().GetFloat64("embedding-threshold")
			scope, _ := cmd.Flags().GetString("scope")

			// Validate scope
			storeScope := constants.Scope(scope)
			if !storeScope.Valid() {
				return fmt.Errorf("invalid scope: %s (must be local, global, or both)", scope)
			}

			// Check initialization — for ScopeBoth, degrade gracefully if one store is missing
			hasLocal := true
			hasGlobal := true

			if storeScope == store.ScopeLocal || storeScope == store.ScopeBoth {
				floopDir := filepath.Join(root, ".floop")
				if _, err := os.Stat(floopDir); os.IsNotExist(err) {
					hasLocal = false
					if storeScope == store.ScopeLocal {
						return fmt.Errorf(".floop not initialized. Run 'floop init' first")
					}
				}
			}

			if storeScope == store.ScopeGlobal || storeScope == store.ScopeBoth {
				globalPath, err := store.GlobalFloopPath()
				if err != nil {
					hasGlobal = false
					if storeScope == store.ScopeGlobal {
						return fmt.Errorf("failed to get global path: %w", err)
					}
				} else if _, err := os.Stat(globalPath); os.IsNotExist(err) {
					hasGlobal = false
					if storeScope == store.ScopeGlobal {
						return fmt.Errorf("global .floop not initialized. Run 'floop init --global' first")
					}
				}
			}

			if storeScope == store.ScopeBoth {
				if !hasLocal && !hasGlobal {
					return fmt.Errorf("no .floop stores initialized. Run 'floop init' first")
				}
				if !hasLocal {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning: local .floop not initialized, deduplicating global store only")
					storeScope = store.ScopeGlobal
				} else if !hasGlobal {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning: global .floop not initialized, deduplicating local store only")
					storeScope = store.ScopeLocal
				}
			}

			ctx := context.Background()

			// Load config and create LLM client once
			floopCfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
			}
			useLLM := floopCfg != nil && floopCfg.LLM.Enabled && floopCfg.LLM.Provider != ""
			llmClient := createLLMClient(floopCfg)

			// Configure deduplication
			dedupConfig := dedup.DeduplicatorConfig{
				SimilarityThreshold: threshold,
				EmbeddingThreshold:  embeddingThreshold,
				AutoMerge:           !dryRun,
				UseLLM:              useLLM,
				MaxBatchSize:        100,
			}

			// Handle cross-store deduplication
			if storeScope == store.ScopeBoth {
				return runCrossStoreDedup(ctx, root, dedupConfig, llmClient, dryRun, jsonOut)
			}

			// Single store deduplication
			return runSingleStoreDedup(ctx, root, storeScope, dedupConfig, llmClient, dryRun, jsonOut)
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show duplicates without merging")
	cmd.Flags().Float64("threshold", constants.DefaultAutoMergeThreshold, "Similarity threshold for duplicate detection (0.0-1.0)")
	cmd.Flags().Float64("embedding-threshold", constants.DefaultEmbeddingDedupThreshold, "Cosine similarity threshold for embedding-based duplicate detection (0.0-1.0)")
	cmd.Flags().String("scope", "both", "Store scope: local, global, or both")

	return cmd
}

// duplicatePair represents a pair of behaviors detected as duplicates.
type duplicatePair struct {
	BehaviorA  *models.Behavior
	BehaviorB  *models.Behavior
	Similarity float64
}

// runSingleStoreDedup runs deduplication on a single store.
func runSingleStoreDedup(ctx context.Context, root string, scope store.StoreScope, cfg dedup.DeduplicatorConfig, llmClient llm.Client, dryRun, jsonOut bool) error {
	// Open the appropriate store
	var graphStore store.GraphStore
	var err error

	switch scope {
	case store.ScopeLocal:
		graphStore, err = store.NewSQLiteGraphStore(root)
	case store.ScopeGlobal:
		globalPath, pathErr := store.GlobalFloopPath()
		if pathErr != nil {
			return fmt.Errorf("failed to get global path: %w", pathErr)
		}
		graphStore, err = store.NewSQLiteGraphStore(filepath.Dir(globalPath))
	default:
		return fmt.Errorf("runSingleStoreDedup requires local or global scope, got %q", scope)
	}

	if err != nil {
		return fmt.Errorf("failed to open store: %w", err)
	}
	defer graphStore.Close()

	// Load all behaviors
	behaviors, err := edges.LoadBehaviorsFromStore(ctx, graphStore)
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

	// Find duplicate pairs
	duplicates := findDuplicatePairs(behaviors, cfg, llmClient)

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
	mergeCount := mergeDuplicatePairs(ctx, graphStore, duplicates, llmClient, jsonOut)

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

// findDuplicatePairs performs pairwise similarity comparison across all behaviors,
// returning pairs that exceed the configured similarity threshold.
func findDuplicatePairs(behaviors []models.Behavior, cfg dedup.DeduplicatorConfig, llmClient llm.Client) []duplicatePair {
	useLLM := cfg.UseLLM && llmClient != nil

	// Create embedding cache so each behavior text is embedded at most once.
	var cache *dedup.EmbeddingCache
	if useLLM {
		cache = dedup.NewEmbeddingCache()
	}

	var duplicates []duplicatePair
	for i := 0; i < len(behaviors); i++ {
		for j := i + 1; j < len(behaviors); j++ {
			sim := edges.ComputeBehaviorSimilarity(&behaviors[i], &behaviors[j], llmClient, useLLM, cache)
			if sim >= cfg.SimilarityThreshold {
				duplicates = append(duplicates, duplicatePair{
					BehaviorA:  &behaviors[i],
					BehaviorB:  &behaviors[j],
					Similarity: sim,
				})
			}
		}
	}
	return duplicates
}

// mergeDuplicatePairs merges each duplicate pair, updating the store.
// Returns the number of successful merges.
func mergeDuplicatePairs(ctx context.Context, graphStore store.GraphStore, duplicates []duplicatePair, llmClient llm.Client, jsonOut bool) int {
	mergeCount := 0
	merged := make(map[string]bool)

	merger := dedup.NewBehaviorMerger(dedup.MergerConfig{
		UseLLM:    llmClient != nil,
		LLMClient: llmClient,
	})

	for _, dup := range duplicates {
		if merged[dup.BehaviorA.ID] || merged[dup.BehaviorB.ID] {
			continue
		}

		mergedBehavior, err := merger.Merge(ctx, []*models.Behavior{dup.BehaviorA, dup.BehaviorB})
		if err != nil {
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "Warning: failed to merge %s and %s: %v\n",
					dup.BehaviorA.ID, dup.BehaviorB.ID, err)
			}
			continue
		}

		mergedNode := models.BehaviorToNode(mergedBehavior)
		mergedNode.ID = dup.BehaviorA.ID
		if err := graphStore.UpdateNode(ctx, mergedNode); err != nil {
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "Warning: failed to save merged behavior: %v\n", err)
			}
			continue
		}

		if err := graphStore.DeleteNode(ctx, dup.BehaviorB.ID); err != nil {
			if !jsonOut {
				fmt.Fprintf(os.Stderr, "Warning: failed to delete merged behavior %s: %v\n",
					dup.BehaviorB.ID, err)
			}
		}

		merged[dup.BehaviorB.ID] = true
		mergeCount++

		if !jsonOut {
			fmt.Printf("Merged: %s <- %s (similarity: %.2f)\n",
				mergedBehavior.Name, dup.BehaviorB.Name, dup.Similarity)
		}
	}
	return mergeCount
}

// runCrossStoreDedup runs deduplication across local and global stores.
func runCrossStoreDedup(ctx context.Context, root string, cfg dedup.DeduplicatorConfig, llmClient llm.Client, dryRun, jsonOut bool) error {
	// Open local store
	localStore, err := store.NewSQLiteGraphStore(root)
	if err != nil {
		return fmt.Errorf("failed to open local store: %w", err)
	}
	defer localStore.Close()

	// Open global store using same path resolution as pre-check
	globalPath, err := store.GlobalFloopPath()
	if err != nil {
		return fmt.Errorf("failed to get global path: %w", err)
	}
	globalStore, err := store.NewSQLiteGraphStore(filepath.Dir(globalPath))
	if err != nil {
		return fmt.Errorf("failed to open global store: %w", err)
	}
	defer globalStore.Close()

	// Create merger with LLM client
	merger := dedup.NewBehaviorMerger(dedup.MergerConfig{
		UseLLM:    cfg.UseLLM && llmClient != nil,
		LLMClient: llmClient,
	})

	// Configure auto-merge based on dry-run
	crossCfg := cfg
	crossCfg.AutoMerge = !dryRun

	// Create cross-store deduplicator with LLM client for embedding-based comparison
	deduplicator := dedup.NewCrossStoreDeduplicatorWithLLM(localStore, globalStore, merger, crossCfg, llmClient)

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
