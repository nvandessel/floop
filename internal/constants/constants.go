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

// Tag similarity and graph placement constants.
// Weights are proportional, not required to sum to 1.0 — WeightedScoreWithTags
// normalizes by dividing each by the total of present signals.
const (
	// TagSimilarityWeight is the weight for tag Jaccard similarity in scoring.
	TagSimilarityWeight = 0.2

	// SimilarToThreshold is the minimum score for creating similar-to edges.
	SimilarToThreshold = 0.5

	// SpecializeThreshold is the minimum score for considering specialization edges.
	SpecializeThreshold = 0.7

	// SimilarToUpperBound is the upper bound; above this, behaviors are potential duplicates.
	SimilarToUpperBound = 0.9
)

// Activation tier thresholds determine which injection tier a behavior receives
// based on its spreading activation level.
const (
	// FullTierActivationThreshold is the minimum activation for full content injection.
	FullTierActivationThreshold = 0.7

	// SummaryTierActivationThreshold is the minimum activation for summary injection.
	// Behaviors below this threshold receive name-only injection.
	SummaryTierActivationThreshold = 0.4
)

// Token cost estimates per injection tier, used for budget enforcement.
const (
	// FullTierTokenCost is the estimated token cost for full behavior injection.
	FullTierTokenCost = 80

	// SummaryTierTokenCost is the estimated token cost for summary injection.
	SummaryTierTokenCost = 30

	// NameOnlyTierTokenCost is the estimated token cost for name-only injection.
	NameOnlyTierTokenCost = 10
)

// Deduplication thresholds control automatic merging of similar behaviors.
const (
	// DefaultAutoMergeThreshold is the similarity threshold for automatic dedup merging.
	// Behavior pairs with similarity >= this value are considered duplicates.
	DefaultAutoMergeThreshold = 0.9

	// DefaultAutoAcceptThreshold is the minimum confidence for auto-accepting learned behaviors.
	// Behaviors with confidence >= this value and no review flags are auto-accepted.
	DefaultAutoAcceptThreshold = 0.8
)

// Spreading activation sigmoid parameters control the squashing function
// that maps raw activation into a sharp [0, 1] range.
const (
	// SigmoidGain controls the steepness of the sigmoid curve.
	SigmoidGain = 10.0

	// SigmoidCenter is the inflection point of the sigmoid.
	// Values below this are suppressed toward 0; values above are amplified toward 1.
	SigmoidCenter = 0.3
)

// Backup rotation controls how many backup files are retained.
const (
	// MaxBackupRotation is the default maximum number of backup files to keep.
	MaxBackupRotation = 10
)

// Scoring constants used in the relevance scorer.
const (
	// NeutralScore is the default score returned when insufficient data is available.
	NeutralScore = 0.5

	// MaxContextSpecificityBonus is the maximum bonus awarded for context specificity.
	MaxContextSpecificityBonus = 0.3

	// ContextSpecificityFactor is the per-condition bonus for context specificity.
	ContextSpecificityFactor = 0.1
)

// Behavior status kind strings represent lifecycle states set by curation commands.
// Defined as plain strings here (not models.BehaviorKind) to avoid import cycles
// between the store and models packages.
const (
	BehaviorKindForgotten  = "forgotten-behavior"  // Marked as forgotten via floop forget
	BehaviorKindDeprecated = "deprecated-behavior" // Marked as deprecated via floop deprecate
	BehaviorKindMerged     = "merged-behavior"     // Result of merging duplicate behaviors
)

// Edge kind constants
const (
	// CoActivatedEdgeKind is the edge kind used for Hebbian co-activation edges.
	// This is a system-created edge kind, not user-facing.
	CoActivatedEdgeKind = "co-activated"
)

// ValidUserEdgeKinds defines the allowed edge kinds for user-facing commands.
// System edge kinds like CoActivatedEdgeKind are not included.
var ValidUserEdgeKinds = map[string]bool{
	"requires":     true,
	"overrides":    true,
	"conflicts":    true,
	"similar-to":   true,
	"learned-from": true,
}
