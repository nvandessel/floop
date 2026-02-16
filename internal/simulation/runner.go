package simulation

import (
	"context"
	"fmt"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/tiering"
)

// Runner orchestrates multi-session simulation experiments against a real
// graph store and activation engine.
type Runner struct {
	t     *testing.T
	store *store.SQLiteGraphStore
}

// NewRunner creates a simulation runner with an isolated SQLite store
// and sandboxed HOME directory.
func NewRunner(t *testing.T) *Runner {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	s, err := store.NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewRunner: failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	return &Runner{t: t, store: s}
}

// Run executes the scenario and returns the collected results.
func (r *Runner) Run(scenario Scenario) SimulationResult {
	r.t.Helper()
	ctx := context.Background()

	// Phase 1: Seed the graph with behaviors and edges.
	r.seedGraph(ctx, scenario)

	// Phase 2: Configure the engine.
	spreadCfg := spreading.DefaultConfig()
	if scenario.SpreadConfig != nil {
		spreadCfg = *scenario.SpreadConfig
	}
	engine := spreading.NewEngine(r.store, spreadCfg)

	hebbianCfg := spreading.DefaultHebbianConfig()
	if scenario.HebbianConfig != nil {
		hebbianCfg = *scenario.HebbianConfig
	}

	var tierMapper *tiering.ActivationTierMapper
	if scenario.TokenBudget > 0 {
		tierMapper = tiering.NewActivationTierMapper(tiering.DefaultActivationTierConfig())
	}

	// Build a pipeline for scenarios that use real seed selection.
	var pipeline *spreading.Pipeline
	if scenario.SeedOverride == nil {
		pipeline = spreading.NewPipeline(r.store, spreadCfg)
	}

	// Phase 3: Run sessions.
	sessions := make([]SessionResult, len(scenario.Sessions))
	for i, sessCtx := range scenario.Sessions {
		if scenario.BeforeSession != nil {
			scenario.BeforeSession(i, r.store)
		}
		sr := r.runSession(ctx, i, sessCtx, engine, pipeline, hebbianCfg, tierMapper, scenario)
		sessions[i] = sr
	}

	return SimulationResult{
		Sessions: sessions,
		Store:    r.store,
	}
}

// seedGraph inserts all behaviors and edges from the scenario into the store.
func (r *Runner) seedGraph(ctx context.Context, scenario Scenario) {
	r.t.Helper()

	for _, bs := range scenario.Behaviors {
		node := bs.ToNode()
		if _, err := r.store.AddNode(ctx, node); err != nil {
			r.t.Fatalf("seedGraph: AddNode(%s): %v", bs.ID, err)
		}
	}

	for _, es := range scenario.Edges {
		edge := es.ToEdge()
		if err := r.store.AddEdge(ctx, edge); err != nil {
			r.t.Fatalf("seedGraph: AddEdge(%s->%s): %v", es.Source, es.Target, err)
		}
	}
}

// runSession executes a single activation session and returns the result.
func (r *Runner) runSession(
	ctx context.Context,
	index int,
	sessCtx SessionContext,
	engine *spreading.Engine,
	pipeline *spreading.Pipeline,
	hebbianCfg spreading.HebbianConfig,
	tierMapper *tiering.ActivationTierMapper,
	scenario Scenario,
) SessionResult {
	r.t.Helper()

	// Step 1: Get seeds.
	var seeds []spreading.Seed
	var results []spreading.Result
	var err error

	if scenario.SeedOverride != nil {
		seeds = scenario.SeedOverride(index)
		results, err = engine.Activate(ctx, seeds)
		if err != nil {
			r.t.Fatalf("session %d: Activate: %v", index, err)
		}
	} else {
		results, err = pipeline.Run(ctx, sessCtx.ContextSnapshot)
		if err != nil {
			r.t.Fatalf("session %d: Pipeline.Run: %v", index, err)
		}
		// Pipeline doesn't expose seeds, but we can identify them from distance=0
		for _, res := range results {
			if res.Distance == 0 {
				seeds = append(seeds, spreading.Seed{
					BehaviorID: res.BehaviorID,
					Activation: res.Activation,
					Source:     res.SeedSource,
				})
			}
		}
	}

	// Step 2: Extract co-activation pairs and apply Hebbian learning.
	seedIDs := make(map[string]bool, len(seeds))
	for _, s := range seeds {
		seedIDs[s.BehaviorID] = true
	}
	pairs := spreading.ExtractCoActivationPairs(results, seedIDs, hebbianCfg)

	if scenario.HebbianEnabled && len(pairs) > 0 {
		r.applyHebbian(ctx, pairs, hebbianCfg, scenario.CreateEdges)
	}

	// Step 3: Touch edges and record activation hits.
	activatedIDs := make([]string, 0, len(results))
	for _, res := range results {
		activatedIDs = append(activatedIDs, res.BehaviorID)
	}
	if len(activatedIDs) > 0 {
		if err := r.store.TouchEdges(ctx, activatedIDs); err != nil {
			r.t.Fatalf("session %d: TouchEdges: %v", index, err)
		}
		for _, id := range activatedIDs {
			if err := r.store.RecordActivationHit(ctx, id); err != nil {
				// Not all activated nodes may have stats rows (e.g., if they
				// were reached by traversal but aren't in the scenario).
				// Log but don't fail.
				r.t.Logf("session %d: RecordActivationHit(%s): %v (ignored)", index, id, err)
			}
		}
	}

	// Step 4: Optionally run tiering.
	if tierMapper != nil && scenario.TokenBudget > 0 {
		behaviors := r.loadBehaviors(ctx, scenario)
		_ = tierMapper.MapResults(results, behaviors, scenario.TokenBudget)
	}

	// Step 5: Snapshot edge weights.
	edgeWeights := r.snapshotEdgeWeights(ctx, scenario)

	return SessionResult{
		Index:       index,
		Seeds:       seeds,
		Results:     results,
		Pairs:       pairs,
		EdgeWeights: edgeWeights,
	}
}

// applyHebbian applies Oja updates for each co-activation pair.
// When createEdges is true, new co-activated edges are created for pairs
// that don't yet have one. When false, only existing edges are updated.
func (r *Runner) applyHebbian(ctx context.Context, pairs []spreading.CoActivationPair, cfg spreading.HebbianConfig, createEdges bool) {
	r.t.Helper()

	updates := make([]store.EdgeWeightUpdate, 0, len(pairs))
	for _, pair := range pairs {
		// Look up the current edge weight.
		currentWeight := r.getEdgeWeight(ctx, pair.BehaviorA, pair.BehaviorB, "co-activated")
		if currentWeight < 0 {
			if !createEdges {
				continue
			}
			// Create a new co-activated edge at MinWeight so Oja can strengthen it.
			edge := store.Edge{
				Source:    pair.BehaviorA,
				Target:    pair.BehaviorB,
				Kind:      "co-activated",
				Weight:    cfg.MinWeight,
				CreatedAt: TimeAgo(0),
			}
			if err := r.store.AddEdge(ctx, edge); err != nil {
				r.t.Logf("applyHebbian: AddEdge(%s->%s): %v", pair.BehaviorA, pair.BehaviorB, err)
				continue
			}
			currentWeight = cfg.MinWeight
		}

		newWeight := spreading.OjaUpdate(currentWeight, pair.ActivationA, pair.ActivationB, cfg)
		updates = append(updates, store.EdgeWeightUpdate{
			Source:    pair.BehaviorA,
			Target:    pair.BehaviorB,
			Kind:      "co-activated",
			NewWeight: newWeight,
		})
	}

	if len(updates) > 0 {
		if err := r.store.BatchUpdateEdgeWeights(ctx, updates); err != nil {
			r.t.Fatalf("applyHebbian: BatchUpdateEdgeWeights: %v", err)
		}
	}
}

// getEdgeWeight returns the weight of an edge, or -1 if the edge doesn't exist.
func (r *Runner) getEdgeWeight(ctx context.Context, src, tgt, kind string) float64 {
	edges, err := r.store.GetEdges(ctx, src, store.DirectionOutbound, kind)
	if err != nil {
		r.t.Fatalf("getEdgeWeight: GetEdges(%s): %v", src, err)
	}
	for _, e := range edges {
		if e.Target == tgt {
			return e.Weight
		}
	}
	return -1
}

// snapshotEdgeWeights captures current edge weights for all behaviors in the scenario.
func (r *Runner) snapshotEdgeWeights(ctx context.Context, scenario Scenario) map[string]float64 {
	weights := make(map[string]float64)
	seen := make(map[string]bool)

	for _, bs := range scenario.Behaviors {
		edges, err := r.store.GetEdges(ctx, bs.ID, store.DirectionBoth, "")
		if err != nil {
			r.t.Fatalf("snapshotEdgeWeights: GetEdges(%s): %v", bs.ID, err)
		}
		for _, e := range edges {
			key := EdgeKey(e.Source, e.Target, e.Kind)
			if !seen[key] {
				weights[key] = e.Weight
				seen[key] = true
			}
		}
	}

	return weights
}

// loadBehaviors loads all scenario behaviors as a map for tiering.
func (r *Runner) loadBehaviors(ctx context.Context, scenario Scenario) map[string]*models.Behavior {
	behaviors := make(map[string]*models.Behavior, len(scenario.Behaviors))
	for _, bs := range scenario.Behaviors {
		node, err := r.store.GetNode(ctx, bs.ID)
		if err != nil {
			r.t.Fatalf("loadBehaviors: GetNode(%s): %v", bs.ID, err)
		}
		b := models.NodeToBehavior(*node)
		behaviors[bs.ID] = &b
	}
	return behaviors
}

// FormatSessionDebug returns a debug string for a session result.
func FormatSessionDebug(sr SessionResult) string {
	s := fmt.Sprintf("Session %d: seeds=%d results=%d pairs=%d\n", sr.Index, len(sr.Seeds), len(sr.Results), len(sr.Pairs))
	for _, r := range sr.Results {
		s += fmt.Sprintf("  %s: activation=%.4f distance=%d\n", r.BehaviorID, r.Activation, r.Distance)
	}
	for k, v := range sr.EdgeWeights {
		s += fmt.Sprintf("  edge %s: weight=%.6f\n", k, v)
	}
	return s
}
