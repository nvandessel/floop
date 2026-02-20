package dedup

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/logging"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/similarity"
	"github.com/nvandessel/feedback-loop/internal/vecmath"
)

// SimilarityConfig configures a single similarity computation.
type SimilarityConfig struct {
	UseLLM              bool
	LLMClient           llm.Client
	SimilarityThreshold float64
	EmbeddingThreshold  float64 // 0 = use SimilarityThreshold for embeddings too
	Logger              *slog.Logger
	Decisions           *logging.DecisionLogger
	EmbeddingCache      *EmbeddingCache // nil = no caching
}

// SimilarityResult holds the score and method used for a similarity computation.
type SimilarityResult struct {
	Score  float64
	Method string // "embedding", "llm", "jaccard"
}

// ComputeSimilarity calculates similarity between two behaviors using a 3-tier
// fallback chain: embedding → LLM → Jaccard. This is the single source of truth
// for similarity computation across all dedup paths (store, cross-store, CLI).
func ComputeSimilarity(a, b *models.Behavior, cfg SimilarityConfig) SimilarityResult {
	if cfg.UseLLM && cfg.LLMClient != nil && cfg.LLMClient.Available() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Prefer embedding-based comparison if supported
		if ec, ok := cfg.LLMClient.(llm.EmbeddingComparer); ok {
			score, err := computeEmbeddingSimilarity(ctx, ec, a, b, cfg.EmbeddingCache)
			if err == nil {
				logSimilarity(a, b, score, "embedding", cfg)
				return SimilarityResult{Score: score, Method: "embedding"}
			}
			// Fall through on error
		}

		// Try full LLM comparison
		result, err := cfg.LLMClient.CompareBehaviors(ctx, a, b)
		if err == nil && result != nil {
			logSimilarity(a, b, result.SemanticSimilarity, "llm", cfg)
			return SimilarityResult{Score: result.SemanticSimilarity, Method: "llm"}
		}
		// Fall through to Jaccard on error
		if cfg.Logger != nil {
			cfg.Logger.Debug("LLM comparison failed, falling back to jaccard", "error", err)
		}
	}

	// Fallback: weighted Jaccard similarity with tag enhancement
	whenOverlap := similarity.ComputeWhenOverlap(a.When, b.When)
	contentSim := similarity.ComputeContentSimilarity(a.Content.Canonical, b.Content.Canonical)
	tagSim := similarity.ComputeTagSimilarity(a.Content.Tags, b.Content.Tags)
	score := similarity.WeightedScoreWithTags(whenOverlap, contentSim, tagSim)

	logSimilarity(a, b, score, "jaccard", cfg)
	return SimilarityResult{Score: score, Method: "jaccard"}
}

// computeEmbeddingSimilarity uses the EmbeddingCache if available, otherwise
// delegates to CompareEmbeddings directly.
func computeEmbeddingSimilarity(ctx context.Context, ec llm.EmbeddingComparer, a, b *models.Behavior, cache *EmbeddingCache) (float64, error) {
	if cache != nil {
		vecA, err := cache.GetOrCompute(ctx, ec, a.Content.Canonical)
		if err != nil {
			return 0, err
		}
		vecB, err := cache.GetOrCompute(ctx, ec, b.Content.Canonical)
		if err != nil {
			return 0, err
		}
		return vecmath.CosineSimilarity(vecA, vecB), nil
	}
	return ec.CompareEmbeddings(ctx, a.Content.Canonical, b.Content.Canonical)
}

// logSimilarity logs a similarity computation to the structured logger and decision logger.
func logSimilarity(a, b *models.Behavior, score float64, method string, cfg SimilarityConfig) {
	threshold := cfg.SimilarityThreshold
	if method == "embedding" && cfg.EmbeddingThreshold > 0 {
		threshold = cfg.EmbeddingThreshold
	}
	isDup := score >= threshold

	if cfg.Logger != nil {
		cfg.Logger.Debug("similarity computed",
			"behavior_a", a.ID, "behavior_b", b.ID,
			"score", score, "method", method)
	}
	if cfg.Decisions != nil {
		cfg.Decisions.Log(map[string]any{
			"event":        "similarity_computed",
			"behavior_a":   a.ID,
			"behavior_b":   b.ID,
			"score":        score,
			"method":       method,
			"threshold":    threshold,
			"is_duplicate": isDup,
		})
	}
}

// EmbeddingCache caches embedding vectors by text to avoid redundant computation
// during batch pairwise comparisons.
type EmbeddingCache struct {
	mu    sync.Mutex
	cache map[string][]float32
}

// NewEmbeddingCache creates a new empty EmbeddingCache.
func NewEmbeddingCache() *EmbeddingCache {
	return &EmbeddingCache{
		cache: make(map[string][]float32),
	}
}

// GetOrCompute returns the cached embedding for text, or computes and caches it.
func (c *EmbeddingCache) GetOrCompute(ctx context.Context, ec llm.EmbeddingComparer, text string) ([]float32, error) {
	c.mu.Lock()
	if vec, ok := c.cache[text]; ok {
		c.mu.Unlock()
		return vec, nil
	}
	c.mu.Unlock()

	vec, err := ec.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[text] = vec
	c.mu.Unlock()

	return vec, nil
}
