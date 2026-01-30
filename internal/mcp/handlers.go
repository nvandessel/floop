package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/feedback-loop/internal/activation"
	"github.com/nvandessel/feedback-loop/internal/dedup"
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

	// Register floop_deduplicate tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_deduplicate",
		Description: "Find and merge duplicate behaviors in the store",
	}, s.handleFloopDeduplicate)

	return nil
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

	// Configure learning loop with auto-merge if requested
	loopConfig := &learning.LearningLoopConfig{
		AutoAcceptThreshold: 0.8,
		AutoMerge:           args.AutoMerge,
		AutoMergeThreshold:  0.9,
	}

	// If auto-merge is enabled, create a deduplicator
	if args.AutoMerge {
		merger := dedup.NewBehaviorMerger(dedup.MergerConfig{}) // Empty config uses basic merge
		dedupConfig := dedup.DeduplicatorConfig{
			SimilarityThreshold: 0.9,
			AutoMerge:           true,
		}
		loopConfig.Deduplicator = dedup.NewStoreDeduplicator(s.store, merger, dedupConfig)
	}

	// Process correction through learning loop
	loop := learning.NewLearningLoop(s.store, loopConfig)

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
	if learningResult.MergedIntoExisting {
		message = fmt.Sprintf("Merged into existing behavior: %s (similarity: %.2f)",
			learningResult.MergedBehaviorID, learningResult.MergeSimilarity)
	} else if learningResult.RequiresReview {
		message = fmt.Sprintf("Behavior requires review: %s (%s)",
			learningResult.CandidateBehavior.Name,
			strings.Join(learningResult.ReviewReasons, ", "))
	}

	return nil, FloopLearnOutput{
		CorrectionID:    correction.ID,
		BehaviorID:      learningResult.CandidateBehavior.ID,
		AutoAccepted:    learningResult.AutoAccepted,
		Confidence:      learningResult.Placement.Confidence,
		RequiresReview:  learningResult.RequiresReview,
		ReviewReasons:   learningResult.ReviewReasons,
		MergedIntoID:    learningResult.MergedBehaviorID,
		MergeSimilarity: learningResult.MergeSimilarity,
		Message:         message,
	}, nil
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

// handleFloopDeduplicate implements the floop_deduplicate tool.
func (s *Server) handleFloopDeduplicate(ctx context.Context, req *sdk.CallToolRequest, args FloopDeduplicateInput) (*sdk.CallToolResult, FloopDeduplicateOutput, error) {
	// Set defaults
	threshold := args.Threshold
	if threshold <= 0 || threshold > 1.0 {
		threshold = 0.9
	}

	scope := args.Scope
	if scope == "" {
		scope = "both"
	}

	// Validate scope
	if scope != "local" && scope != "global" && scope != "both" {
		return nil, FloopDeduplicateOutput{}, fmt.Errorf("invalid scope: %s (must be 'local', 'global', or 'both')", scope)
	}

	// Configure deduplicator
	dedupConfig := dedup.DeduplicatorConfig{
		SimilarityThreshold: threshold,
		AutoMerge:           !args.DryRun,
	}

	merger := dedup.NewBehaviorMerger(dedup.MergerConfig{}) // Empty config uses basic merge
	deduplicator := dedup.NewStoreDeduplicator(s.store, merger, dedupConfig)

	// Perform deduplication
	report, err := deduplicator.DeduplicateStore(ctx, s.store)
	if err != nil {
		return nil, FloopDeduplicateOutput{}, fmt.Errorf("deduplication failed: %w", err)
	}

	// Sync store to persist changes (if not dry run)
	if !args.DryRun {
		if err := s.store.Sync(ctx); err != nil {
			return nil, FloopDeduplicateOutput{}, fmt.Errorf("failed to sync store: %w", err)
		}
	}

	// Convert results to output format
	results := make([]DeduplicationResult, 0)
	if report.MergedBehaviors != nil {
		for _, merged := range report.MergedBehaviors {
			results = append(results, DeduplicationResult{
				BehaviorID:   merged.ID,
				BehaviorName: merged.Name,
				Action:       "merge",
				MergedID:     merged.ID,
			})
		}
	}

	// Build message
	var message string
	if args.DryRun {
		message = fmt.Sprintf("Dry run: found %d duplicate pairs (no changes made)", report.DuplicatesFound)
	} else {
		message = fmt.Sprintf("Deduplication complete: found %d duplicates, merged %d behaviors",
			report.DuplicatesFound, report.MergesPerformed)
	}

	return nil, FloopDeduplicateOutput{
		DuplicatesFound: report.DuplicatesFound,
		Merged:          report.MergesPerformed,
		Results:         results,
		Message:         message,
	}, nil
}
