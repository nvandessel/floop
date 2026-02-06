package ranking

import (
	"context"
	"fmt"
	"math"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// PageRankConfig holds configuration for PageRank computation.
type PageRankConfig struct {
	// DampingFactor (d) is the probability of following an edge vs. teleporting.
	// Standard value: 0.85.
	DampingFactor float64

	// MaxIterations is the maximum number of power iteration steps. Default: 100.
	MaxIterations int

	// Tolerance is the convergence threshold. Default: 1e-6.
	Tolerance float64
}

// DefaultPageRankConfig returns the default PageRank configuration.
func DefaultPageRankConfig() PageRankConfig {
	return PageRankConfig{
		DampingFactor: 0.85,
		MaxIterations: 100,
		Tolerance:     1e-6,
	}
}

// ComputePageRank calculates PageRank scores for all nodes in the behavior graph.
// Returns a map of behaviorID to PageRank score (0.0-1.0, normalized).
//
// Algorithm: Standard power iteration
//  1. Initialize all nodes with score = 1/N
//  2. For each iteration:
//     PR(v) = (1-d)/N + d * sum(PR(u)/outDegree(u)) for all u linking to v
//  3. Converge when max change < Tolerance
//  4. Normalize to [0, 1] range
//
// Edge directions: All edge kinds (requires, overrides, similar-to, etc.)
// are treated as bidirectional links for PageRank purposes.
func ComputePageRank(ctx context.Context, s store.GraphStore, config PageRankConfig) (map[string]float64, error) {
	// Query all behavior nodes.
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("computing pagerank: query nodes: %w", err)
	}

	n := len(nodes)
	if n == 0 {
		return make(map[string]float64), nil
	}

	// Build adjacency lists (bidirectional).
	// inbound[v] = list of nodes u that have an edge to v (treating all edges as bidirectional).
	inbound := make(map[string][]string, n)
	outDegree := make(map[string]int, n)

	// Initialize maps for all nodes.
	nodeIDs := make([]string, 0, n)
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.ID)
		inbound[node.ID] = nil
		outDegree[node.ID] = 0
	}

	// Build adjacency from edges. Treat all edges as bidirectional:
	// an edge A->B creates links A->B and B->A.
	nodeSet := make(map[string]bool, n)
	for _, id := range nodeIDs {
		nodeSet[id] = true
	}

	for _, nodeID := range nodeIDs {
		edges, err := s.GetEdges(ctx, nodeID, store.DirectionBoth, "")
		if err != nil {
			return nil, fmt.Errorf("computing pagerank: get edges for %s: %w", nodeID, err)
		}

		for _, edge := range edges {
			neighbor := edge.Target
			if neighbor == nodeID {
				neighbor = edge.Source
			}
			// Only include edges to nodes in our behavior set.
			if !nodeSet[neighbor] {
				continue
			}

			// Bidirectional: nodeID -> neighbor and neighbor -> nodeID.
			// nodeID links to neighbor => nodeID is in inbound[neighbor].
			inbound[neighbor] = append(inbound[neighbor], nodeID)
			outDegree[nodeID]++
		}
	}

	// Deduplicate inbound lists (edges may appear twice via bidirectional traversal).
	for id, sources := range inbound {
		inbound[id] = dedup(sources)
	}

	// Recount outDegree from deduplicated inbound lists.
	for id := range outDegree {
		outDegree[id] = 0
	}
	for _, sources := range inbound {
		for _, src := range sources {
			outDegree[src]++
		}
	}

	// Power iteration.
	d := config.DampingFactor
	nf := float64(n)
	scores := make(map[string]float64, n)
	for _, id := range nodeIDs {
		scores[id] = 1.0 / nf
	}

	for iter := 0; iter < config.MaxIterations; iter++ {
		newScores := make(map[string]float64, n)
		maxDelta := 0.0

		for _, v := range nodeIDs {
			sum := 0.0
			for _, u := range inbound[v] {
				deg := outDegree[u]
				if deg > 0 {
					sum += scores[u] / float64(deg)
				}
			}

			newScore := (1.0-d)/nf + d*sum
			newScores[v] = newScore

			delta := math.Abs(newScore - scores[v])
			if delta > maxDelta {
				maxDelta = delta
			}
		}

		scores = newScores

		if maxDelta < config.Tolerance {
			break
		}
	}

	// Normalize to [0, 1] by dividing by max score.
	maxScore := 0.0
	for _, score := range scores {
		if score > maxScore {
			maxScore = score
		}
	}

	if maxScore > 0 {
		for id, score := range scores {
			scores[id] = score / maxScore
		}
	}

	return scores, nil
}

// dedup removes duplicate strings from a slice, preserving order.
func dedup(ss []string) []string {
	if len(ss) == 0 {
		return ss
	}
	seen := make(map[string]bool, len(ss))
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
