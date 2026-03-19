package consolidation

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// EdgeKindSupersedes is a directed edge from the new behavior to the old one
// it replaced. The old behavior is soft-deleted (kind=merged-behavior).
const EdgeKindSupersedes store.EdgeKind = "supersedes"

// EdgeKindSupplements is a directed edge from the new supplementary detail to
// the existing behavior it extends.
const EdgeKindSupplements store.EdgeKind = "supplements"

// Promote writes classified memories into the graph store, executing merge
// proposals (absorb/supersede/supplement) and logging every decision.
// It replaces the heuristic Promote with merge-aware logic.
func (c *LLMConsolidator) Promote(ctx context.Context, runID string, memories []ClassifiedMemory, edges []store.Edge, merges []MergeProposal, skips []int, s store.GraphStore) (int, error) {
	if s == nil {
		return 0, nil
	}

	cl := NewConsolidationLogger(c.decisions, runID, c.config.Model)

	// Index merge proposals by memory position so we can skip merged memories
	// in the create-new pass. Uses MemoryIndex from MergeProposal for exact matching.
	mergedIndices := make(map[int]bool)
	for _, merge := range merges {
		mergedIndices[merge.MemoryIndex] = true
	}
	// Build set of memories to skip (already captured by existing behaviors).
	skipped := make(map[int]bool, len(skips))
	for _, idx := range skips {
		skipped[idx] = true
	}
	mergeCount := 0
	// nodesCreatedByMerge counts new nodes added by supersede/supplement strategies
	// (absorb modifies an existing node, so it doesn't create a new one).
	nodesCreatedByMerge := 0

	for _, merge := range merges {
		mergeStart := time.Now()
		err := c.executeMerge(ctx, merge, s, runID)
		elapsed := time.Since(mergeStart).Milliseconds()

		if err != nil {
			// Merge failed — unmark the memory so it falls through to create-as-new
			delete(mergedIndices, merge.MemoryIndex)
			cl.LogPromote("merge_failed", elapsed, map[string]any{
				"target_id": merge.TargetID,
				"strategy":  merge.Strategy,
				"error":     err.Error(),
			})
			continue
		}

		mergeCount++
		if merge.Strategy == "supersede" || merge.Strategy == "supplement" {
			nodesCreatedByMerge++
		}
		cl.LogPromote("merge", elapsed, map[string]any{
			"target_id":  merge.TargetID,
			"strategy":   merge.Strategy,
			"similarity": merge.Similarity,
		})
	}

	// Create nodes for non-merged memories and build pending→actual ID map
	promoted := 0
	baseTS := time.Now().UnixNano()
	pendingToActual := make(map[string]string) // "pending-N" → actual node ID

	for i, mem := range memories {
		pendingID := PendingNodeID(i)

		if skipped[i] {
			// Skipped memories don't get nodes; co-occurrence edges referencing
			// them will be filtered out by the pending-ID check below.
			cl.LogPromote("skip", 0, map[string]any{
				"reason":      "already_captured",
				"memory_kind": string(mem.Kind),
			})
			continue
		}
		if mergedIndices[i] {
			// Map merged memory's pending ID to its merge target
			for _, merge := range merges {
				if merge.MemoryIndex == i {
					pendingToActual[pendingID] = merge.TargetID
					break
				}
			}
			cl.LogPromote("skip", 0, map[string]any{
				"reason":      "merged",
				"memory_kind": string(mem.Kind),
			})
			continue
		}

		node := c.buildPromoteNode(mem, runID, baseTS, i)
		if _, err := s.AddNode(ctx, node); err != nil {
			return 0, fmt.Errorf("adding consolidated node: %w", err)
		}
		pendingToActual[pendingID] = node.ID
		promoted++

		cl.LogPromote("promote", 0, map[string]any{
			"memory_kind": string(mem.Kind),
			"confidence":  mem.Confidence,
			"node_id":     node.ID,
		})
	}

	// Rewrite pending IDs in edges to actual node IDs, then write to store
	for _, edge := range edges {
		if actual, ok := pendingToActual[edge.Source]; ok {
			edge.Source = actual
		}
		if actual, ok := pendingToActual[edge.Target]; ok {
			edge.Target = actual
		}
		// Skip edges with unresolved pending IDs (shouldn't happen, but defensive)
		if strings.HasPrefix(edge.Source, "pending-") || strings.HasPrefix(edge.Target, "pending-") {
			continue
		}
		if err := s.AddEdge(ctx, edge); err != nil {
			return 0, fmt.Errorf("adding edge: %w", err)
		}
	}

	return promoted + nodesCreatedByMerge, nil
}

// executeMerge applies a single merge proposal to the graph store.
func (c *LLMConsolidator) executeMerge(ctx context.Context, merge MergeProposal, s store.GraphStore, runID string) error {
	switch merge.Strategy {
	case "absorb":
		return c.executeAbsorb(ctx, merge, s)
	case "supersede":
		return c.executeSupersede(ctx, merge, s, runID)
	case "supplement":
		return c.executeSupplement(ctx, merge, s, runID)
	default:
		return fmt.Errorf("unknown merge strategy: %q", merge.Strategy)
	}
}

// executeAbsorb updates an existing node with merged content, bumps confidence,
// and appends source events to provenance.
func (c *LLMConsolidator) executeAbsorb(ctx context.Context, merge MergeProposal, s store.GraphStore) error {
	existing, err := s.GetNode(ctx, merge.TargetID)
	if err != nil {
		return fmt.Errorf("fetching target node %s: %w", merge.TargetID, err)
	}
	if existing == nil {
		return fmt.Errorf("target node not found: %s", merge.TargetID)
	}

	// Update content: prefer new canonical if it is longer/richer
	if existing.Content == nil {
		existing.Content = make(map[string]interface{})
	}
	contentMap, _ := existing.Content["content"].(map[string]interface{})
	if contentMap == nil {
		contentMap = make(map[string]interface{})
	}
	existingCanonical, _ := contentMap["canonical"].(string)
	if len(merge.Memory.Content.Canonical) > len(existingCanonical) {
		contentMap["canonical"] = merge.Memory.Content.Canonical
	}
	if merge.Memory.Content.Summary != "" {
		contentMap["summary"] = merge.Memory.Content.Summary
		// Also update top-level name for UI/query consistency
		existing.Content["name"] = merge.Memory.Content.Summary
	}
	if len(merge.Memory.Content.Tags) > 0 {
		contentMap["tags"] = toInterfaceSlice(merge.Memory.Content.Tags)
	}
	existing.Content["content"] = contentMap

	// Bump confidence: take the max of existing and new, capped at 1.0
	if existing.Metadata == nil {
		existing.Metadata = make(map[string]interface{})
	}
	oldConf, _ := existing.Metadata["confidence"].(float64)
	newConf := merge.Memory.Confidence
	maxConf := oldConf
	if newConf > oldConf {
		maxConf = newConf
		existing.Metadata["confidence"] = newConf
	}

	// Append provenance
	prov, _ := existing.Metadata["provenance"].(map[string]interface{})
	if prov == nil {
		prov = make(map[string]interface{})
	}
	prov["consolidated_by"] = c.config.Model
	prov["source_type"] = string(models.SourceTypeConsolidated)
	now := time.Now().UTC()
	prov["consolidated_at"] = now.Format(time.RFC3339)
	prov["confidence"] = maxConf

	// Merge source events with deduplication
	existingEvents, _ := prov["source_events"].([]interface{})
	seen := make(map[string]bool, len(existingEvents))
	for _, e := range existingEvents {
		if str, ok := e.(string); ok {
			seen[str] = true
		}
	}
	for _, evtID := range merge.Memory.SourceEvents {
		if !seen[evtID] {
			existingEvents = append(existingEvents, evtID)
			seen[evtID] = true
		}
	}
	prov["source_events"] = existingEvents
	existing.Metadata["provenance"] = prov

	return s.UpdateNode(ctx, *existing)
}

// executeSupersede marks the old behavior as merged (soft-delete), creates a
// new node with combined lineage, and adds a supersedes edge.
// The new node is created first; the old node is only soft-deleted once the
// new node and edge are safely written (atomic w.r.t. partial failure).
func (c *LLMConsolidator) executeSupersede(ctx context.Context, merge MergeProposal, s store.GraphStore, runID string) error {
	existing, err := s.GetNode(ctx, merge.TargetID)
	if err != nil {
		return fmt.Errorf("fetching target node %s: %w", merge.TargetID, err)
	}
	if existing == nil {
		return fmt.Errorf("target node not found: %s", merge.TargetID)
	}

	// Combine lineage: gather source events from old + new with deduplication
	var combinedEvents []string
	seen := make(map[string]bool)
	if oldProv, ok := existing.Metadata["provenance"].(map[string]interface{}); ok {
		if oldEvents, ok := oldProv["source_events"].([]interface{}); ok {
			for _, e := range oldEvents {
				if str, ok := e.(string); ok && !seen[str] {
					combinedEvents = append(combinedEvents, str)
					seen[str] = true
				}
			}
		}
	}
	for _, evtID := range merge.Memory.SourceEvents {
		if !seen[evtID] {
			combinedEvents = append(combinedEvents, evtID)
			seen[evtID] = true
		}
	}

	// Create new node first (before any mutations to the old node)
	ts := time.Now().UnixNano()
	newID := fmt.Sprintf("supersede-%s-%d", merge.TargetID, ts)
	node := c.buildPromoteNode(merge.Memory, runID, ts, merge.MemoryIndex)
	// Override provenance with combined lineage
	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}
	prov, _ := node.Metadata["provenance"].(map[string]interface{})
	if prov == nil {
		prov = make(map[string]interface{})
	}
	prov["source_events"] = toInterfaceSlice(combinedEvents)
	prov["supersedes"] = merge.TargetID
	node.Metadata["provenance"] = prov
	node.ID = newID

	if _, err := s.AddNode(ctx, node); err != nil {
		return fmt.Errorf("creating superseding node: %w", err)
	}

	// Add supersedes edge: new -> old
	edge := store.Edge{
		Source:    newID,
		Target:    merge.TargetID,
		Kind:      EdgeKindSupersedes,
		Weight:    1.0,
		CreatedAt: time.Now(),
	}
	if err := s.AddEdge(ctx, edge); err != nil {
		// Clean up the orphaned new node before returning
		if rbErr := s.DeleteNode(ctx, newID); rbErr != nil {
			slog.Warn("supersede rollback: failed to delete orphaned node", "new_id", newID, "error", rbErr)
		}
		return fmt.Errorf("adding supersedes edge: %w", err)
	}

	// Only soft-delete old node after new node + edge are safely written
	existing.Kind = store.NodeKindMerged
	if existing.Metadata == nil {
		existing.Metadata = make(map[string]interface{})
	}
	existing.Metadata["merged_at"] = time.Now().UTC().Format(time.RFC3339)
	existing.Metadata["merged_reason"] = "superseded"
	if err := s.UpdateNode(ctx, *existing); err != nil {
		// Rollback: remove the edge and orphaned new node
		if rbErr := s.RemoveEdge(ctx, newID, merge.TargetID, EdgeKindSupersedes); rbErr != nil {
			slog.Warn("supersede rollback: failed to remove edge", "new_id", newID, "target", merge.TargetID, "error", rbErr)
		}
		if rbErr := s.DeleteNode(ctx, newID); rbErr != nil {
			slog.Warn("supersede rollback: failed to delete orphaned node", "new_id", newID, "error", rbErr)
		}
		return fmt.Errorf("marking old node as merged: %w", err)
	}

	return nil
}

// executeSupplement keeps the existing behavior unchanged and creates a new node
// with a supplements edge pointing to the existing behavior.
func (c *LLMConsolidator) executeSupplement(ctx context.Context, merge MergeProposal, s store.GraphStore, runID string) error {
	// Verify target exists
	existing, err := s.GetNode(ctx, merge.TargetID)
	if err != nil {
		return fmt.Errorf("fetching target node %s: %w", merge.TargetID, err)
	}
	if existing == nil {
		return fmt.Errorf("target node not found: %s", merge.TargetID)
	}

	// Create supplementary node
	ts := time.Now().UnixNano()
	newID := fmt.Sprintf("supplement-%s-%d", merge.TargetID, ts)
	node := c.buildPromoteNode(merge.Memory, runID, ts, merge.MemoryIndex)
	node.ID = newID

	// Add supplements provenance for self-describing nodes
	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}
	prov, _ := node.Metadata["provenance"].(map[string]interface{})
	if prov == nil {
		prov = make(map[string]interface{})
	}
	prov["supplements"] = merge.TargetID
	node.Metadata["provenance"] = prov

	if _, err := s.AddNode(ctx, node); err != nil {
		return fmt.Errorf("creating supplement node: %w", err)
	}

	// Add supplements edge: new detail -> existing behavior
	edge := store.Edge{
		Source:    newID,
		Target:    merge.TargetID,
		Kind:      EdgeKindSupplements,
		Weight:    merge.Similarity,
		CreatedAt: time.Now(),
	}
	if err := s.AddEdge(ctx, edge); err != nil {
		// Clean up the orphaned new node before returning
		if rbErr := s.DeleteNode(ctx, newID); rbErr != nil {
			slog.Warn("supplement rollback: failed to delete orphaned node", "new_id", newID, "error", rbErr)
		}
		return fmt.Errorf("adding supplements edge: %w", err)
	}

	return nil
}

// buildPromoteNode constructs a store.Node from a ClassifiedMemory with rich
// provenance including model, source events, confidence, and session context.
func (c *LLMConsolidator) buildPromoteNode(mem ClassifiedMemory, runID string, baseTS int64, index int) store.Node {
	contentMap := map[string]interface{}{
		"canonical":  mem.Content.Canonical,
		"summary":    mem.Content.Summary,
		"tags":       toInterfaceSlice(mem.Content.Tags),
		"structured": mem.Content.Structured,
	}
	if mem.EpisodeData != nil {
		contentMap["episode_data"] = mem.EpisodeData
	}
	if mem.WorkflowData != nil {
		contentMap["workflow_data"] = mem.WorkflowData
	}

	// Rich provenance
	prov := map[string]interface{}{
		"source_type":     string(models.SourceTypeConsolidated),
		"consolidated_by": c.config.Model,
		"source_events":   toInterfaceSlice(mem.SourceEvents),
		"confidence":      mem.Confidence,
		"importance":      mem.Importance,
		"consolidated_at": time.Now().UTC().Format(time.RFC3339),
	}

	// Session context as provenance metadata
	if phase, ok := mem.SessionContext["session_phase"].(string); ok {
		prov["session_phase"] = phase
	}
	if sentiment, ok := mem.SessionContext["sentiment"].(string); ok {
		prov["sentiment"] = sentiment
	}

	metadata := map[string]interface{}{
		"confidence":        mem.Confidence,
		"importance":        mem.Importance,
		"scope":             mem.Scope,
		"provenance":        prov,
		"consolidation_run": runID,
	}

	return store.Node{
		ID:   fmt.Sprintf("consolidated-%d-%d", baseTS, index),
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"name":        mem.Content.Summary,
			"kind":        string(mem.Kind),
			"content":     contentMap,
			"memory_type": string(mem.MemoryType),
		},
		Metadata: metadata,
	}
}

// persistRun writes a consolidation run record to the consolidation_runs table.
// It silently no-ops if the store does not support SQL (e.g., InMemoryGraphStore).
// Errors are logged but not fatal — run persistence is best-effort.
func persistRun(ctx context.Context, s store.GraphStore, model string, rec ConsolidationRunRecord, runID string, mergeCount int) {
	// Type-assert to get the underlying *sql.DB.
	type sqlDBProvider interface {
		DB() *sql.DB
	}
	provider, ok := s.(sqlDBProvider)
	if !ok {
		return
	}
	db := provider.DB()
	if db == nil {
		return
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO consolidation_runs (id, model, candidates_found, memories_promoted, merges_executed, duration_ms, project_id, session_id, tokens_used, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		runID, model, rec.CandidatesFound, rec.Promoted, mergeCount, rec.DurationMS,
		nullIfEmpty(rec.ProjectID), nullIfEmpty(rec.SessionID), nullIfZero(rec.TokensUsed),
	); err != nil {
		slog.Warn("failed to persist consolidation run", "run_id", runID, "error", err)
	}
}
