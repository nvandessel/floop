package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/ratelimit"
)

// handleFloopFeedback implements the floop_feedback tool.
// It records explicit feedback (confirmed or overridden) for a behavior.
func (s *Server) handleFloopFeedback(ctx context.Context, req *sdk.CallToolRequest, args FloopFeedbackInput) (_ *sdk.CallToolResult, _ FloopFeedbackOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_feedback", start, retErr, sanitizeToolParams("floop_feedback", map[string]interface{}{
			"behavior_id": args.BehaviorID, "signal": args.Signal,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_feedback"); err != nil {
		return nil, FloopFeedbackOutput{}, err
	}

	// Validate required fields
	if args.BehaviorID == "" {
		return nil, FloopFeedbackOutput{}, fmt.Errorf("'behavior_id' parameter is required")
	}
	if args.Signal == "" {
		return nil, FloopFeedbackOutput{}, fmt.Errorf("'signal' parameter is required")
	}
	if args.Signal != "confirmed" && args.Signal != "overridden" {
		return nil, FloopFeedbackOutput{}, fmt.Errorf("'signal' must be 'confirmed' or 'overridden', got %q", args.Signal)
	}

	// Verify behavior exists
	node, err := s.store.GetNode(ctx, args.BehaviorID)
	if err != nil {
		return nil, FloopFeedbackOutput{}, fmt.Errorf("failed to look up behavior: %w", err)
	}
	if node == nil {
		return nil, FloopFeedbackOutput{}, fmt.Errorf("behavior not found: %s", args.BehaviorID)
	}

	// Record the feedback signal
	type feedbackRecorder interface {
		RecordConfirmed(ctx context.Context, behaviorID string) error
		RecordOverridden(ctx context.Context, behaviorID string) error
	}

	recorder, ok := s.store.(feedbackRecorder)
	if !ok {
		return nil, FloopFeedbackOutput{}, fmt.Errorf("store does not support feedback recording")
	}

	switch args.Signal {
	case "confirmed":
		if err := recorder.RecordConfirmed(ctx, args.BehaviorID); err != nil {
			return nil, FloopFeedbackOutput{}, fmt.Errorf("failed to record confirmed: %w", err)
		}
	case "overridden":
		if err := recorder.RecordOverridden(ctx, args.BehaviorID); err != nil {
			return nil, FloopFeedbackOutput{}, fmt.Errorf("failed to record overridden: %w", err)
		}
	}

	message := fmt.Sprintf("Feedback recorded: behavior %s marked as %s", args.BehaviorID, args.Signal)

	return nil, FloopFeedbackOutput{
		BehaviorID: args.BehaviorID,
		Signal:     args.Signal,
		Message:    message,
	}, nil
}
