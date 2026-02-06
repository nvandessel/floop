// Package dedup provides deduplication functionality for behaviors in the graph store.
package dedup

import (
	"context"
	"fmt"
	"strings"
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
		b := nodeToBehavior(node)
		behaviors = append(behaviors, b)
	}

	return behaviors, nil
}

// nodeToBehavior converts a store.Node to a models.Behavior.
func nodeToBehavior(node store.Node) models.Behavior {
	b := models.Behavior{
		ID: node.ID,
	}

	// Extract kind
	if kind, ok := node.Content["kind"].(string); ok {
		b.Kind = models.BehaviorKind(kind)
	}

	// Extract name
	if name, ok := node.Content["name"].(string); ok {
		b.Name = name
	}

	// Extract when conditions
	if when, ok := node.Content["when"].(map[string]interface{}); ok {
		b.When = when
	}

	// Extract content
	if content, ok := node.Content["content"].(map[string]interface{}); ok {
		if canonical, ok := content["canonical"].(string); ok {
			b.Content.Canonical = canonical
		}
		if expanded, ok := content["expanded"].(string); ok {
			b.Content.Expanded = expanded
		}
		if summary, ok := content["summary"].(string); ok {
			b.Content.Summary = summary
		}
		if structured, ok := content["structured"].(map[string]interface{}); ok {
			b.Content.Structured = structured
		}
	} else if content, ok := node.Content["content"].(models.BehaviorContent); ok {
		b.Content = content
	}

	// Extract confidence from metadata
	if confidence, ok := node.Metadata["confidence"].(float64); ok {
		b.Confidence = confidence
	}

	// Extract priority from metadata
	if priority, ok := node.Metadata["priority"].(int); ok {
		b.Priority = priority
	}

	// Extract provenance
	if provenance, ok := node.Metadata["provenance"].(map[string]interface{}); ok {
		if sourceType, ok := provenance["source_type"].(string); ok {
			b.Provenance.SourceType = models.SourceType(sourceType)
		}
		if createdAt, ok := provenance["created_at"].(time.Time); ok {
			b.Provenance.CreatedAt = createdAt
		} else if createdAtStr, ok := provenance["created_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
				b.Provenance.CreatedAt = t
			}
		}
		if author, ok := provenance["author"].(string); ok {
			b.Provenance.Author = author
		}
	}

	// Extract stats
	if stats, ok := node.Metadata["stats"].(map[string]interface{}); ok {
		if activated, ok := stats["times_activated"].(int); ok {
			b.Stats.TimesActivated = activated
		}
		if followed, ok := stats["times_followed"].(int); ok {
			b.Stats.TimesFollowed = followed
		}
		if confirmed, ok := stats["times_confirmed"].(int); ok {
			b.Stats.TimesConfirmed = confirmed
		}
		if overridden, ok := stats["times_overridden"].(int); ok {
			b.Stats.TimesOverridden = overridden
		}
	}

	return b
}

// computeSimilarity calculates similarity between two behaviors.
// Uses LLM-based comparison if available and configured, otherwise falls back
// to Jaccard word overlap combined with when-condition overlap.
func (d *CrossStoreDeduplicator) computeSimilarity(a, b *models.Behavior) float64 {
	// Try LLM-based comparison if configured and available
	if d.config.UseLLM && d.llmClient != nil && d.llmClient.Available() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := d.llmClient.CompareBehaviors(ctx, a, b)
		if err == nil && result != nil {
			return result.SemanticSimilarity
		}
		// Fall through to Jaccard on error
	}

	// Fallback: weighted Jaccard similarity
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
func (d *CrossStoreDeduplicator) computeWhenOverlap(a, b map[string]interface{}) float64 {
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
func (d *CrossStoreDeduplicator) computeContentSimilarity(a, b string) float64 {
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

// valuesEqual compares two interface{} values for equality.
func valuesEqual(a, b interface{}) bool {
	// Handle string comparison
	aStr, aIsStr := a.(string)
	bStr, bIsStr := b.(string)
	if aIsStr && bIsStr {
		return aStr == bStr
	}

	// Handle slice comparison (both must contain at least one common element)
	aSlice, aIsSlice := a.([]interface{})
	bSlice, bIsSlice := b.([]interface{})
	if aIsSlice && bIsSlice {
		for _, av := range aSlice {
			for _, bv := range bSlice {
				if valuesEqual(av, bv) {
					return true
				}
			}
		}
		return false
	}

	// Handle string slice comparison
	aStrSlice, aIsStrSlice := a.([]string)
	bStrSlice, bIsStrSlice := b.([]string)
	if aIsStrSlice && bIsStrSlice {
		for _, av := range aStrSlice {
			for _, bv := range bStrSlice {
				if av == bv {
					return true
				}
			}
		}
		return false
	}

	// Fallback to direct equality
	return a == b
}

// tokenize splits a string into word tokens.
func tokenize(s string) []string {
	words := make([]string, 0)
	current := ""
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			current += string(r)
		} else if current != "" {
			words = append(words, current)
			current = ""
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
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
			Source:   edge.Source,
			Target:   newID,
			Kind:     edge.Kind,
			Metadata: edge.Metadata,
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
			Source:   newID,
			Target:   edge.Target,
			Kind:     edge.Kind,
			Metadata: edge.Metadata,
		}
		if err := s.AddEdge(ctx, newEdge); err != nil {
			return fmt.Errorf("failed to add redirected outbound edge: %w", err)
		}
	}

	return nil
}
