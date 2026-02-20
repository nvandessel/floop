// Package dedup provides deduplication functionality for behaviors in the graph store.
package dedup

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/logging"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// scopedWriter is implemented by stores that support writing to a specific scope.
type scopedWriter interface {
	AddNodeToScope(ctx context.Context, node store.Node, scope constants.Scope) (string, error)
}

// StoreDeduplicator implements the Deduplicator interface for a single store.
// It provides methods for finding and merging duplicate behaviors within a store.
type StoreDeduplicator struct {
	store          store.GraphStore
	merger         *BehaviorMerger
	config         DeduplicatorConfig
	llmClient      llm.Client
	logger         *slog.Logger
	decisions      *logging.DecisionLogger
	embeddingCache *EmbeddingCache
}

// NewStoreDeduplicator creates a new StoreDeduplicator with the given store and configuration.
func NewStoreDeduplicator(s store.GraphStore, merger *BehaviorMerger, config DeduplicatorConfig) *StoreDeduplicator {
	return &StoreDeduplicator{
		store:  s,
		merger: merger,
		config: config,
	}
}

// NewStoreDeduplicatorWithLLM creates a new StoreDeduplicator with LLM support.
func NewStoreDeduplicatorWithLLM(s store.GraphStore, merger *BehaviorMerger, config DeduplicatorConfig, client llm.Client) *StoreDeduplicator {
	return &StoreDeduplicator{
		store:     s,
		merger:    merger,
		config:    config,
		llmClient: client,
	}
}

// SetLogger sets the structured logger and decision logger for observability.
func (d *StoreDeduplicator) SetLogger(logger *slog.Logger, decisions *logging.DecisionLogger) {
	d.logger = logger
	d.decisions = decisions
}

// FindDuplicates finds potential duplicates of a behavior in the store.
// Returns a list of matches sorted by similarity score (highest first).
func (d *StoreDeduplicator) FindDuplicates(ctx context.Context, behavior *models.Behavior) ([]DuplicateMatch, error) {
	// Get all behaviors from the store
	nodes, err := d.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("failed to query behaviors: %w", err)
	}

	var matches []DuplicateMatch

	for _, node := range nodes {
		// Skip self
		if node.ID == behavior.ID {
			continue
		}

		other := models.NodeToBehavior(node)
		sim := d.computeSimilarity(behavior, &other)

		if sim.score >= d.effectiveThreshold(sim.method) {
			matches = append(matches, DuplicateMatch{
				Behavior:         &other,
				Similarity:       sim.score,
				SimilarityMethod: sim.method,
				MergeRecommended: sim.score >= 0.95,
			})
		}
	}

	// Sort by similarity (highest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Similarity > matches[j].Similarity
	})

	return matches, nil
}

// MergeDuplicates merges the specified duplicate matches into the primary behavior.
// Returns the merged behavior with combined content and updated metadata.
// The original duplicate behaviors are removed from the store.
func (d *StoreDeduplicator) MergeDuplicates(ctx context.Context, matches []DuplicateMatch, primary *models.Behavior) (*models.Behavior, error) {
	if len(matches) == 0 {
		return primary, nil
	}

	// Collect all behaviors to merge
	behaviors := []*models.Behavior{primary}
	for _, match := range matches {
		behaviors = append(behaviors, match.Behavior)
	}

	// Perform the merge
	merged, err := d.merger.Merge(ctx, behaviors)
	if err != nil {
		return nil, fmt.Errorf("merge failed: %w", err)
	}

	// Remove the duplicate behaviors from the store (not the primary)
	for _, match := range matches {
		if err := d.store.DeleteNode(ctx, match.Behavior.ID); err != nil {
			// Log but continue - partial success is better than failure
			continue
		}
	}

	return merged, nil
}

// DeduplicateStore performs deduplication on the entire store.
// Analyzes all behaviors, finds duplicates, and optionally merges them
// based on the configuration provided at construction time.
func (d *StoreDeduplicator) DeduplicateStore(ctx context.Context, s store.GraphStore) (*DeduplicationReport, error) {
	// Get all behaviors from the store
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("failed to query behaviors: %w", err)
	}

	report := &DeduplicationReport{
		TotalBehaviors:  len(nodes),
		MergedBehaviors: make([]*models.Behavior, 0),
		Errors:          make([]string, 0),
	}

	// Track which behaviors have been processed/merged
	processed := make(map[string]bool)

	// Convert nodes to behaviors
	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		behaviors = append(behaviors, models.NodeToBehavior(node))
	}

	// Create embedding cache for batch pairwise comparisons so each
	// behavior's canonical text is embedded at most once.
	if d.config.UseLLM && d.llmClient != nil {
		d.embeddingCache = NewEmbeddingCache()
	}

	// Find and process duplicates
	for i := range behaviors {
		behavior := &behaviors[i]

		// Skip if already processed
		if processed[behavior.ID] {
			continue
		}
		processed[behavior.ID] = true

		// Find duplicates for this behavior
		var duplicates []DuplicateMatch
		for j := range behaviors {
			if i == j || processed[behaviors[j].ID] {
				continue
			}

			other := &behaviors[j]
			sim := d.computeSimilarity(behavior, other)

			if sim.score >= d.effectiveThreshold(sim.method) {
				duplicates = append(duplicates, DuplicateMatch{
					Behavior:         other,
					Similarity:       sim.score,
					SimilarityMethod: sim.method,
					MergeRecommended: sim.score >= 0.95,
				})
				processed[other.ID] = true
			}
		}

		if len(duplicates) == 0 {
			continue
		}

		report.DuplicatesFound += len(duplicates)

		// Merge if auto-merge is enabled
		if d.config.AutoMerge {
			merged, err := d.MergeDuplicates(ctx, duplicates, behavior)
			if err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("failed to merge %s: %v", behavior.ID, err))
				continue
			}

			report.MergesPerformed++
			report.MergedBehaviors = append(report.MergedBehaviors, merged)

			// Update the store with the merged behavior, routing to correct scope
			node := models.BehaviorToNode(merged)
			if sw, ok := s.(scopedWriter); ok {
				scope := models.ClassifyScope(merged)
				if _, err := sw.AddNodeToScope(ctx, node, scope); err != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("failed to save merged behavior %s: %v", merged.ID, err))
				}
			} else {
				if _, err := s.AddNode(ctx, node); err != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("failed to save merged behavior %s: %v", merged.ID, err))
				}
			}
		}
	}

	return report, nil
}

// similarityResult holds the score and method used for a similarity computation.
type similarityResult struct {
	score  float64
	method string
}

// effectiveThreshold returns the threshold for the given similarity method.
// Uses EmbeddingThreshold for embedding results when configured, otherwise
// falls back to SimilarityThreshold.
func (d *StoreDeduplicator) effectiveThreshold(method string) float64 {
	if method == "embedding" && d.config.EmbeddingThreshold > 0 {
		return d.config.EmbeddingThreshold
	}
	return d.config.SimilarityThreshold
}

// computeSimilarity calculates similarity between two behaviors.
// Delegates to the unified ComputeSimilarity function with this deduplicator's config.
func (d *StoreDeduplicator) computeSimilarity(a, b *models.Behavior) similarityResult {
	result := ComputeSimilarity(a, b, d.similarityConfig())
	return similarityResult{score: result.Score, method: result.Method}
}

// similarityConfig builds a SimilarityConfig from the deduplicator's state.
func (d *StoreDeduplicator) similarityConfig() SimilarityConfig {
	return SimilarityConfig{
		UseLLM:              d.config.UseLLM,
		LLMClient:           d.llmClient,
		SimilarityThreshold: d.config.SimilarityThreshold,
		EmbeddingThreshold:  d.config.EmbeddingThreshold,
		Logger:              d.logger,
		Decisions:           d.decisions,
		EmbeddingCache:      d.embeddingCache,
	}
}
