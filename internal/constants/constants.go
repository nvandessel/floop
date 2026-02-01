// Package constants provides named constants used throughout the feedback-loop codebase.
// This centralizes magic numbers for better maintainability and documentation.
package constants

// Behavior extraction constants
const (
	// MaxCorrectionPreviewLen is the maximum length for correction text in previews/IDs.
	// Used when generating behavior names from correction text.
	MaxCorrectionPreviewLen = 100

	// MaxBehaviorNameLen is the maximum length for a behavior name.
	// Longer names are truncated to this length.
	MaxBehaviorNameLen = 50
)

// Confidence and threshold constants
const (
	// DefaultLearnedConfidence is the starting confidence for newly learned behaviors.
	// Learned behaviors start with lower confidence than manually defined ones.
	DefaultLearnedConfidence = 0.6

	// DefaultSimilarityThreshold is the threshold for considering two behaviors duplicates.
	// Values above this threshold indicate high semantic similarity.
	DefaultSimilarityThreshold = 0.95

	// LowConfidenceThreshold is the threshold below which behaviors require review.
	// Behaviors with confidence below this need human verification.
	LowConfidenceThreshold = 0.6
)

// Similarity weight constants for behavior comparison
const (
	// WhenOverlapWeight is the weight given to 'when' condition overlap in similarity.
	WhenOverlapWeight = 0.4

	// ContentSimilarityWeight is the weight given to content similarity.
	ContentSimilarityWeight = 0.6
)

// Token budget allocation constants
const (
	// FullTierBudgetPercent is the percentage of token budget for full behavior content.
	FullTierBudgetPercent = 0.60

	// SummaryTierBudgetPercent is the percentage of token budget for summarized behaviors.
	SummaryTierBudgetPercent = 0.30
)
