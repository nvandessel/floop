// Package dedup provides deduplication functionality for behaviors in the graph store.
package dedup

import (
	"context"
	"fmt"
	"time"

	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// DeduplicationResult represents the outcome of deduplicating a single behavior
// across stores.
type DeduplicationResult struct {
	// LocalBehavior is the behavior from the local store being compared.
	LocalBehavior *models.Behavior `json:"local_behavior" yaml:"local_behavior"`

	// Action indicates what deduplication action was taken.
	// Possible values: "skip" (same ID exists), "merge" (semantic duplicate merged),
	// "none" (no duplicate found)
	Action string `json:"action" yaml:"action"`

	// GlobalMatch is the matching behavior from the global store, if found.
	GlobalMatch *models.Behavior `json:"global_match,omitempty" yaml:"global_match,omitempty"`

	// MergedBehavior is the result of merging, if a merge was performed.
	MergedBehavior *models.Behavior `json:"merged_behavior,omitempty" yaml:"merged_behavior,omitempty"`

	// Similarity is the similarity score if a semantic duplicate was found.
	Similarity float64 `json:"similarity,omitempty" yaml:"similarity,omitempty"`

	// Error contains any error that occurred during processing.
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
}

// CrossStoreDeduplicator handles deduplication of behaviors between a local
// and global store. It identifies behaviors that exist in both stores and
// determines how to resolve them.
type CrossStoreDeduplicator struct {
	// localStore is the local behavior store (project-specific)
	localStore store.GraphStore

	// globalStore is the global behavior store (user-wide)
	globalStore store.GraphStore

	// merger handles combining behaviors when semantic duplicates are found
	merger *BehaviorMerger

	// config contains deduplication settings
	config DeduplicatorConfig

	// llmClient is the optional LLM client for semantic comparison
	llmClient llm.Client
}

// NewCrossStoreDeduplicator creates a new CrossStoreDeduplicator with the given
// local and global stores and a behavior merger.
func NewCrossStoreDeduplicator(localStore, globalStore store.GraphStore, merger *BehaviorMerger) *CrossStoreDeduplicator {
	return &CrossStoreDeduplicator{
		localStore:  localStore,
		globalStore: globalStore,
		merger:      merger,
		config:      DefaultConfig(),
	}
}

// NewCrossStoreDeduplicatorWithConfig creates a new CrossStoreDeduplicator with
// custom configuration.
func NewCrossStoreDeduplicatorWithConfig(localStore, globalStore store.GraphStore, merger *BehaviorMerger, config DeduplicatorConfig) *CrossStoreDeduplicator {
	return &CrossStoreDeduplicator{
		localStore:  localStore,
		globalStore: globalStore,
		merger:      merger,
		config:      config,
	}
}

// NewCrossStoreDeduplicatorWithLLM creates a new CrossStoreDeduplicator with
// LLM support for semantic comparison.
func NewCrossStoreDeduplicatorWithLLM(localStore, globalStore store.GraphStore, merger *BehaviorMerger, config DeduplicatorConfig, client llm.Client) *CrossStoreDeduplicator {
	return &CrossStoreDeduplicator{
		localStore:  localStore,
		globalStore: globalStore,
		merger:      merger,
		config:      config,
		llmClient:   client,
	}
}

// DeduplicateAcrossStores performs deduplication of behaviors between the local
// and global stores.
//
// Logic:
//   - Same ID in both stores: Local wins (skip, no action needed)
//   - Semantic duplicates with different IDs: Use merger to merge, update edges
//     to point to survivor
//   - Compare behaviors from local store against global store
func (d *CrossStoreDeduplicator) DeduplicateAcrossStores(ctx context.Context) ([]DeduplicationResult, error) {
	// Get all behaviors from the local store
	localBehaviors, err := d.getBehaviorsFromStore(ctx, d.localStore)
	if err != nil {
		return nil, fmt.Errorf("failed to get local behaviors: %w", err)
	}

	// Get all behaviors from the global store
	globalBehaviors, err := d.getBehaviorsFromStore(ctx, d.globalStore)
	if err != nil {
		return nil, fmt.Errorf("failed to get global behaviors: %w", err)
	}

	// Build a map of global behaviors by ID for quick lookup
	globalByID := make(map[string]*models.Behavior)
	for i := range globalBehaviors {
		globalByID[globalBehaviors[i].ID] = &globalBehaviors[i]
	}

	results := make([]DeduplicationResult, 0, len(localBehaviors))

	// Process each local behavior
	for i := range localBehaviors {
		local := &localBehaviors[i]
		result := d.deduplicateBehavior(ctx, local, globalBehaviors, globalByID)
		results = append(results, result)
	}

	return results, nil
}

// deduplicateBehavior handles deduplication for a single local behavior.
func (d *CrossStoreDeduplicator) deduplicateBehavior(
	ctx context.Context,
	local *models.Behavior,
	globalBehaviors []models.Behavior,
	globalByID map[string]*models.Behavior,
) DeduplicationResult {
	result := DeduplicationResult{
		LocalBehavior: local,
		Action:        "none",
	}

	// Check for exact ID match - local wins, skip
	if globalMatch, exists := globalByID[local.ID]; exists {
		result.Action = "skip"
		result.GlobalMatch = globalMatch
		return result
	}

	// Check for semantic duplicates
	var bestMatch *models.Behavior
	var bestSimilarity float64

	for j := range globalBehaviors {
		global := &globalBehaviors[j]

		// Skip if same ID (already handled above)
		if global.ID == local.ID {
			continue
		}

		similarity := d.computeSimilarity(local, global)
		if similarity > bestSimilarity {
			bestSimilarity = similarity
			bestMatch = global
		}
	}

	// If similarity is above threshold, merge
	if bestSimilarity >= d.config.SimilarityThreshold && bestMatch != nil {
		result.Action = "merge"
		result.GlobalMatch = bestMatch
		result.Similarity = bestSimilarity

		// Perform merge if auto-merge is enabled
		if d.config.AutoMerge && d.merger != nil {
			merged, err := d.merger.Merge(ctx, []*models.Behavior{local, bestMatch})
			if err != nil {
				result.Error = fmt.Sprintf("merge failed: %v", err)
			} else {
				result.MergedBehavior = merged

				// Update edges to point to the survivor
				if err := d.updateEdges(ctx, local.ID, bestMatch.ID, merged.ID); err != nil {
					result.Error = fmt.Sprintf("edge update failed: %v", err)
				}
			}
		}
	}

	return result
}

// getBehaviorsFromStore retrieves all behaviors from a store.
func (d *CrossStoreDeduplicator) getBehaviorsFromStore(ctx context.Context, s store.GraphStore) ([]models.Behavior, error) {
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{
		"kind": "behavior",
	})
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

// computeSimilarity calculates similarity between two behaviors.
// Delegates to the unified ComputeSimilarity function.
func (d *CrossStoreDeduplicator) computeSimilarity(a, b *models.Behavior) float64 {
	result := ComputeSimilarity(a, b, SimilarityConfig{
		UseLLM:              d.config.UseLLM,
		LLMClient:           d.llmClient,
		SimilarityThreshold: d.config.SimilarityThreshold,
	})
	return result.Score
}

// updateEdges updates edges in both stores to point to the merged behavior.
// It redirects edges that referenced either the local or global behavior to
// point to the merged behavior instead.
func (d *CrossStoreDeduplicator) updateEdges(ctx context.Context, localID, globalID, mergedID string) error {
	// Update edges in the local store
	if err := d.redirectEdges(ctx, d.localStore, localID, mergedID); err != nil {
		return fmt.Errorf("failed to update local edges: %w", err)
	}

	// Update edges in the global store
	if err := d.redirectEdges(ctx, d.globalStore, globalID, mergedID); err != nil {
		return fmt.Errorf("failed to update global edges: %w", err)
	}

	return nil
}

// redirectEdges redirects all edges referencing oldID to point to newID instead.
func (d *CrossStoreDeduplicator) redirectEdges(ctx context.Context, s store.GraphStore, oldID, newID string) error {
	// Get all edges connected to the old node (both directions)
	inbound, err := s.GetEdges(ctx, oldID, store.DirectionInbound, "")
	if err != nil {
		return fmt.Errorf("failed to get inbound edges: %w", err)
	}

	outbound, err := s.GetEdges(ctx, oldID, store.DirectionOutbound, "")
	if err != nil {
		return fmt.Errorf("failed to get outbound edges: %w", err)
	}

	// Redirect inbound edges (where oldID is the target)
	for _, edge := range inbound {
		// Remove old edge
		if err := s.RemoveEdge(ctx, edge.Source, oldID, edge.Kind); err != nil {
			return fmt.Errorf("failed to remove inbound edge: %w", err)
		}
		// Add new edge pointing to newID
		newEdge := store.Edge{
			Source:        edge.Source,
			Target:        newID,
			Kind:          edge.Kind,
			Weight:        edge.Weight,
			CreatedAt:     edge.CreatedAt,
			LastActivated: edge.LastActivated,
			Metadata:      edge.Metadata,
		}
		// Defensive fallback for legacy edges missing Weight/CreatedAt
		if newEdge.Weight <= 0 {
			newEdge.Weight = 1.0
		}
		if newEdge.CreatedAt.IsZero() {
			newEdge.CreatedAt = time.Now()
		}
		if err := s.AddEdge(ctx, newEdge); err != nil {
			return fmt.Errorf("failed to add redirected inbound edge: %w", err)
		}
	}

	// Redirect outbound edges (where oldID is the source)
	for _, edge := range outbound {
		// Remove old edge
		if err := s.RemoveEdge(ctx, oldID, edge.Target, edge.Kind); err != nil {
			return fmt.Errorf("failed to remove outbound edge: %w", err)
		}
		// Add new edge from newID
		newEdge := store.Edge{
			Source:        newID,
			Target:        edge.Target,
			Kind:          edge.Kind,
			Weight:        edge.Weight,
			CreatedAt:     edge.CreatedAt,
			LastActivated: edge.LastActivated,
			Metadata:      edge.Metadata,
		}
		// Defensive fallback for legacy edges missing Weight/CreatedAt
		if newEdge.Weight <= 0 {
			newEdge.Weight = 1.0
		}
		if newEdge.CreatedAt.IsZero() {
			newEdge.CreatedAt = time.Now()
		}
		if err := s.AddEdge(ctx, newEdge); err != nil {
			return fmt.Errorf("failed to add redirected outbound edge: %w", err)
		}
	}

	return nil
}
