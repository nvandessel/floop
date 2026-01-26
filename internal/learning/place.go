package learning

import (
	"context"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// PlacementDecision describes where a new behavior should go in the graph.
type PlacementDecision struct {
	// Action indicates what to do: "create", "merge", or "specialize"
	Action string

	// TargetID is set for merge/specialize actions to indicate the existing behavior
	TargetID string

	// ProposedEdges are the edges to add when placing the behavior
	ProposedEdges []ProposedEdge

	// SimilarBehaviors lists existing behaviors that are similar
	SimilarBehaviors []SimilarityMatch

	// Confidence indicates how confident the placer is in this decision (0.0-1.0)
	Confidence float64
}

// ProposedEdge represents a proposed edge to add to the graph.
type ProposedEdge struct {
	From string
	To   string
	Kind string // "requires", "overrides", "conflicts", "similar-to"
}

// SimilarityMatch represents a similar existing behavior.
type SimilarityMatch struct {
	ID    string
	Score float64
}

// GraphPlacer determines where a new behavior fits in the graph.
// It analyzes existing behaviors to find relationships and detect
// potential duplicates or merge opportunities.
type GraphPlacer interface {
	// Place determines where a behavior should be placed in the graph.
	// It returns a PlacementDecision indicating whether to create a new
	// behavior, merge with an existing one, or specialize an existing one.
	Place(ctx context.Context, behavior *models.Behavior) (*PlacementDecision, error)
}

// NewGraphPlacer creates a new GraphPlacer with the given store.
func NewGraphPlacer(s store.GraphStore) GraphPlacer {
	// Stub - implementation will be provided by subagent
	panic("not implemented")
}
