package learning

import (
	"github.com/nvandessel/feedback-loop/internal/models"
)

// BehaviorExtractor transforms corrections into candidate behaviors.
// It analyzes the correction content to infer activation conditions,
// behavior kind, and content structure.
type BehaviorExtractor interface {
	// Extract creates a candidate behavior from a correction.
	// The extracted behavior includes:
	// - Inferred 'when' conditions based on correction context
	// - Behavior kind (directive, constraint, preference, procedure)
	// - Structured content with avoid/prefer patterns
	// - Provenance linking back to the source correction
	Extract(correction models.Correction) (*models.Behavior, error)
}

// NewBehaviorExtractor creates a new BehaviorExtractor instance.
func NewBehaviorExtractor() BehaviorExtractor {
	// Stub - implementation will be provided by subagent
	panic("not implemented")
}
