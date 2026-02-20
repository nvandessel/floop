// Package dedup provides deduplication functionality for behaviors in the graph store.
// It detects duplicate or highly similar behaviors and optionally merges them.
package dedup

import (
	"context"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// DuplicateMatch represents a potential duplicate of a behavior.
type DuplicateMatch struct {
	// Behavior is the matching behavior found in the store.
	Behavior *models.Behavior `json:"behavior" yaml:"behavior"`

	// Similarity is the similarity score between 0.0 and 1.0.
	// Higher values indicate more similar behaviors.
	Similarity float64 `json:"similarity" yaml:"similarity"`

	// SimilarityMethod describes how similarity was computed.
	// Valid values: "jaccard", "llm", "embedding", "hybrid"
	SimilarityMethod string `json:"similarity_method" yaml:"similarity_method"`

	// MergeRecommended indicates whether merging is recommended based on
	// the similarity score and other heuristics.
	MergeRecommended bool `json:"merge_recommended" yaml:"merge_recommended"`
}

// DeduplicationReport contains the results of a batch deduplication operation.
type DeduplicationReport struct {
	// TotalBehaviors is the total number of behaviors analyzed.
	TotalBehaviors int `json:"total_behaviors" yaml:"total_behaviors"`

	// DuplicatesFound is the number of duplicate pairs found.
	DuplicatesFound int `json:"duplicates_found" yaml:"duplicates_found"`

	// MergesPerformed is the number of merge operations performed.
	MergesPerformed int `json:"merges_performed" yaml:"merges_performed"`

	// MergedBehaviors contains the resulting merged behaviors.
	MergedBehaviors []*models.Behavior `json:"merged_behaviors,omitempty" yaml:"merged_behaviors,omitempty"`

	// Errors contains any errors encountered during processing.
	Errors []string `json:"errors,omitempty" yaml:"errors,omitempty"`
}

// DeduplicatorConfig configures the deduplication behavior.
type DeduplicatorConfig struct {
	// SimilarityThreshold is the minimum similarity score for duplicate detection.
	// Range: 0.0 to 1.0, default: 0.9
	SimilarityThreshold float64 `json:"similarity_threshold,omitempty" yaml:"similarity_threshold,omitempty"`

	// EmbeddingThreshold is the cosine similarity threshold for embedding-based
	// duplicate detection. When embeddings are available, this threshold is used
	// instead of SimilarityThreshold for the embedding comparison path.
	// Range: 0.0 to 1.0, default: 0.7
	EmbeddingThreshold float64 `json:"embedding_threshold,omitempty" yaml:"embedding_threshold,omitempty"`

	// AutoMerge enables automatic merging of detected duplicates.
	// When false, duplicates are only reported, not merged.
	AutoMerge bool `json:"auto_merge,omitempty" yaml:"auto_merge,omitempty"`

	// UseLLM enables LLM-based semantic comparison for more accurate similarity detection.
	// When false, only Jaccard word overlap is used.
	UseLLM bool `json:"use_llm,omitempty" yaml:"use_llm,omitempty"`

	// MaxBatchSize limits the number of behaviors to process at once.
	// Use 0 for no limit.
	MaxBatchSize int `json:"max_batch_size,omitempty" yaml:"max_batch_size,omitempty"`
}

// DefaultConfig returns a DeduplicatorConfig with sensible defaults.
func DefaultConfig() DeduplicatorConfig {
	return DeduplicatorConfig{
		SimilarityThreshold: constants.DefaultAutoMergeThreshold,
		EmbeddingThreshold:  constants.DefaultEmbeddingDedupThreshold,
		AutoMerge:           false,
		UseLLM:              false,
		MaxBatchSize:        100,
	}
}

// Deduplicator provides methods for detecting and merging duplicate behaviors.
type Deduplicator interface {
	// FindDuplicates finds potential duplicates of a behavior in the store.
	// Returns a list of matches sorted by similarity score (highest first).
	FindDuplicates(ctx context.Context, behavior *models.Behavior) ([]DuplicateMatch, error)

	// MergeDuplicates merges the specified duplicate matches into the primary behavior.
	// Returns the merged behavior with combined content and updated metadata.
	// The original duplicate behaviors are removed from the store.
	MergeDuplicates(ctx context.Context, matches []DuplicateMatch, primary *models.Behavior) (*models.Behavior, error)

	// DeduplicateStore performs deduplication on the entire store.
	// Analyzes all behaviors, finds duplicates, and optionally merges them
	// based on the configuration provided at construction time.
	DeduplicateStore(ctx context.Context, store store.GraphStore) (*DeduplicationReport, error)
}
