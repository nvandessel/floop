package learning

import (
	"context"

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

	// AutoAccepted indicates whether the behavior was automatically accepted
	AutoAccepted bool

	// RequiresReview indicates whether human review is needed
	RequiresReview bool

	// ReviewReasons explains why review is required
	ReviewReasons []string
}

// LearningLoop orchestrates the correction -> behavior pipeline.
// It coordinates CorrectionCapture, BehaviorExtractor, and GraphPlacer
// to process corrections and produce learned behaviors.
type LearningLoop interface {
	// ProcessCorrection processes a single correction into a candidate behavior.
	// It extracts a behavior, determines graph placement, and optionally
	// auto-accepts the behavior if confidence is high enough.
	ProcessCorrection(ctx context.Context, correction models.Correction) (*LearningResult, error)

	// ApprovePending approves a pending behavior, updating its provenance.
	ApprovePending(ctx context.Context, behaviorID, approver string) error

	// RejectPending rejects a pending behavior with a reason.
	RejectPending(ctx context.Context, behaviorID, rejector, reason string) error
}

// LearningLoopConfig holds configuration for the learning loop.
type LearningLoopConfig struct {
	// AutoAcceptThreshold is the minimum confidence for auto-accepting behaviors.
	// Behaviors with confidence >= this threshold and no review flags are auto-accepted.
	// Default: 0.8
	AutoAcceptThreshold float64
}

// DefaultLearningLoopConfig returns sensible defaults for the learning loop.
func DefaultLearningLoopConfig() LearningLoopConfig {
	return LearningLoopConfig{
		AutoAcceptThreshold: 0.8,
	}
}

// NewLearningLoop creates a new learning loop with the given store and config.
// If config is nil, default configuration is used.
func NewLearningLoop(s store.GraphStore, config *LearningLoopConfig) LearningLoop {
	cfg := DefaultLearningLoopConfig()
	if config != nil {
		cfg = *config
	}
	return &learningLoop{
		store:               s,
		capturer:            NewCorrectionCapture(),
		extractor:           NewBehaviorExtractor(),
		placer:              NewGraphPlacer(s),
		autoAcceptThreshold: cfg.AutoAcceptThreshold,
	}
}

// learningLoop is the concrete implementation of LearningLoop.
type learningLoop struct {
	store               store.GraphStore
	capturer            CorrectionCapture
	extractor           BehaviorExtractor
	placer              GraphPlacer
	autoAcceptThreshold float64
}

// ProcessCorrection implements LearningLoop.
func (l *learningLoop) ProcessCorrection(ctx context.Context, correction models.Correction) (*LearningResult, error) {
	// Implementation will be added after component implementations are complete
	panic("not implemented - waiting for component implementations")
}

// ApprovePending implements LearningLoop.
func (l *learningLoop) ApprovePending(ctx context.Context, behaviorID, approver string) error {
	// Implementation will be added after component implementations are complete
	panic("not implemented - waiting for component implementations")
}

// RejectPending implements LearningLoop.
func (l *learningLoop) RejectPending(ctx context.Context, behaviorID, rejector, reason string) error {
	// Implementation will be added after component implementations are complete
	panic("not implemented - waiting for component implementations")
}
