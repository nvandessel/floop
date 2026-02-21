// Package store provides graph storage implementations.
package store

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/nvandessel/feedback-loop/internal/constants"
)

// StoreScope is an alias for constants.Scope for backward compatibility.
type StoreScope = constants.Scope

const (
	// ScopeLocal means operations target only the local project store.
	ScopeLocal = constants.ScopeLocal
	// ScopeGlobal means operations target only the global user store.
	ScopeGlobal = constants.ScopeGlobal
	// ScopeBoth means operations target both stores.
	ScopeBoth = constants.ScopeBoth
)

// MultiGraphStore implements GraphStore by wrapping two SQLiteGraphStore instances:
// one for local project behaviors (./.floop/) and one for global user behaviors (~/.floop/).
// Thread-safe through delegation to thread-safe underlying stores.
//
// AddNode defaults to the global store. Use AddNodeToScope for explicit routing.
type MultiGraphStore struct {
	mu          sync.RWMutex
	localStore  GraphStore
	globalStore GraphStore
}

// NewMultiGraphStore creates a MultiGraphStore with local and global stores.
// projectRoot is used for the local store path.
// AddNode defaults to global; use AddNodeToScope for explicit routing.
func NewMultiGraphStore(projectRoot string) (*MultiGraphStore, error) {
	// Create local store (SQLite-backed with JSONL export)
	localStore, err := NewSQLiteGraphStore(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create local store: %w", err)
	}

	// Create global store (at $HOME/.floop/)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	globalStore, err := NewSQLiteGraphStore(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create global store: %w", err)
	}

	return &MultiGraphStore{
		localStore:  localStore,
		globalStore: globalStore,
	}, nil
}

// AddNode adds a node to the global store.
// Sets metadata["scope"] to "global". Use AddNodeToScope for explicit routing.
func (m *MultiGraphStore) AddNode(ctx context.Context, node Node) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}
	node.Metadata["scope"] = string(constants.ScopeGlobal)
	return m.globalStore.AddNode(ctx, node)
}

// AddNodeToScope adds a node to the specified scope (local or global).
// ScopeBoth is not a valid write scope — each behavior belongs to exactly one store.
func (m *MultiGraphStore) AddNodeToScope(ctx context.Context, node Node, scope StoreScope) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}

	switch scope {
	case ScopeLocal:
		node.Metadata["scope"] = string(constants.ScopeLocal)
		return m.localStore.AddNode(ctx, node)
	case ScopeGlobal:
		node.Metadata["scope"] = string(constants.ScopeGlobal)
		return m.globalStore.AddNode(ctx, node)
	default:
		return "", fmt.Errorf("invalid write scope: %s (use ScopeLocal or ScopeGlobal)", scope)
	}
}

// UpdateNode updates a node in whichever store contains it.
func (m *MultiGraphStore) UpdateNode(ctx context.Context, node Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Try local first
	localNode, err := m.localStore.GetNode(ctx, node.ID)
	if err != nil {
		return fmt.Errorf("error checking local store: %w", err)
	}
	if localNode != nil {
		return m.localStore.UpdateNode(ctx, node)
	}

	// Try global
	globalNode, err := m.globalStore.GetNode(ctx, node.ID)
	if err != nil {
		return fmt.Errorf("error checking global store: %w", err)
	}
	if globalNode != nil {
		return m.globalStore.UpdateNode(ctx, node)
	}

	return fmt.Errorf("node not found in either store: %s", node.ID)
}

// GetNode retrieves a node by ID, checking local first, then global.
func (m *MultiGraphStore) GetNode(ctx context.Context, id string) (*Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check local first
	node, err := m.localStore.GetNode(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error checking local store: %w", err)
	}
	if node != nil {
		return node, nil
	}

	// Fallback to global
	node, err = m.globalStore.GetNode(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error checking global store: %w", err)
	}
	return node, nil
}

// DeleteNode removes a node from both stores (idempotent).
func (m *MultiGraphStore) DeleteNode(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Delete from both stores, ignoring "not found" errors
	localErr := m.localStore.DeleteNode(ctx, id)
	globalErr := m.globalStore.DeleteNode(ctx, id)

	// Only return error if both failed with actual errors
	if localErr != nil && globalErr != nil {
		return fmt.Errorf("failed to delete from both stores: local=%v, global=%v", localErr, globalErr)
	}

	return nil
}

// QueryNodes queries both stores and merges results, with local winning on conflicts.
func (m *MultiGraphStore) QueryNodes(ctx context.Context, predicate map[string]interface{}) ([]Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Query both stores in parallel
	type result struct {
		nodes []Node
		err   error
	}

	localChan := make(chan result, 1)
	globalChan := make(chan result, 1)

	go func() {
		nodes, err := m.localStore.QueryNodes(ctx, predicate)
		localChan <- result{nodes, err}
	}()

	go func() {
		nodes, err := m.globalStore.QueryNodes(ctx, predicate)
		globalChan <- result{nodes, err}
	}()

	localResult := <-localChan
	globalResult := <-globalChan

	if localResult.err != nil {
		return nil, fmt.Errorf("local query failed: %w", localResult.err)
	}
	if globalResult.err != nil {
		return nil, fmt.Errorf("global query failed: %w", globalResult.err)
	}

	return mergeNodes(localResult.nodes, globalResult.nodes), nil
}

// AddEdge adds an edge, routing it based on endpoint locations:
//   - Both endpoints in same store → store edge there
//   - Endpoints in different stores → store edge in global store
func (m *MultiGraphStore) AddEdge(ctx context.Context, edge Edge) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Determine which store(s) have source and target
	srcLocal, err := m.localStore.GetNode(ctx, edge.Source)
	if err != nil {
		return fmt.Errorf("error checking local store for source: %w", err)
	}
	srcGlobal, err := m.globalStore.GetNode(ctx, edge.Source)
	if err != nil {
		return fmt.Errorf("error checking global store for source: %w", err)
	}
	tgtLocal, err := m.localStore.GetNode(ctx, edge.Target)
	if err != nil {
		return fmt.Errorf("error checking local store for target: %w", err)
	}
	tgtGlobal, err := m.globalStore.GetNode(ctx, edge.Target)
	if err != nil {
		return fmt.Errorf("error checking global store for target: %w", err)
	}

	srcInLocal := srcLocal != nil
	srcInGlobal := srcGlobal != nil
	tgtInLocal := tgtLocal != nil
	tgtInGlobal := tgtGlobal != nil

	// Both in local → local store
	if srcInLocal && tgtInLocal {
		return m.localStore.AddEdge(ctx, edge)
	}
	// Both in global → global store
	if srcInGlobal && tgtInGlobal {
		return m.globalStore.AddEdge(ctx, edge)
	}
	// Cross-store → global store
	if (srcInLocal || srcInGlobal) && (tgtInLocal || tgtInGlobal) {
		return m.globalStore.AddEdge(ctx, edge)
	}

	return fmt.Errorf("source or target not found in either store: source=%s, target=%s", edge.Source, edge.Target)
}

// RemoveEdge removes an edge from both stores.
func (m *MultiGraphStore) RemoveEdge(ctx context.Context, source, target, kind string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove from both stores, ignoring errors
	localErr := m.localStore.RemoveEdge(ctx, source, target, kind)
	globalErr := m.globalStore.RemoveEdge(ctx, source, target, kind)

	// Only return error if both failed
	if localErr != nil && globalErr != nil {
		return fmt.Errorf("failed to remove from both stores: local=%v, global=%v", localErr, globalErr)
	}

	return nil
}

// GetEdges returns edges from both stores, merged and deduplicated.
func (m *MultiGraphStore) GetEdges(ctx context.Context, nodeID string, direction Direction, kind string) ([]Edge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	localEdges, err := m.localStore.GetEdges(ctx, nodeID, direction, kind)
	if err != nil {
		return nil, fmt.Errorf("local GetEdges failed: %w", err)
	}

	globalEdges, err := m.globalStore.GetEdges(ctx, nodeID, direction, kind)
	if err != nil {
		return nil, fmt.Errorf("global GetEdges failed: %w", err)
	}

	return mergeEdges(localEdges, globalEdges), nil
}

// Traverse traverses the graph starting from a node.
// Currently delegates to local store only for simplicity.
func (m *MultiGraphStore) Traverse(ctx context.Context, start string, edgeKinds []string, direction Direction, maxDepth int) ([]Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check which store has the start node
	localNode, err := m.localStore.GetNode(ctx, start)
	if err != nil {
		return nil, fmt.Errorf("error checking local store: %w", err)
	}
	if localNode != nil {
		return m.localStore.Traverse(ctx, start, edgeKinds, direction, maxDepth)
	}

	// Try global
	globalNode, err := m.globalStore.GetNode(ctx, start)
	if err != nil {
		return nil, fmt.Errorf("error checking global store: %w", err)
	}
	if globalNode != nil {
		return m.globalStore.Traverse(ctx, start, edgeKinds, direction, maxDepth)
	}

	return nil, fmt.Errorf("start node not found in either store: %s", start)
}

// LocalStore returns the local (project-specific) store instance.
// Used for project-scoped data like co-activation tracking.
func (m *MultiGraphStore) LocalStore() GraphStore {
	return m.localStore
}

// GlobalStore returns the global store instance for direct access.
// This is used by the seeder to write seed behaviors to the global store.
func (m *MultiGraphStore) GlobalStore() GraphStore {
	return m.globalStore
}

// Sync syncs both stores to disk.
func (m *MultiGraphStore) Sync(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.localStore.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync local store: %w", err)
	}

	if err := m.globalStore.Sync(ctx); err != nil {
		return fmt.Errorf("failed to sync global store: %w", err)
	}

	return nil
}

// Close syncs and closes both stores.
func (m *MultiGraphStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	localErr := m.localStore.Close()
	globalErr := m.globalStore.Close()

	if localErr != nil && globalErr != nil {
		return fmt.Errorf("failed to close both stores: local=%v, global=%v", localErr, globalErr)
	}
	if localErr != nil {
		return fmt.Errorf("failed to close local store: %w", localErr)
	}
	if globalErr != nil {
		return fmt.Errorf("failed to close global store: %w", globalErr)
	}

	return nil
}

// withExtendedStore finds the store containing the given behavior and calls fn
// with the ExtendedGraphStore that owns it. Tries local first, then global.
// The caller must hold m.mu.
func (m *MultiGraphStore) withExtendedStore(ctx context.Context, behaviorID string, fn func(ExtendedGraphStore) error) error {
	if es, ok := m.localStore.(ExtendedGraphStore); ok {
		node, err := m.localStore.GetNode(ctx, behaviorID)
		if err != nil {
			return fmt.Errorf("error checking local store: %w", err)
		}
		if node != nil {
			return fn(es)
		}
	}

	if es, ok := m.globalStore.(ExtendedGraphStore); ok {
		node, err := m.globalStore.GetNode(ctx, behaviorID)
		if err != nil {
			return fmt.Errorf("error checking global store: %w", err)
		}
		if node != nil {
			return fn(es)
		}
	}

	return fmt.Errorf("behavior not found in either store: %s", behaviorID)
}

// forEachExtendedStore calls fn on each store that implements ExtendedGraphStore.
// The caller must hold m.mu.
func (m *MultiGraphStore) forEachExtendedStore(scope string, fn func(ExtendedGraphStore) error) error {
	if es, ok := m.localStore.(ExtendedGraphStore); ok {
		if err := fn(es); err != nil {
			return fmt.Errorf("local %s: %w", scope, err)
		}
	}
	if es, ok := m.globalStore.(ExtendedGraphStore); ok {
		if err := fn(es); err != nil {
			return fmt.Errorf("global %s: %w", scope, err)
		}
	}
	return nil
}

// UpdateConfidence updates the confidence for a behavior in whichever store contains it.
func (m *MultiGraphStore) UpdateConfidence(ctx context.Context, behaviorID string, newConfidence float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.withExtendedStore(ctx, behaviorID, func(es ExtendedGraphStore) error {
		return es.UpdateConfidence(ctx, behaviorID, newConfidence)
	})
}

// RecordActivationHit delegates to whichever store contains the behavior.
func (m *MultiGraphStore) RecordActivationHit(ctx context.Context, behaviorID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.withExtendedStore(ctx, behaviorID, func(es ExtendedGraphStore) error {
		return es.RecordActivationHit(ctx, behaviorID)
	})
}

// RecordConfirmed delegates to whichever store contains the behavior.
func (m *MultiGraphStore) RecordConfirmed(ctx context.Context, behaviorID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.withExtendedStore(ctx, behaviorID, func(es ExtendedGraphStore) error {
		return es.RecordConfirmed(ctx, behaviorID)
	})
}

// RecordOverridden delegates to whichever store contains the behavior.
func (m *MultiGraphStore) RecordOverridden(ctx context.Context, behaviorID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.withExtendedStore(ctx, behaviorID, func(es ExtendedGraphStore) error {
		return es.RecordOverridden(ctx, behaviorID)
	})
}

// TouchEdges delegates to both stores.
func (m *MultiGraphStore) TouchEdges(ctx context.Context, behaviorIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.forEachExtendedStore("TouchEdges", func(es ExtendedGraphStore) error {
		return es.TouchEdges(ctx, behaviorIDs)
	})
}

// BatchUpdateEdgeWeights delegates to both stores.
func (m *MultiGraphStore) BatchUpdateEdgeWeights(ctx context.Context, updates []EdgeWeightUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.forEachExtendedStore("BatchUpdateEdgeWeights", func(es ExtendedGraphStore) error {
		return es.BatchUpdateEdgeWeights(ctx, updates)
	})
}

// PruneWeakEdges delegates to both stores and returns the total count pruned.
func (m *MultiGraphStore) PruneWeakEdges(ctx context.Context, kind string, threshold float64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := 0
	if es, ok := m.localStore.(ExtendedGraphStore); ok {
		n, err := es.PruneWeakEdges(ctx, kind, threshold)
		if err != nil {
			return 0, fmt.Errorf("local PruneWeakEdges: %w", err)
		}
		total += n
	}
	if es, ok := m.globalStore.(ExtendedGraphStore); ok {
		n, err := es.PruneWeakEdges(ctx, kind, threshold)
		if err != nil {
			return 0, fmt.Errorf("global PruneWeakEdges: %w", err)
		}
		total += n
	}
	return total, nil
}

// ValidateBehaviorGraph validates both stores with cross-store awareness.
// Local store validates with nil externalIDs (all edges should be local-local).
// Global store validates with local IDs as externalIDs (cross-store edges
// reference local behaviors).
// Errors from local store are prefixed with "local:" and global with "global:".
func (m *MultiGraphStore) ValidateBehaviorGraph(ctx context.Context) ([]ValidationError, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect IDs from both stores for cross-store resolution
	var localIDs, globalIDs map[string]bool

	if sqlStore, ok := m.localStore.(*SQLiteGraphStore); ok {
		var err error
		localIDs, err = sqlStore.AllBehaviorIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get local behavior IDs: %w", err)
		}
	}
	if sqlStore, ok := m.globalStore.(*SQLiteGraphStore); ok {
		var err error
		globalIDs, err = sqlStore.AllBehaviorIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get global behavior IDs: %w", err)
		}
	}

	var allErrors []ValidationError

	// Local store: validate with global IDs as external (in case of any cross-store edges)
	if sqlStore, ok := m.localStore.(*SQLiteGraphStore); ok {
		errors, err := sqlStore.ValidateWithExternalIDs(ctx, globalIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to validate local store: %w", err)
		}
		for _, e := range errors {
			e.BehaviorID = "local:" + e.BehaviorID
			allErrors = append(allErrors, e)
		}
	}

	// Global store: validate with local IDs as external (cross-store edges reference local behaviors)
	if sqlStore, ok := m.globalStore.(*SQLiteGraphStore); ok {
		errors, err := sqlStore.ValidateWithExternalIDs(ctx, localIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to validate global store: %w", err)
		}
		for _, e := range errors {
			e.BehaviorID = "global:" + e.BehaviorID
			allErrors = append(allErrors, e)
		}
	}

	return allErrors, nil
}

// StoreEmbedding stores an embedding in whichever store contains the behavior.
func (m *MultiGraphStore) StoreEmbedding(ctx context.Context, behaviorID string, embedding []float32, modelName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.withEmbeddingStore(ctx, behaviorID, func(es EmbeddingStore) error {
		return es.StoreEmbedding(ctx, behaviorID, embedding, modelName)
	})
}

// GetAllEmbeddings returns embeddings from both stores, merged.
func (m *MultiGraphStore) GetAllEmbeddings(ctx context.Context) ([]BehaviorEmbedding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []BehaviorEmbedding
	if es, ok := m.localStore.(EmbeddingStore); ok {
		embeddings, err := es.GetAllEmbeddings(ctx)
		if err != nil {
			return nil, fmt.Errorf("local GetAllEmbeddings: %w", err)
		}
		all = append(all, embeddings...)
	}
	if es, ok := m.globalStore.(EmbeddingStore); ok {
		embeddings, err := es.GetAllEmbeddings(ctx)
		if err != nil {
			return nil, fmt.Errorf("global GetAllEmbeddings: %w", err)
		}
		all = append(all, embeddings...)
	}
	return all, nil
}

// GetBehaviorIDsWithoutEmbeddings returns IDs from both stores, merged.
func (m *MultiGraphStore) GetBehaviorIDsWithoutEmbeddings(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []string
	if es, ok := m.localStore.(EmbeddingStore); ok {
		ids, err := es.GetBehaviorIDsWithoutEmbeddings(ctx)
		if err != nil {
			return nil, fmt.Errorf("local GetBehaviorIDsWithoutEmbeddings: %w", err)
		}
		all = append(all, ids...)
	}
	if es, ok := m.globalStore.(EmbeddingStore); ok {
		ids, err := es.GetBehaviorIDsWithoutEmbeddings(ctx)
		if err != nil {
			return nil, fmt.Errorf("global GetBehaviorIDsWithoutEmbeddings: %w", err)
		}
		all = append(all, ids...)
	}
	return all, nil
}

// withEmbeddingStore finds the store containing the given behavior and calls fn
// with the EmbeddingStore that owns it. Tries local first, then global.
// The caller must hold m.mu.
func (m *MultiGraphStore) withEmbeddingStore(ctx context.Context, behaviorID string, fn func(EmbeddingStore) error) error {
	if es, ok := m.localStore.(EmbeddingStore); ok {
		node, err := m.localStore.GetNode(ctx, behaviorID)
		if err != nil {
			return fmt.Errorf("error checking local store: %w", err)
		}
		if node != nil {
			return fn(es)
		}
	}

	if es, ok := m.globalStore.(EmbeddingStore); ok {
		node, err := m.globalStore.GetNode(ctx, behaviorID)
		if err != nil {
			return fmt.Errorf("error checking global store: %w", err)
		}
		if node != nil {
			return fn(es)
		}
	}

	return fmt.Errorf("behavior not found in either store: %s", behaviorID)
}

// mergeNodes merges two slices of nodes, with local winning on ID conflicts.
func mergeNodes(local, global []Node) []Node {
	// Build map of local IDs
	localIDs := make(map[string]bool)
	for _, node := range local {
		localIDs[node.ID] = true
	}

	// Start with all local nodes
	result := make([]Node, len(local))
	copy(result, local)

	// Add global nodes that don't conflict
	for _, node := range global {
		if !localIDs[node.ID] {
			result = append(result, node)
		}
	}

	return result
}

// mergeEdges merges two slices of edges, removing duplicates.
func mergeEdges(local, global []Edge) []Edge {
	// Use a map to deduplicate
	seen := make(map[string]bool)
	result := make([]Edge, 0, len(local)+len(global))

	for _, edge := range local {
		key := fmt.Sprintf("%s:%s:%s", edge.Source, edge.Target, edge.Kind)
		if !seen[key] {
			seen[key] = true
			result = append(result, edge)
		}
	}

	for _, edge := range global {
		key := fmt.Sprintf("%s:%s:%s", edge.Source, edge.Target, edge.Kind)
		if !seen[key] {
			seen[key] = true
			result = append(result, edge)
		}
	}

	return result
}
