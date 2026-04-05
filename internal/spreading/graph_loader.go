//go:build cgo

package spreading

/*
#include "sproink.h"
*/
import "C"

import (
	"context"
	"fmt"

	"github.com/nvandessel/floop/internal/ranking"
	"github.com/nvandessel/floop/internal/store"
)

// loadGraph loads all edges from the store, builds the IDMap, pre-materializes
// affinity edges, pre-applies temporal decay, and calls sproink_graph_build to
// create the CSR graph.
func loadGraph(ctx context.Context, s store.ExtendedGraphStore, config Config) (*C.SproinkGraph, *IDMap, error) {
	// Step a: bulk load all edges
	edges, err := s.GetAllEdges(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("loadGraph: GetAllEdges: %w", err)
	}

	// Step b: get all behavior nodes (for isolated nodes with no edges)
	behaviorNodes, err := s.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, nil, fmt.Errorf("loadGraph: QueryNodes: %w", err)
	}

	// Step c: build IDMap from all unique node IDs
	idmap := NewIDMap()
	for _, e := range edges {
		idmap.GetOrAssign(e.Source)
		idmap.GetOrAssign(e.Target)
	}
	for _, n := range behaviorNodes {
		idmap.GetOrAssign(n.ID)
	}

	// Step d: pre-materialize virtual affinity edges
	if config.Affinity != nil && config.Affinity.Enabled && config.TagProvider != nil {
		allTags := config.TagProvider.GetAllBehaviorTags(ctx)
		if len(allTags) > 0 {
			// Track seen pairs to deduplicate (sproink handles bidirectional internally)
			type pair struct{ a, b string }
			seen := make(map[pair]struct{})

			for nodeID, nodeTags := range allTags {
				affinityEdges := virtualAffinityEdges(nodeID, nodeTags, allTags, *config.Affinity)
				for _, e := range affinityEdges {
					// Canonical order: smaller UUID first
					a, b := e.Source, e.Target
					if a > b {
						a, b = b, a
					}
					p := pair{a, b}
					if _, dup := seen[p]; dup {
						continue
					}
					seen[p] = struct{}{}
					edges = append(edges, e)
					// Ensure affinity node IDs are in the IDMap
					idmap.GetOrAssign(e.Source)
					idmap.GetOrAssign(e.Target)
				}
			}
		}
	}

	// If there are no nodes at all, return an empty graph (no CSR to build)
	if idmap.Len() == 0 {
		graph, err := sproinkGraphBuild(0, 0, nil, nil, nil, nil)
		if err != nil {
			// sproink may refuse to build an empty graph; treat as valid empty state
			return nil, idmap, nil
		}
		return graph, idmap, nil
	}

	// Step e+f: build parallel arrays with temporal decay applied
	numEdges := len(edges)
	sources := make([]uint32, numEdges)
	targets := make([]uint32, numEdges)
	weights := make([]float64, numEdges)
	kinds := make([]uint8, numEdges)

	for i, e := range edges {
		sources[i] = idmap.GetOrAssign(e.Source)
		targets[i] = idmap.GetOrAssign(e.Target)

		// Pre-apply temporal decay
		w := e.Weight
		if e.LastActivated != nil {
			w = ranking.EdgeDecay(e.Weight, *e.LastActivated, config.TemporalDecayRate)
		}
		weights[i] = w

		kinds[i] = edgeKindToU8(e.Kind)
	}

	// Step g: call sproink_graph_build
	graph, err := sproinkGraphBuild(uint32(idmap.Len()), uint32(numEdges), sources, targets, weights, kinds)
	if err != nil {
		return nil, nil, fmt.Errorf("loadGraph: %w", err)
	}

	return graph, idmap, nil
}
