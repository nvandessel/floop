package store

import (
	"context"
	"fmt"
	"sync"
)

// embeddingEntry stores an embedding and the model that produced it.
type embeddingEntry struct {
	embedding []float32
	modelName string
}

// InMemoryGraphStore implements GraphStore for testing and development.
type InMemoryGraphStore struct {
	mu         sync.RWMutex
	nodes      map[string]Node
	edges      []Edge
	embeddings map[string]embeddingEntry
}

// NewInMemoryGraphStore creates a new in-memory store.
func NewInMemoryGraphStore() *InMemoryGraphStore {
	return &InMemoryGraphStore{
		nodes:      make(map[string]Node),
		edges:      make([]Edge, 0),
		embeddings: make(map[string]embeddingEntry),
	}
}

// AddNode adds a node to the store.
func (s *InMemoryGraphStore) AddNode(ctx context.Context, node Node) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if node.ID == "" {
		return "", fmt.Errorf("node ID is required")
	}

	s.nodes[node.ID] = node
	return node.ID, nil
}

// UpdateNode updates an existing node in the store.
func (s *InMemoryGraphStore) UpdateNode(ctx context.Context, node Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.nodes[node.ID]; !exists {
		return fmt.Errorf("node not found: %s", node.ID)
	}

	s.nodes[node.ID] = node
	return nil
}

// GetNode retrieves a node by ID. Returns nil if not found.
func (s *InMemoryGraphStore) GetNode(ctx context.Context, id string) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, exists := s.nodes[id]
	if !exists {
		return nil, nil
	}
	return &node, nil
}

// DeleteNode removes a node and its associated edges.
func (s *InMemoryGraphStore) DeleteNode(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.nodes, id)

	// Remove edges involving this node
	filtered := make([]Edge, 0, len(s.edges))
	for _, e := range s.edges {
		if e.Source != id && e.Target != id {
			filtered = append(filtered, e)
		}
	}
	s.edges = filtered

	return nil
}

// QueryNodes returns nodes matching the predicate.
func (s *InMemoryGraphStore) QueryNodes(ctx context.Context, predicate map[string]interface{}) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]Node, 0)
	for _, node := range s.nodes {
		if matchesPredicate(node, predicate) {
			results = append(results, node)
		}
	}
	return results, nil
}

// AddEdge adds an edge to the store.
// Weight must be in (0.0, 1.0] and CreatedAt must be non-zero.
func (s *InMemoryGraphStore) AddEdge(ctx context.Context, edge Edge) error {
	if edge.Weight <= 0 || edge.Weight > 1.0 {
		return fmt.Errorf("edge weight must be in (0.0, 1.0], got %f", edge.Weight)
	}
	if edge.CreatedAt.IsZero() {
		return fmt.Errorf("edge CreatedAt must be set")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.edges = append(s.edges, edge)
	return nil
}

// RemoveEdge removes an edge matching source, target, and kind.
func (s *InMemoryGraphStore) RemoveEdge(ctx context.Context, source, target, kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]Edge, 0, len(s.edges))
	for _, e := range s.edges {
		if !(e.Source == source && e.Target == target && e.Kind == kind) {
			filtered = append(filtered, e)
		}
	}
	s.edges = filtered
	return nil
}

// GetEdges returns edges connected to a node.
func (s *InMemoryGraphStore) GetEdges(ctx context.Context, nodeID string, direction Direction, kind string) ([]Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]Edge, 0)
	for _, e := range s.edges {
		if kind != "" && e.Kind != kind {
			continue
		}

		switch direction {
		case DirectionOutbound:
			if e.Source == nodeID {
				results = append(results, e)
			}
		case DirectionInbound:
			if e.Target == nodeID {
				results = append(results, e)
			}
		case DirectionBoth:
			if e.Source == nodeID || e.Target == nodeID {
				results = append(results, e)
			}
		}
	}
	return results, nil
}

// Traverse returns all nodes reachable from start by following edges of the given kinds.
func (s *InMemoryGraphStore) Traverse(ctx context.Context, start string, edgeKinds []string, direction Direction, maxDepth int) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	visited := make(map[string]bool)
	results := make([]Node, 0)

	s.traverseRecursive(start, edgeKinds, direction, maxDepth, 0, visited, &results)

	return results, nil
}

func (s *InMemoryGraphStore) traverseRecursive(current string, edgeKinds []string, direction Direction, maxDepth, depth int, visited map[string]bool, results *[]Node) {
	if depth > maxDepth || visited[current] {
		return
	}
	visited[current] = true

	if node, exists := s.nodes[current]; exists {
		*results = append(*results, node)
	}

	for _, e := range s.edges {
		if !edgeKindMatches(e.Kind, edgeKinds) {
			continue
		}

		var next string
		switch direction {
		case DirectionOutbound:
			if e.Source == current {
				next = e.Target
			}
		case DirectionInbound:
			if e.Target == current {
				next = e.Source
			}
		case DirectionBoth:
			if e.Source == current {
				next = e.Target
			} else if e.Target == current {
				next = e.Source
			}
		}

		if next != "" {
			s.traverseRecursive(next, edgeKinds, direction, maxDepth, depth+1, visited, results)
		}
	}
}

// StoreEmbedding stores an embedding vector for a behavior.
func (s *InMemoryGraphStore) StoreEmbedding(ctx context.Context, behaviorID string, embedding []float32, modelName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.nodes[behaviorID]; !exists {
		return fmt.Errorf("behavior not found: %s", behaviorID)
	}

	s.embeddings[behaviorID] = embeddingEntry{
		embedding: embedding,
		modelName: modelName,
	}
	return nil
}

// GetAllEmbeddings returns all behaviors that have embeddings.
func (s *InMemoryGraphStore) GetAllEmbeddings(ctx context.Context) ([]BehaviorEmbedding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []BehaviorEmbedding
	for id, entry := range s.embeddings {
		results = append(results, BehaviorEmbedding{
			BehaviorID: id,
			Embedding:  entry.embedding,
		})
	}
	return results, nil
}

// GetBehaviorIDsWithoutEmbeddings returns IDs of behaviors that do not have embeddings.
func (s *InMemoryGraphStore) GetBehaviorIDsWithoutEmbeddings(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ids []string
	for id, node := range s.nodes {
		if node.Kind == "behavior" {
			if _, has := s.embeddings[id]; !has {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

// Sync is a no-op for in-memory storage.
func (s *InMemoryGraphStore) Sync(ctx context.Context) error {
	return nil
}

// Close is a no-op for in-memory storage.
func (s *InMemoryGraphStore) Close() error {
	return nil
}

// matchesPredicate checks if a node matches a predicate.
func matchesPredicate(node Node, predicate map[string]interface{}) bool {
	for key, required := range predicate {
		var actual interface{}

		switch key {
		case "kind":
			actual = node.Kind
		case "id":
			actual = node.ID
		default:
			// Check content first, then metadata
			if val, ok := node.Content[key]; ok {
				actual = val
			} else if val, ok := node.Metadata[key]; ok {
				actual = val
			}
		}

		if actual != required {
			return false
		}
	}
	return true
}

// edgeKindMatches checks if an edge kind is in the allowed list.
func edgeKindMatches(kind string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, k := range allowed {
		if k == kind {
			return true
		}
	}
	return false
}
