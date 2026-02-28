package mcp

import (
	"context"
	"fmt"
	"os"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/constants"
	"github.com/nvandessel/floop/internal/dedup"
	"github.com/nvandessel/floop/internal/ratelimit"
	"github.com/nvandessel/floop/internal/store"
)

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
		threshold = constants.DefaultAutoMergeThreshold
	}

	scope := constants.Scope(args.Scope)
	if args.Scope == "" {
		scope = constants.ScopeBoth
	}

	// Validate scope
	if !scope.Valid() {
		return nil, FloopDeduplicateOutput{}, fmt.Errorf("invalid scope: %s (must be 'local', 'global', or 'both')", args.Scope)
	}

	// Configure deduplicator with LLM support when available
	useLLM := s.llmClient != nil && s.llmClient.Available()
	dedupConfig := dedup.DeduplicatorConfig{
		SimilarityThreshold: threshold,
		EmbeddingThreshold:  constants.DefaultEmbeddingDedupThreshold,
		AutoMerge:           !args.DryRun,
		UseLLM:              useLLM,
	}

	merger := dedup.NewBehaviorMerger(dedup.MergerConfig{
		UseLLM:    useLLM,
		LLMClient: s.llmClient,
	})

	var deduplicator *dedup.StoreDeduplicator
	if useLLM {
		deduplicator = dedup.NewStoreDeduplicatorWithLLM(s.store, merger, dedupConfig, s.llmClient)
	} else {
		deduplicator = dedup.NewStoreDeduplicator(s.store, merger, dedupConfig)
	}

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

		// Embed merged behaviors for vector retrieval
		if report.MergesPerformed > 0 && s.embedder != nil && s.embedder.Available() {
			for _, merged := range report.MergedBehaviors {
				bid := merged.ID
				text := merged.Content.Canonical
				if text != "" {
					s.runBackground("embed-merged-behavior", func() {
						if es, ok := s.store.(store.EmbeddingStore); ok {
							vec, err := s.embedder.EmbedAndStore(context.Background(), es, bid, text)
							if err != nil {
								fmt.Fprintf(os.Stderr, "warning: failed to embed merged behavior %s: %v\n", bid, err)
							} else if s.vectorIndex != nil {
								if err := s.vectorIndex.Add(context.Background(), bid, vec); err != nil {
									fmt.Fprintf(os.Stderr, "warning: failed to add merged behavior %s to vector index: %v\n", bid, err)
								}
							}
						}
					})
				}
			}
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
