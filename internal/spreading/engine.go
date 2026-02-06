// Package spreading implements a spreading activation engine for the behavior
// graph. Activation energy propagates from seed nodes through weighted edges,
// decaying with distance, and is shaped by sigmoid squashing to produce sharp
// relevance signals.
package spreading

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/nvandessel/feedback-loop/internal/ranking"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// Config holds tunable parameters for the spreading activation engine.
// Defaults are derived from the SYNAPSE paper (arxiv 2601.02744).
type Config struct {
	// MaxSteps is the number of propagation iterations (T). Default: 3.
	MaxSteps int

	// DecayFactor is the energy retention per hop (delta). Default: 0.5.
	// Higher values allow activation to spread further.
	DecayFactor float64

	// SpreadFactor is the energy transmission efficiency (S). Default: 0.8.
	// Fraction of a node's activation that flows through each edge.
	SpreadFactor float64

	// MinActivation is the threshold below which nodes are excluded (epsilon). Default: 0.01.
	MinActivation float64

	// TemporalDecayRate is the rho parameter for edge temporal decay. Default: 0.01.
	TemporalDecayRate float64
}

// DefaultConfig returns the default spreading activation configuration.
func DefaultConfig() Config {
	return Config{
		MaxSteps:          3,
		DecayFactor:       0.5,
		SpreadFactor:      0.8,
		MinActivation:     0.01,
		TemporalDecayRate: ranking.DefaultDecayRate,
	}
}

// Seed represents an initial activation anchor.
type Seed struct {
	BehaviorID string  // The behavior to seed
	Activation float64 // Initial activation level (0.0-1.0)
	Source     string  // What triggered this seed (e.g., "context:language=go")
}

// Result represents a behavior's activation state after propagation.
type Result struct {
	BehaviorID string  // The activated behavior
	Activation float64 // Final activation level (0.0-1.0)
	Distance   int     // Minimum hops from nearest seed
	SeedSource string  // Which seed triggered this (nearest)
}

// Engine performs spreading activation over the behavior graph.
// The engine is stateless: all mutable state lives in activation maps
// created during each call to Activate.
type Engine struct {
	config Config
	store  store.GraphStore
}

// NewEngine creates a new spreading activation engine.
func NewEngine(s store.GraphStore, config Config) *Engine {
	return &Engine{
		config: config,
		store:  s,
	}
}

// Activate performs spreading activation from the given seeds.
// It returns all behaviors with activation above MinActivation,
// sorted by activation descending.
func (e *Engine) Activate(ctx context.Context, seeds []Seed) ([]Result, error) {
	if len(seeds) == 0 {
		return nil, nil
	}

	// Step 1: Initialize maps from seeds.
	activation := make(map[string]float64)
	distance := make(map[string]int)
	seedSource := make(map[string]string)

	for _, s := range seeds {
		activation[s.BehaviorID] = s.Activation
		distance[s.BehaviorID] = 0
		seedSource[s.BehaviorID] = s.Source
	}

	// Step 2: Propagation loop.
	for step := 0; step < e.config.MaxSteps; step++ {
		// Create a snapshot of current activations. New activations are
		// written into a fresh map so that updates within a single step
		// do not affect each other (synchronous update).
		newActivation := make(map[string]float64, len(activation))
		for id, act := range activation {
			newActivation[id] = act
		}

		// Iterate over every node that currently has activation above
		// the threshold.
		for nodeID, nodeAct := range activation {
			if nodeAct < e.config.MinActivation {
				continue
			}

			edges, err := e.store.GetEdges(ctx, nodeID, store.DirectionBoth, "")
			if err != nil {
				return nil, fmt.Errorf("spreading activation: get edges for %s: %w", nodeID, err)
			}
			if len(edges) == 0 {
				continue
			}

			outDegree := float64(len(edges))

			for _, edge := range edges {
				neighbor := neighborID(nodeID, edge)

				effectiveWeight := ranking.EdgeDecay(edge.Weight, edgeLastActivated(edge), e.config.TemporalDecayRate)

				energy := nodeAct * e.config.SpreadFactor * effectiveWeight / outDegree
				energy *= e.config.DecayFactor

				// Use max, not sum, to prevent runaway activation.
				if energy > newActivation[neighbor] {
					newActivation[neighbor] = energy
				}

				// Track distance and seed source via the shortest path.
				newDist := distance[nodeID] + 1
				if existingDist, exists := distance[neighbor]; !exists || newDist < existingDist {
					distance[neighbor] = newDist
					seedSource[neighbor] = seedSource[nodeID]
				}
			}
		}

		activation = newActivation
	}

	// Step 3: Sigmoid squashing — centered at 0.3.
	for id, act := range activation {
		activation[id] = sigmoid(act)
	}

	// Step 4: Filter by MinActivation and build results.
	results := make([]Result, 0, len(activation))
	for id, act := range activation {
		if act < e.config.MinActivation {
			continue
		}
		results = append(results, Result{
			BehaviorID: id,
			Activation: act,
			Distance:   distance[id],
			SeedSource: seedSource[id],
		})
	}

	// Step 5: Sort by activation descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Activation > results[j].Activation
	})

	return results, nil
}

// sigmoid applies a sigmoid function centered at 0.3 to map raw activation
// into a sharper [0, 1] range. Values below 0.3 are suppressed toward 0;
// values above 0.3 are amplified toward 1.
func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-10.0*(x-0.3)))
}

// neighborID returns the ID of the node on the other end of the edge
// relative to the given nodeID.
func neighborID(nodeID string, edge store.Edge) string {
	if edge.Source == nodeID {
		return edge.Target
	}
	return edge.Source
}

// edgeLastActivated returns the LastActivated time for an edge, or the
// zero time if the field is nil.
func edgeLastActivated(edge store.Edge) time.Time {
	if edge.LastActivated != nil {
		return *edge.LastActivated
	}
	return time.Time{}
}
