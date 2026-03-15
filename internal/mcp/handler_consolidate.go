package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/consolidation"
	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/utils"
	_ "modernc.org/sqlite" // SQLite driver
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
func (s *Server) handleFloopConsolidate(ctx context.Context, req *sdk.CallToolRequest, args FloopConsolidateInput) (_ *sdk.CallToolResult, _ FloopConsolidateOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_consolidate", start, retErr, map[string]string{
			"session": args.Session,
			"since":   args.Since,
			"dry_run": fmt.Sprintf("%t", args.DryRun),
		}, "global")
	}()

	// Open the global floop DB for event storage
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, FloopConsolidateOutput{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	dbDir := filepath.Join(homeDir, ".floop")
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return nil, FloopConsolidateOutput{}, fmt.Errorf("creating .floop directory: %w", err)
	}
	dbPath := filepath.Join(dbDir, "floop.db")

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, FloopConsolidateOutput{}, fmt.Errorf("opening event database: %w", err)
	}
	defer db.Close()

	eventStore := events.NewSQLiteEventStore(db)

	// Ensure schema exists
	if err := eventStore.InitSchema(ctx); err != nil {
		return nil, FloopConsolidateOutput{}, fmt.Errorf("initializing event schema: %w", err)
	}

	// Query events based on filter
	var evts []events.Event
	switch {
	case args.Since != "":
		dur, err := utils.ParseDuration(args.Since)
		if err != nil {
			return nil, FloopConsolidateOutput{}, fmt.Errorf("invalid 'since' duration %q: %w", args.Since, err)
		}
		since := time.Now().Add(-dur)
		evts, err = eventStore.GetSince(ctx, since)
		if err != nil {
			return nil, FloopConsolidateOutput{}, fmt.Errorf("querying events since %v: %w", since, err)
		}
	case args.Session != "":
		evts, err = eventStore.GetBySession(ctx, args.Session)
		if err != nil {
			return nil, FloopConsolidateOutput{}, fmt.Errorf("querying events for session %q: %w", args.Session, err)
		}
	default:
		evts, err = eventStore.GetUnconsolidated(ctx)
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

	// Run consolidation pipeline
	consolidator := consolidation.NewHeuristicConsolidator()
	runner := consolidation.NewRunner(consolidator)

	opts := consolidation.RunOptions{
		DryRun: args.DryRun,
	}

	// Pass the server's graph store for promotion (nil if dry-run handled internally)
	result, err := runner.Run(ctx, evts, s.store, opts)
	if err != nil {
		return nil, FloopConsolidateOutput{}, fmt.Errorf("consolidation pipeline failed: %w", err)
	}

	// Build candidate summaries
	var candidateSummaries []CandidateSummary
	for _, c := range result.Candidates {
		preview := c.RawText
		if len(preview) > 80 {
			preview = preview[:80] + "..."
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
