// Package store defines the GraphStore interface for storing and querying
// the behavior graph.
package store

import (
	"context"
	"time"
)

// Node represents a node in the behavior graph.
type Node struct {
	ID       string                 `json:"id"`
	Kind     string                 `json:"kind"` // "behavior", "correction", "context-snapshot"
	Content  map[string]interface{} `json:"content"`
	Metadata map[string]interface{} `json:"metadata"`
}

// Edge represents a relationship between nodes.
type Edge struct {
	Source        string                 `json:"source"`
	Target        string                 `json:"target"`
	Kind          string                 `json:"kind"`                     // "requires", "overrides", "conflicts", "learned-from", "similar-to"
	Weight        float64                `json:"weight"`                   // 0.0-1.0, activation transmission factor
	CreatedAt     time.Time              `json:"created_at"`               // when edge was created
	LastActivated *time.Time             `json:"last_activated,omitempty"` // when activation last flowed through
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// EdgeWeightUpdate describes a weight update for a specific edge.
type EdgeWeightUpdate struct {
	Source    string  // Source behavior ID
	Target    string  // Target behavior ID
	Kind      string  // Edge kind (e.g., "co-activated")
	NewWeight float64 // Updated weight value
}

// Direction specifies edge traversal direction.
type Direction string

const (
	DirectionOutbound Direction = "outbound" // Follow edges from source to target
	DirectionInbound  Direction = "inbound"  // Follow edges from target to source
	DirectionBoth     Direction = "both"     // Follow edges in both directions
)

// GraphStore defines the interface for storing and querying the behavior graph.
type GraphStore interface {
	// Node operations
	AddNode(ctx context.Context, node Node) (string, error)
	UpdateNode(ctx context.Context, node Node) error
	GetNode(ctx context.Context, id string) (*Node, error)
	DeleteNode(ctx context.Context, id string) error

	// QueryNodes queries nodes by predicate.
	// Predicate is a map of field names to required values.
	// Supports flat key matching only (e.g., "kind", "id").
	// e.g., {"kind": "behavior"}
	QueryNodes(ctx context.Context, predicate map[string]interface{}) ([]Node, error)

	// Edge operations
	AddEdge(ctx context.Context, edge Edge) error
	RemoveEdge(ctx context.Context, source, target, kind string) error
	GetEdges(ctx context.Context, nodeID string, direction Direction, kind string) ([]Edge, error)

	// Traverse returns all nodes reachable from start by following edges of the given kinds.
	Traverse(ctx context.Context, start string, edgeKinds []string, direction Direction, maxDepth int) ([]Node, error)

	// Persistence
	Sync(ctx context.Context) error
	Close() error
}

// ExtendedGraphStore provides additional operations beyond the base GraphStore.
// SQLiteGraphStore implements this interface. MultiGraphStore delegates to it
// via type assertion to avoid coupling the base interface to SQLite-specific features.
type ExtendedGraphStore interface {
	GraphStore

	// UpdateConfidence updates the confidence for a behavior.
	UpdateConfidence(ctx context.Context, behaviorID string, newConfidence float64) error

	// RecordActivationHit records that a behavior was activated.
	RecordActivationHit(ctx context.Context, behaviorID string) error

	// RecordConfirmed records that a behavior was confirmed by the user.
	RecordConfirmed(ctx context.Context, behaviorID string) error

	// RecordOverridden records that a behavior was overridden by the user.
	RecordOverridden(ctx context.Context, behaviorID string) error

	// TouchEdges updates the last_activated timestamp on all edges involving the given behaviors.
	TouchEdges(ctx context.Context, behaviorIDs []string) error

	// BatchUpdateEdgeWeights applies multiple edge weight updates atomically.
	BatchUpdateEdgeWeights(ctx context.Context, updates []EdgeWeightUpdate) error

	// PruneWeakEdges removes edges of the given kind below the weight threshold.
	PruneWeakEdges(ctx context.Context, kind string, threshold float64) (int, error)

	// ValidateBehaviorGraph checks the graph for consistency issues.
	ValidateBehaviorGraph(ctx context.Context) ([]ValidationError, error)
}

// BehaviorEmbedding pairs a behavior ID with its embedding vector.
type BehaviorEmbedding struct {
	BehaviorID string
	Embedding  []float32
}

// EmbeddingStore provides embedding vector persistence.
// SQLiteGraphStore implements this interface. Consumers should type-assert
// to check for support: if es, ok := store.(EmbeddingStore); ok { ... }
type EmbeddingStore interface {
	StoreEmbedding(ctx context.Context, behaviorID string, embedding []float32, modelName string) error
	GetAllEmbeddings(ctx context.Context) ([]BehaviorEmbedding, error)
	GetBehaviorIDsWithoutEmbeddings(ctx context.Context) ([]string, error)
}
