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
	"github.com/nvandessel/feedback-loop/internal/backup"
	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/dedup"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/pathutil"

	"github.com/nvandessel/feedback-loop/internal/ratelimit"
	"github.com/nvandessel/feedback-loop/internal/sanitize"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/tiering"
	"github.com/nvandessel/feedback-loop/internal/visualization"
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

	// Register floop_backup tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_backup",
		Description: "Export full graph state (nodes + edges) to a backup file",
	}, s.handleFloopBackup)

	// Register floop_restore tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_restore",
		Description: "Import graph state from a backup file (merge or replace)",
	}, s.handleFloopRestore)

	// Register floop_connect tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_connect",
		Description: "Create an edge between two behaviors for spreading activation",
	}, s.handleFloopConnect)

	// Register floop_validate tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_validate",
		Description: "Validate the behavior graph for consistency issues (dangling references, cycles, self-references)",
	}, s.handleFloopValidate)

	// Register floop_graph tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_graph",
		Description: "Render the behavior graph in DOT (Graphviz), JSON, or interactive HTML format for visualization",
	}, s.handleFloopGraph)

	// Register floop_feedback tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_feedback",
		Description: "Provide explicit feedback on a behavior: confirmed (helpful) or overridden (contradicted)",
	}, s.handleFloopFeedback)

	return nil
}

// registerResources registers MCP resources for auto-loading into context.
func (s *Server) registerResources() error {
	// Register the active behaviors resource
	// This gets automatically loaded into Claude's context
	s.server.AddResource(&sdk.Resource{
		URI:         "floop://behaviors/active",
		Name:        "floop-active-behaviors",
		Description: "Patterns and suggestions from previous sessions that may be relevant to the current task.",
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

	// Create tiered injection plan via bridge → ActivationTierMapper
	results, behaviorMap := tiering.BehaviorsToResults(result.Active)
	mapper := tiering.NewActivationTierMapper(tiering.DefaultActivationTierConfig())
	plan := mapper.MapResults(results, behaviorMap, s.floopConfig.TokenBudget.Default)

	// Compile tiered prompt
	compiler := assembly.NewCompiler()
	tieredPrompt := compiler.CompileTiered(plan)

	// Build final output with header
	var sb strings.Builder
	sb.WriteString("# Learned Behaviors\n\n")
	sb.WriteString("Suggestions based on patterns from previous sessions.\n")
	sb.WriteString("Apply these where relevant; override when context requires it.\n\n")

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
func (s *Server) handleFloopActive(ctx context.Context, req *sdk.CallToolRequest, args FloopActiveInput) (_ *sdk.CallToolResult, _ FloopActiveOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_active", start, retErr, sanitizeToolParams("floop_active", map[string]interface{}{
			"file": args.File, "task": args.Task,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_active"); err != nil {
		return nil, FloopActiveOutput{}, err
	}

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

	// Spread activation through graph edges
	seeds := matchesToSeeds(matches)

	// Boost seeds with PageRank scores (15% blend — tiebreaker, not dominator)
	s.pageRankMu.RLock()
	prScores := s.pageRankCache
	s.pageRankMu.RUnlock()
	seeds = boostSeedsWithPageRank(seeds, prScores, 0.15)

	var spreadResults []spreading.Result
	if len(seeds) > 0 {
		spreadConfig := spreading.DefaultConfig()
		affinityConfig := spreading.DefaultAffinityConfig()
		spreadConfig.Affinity = &affinityConfig
		spreadConfig.TagProvider = spreading.NewStoreTagProvider(s.store)
		engine := spreading.NewEngine(s.store, spreadConfig)
		var err error
		spreadResults, err = engine.Activate(ctx, seeds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: spreading activation failed: %v\n", err)
		} else {
			matches = mergeSpreadResults(ctx, s.store, matches, spreadResults)
		}

		// Background: stamp LastActivated on edges touching seed behaviors
		seedIDs := make([]string, len(seeds))
		for i, seed := range seeds {
			seedIDs[i] = seed.BehaviorID
		}
		s.runBackground("edge-timestamp", func() {
			type edgeToucher interface {
				TouchEdges(ctx context.Context, behaviorIDs []string) error
			}
			if toucher, ok := s.store.(edgeToucher); ok {
				if err := toucher.TouchEdges(context.Background(), seedIDs); err != nil {
					fmt.Fprintf(os.Stderr, "warning: edge timestamp failed: %v\n", err)
				}
			}
		})

		// Background: Hebbian co-activation learning.
		// Extract co-activated pairs from spread results and update edge weights
		// via Oja's self-limiting rule. New edges are gated by co-occurrence count.
		seedIDSet := make(map[string]bool, len(seedIDs))
		for _, id := range seedIDs {
			seedIDSet[id] = true
		}
		pairs := spreading.ExtractCoActivationPairs(spreadResults, seedIDSet, s.hebbianConfig)
		if len(pairs) > 0 {
			s.runBackground("hebbian-update", func() {
				s.applyHebbianUpdates(context.Background(), pairs, s.hebbianConfig)
				s.debouncedRefreshPageRank()
			})
		}
	}

	// Resolve conflicts and get final active set
	resolver := activation.NewResolver()
	result := resolver.Resolve(matches)

	// Build spread metadata index for populating summaries
	spreadIndex := buildSpreadIndex(seeds, matches, spreadResults)

	// Build inputs for token budget enforcement via ActivationTierMapper.
	// Convert active behaviors into spreading.Result slice and behavior map.
	tierResults := make([]spreading.Result, 0, len(result.Active))
	behaviorMap := make(map[string]*models.Behavior, len(result.Active))
	for i := range result.Active {
		b := &result.Active[i]
		behaviorMap[b.ID] = b
		act := 0.0
		dist := 0
		seedSrc := ""
		if meta, ok := spreadIndex[b.ID]; ok {
			act = meta.activation
			dist = meta.distance
			seedSrc = meta.seedSource
		}
		tierResults = append(tierResults, spreading.Result{
			BehaviorID: b.ID,
			Activation: act,
			Distance:   dist,
			SeedSource: seedSrc,
		})
	}

	// Apply token budget enforcement: tier and demote behaviors to fit budget.
	mapper := tiering.NewActivationTierMapper(tiering.DefaultActivationTierConfig())
	plan := mapper.MapResults(tierResults, behaviorMap, s.floopConfig.TokenBudget.Default)

	// Build summaries from the injection plan (included behaviors only).
	included := plan.IncludedBehaviors()
	summaries := make([]BehaviorSummary, 0, len(included))
	for _, ib := range included {
		b := ib.Behavior
		when := b.When
		if when == nil {
			when = make(map[string]interface{})
		}

		// Content varies by tier:
		// - Full: all content fields (canonical + expanded + structured)
		// - Summary/NameOnly: only the tier-appropriate content string
		var content map[string]interface{}
		if ib.Tier == models.TierFull {
			content = behaviorContentToMap(b.Content)
		} else {
			content = map[string]interface{}{
				"canonical": ib.Content,
			}
		}

		summary := BehaviorSummary{
			ID:         b.ID,
			Name:       b.Name,
			Kind:       string(b.Kind),
			Tier:       ib.Tier.String(),
			Content:    content,
			Confidence: b.Confidence,
			When:       when,
			Tags:       b.Content.Tags,
		}
		if meta, ok := spreadIndex[b.ID]; ok {
			summary.Activation = meta.activation
			summary.Distance = meta.distance
			summary.SeedSource = meta.seedSource
		}
		summaries = append(summaries, summary)
	}

	// Build context map for output
	ctxMap := map[string]interface{}{
		"file":     actCtx.FilePath,
		"language": actCtx.FileLanguage,
		"task":     actCtx.Task,
		"repo":     actCtx.RepoRoot,
	}

	// Compute session-scoped implicit confirmations.
	// Behaviors that are active and NOT yet confirmed this session get
	// a single implicit confirmation. This bounds the signal to 1 per
	// behavior per session instead of N-1 (where N = floop_active calls).
	activeBehaviors := result.Active

	var implicitConfirmIDs []string
	s.confirmedSessionMu.Lock()
	for _, b := range activeBehaviors {
		if strings.HasPrefix(b.ID, "seed-") {
			continue
		}
		if _, already := s.confirmedThisSession[b.ID]; !already {
			s.confirmedThisSession[b.ID] = struct{}{}
			implicitConfirmIDs = append(implicitConfirmIDs, b.ID)
		}
	}
	s.confirmedSessionMu.Unlock()

	// Record activation hits + implicit confirmations in background.
	// Note: confidence reinforcement has been replaced by ACT-R base-level activation
	// (see ranking/actr.go), which derives frequency+recency from existing data.
	s.runBackground("activation-recording", func() {
		type activationRecorder interface {
			RecordActivationHit(ctx context.Context, behaviorID string) error
		}
		if recorder, ok := s.store.(activationRecorder); ok {
			for _, b := range activeBehaviors {
				if strings.HasPrefix(b.ID, "seed-") {
					continue
				}
				if err := recorder.RecordActivationHit(context.Background(), b.ID); err != nil {
					fmt.Fprintf(os.Stderr, "warning: activation hit recording failed: %v\n", err)
				}
			}
		}

		// Record session-scoped implicit confirmations.
		type confirmRecorder interface {
			RecordConfirmed(ctx context.Context, behaviorID string) error
		}
		if recorder, ok := s.store.(confirmRecorder); ok {
			for _, id := range implicitConfirmIDs {
				if err := recorder.RecordConfirmed(context.Background(), id); err != nil {
					fmt.Fprintf(os.Stderr, "warning: implicit confirmation recording failed: %v\n", err)
				}
			}
		}
	})

	return nil, FloopActiveOutput{
		Context: ctxMap,
		Active:  summaries,
		Count:   len(summaries),
		TokenStats: &TokenStats{
			TotalCanonicalTokens: plan.TotalTokens,
			BudgetDefault:        s.floopConfig.TokenBudget.Default,
			BehaviorCount:        plan.BehaviorCount(),
			FullCount:            len(plan.FullBehaviors),
			SummaryCount:         len(plan.SummarizedBehaviors),
			NameOnlyCount:        len(plan.NameOnlyBehaviors),
			OmittedCount:         len(plan.OmittedBehaviors),
		},
	}, nil
}

// handleFloopLearn implements the floop_learn tool.
func (s *Server) handleFloopLearn(ctx context.Context, req *sdk.CallToolRequest, args FloopLearnInput) (_ *sdk.CallToolResult, _ FloopLearnOutput, retErr error) {
	start := time.Now()
	var auditScope string
	defer func() {
		if auditScope == "" {
			auditScope = "local" // fallback if error before scope is determined
		}
		s.auditTool("floop_learn", start, retErr, sanitizeToolParams("floop_learn", map[string]interface{}{
			"wrong": args.Wrong, "right": args.Right, "file": args.File, "task": args.Task, "auto_merge": args.AutoMerge,
		}), auditScope)
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_learn"); err != nil {
		return nil, FloopLearnOutput{}, err
	}

	// Validate required parameters
	if args.Wrong == "" {
		return nil, FloopLearnOutput{}, fmt.Errorf("'wrong' parameter is required")
	}
	if args.Right == "" {
		return nil, FloopLearnOutput{}, fmt.Errorf("'right' parameter is required")
	}

	// Sanitize inputs at the handler level as defense-in-depth.
	// The extraction layer also sanitizes, but this protects against
	// any code path that bypasses the learning loop.
	args.Wrong = sanitize.SanitizeBehaviorContent(args.Wrong)
	args.Right = sanitize.SanitizeBehaviorContent(args.Right)
	if args.Task != "" {
		args.Task = sanitize.SanitizeBehaviorContent(args.Task)
	}
	if args.File != "" {
		args.File = sanitize.SanitizeFilePath(args.File)
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
	auditScope = string(learningResult.Scope)

	// Sync store to persist changes
	if err := s.store.Sync(ctx); err != nil {
		return nil, FloopLearnOutput{}, fmt.Errorf("failed to sync store: %w", err)
	}

	// Auto-backup after successful learn (bounded background worker)
	if s.backupConfig == nil || s.backupConfig.AutoBackup {
		s.runBackground("auto-backup", func() {
			backupDir, err := backup.DefaultBackupDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: auto-backup failed (dir): %v\n", err)
				return
			}
			backupPath := backup.GenerateBackupPath(backupDir)
			if _, err := backup.Backup(context.Background(), s.store, backupPath); err != nil {
				fmt.Fprintf(os.Stderr, "warning: auto-backup failed: %v\n", err)
				return
			}
			if _, err := backup.ApplyRetention(backupDir, s.retentionPolicy); err != nil {
				fmt.Fprintf(os.Stderr, "warning: auto-backup retention failed: %v\n", err)
			}
		})
	}

	// Debounced PageRank refresh after graph mutation
	s.debouncedRefreshPageRank()

	// Mark correction as processed and write to corrections log for audit trail
	correction.Processed = true
	processedAt := time.Now()
	correction.ProcessedAt = &processedAt

	correctionsPath := filepath.Join(s.root, ".floop", "corrections.jsonl")
	if f, err := os.OpenFile(correctionsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600); err == nil {
		json.NewEncoder(f).Encode(correction)
		f.Close()
	}
	// Note: We don't fail if corrections.jsonl write fails - the behavior is already saved

	// Build result message with scope info
	scope := string(learningResult.Scope)
	message := fmt.Sprintf("Learned behavior (%s): %s", scope, learningResult.CandidateBehavior.Name)
	if learningResult.MergedIntoExisting {
		message = fmt.Sprintf("Merged into existing behavior (%s): %s (similarity: %.2f)",
			scope, learningResult.MergedBehaviorID, learningResult.MergeSimilarity)
	} else if learningResult.RequiresReview {
		message = fmt.Sprintf("Behavior requires review (%s): %s (%s)",
			scope, learningResult.CandidateBehavior.Name,
			strings.Join(learningResult.ReviewReasons, ", "))
	}

	return nil, FloopLearnOutput{
		CorrectionID:    correction.ID,
		BehaviorID:      learningResult.CandidateBehavior.ID,
		Scope:           scope,
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
func (s *Server) handleFloopList(ctx context.Context, req *sdk.CallToolRequest, args FloopListInput) (_ *sdk.CallToolResult, _ FloopListOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_list", start, retErr, sanitizeToolParams("floop_list", map[string]interface{}{
			"corrections": args.Corrections, "tag": args.Tag,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_list"); err != nil {
		return nil, FloopListOutput{}, err
	}

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

	behaviors := make([]BehaviorListItem, 0, len(nodes))
	for _, node := range nodes {
		behavior := learning.NodeToBehavior(node)

		// Filter by tag if specified
		if args.Tag != "" {
			found := false
			for _, t := range behavior.Content.Tags {
				if t == args.Tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Determine source
		source := "unknown"
		if behavior.Provenance.SourceType != "" {
			source = string(behavior.Provenance.SourceType)
		}

		behaviors = append(behaviors, BehaviorListItem{
			ID:         behavior.ID,
			Name:       behavior.Name,
			Kind:       string(behavior.Kind),
			Confidence: behavior.Confidence,
			Tags:       behavior.Content.Tags,
			Source:     source,
			CreatedAt:  behavior.Provenance.CreatedAt,
		})
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
	if len(content.Tags) > 0 {
		m["tags"] = content.Tags
	}
	return m
}

// matchesToSeeds converts activation results to spreading seeds.
func matchesToSeeds(matches []activation.ActivationResult) []spreading.Seed {
	seeds := make([]spreading.Seed, len(matches))
	for i, m := range matches {
		seeds[i] = spreading.Seed{
			BehaviorID: m.Behavior.ID,
			Activation: spreading.SpecificityToActivation(m.Specificity),
			Source:     spreading.BuildSourceLabel(m.MatchedConditions),
		}
	}
	return seeds
}

// mergeSpreadResults merges spreading engine results back into the activation
// matches slice. Behaviors already present via direct match are kept as-is;
// spread-only behaviors are loaded from the store and appended with Specificity 0
// so the Resolver ranks them below direct matches.
func mergeSpreadResults(ctx context.Context, gs store.GraphStore, matches []activation.ActivationResult, spread []spreading.Result) []activation.ActivationResult {
	// Index existing matches by ID.
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		seen[m.Behavior.ID] = true
	}

	for _, sr := range spread {
		if seen[sr.BehaviorID] {
			continue
		}
		// Load the full behavior node for spread-only results.
		node, err := gs.GetNode(ctx, sr.BehaviorID)
		if err != nil || node == nil {
			continue
		}
		if node.Kind != "behavior" {
			continue
		}
		behavior := learning.NodeToBehavior(*node)
		matches = append(matches, activation.ActivationResult{
			Behavior:    behavior,
			Specificity: 0, // Spread-only: always lower than direct matches in Resolver
		})
		seen[sr.BehaviorID] = true
	}

	return matches
}

// spreadMeta holds spreading activation metadata for a single behavior.
type spreadMeta struct {
	activation float64
	distance   int
	seedSource string
}

// buildSpreadIndex creates a lookup from behavior ID to spreading metadata.
// It uses spreadResults from the engine for post-sigmoid activation values,
// falling back to raw seed values only for behaviors not in the engine output.
func buildSpreadIndex(seeds []spreading.Seed, matches []activation.ActivationResult, spreadResults []spreading.Result) map[string]spreadMeta {
	index := make(map[string]spreadMeta, len(spreadResults))

	// Primary source: engine results with post-propagation activation values.
	for _, sr := range spreadResults {
		index[sr.BehaviorID] = spreadMeta{
			activation: sr.Activation,
			distance:   sr.Distance,
			seedSource: sr.SeedSource,
		}
	}

	// Fallback: seeds not present in engine results (shouldn't happen, but defensive).
	for _, s := range seeds {
		if _, ok := index[s.BehaviorID]; !ok {
			index[s.BehaviorID] = spreadMeta{
				activation: s.Activation,
				distance:   0,
				seedSource: s.Source,
			}
		}
	}

	// Fallback: matched behaviors with no spreading data at all.
	for _, m := range matches {
		if _, ok := index[m.Behavior.ID]; !ok {
			index[m.Behavior.ID] = spreadMeta{
				activation: spreading.SpecificityToActivation(m.Specificity),
				distance:   0,
				seedSource: "direct",
			}
		}
	}

	return index
}

// boostSeedsWithPageRank blends PageRank scores into seed activations.
// The weight parameter controls how much PageRank influences the result:
// seed.Activation = (1-weight)*seed.Activation + weight*pageRank[seed.ID]
func boostSeedsWithPageRank(seeds []spreading.Seed, pageRank map[string]float64, weight float64) []spreading.Seed {
	for i := range seeds {
		if pr, ok := pageRank[seeds[i].BehaviorID]; ok {
			seeds[i].Activation = (1-weight)*seeds[i].Activation + weight*pr
		}
	}
	return seeds
}

// handleFloopDeduplicate implements the floop_deduplicate tool.
func (s *Server) handleFloopDeduplicate(ctx context.Context, req *sdk.CallToolRequest, args FloopDeduplicateInput) (_ *sdk.CallToolResult, _ FloopDeduplicateOutput, retErr error) {
	start := time.Now()
	defer func() {
		auditScope := "local"
		if args.Scope == "global" {
			auditScope = "global"
		}
		s.auditTool("floop_deduplicate", start, retErr, sanitizeToolParams("floop_deduplicate", map[string]interface{}{
			"dry_run": args.DryRun, "threshold": args.Threshold, "scope": args.Scope,
		}), auditScope)
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_deduplicate"); err != nil {
		return nil, FloopDeduplicateOutput{}, err
	}

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

		// Debounced PageRank refresh after graph mutation
		s.debouncedRefreshPageRank()
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

// handleFloopBackup implements the floop_backup tool.
func (s *Server) handleFloopBackup(ctx context.Context, req *sdk.CallToolRequest, args FloopBackupInput) (_ *sdk.CallToolResult, _ FloopBackupOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_backup", start, retErr, sanitizeToolParams("floop_backup", map[string]interface{}{
			"output_path": args.OutputPath,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_backup"); err != nil {
		return nil, FloopBackupOutput{}, err
	}

	outputPath := args.OutputPath
	if outputPath == "" {
		// Default path -- controlled by us, no validation needed
		backupDir, err := backup.DefaultBackupDir()
		if err != nil {
			return nil, FloopBackupOutput{}, fmt.Errorf("failed to get backup directory: %w", err)
		}
		outputPath = backup.GenerateBackupPath(backupDir)
	} else {
		// User-specified path -- validate against allowed directories
		allowedDirs, err := pathutil.DefaultAllowedBackupDirsWithProjectRoot(s.root)
		if err != nil {
			return nil, FloopBackupOutput{}, fmt.Errorf("failed to determine allowed backup dirs: %w", err)
		}
		if err := pathutil.ValidatePath(outputPath, allowedDirs); err != nil {
			return nil, FloopBackupOutput{}, fmt.Errorf("backup path rejected: %w", err)
		}
	}

	result, err := backup.Backup(ctx, s.store, outputPath)
	if err != nil {
		return nil, FloopBackupOutput{}, fmt.Errorf("backup failed: %w", err)
	}

	// Apply retention policy
	backupDir := filepath.Dir(outputPath)
	if _, err := backup.ApplyRetention(backupDir, s.retentionPolicy); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to apply retention: %v\n", err)
	}

	// Get file size for output
	var sizeBytes int64
	if info, err := os.Stat(outputPath); err == nil {
		sizeBytes = info.Size()
	}

	return nil, FloopBackupOutput{
		Path:       outputPath,
		NodeCount:  len(result.Nodes),
		EdgeCount:  len(result.Edges),
		Version:    result.Version,
		Compressed: result.Version == backup.FormatV2,
		SizeBytes:  sizeBytes,
		Message:    fmt.Sprintf("Backup created: %d nodes, %d edges → %s", len(result.Nodes), len(result.Edges), outputPath),
	}, nil
}

// handleFloopRestore implements the floop_restore tool.
func (s *Server) handleFloopRestore(ctx context.Context, req *sdk.CallToolRequest, args FloopRestoreInput) (_ *sdk.CallToolResult, _ FloopRestoreOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_restore", start, retErr, sanitizeToolParams("floop_restore", map[string]interface{}{
			"input_path": args.InputPath, "mode": args.Mode,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_restore"); err != nil {
		return nil, FloopRestoreOutput{}, err
	}

	if args.InputPath == "" {
		return nil, FloopRestoreOutput{}, fmt.Errorf("'input_path' parameter is required")
	}

	// Validate user-supplied path against allowed directories
	allowedDirs, err := pathutil.DefaultAllowedBackupDirsWithProjectRoot(s.root)
	if err != nil {
		return nil, FloopRestoreOutput{}, fmt.Errorf("failed to determine allowed backup dirs: %w", err)
	}
	if err := pathutil.ValidatePath(args.InputPath, allowedDirs); err != nil {
		return nil, FloopRestoreOutput{}, fmt.Errorf("restore path rejected: %w", err)
	}

	mode := backup.RestoreMerge
	if args.Mode == "replace" {
		mode = backup.RestoreReplace
	}

	result, err := backup.Restore(ctx, s.store, args.InputPath, mode)
	if err != nil {
		return nil, FloopRestoreOutput{}, fmt.Errorf("restore failed: %w", err)
	}

	// Debounced PageRank refresh after restore
	s.debouncedRefreshPageRank()

	return nil, FloopRestoreOutput{
		NodesRestored: result.NodesRestored,
		NodesSkipped:  result.NodesSkipped,
		EdgesRestored: result.EdgesRestored,
		EdgesSkipped:  result.EdgesSkipped,
		Message:       fmt.Sprintf("Restore complete: %d nodes restored, %d skipped; %d edges restored, %d skipped", result.NodesRestored, result.NodesSkipped, result.EdgesRestored, result.EdgesSkipped),
	}, nil
}

// validEdgeKinds defines the allowed edge kinds for floop_connect.
var validEdgeKinds = map[string]bool{
	"requires":     true,
	"overrides":    true,
	"conflicts":    true,
	"similar-to":   true,
	"learned-from": true,
}

// handleFloopConnect implements the floop_connect tool.
func (s *Server) handleFloopConnect(ctx context.Context, req *sdk.CallToolRequest, args FloopConnectInput) (_ *sdk.CallToolResult, _ FloopConnectOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_connect", start, retErr, sanitizeToolParams("floop_connect", map[string]interface{}{
			"source": args.Source, "target": args.Target, "kind": args.Kind,
			"weight": args.Weight, "bidirectional": args.Bidirectional,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_connect"); err != nil {
		return nil, FloopConnectOutput{}, err
	}

	// Validate required fields
	if args.Source == "" {
		return nil, FloopConnectOutput{}, fmt.Errorf("'source' parameter is required")
	}
	if args.Target == "" {
		return nil, FloopConnectOutput{}, fmt.Errorf("'target' parameter is required")
	}
	if args.Kind == "" {
		return nil, FloopConnectOutput{}, fmt.Errorf("'kind' parameter is required")
	}

	// Validate kind
	if !validEdgeKinds[args.Kind] {
		return nil, FloopConnectOutput{}, fmt.Errorf("invalid edge kind: %s (must be one of: requires, overrides, conflicts, similar-to, learned-from)", args.Kind)
	}

	// Default weight
	weight := args.Weight
	if weight == 0 {
		weight = 0.8
	}
	if weight <= 0 || weight > 1.0 {
		return nil, FloopConnectOutput{}, fmt.Errorf("weight must be in (0.0, 1.0], got %f", weight)
	}

	// No self-edges
	if args.Source == args.Target {
		return nil, FloopConnectOutput{}, fmt.Errorf("self-edges are not allowed: source and target are both %s", args.Source)
	}

	// Validate source exists
	sourceNode, err := s.store.GetNode(ctx, args.Source)
	if err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to check source node: %w", err)
	}
	if sourceNode == nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("source node not found: %s", args.Source)
	}

	// Validate target exists
	targetNode, err := s.store.GetNode(ctx, args.Target)
	if err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to check target node: %w", err)
	}
	if targetNode == nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("target node not found: %s", args.Target)
	}

	// Check for duplicate edge
	existing, err := s.store.GetEdges(ctx, args.Source, store.DirectionOutbound, args.Kind)
	if err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to check existing edges: %w", err)
	}
	for _, e := range existing {
		if e.Target == args.Target {
			fmt.Fprintf(os.Stderr, "warning: edge %s -[%s]-> %s already exists (weight: %.2f)\n", args.Source, args.Kind, args.Target, e.Weight)
		}
	}

	// Create edge
	now := time.Now()
	edge := store.Edge{
		Source:    args.Source,
		Target:    args.Target,
		Kind:      args.Kind,
		Weight:    weight,
		CreatedAt: now,
	}

	if err := s.store.AddEdge(ctx, edge); err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to add edge: %w", err)
	}

	// Create reverse edge if bidirectional
	if args.Bidirectional {
		reverseEdge := store.Edge{
			Source:    args.Target,
			Target:    args.Source,
			Kind:      args.Kind,
			Weight:    weight,
			CreatedAt: now,
		}
		if err := s.store.AddEdge(ctx, reverseEdge); err != nil {
			return nil, FloopConnectOutput{}, fmt.Errorf("failed to add reverse edge: %w", err)
		}
	}

	// Sync store
	if err := s.store.Sync(ctx); err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to sync store: %w", err)
	}

	// Debounced PageRank refresh after connect
	s.debouncedRefreshPageRank()

	message := fmt.Sprintf("Edge created: %s -[%s (%.2f)]-> %s", args.Source, args.Kind, weight, args.Target)
	if args.Bidirectional {
		message += " (bidirectional)"
	}

	return nil, FloopConnectOutput{
		Source:        args.Source,
		Target:        args.Target,
		Kind:          args.Kind,
		Weight:        weight,
		Bidirectional: args.Bidirectional,
		Message:       message,
	}, nil
}

// handleFloopValidate implements the floop_validate tool.
func (s *Server) handleFloopValidate(ctx context.Context, req *sdk.CallToolRequest, args FloopValidateInput) (_ *sdk.CallToolResult, _ FloopValidateOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_validate", start, retErr, nil, "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_validate"); err != nil {
		return nil, FloopValidateOutput{}, err
	}

	// Check if the store supports validation (MultiGraphStore or SQLiteGraphStore)
	type graphValidator interface {
		ValidateBehaviorGraph(ctx context.Context) ([]store.ValidationError, error)
	}

	validator, ok := s.store.(graphValidator)
	if !ok {
		return nil, FloopValidateOutput{}, fmt.Errorf("store does not support graph validation")
	}

	// Perform validation
	validationErrors, err := validator.ValidateBehaviorGraph(ctx)
	if err != nil {
		return nil, FloopValidateOutput{}, fmt.Errorf("validation failed: %w", err)
	}

	// Convert to output format
	outputErrors := make([]ValidationErrorOutput, len(validationErrors))
	for i, ve := range validationErrors {
		outputErrors[i] = ValidationErrorOutput{
			BehaviorID: ve.BehaviorID,
			Field:      ve.Field,
			RefID:      ve.RefID,
			Issue:      ve.Issue,
		}
	}

	// Build message
	var message string
	if len(validationErrors) == 0 {
		message = "Behavior graph is valid - no issues found"
	} else {
		// Categorize errors
		var dangling, cycles, selfRefs int
		for _, ve := range validationErrors {
			switch ve.Issue {
			case "dangling":
				dangling++
			case "cycle":
				cycles++
			case "self-reference":
				selfRefs++
			}
		}

		parts := []string{}
		if dangling > 0 {
			parts = append(parts, fmt.Sprintf("%d dangling reference(s)", dangling))
		}
		if cycles > 0 {
			parts = append(parts, fmt.Sprintf("%d cycle(s)", cycles))
		}
		if selfRefs > 0 {
			parts = append(parts, fmt.Sprintf("%d self-reference(s)", selfRefs))
		}
		message = fmt.Sprintf("Found %d issue(s): %s", len(validationErrors), strings.Join(parts, ", "))
	}

	return nil, FloopValidateOutput{
		Valid:      len(validationErrors) == 0,
		ErrorCount: len(validationErrors),
		Errors:     outputErrors,
		Message:    message,
	}, nil
}

// handleFloopGraph implements the floop_graph tool.
func (s *Server) handleFloopGraph(ctx context.Context, req *sdk.CallToolRequest, args FloopGraphInput) (_ *sdk.CallToolResult, _ FloopGraphOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_graph", start, retErr, sanitizeToolParams("floop_graph", map[string]interface{}{
			"format": args.Format,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_graph"); err != nil {
		return nil, FloopGraphOutput{}, err
	}

	format := args.Format
	if format == "" {
		format = "json"
	}

	switch visualization.Format(format) {
	case visualization.FormatDOT:
		dot, err := visualization.RenderDOT(ctx, s.store)
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("render DOT: %w", err)
		}
		// Count nodes for output metadata
		nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("query nodes: %w", err)
		}
		return nil, FloopGraphOutput{
			Format:    "dot",
			Graph:     dot,
			NodeCount: len(nodes),
		}, nil

	case visualization.FormatJSON:
		result, err := visualization.RenderJSON(ctx, s.store)
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("render JSON: %w", err)
		}
		nodeCount, _ := result["node_count"].(int)
		edgeCount, _ := result["edge_count"].(int)
		return nil, FloopGraphOutput{
			Format:    "json",
			Graph:     result,
			NodeCount: nodeCount,
			EdgeCount: edgeCount,
		}, nil

	case visualization.FormatHTML:
		s.pageRankMu.RLock()
		pageRank := s.pageRankCache
		s.pageRankMu.RUnlock()

		enrichment := &visualization.EnrichmentData{PageRank: pageRank}
		htmlBytes, err := visualization.RenderHTML(ctx, s.store, enrichment)
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("render HTML: %w", err)
		}

		nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("query nodes: %w", err)
		}
		edges, err := visualization.CollectEdges(ctx, s.store, nodes)
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("collect edges: %w", err)
		}

		return nil, FloopGraphOutput{
			Format:    "html",
			Graph:     string(htmlBytes),
			NodeCount: len(nodes),
			EdgeCount: len(edges),
		}, nil

	default:
		return nil, FloopGraphOutput{}, fmt.Errorf("unsupported format %q (use 'dot', 'json', or 'html')", format)
	}
}

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
