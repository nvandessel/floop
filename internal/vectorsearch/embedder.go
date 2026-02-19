package vectorsearch

import (
	"context"
	"fmt"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// EmbedFunc is a function that returns a dense vector embedding for the given text.
// This matches the signature of llm.EmbeddingComparer.Embed.
type EmbedFunc func(ctx context.Context, text string) ([]float32, error)

// NodeGetter provides access to individual nodes. This is a subset of store.GraphStore
// needed by BackfillMissing to load behavior content.
type NodeGetter interface {
	store.EmbeddingStore
	GetNode(ctx context.Context, id string) (*store.Node, error)
}

// Embedder bridges an embedding function with the embedding store.
// It handles nomic-embed-text task prefixes and orchestrates embed + store operations.
type Embedder struct {
	embed     EmbedFunc
	modelName string
}

// NewEmbedder creates an Embedder from an embed function and model name.
// Returns nil if embedFn is nil.
func NewEmbedder(embedFn EmbedFunc, modelName string) *Embedder {
	if embedFn == nil {
		return nil
	}
	return &Embedder{
		embed:     embedFn,
		modelName: modelName,
	}
}

// Available returns true if the embedder is ready to produce embeddings.
func (e *Embedder) Available() bool {
	return e != nil && e.embed != nil
}

// EmbedAndStore embeds the given text with a search_document prefix and stores
// the resulting vector in the embedding store.
func (e *Embedder) EmbedAndStore(ctx context.Context, es store.EmbeddingStore, behaviorID, text string) error {
	prefixed := "search_document: " + text
	vec, err := e.embed(ctx, prefixed)
	if err != nil {
		return fmt.Errorf("embed behavior %s: %w", behaviorID, err)
	}
	return es.StoreEmbedding(ctx, behaviorID, vec, e.modelName)
}

// EmbedQuery embeds a context query with a search_query prefix for retrieval.
func (e *Embedder) EmbedQuery(ctx context.Context, queryText string) ([]float32, error) {
	prefixed := "search_query: " + queryText
	return e.embed(ctx, prefixed)
}

// BackfillMissing embeds all behaviors that don't yet have embedding vectors.
// Returns the number of behaviors successfully embedded.
func (e *Embedder) BackfillMissing(ctx context.Context, ns NodeGetter) (int, error) {
	ids, err := ns.GetBehaviorIDsWithoutEmbeddings(ctx)
	if err != nil {
		return 0, fmt.Errorf("get unembedded behaviors: %w", err)
	}

	count := 0
	for _, id := range ids {
		node, err := ns.GetNode(ctx, id)
		if err != nil {
			continue // skip missing nodes
		}

		text, ok := extractCanonical(node)
		if !ok {
			continue // skip behaviors without canonical text
		}

		if err := e.EmbedAndStore(ctx, ns, id, text); err != nil {
			continue // skip failures, best-effort backfill
		}
		count++
	}

	return count, nil
}

// extractCanonical extracts the canonical text from a behavior node's content map.
func extractCanonical(node *store.Node) (string, bool) {
	if node == nil || node.Content == nil {
		return "", false
	}
	val, ok := node.Content["canonical"]
	if !ok {
		return "", false
	}
	text, ok := val.(string)
	if !ok || text == "" {
		return "", false
	}
	return text, true
}
