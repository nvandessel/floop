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
	DryRun bool
}

// RunResult holds the output of a consolidation run.
type RunResult struct {
	RunID          string // unique run identifier shared between decision logs and DB
	Candidates     []Candidate
	Classified     []ClassifiedMemory
	Edges          []store.Edge
	Merges         []MergeProposal
	Skips          []int // memory indices the LLM marked as already captured
	Promoted       int
	SourceEventIDs []string // event IDs that were processed (callers should mark consolidated)
	Duration       time.Duration
}

// Runner orchestrates the consolidation pipeline.
type Runner struct {
	consolidator Consolidator
	model        string // model identifier for run persistence
}

// NewRunner creates a new consolidation runner.
func NewRunner(c Consolidator) *Runner {
	return &Runner{consolidator: c}
}

// NewRunnerWithModel creates a new consolidation runner with a model identifier
// for persisting run records to the consolidation_runs table.
func NewRunnerWithModel(c Consolidator, model string) *Runner {
	return &Runner{consolidator: c, model: model}
}

// Run executes the full consolidation pipeline: Extract, Classify, Relate, Promote.
// If DryRun is true or the store is nil, it stops after Classify.
func (r *Runner) Run(ctx context.Context, evts []events.Event, s store.GraphStore, opts RunOptions) (*RunResult, error) {
	start := time.Now()
	runID := fmt.Sprintf("run-%d", start.UnixNano())
	result := &RunResult{RunID: runID}

	// Stage 1: Extract
	candidates, err := r.consolidator.Extract(ctx, evts)
	if err != nil {
		return nil, fmt.Errorf("extract stage: %w", err)
	}
	result.Candidates = candidates

	if len(candidates) == 0 {
		// No candidates extracted, but all events were scanned — mark them
		// as consolidated so they aren't re-processed on the next run.
		result.SourceEventIDs = collectEventIDs(evts)
		result.Duration = time.Since(start)
		return result, nil
	}

	if ctx.Err() != nil {
		return result, ctx.Err()
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

	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	// Stage 3: Relate
	edges, merges, skips, err := r.consolidator.Relate(ctx, classified, s)
	if err != nil {
		return nil, fmt.Errorf("relate stage: %w", err)
	}
	result.Edges = edges
	result.Merges = merges
	result.Skips = skips

	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	// Stage 4: Promote
	promoteResult, err := r.consolidator.Promote(ctx, runID, classified, edges, merges, skips, s)
	if err != nil {
		return nil, fmt.Errorf("promote stage: %w", err)
	}
	result.Promoted = promoteResult.Promoted

	// All input events were scanned — mark them consolidated.
	result.SourceEventIDs = collectEventIDs(evts)

	result.Duration = time.Since(start)

	// Persist run record with all stage counts (best-effort).
	{
		model := r.model
		if model == "" {
			model = "unknown"
		}
		var projectID, sessionID string
		for _, mem := range classified {
			if projectID == "" {
				if pid, ok := mem.SessionContext["project_id"].(string); ok {
					projectID = pid
				}
			}
			if sessionID == "" {
				if sid, ok := mem.SessionContext["session_id"].(string); ok {
					sessionID = sid
				}
			}
			if projectID != "" && sessionID != "" {
				break
			}
		}
		persistRun(ctx, s, model, ConsolidationRunRecord{
			CandidatesFound: len(candidates),
			Classified:      len(classified),
			Promoted:        promoteResult.Promoted,
			DurationMS:      result.Duration.Milliseconds(),
			ProjectID:       projectID,
			SessionID:       sessionID,
		}, result.RunID, promoteResult.MergesExecuted)
	}

	return result, nil
}

// collectEventIDs extracts IDs from a slice of events.
func collectEventIDs(evts []events.Event) []string {
	ids := make([]string, len(evts))
	for i, evt := range evts {
		ids[i] = evt.ID
	}
	return ids
}
