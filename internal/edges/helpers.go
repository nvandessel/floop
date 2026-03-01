// Package edges provides shared helpers for edge derivation and behavior
// similarity computation. These functions are extracted from the CLI commands
// to enable reuse in pack installation and other automated workflows.
package edges

import (
	"context"

	"github.com/nvandessel/floop/internal/dedup"
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// LoadBehaviorsFromStore loads all active behaviors (kind == "behavior") from a
// graph store, excluding forgotten, deprecated, and merged behaviors.
// Extracted from cmd/floop/cmd_dedup.go:loadBehaviorsFromStore.
func LoadBehaviorsFromStore(ctx context.Context, graphStore store.GraphStore) ([]models.Behavior, error) {
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": string(store.NodeKindBehavior)})
	if err != nil {
		return nil, err
	}

	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		b := models.NodeToBehavior(node)
		behaviors = append(behaviors, b)
	}

	return behaviors, nil
}

// ComputeBehaviorSimilarity calculates similarity between two behaviors.
// Delegates to the unified dedup.ComputeSimilarity function.
// Extracted from cmd/floop/cmd_dedup.go:computeBehaviorSimilarity.
func ComputeBehaviorSimilarity(a, b *models.Behavior, llmClient llm.Client, useLLM bool, cache *dedup.EmbeddingCache) float64 {
	result := dedup.ComputeSimilarity(a, b, dedup.SimilarityConfig{
		UseLLM:         useLLM,
		LLMClient:      llmClient,
		EmbeddingCache: cache,
	})
	return result.Score
}
