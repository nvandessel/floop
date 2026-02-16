package simulation

import (
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// Scenario defines a complete simulation experiment.
type Scenario struct {
	Name           string
	Behaviors      []BehaviorSpec
	Edges          []EdgeSpec
	Sessions       []SessionContext
	SpreadConfig   *spreading.Config
	HebbianConfig  *spreading.HebbianConfig
	TokenBudget    int // 0 = skip tiering
	HebbianEnabled bool
	CreateEdges    bool // When true, Hebbian creates new co-activated edges for novel pairs

	// SeedOverride, when non-nil, is called with the session index to produce
	// seeds directly, bypassing the real SeedSelector. Use this for scenarios
	// that need deterministic seed control.
	SeedOverride func(sessionIndex int) []spreading.Seed

	// BeforeSession, when non-nil, is called before each session executes.
	// Use this to manipulate the store between sessions (e.g., backdating
	// edge timestamps for temporal decay testing).
	BeforeSession func(sessionIndex int, s *store.SQLiteGraphStore)
}

// SessionContext provides the context snapshot for a single activation session.
type SessionContext struct {
	models.ContextSnapshot

	// Label is an optional human-readable tag for debugging output.
	Label string
}

// EdgeSpec defines a pre-seeded edge in the graph.
type EdgeSpec struct {
	Source        string
	Target        string
	Kind          string
	Weight        float64
	CreatedAt     time.Time
	LastActivated *time.Time
}

// ToEdge converts an EdgeSpec to a store.Edge, applying defaults.
func (e EdgeSpec) ToEdge() store.Edge {
	createdAt := e.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().Add(-24 * time.Hour)
	}
	return store.Edge{
		Source:        e.Source,
		Target:        e.Target,
		Kind:          e.Kind,
		Weight:        e.Weight,
		CreatedAt:     createdAt,
		LastActivated: e.LastActivated,
	}
}

// SessionResult captures the outcome of a single activation session.
type SessionResult struct {
	Index       int
	Seeds       []spreading.Seed
	Results     []spreading.Result
	Pairs       []spreading.CoActivationPair
	EdgeWeights map[string]float64 // "src->tgt:kind" → weight
}

// SimulationResult captures all sessions and the final store state.
type SimulationResult struct {
	Sessions []SessionResult
	Store    *store.SQLiteGraphStore
}

// EdgeKey builds the canonical map key for an edge.
func EdgeKey(src, tgt, kind string) string {
	return src + "->" + tgt + ":" + kind
}
