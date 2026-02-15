// Package store provides graph storage implementations.
package store

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileGraphStore implements GraphStore using JSONL files for persistence.
// It stores nodes and edges in .floop/nodes.jsonl and .floop/edges.jsonl.
// Thread-safe for concurrent access.
type FileGraphStore struct {
	mu        sync.RWMutex
	floopDir  string
	nodesFile string
	edgesFile string

	// In-memory cache, synced to disk on Sync() or Close()
	nodes map[string]Node
	edges []Edge
	dirty bool // tracks if there are unsaved changes

	// LoadErrors tracks any errors encountered while loading data.
	// Malformed lines are skipped but recorded here for debugging.
	LoadErrors []LoadError
}

// LoadError represents an error encountered while loading data from disk.
type LoadError struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
	Error   string `json:"error"`
}

// NewFileGraphStore creates a new FileGraphStore rooted at projectRoot.
// It loads existing data from .floop/nodes.jsonl and .floop/edges.jsonl.
func NewFileGraphStore(projectRoot string) (*FileGraphStore, error) {
	floopDir := filepath.Join(projectRoot, ".floop")

	// Ensure .floop directory exists
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create .floop directory: %w", err)
	}

	s := &FileGraphStore{
		floopDir:   floopDir,
		nodesFile:  filepath.Join(floopDir, "nodes.jsonl"),
		edgesFile:  filepath.Join(floopDir, "edges.jsonl"),
		nodes:      make(map[string]Node),
		edges:      make([]Edge, 0),
		LoadErrors: make([]LoadError, 0),
	}

	// Load existing data
	if err := s.loadNodes(); err != nil {
		return nil, fmt.Errorf("failed to load nodes: %w", err)
	}
	if err := s.loadEdges(); err != nil {
		return nil, fmt.Errorf("failed to load edges: %w", err)
	}

	return s, nil
}

// loadNodes reads nodes from the JSONL file into memory.
func (s *FileGraphStore) loadNodes() error {
	f, err := os.Open(s.nodesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file yet is fine
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}
		var node Node
		if err := json.Unmarshal([]byte(line), &node); err != nil {
			// Record the error but continue loading
			s.LoadErrors = append(s.LoadErrors, LoadError{
				File:    s.nodesFile,
				Line:    lineNum,
				Content: truncateForError(line),
				Error:   err.Error(),
			})
			continue
		}
		s.nodes[node.ID] = node
	}
	return scanner.Err()
}

// loadEdges reads edges from the JSONL file into memory.
func (s *FileGraphStore) loadEdges() error {
	f, err := os.Open(s.edgesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file yet is fine
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}
		var edge Edge
		if err := json.Unmarshal([]byte(line), &edge); err != nil {
			// Record the error but continue loading
			s.LoadErrors = append(s.LoadErrors, LoadError{
				File:    s.edgesFile,
				Line:    lineNum,
				Content: truncateForError(line),
				Error:   err.Error(),
			})
			continue
		}
		s.edges = append(s.edges, edge)
	}
	return scanner.Err()
}

// AddNode adds a node to the store.
func (s *FileGraphStore) AddNode(ctx context.Context, node Node) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if node.ID == "" {
		return "", fmt.Errorf("node ID is required")
	}

	s.nodes[node.ID] = node
	s.dirty = true
	return node.ID, nil
}

// UpdateNode updates an existing node in the store.
func (s *FileGraphStore) UpdateNode(ctx context.Context, node Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.nodes[node.ID]; !exists {
		return fmt.Errorf("node not found: %s", node.ID)
	}

	s.nodes[node.ID] = node
	s.dirty = true
	return nil
}

// GetNode retrieves a node by ID. Returns nil if not found.
func (s *FileGraphStore) GetNode(ctx context.Context, id string) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, exists := s.nodes[id]
	if !exists {
		return nil, nil
	}
	return &node, nil
}

// DeleteNode removes a node and its associated edges.
func (s *FileGraphStore) DeleteNode(ctx context.Context, id string) error {
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
	s.dirty = true

	return nil
}

// QueryNodes returns nodes matching the predicate.
func (s *FileGraphStore) QueryNodes(ctx context.Context, predicate map[string]interface{}) ([]Node, error) {
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
func (s *FileGraphStore) AddEdge(ctx context.Context, edge Edge) error {
	if edge.Weight <= 0 || edge.Weight > 1.0 {
		return fmt.Errorf("edge weight must be in (0.0, 1.0], got %f", edge.Weight)
	}
	if edge.CreatedAt.IsZero() {
		return fmt.Errorf("edge CreatedAt must be set")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.edges = append(s.edges, edge)
	s.dirty = true
	return nil
}

// RemoveEdge removes an edge matching source, target, and kind.
func (s *FileGraphStore) RemoveEdge(ctx context.Context, source, target, kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]Edge, 0, len(s.edges))
	for _, e := range s.edges {
		if !(e.Source == source && e.Target == target && e.Kind == kind) {
			filtered = append(filtered, e)
		}
	}
	s.edges = filtered
	s.dirty = true
	return nil
}

// GetEdges returns edges connected to a node.
func (s *FileGraphStore) GetEdges(ctx context.Context, nodeID string, direction Direction, kind string) ([]Edge, error) {
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
func (s *FileGraphStore) Traverse(ctx context.Context, start string, edgeKinds []string, direction Direction, maxDepth int) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	visited := make(map[string]bool)
	results := make([]Node, 0)

	s.traverseRecursive(start, edgeKinds, direction, maxDepth, 0, visited, &results)

	return results, nil
}

func (s *FileGraphStore) traverseRecursive(current string, edgeKinds []string, direction Direction, maxDepth, depth int, visited map[string]bool, results *[]Node) {
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

// Sync writes all changes to disk.
func (s *FileGraphStore) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.dirty {
		return nil
	}

	// Write nodes
	if err := s.writeNodes(); err != nil {
		return fmt.Errorf("failed to write nodes: %w", err)
	}

	// Write edges
	if err := s.writeEdges(); err != nil {
		return fmt.Errorf("failed to write edges: %w", err)
	}

	s.dirty = false
	return nil
}

// writeNodes writes all nodes to the JSONL file.
func (s *FileGraphStore) writeNodes() error {
	f, err := os.Create(s.nodesFile)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	for _, node := range s.nodes {
		if err := encoder.Encode(node); err != nil {
			return err
		}
	}
	return nil
}

// writeEdges writes all edges to the JSONL file.
func (s *FileGraphStore) writeEdges() error {
	f, err := os.Create(s.edgesFile)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	for _, edge := range s.edges {
		if err := encoder.Encode(edge); err != nil {
			return err
		}
	}
	return nil
}

// Close syncs and closes the store.
func (s *FileGraphStore) Close() error {
	return s.Sync(context.Background())
}

// truncateForError truncates a string for error reporting to avoid huge messages.
func truncateForError(s string) string {
	const maxLen = 100
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
