package mcp

import (
	"context"
	"fmt"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/vectorsearch"
)

// vectorRetrieveTopK is the default number of candidates returned by vector search.
const vectorRetrieveTopK = 50

// vectorRetrieve uses the embedder to find semantically relevant behaviors via
// vector similarity, then appends any behaviors that don't yet have embeddings
// (safety net during migration). Returns the combined node set.
func vectorRetrieve(ctx context.Context, embedder *vectorsearch.Embedder, gs store.GraphStore, actCtx models.ContextSnapshot, topK int) ([]store.Node, error) {
	es, ok := gs.(store.EmbeddingStore)
	if !ok {
		return nil, fmt.Errorf("store does not implement EmbeddingStore")
	}

	// 1. Compose and embed the context query
	queryText := vectorsearch.ComposeContextQuery(actCtx)
	queryVec, err := embedder.EmbedQuery(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("embedding context query: %w", err)
	}

	// 2. Get all stored embeddings
	allEmbeddings, err := es.GetAllEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading embeddings: %w", err)
	}

	// 3. Brute-force kNN
	results := vectorsearch.BruteForceSearch(queryVec, allEmbeddings, topK)

	// 4. Load matched nodes
	seen := make(map[string]bool, len(results))
	nodes := make([]store.Node, 0, len(results))
	for _, r := range results {
		node, err := gs.GetNode(ctx, r.BehaviorID)
		if err != nil || node == nil {
			continue
		}
		nodes = append(nodes, *node)
		seen[r.BehaviorID] = true
	}

	// 5. Include all behaviors without embeddings (no silent drops during migration)
	unembedded, err := es.GetBehaviorIDsWithoutEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading unembedded behavior IDs: %w", err)
	}
	for _, id := range unembedded {
		if seen[id] {
			continue
		}
		node, err := gs.GetNode(ctx, id)
		if err != nil || node == nil {
			continue
		}
		nodes = append(nodes, *node)
		seen[id] = true
	}

	return nodes, nil
}
