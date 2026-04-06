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
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/nvandessel/floop/internal/constants"
	"github.com/nvandessel/floop/internal/store"
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

// Compile-time check: NativeEngine implements Activator.
var _ Activator = (*NativeEngine)(nil)

// NativeEngine performs spreading activation via the sproink FFI library.
// It owns a CSR graph built from the store's edges and rebuilds it when
// the store version changes.
type NativeEngine struct {
	graph   *C.SproinkGraph
	idmap   *IDMap
	config  Config
	store   store.ExtendedGraphStore
	mu      sync.RWMutex
	version uint64
}

// NewNativeEngine creates a NativeEngine by loading the full graph from the store.
func NewNativeEngine(s store.ExtendedGraphStore, config Config) (*NativeEngine, error) {
	ctx := context.Background()
	graph, idmap, err := loadGraph(ctx, s, config)
	if err != nil {
		return nil, fmt.Errorf("NewNativeEngine: %w", err)
	}
	return &NativeEngine{
		graph:   graph,
		idmap:   idmap,
		config:  config,
		store:   s,
		version: s.Version(), // safe: no concurrent access yet
	}, nil
}

// Activate runs spreading activation from the given seeds via the sproink FFI.
func (e *NativeEngine) Activate(ctx context.Context, seeds []Seed) ([]Result, error) {
	if len(seeds) == 0 {
		return []Result{}, nil
	}

	// Version check before lock — both reads must be atomic.
	if e.store.Version() > atomic.LoadUint64(&e.version) {
		if err := e.Rebuild(ctx); err != nil {
			return nil, fmt.Errorf("NativeEngine.Activate: rebuild: %w", err)
		}
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Map seeds to parallel arrays, dropping unknown BehaviorIDs.
	seedNodes := make([]uint32, 0, len(seeds))
	seedActivations := make([]float64, 0, len(seeds))
	seedSources := make(map[uint32]string, len(seeds))
	for _, s := range seeds {
		id, ok := e.idmap.ToU32(s.BehaviorID)
		if !ok {
			continue
		}
		seedNodes = append(seedNodes, id)
		seedActivations = append(seedActivations, s.Activation)
		seedSources[id] = s.Source
	}

	if len(seedNodes) == 0 {
		return []Result{}, nil
	}

	// Resolve inhibition config.
	inhEnabled := e.config.Inhibition != nil && e.config.Inhibition.Enabled
	var inhStrength float64
	var inhBreadth uint32
	if e.config.Inhibition != nil {
		inhStrength = e.config.Inhibition.Strength
		inhBreadth = uint32(e.config.Inhibition.Breadth)
	}

	results, err := sproinkActivate(
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
		return nil, fmt.Errorf("NativeEngine.Activate: %w", err)
	}
	defer sproinkResultsFree(results)

	n := sproinkResultsLen(results)
	if n == 0 {
		return []Result{}, nil
	}

	nodes := sproinkResultsNodes(results)
	activations := sproinkResultsActivations(results)
	distances := sproinkResultsDistances(results)

	// Build seed lookup for SeedSource attribution.
	seedSet := make(map[uint32]bool, len(seedNodes))
	for _, sn := range seedNodes {
		seedSet[sn] = true
	}

	// Sort seeds by BehaviorID for deterministic alphabetical tiebreak.
	type seedEntry struct {
		behaviorID string
		source     string
	}
	sortedSeeds := make([]seedEntry, 0, len(seedNodes))
	for _, sn := range seedNodes {
		sortedSeeds = append(sortedSeeds, seedEntry{
			behaviorID: e.idmap.ToUUID(sn),
			source:     seedSources[sn],
		})
	}
	sort.Slice(sortedSeeds, func(i, j int) bool {
		return sortedSeeds[i].behaviorID < sortedSeeds[j].behaviorID
	})

	// Build result slice with SeedSource attribution, MinActivation filter.
	out := make([]Result, 0, n)
	for i := uint32(0); i < n; i++ {
		act := activations[i]
		if act < e.config.MinActivation {
			continue
		}

		nodeID := e.idmap.ToUUID(nodes[i])
		dist := int(distances[i])

		// Derive SeedSource: seeds use their own source; non-seeds use
		// alphabetically first seed (tiebreak when per-seed distance is unavailable).
		var source string
		if dist == 0 && seedSet[nodes[i]] {
			source = seedSources[nodes[i]]
		} else {
			source = sortedSeeds[0].source
		}

		out = append(out, Result{
			BehaviorID: nodeID,
			Activation: act,
			Distance:   dist,
			SeedSource: source,
		})
	}

	// Sort by activation descending.
	sort.Slice(out, func(i, j int) bool {
		return out[i].Activation > out[j].Activation
	})

	return out, nil
}

// Rebuild frees the current graph and builds a fresh one from the store.
// Re-checks version after acquiring the lock to avoid thundering herd:
// if N goroutines all observe staleness, only the first actually rebuilds.
func (e *NativeEngine) Rebuild(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Re-check: a concurrent Rebuild may have already updated.
	if e.store.Version() <= atomic.LoadUint64(&e.version) {
		return nil
	}

	sproinkGraphFree(e.graph)

	graph, idmap, err := loadGraph(ctx, e.store, e.config)
	if err != nil {
		return fmt.Errorf("NativeEngine.Rebuild: %w", err)
	}

	e.graph = graph
	e.idmap = idmap
	atomic.StoreUint64(&e.version, e.store.Version())
	return nil
}

// Close frees the owned graph. Safe to call multiple times.
func (e *NativeEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.graph != nil {
		sproinkGraphFree(e.graph)
		e.graph = nil
	}
	return nil
}
