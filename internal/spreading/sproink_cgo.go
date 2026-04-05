//go:build cgo

package spreading

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/sproink/include
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../../third_party/sproink/lib/linux_amd64 -lsproink -lm -ldl -lpthread
#cgo linux,arm64 LDFLAGS: -L${SRCDIR}/../../third_party/sproink/lib/linux_arm64 -lsproink -lm -ldl -lpthread
#cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/../../third_party/sproink/lib/darwin_amd64 -lsproink -lm -ldl -lpthread
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../../third_party/sproink/lib/darwin_arm64 -lsproink -lm -ldl -lpthread
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../../third_party/sproink/lib/windows_amd64 -lsproink -lws2_32 -luserenv -lbcrypt
#include "sproink.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// sproinkGraphBuild creates a CSR graph from parallel edge arrays.
// Returns nil if the C library fails to allocate.
func sproinkGraphBuild(numNodes, numEdges uint32, sources, targets []uint32, weights []float64, kinds []uint8) (*C.SproinkGraph, error) {
	var srcPtr, tgtPtr *C.uint32_t
	var wPtr *C.double
	var kPtr *C.uint8_t

	if numEdges > 0 {
		srcPtr = (*C.uint32_t)(unsafe.Pointer(&sources[0]))
		tgtPtr = (*C.uint32_t)(unsafe.Pointer(&targets[0]))
		wPtr = (*C.double)(unsafe.Pointer(&weights[0]))
		kPtr = (*C.uint8_t)(unsafe.Pointer(&kinds[0]))
	}

	graph := C.sproink_graph_build(
		C.uint32_t(numNodes),
		C.uint32_t(numEdges),
		srcPtr, tgtPtr, wPtr, kPtr,
	)
	if graph == nil {
		return nil, fmt.Errorf("sproink_graph_build returned nil")
	}
	return graph, nil
}

// sproinkGraphFree frees a graph returned by sproinkGraphBuild. Safe to call with nil.
func sproinkGraphFree(graph *C.SproinkGraph) {
	C.sproink_graph_free(graph)
}

// sproinkActivate runs spreading activation on the graph.
func sproinkActivate(
	graph *C.SproinkGraph,
	seedNodes []uint32,
	seedActivations []float64,
	maxSteps uint32,
	decayFactor, spreadFactor, minActivation float64,
	sigmoidGain, sigmoidCenter float64,
	inhibitionEnabled bool,
	inhibitionStrength float64,
	inhibitionBreadth uint32,
) (*C.SproinkResults, error) {
	numSeeds := len(seedNodes)
	var snPtr *C.uint32_t
	var saPtr *C.double
	if numSeeds > 0 {
		snPtr = (*C.uint32_t)(unsafe.Pointer(&seedNodes[0]))
		saPtr = (*C.double)(unsafe.Pointer(&seedActivations[0]))
	}

	results := C.sproink_activate(
		graph,
		C.uint32_t(numSeeds),
		snPtr, saPtr,
		C.uint32_t(maxSteps),
		C.double(decayFactor),
		C.double(spreadFactor),
		C.double(minActivation),
		C.double(sigmoidGain),
		C.double(sigmoidCenter),
		C.bool(inhibitionEnabled),
		C.double(inhibitionStrength),
		C.uint32_t(inhibitionBreadth),
	)
	if results == nil {
		return nil, fmt.Errorf("sproink_activate returned nil")
	}
	return results, nil
}

// sproinkResultsLen returns the number of results, or 0 if results is nil.
func sproinkResultsLen(results *C.SproinkResults) uint32 {
	return uint32(C.sproink_results_len(results))
}

// sproinkResultsNodes copies result node IDs into a Go slice.
func sproinkResultsNodes(results *C.SproinkResults) []uint32 {
	n := sproinkResultsLen(results)
	if n == 0 {
		return nil
	}
	nodes := make([]uint32, n)
	C.sproink_results_nodes(results, (*C.uint32_t)(unsafe.Pointer(&nodes[0])))
	return nodes
}

// sproinkResultsActivations copies result activation values into a Go slice.
func sproinkResultsActivations(results *C.SproinkResults) []float64 {
	n := sproinkResultsLen(results)
	if n == 0 {
		return nil
	}
	activations := make([]float64, n)
	C.sproink_results_activations(results, (*C.double)(unsafe.Pointer(&activations[0])))
	return activations
}

// sproinkResultsDistances copies result hop distances into a Go slice.
func sproinkResultsDistances(results *C.SproinkResults) []uint32 {
	n := sproinkResultsLen(results)
	if n == 0 {
		return nil
	}
	distances := make([]uint32, n)
	C.sproink_results_distances(results, (*C.uint32_t)(unsafe.Pointer(&distances[0])))
	return distances
}

// sproinkResultsFree frees results returned by sproinkActivate. Safe to call with nil.
func sproinkResultsFree(results *C.SproinkResults) {
	C.sproink_results_free(results)
}

// sproinkExtractPairs extracts co-activation pairs from results.
func sproinkExtractPairs(
	results *C.SproinkResults,
	numSeeds uint32,
	seedNodes []uint32,
	activationThreshold float64,
) (*C.SproinkPairs, error) {
	var snPtr *C.uint32_t
	if numSeeds > 0 {
		snPtr = (*C.uint32_t)(unsafe.Pointer(&seedNodes[0]))
	}

	pairs := C.sproink_extract_pairs(
		results,
		C.uint32_t(numSeeds),
		snPtr,
		C.double(activationThreshold),
	)
	if pairs == nil {
		return nil, fmt.Errorf("sproink_extract_pairs returned nil")
	}
	return pairs, nil
}

// sproinkPairsLen returns the number of pairs, or 0 if pairs is nil.
func sproinkPairsLen(pairs *C.SproinkPairs) uint32 {
	return uint32(C.sproink_pairs_len(pairs))
}

// sproinkPairsData copies pair node IDs and activations into Go slices.
// sproink_pairs_nodes takes two separate out-pointers (out_a, out_b).
func sproinkPairsData(pairs *C.SproinkPairs) (nodesA, nodesB []uint32, activationsA, activationsB []float64) {
	n := sproinkPairsLen(pairs)
	if n == 0 {
		return nil, nil, nil, nil
	}

	nodesA = make([]uint32, n)
	nodesB = make([]uint32, n)
	C.sproink_pairs_nodes(
		pairs,
		(*C.uint32_t)(unsafe.Pointer(&nodesA[0])),
		(*C.uint32_t)(unsafe.Pointer(&nodesB[0])),
	)

	activationsA = make([]float64, n)
	activationsB = make([]float64, n)
	C.sproink_pairs_activations(
		pairs,
		(*C.double)(unsafe.Pointer(&activationsA[0])),
		(*C.double)(unsafe.Pointer(&activationsB[0])),
	)

	return nodesA, nodesB, activationsA, activationsB
}

// sproinkPairsFree frees pairs returned by sproinkExtractPairs. Safe to call with nil.
func sproinkPairsFree(pairs *C.SproinkPairs) {
	C.sproink_pairs_free(pairs)
}

// sproinkOjaUpdate computes a single Oja weight update.
func sproinkOjaUpdate(currentWeight, activationA, activationB, learningRate, minWeight, maxWeight float64) float64 {
	return float64(C.sproink_oja_update(
		C.double(currentWeight),
		C.double(activationA),
		C.double(activationB),
		C.double(learningRate),
		C.double(minWeight),
		C.double(maxWeight),
	))
}
