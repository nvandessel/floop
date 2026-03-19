package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/consolidation"
	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/utils"
)

// FloopConsolidateInput defines the input for floop_consolidate tool.
type FloopConsolidateInput struct {
	Session string `json:"session,omitempty" jsonschema:"Session ID to consolidate (default: all unconsolidated events)"`
	Since   string `json:"since,omitempty" jsonschema:"Duration to look back (e.g. 1h, 24h, 7d). Overrides session filter"`
	DryRun  bool   `json:"dry_run,omitempty" jsonschema:"If true, extract and classify but do not promote to graph (default: false)"`
}

// FloopConsolidateOutput defines the output for floop_consolidate tool.
type FloopConsolidateOutput struct {
	EventsProcessed int                `json:"events_processed" jsonschema:"Number of events processed"`
	CandidatesFound int                `json:"candidates_found" jsonschema:"Number of memory candidates extracted"`
	Classified      int                `json:"classified" jsonschema:"Number of memories classified"`
	Promoted        int                `json:"promoted" jsonschema:"Number of memories promoted to graph (0 if dry_run)"`
	DryRun          bool               `json:"dry_run" jsonschema:"Whether this was a dry run"`
	Duration        string             `json:"duration" jsonschema:"Pipeline execution duration"`
	Candidates      []CandidateSummary `json:"candidates,omitempty" jsonschema:"Summary of extracted candidates"`
	Message         string             `json:"message" jsonschema:"Human-readable result message"`
}

// CandidateSummary provides a simplified view of a consolidation candidate.
type CandidateSummary struct {
	Type       string  `json:"type" jsonschema:"Candidate type (correction, decision, failure, etc.)"`
	Confidence float64 `json:"confidence" jsonschema:"Extraction confidence (0.0-1.0)"`
	Preview    string  `json:"preview" jsonschema:"Truncated preview of the raw text"`
}

// handleFloopConsolidate implements the floop_consolidate tool.
// Uses the shared event store on the Server struct — no per-call DB connections.
func (s *Server) handleFloopConsolidate(ctx context.Context, req *sdk.CallToolRequest, args FloopConsolidateInput) (_ *sdk.CallToolResult, _ FloopConsolidateOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_consolidate", start, retErr, map[string]string{
			"session": args.Session,
			"since":   args.Since,
			"dry_run": fmt.Sprintf("%t", args.DryRun),
		}, "global")
	}()

	if s.eventStore == nil {
		return nil, FloopConsolidateOutput{}, fmt.Errorf("event store not available")
	}

	// Query events based on filter
	var evts []events.Event
	var err error
	switch {
	case args.Since != "":
		dur, parseErr := utils.ParseDuration(args.Since)
		if parseErr != nil {
			return nil, FloopConsolidateOutput{}, fmt.Errorf("invalid 'since' duration %q: %w", args.Since, parseErr)
		}
		since := time.Now().Add(-dur)
		evts, err = s.eventStore.GetSince(ctx, since)
		if err != nil {
			return nil, FloopConsolidateOutput{}, fmt.Errorf("querying events since %v: %w", since, err)
		}
	case args.Session != "":
		evts, err = s.eventStore.GetUnconsolidatedBySession(ctx, args.Session)
		if err != nil {
			return nil, FloopConsolidateOutput{}, fmt.Errorf("querying unconsolidated events for session %q: %w", args.Session, err)
		}
	default:
		evts, err = s.eventStore.GetUnconsolidated(ctx)
		if err != nil {
			return nil, FloopConsolidateOutput{}, fmt.Errorf("querying unconsolidated events: %w", err)
		}
	}

	if len(evts) == 0 {
		return nil, FloopConsolidateOutput{
			EventsProcessed: 0,
			DryRun:          args.DryRun,
			Duration:        time.Since(start).String(),
			Message:         "No events found to consolidate",
		}, nil
	}

	// Resolve executor from config
	executor := ""
	if s.floopConfig != nil && s.floopConfig.Consolidation.Executor != "" {
		executor = s.floopConfig.Consolidation.Executor
	}

	// Warn if LLM executor is requested but no client is available
	if executor == "llm" && s.llmClient == nil {
		s.logger.Warn("executor=llm requested but no LLM provider configured; falling back to heuristic")
	}

	// Run consolidation pipeline
	var model string
	if s.floopConfig != nil {
		model = s.floopConfig.LLM.ComparisonModel
	}
	c := consolidation.NewConsolidator(executor, s.llmClient, nil, model)
	result, err := consolidation.NewRunner(c).
		Run(ctx, evts, s.store, consolidation.RunOptions{DryRun: args.DryRun})
	if err != nil {
		return nil, FloopConsolidateOutput{}, fmt.Errorf("consolidation pipeline failed: %w", err)
	}

	// Mark processed events as consolidated — fail the call if this errors,
	// since leaving events unmarked will cause duplicate promotion on next run.
	if !args.DryRun && len(result.SourceEventIDs) > 0 {
		if err := s.eventStore.MarkConsolidated(ctx, result.SourceEventIDs); err != nil {
			return nil, FloopConsolidateOutput{}, fmt.Errorf("marking events consolidated: %w", err)
		}
	}

	// Build candidate summaries
	var candidateSummaries []CandidateSummary
	for _, c := range result.Candidates {
		preview := c.RawText
		if runes := []rune(preview); len(runes) > 80 {
			preview = string(runes[:80]) + "..."
		}
		candidateSummaries = append(candidateSummaries, CandidateSummary{
			Type:       c.CandidateType,
			Confidence: c.Confidence,
			Preview:    preview,
		})
	}

	message := fmt.Sprintf("Consolidated %d events: %d candidates, %d classified, %d promoted",
		len(evts), len(result.Candidates), len(result.Classified), result.Promoted)
	if args.DryRun {
		message += " (dry run)"
	}

	// Sync store after promotion
	if !args.DryRun && result.Promoted > 0 {
		if err := s.store.Sync(ctx); err != nil {
			s.logger.Warn("failed to sync store after consolidation", "error", err)
		}
		s.debouncedRefreshPageRank()
	}

	return nil, FloopConsolidateOutput{
		EventsProcessed: len(evts),
		CandidatesFound: len(result.Candidates),
		Classified:      len(result.Classified),
		Promoted:        result.Promoted,
		DryRun:          args.DryRun,
		Duration:        result.Duration.String(),
		Candidates:      candidateSummaries,
		Message:         message,
	}, nil
}
