package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// registerResources registers MCP resources for auto-loading into context.
func (s *Server) registerResources() error {
	// Register the active behaviors resource
	// This gets automatically loaded into Claude's context
	s.server.AddResource(&sdk.Resource{
		URI:         "floop://behaviors/active",
		Name:        "floop-active-behaviors",
		Description: "Learned behaviors that should guide agent actions. These are corrections and preferences captured from previous sessions.",
		MIMEType:    "text/markdown",
	}, s.handleBehaviorsResource)

	return nil
}

// handleBehaviorsResource returns active behaviors formatted for context injection.
func (s *Server) handleBehaviorsResource(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	// Build context for activation (default task: development)
	ctxBuilder := activation.NewContextBuilder()
	ctxBuilder.WithRepoRoot(s.root)
	ctxBuilder.WithTask("development")
	actCtx := ctxBuilder.Build()

	// Load all behaviors from store
	nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("failed to query behaviors: %w", err)
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

	// Format as markdown for context injection
	var sb strings.Builder
	sb.WriteString("# Learned Behaviors\n\n")
	sb.WriteString("These behaviors were learned from previous corrections. Follow them.\n\n")

	// Group by kind
	directives := []models.Behavior{}
	preferences := []models.Behavior{}
	procedures := []models.Behavior{}
	constraints := []models.Behavior{}

	for _, b := range result.Active {
		switch b.Kind {
		case models.BehaviorKindDirective:
			directives = append(directives, b)
		case models.BehaviorKindPreference:
			preferences = append(preferences, b)
		case models.BehaviorKindProcedure:
			procedures = append(procedures, b)
		case models.BehaviorKindConstraint:
			constraints = append(constraints, b)
		}
	}

	if len(constraints) > 0 {
		sb.WriteString("## Constraints (MUST follow)\n")
		for _, b := range constraints {
			sb.WriteString(fmt.Sprintf("- %s\n", b.Content.Canonical))
		}
		sb.WriteString("\n")
	}

	if len(directives) > 0 {
		sb.WriteString("## Directives\n")
		for _, b := range directives {
			sb.WriteString(fmt.Sprintf("- %s\n", b.Content.Canonical))
		}
		sb.WriteString("\n")
	}

	if len(preferences) > 0 {
		sb.WriteString("## Preferences\n")
		for _, b := range preferences {
			sb.WriteString(fmt.Sprintf("- %s\n", b.Content.Canonical))
		}
		sb.WriteString("\n")
	}

	if len(procedures) > 0 {
		sb.WriteString("## Procedures\n")
		for _, b := range procedures {
			sb.WriteString(fmt.Sprintf("- %s\n", b.Content.Canonical))
		}
		sb.WriteString("\n")
	}

	if len(result.Active) == 0 {
		sb.WriteString("No active behaviors for current context.\n")
	} else {
		sb.WriteString(fmt.Sprintf("---\n*%d behaviors active*\n", len(result.Active)))
	}

	return &sdk.ReadResourceResult{
		Contents: []*sdk.ResourceContents{
			{
				URI:      "floop://behaviors/active",
				MIMEType: "text/markdown",
				Text:     sb.String(),
			},
		},
	}, nil
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

	if args.Task != "" {
		ctxBuilder.WithTask(args.Task)
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
		// List corrections from corrections.jsonl file (not graph store)
		correctionsPath := filepath.Join(s.root, ".floop", "corrections.jsonl")
		file, err := os.Open(correctionsPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, FloopListOutput{
					Corrections: []CorrectionListItem{},
					Count:       0,
				}, nil
			}
			return nil, FloopListOutput{}, fmt.Errorf("failed to open corrections file: %w", err)
		}
		defer file.Close()

		var corrections []CorrectionListItem
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var c models.Correction
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				continue // Skip malformed lines
			}
			corrections = append(corrections, CorrectionListItem{
				ID:              c.ID,
				Timestamp:       c.Timestamp,
				AgentAction:     c.AgentAction,
				CorrectedAction: c.CorrectedAction,
				Processed:       c.Processed,
			})
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
