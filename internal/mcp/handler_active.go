package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/activation"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/ratelimit"
	"github.com/nvandessel/floop/internal/sanitize"
	"github.com/nvandessel/floop/internal/spreading"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/tiering"
)

// handleFloopActive implements the floop_active tool.
func (s *Server) handleFloopActive(ctx context.Context, req *sdk.CallToolRequest, args FloopActiveInput) (_ *sdk.CallToolResult, _ FloopActiveOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_active", start, retErr, sanitizeToolParams("floop_active", map[string]interface{}{
			"file": args.File, "task": args.Task, "language": args.Language,
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

	if args.Language != "" {
		ctxBuilder.WithLanguage(sanitize.SanitizeBehaviorContent(args.Language))
	}

	ctxBuilder.WithRepoRoot(s.root)

	actCtx := ctxBuilder.Build()

	// Load behaviors — vector pre-filter when embedder is available, else load all
	var (
		nodes []store.Node
		err   error
	)
	if s.embedder != nil && s.embedder.Available() {
		nodes, err = vectorRetrieve(ctx, s.embedder, s.vectorIndex, s.store, actCtx, vectorRetrieveTopK)
		if err != nil {
			nodes = nil // distinguish error from empty results
			s.logger.Warn("vector retrieval failed, falling back to full scan", "error", err)
		}
	}
	if nodes == nil {
		nodes, err = s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
		if err != nil {
			return nil, FloopActiveOutput{}, fmt.Errorf("failed to query behaviors: %w", err)
		}
	}

	// Convert nodes to behaviors
	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		behavior := models.NodeToBehavior(node)
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
		spreadResults, err = s.activator.Activate(ctx, seeds)
		if err != nil {
			s.logger.Warn("spreading activation failed", "error", err)
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
					s.logger.Warn("edge timestamp failed", "error", err)
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
				if s.applyHebbianUpdates(context.Background(), pairs, s.hebbianConfig) {
					if err := s.store.Sync(context.Background()); err != nil {
						s.logger.Warn("hebbian sync after edge update failed", "error", err)
					}
				}
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
					s.logger.Warn("activation hit recording failed", "behavior_id", b.ID, "error", err)
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
					s.logger.Warn("implicit confirmation recording failed", "behavior_id", id, "error", err)
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

// matchesToSeeds converts activation results to spreading seeds.
func matchesToSeeds(matches []activation.ActivationResult) []spreading.Seed {
	seeds := make([]spreading.Seed, len(matches))
	for i, m := range matches {
		seeds[i] = spreading.Seed{
			BehaviorID: m.Behavior.ID,
			Activation: spreading.MatchScoreToActivation(len(m.Behavior.When), m.MatchScore),
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
		behavior := models.NodeToBehavior(*node)
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
				activation: spreading.MatchScoreToActivation(len(m.Behavior.When), m.MatchScore),
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

// behaviorContentToMap converts BehaviorContent to a map for JSON serialization.
func behaviorContentToMap(content models.BehaviorContent) map[string]interface{} {
	m := make(map[string]interface{})
	m["canonical"] = content.Canonical
	if content.Structured != nil && len(content.Structured) > 0 {
		m["structured"] = content.Structured
	}
	if len(content.Tags) > 0 {
		m["tags"] = content.Tags
	}
	return m
}
