package vectorindex

import (
	"context"
	"path/filepath"
	"sort"
	"sync"

	"github.com/coder/hnsw"
)

const hnswFileName = "hnsw.bin"

// HNSWIndex performs approximate nearest neighbor search using a Hierarchical
// Navigable Small World graph backed by github.com/coder/hnsw.
// Thread-safe. Suitable for large vector counts.
//
// The underlying hnsw.Graph.Delete can leave dangling neighbor pointers that
// cause panics during Search. To work around this, HNSWIndex maintains a
// shadow map of all vectors and rebuilds the graph on mutation operations
// that remove nodes.
type HNSWIndex struct {
	mu      sync.RWMutex
	graph   *hnsw.SavedGraph[string]
	vectors map[string][]float32
}

// HNSWConfig holds configuration parameters for HNSWIndex.
type HNSWConfig struct {
	// Dir is the directory where the HNSW graph is persisted.
	// If empty, the graph is in-memory only and Save is a no-op.
	Dir string

	// M is the maximum number of neighbors per node. Default: 16.
	M int

	// EfSearch is the number of candidates considered during search. Default: 100.
	EfSearch int

	// Ml is the level generation factor. Default: 0.25.
	Ml float64
}

func (c *HNSWConfig) withDefaults() HNSWConfig {
	out := *c
	if out.M == 0 {
		out.M = 16
	}
	if out.EfSearch == 0 {
		out.EfSearch = 100
	}
	if out.Ml == 0 {
		out.Ml = 0.25
	}
	return out
}

// newHNSWGraph creates a fresh hnsw graph with the given config, path, and
// optionally pre-populated nodes.
func newHNSWGraph(cfg HNSWConfig, path string, nodes []hnsw.Node[string]) *hnsw.SavedGraph[string] {
	g := hnsw.NewGraph[string]()
	g.M = cfg.M
	g.EfSearch = cfg.EfSearch
	g.Ml = cfg.Ml
	g.Distance = hnsw.CosineDistance
	if len(nodes) > 0 {
		g.Add(nodes...)
	}
	return &hnsw.SavedGraph[string]{Graph: g, Path: path}
}

// NewHNSWIndex creates an HNSWIndex. If cfg.Dir is non-empty, the graph
// is loaded from (or created at) that directory and persisted on Save.
func NewHNSWIndex(cfg HNSWConfig) (*HNSWIndex, error) {
	cfg = cfg.withDefaults()

	var (
		sg   *hnsw.SavedGraph[string]
		path string
	)

	if cfg.Dir != "" {
		path = filepath.Join(cfg.Dir, hnswFileName)
		loaded, err := hnsw.LoadSavedGraph[string](path)
		if err != nil {
			return nil, err
		}
		sg = loaded
		// Apply configuration to the loaded graph.
		sg.M = cfg.M
		sg.EfSearch = cfg.EfSearch
		sg.Ml = cfg.Ml
		sg.Distance = hnsw.CosineDistance
	} else {
		sg = newHNSWGraph(cfg, "", nil)
	}

	// Build shadow map from loaded graph using exact iteration.
	vecs := make(map[string][]float32, sg.Len())
	for _, n := range sg.All() {
		vecs[n.Key] = n.Value
	}

	return &HNSWIndex{graph: sg, vectors: vecs}, nil
}

// rebuild constructs a fresh HNSW graph from the shadow map.
// Caller must hold h.mu for writing.
func (h *HNSWIndex) rebuild() {
	nodes := make([]hnsw.Node[string], 0, len(h.vectors))
	for k, v := range h.vectors {
		nodes = append(nodes, hnsw.MakeNode(k, v))
	}
	path := h.graph.Path
	g := hnsw.NewGraph[string]()
	g.M = h.graph.M
	g.EfSearch = h.graph.EfSearch
	g.Ml = h.graph.Ml
	g.Distance = hnsw.CosineDistance
	if len(nodes) > 0 {
		g.Add(nodes...)
	}
	h.graph = &hnsw.SavedGraph[string]{Graph: g, Path: path}
}

// Add inserts or replaces the vector for the given behavior ID.
// If the ID already exists, the graph is rebuilt to avoid dangling pointers.
func (h *HNSWIndex) Add(_ context.Context, behaviorID string, vector []float32) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	cp := make([]float32, len(vector))
	copy(cp, vector)

	_, existed := h.vectors[behaviorID]
	h.vectors[behaviorID] = cp

	if existed {
		// Rebuild to safely replace the node.
		h.rebuild()
	} else {
		h.graph.Add(hnsw.MakeNode(behaviorID, cp))
	}

	return nil
}

// Remove deletes the vector for the given behavior ID. No-op if not found.
func (h *HNSWIndex) Remove(_ context.Context, behaviorID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.vectors[behaviorID]; !ok {
		return nil
	}

	delete(h.vectors, behaviorID)
	// Rebuild to avoid dangling neighbor pointers after delete.
	h.rebuild()

	return nil
}

// Search returns the topK most similar vectors to query, sorted by descending score.
// Score is computed as 1.0 - CosineDistance(query, result).
func (h *HNSWIndex) Search(_ context.Context, query []float32, topK int) ([]SearchResult, error) {
	if len(query) == 0 || topK <= 0 {
		return nil, nil
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.graph.Len() == 0 {
		return nil, nil
	}

	nodes := h.graph.Search(query, topK)

	results := make([]SearchResult, 0, len(nodes))
	for _, n := range nodes {
		dist := hnsw.CosineDistance(query, n.Value)
		score := 1.0 - float64(dist)
		results = append(results, SearchResult{
			BehaviorID: n.Key,
			Score:      score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// Len returns the number of vectors in the index.
func (h *HNSWIndex) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.vectors)
}

// Save persists the graph to disk. No-op if Dir was empty at creation time.
func (h *HNSWIndex) Save(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.graph.Path == "" {
		return nil
	}
	return h.graph.Save()
}

// Close saves and releases resources.
func (h *HNSWIndex) Close() error {
	return h.Save(context.Background())
}
