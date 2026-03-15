package consolidation

import (
	"context"
	"fmt"
	"time"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/store"
)

// RunOptions configures a consolidation run.
type RunOptions struct {
	DryRun    bool
	ProjectID string
}

// RunResult holds the output of a consolidation run.
type RunResult struct {
	Candidates []Candidate
	Classified []ClassifiedMemory
	Edges      []store.Edge
	Merges     []MergeProposal
	Promoted   int
	Duration   time.Duration
}

// Runner orchestrates the consolidation pipeline.
type Runner struct {
	consolidator Consolidator
}

// NewRunner creates a new consolidation runner.
func NewRunner(c Consolidator) *Runner {
	return &Runner{consolidator: c}
}

// Run executes the full consolidation pipeline: Extract, Classify, Relate, Promote.
// If DryRun is true or the store is nil, it stops after Classify.
func (r *Runner) Run(ctx context.Context, evts []events.Event, s store.GraphStore, opts RunOptions) (*RunResult, error) {
	start := time.Now()
	result := &RunResult{}

	// Stage 1: Extract
	candidates, err := r.consolidator.Extract(ctx, evts)
	if err != nil {
		return nil, fmt.Errorf("extract stage: %w", err)
	}
	result.Candidates = candidates

	if len(candidates) == 0 {
		result.Duration = time.Since(start)
		return result, nil
	}

	// Stage 2: Classify
	classified, err := r.consolidator.Classify(ctx, candidates)
	if err != nil {
		return nil, fmt.Errorf("classify stage: %w", err)
	}
	result.Classified = classified

	// If dry-run or no store, stop here
	if opts.DryRun || s == nil {
		result.Duration = time.Since(start)
		return result, nil
	}

	// Stage 3: Relate
	edges, merges, err := r.consolidator.Relate(ctx, classified, s)
	if err != nil {
		return nil, fmt.Errorf("relate stage: %w", err)
	}
	result.Edges = edges
	result.Merges = merges

	// Stage 4: Promote
	err = r.consolidator.Promote(ctx, classified, edges, merges, s)
	if err != nil {
		return nil, fmt.Errorf("promote stage: %w", err)
	}
	result.Promoted = len(classified) - len(merges)

	result.Duration = time.Since(start)
	return result, nil
}
