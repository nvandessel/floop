package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/activation"
	"github.com/nvandessel/floop/internal/backup"
	"github.com/nvandessel/floop/internal/constants"
	"github.com/nvandessel/floop/internal/dedup"
	"github.com/nvandessel/floop/internal/learning"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/ratelimit"
	"github.com/nvandessel/floop/internal/sanitize"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/tagging"
)

// handleFloopLearn implements the floop_learn tool.
func (s *Server) handleFloopLearn(ctx context.Context, req *sdk.CallToolRequest, args FloopLearnInput) (_ *sdk.CallToolResult, _ FloopLearnOutput, retErr error) {
	start := time.Now()
	var auditScope string
	defer func() {
		if auditScope == "" {
			auditScope = "local" // fallback if error before scope is determined
		}
		s.auditTool("floop_learn", start, retErr, sanitizeToolParams("floop_learn", map[string]interface{}{
			"wrong": args.Wrong, "right": args.Right, "file": args.File, "task": args.Task, "language": args.Language, "auto_merge": args.AutoMerge, "tags": args.Tags,
		}), auditScope)
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_learn"); err != nil {
		return nil, FloopLearnOutput{}, err
	}

	// Validate required parameters
	if args.Right == "" {
		return nil, FloopLearnOutput{}, fmt.Errorf("'right' parameter is required")
	}

	// Sanitize inputs at the handler level as defense-in-depth.
	// The extraction layer also sanitizes, but this protects against
	// any code path that bypasses the learning loop.
	if args.Wrong != "" {
		args.Wrong = sanitize.SanitizeBehaviorContent(args.Wrong)
	}
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

	if args.Language != "" {
		ctxBuilder.WithLanguage(sanitize.SanitizeBehaviorContent(args.Language))
	}

	ctxBuilder.WithRepoRoot(s.root)
	ctxSnapshot := ctxBuilder.Build()

	// Create correction with nanosecond-precision ID for uniqueness
	now := time.Now()

	// Silently truncate extra tags to MaxExtraTags
	extraTags := args.Tags
	if len(extraTags) > tagging.MaxExtraTags {
		extraTags = extraTags[:tagging.MaxExtraTags]
	}

	correction := models.Correction{
		ID:              fmt.Sprintf("c-%d", now.UnixNano()),
		Timestamp:       now,
		Context:         ctxSnapshot,
		AgentAction:     args.Wrong,
		CorrectedAction: args.Right,
		Corrector:       "mcp-client",
		ExtraTags:       extraTags,
		Processed:       false,
	}

	// Configure learning loop - auto-merge is ON by default
	// This prevents duplicate behaviors from accumulating
	loopConfig := &learning.LearningLoopConfig{
		AutoAcceptThreshold: constants.DefaultAutoAcceptThreshold,
		AutoMerge:           true, // Always deduplicate
		AutoMergeThreshold:  constants.DefaultAutoMergeThreshold,
	}

	// Create deduplicator for automatic merging
	merger := dedup.NewBehaviorMerger(dedup.MergerConfig{})
	dedupConfig := dedup.DeduplicatorConfig{
		SimilarityThreshold: constants.DefaultAutoMergeThreshold,
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

	// Background: embed the new/merged behavior for vector retrieval
	if s.embedder != nil && s.embedder.Available() && learningResult.CandidateBehavior.ID != "" {
		bid := learningResult.CandidateBehavior.ID
		text := learningResult.CandidateBehavior.Content.Canonical
		if text != "" {
			s.runBackground("embed-new-behavior", func() {
				if es, ok := s.store.(store.EmbeddingStore); ok {
					vec, err := s.embedder.EmbedAndStore(context.Background(), es, bid, text)
					if err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to embed behavior %s: %v\n", bid, err)
					} else if s.vectorIndex != nil {
						if err := s.vectorIndex.Add(context.Background(), bid, vec); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to add behavior %s to vector index: %v\n", bid, err)
						}
					}
				}
			})
		}
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
