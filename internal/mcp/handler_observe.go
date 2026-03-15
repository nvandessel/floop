package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/events"
	_ "modernc.org/sqlite" // SQLite driver
)

// FloopObserveInput defines the input for floop_observe tool.
type FloopObserveInput struct {
	Source    string         `json:"source" jsonschema:"Agent source identifier,required"`
	Content   string         `json:"content" jsonschema:"Event content,required"`
	Actor     string         `json:"actor,omitempty" jsonschema:"Event actor: user, agent, tool, system (default: agent)"`
	Kind      string         `json:"kind,omitempty" jsonschema:"Event kind: message, action, result, error, correction (default: message)"`
	Metadata  map[string]any `json:"metadata,omitempty" jsonschema:"Optional metadata key-value pairs"`
	SessionID string         `json:"session_id,omitempty" jsonschema:"Optional session ID (auto-generated if omitted)"`
}

// FloopObserveOutput defines the output for floop_observe tool.
type FloopObserveOutput struct {
	EventID   string `json:"event_id" jsonschema:"ID of the recorded event"`
	SessionID string `json:"session_id" jsonschema:"Session ID used for the event"`
	Message   string `json:"message" jsonschema:"Human-readable result message"`
}

// handleFloopObserve implements the floop_observe tool.
func (s *Server) handleFloopObserve(ctx context.Context, req *sdk.CallToolRequest, args FloopObserveInput) (_ *sdk.CallToolResult, _ FloopObserveOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_observe", start, retErr, map[string]string{
			"source": args.Source,
		}, "global")
	}()

	// Validate required parameters
	if args.Source == "" {
		return nil, FloopObserveOutput{}, fmt.Errorf("'source' parameter is required")
	}
	if args.Content == "" {
		return nil, FloopObserveOutput{}, fmt.Errorf("'content' parameter is required")
	}

	// Apply defaults
	actor := events.EventActor(args.Actor)
	if actor == "" {
		actor = events.ActorAgent
	}
	kind := events.EventKind(args.Kind)
	if kind == "" {
		kind = events.KindMessage
	}

	// Validate actor and kind
	validActors := map[events.EventActor]bool{
		events.ActorUser: true, events.ActorAgent: true,
		events.ActorTool: true, events.ActorSystem: true,
	}
	if !validActors[actor] {
		return nil, FloopObserveOutput{}, fmt.Errorf("invalid actor %q: must be user, agent, tool, or system", args.Actor)
	}

	validKinds := map[events.EventKind]bool{
		events.KindMessage: true, events.KindAction: true,
		events.KindResult: true, events.KindError: true, events.KindCorrection: true,
	}
	if !validKinds[kind] {
		return nil, FloopObserveOutput{}, fmt.Errorf("invalid kind %q: must be message, action, result, error, or correction", args.Kind)
	}

	sessionID := args.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("mcp-%d", time.Now().UnixNano())
	}

	now := time.Now()
	evt := events.Event{
		ID:        fmt.Sprintf("evt-%d", now.UnixNano()),
		SessionID: sessionID,
		Timestamp: now,
		Source:    args.Source,
		Actor:     actor,
		Kind:      kind,
		Content:   args.Content,
		Metadata:  args.Metadata,
		CreatedAt: now,
	}

	// Open the global floop DB for event storage
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, FloopObserveOutput{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	dbDir := filepath.Join(homeDir, ".floop")
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return nil, FloopObserveOutput{}, fmt.Errorf("creating .floop directory: %w", err)
	}
	dbPath := filepath.Join(dbDir, "floop.db")

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, FloopObserveOutput{}, fmt.Errorf("opening event database: %w", err)
	}
	defer db.Close()

	eventStore := events.NewSQLiteEventStore(db)

	// Ensure schema exists
	if err := eventStore.InitSchema(ctx); err != nil {
		return nil, FloopObserveOutput{}, fmt.Errorf("initializing event schema: %w", err)
	}

	// Add the event
	if err := eventStore.Add(ctx, evt); err != nil {
		return nil, FloopObserveOutput{}, fmt.Errorf("recording event: %w", err)
	}

	message := fmt.Sprintf("Recorded event %s from %s", evt.ID, args.Source)

	return nil, FloopObserveOutput{
		EventID:   evt.ID,
		SessionID: sessionID,
		Message:   message,
	}, nil
}
