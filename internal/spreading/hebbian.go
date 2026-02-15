package spreading

import (
	"math"
	"time"
)

// HebbianConfig configures Oja-stabilized Hebbian co-activation learning.
type HebbianConfig struct {
	// LearningRate (eta) controls how fast edge weights adapt. Default: 0.05.
	// Oja's rule is self-limiting, so a fixed rate works well.
	LearningRate float64

	// MinWeight is the floor for edge weights. Default: 0.01.
	// Edges below this are candidates for pruning.
	MinWeight float64

	// MaxWeight is the ceiling for edge weights. Default: 0.95.
	// Prevents co-activated edges from dominating semantic edges.
	MaxWeight float64

	// ActivationThreshold is the minimum activation for a behavior to be
	// included in co-activation pairs. Default: 0.3 (sigmoid inflection).
	ActivationThreshold float64

	// CreationGate is the number of co-occurrences required within
	// CreationWindow before a new co-activated edge is created. Default: 3.
	CreationGate int

	// CreationWindow is the time window for counting co-occurrences. Default: 7 days.
	CreationWindow time.Duration
}

// DefaultHebbianConfig returns the default Hebbian learning configuration.
func DefaultHebbianConfig() HebbianConfig {
	return HebbianConfig{
		LearningRate:        0.05,
		MinWeight:           0.01,
		MaxWeight:           0.95,
		ActivationThreshold: 0.3,
		CreationGate:        3,
		CreationWindow:      7 * 24 * time.Hour,
	}
}

// CoActivationPair represents two behaviors that were co-activated.
type CoActivationPair struct {
	BehaviorA   string  // First behavior ID
	BehaviorB   string  // Second behavior ID
	ActivationA float64 // Activation level of A
	ActivationB float64 // Activation level of B
}

// OjaUpdate computes the new edge weight using Oja's rule.
//
// Oja's rule: dW = eta * (A_i * A_j - A_j^2 * W)
//
// The A_j^2 * W term is the forgetting factor that prevents unbounded weight
// growth. The weight norm converges to 1.0 naturally, which is critical —
// naive Hebbian learning causes runaway clustering in spreading activation
// networks.
//
// The result is clamped to [minWeight, maxWeight].
func OjaUpdate(currentWeight, activationA, activationB float64, cfg HebbianConfig) float64 {
	// Oja's rule: dW = eta * (A_i * A_j - A_j^2 * W)
	hebbian := activationA * activationB
	forgetting := activationB * activationB * currentWeight
	dw := cfg.LearningRate * (hebbian - forgetting)

	newWeight := currentWeight + dw
	return clampWeight(newWeight, cfg.MinWeight, cfg.MaxWeight)
}

// ExtractCoActivationPairs identifies pairs of behaviors that were
// co-activated above the threshold. Only pairs where both members
// exceed the activation threshold are included.
//
// seedIDs is the set of seed behavior IDs. Pairs where BOTH members
// are seeds are excluded — seed co-activation reflects context matching,
// not behavioral affinity.
func ExtractCoActivationPairs(results []Result, seedIDs map[string]bool, cfg HebbianConfig) []CoActivationPair {
	// Filter to behaviors above threshold
	active := make([]Result, 0, len(results))
	for _, r := range results {
		if r.Activation >= cfg.ActivationThreshold {
			active = append(active, r)
		}
	}

	if len(active) < 2 {
		return nil
	}

	// Generate all unique pairs, excluding seed-seed pairs
	pairs := make([]CoActivationPair, 0, len(active)*(len(active)-1)/2)
	for i := 0; i < len(active); i++ {
		for j := i + 1; j < len(active); j++ {
			aIsSeed := seedIDs[active[i].BehaviorID]
			bIsSeed := seedIDs[active[j].BehaviorID]

			// Skip pairs where both are seeds
			if aIsSeed && bIsSeed {
				continue
			}

			// Canonical ordering: smaller ID first
			a, b := active[i], active[j]
			if a.BehaviorID > b.BehaviorID {
				a, b = b, a
			}

			pairs = append(pairs, CoActivationPair{
				BehaviorA:   a.BehaviorID,
				BehaviorB:   b.BehaviorID,
				ActivationA: a.Activation,
				ActivationB: b.Activation,
			})
		}
	}

	return pairs
}

// clampWeight restricts a weight to [min, max].
func clampWeight(w, min, max float64) float64 {
	if math.IsNaN(w) || math.IsInf(w, 0) {
		return min
	}
	if w < min {
		return min
	}
	if w > max {
		return max
	}
	return w
}
