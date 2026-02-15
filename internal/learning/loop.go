package learning

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/dedup"
	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/logging"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// LearningResult represents the result of processing a correction.
type LearningResult struct {
	// Correction is the original correction that was processed
	Correction models.Correction

	// CandidateBehavior is the behavior extracted from the correction
	CandidateBehavior models.Behavior

	// Placement describes where the behavior was/should be placed
	Placement PlacementDecision

	// Scope indicates where the behavior was stored (local or global)
	Scope constants.Scope

	// AutoAccepted indicates whether the behavior was automatically accepted
	AutoAccepted bool

	// RequiresReview indicates whether human review is needed
	RequiresReview bool

	// ReviewReasons explains why review is required
	ReviewReasons []string

	// MergedIntoExisting indicates whether the behavior was merged into an existing one
	MergedIntoExisting bool

	// MergedBehaviorID is the ID of the merged behavior (if merge occurred)
	MergedBehaviorID string

	// MergeSimilarity is the similarity score with the merged behavior
	MergeSimilarity float64
}

// LearningLoop orchestrates the correction -> behavior pipeline.
// It coordinates CorrectionCapture, BehaviorExtractor, and GraphPlacer
// to process corrections and produce learned behaviors.
type LearningLoop interface {
	// ProcessCorrection processes a single correction into a candidate behavior.
	// It extracts a behavior, determines graph placement, and optionally
	// auto-accepts the behavior if confidence is high enough.
	ProcessCorrection(ctx context.Context, correction models.Correction) (*LearningResult, error)
}

// LearningLoopConfig holds configuration for the learning loop.
type LearningLoopConfig struct {
	// AutoAcceptThreshold is the minimum confidence for auto-accepting behaviors.
	// Behaviors with confidence >= this threshold and no review flags are auto-accepted.
	// Default: 0.8
	AutoAcceptThreshold float64

	// AutoMerge enables automatic merging of duplicate behaviors.
	// When enabled, new behaviors that are highly similar to existing ones
	// are automatically merged instead of creating duplicates.
	AutoMerge bool

	// AutoMergeThreshold is the minimum similarity score for auto-merging.
	// Behaviors with similarity >= this threshold are merged.
	// Default: 0.9
	AutoMergeThreshold float64

	// LLMClient is the optional LLM client for semantic comparison and merging.
	LLMClient llm.Client

	// Deduplicator is the optional deduplicator for finding duplicates.
	// If nil, auto-merge is disabled regardless of AutoMerge setting.
	Deduplicator dedup.Deduplicator

	// Logger is the optional structured logger for operational output.
	Logger *slog.Logger

	// DecisionLogger is the optional decision event logger.
	DecisionLogger *logging.DecisionLogger
}

// DefaultLearningLoopConfig returns sensible defaults for the learning loop.
func DefaultLearningLoopConfig() LearningLoopConfig {
	return LearningLoopConfig{
		AutoAcceptThreshold: 0.8,
		AutoMerge:           false,
		AutoMergeThreshold:  0.9,
	}
}

// NewLearningLoop creates a new learning loop with the given store and config.
// If config is nil, default configuration is used.
func NewLearningLoop(s store.GraphStore, config *LearningLoopConfig) LearningLoop {
	cfg := DefaultLearningLoopConfig()
	if config != nil {
		cfg = *config
	}

	// Create placer with optional LLM support
	var placer GraphPlacer
	if cfg.LLMClient != nil {
		placer = NewGraphPlacerWithConfig(s, &GraphPlacerConfig{
			LLMClient:              cfg.LLMClient,
			UseLLMForSimilarity:    true,
			LLMSimilarityThreshold: 0.5,
		})
	} else {
		placer = NewGraphPlacer(s)
	}

	return &learningLoop{
		store:               s,
		capturer:            NewCorrectionCapture(),
		extractor:           NewBehaviorExtractor(),
		placer:              placer,
		autoAcceptThreshold: cfg.AutoAcceptThreshold,
		autoMerge:           cfg.AutoMerge,
		autoMergeThreshold:  cfg.AutoMergeThreshold,
		deduplicator:        cfg.Deduplicator,
		logger:              cfg.Logger,
		decisions:           cfg.DecisionLogger,
	}
}

// learningLoop is the concrete implementation of LearningLoop.
type learningLoop struct {
	store               store.GraphStore
	capturer            CorrectionCapture
	extractor           BehaviorExtractor
	placer              GraphPlacer
	autoAcceptThreshold float64
	autoMerge           bool
	autoMergeThreshold  float64
	deduplicator        dedup.Deduplicator
	logger              *slog.Logger
	decisions           *logging.DecisionLogger
}

// ProcessCorrection implements LearningLoop.
func (l *learningLoop) ProcessCorrection(ctx context.Context, correction models.Correction) (*LearningResult, error) {
	// Step 1: Extract candidate behavior
	candidate, err := l.extractor.Extract(correction)
	if err != nil {
		return nil, fmt.Errorf("extraction failed: %w", err)
	}

	if l.logger != nil {
		l.logger.Debug("behavior extracted", "behavior_id", candidate.ID, "kind", candidate.Kind, "correction_id", correction.ID)
	}

	// Step 2: Check for duplicates and auto-merge if enabled
	if l.autoMerge && l.deduplicator != nil {
		mergeResult, err := l.tryAutoMerge(ctx, candidate)
		if err == nil && mergeResult != nil {
			return mergeResult, nil
		}
		// Continue with normal flow if auto-merge didn't happen
	}

	// Step 3: Determine graph placement
	placement, err := l.placer.Place(ctx, candidate)
	if err != nil {
		return nil, fmt.Errorf("placement failed: %w", err)
	}

	if l.logger != nil {
		l.logger.Debug("placement decided", "behavior_id", candidate.ID, "action", placement.Action, "confidence", placement.Confidence)
	}

	// Step 4: Decide if auto-accept or needs review
	requiresReview, reasons := l.needsReview(candidate, placement)
	autoAccepted := !requiresReview && placement.Confidence >= l.autoAcceptThreshold

	// Step 5: Commit to graph
	scope, err := l.commitBehavior(ctx, candidate, placement)
	if err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	return &LearningResult{
		Correction:        correction,
		CandidateBehavior: *candidate,
		Placement:         *placement,
		Scope:             scope,
		AutoAccepted:      autoAccepted,
		RequiresReview:    requiresReview,
		ReviewReasons:     reasons,
	}, nil
}

// tryAutoMerge attempts to merge the candidate with existing duplicates.
// Returns a LearningResult if merge occurred, nil otherwise.
func (l *learningLoop) tryAutoMerge(ctx context.Context, candidate *models.Behavior) (*LearningResult, error) {
	// Find duplicates
	duplicates, err := l.deduplicator.FindDuplicates(ctx, candidate)
	if err != nil {
		return nil, err
	}

	// Check if any duplicate exceeds the merge threshold
	var bestMatch *dedup.DuplicateMatch
	for i := range duplicates {
		if duplicates[i].Similarity >= l.autoMergeThreshold {
			if bestMatch == nil || duplicates[i].Similarity > bestMatch.Similarity {
				bestMatch = &duplicates[i]
			}
		}
	}

	if bestMatch == nil {
		if l.logger != nil {
			l.logger.Debug("no merge candidate found", "behavior_id", candidate.ID, "duplicates_found", len(duplicates))
		}
		if l.decisions != nil {
			l.decisions.Log(map[string]any{
				"event":            "auto_merge_skipped",
				"behavior_id":      candidate.ID,
				"duplicates_found": len(duplicates),
				"threshold":        l.autoMergeThreshold,
				"reason":           "no duplicate above threshold",
			})
		}
		return nil, nil // No suitable duplicate found
	}

	if l.logger != nil {
		l.logger.Debug("auto-merge triggered", "behavior_id", candidate.ID, "merge_target", bestMatch.Behavior.ID, "similarity", bestMatch.Similarity)
	}
	if l.decisions != nil {
		l.decisions.Log(map[string]any{
			"event":        "auto_merge_triggered",
			"behavior_id":  candidate.ID,
			"merge_target": bestMatch.Behavior.ID,
			"similarity":   bestMatch.Similarity,
			"threshold":    l.autoMergeThreshold,
		})
	}

	// Perform the merge
	merged, err := l.deduplicator.MergeDuplicates(ctx, []dedup.DuplicateMatch{*bestMatch}, candidate)
	if err != nil {
		return nil, fmt.Errorf("merge failed: %w", err)
	}

	// Create placement for the merged behavior
	placement := &PlacementDecision{
		Action:     "merge",
		TargetID:   bestMatch.Behavior.ID,
		Confidence: bestMatch.Similarity,
	}

	return &LearningResult{
		Correction:         models.Correction{}, // Original correction not needed for merge
		CandidateBehavior:  *merged,
		Placement:          *placement,
		Scope:              ClassifyScope(merged),
		AutoAccepted:       true,
		RequiresReview:     false,
		MergedIntoExisting: true,
		MergedBehaviorID:   bestMatch.Behavior.ID,
		MergeSimilarity:    bestMatch.Similarity,
	}, nil
}

// needsReview determines if human review is required.
func (l *learningLoop) needsReview(candidate *models.Behavior, placement *PlacementDecision) (bool, []string) {
	var reasons []string

	// Constraints always need review
	if candidate.Kind == models.BehaviorKindConstraint {
		reasons = append(reasons, "Constraints require human review")
	}

	// Merging into existing behavior needs review
	if placement.Action == "merge" {
		reasons = append(reasons, fmt.Sprintf("Would merge into existing behavior: %s", placement.TargetID))
	}

	// Conflicts need review
	if len(candidate.Conflicts) > 0 {
		reasons = append(reasons, fmt.Sprintf("Conflicts with: %v", candidate.Conflicts))
	}

	// Low confidence placements need review
	if placement.Confidence < constants.LowConfidenceThreshold {
		reasons = append(reasons, fmt.Sprintf("Low placement confidence: %.2f", placement.Confidence))
	}

	// High similarity to existing might be duplicate
	for _, sim := range placement.SimilarBehaviors {
		if sim.Score > 0.85 {
			reasons = append(reasons, fmt.Sprintf("Very similar to existing: %s (%.2f)", sim.ID, sim.Score))
		}
	}

	needsRev := len(reasons) > 0

	if needsRev {
		if l.logger != nil {
			l.logger.Debug("review required", "behavior_id", candidate.ID, "reasons", reasons)
		}
		if l.decisions != nil {
			l.decisions.Log(map[string]any{
				"event":       "review_required",
				"behavior_id": candidate.ID,
				"reasons":     reasons,
				"confidence":  placement.Confidence,
			})
		}
	} else {
		accepted := placement.Confidence >= l.autoAcceptThreshold
		if l.logger != nil {
			l.logger.Debug("auto-accept check", "behavior_id", candidate.ID, "confidence", placement.Confidence, "threshold", l.autoAcceptThreshold, "accepted", accepted)
		}
		if l.decisions != nil {
			l.decisions.Log(map[string]any{
				"event":       "auto_accept",
				"behavior_id": candidate.ID,
				"confidence":  placement.Confidence,
				"threshold":   l.autoAcceptThreshold,
				"accepted":    accepted,
			})
		}
	}

	return needsRev, reasons
}

// ScopedNodeAdder is implemented by stores that support writing to a specific scope.
// MultiGraphStore implements this; InMemoryGraphStore (used in tests) does not.
type ScopedNodeAdder interface {
	AddNodeToScope(ctx context.Context, node store.Node, scope constants.Scope) (string, error)
}

// commitBehavior saves the behavior to the graph.
// Returns the scope the behavior was written to.
func (l *learningLoop) commitBehavior(ctx context.Context, behavior *models.Behavior, placement *PlacementDecision) (constants.Scope, error) {
	// Convert behavior to node
	node := store.Node{
		ID:   behavior.ID,
		Kind: "behavior",
		Content: map[string]interface{}{
			"name":       behavior.Name,
			"kind":       string(behavior.Kind),
			"when":       behavior.When,
			"content":    behavior.Content,
			"provenance": behavior.Provenance,
			"requires":   behavior.Requires,
			"overrides":  behavior.Overrides,
			"conflicts":  behavior.Conflicts,
		},
		Metadata: map[string]interface{}{
			"confidence": behavior.Confidence,
			"priority":   behavior.Priority,
			"stats":      behavior.Stats,
		},
	}

	// Classify scope based on behavior's When conditions
	scope := ClassifyScope(behavior)

	// Use scoped write if the store supports it; fall back to AddNode for plain stores (tests)
	if scoped, ok := l.store.(ScopedNodeAdder); ok {
		if _, err := scoped.AddNodeToScope(ctx, node, scope); err != nil {
			return scope, err
		}
	} else {
		if _, err := l.store.AddNode(ctx, node); err != nil {
			return scope, err
		}
	}

	// Add edges
	for _, e := range placement.ProposedEdges {
		edge := store.Edge{
			Source:    e.From,
			Target:    e.To,
			Kind:      e.Kind,
			Weight:    1.0,
			CreatedAt: time.Now(),
		}
		if err := l.store.AddEdge(ctx, edge); err != nil {
			return scope, err
		}
	}

	return scope, l.store.Sync(ctx)
}
