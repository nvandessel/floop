// Package dedup provides deduplication functionality for behaviors in the graph store.
package dedup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/sanitize"
)

// BehaviorMerger handles merging multiple behaviors into one.
// It uses LLM-assisted merging when available, with rule-based fallback.
type BehaviorMerger struct {
	// llmClient is the optional LLM client for smart merging
	llmClient llm.Client

	// useLLM controls whether LLM merging is attempted
	useLLM bool
}

// MergerConfig configures the BehaviorMerger.
type MergerConfig struct {
	// LLMClient is the optional LLM client for semantic merging
	LLMClient llm.Client

	// UseLLM enables LLM-based merging when the client is available
	UseLLM bool
}

// NewBehaviorMerger creates a new BehaviorMerger with the given configuration.
func NewBehaviorMerger(cfg MergerConfig) *BehaviorMerger {
	return &BehaviorMerger{
		llmClient: cfg.LLMClient,
		useLLM:    cfg.UseLLM,
	}
}

// Merge combines multiple behaviors into a single unified behavior.
// Uses LLM-assisted merging when available, otherwise falls back to rule-based.
func (m *BehaviorMerger) Merge(ctx context.Context, behaviors []*models.Behavior) (*models.Behavior, error) {
	if len(behaviors) == 0 {
		return nil, fmt.Errorf("no behaviors to merge")
	}

	if len(behaviors) == 1 {
		return behaviors[0], nil
	}

	// Try LLM-assisted merge if available
	if m.shouldUseLLM() {
		result, err := m.llmMerge(ctx, behaviors)
		if err == nil {
			return result, nil
		}
		// Fall through to rule-based on error
	}

	// Rule-based merge
	return m.ruleMerge(behaviors), nil
}

// shouldUseLLM checks if LLM merging should be attempted.
func (m *BehaviorMerger) shouldUseLLM() bool {
	return m.useLLM && m.llmClient != nil && m.llmClient.Available()
}

// llmMerge performs LLM-assisted behavior merging.
func (m *BehaviorMerger) llmMerge(ctx context.Context, behaviors []*models.Behavior) (*models.Behavior, error) {
	result, err := m.llmClient.MergeBehaviors(ctx, behaviors)
	if err != nil {
		return nil, fmt.Errorf("llm merge failed: %w", err)
	}

	if result.Merged == nil {
		return nil, fmt.Errorf("llm merge returned nil result")
	}

	merged := result.Merged

	// Sanitize LLM-generated content to prevent stored prompt injection
	merged.Content.Canonical = sanitize.SanitizeBehaviorContent(merged.Content.Canonical)
	merged.Content.Expanded = sanitize.SanitizeBehaviorContent(merged.Content.Expanded)
	merged.Content.Summary = sanitize.SanitizeBehaviorContent(merged.Content.Summary)
	merged.Name = sanitize.SanitizeBehaviorName(merged.Name)
	for i, tag := range merged.Content.Tags {
		merged.Content.Tags[i] = sanitize.SanitizeBehaviorName(tag)
	}

	// Ensure the merged behavior has proper metadata
	merged.ID = generateMergedID(behaviors)
	merged.Provenance = createMergeProvenance(behaviors)

	// Merge when conditions from all sources
	merged.When = mergeWhenConditions(behaviors)

	// Track merge relationships
	for _, b := range behaviors {
		if b.ID != "" {
			merged.SimilarTo = append(merged.SimilarTo, models.SimilarityLink{
				ID:    b.ID,
				Score: 1.0, // Merged behaviors have perfect similarity to sources
			})
		}
	}

	return merged, nil
}

// ruleMerge performs rule-based behavior merging without LLM.
func (m *BehaviorMerger) ruleMerge(behaviors []*models.Behavior) *models.Behavior {
	// Use the first behavior as the base
	primary := behaviors[0]

	merged := &models.Behavior{
		ID:   generateMergedID(behaviors),
		Name: generateMergedName(behaviors),
		Kind: selectBestKind(behaviors),
		When: mergeWhenConditions(behaviors),
		Content: models.BehaviorContent{
			Canonical: mergeCanonicalContent(behaviors),
			Expanded:  mergeExpandedContent(behaviors),
		},
		Provenance: createMergeProvenance(behaviors),
		Confidence: averageConfidence(behaviors),
		Priority:   maxPriority(behaviors),
	}

	// Sanitize merged content to prevent stored prompt injection
	merged.Content.Canonical = sanitize.SanitizeBehaviorContent(merged.Content.Canonical)
	merged.Content.Expanded = sanitize.SanitizeBehaviorContent(merged.Content.Expanded)
	merged.Content.Summary = sanitize.SanitizeBehaviorContent(merged.Content.Summary)
	merged.Name = sanitize.SanitizeBehaviorName(merged.Name)
	for i, tag := range merged.Content.Tags {
		merged.Content.Tags[i] = sanitize.SanitizeBehaviorName(tag)
	}

	// Track merge relationships
	for _, b := range behaviors {
		if b.ID != "" && b.ID != primary.ID {
			merged.SimilarTo = append(merged.SimilarTo, models.SimilarityLink{
				ID:    b.ID,
				Score: 1.0,
			})
		}
	}

	return merged
}

// generateMergedID creates a unique ID for the merged behavior.
func generateMergedID(behaviors []*models.Behavior) string {
	if len(behaviors) > 0 && behaviors[0].ID != "" {
		return behaviors[0].ID + "-merged"
	}
	return fmt.Sprintf("merged-%d", time.Now().UnixNano())
}

// generateMergedName creates a name for the merged behavior.
func generateMergedName(behaviors []*models.Behavior) string {
	if len(behaviors) == 0 {
		return "Merged Behavior"
	}

	// Use the first non-empty name
	for _, b := range behaviors {
		if b.Name != "" {
			return b.Name + " (merged)"
		}
	}

	return "Merged Behavior"
}

// selectBestKind chooses the most appropriate kind for the merged behavior.
func selectBestKind(behaviors []*models.Behavior) models.BehaviorKind {
	// Priority: procedure > constraint > directive > preference
	kindPriority := map[models.BehaviorKind]int{
		models.BehaviorKindProcedure:  4,
		models.BehaviorKindConstraint: 3,
		models.BehaviorKindDirective:  2,
		models.BehaviorKindPreference: 1,
	}

	var best models.BehaviorKind
	bestPriority := 0

	for _, b := range behaviors {
		if priority, ok := kindPriority[b.Kind]; ok && priority > bestPriority {
			best = b.Kind
			bestPriority = priority
		}
	}

	if best == "" {
		return models.BehaviorKindDirective
	}
	return best
}

// mergeWhenConditions unions all when conditions from the behaviors.
// Keys and string values are sanitized to prevent stored prompt injection.
func mergeWhenConditions(behaviors []*models.Behavior) map[string]interface{} {
	result := make(map[string]interface{})

	for _, b := range behaviors {
		for key, value := range b.When {
			// Sanitize the key to prevent injection via condition keys.
			cleanKey := sanitize.SanitizeBehaviorName(key)
			if cleanKey == "" {
				continue
			}
			// Sanitize the value to prevent injection via condition values.
			cleanValue := sanitizeWhenValue(value)
			if existing, ok := result[cleanKey]; ok {
				// Try to merge values
				result[cleanKey] = mergeConditionValues(existing, cleanValue)
			} else {
				result[cleanKey] = cleanValue
			}
		}
	}

	return result
}

// sanitizeWhenValue sanitizes a when condition value. String values are
// sanitized using SanitizeBehaviorContent. Slices are sanitized recursively.
// Non-string types (int, bool, etc.) are passed through unchanged.
func sanitizeWhenValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return sanitize.SanitizeBehaviorContent(val)
	case []string:
		clean := make([]string, 0, len(val))
		for _, s := range val {
			c := sanitize.SanitizeBehaviorContent(s)
			if c != "" {
				clean = append(clean, c)
			}
		}
		return clean
	case []interface{}:
		clean := make([]interface{}, 0, len(val))
		for _, item := range val {
			clean = append(clean, sanitizeWhenValue(item))
		}
		return clean
	default:
		return v
	}
}

// mergeConditionValues combines two condition values.
func mergeConditionValues(a, b interface{}) interface{} {
	// If both are strings and equal, keep one
	aStr, aIsStr := a.(string)
	bStr, bIsStr := b.(string)
	if aIsStr && bIsStr {
		if aStr == bStr {
			return aStr
		}
		// Different strings - make a slice
		return []string{aStr, bStr}
	}

	// If both are slices, union them
	aSlice, aIsSlice := a.([]string)
	bSlice, bIsSlice := b.([]string)
	if aIsSlice && bIsSlice {
		seen := make(map[string]bool)
		var result []string
		for _, v := range aSlice {
			if !seen[v] {
				result = append(result, v)
				seen[v] = true
			}
		}
		for _, v := range bSlice {
			if !seen[v] {
				result = append(result, v)
				seen[v] = true
			}
		}
		return result
	}

	// If a is a slice and b is a string, add b to the slice
	if aIsSlice && bIsStr {
		for _, v := range aSlice {
			if v == bStr {
				return aSlice // Already present
			}
		}
		return append(aSlice, bStr)
	}

	// If b is a slice and a is a string, add a to the slice
	if bIsSlice && aIsStr {
		for _, v := range bSlice {
			if v == aStr {
				return bSlice // Already present
			}
		}
		return append([]string{aStr}, bSlice...)
	}

	// Default: keep the first value
	return a
}

// mergeCanonicalContent combines canonical content from all behaviors.
func mergeCanonicalContent(behaviors []*models.Behavior) string {
	var parts []string
	seen := make(map[string]bool)

	for _, b := range behaviors {
		content := strings.TrimSpace(b.Content.Canonical)
		if content != "" && !seen[content] {
			parts = append(parts, content)
			seen[content] = true
		}
	}

	if len(parts) == 1 {
		return parts[0]
	}

	return strings.Join(parts, "; ")
}

// mergeExpandedContent combines expanded content from all behaviors.
func mergeExpandedContent(behaviors []*models.Behavior) string {
	var parts []string
	seen := make(map[string]bool)

	for _, b := range behaviors {
		content := strings.TrimSpace(b.Content.Expanded)
		if content != "" && !seen[content] {
			parts = append(parts, content)
			seen[content] = true
		}
	}

	return strings.Join(parts, "\n\n")
}

// createMergeProvenance creates provenance tracking for a merged behavior.
func createMergeProvenance(behaviors []*models.Behavior) models.Provenance {
	return models.Provenance{
		SourceType: models.SourceTypeLearned,
		CreatedAt:  time.Now(),
		Author:     "merge",
	}
}

// averageConfidence calculates the average confidence across behaviors.
func averageConfidence(behaviors []*models.Behavior) float64 {
	if len(behaviors) == 0 {
		return 0.0
	}

	var sum float64
	for _, b := range behaviors {
		sum += b.Confidence
	}
	return sum / float64(len(behaviors))
}

// maxPriority returns the highest priority from all behaviors.
func maxPriority(behaviors []*models.Behavior) int {
	max := 0
	for _, b := range behaviors {
		if b.Priority > max {
			max = b.Priority
		}
	}
	return max
}
