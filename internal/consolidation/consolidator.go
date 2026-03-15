// Package consolidation implements the memory consolidation pipeline,
// extracting behavioral memories from raw conversation events.
package consolidation

import (
	"context"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// Candidate is a memory candidate extracted from raw events.
type Candidate struct {
	SourceEvents   []string       // event IDs
	RawText        string         // relevant excerpt
	CandidateType  string         // correction, discovery, decision, failure, workflow, context
	Confidence     float64        // 0.0-1.0
	SessionContext map[string]any // project, file, task, branch, model
}

// ClassifiedMemory is a typed, classified memory ready for graph insertion.
type ClassifiedMemory struct {
	Candidate
	Kind         models.BehaviorKind
	MemoryType   string // semantic, episodic, procedural
	Scope        string // "universal" or "project:namespace/name"
	Importance   float64
	Content      models.BehaviorContent
	EpisodeData  *models.EpisodeData
	WorkflowData *models.WorkflowData
}

// MergeProposal proposes merging a new memory into an existing behavior.
type MergeProposal struct {
	Memory     ClassifiedMemory
	TargetID   string  // existing behavior ID
	Similarity float64 // cosine similarity
	Strategy   string  // "absorb", "supersede", "supplement"
}

// Consolidator defines the four-stage consolidation pipeline.
type Consolidator interface {
	// Extract scans raw events for behavioral signals and returns candidates.
	Extract(ctx context.Context, events []events.Event) ([]Candidate, error)

	// Classify assigns behavior kinds and memory types to candidates.
	Classify(ctx context.Context, candidates []Candidate) ([]ClassifiedMemory, error)

	// Relate finds relationships between new memories and existing behaviors.
	Relate(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore) ([]store.Edge, []MergeProposal, error)

	// Promote writes classified memories and edges into the graph store.
	Promote(ctx context.Context, memories []ClassifiedMemory, edges []store.Edge, merges []MergeProposal, s store.GraphStore) error
}
