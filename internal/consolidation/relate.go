package consolidation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/vecmath"
)

// scoredNode pairs a store.Node with its cosine similarity score from vector search.
// Nodes retrieved via the unranked fallback path carry a Score of 0.0.
type scoredNode struct {
	Node  store.Node
	Score float64
}

// Relate finds relationships between new memories and existing behaviors.
// It uses a three-level fallback chain:
//  1. Vector search for neighbors + LLM proposals + co-occurrence edges
//  2. Vector search + co-occurrence edges (on LLM failure)
//  3. Co-occurrence edges only (on both failures)
func (c *LLMConsolidator) Relate(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore) ([]store.Edge, []MergeProposal, []int, error) {
	if len(memories) == 0 {
		return nil, nil, nil, nil
	}

	// 1. Find neighbors (vector search or fallback).
	neighbors, err := c.findNeighbors(ctx, memories, s)
	if err != nil {
		c.logDecision(map[string]any{
			"stage": "relate",
			"event": "neighbor_search_failed",
			"error": err.Error(),
		})
		// Continue with empty neighbors.
		neighbors = make(map[int][]scoredNode)
	}

	c.logDecision(map[string]any{
		"stage":           "relate",
		"event":           "neighbors_found",
		"memory_count":    len(memories),
		"neighbor_counts": neighborCounts(neighbors),
	})

	// 2. LLM relationship proposals.
	var edges []store.Edge
	var merges []MergeProposal
	var skips []int

	if c.client != nil && c.client.Available() {
		msgs, promptErr := RelateMemoriesPrompt(memories, neighbors)
		if promptErr != nil {
			c.logDecision(map[string]any{
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
			c.logDecision(map[string]any{
				"stage": "relate",
				"event": "llm_failed",
				"error": llmErr.Error(),
			})
			// Fall through to co-occurrence only.
		} else {
			proposals, parseErr := ParseRelationships(response)
			if parseErr != nil {
				c.logDecision(map[string]any{
					"stage": "relate",
					"event": "parse_failed",
					"error": parseErr.Error(),
				})
			} else {
				edges, merges, skips = convertProposals(proposals, memories, neighbors)
				c.logDecision(map[string]any{
					"stage":     "relate",
					"event":     "proposals_converted",
					"edges":     len(edges),
					"merges":    len(merges),
					"skips":     len(skips),
					"proposals": len(proposals),
				})
			}
		}
	}

	// 3. Co-occurrence edges (always).
	coEdges := buildCoOccurrenceEdges(memories)
	edges = append(edges, coEdges...)

	c.logDecision(map[string]any{
		"stage":           "relate",
		"event":           "complete",
		"total_edges":     len(edges),
		"cooccurrence":    len(coEdges),
		"merge_proposals": len(merges),
		"skips":           len(skips),
	})

	return edges, merges, skips, nil
}

// findNeighbors retrieves semantically similar behaviors for each memory.
// If the LLM client supports embeddings, it embeds the canonical text and
// compares against stored embeddings. Otherwise, it falls back to QueryNodes.
func (c *LLMConsolidator) findNeighbors(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore) (map[int][]scoredNode, error) {
	if s == nil {
		return make(map[int][]scoredNode), nil
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
func (c *LLMConsolidator) findNeighborsByEmbedding(ctx context.Context, ec llm.EmbeddingComparer, memories []ClassifiedMemory, s store.GraphStore, topK int) (map[int][]scoredNode, error) {
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

	result := make(map[int][]scoredNode)

	for i, mem := range memories {
		queryVec, embedErr := ec.Embed(ctx, mem.Content.Canonical)
		if embedErr != nil {
			c.logDecision(map[string]any{
				"stage": "relate",
				"event": "embed_failed",
				"index": i,
				"error": embedErr.Error(),
			})
			// Fall back to unranked query neighbors for this memory.
			fallback, qErr := c.findNeighborsByQuery(ctx, memories[i:i+1], s, topK)
			if qErr != nil {
				c.logDecision(map[string]any{
					"stage": "relate",
					"event": "embed_fallback_also_failed",
					"index": i,
					"error": qErr.Error(),
				})
			} else {
				result[i] = fallback[0]
			}
			continue
		}

		// Score all existing behaviors by cosine similarity.
		type embScore struct {
			id    string
			score float64
		}
		var scores []embScore
		for _, be := range allEmbeddings {
			sim := vecmath.CosineSimilarity(queryVec, be.Embedding)
			if sim > 0.3 { // minimum threshold
				scores = append(scores, embScore{id: be.BehaviorID, score: sim})
			}
		}

		if len(scores) == 0 {
			c.logDecision(map[string]any{
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
				c.logDecision(map[string]any{
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
			// Only include behavior nodes — non-behavior nodes with embeddings
			// (e.g. context nodes) should not appear as relationship candidates.
			if node.Kind != store.NodeKindBehavior {
				continue
			}
			result[i] = append(result[i], scoredNode{Node: *node, Score: sc.score})
		}
	}

	return result, nil
}

// findNeighborsByQuery falls back to fetching all behavior nodes and returning
// them unranked as neighbors for each memory.
func (c *LLMConsolidator) findNeighborsByQuery(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore, topK int) (map[int][]scoredNode, error) {
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

	result := make(map[int][]scoredNode)
	for i := range memories {
		entry := make([]scoredNode, len(capped))
		for j, n := range capped {
			entry[j] = scoredNode{Node: n, Score: 0.0} // unranked fallback
		}
		result[i] = entry
	}
	return result, nil
}

// maxCoOccurrencePerSession caps the number of memories linked via
// co-occurrence edges per session, avoiding O(n²) edge growth.
// With a cap of 15, the maximum edges per session is C(15,2) = 105.
const maxCoOccurrencePerSession = 15

// buildCoOccurrenceEdges generates co-activated edges between memories
// that share the same session ID. If a session has more than
// maxCoOccurrencePerSession memories, only the first N are linked.
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
		// Cap to avoid O(n²) edge count for large sessions.
		capped := indices
		if len(capped) > maxCoOccurrencePerSession {
			capped = capped[:maxCoOccurrencePerSession]
		}
		// Create edges between all pairs.
		for a := 0; a < len(capped); a++ {
			for b := a + 1; b < len(capped); b++ {
				srcID := PendingNodeID(capped[a])
				tgtID := PendingNodeID(capped[b])
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

// convertProposals converts parsed LLM proposals into store edges and merge proposals.
// neighbors provides the scored neighbor lists from vector search so merge proposals
// can carry the actual cosine similarity rather than defaulting to 0.0.
//
// Per-edge validation happens here rather than in ParseRelationships so that one
// invalid edge (missing weight, bad kind, hallucinated target) does not discard
// all proposals in the response.
func convertProposals(proposals []relateProposal, memories []ClassifiedMemory, neighbors map[int][]scoredNode) ([]store.Edge, []MergeProposal, []int) {
	now := time.Now()
	var edges []store.Edge
	var merges []MergeProposal
	var skips []int

	// Build the set of valid neighbor IDs — the only IDs the LLM has seen.
	// Edge targets and merge targets that fall outside this set are hallucinated.
	knownIDs := make(map[string]bool)
	for _, scoredNodes := range neighbors {
		for _, sn := range scoredNodes {
			knownIDs[sn.Node.ID] = true
		}
	}

	for _, p := range proposals {
		if p.MemoryIndex < 0 || p.MemoryIndex >= len(memories) {
			continue
		}

		switch p.Action {
		case "create":
			srcID := PendingNodeID(p.MemoryIndex)
			for _, e := range p.Edges {
				// Per-edge validation: skip individual bad edges.
				if e.Target == "" {
					continue
				}
				if _, ok := validEdgeKind[e.Kind]; !ok {
					continue
				}
				if e.Weight <= 0 || e.Weight > 1.0 {
					continue
				}
				// Target must be a neighbor the LLM was shown, or another
				// pending memory (pending-N). Hallucinated IDs create dangling edges.
				if !knownIDs[e.Target] && !isPendingID(e.Target) {
					continue
				}
				edges = append(edges, store.Edge{
					Source:    srcID,
					Target:    e.Target,
					Kind:      validEdgeKind[e.Kind],
					Weight:    e.Weight,
					CreatedAt: now,
				})
			}

		case "merge":
			if p.MergeInto == nil {
				continue
			}
			// Merge target must be a neighbor the LLM was shown.
			if !knownIDs[p.MergeInto.TargetID] {
				continue
			}
			// Use the highest edge weight if available, otherwise check if the
			// merge target was a scored neighbor (carrying cosine similarity).
			sim := highestWeight(p.Edges)
			if sim == 0.0 {
				sim = neighborSimilarity(neighbors, p.MemoryIndex, p.MergeInto.TargetID)
			}
			merges = append(merges, MergeProposal{
				Memory:      memories[p.MemoryIndex],
				MemoryIndex: p.MemoryIndex,
				TargetID:    p.MergeInto.TargetID,
				Similarity:  sim,
				Strategy:    p.MergeInto.Strategy,
			})

		case "skip":
			skips = append(skips, p.MemoryIndex)
		}
	}

	return edges, merges, skips
}

// isPendingID returns true if the ID matches the pending-N format used for
// cross-referencing new memories within the same batch.
func isPendingID(id string) bool {
	return strings.HasPrefix(id, "pending-")
}

// neighborSimilarity returns the cosine similarity score for the given target
// from the scored neighbor list, or 0.0 if the target was not among the neighbors.
func neighborSimilarity(neighbors map[int][]scoredNode, memIdx int, targetID string) float64 {
	for _, sn := range neighbors[memIdx] {
		if sn.Node.ID == targetID {
			return sn.Score
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
func neighborCounts(neighbors map[int][]scoredNode) map[int]int {
	counts := make(map[int]int, len(neighbors))
	for idx, nodes := range neighbors {
		counts[idx] = len(nodes)
	}
	return counts
}
