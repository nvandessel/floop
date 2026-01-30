package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/feedback-loop/internal/activation"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/models"
)

// registerTools registers all floop MCP tools with the server.
func (s *Server) registerTools() error {
	// Register floop_active tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_active",
		Description: "Get active behaviors for the current context (file, task, environment)",
	}, s.handleFloopActive)

	// Register floop_learn tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_learn",
		Description: "Capture a correction and extract a reusable behavior",
	}, s.handleFloopLearn)

	// Register floop_list tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_list",
		Description: "List all behaviors or corrections",
	}, s.handleFloopList)

	return nil
}

// FloopActiveInput defines the input for floop_active tool.
type FloopActiveInput struct {
	File string `json:"file,omitempty" jsonschema:"Current file path (relative to project root)"`
	Task string `json:"task,omitempty" jsonschema:"Current task type (e.g. 'development', 'testing', 'refactoring')"`
}

// FloopActiveOutput defines the output for floop_active tool.
type FloopActiveOutput struct {
	Context map[string]interface{} `json:"context" jsonschema:"Context used for activation"`
	Active  []BehaviorSummary      `json:"active" jsonschema:"List of active behaviors"`
	Count   int                    `json:"count" jsonschema:"Number of active behaviors"`
}

// BehaviorSummary provides a simplified view of a behavior.
type BehaviorSummary struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Kind       string                 `json:"kind"`
	Content    map[string]interface{} `json:"content"`
	Confidence float64                `json:"confidence"`
	When       map[string]interface{} `json:"when,omitempty"`
}

// handleFloopActive implements the floop_active tool.
func (s *Server) handleFloopActive(ctx context.Context, req *sdk.CallToolRequest, args FloopActiveInput) (*sdk.CallToolResult, FloopActiveOutput, error) {
	// Build context from parameters
	ctxBuilder := activation.NewContextBuilder()

	if args.File != "" {
		// Resolve file path relative to project root
		filePath := args.File
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(s.root, filePath)
		}
		ctxBuilder.WithFile(filePath)
	}

	if args.Task != "" {
		ctxBuilder.WithTask(args.Task)
	}

	ctxBuilder.WithRepoRoot(s.root)

	actCtx := ctxBuilder.Build()

	// Load all behaviors from store
	nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, FloopActiveOutput{}, fmt.Errorf("failed to query behaviors: %w", err)
	}

	// Convert nodes to behaviors
	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		behavior := learning.NodeToBehavior(node)
		behaviors = append(behaviors, behavior)
	}

	// Evaluate which behaviors are active
	evaluator := activation.NewEvaluator()
	matches := evaluator.Evaluate(actCtx, behaviors)

	// Resolve conflicts and get final active set
	resolver := activation.NewResolver()
	result := resolver.Resolve(matches)

	// Convert to summary format
	summaries := make([]BehaviorSummary, len(result.Active))
	for i, b := range result.Active {
		when := b.When
		if when == nil {
			when = make(map[string]interface{})
		}
		summaries[i] = BehaviorSummary{
			ID:         b.ID,
			Name:       b.Name,
			Kind:       string(b.Kind),
			Content:    behaviorContentToMap(b.Content),
			Confidence: b.Confidence,
			When:       when,
		}
	}

	// Build context map for output
	ctxMap := map[string]interface{}{
		"file":     actCtx.FilePath,
		"language": actCtx.FileLanguage,
		"task":     actCtx.Task,
		"repo":     actCtx.RepoRoot,
	}

	return nil, FloopActiveOutput{
		Context: ctxMap,
		Active:  summaries,
		Count:   len(summaries),
	}, nil
}

// FloopLearnInput defines the input for floop_learn tool.
type FloopLearnInput struct {
	Wrong string `json:"wrong" jsonschema:"What the agent did that needs correction,required"`
	Right string `json:"right" jsonschema:"What should have been done instead,required"`
	File  string `json:"file,omitempty" jsonschema:"Relevant file path for context"`
}

// FloopLearnOutput defines the output for floop_learn tool.
type FloopLearnOutput struct {
	CorrectionID  string   `json:"correction_id" jsonschema:"ID of the captured correction"`
	BehaviorID    string   `json:"behavior_id" jsonschema:"ID of the extracted behavior"`
	AutoAccepted  bool     `json:"auto_accepted" jsonschema:"Whether behavior was automatically accepted"`
	Confidence    float64  `json:"confidence" jsonschema:"Placement confidence (0.0-1.0)"`
	RequiresReview bool    `json:"requires_review" jsonschema:"Whether behavior requires manual review"`
	ReviewReasons []string `json:"review_reasons,omitempty" jsonschema:"Reasons why review is needed"`
	Message       string   `json:"message" jsonschema:"Human-readable result message"`
}

// handleFloopLearn implements the floop_learn tool.
func (s *Server) handleFloopLearn(ctx context.Context, req *sdk.CallToolRequest, args FloopLearnInput) (*sdk.CallToolResult, FloopLearnOutput, error) {
	// Validate required parameters
	if args.Wrong == "" {
		return nil, FloopLearnOutput{}, fmt.Errorf("'wrong' parameter is required")
	}
	if args.Right == "" {
		return nil, FloopLearnOutput{}, fmt.Errorf("'right' parameter is required")
	}

	// Build context
	ctxBuilder := activation.NewContextBuilder()

	if args.File != "" {
		filePath := args.File
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(s.root, filePath)
		}
		ctxBuilder.WithFile(filePath)
	}

	ctxBuilder.WithRepoRoot(s.root)
	ctxSnapshot := ctxBuilder.Build()

	// Create correction
	correction := models.Correction{
		ID:              fmt.Sprintf("correction-%d", time.Now().Unix()),
		Timestamp:       time.Now(),
		Context:         ctxSnapshot,
		AgentAction:     args.Wrong,
		CorrectedAction: args.Right,
		Corrector:       "mcp-client",
		Processed:       false,
	}

	// Process correction through learning loop
	loop := learning.NewLearningLoop(s.store, &learning.LearningLoopConfig{
		AutoAcceptThreshold: 0.8,
	})

	learningResult, err := loop.ProcessCorrection(ctx, correction)
	if err != nil {
		return nil, FloopLearnOutput{}, fmt.Errorf("failed to process correction: %w", err)
	}

	// Sync store to persist changes
	if err := s.store.Sync(ctx); err != nil {
		return nil, FloopLearnOutput{}, fmt.Errorf("failed to sync store: %w", err)
	}

	// Build result message
	message := fmt.Sprintf("Learned behavior: %s", learningResult.CandidateBehavior.Name)
	if learningResult.RequiresReview {
		message = fmt.Sprintf("Behavior requires review: %s (%s)",
			learningResult.CandidateBehavior.Name,
			strings.Join(learningResult.ReviewReasons, ", "))
	}

	return nil, FloopLearnOutput{
		CorrectionID:   correction.ID,
		BehaviorID:     learningResult.CandidateBehavior.ID,
		AutoAccepted:   learningResult.AutoAccepted,
		Confidence:     learningResult.Placement.Confidence,
		RequiresReview: learningResult.RequiresReview,
		ReviewReasons:  learningResult.ReviewReasons,
		Message:        message,
	}, nil
}

// FloopListInput defines the input for floop_list tool.
type FloopListInput struct {
	Corrections bool `json:"corrections,omitempty" jsonschema:"List corrections instead of behaviors (default: false)"`
}

// FloopListOutput defines the output for floop_list tool.
type FloopListOutput struct {
	Behaviors   []BehaviorListItem   `json:"behaviors,omitempty" jsonschema:"List of behaviors"`
	Corrections []CorrectionListItem `json:"corrections,omitempty" jsonschema:"List of corrections"`
	Count       int                  `json:"count" jsonschema:"Number of items"`
}

// BehaviorListItem provides a list view of a behavior.
type BehaviorListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Confidence float64   `json:"confidence"`
	Source     string    `json:"source"`
	CreatedAt  time.Time `json:"created_at"`
}

// CorrectionListItem provides a list view of a correction.
type CorrectionListItem struct {
	ID              string    `json:"id"`
	Timestamp       time.Time `json:"timestamp"`
	AgentAction     string    `json:"agent_action"`
	CorrectedAction string    `json:"corrected_action"`
	Processed       bool      `json:"processed"`
}

// handleFloopList implements the floop_list tool.
func (s *Server) handleFloopList(ctx context.Context, req *sdk.CallToolRequest, args FloopListInput) (*sdk.CallToolResult, FloopListOutput, error) {
	if args.Corrections {
		// List corrections
		nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "correction"})
		if err != nil {
			return nil, FloopListOutput{}, fmt.Errorf("failed to query corrections: %w", err)
		}

		corrections := make([]CorrectionListItem, len(nodes))
		for i, node := range nodes {
			// Extract correction data from node content
			agentAction, _ := node.Content["agent_action"].(string)
			correctedAction, _ := node.Content["corrected_action"].(string)
			processed, _ := node.Content["processed"].(bool)

			var timestamp time.Time
			if ts, ok := node.Content["timestamp"].(string); ok {
				timestamp, _ = time.Parse(time.RFC3339, ts)
			}

			corrections[i] = CorrectionListItem{
				ID:              node.ID,
				Timestamp:       timestamp,
				AgentAction:     agentAction,
				CorrectedAction: correctedAction,
				Processed:       processed,
			}
		}

		return nil, FloopListOutput{
			Corrections: corrections,
			Count:       len(corrections),
		}, nil
	}

	// List behaviors
	nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, FloopListOutput{}, fmt.Errorf("failed to query behaviors: %w", err)
	}

	behaviors := make([]BehaviorListItem, len(nodes))
	for i, node := range nodes {
		behavior := learning.NodeToBehavior(node)

		// Determine source
		source := "unknown"
		if behavior.Provenance.SourceType != "" {
			source = string(behavior.Provenance.SourceType)
		}

		behaviors[i] = BehaviorListItem{
			ID:         behavior.ID,
			Name:       behavior.Name,
			Kind:       string(behavior.Kind),
			Confidence: behavior.Confidence,
			Source:     source,
			CreatedAt:  behavior.Provenance.CreatedAt,
		}
	}

	return nil, FloopListOutput{
		Behaviors: behaviors,
		Count:     len(behaviors),
	}, nil
}

// behaviorContentToMap converts BehaviorContent to a map for JSON serialization.
func behaviorContentToMap(content models.BehaviorContent) map[string]interface{} {
	m := make(map[string]interface{})
	m["canonical"] = content.Canonical
	if content.Expanded != "" {
		m["expanded"] = content.Expanded
	}
	if content.Structured != nil && len(content.Structured) > 0 {
		m["structured"] = content.Structured
	}
	return m
}
