package mcp

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/events"
)

// observeCounter provides unique IDs for MCP observe events.
var observeCounter atomic.Int64

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
// Uses the shared event store on the Server struct — no per-call DB connections.
func (s *Server) handleFloopObserve(ctx context.Context, req *sdk.CallToolRequest, args FloopObserveInput) (_ *sdk.CallToolResult, _ FloopObserveOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_observe", start, retErr, map[string]string{
			"source": args.Source,
		}, "global")
	}()

	if args.Source == "" {
		return nil, FloopObserveOutput{}, fmt.Errorf("'source' parameter is required")
	}
	if args.Content == "" {
		return nil, FloopObserveOutput{}, fmt.Errorf("'content' parameter is required")
	}
	if s.eventStore == nil {
		return nil, FloopObserveOutput{}, fmt.Errorf("event store not available")
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

	now := time.Now()
	counter := observeCounter.Add(1)

	sessionID := args.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("mcp-%d-%d", now.UnixNano(), counter)
	}

	evt := events.Event{
		ID:        fmt.Sprintf("evt-%d-%d", now.UnixNano(), counter),
		SessionID: sessionID,
		Timestamp: now,
		Source:    args.Source,
		Actor:     actor,
		Kind:      kind,
		Content:   args.Content,
		Metadata:  args.Metadata,
		ProjectID: s.projectID,
		CreatedAt: now,
	}

	if err := s.eventStore.Add(ctx, evt); err != nil {
		return nil, FloopObserveOutput{}, fmt.Errorf("recording event: %w", err)
	}

	return nil, FloopObserveOutput{
		EventID:   evt.ID,
		SessionID: sessionID,
		Message:   fmt.Sprintf("Recorded event %s from %s", evt.ID, args.Source),
	}, nil
}
