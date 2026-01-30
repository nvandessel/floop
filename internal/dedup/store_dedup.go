// Package dedup provides deduplication functionality for behaviors in the graph store.
package dedup

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// StoreDeduplicator implements the Deduplicator interface for a single store.
// It provides methods for finding and merging duplicate behaviors within a store.
type StoreDeduplicator struct {
	store  store.GraphStore
	merger *BehaviorMerger
	config DeduplicatorConfig
}

// NewStoreDeduplicator creates a new StoreDeduplicator with the given store and configuration.
func NewStoreDeduplicator(s store.GraphStore, merger *BehaviorMerger, config DeduplicatorConfig) *StoreDeduplicator {
	return &StoreDeduplicator{
		store:  s,
		merger: merger,
		config: config,
	}
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

		other := nodeToBehavior(node)
		similarity := d.computeSimilarity(behavior, &other)

		if similarity >= d.config.SimilarityThreshold {
			matches = append(matches, DuplicateMatch{
				Behavior:         &other,
				Similarity:       similarity,
				SimilarityMethod: "jaccard",
				MergeRecommended: similarity >= 0.95,
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
		behaviors = append(behaviors, nodeToBehavior(node))
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
			similarity := d.computeSimilarity(behavior, other)

			if similarity >= d.config.SimilarityThreshold {
				duplicates = append(duplicates, DuplicateMatch{
					Behavior:         other,
					Similarity:       similarity,
					SimilarityMethod: "jaccard",
					MergeRecommended: similarity >= 0.95,
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

			// Update the store with the merged behavior
			node := behaviorToNode(merged)
			if _, err := s.AddNode(ctx, node); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("failed to save merged behavior %s: %v", merged.ID, err))
			}
		}
	}

	return report, nil
}

// computeSimilarity calculates similarity between two behaviors using
// Jaccard word overlap combined with when-condition overlap.
func (d *StoreDeduplicator) computeSimilarity(a, b *models.Behavior) float64 {
	score := 0.0

	// Check 'when' overlap (40% weight)
	whenOverlap := d.computeWhenOverlap(a.When, b.When)
	score += whenOverlap * 0.4

	// Check content similarity using Jaccard word overlap (60% weight)
	contentSim := d.computeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
	score += contentSim * 0.6

	return score
}

// computeWhenOverlap calculates overlap between two when predicates.
func (d *StoreDeduplicator) computeWhenOverlap(a, b map[string]interface{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0 // Both empty = perfect overlap
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0 // One empty = no overlap
	}

	matches := 0
	total := len(a) + len(b)

	for key, valueA := range a {
		if valueB, exists := b[key]; exists {
			if valuesEqual(valueA, valueB) {
				matches += 2 // Count both sides as matched
			}
		}
	}

	if total == 0 {
		return 0.0
	}
	return float64(matches) / float64(total)
}

// computeContentSimilarity calculates Jaccard similarity between two strings.
func (d *StoreDeduplicator) computeContentSimilarity(a, b string) float64 {
	wordsA := tokenize(a)
	wordsB := tokenize(b)

	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	setA := make(map[string]bool)
	for _, w := range wordsA {
		setA[strings.ToLower(w)] = true
	}

	setB := make(map[string]bool)
	for _, w := range wordsB {
		setB[strings.ToLower(w)] = true
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// behaviorToNode converts a models.Behavior to a store.Node.
func behaviorToNode(b *models.Behavior) store.Node {
	return store.Node{
		ID:   b.ID,
		Kind: "behavior",
		Content: map[string]interface{}{
			"name":    b.Name,
			"kind":    string(b.Kind),
			"when":    b.When,
			"content": b.Content,
		},
		Metadata: map[string]interface{}{
			"confidence": b.Confidence,
			"priority":   b.Priority,
			"provenance": b.Provenance,
		},
	}
}
