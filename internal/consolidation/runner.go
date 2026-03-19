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

// ModelProvider is an optional interface that Consolidator implementations
// can satisfy to report which LLM model they use. Runner uses this for
// run persistence so the DB model column matches actual LLM usage.
type ModelProvider interface {
	Model() string
}

// RunIDSetter is an optional interface that Consolidator implementations
// can satisfy to receive the run ID before pipeline execution. This allows
// all decision log entries across stages to share the same run_id.
type RunIDSetter interface {
	SetRunID(string)
}

// Runner orchestrates the consolidation pipeline.
// A Runner (and its Consolidator) must not be shared across concurrent Run calls
// because Run sets mutable state on the consolidator via SetRunID. Each goroutine
// should create its own Runner with its own Consolidator instance.
type Runner struct {
	consolidator Consolidator
}

// NewRunner creates a new consolidation runner.
// The returned Runner must not be used concurrently; see Runner doc.
func NewRunner(c Consolidator) *Runner {
	return &Runner{consolidator: c}
}

// model returns the LLM model identifier from the consolidator, or "unknown".
func (r *Runner) model() string {
	if mp, ok := r.consolidator.(ModelProvider); ok {
		if m := mp.Model(); m != "" {
			return m
		}
	}
	return "unknown"
}

// Run executes the full consolidation pipeline: Extract, Classify, Relate, Promote.
// If DryRun is true or the store is nil, it stops after Classify.
func (r *Runner) Run(ctx context.Context, evts []events.Event, s store.GraphStore, opts RunOptions) (*RunResult, error) {
	start := time.Now()
	runID := fmt.Sprintf("run-%d", start.UnixNano())
	result := &RunResult{RunID: runID}

	// Set runID on the consolidator if it supports it, so all decision log
	// entries across Extract/Classify/Relate/Promote share the same run_id.
	if rs, ok := r.consolidator.(RunIDSetter); ok {
		rs.SetRunID(runID)
	}

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
	promoteResult, err := r.consolidator.Promote(ctx, classified, edges, merges, skips, s)
	if err != nil {
		return nil, fmt.Errorf("promote stage: %w", err)
	}
	result.Promoted = promoteResult.Promoted

	// All input events were scanned — mark them consolidated.
	result.SourceEventIDs = collectEventIDs(evts)

	result.Duration = time.Since(start)

	// Persist run record with all stage counts (best-effort).
	// NOTE: TokensUsed is always 0 because the SubagentClient (llm.Client)
	// does not currently report token counts. When token reporting is added
	// to llm.Client (e.g. via a CompletionResult return type), accumulate
	// tokens across all LLM calls in Extract/Classify/Relate and pass the
	// total here via ConsolidationRunRecord.TokensUsed.
	{
		model := r.model()
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
