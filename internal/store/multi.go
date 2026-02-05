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
type MultiGraphStore struct {
	mu          sync.RWMutex
	localStore  GraphStore
	globalStore GraphStore
	writeScope  StoreScope // Controls where AddNode writes
}

// NewMultiGraphStore creates a MultiGraphStore with local and global stores.
// projectRoot is used for the local store path.
// writeScope controls where new nodes are written (ScopeLocal, ScopeGlobal, or ScopeBoth).
func NewMultiGraphStore(projectRoot string, writeScope StoreScope) (*MultiGraphStore, error) {
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
		writeScope:  writeScope,
	}, nil
}

// AddNode adds a node to the store(s) based on writeScope.
// Sets metadata["scope"] to track origin.
func (m *MultiGraphStore) AddNode(ctx context.Context, node Node) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure metadata map exists
	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}

	switch m.writeScope {
	case ScopeLocal:
		node.Metadata["scope"] = string(constants.ScopeLocal)
		return m.localStore.AddNode(ctx, node)
	case ScopeGlobal:
		node.Metadata["scope"] = string(constants.ScopeGlobal)
		return m.globalStore.AddNode(ctx, node)
	case ScopeBoth:
		node.Metadata["scope"] = string(constants.ScopeBoth)
		// Write to local first
		id, err := m.localStore.AddNode(ctx, node)
		if err != nil {
			return "", fmt.Errorf("failed to add to local store: %w", err)
		}
		// Then write to global
		if _, err := m.globalStore.AddNode(ctx, node); err != nil {
			return "", fmt.Errorf("failed to add to global store: %w", err)
		}
		return id, nil
	default:
		return "", fmt.Errorf("invalid write scope: %s", m.writeScope)
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

// AddEdge adds an edge to the store containing the source node.
func (m *MultiGraphStore) AddEdge(ctx context.Context, edge Edge) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find which store has the source node
	localNode, err := m.localStore.GetNode(ctx, edge.Source)
	if err != nil {
		return fmt.Errorf("error checking local store: %w", err)
	}
	if localNode != nil {
		return m.localStore.AddEdge(ctx, edge)
	}

	// Try global
	globalNode, err := m.globalStore.GetNode(ctx, edge.Source)
	if err != nil {
		return fmt.Errorf("error checking global store: %w", err)
	}
	if globalNode != nil {
		return m.globalStore.AddEdge(ctx, edge)
	}

	return fmt.Errorf("source node not found in either store: %s", edge.Source)
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
