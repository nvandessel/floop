//go:build cgo

package spreading

/*
#include "sproink.h"
*/
import "C"

import "github.com/nvandessel/floop/internal/constants"

// NativeExtractPairs extracts co-activation pairs using sproink's FFI.
// It maps u32 node IDs back to UUID strings via the engine's IDMap and
// returns pairs in canonical order (smaller BehaviorID first).
//
// The caller must not free sproinkResults — ownership remains with the caller.
func (e *NativeEngine) NativeExtractPairs(sproinkResults *C.SproinkResults, seedNodes []uint32, threshold float64) []CoActivationPair {
	pairs, err := sproinkExtractPairs(sproinkResults, uint32(len(seedNodes)), seedNodes, threshold)
	if err != nil {
		return nil
	}
	defer sproinkPairsFree(pairs)

	nodesA, nodesB, activationsA, activationsB := sproinkPairsData(pairs)
	n := len(nodesA)
	if n == 0 {
		return nil
	}

	out := make([]CoActivationPair, n)
	for i := 0; i < n; i++ {
		uuidA := e.idmap.ToUUID(nodesA[i])
		uuidB := e.idmap.ToUUID(nodesB[i])
		actA := activationsA[i]
		actB := activationsB[i]

		// Canonical ordering: smaller UUID first.
		if uuidA > uuidB {
			uuidA, uuidB = uuidB, uuidA
			actA, actB = actB, actA
		}

		out[i] = CoActivationPair{
			BehaviorA:   uuidA,
			BehaviorB:   uuidB,
			ActivationA: actA,
			ActivationB: actB,
		}
	}

	return out
}

// NativeOjaUpdate computes Oja's rule via the sproink FFI.
func NativeOjaUpdate(currentWeight, activationA, activationB float64, cfg HebbianConfig) float64 {
	return sproinkOjaUpdate(currentWeight, activationA, activationB, cfg.LearningRate, cfg.MinWeight, cfg.MaxWeight)
}

// nativeActivateAndExtractPairs is a test helper that runs activation and pair
// extraction in one call, avoiding the need for test files to reference C types.
// It returns the extracted pairs and the Go-side results for comparison.
func (e *NativeEngine) nativeActivateAndExtractPairs(seeds []Seed, threshold float64) ([]CoActivationPair, []Result, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	seedNodes := make([]uint32, 0, len(seeds))
	seedActivations := make([]float64, 0, len(seeds))
	for _, s := range seeds {
		id, ok := e.idmap.ToU32(s.BehaviorID)
		if !ok {
			continue
		}
		seedNodes = append(seedNodes, id)
		seedActivations = append(seedActivations, s.Activation)
	}

	if len(seedNodes) == 0 {
		return nil, nil, nil
	}

	inhEnabled := e.config.Inhibition != nil && e.config.Inhibition.Enabled
	var inhStrength float64
	var inhBreadth uint32
	if e.config.Inhibition != nil {
		inhStrength = e.config.Inhibition.Strength
		inhBreadth = uint32(e.config.Inhibition.Breadth)
	}

	raw, err := sproinkActivate(
		e.graph,
		seedNodes,
		seedActivations,
		uint32(e.config.MaxSteps),
		e.config.DecayFactor,
		e.config.SpreadFactor,
		e.config.MinActivation,
		constants.SigmoidGain,
		constants.SigmoidCenter,
		inhEnabled,
		inhStrength,
		inhBreadth,
	)
	if err != nil {
		return nil, nil, err
	}
	defer sproinkResultsFree(raw)

	// Extract pairs via FFI.
	pairs := e.NativeExtractPairs(raw, seedNodes, threshold)

	// Build Go-side results for comparison.
	n := sproinkResultsLen(raw)
	nodes := sproinkResultsNodes(raw)
	activations := sproinkResultsActivations(raw)
	distances := sproinkResultsDistances(raw)

	goResults := make([]Result, n)
	for i := uint32(0); i < n; i++ {
		goResults[i] = Result{
			BehaviorID: e.idmap.ToUUID(nodes[i]),
			Activation: activations[i],
			Distance:   int(distances[i]),
		}
	}

	return pairs, goResults, nil
}
