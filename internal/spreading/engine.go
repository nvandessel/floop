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

	"github.com/nvandessel/floop/internal/constants"
	"github.com/nvandessel/floop/internal/ranking"
	"github.com/nvandessel/floop/internal/store"
)

// Config holds tunable parameters for the spreading activation engine.
// Defaults are derived from the SYNAPSE paper (arxiv 2601.02744).
type Config struct {
	// MaxSteps is the number of propagation iterations (T). Default: 3.
	MaxSteps int

	// DecayFactor is the energy retention per hop (delta). Default: 0.7.
	// Higher values allow activation to spread further.
	DecayFactor float64

	// SpreadFactor is the energy transmission efficiency (S). Default: 0.85.
	// Fraction of a node's activation that flows through each edge.
	SpreadFactor float64

	// MinActivation is the threshold below which nodes are excluded (epsilon). Default: 0.01.
	MinActivation float64

	// TemporalDecayRate is the rho parameter for edge temporal decay. Default: 0.01.
	TemporalDecayRate float64

	// Inhibition configures lateral inhibition. When non-nil, highly activated
	// nodes suppress weaker competitors, focusing the activation pattern.
	Inhibition *InhibitionConfig

	// Affinity configures virtual edge generation from shared tags.
	// When non-nil and Enabled, behaviors sharing tags create implicit
	// connections during propagation without needing explicit graph edges.
	Affinity *AffinityConfig

	// TagProvider supplies behavior tags for feature affinity.
	// Required when Affinity is enabled; ignored otherwise.
	TagProvider TagProvider
}

// DefaultConfig returns the default spreading activation configuration.
func DefaultConfig() Config {
	inh := DefaultInhibitionConfig()
	return Config{
		MaxSteps:          3,
		DecayFactor:       0.7,
		SpreadFactor:      0.85,
		MinActivation:     0.01,
		TemporalDecayRate: ranking.DefaultDecayRate,
		Inhibition:        &inh,
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

// Compile-time check: Engine implements Activator.
var _ Activator = (*Engine)(nil)

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

// StepSnapshot captures the activation state at a single point during propagation.
type StepSnapshot struct {
	// Step is the propagation step number. 0 = initial seed state,
	// 1-N = after each propagation step, final = post-inhibition/sigmoid.
	Step int `json:"step"`

	// Activation maps nodeID to activation level at this step.
	Activation map[string]float64 `json:"activation"`

	// Final is true for the last snapshot (post-inhibition + sigmoid applied).
	Final bool `json:"final"`
}

// propagateStep performs one step of spreading activation.
// It reads from activation and writes to newActivation (synchronous update).
// If distance and seedSource are non-nil, it also tracks shortest paths.
func (e *Engine) propagateStep(ctx context.Context, activation, newActivation map[string]float64,
	distance map[string]int, seedSource map[string]string,
	allTags map[string][]string, affinityEnabled bool) error {

	for nodeID, nodeAct := range activation {
		if nodeAct < e.config.MinActivation {
			continue
		}

		edges, err := e.store.GetEdges(ctx, nodeID, store.DirectionBoth, "")
		if err != nil {
			return fmt.Errorf("spreading activation: get edges for %s: %w", nodeID, err)
		}

		// Append virtual affinity edges from shared tags.
		if affinityEnabled && allTags != nil {
			if nodeTags, ok := allTags[nodeID]; ok && len(nodeTags) > 0 {
				edges = append(edges, virtualAffinityEdges(nodeID, nodeTags, allTags, *e.config.Affinity)...)
			}
		}

		if len(edges) == 0 {
			continue
		}

		// Count edges by category: virtual affinity, conflict, directional suppressive, and positive.
		// Each category uses an independent denominator so they don't dilute each other's
		// normalization. See docs/SCIENCE.md "Suppressive Edge Semantics" for the design
		// rationale and the three-denominator model (floop-g30, greptile-199).
		var virtualOutDegree float64
		var positiveCount, conflictCount, directionalSuppressiveCount int
		for _, edge := range edges {
			if edge.Kind == edgeKindFeatureAffinity {
				virtualOutDegree++
			} else {
				switch edge.Kind {
				case store.EdgeKindConflicts:
					conflictCount++
				case store.EdgeKindOverrides, store.EdgeKindDeprecatedTo, store.EdgeKindMergedInto:
					// Only count outbound directional edges — inbound ones don't suppress.
					if edge.Source == nodeID {
						directionalSuppressiveCount++
					}
				default:
					positiveCount++
				}
			}
		}

		for _, edge := range edges {
			neighbor := neighborID(nodeID, edge)

			effectiveWeight := ranking.EdgeDecay(edge.Weight, edgeLastActivated(edge), e.config.TemporalDecayRate)

			// Track whether this edge actually spread or suppressed energy,
			// so we only update distance for edges that did real work.
			energySpread := false

			switch edge.Kind {
			case store.EdgeKindConflicts:
				// Conflicts are symmetric — suppress in both directions.
				// Use conflictCount as the denominator, independent of directional edges.
				energy := nodeAct * e.config.SpreadFactor * effectiveWeight / float64(conflictCount)
				energy *= e.config.DecayFactor
				newActivation[neighbor] -= energy
				if newActivation[neighbor] < 0 {
					newActivation[neighbor] = 0
				}
				energySpread = true
			case store.EdgeKindOverrides, store.EdgeKindDeprecatedTo, store.EdgeKindMergedInto:
				// Directional suppression: only suppress when traversing outbound (source → target).
				// Seeding a deprecated node should NOT suppress its replacement.
				if edge.Source == nodeID {
					energy := nodeAct * e.config.SpreadFactor * effectiveWeight / float64(directionalSuppressiveCount)
					energy *= e.config.DecayFactor
					newActivation[neighbor] -= energy
					if newActivation[neighbor] < 0 {
						newActivation[neighbor] = 0
					}
					energySpread = true
				}
			default:
				// Use separate outDegree for real vs virtual edges so that
				// virtual affinity edges don't dilute real edge normalization.
				outDegree := float64(positiveCount)
				if edge.Kind == edgeKindFeatureAffinity {
					outDegree = virtualOutDegree
				}
				if outDegree == 0 {
					// Unreachable: counts are derived from the same slice we're iterating.
					panic(fmt.Sprintf("spreading: outDegree=0 for edge kind %q (nodeID=%s)", edge.Kind, nodeID))
				}
				// Normal edges spread: use max to prevent runaway activation.
				energy := nodeAct * e.config.SpreadFactor * effectiveWeight / outDegree
				energy *= e.config.DecayFactor
				if energy > newActivation[neighbor] {
					newActivation[neighbor] = energy
				}
				energySpread = true
			}

			// Track distance and seed source via the shortest path.
			// Only update when energy was actually spread — inbound directional
			// edges that skip the suppression guard should not record distances.
			if distance != nil && energySpread {
				newDist := distance[nodeID] + 1
				if existingDist, exists := distance[neighbor]; !exists || newDist < existingDist {
					distance[neighbor] = newDist
					seedSource[neighbor] = seedSource[nodeID]
				}
			}
		}
	}
	return nil
}

// postProcess applies inhibition, sigmoid, and MinActivation filtering to an activation map.
func (e *Engine) postProcess(activation map[string]float64) map[string]float64 {
	if e.config.Inhibition != nil {
		activation = ApplyInhibition(activation, *e.config.Inhibition)
	}
	for id, act := range activation {
		activation[id] = sigmoid(act)
	}
	for id, act := range activation {
		if act < e.config.MinActivation {
			delete(activation, id)
		}
	}
	return activation
}

// ActivateWithSteps performs spreading activation and returns per-step snapshots.
// It returns MaxSteps+2 snapshots: initial seed state, one after each propagation
// step, and a final post-processed snapshot with inhibition and sigmoid applied.
func (e *Engine) ActivateWithSteps(ctx context.Context, seeds []Seed) ([]StepSnapshot, error) {
	if len(seeds) == 0 {
		return []StepSnapshot{}, nil
	}

	activation := make(map[string]float64)
	for _, s := range seeds {
		activation[s.BehaviorID] = s.Activation
	}

	// Capture initial seed state (step 0).
	snapshots := make([]StepSnapshot, 0, e.config.MaxSteps+2)
	snapshots = append(snapshots, StepSnapshot{
		Step:       0,
		Activation: copyActivation(activation),
	})

	var allTags map[string][]string
	affinityEnabled := e.config.Affinity != nil && e.config.Affinity.Enabled && e.config.TagProvider != nil
	if affinityEnabled {
		allTags = e.config.TagProvider.GetAllBehaviorTags(ctx)
	}

	for step := 0; step < e.config.MaxSteps; step++ {
		newActivation := copyActivation(activation)

		if err := e.propagateStep(ctx, activation, newActivation, nil, nil, allTags, affinityEnabled); err != nil {
			return nil, err
		}

		activation = newActivation
		snapshots = append(snapshots, StepSnapshot{
			Step:       step + 1,
			Activation: copyActivation(activation),
		})
	}

	// Final snapshot: apply inhibition + sigmoid.
	finalActivation := e.postProcess(copyActivation(activation))

	snapshots = append(snapshots, StepSnapshot{
		Step:       e.config.MaxSteps + 1,
		Activation: finalActivation,
		Final:      true,
	})

	return snapshots, nil
}

// Activate performs spreading activation from the given seeds.
// It returns all behaviors with activation above MinActivation,
// sorted by activation descending.
func (e *Engine) Activate(ctx context.Context, seeds []Seed) ([]Result, error) {
	if len(seeds) == 0 {
		return []Result{}, nil
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

	// Step 1b: Pre-load all behavior tags for feature affinity.
	var allTags map[string][]string
	affinityEnabled := e.config.Affinity != nil && e.config.Affinity.Enabled && e.config.TagProvider != nil
	if affinityEnabled {
		allTags = e.config.TagProvider.GetAllBehaviorTags(ctx)
	}

	// Step 2: Propagation loop.
	for step := 0; step < e.config.MaxSteps; step++ {
		// Create a snapshot of current activations. New activations are
		// written into a fresh map so that updates within a single step
		// do not affect each other (synchronous update).
		newActivation := copyActivation(activation)

		if err := e.propagateStep(ctx, activation, newActivation, distance, seedSource, allTags, affinityEnabled); err != nil {
			return nil, err
		}

		activation = newActivation
	}

	// Step 3: Post-process (inhibition + sigmoid + filter).
	activation = e.postProcess(activation)

	// Step 4: Build results.
	results := make([]Result, 0, len(activation))
	for id, act := range activation {
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

// copyActivation returns an independent copy of an activation map.
func copyActivation(m map[string]float64) map[string]float64 {
	c := make(map[string]float64, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// sigmoid applies a sigmoid function centered at 0.3 to map raw activation
// into a sharper [0, 1] range. Values below 0.3 are suppressed toward 0;
// values above 0.3 are amplified toward 1.
func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-constants.SigmoidGain*(x-constants.SigmoidCenter)))
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
