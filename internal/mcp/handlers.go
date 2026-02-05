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
	"github.com/nvandessel/feedback-loop/internal/assembly"
	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/dedup"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/summarization"
	"github.com/nvandessel/feedback-loop/internal/tiering"
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

	// Register expansion resource template for getting full behavior details
	s.server.AddResourceTemplate(&sdk.ResourceTemplate{
		URITemplate: "floop://behaviors/expand/{id}",
		Name:        "floop-behavior-expand",
		Description: "Get full details for a specific behavior. Use this when you need the complete content of a summarized behavior.",
		MIMEType:    "text/markdown",
	}, s.handleBehaviorExpandResource)

	return nil
}

// Default token budget for behavior injection
const defaultTokenBudget = 2000

// handleBehaviorsResource returns active behaviors formatted for context injection.
// Uses tiered injection to optimize token usage while preserving critical behaviors.
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

	if len(result.Active) == 0 {
		return &sdk.ReadResourceResult{
			Contents: []*sdk.ResourceContents{
				{
					URI:      "floop://behaviors/active",
					MIMEType: "text/markdown",
					Text:     "# Learned Behaviors\n\nNo memories for current context yet. Learn from corrections using `floop_learn`.\n",
				},
			},
		}, nil
	}

	// Create tiered injection plan
	scorer := ranking.NewRelevanceScorer(ranking.DefaultScorerConfig())
	summarizer := summarization.NewRuleSummarizer(summarization.DefaultConfig())
	assigner := tiering.NewTierAssigner(tiering.DefaultTierAssignerConfig(), scorer, summarizer)

	plan := assigner.AssignTiers(result.Active, &actCtx, defaultTokenBudget)

	// Compile tiered prompt
	compiler := assembly.NewCompiler()
	tieredPrompt := compiler.CompileTiered(plan)

	// Build final output with header
	var sb strings.Builder
	sb.WriteString("# Learned Behaviors\n\n")
	sb.WriteString("**CRITICAL:** These are YOUR learned memories from past sessions.\n")
	sb.WriteString("Violating a learned behavior means repeating a past mistake.\n\n")

	// Add the compiled tiered content
	if tieredPrompt.Text != "" {
		sb.WriteString(tieredPrompt.Text)
		sb.WriteString("\n\n")
	}

	// Add footer with stats
	sb.WriteString(fmt.Sprintf("---\n*%d memories active", plan.IncludedCount()))
	if len(plan.OmittedBehaviors) > 0 {
		sb.WriteString(fmt.Sprintf(" (%d summarized, %d available via floop://behaviors/expand/{id})",
			len(plan.SummarizedBehaviors), len(plan.OmittedBehaviors)))
	}
	sb.WriteString("*\n")

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

// handleBehaviorExpandResource returns full details for a specific behavior.
// This allows agents to retrieve complete content for summarized or omitted behaviors.
func (s *Server) handleBehaviorExpandResource(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	// Extract behavior ID from URI
	// URI format: floop://behaviors/expand/{id}
	uri := req.Params.URI
	prefix := "floop://behaviors/expand/"
	if !strings.HasPrefix(uri, prefix) {
		return nil, fmt.Errorf("invalid URI format: %s", uri)
	}
	behaviorID := strings.TrimPrefix(uri, prefix)
	if behaviorID == "" {
		return nil, fmt.Errorf("behavior ID is required")
	}

	// Query for the specific behavior
	nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{
		"kind": "behavior",
		"id":   behaviorID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query behavior: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("behavior not found: %s", behaviorID)
	}

	behavior := learning.NodeToBehavior(nodes[0])

	// Format full behavior details
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Behavior: %s\n\n", behavior.Name))
	sb.WriteString(fmt.Sprintf("**ID:** %s\n", behavior.ID))
	sb.WriteString(fmt.Sprintf("**Kind:** %s\n", behavior.Kind))
	sb.WriteString(fmt.Sprintf("**Confidence:** %.2f\n", behavior.Confidence))
	sb.WriteString(fmt.Sprintf("**Priority:** %d\n\n", behavior.Priority))

	sb.WriteString("## Content\n\n")
	sb.WriteString(behavior.Content.Canonical)
	sb.WriteString("\n")

	if behavior.Content.Expanded != "" {
		sb.WriteString("\n### Expanded\n\n")
		sb.WriteString(behavior.Content.Expanded)
		sb.WriteString("\n")
	}

	if len(behavior.When) > 0 {
		sb.WriteString("\n## Activation Context\n\n")
		for k, v := range behavior.When {
			sb.WriteString(fmt.Sprintf("- **%s:** %v\n", k, v))
		}
	}

	if behavior.Stats.TimesActivated > 0 {
		sb.WriteString("\n## Statistics\n\n")
		sb.WriteString(fmt.Sprintf("- Times Activated: %d\n", behavior.Stats.TimesActivated))
		sb.WriteString(fmt.Sprintf("- Times Followed: %d\n", behavior.Stats.TimesFollowed))
		if behavior.Stats.TimesConfirmed > 0 {
			sb.WriteString(fmt.Sprintf("- Times Confirmed: %d\n", behavior.Stats.TimesConfirmed))
		}
		if behavior.Stats.TimesOverridden > 0 {
			sb.WriteString(fmt.Sprintf("- Times Overridden: %d\n", behavior.Stats.TimesOverridden))
		}
	}

	return &sdk.ReadResourceResult{
		Contents: []*sdk.ResourceContents{
			{
				URI:      uri,
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

	// Create correction with nanosecond-precision ID for uniqueness
	now := time.Now()
	correction := models.Correction{
		ID:              fmt.Sprintf("c-%d", now.UnixNano()),
		Timestamp:       now,
		Context:         ctxSnapshot,
		AgentAction:     args.Wrong,
		CorrectedAction: args.Right,
		Corrector:       "mcp-client",
		Processed:       false,
	}

	// Configure learning loop - auto-merge is ON by default
	// This prevents duplicate behaviors from accumulating
	loopConfig := &learning.LearningLoopConfig{
		AutoAcceptThreshold: 0.8,
		AutoMerge:           true, // Always deduplicate
		AutoMergeThreshold:  0.9,
	}

	// Create deduplicator for automatic merging
	merger := dedup.NewBehaviorMerger(dedup.MergerConfig{})
	dedupConfig := dedup.DeduplicatorConfig{
		SimilarityThreshold: 0.9,
		AutoMerge:           true,
	}
	loopConfig.Deduplicator = dedup.NewStoreDeduplicator(s.store, merger, dedupConfig)

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

	// Mark correction as processed and write to corrections log for audit trail
	correction.Processed = true
	processedAt := time.Now()
	correction.ProcessedAt = &processedAt

	correctionsPath := filepath.Join(s.root, ".floop", "corrections.jsonl")
	if f, err := os.OpenFile(correctionsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		json.NewEncoder(f).Encode(correction)
		f.Close()
	}
	// Note: We don't fail if corrections.jsonl write fails - the behavior is already saved

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

	scope := constants.Scope(args.Scope)
	if args.Scope == "" {
		scope = constants.ScopeBoth
	}

	// Validate scope
	if !scope.Valid() {
		return nil, FloopDeduplicateOutput{}, fmt.Errorf("invalid scope: %s (must be 'local', 'global', or 'both')", args.Scope)
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
