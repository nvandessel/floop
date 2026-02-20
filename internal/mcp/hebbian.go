package mcp

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// coActivationTracker tracks co-activation counts between behavior pairs.
// It gates edge creation: a new co-activated edge is only created after
// CreationGate co-occurrences within CreationWindow.
//
// When a CoActivationStore is provided, entries are persisted to SQLite and
// survive across MCP server restarts. Otherwise, falls back to in-memory only.
type coActivationTracker struct {
	mu      sync.Mutex
	store   store.CoActivationStore // nil = in-memory fallback
	entries map[string][]time.Time  // used when store is nil
}

// newCoActivationTracker creates an in-memory co-activation tracker.
func newCoActivationTracker() *coActivationTracker {
	return &coActivationTracker{
		entries: make(map[string][]time.Time),
	}
}

// newPersistentCoActivationTracker creates a co-activation tracker backed by SQLite.
func newPersistentCoActivationTracker(s store.CoActivationStore) *coActivationTracker {
	return &coActivationTracker{
		store: s,
	}
}

// initCoActivationTracker creates a persistent tracker if the local store supports
// CoActivationStore, otherwise falls back to in-memory.
func initCoActivationTracker(graphStore *store.MultiGraphStore) *coActivationTracker {
	localStore := graphStore.LocalStore()
	if coStore, ok := localStore.(store.CoActivationStore); ok {
		return newPersistentCoActivationTracker(coStore)
	}
	return newCoActivationTracker()
}

// pairKey returns a canonical key for a behavior pair.
// Assumes behaviorA < behaviorB (caller ensures canonical ordering).
func pairKey(behaviorA, behaviorB string) string {
	return behaviorA + ":" + behaviorB
}

// record records a co-activation and returns whether the pair has met the
// creation gate threshold within the creation window.
func (t *coActivationTracker) record(pair spreading.CoActivationPair, cfg spreading.HebbianConfig) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := pairKey(pair.BehaviorA, pair.BehaviorB)
	now := time.Now()

	// Persistent path: write through to SQLite and query count.
	if t.store != nil {
		ctx := context.Background()
		if err := t.store.RecordCoActivation(ctx, key, now); err != nil {
			fmt.Fprintf(os.Stderr, "warning: co-activation: record: %v\n", err)
			return false
		}
		cutoff := now.Add(-cfg.CreationWindow)
		times, err := t.store.GetCoActivations(ctx, key, cutoff)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: co-activation: get: %v\n", err)
			return false
		}
		return len(times) >= cfg.CreationGate
	}

	// In-memory fallback path.
	entries := t.entries[key]
	cutoff := now.Add(-cfg.CreationWindow)
	fresh := make([]time.Time, 0, len(entries)+1)
	for _, ts := range entries {
		if ts.After(cutoff) {
			fresh = append(fresh, ts)
		}
	}
	fresh = append(fresh, now)
	t.entries[key] = fresh

	return len(fresh) >= cfg.CreationGate
}

// applyHebbianUpdates processes co-activation pairs from a single floop_active
// call. For each pair:
//   - If no co-activated edge exists and the creation gate hasn't been met,
//     just record the co-occurrence.
//   - If the creation gate is met, create the edge with initial weight.
//   - If the edge already exists, apply Oja's rule to update the weight.
//
// After all updates, prune edges whose weight has decayed below MinWeight.
func (s *Server) applyHebbianUpdates(
	ctx context.Context,
	pairs []spreading.CoActivationPair,
	cfg spreading.HebbianConfig,
) {
	if len(pairs) == 0 {
		return
	}

	// Duck-typed interfaces for batch operations
	type edgeWeightUpdater interface {
		BatchUpdateEdgeWeights(ctx context.Context, updates []store.EdgeWeightUpdate) error
	}
	type edgePruner interface {
		PruneWeakEdges(ctx context.Context, kind string, threshold float64) (int, error)
	}

	var weightUpdates []store.EdgeWeightUpdate

	for _, pair := range pairs {
		// Check if edge already exists
		edges, err := s.store.GetEdges(ctx, pair.BehaviorA, store.DirectionOutbound, constants.CoActivatedEdgeKind)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: hebbian: get edges for %s: %v\n", pair.BehaviorA, err)
			continue
		}

		var existing *store.Edge
		for i := range edges {
			if edges[i].Target == pair.BehaviorB {
				existing = &edges[i]
				break
			}
		}

		if existing != nil {
			// Edge exists — apply Oja update
			newWeight := spreading.OjaUpdate(existing.Weight, pair.ActivationA, pair.ActivationB, cfg)
			weightUpdates = append(weightUpdates, store.EdgeWeightUpdate{
				Source:    pair.BehaviorA,
				Target:    pair.BehaviorB,
				Kind:      constants.CoActivatedEdgeKind,
				NewWeight: newWeight,
			})
		} else {
			// No edge yet — check creation gate
			if s.coActivationTracker.record(pair, cfg) {
				// Gate met — create new edge with initial weight from Oja
				initialWeight := spreading.OjaUpdate(0.1, pair.ActivationA, pair.ActivationB, cfg)
				err := s.store.AddEdge(ctx, store.Edge{
					Source:    pair.BehaviorA,
					Target:    pair.BehaviorB,
					Kind:      constants.CoActivatedEdgeKind,
					Weight:    initialWeight,
					CreatedAt: time.Now(),
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: hebbian: create edge %s→%s: %v\n",
						pair.BehaviorA, pair.BehaviorB, err)
				}
			}
		}
	}

	// Batch update existing edge weights
	if len(weightUpdates) > 0 {
		if updater, ok := s.store.(edgeWeightUpdater); ok {
			if err := updater.BatchUpdateEdgeWeights(ctx, weightUpdates); err != nil {
				fmt.Fprintf(os.Stderr, "warning: hebbian: batch update weights: %v\n", err)
			}
		}
	}

	// Prune edges whose weight has decayed below MinWeight
	if pruner, ok := s.store.(edgePruner); ok {
		if n, err := pruner.PruneWeakEdges(ctx, constants.CoActivatedEdgeKind, cfg.MinWeight); err != nil {
			fmt.Fprintf(os.Stderr, "warning: hebbian: prune weak edges: %v\n", err)
		} else if n > 0 {
			fmt.Fprintf(os.Stderr, "floop: pruned %d weak co-activated edge(s)\n", n)
		}
	}
}
