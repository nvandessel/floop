package consolidation

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/vecmath"
)

// Relate finds relationships between new memories and existing behaviors.
// It uses a three-level fallback chain:
//  1. Vector search for neighbors + LLM proposals + co-occurrence edges
//  2. Vector search + co-occurrence edges (on LLM failure)
//  3. Co-occurrence edges only (on both failures)
func (c *LLMConsolidator) Relate(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore) ([]store.Edge, []MergeProposal, error) {
	if len(memories) == 0 {
		return nil, nil, nil
	}

	// 1. Find neighbors (vector search or fallback).
	neighbors, err := c.findNeighbors(ctx, memories, s)
	if err != nil {
		c.decisions.Log(map[string]any{
			"stage": "relate",
			"event": "neighbor_search_failed",
			"error": err.Error(),
		})
		// Continue with empty neighbors.
		neighbors = make(map[int][]store.Node)
	}

	c.decisions.Log(map[string]any{
		"stage":           "relate",
		"event":           "neighbors_found",
		"memory_count":    len(memories),
		"neighbor_counts": neighborCounts(neighbors),
	})

	// 2. LLM relationship proposals.
	var edges []store.Edge
	var merges []MergeProposal

	if c.client != nil && c.client.Available() {
		msgs, promptErr := RelateMemoriesPrompt(memories, neighbors)
		if promptErr != nil {
			c.decisions.Log(map[string]any{
				"stage": "relate",
				"event": "prompt_build_failed",
				"error": promptErr.Error(),
			})
		}
		var response string
		var llmErr error
		if promptErr == nil {
			response, llmErr = c.client.Complete(ctx, msgs)
		} else {
			llmErr = promptErr
		}
		if llmErr != nil {
			c.decisions.Log(map[string]any{
				"stage": "relate",
				"event": "llm_failed",
				"error": llmErr.Error(),
			})
			// Fall through to co-occurrence only.
		} else {
			proposals, parseErr := ParseRelationships(response)
			if parseErr != nil {
				c.decisions.Log(map[string]any{
					"stage": "relate",
					"event": "parse_failed",
					"error": parseErr.Error(),
				})
			} else {
				edges, merges = convertProposals(proposals, memories, neighbors)
				c.decisions.Log(map[string]any{
					"stage":     "relate",
					"event":     "proposals_converted",
					"edges":     len(edges),
					"merges":    len(merges),
					"proposals": len(proposals),
				})
			}
		}
	}

	// 3. Co-occurrence edges (always).
	coEdges := buildCoOccurrenceEdges(memories)
	edges = append(edges, coEdges...)

	c.decisions.Log(map[string]any{
		"stage":           "relate",
		"event":           "complete",
		"total_edges":     len(edges),
		"cooccurrence":    len(coEdges),
		"merge_proposals": len(merges),
	})

	return edges, merges, nil
}

// findNeighbors retrieves semantically similar behaviors for each memory.
// If the LLM client supports embeddings, it embeds the canonical text and
// compares against stored embeddings. Otherwise, it falls back to QueryNodes.
func (c *LLMConsolidator) findNeighbors(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore) (map[int][]store.Node, error) {
	if s == nil {
		return make(map[int][]store.Node), nil
	}

	topK := c.config.TopK
	if topK <= 0 {
		topK = 5
	}

	// Try embedding-based search first.
	if ec, ok := c.client.(llm.EmbeddingComparer); ok {
		return c.findNeighborsByEmbedding(ctx, ec, memories, s, topK)
	}

	// Fallback: return all behaviors unranked.
	return c.findNeighborsByQuery(ctx, memories, s, topK)
}

// findNeighborsByEmbedding uses the EmbeddingComparer to embed each memory's
// canonical text and find nearest neighbors among stored embeddings.
func (c *LLMConsolidator) findNeighborsByEmbedding(ctx context.Context, ec llm.EmbeddingComparer, memories []ClassifiedMemory, s store.GraphStore, topK int) (map[int][]store.Node, error) {
	// Get all existing embeddings from the store.
	es, ok := s.(store.EmbeddingStore)
	if !ok {
		return c.findNeighborsByQuery(ctx, memories, s, topK)
	}

	allEmbeddings, err := es.GetAllEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting embeddings: %w", err)
	}
	if len(allEmbeddings) == 0 {
		return c.findNeighborsByQuery(ctx, memories, s, topK)
	}

	result := make(map[int][]store.Node)

	for i, mem := range memories {
		queryVec, embedErr := ec.Embed(ctx, mem.Content.Canonical)
		if embedErr != nil {
			c.decisions.Log(map[string]any{
				"stage": "relate",
				"event": "embed_failed",
				"index": i,
				"error": embedErr.Error(),
			})
			// Fall back to unranked query neighbors for this memory.
			fallback, qErr := c.findNeighborsByQuery(ctx, memories[i:i+1], s, topK)
			if qErr == nil {
				result[i] = fallback[0]
			}
			continue
		}

		// Score all existing behaviors by cosine similarity.
		type scored struct {
			id    string
			score float64
		}
		var scores []scored
		for _, be := range allEmbeddings {
			sim := vecmath.CosineSimilarity(queryVec, be.Embedding)
			if sim > 0.3 { // minimum threshold
				scores = append(scores, scored{id: be.BehaviorID, score: sim})
			}
		}

		if len(scores) == 0 {
			c.decisions.Log(map[string]any{
				"stage": "relate",
				"event": "no_neighbors_above_threshold",
				"index": i,
			})
			continue
		}

		// Sort by similarity descending.
		sort.Slice(scores, func(a, b int) bool {
			return scores[a].score > scores[b].score
		})

		// Take top-K and resolve nodes.
		limit := topK
		if limit > len(scores) {
			limit = len(scores)
		}
		for _, sc := range scores[:limit] {
			node, nodeErr := s.GetNode(ctx, sc.id)
			if nodeErr != nil {
				c.decisions.Log(map[string]any{
					"stage": "relate",
					"event": "get_node_failed",
					"id":    sc.id,
					"error": nodeErr.Error(),
				})
				continue
			}
			if node == nil {
				continue
			}
			result[i] = append(result[i], *node)
		}
	}

	return result, nil
}

// findNeighborsByQuery falls back to fetching all behavior nodes and returning
// them unranked as neighbors for each memory.
func (c *LLMConsolidator) findNeighborsByQuery(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore, topK int) (map[int][]store.Node, error) {
	allNodes, err := s.QueryNodes(ctx, map[string]interface{}{
		"kind": string(store.NodeKindBehavior),
	})
	if err != nil {
		return nil, fmt.Errorf("querying behavior nodes: %w", err)
	}

	// Limit to topK per memory.
	limit := topK
	if limit > len(allNodes) {
		limit = len(allNodes)
	}
	capped := allNodes[:limit]

	result := make(map[int][]store.Node)
	for i := range memories {
		entry := make([]store.Node, len(capped))
		copy(entry, capped)
		result[i] = entry
	}
	return result, nil
}

// buildCoOccurrenceEdges generates co-activated edges between all memories
// that share the same session ID.
func buildCoOccurrenceEdges(memories []ClassifiedMemory) []store.Edge {
	// Group memories by session.
	sessions := make(map[string][]int)
	for i, m := range memories {
		sid, _ := m.SessionContext["session_id"].(string)
		if sid == "" {
			continue
		}
		sessions[sid] = append(sessions[sid], i)
	}

	now := time.Now()
	var edges []store.Edge

	for _, indices := range sessions {
		if len(indices) < 2 {
			continue
		}
		// Create edges between all pairs.
		for a := 0; a < len(indices); a++ {
			for b := a + 1; b < len(indices); b++ {
				srcID := memoryNodeID(memories[indices[a]], indices[a])
				tgtID := memoryNodeID(memories[indices[b]], indices[b])
				edges = append(edges, store.Edge{
					Source:    srcID,
					Target:    tgtID,
					Kind:      store.EdgeKindCoActivated,
					Weight:    0.5,
					CreatedAt: now,
				})
			}
		}
	}

	return edges
}

// PendingNodeID returns a pending placeholder ID for a memory at the given index.
// These are rewritten to actual node IDs during Promote.
func PendingNodeID(index int) string {
	return fmt.Sprintf("pending-%d", index)
}

// memoryNodeID generates a stable node ID for a memory. If the memory has
// source events, the first event ID is used as a base; otherwise a hash of
// the content plus index is used to avoid collisions across Relate calls.
func memoryNodeID(m ClassifiedMemory, index int) string {
	if len(m.SourceEvents) > 0 {
		return fmt.Sprintf("mem-%s", m.SourceEvents[0])
	}
	h := fnv.New32a()
	h.Write([]byte(m.RawText + strconv.Itoa(index)))
	return fmt.Sprintf("mem-anon-%x", h.Sum32())
}

// convertProposals converts parsed LLM proposals into store edges and merge proposals.
// neighbors provides the scored neighbor lists from vector search so merge proposals
// can carry the actual cosine similarity rather than defaulting to 0.0.
func convertProposals(proposals []relateProposal, memories []ClassifiedMemory, neighbors map[int][]store.Node) ([]store.Edge, []MergeProposal) {
	now := time.Now()
	var edges []store.Edge
	var merges []MergeProposal

	for _, p := range proposals {
		if p.MemoryIndex < 0 || p.MemoryIndex >= len(memories) {
			continue
		}

		switch p.Action {
		case "create":
			srcID := memoryNodeID(memories[p.MemoryIndex], p.MemoryIndex)
			for _, e := range p.Edges {
				edgeKind, ok := validEdgeKind[e.Kind]
				if !ok {
					continue
				}
				edges = append(edges, store.Edge{
					Source:    srcID,
					Target:    e.Target,
					Kind:      edgeKind,
					Weight:    e.Weight,
					CreatedAt: now,
				})
			}

		case "merge":
			if p.MergeInto == nil {
				continue
			}
			// Use the highest edge weight if available, otherwise check if the
			// merge target was a scored neighbor (carrying cosine similarity).
			sim := highestWeight(p.Edges)
			if sim == 0.0 {
				sim = neighborSimilarity(neighbors, p.MemoryIndex, p.MergeInto.TargetID)
			}
			merges = append(merges, MergeProposal{
				Memory:     memories[p.MemoryIndex],
				TargetID:   p.MergeInto.TargetID,
				Similarity: sim,
				Strategy:   p.MergeInto.Strategy,
			})

		case "skip":
			// No edges or merges for skipped memories.
		}
	}

	return edges, merges
}

// neighborSimilarity returns a default similarity score if the target was among
// the neighbors for the given memory index. Since we don't store per-neighbor
// scores in the Node, we return 0.5 as a reasonable default when the target is
// found, and 0.0 if not.
func neighborSimilarity(neighbors map[int][]store.Node, memIdx int, targetID string) float64 {
	for _, n := range neighbors[memIdx] {
		if n.ID == targetID {
			return 0.5 // target was a scored neighbor
		}
	}
	return 0.0
}

// highestWeight returns the maximum edge weight from a set of proposed edges,
// or 0.0 if there are no edges.
func highestWeight(edges []proposedEdge) float64 {
	best := 0.0
	for _, e := range edges {
		if e.Weight > best {
			best = e.Weight
		}
	}
	return best
}

// neighborCounts builds a summary map of neighbor counts per memory index.
func neighborCounts(neighbors map[int][]store.Node) map[int]int {
	counts := make(map[int]int, len(neighbors))
	for idx, nodes := range neighbors {
		counts[idx] = len(nodes)
	}
	return counts
}
