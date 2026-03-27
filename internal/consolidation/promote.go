package consolidation

import (
	"context"
	"database/sql"
	"errors"
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
func (c *LLMConsolidator) Promote(ctx context.Context, memories []ClassifiedMemory, edges []store.Edge, merges []MergeProposal, skips []int, s store.GraphStore) (PromoteResult, error) {
	if s == nil {
		return PromoteResult{}, nil
	}

	cl := NewConsolidationLogger(c.decisions, c.runID, c.normalizedModel())

	mergedIndices := make(map[int]bool)
	for _, merge := range merges {
		mergedIndices[merge.MemoryIndex] = true
	}
	skipped := make(map[int]bool, len(skips))
	for _, idx := range skips {
		skipped[idx] = true
	}
	mergeCount := 0
	nodesCreatedByMerge := 0

	for _, merge := range merges {
		mergeStart := time.Now()
		err := c.executeMerge(ctx, merge, s, c.runID)
		elapsed := time.Since(mergeStart).Milliseconds()

		if err != nil {
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

	promoted := 0
	baseTS := time.Now().UnixNano()
	pendingToActual := make(map[string]string)

	for i, mem := range memories {
		pendingID := PendingNodeID(i)

		if skipped[i] {
			cl.LogPromote("skip", 0, map[string]any{
				"reason":      "already_captured",
				"memory_kind": string(mem.Kind),
			})
			continue
		}
		if mergedIndices[i] {
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

		node := c.buildPromoteNode(mem, c.runID, baseTS, i)
		if _, err := s.AddNode(ctx, node); err != nil {
			var dupErr *store.DuplicateContentError
			if errors.As(err, &dupErr) {
				pendingToActual[pendingID] = dupErr.ExistingID
				slog.Info("skipping duplicate content", "node_id", node.ID, "error", err)
				cl.LogPromote("skip", 0, map[string]any{
					"reason":      "duplicate_content",
					"memory_kind": string(mem.Kind),
					"node_id":     node.ID,
				})
				continue
			}
			return PromoteResult{}, fmt.Errorf("adding consolidated node: %w", err)
		}
		pendingToActual[pendingID] = node.ID
		promoted++

		cl.LogPromote("promote", 0, map[string]any{
			"memory_kind": string(mem.Kind),
			"confidence":  mem.Confidence,
			"node_id":     node.ID,
		})
	}

	for _, edge := range edges {
		if actual, ok := pendingToActual[edge.Source]; ok {
			edge.Source = actual
		}
		if actual, ok := pendingToActual[edge.Target]; ok {
			edge.Target = actual
		}
		if strings.HasPrefix(edge.Source, "pending-") || strings.HasPrefix(edge.Target, "pending-") {
			continue
		}
		if err := s.AddEdge(ctx, edge); err != nil {
			return PromoteResult{}, fmt.Errorf("adding edge: %w", err)
		}
	}

	return PromoteResult{
		Promoted:       promoted + nodesCreatedByMerge,
		MergesExecuted: mergeCount,
	}, nil
}

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

func (c *LLMConsolidator) executeAbsorb(ctx context.Context, merge MergeProposal, s store.GraphStore) error {
	node, err := getTargetNode(ctx, s, merge.TargetID)
	if err != nil {
		return err
	}

	if node.Content == nil {
		node.Content = make(map[string]interface{})
	}
	contentMap, _ := node.Content["content"].(map[string]interface{})
	if contentMap == nil {
		contentMap = make(map[string]interface{})
	}
	existingCanonical, _ := contentMap["canonical"].(string)
	if len(merge.Memory.Content.Canonical) > len(existingCanonical) {
		contentMap["canonical"] = merge.Memory.Content.Canonical
	}
	if merge.Memory.Content.Summary != "" {
		contentMap["summary"] = merge.Memory.Content.Summary
		node.Content["name"] = merge.Memory.Content.Summary
	}
	if len(merge.Memory.Content.Tags) > 0 {
		contentMap["tags"] = toInterfaceSlice(merge.Memory.Content.Tags)
	}
	node.Content["content"] = contentMap

	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}
	oldConf, _ := node.Metadata["confidence"].(float64)
	newConf := merge.Memory.Confidence
	maxConf := oldConf
	if newConf > oldConf {
		maxConf = newConf
		node.Metadata["confidence"] = newConf
	}

	prov, _ := node.Metadata["provenance"].(map[string]interface{})
	if prov == nil {
		prov = make(map[string]interface{})
	}
	prov["consolidated_by"] = c.normalizedModel()
	prov["source_type"] = string(models.SourceTypeConsolidated)
	prov["consolidated_at"] = time.Now().UTC().Format(time.RFC3339)
	prov["confidence"] = maxConf

	existingEvents, _ := prov["source_events"].([]interface{})
	prov["source_events"] = mergeSourceEvents(existingEvents, merge.Memory.SourceEvents)
	node.Metadata["provenance"] = prov

	return s.UpdateNode(ctx, node)
}

func (c *LLMConsolidator) executeSupersede(ctx context.Context, merge MergeProposal, s store.GraphStore, runID string) error {
	existing, err := getTargetNode(ctx, s, merge.TargetID)
	if err != nil {
		return err
	}

	var oldEvents []interface{}
	if oldProv, ok := existing.Metadata["provenance"].(map[string]interface{}); ok {
		oldEvents, _ = oldProv["source_events"].([]interface{})
	}
	combinedIface := mergeSourceEvents(oldEvents, merge.Memory.SourceEvents)
	combinedEvents := make([]string, 0, len(combinedIface))
	for _, e := range combinedIface {
		if str, ok := e.(string); ok {
			combinedEvents = append(combinedEvents, str)
		}
	}

	ts := time.Now().UnixNano()
	newID := fmt.Sprintf("supersede-%s-%d", merge.TargetID, ts)
	node := c.buildPromoteNode(merge.Memory, runID, ts, merge.MemoryIndex)
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
		if errors.Is(err, store.ErrDuplicateContent) {
			return fmt.Errorf("superseding node is a duplicate: %w", err)
		}
		return fmt.Errorf("creating superseding node: %w", err)
	}

	edge := store.Edge{
		Source: newID, Target: merge.TargetID,
		Kind: EdgeKindSupersedes, Weight: 1.0, CreatedAt: time.Now(),
	}
	if err := s.AddEdge(ctx, edge); err != nil {
		if rbErr := s.DeleteNode(ctx, newID); rbErr != nil {
			slog.Warn("supersede rollback: failed to delete orphaned node", "new_id", newID, "error", rbErr)
		}
		return fmt.Errorf("adding supersedes edge: %w", err)
	}

	existing.Kind = store.NodeKindMerged
	if existing.Metadata == nil {
		existing.Metadata = make(map[string]interface{})
	}
	existing.Metadata["merged_at"] = time.Now().UTC().Format(time.RFC3339)
	existing.Metadata["merged_reason"] = "superseded"
	if err := s.UpdateNode(ctx, existing); err != nil {
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

func (c *LLMConsolidator) executeSupplement(ctx context.Context, merge MergeProposal, s store.GraphStore, runID string) error {
	if _, err := getTargetNode(ctx, s, merge.TargetID); err != nil {
		return err
	}

	ts := time.Now().UnixNano()
	newID := fmt.Sprintf("supplement-%s-%d", merge.TargetID, ts)
	node := c.buildPromoteNode(merge.Memory, runID, ts, merge.MemoryIndex)
	node.ID = newID

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
		if errors.Is(err, store.ErrDuplicateContent) {
			return fmt.Errorf("supplement node is a duplicate: %w", err)
		}
		return fmt.Errorf("creating supplement node: %w", err)
	}

	edge := store.Edge{
		Source: newID, Target: merge.TargetID,
		Kind: EdgeKindSupplements, Weight: merge.Similarity, CreatedAt: time.Now(),
	}
	if err := s.AddEdge(ctx, edge); err != nil {
		if rbErr := s.DeleteNode(ctx, newID); rbErr != nil {
			slog.Warn("supplement rollback: failed to delete orphaned node", "new_id", newID, "error", rbErr)
		}
		return fmt.Errorf("adding supplements edge: %w", err)
	}

	return nil
}

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

	prov := map[string]interface{}{
		"source_type":     string(models.SourceTypeConsolidated),
		"consolidated_by": c.normalizedModel(),
		"source_events":   toInterfaceSlice(mem.SourceEvents),
		"confidence":      mem.Confidence,
		"importance":      mem.Importance,
		"consolidated_at": time.Now().UTC().Format(time.RFC3339),
	}

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

func persistRun(ctx context.Context, s store.GraphStore, model string, rec ConsolidationRunRecord, runID string, mergeCount int) {
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

func getTargetNode(ctx context.Context, s store.GraphStore, targetID string) (store.Node, error) {
	existing, err := s.GetNode(ctx, targetID)
	if err != nil {
		return store.Node{}, fmt.Errorf("fetching target node %s: %w", targetID, err)
	}
	if existing == nil {
		return store.Node{}, fmt.Errorf("target node not found: %s", targetID)
	}
	return *existing, nil
}

func mergeSourceEvents(existing []interface{}, incoming []string) []interface{} {
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		if str, ok := e.(string); ok {
			seen[str] = true
		}
	}
	for _, evtID := range incoming {
		if !seen[evtID] {
			existing = append(existing, evtID)
			seen[evtID] = true
		}
	}
	return existing
}
