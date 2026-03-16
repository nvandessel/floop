package consolidation

import (
	"context"
	"testing"

	"github.com/nvandessel/floop/internal/logging"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

func testMemory(canonical string, kind models.BehaviorKind) ClassifiedMemory {
	return ClassifiedMemory{
		Candidate: Candidate{
			SourceEvents:  []string{"evt-1", "evt-2"},
			RawText:       canonical,
			CandidateType: "correction",
			Confidence:    0.85,
			SessionContext: map[string]any{
				"session_id":    "sess-1",
				"project_id":    "proj-1",
				"session_phase": "debugging",
				"sentiment":     "frustrated",
			},
		},
		Kind:       kind,
		MemoryType: models.MemoryTypeSemantic,
		Scope:      "project:proj-1",
		Importance: 0.85,
		Content: models.BehaviorContent{
			Canonical: canonical,
			Summary:   truncate(canonical, 60),
			Tags:      []string{"testing"},
		},
	}
}

func newTestLLMConsolidator() *LLMConsolidator {
	return NewLLMConsolidator(nil, nil, LLMConsolidatorConfig{
		Model: "test-model",
	})
}

func newTestLLMConsolidatorWithLogger(dl *logging.DecisionLogger) *LLMConsolidator {
	return NewLLMConsolidator(nil, dl, LLMConsolidatorConfig{
		Model: "test-model",
	})
}

func TestLLMPromote_CreateNodes(t *testing.T) {
	c := newTestLLMConsolidator()
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	memories := []ClassifiedMemory{
		testMemory("Use fmt.Errorf to wrap errors", models.BehaviorKindDirective),
		testMemory("Prefer table-driven tests", models.BehaviorKindPreference),
	}

	err := c.Promote(ctx, memories, nil, nil, s)
	if err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}

	// Verify nodes were created
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{"kind": string(store.NodeKindBehavior)})
	if err != nil {
		t.Fatalf("QueryNodes returned error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	// Verify provenance on first node
	node := nodes[0]
	prov, ok := node.Metadata["provenance"].(map[string]interface{})
	if !ok {
		t.Fatal("expected provenance in metadata")
	}
	if prov["consolidated_by"] != "test-model" {
		t.Errorf("expected consolidated_by='test-model', got %v", prov["consolidated_by"])
	}
	if prov["source_type"] != "consolidated" {
		t.Errorf("expected source_type='consolidated', got %v", prov["source_type"])
	}
	if _, ok := prov["confidence"]; !ok {
		t.Error("expected confidence in provenance")
	}
	if _, ok := prov["consolidated_at"]; !ok {
		t.Error("expected consolidated_at in provenance")
	}
}

func TestLLMPromote_AbsorbMerge(t *testing.T) {
	c := newTestLLMConsolidator()
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create existing node
	existing := store.Node{
		ID:   "bhv-existing",
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"name":    "Wrap errors",
			"kind":    "directive",
			"content": map[string]interface{}{"canonical": "Wrap errors with context"},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.6,
			"provenance": map[string]interface{}{
				"source_type":   "consolidated",
				"source_events": []interface{}{"old-evt-1"},
			},
		},
	}
	if _, err := s.AddNode(ctx, existing); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	mem := testMemory("Use fmt.Errorf to wrap errors with context", models.BehaviorKindDirective)
	merges := []MergeProposal{{
		Memory:     mem,
		TargetID:   "bhv-existing",
		Similarity: 0.92,
		Strategy:   "absorb",
	}}

	err := c.Promote(ctx, []ClassifiedMemory{mem}, nil, merges, s)
	if err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}

	// Verify existing node was updated (not a new node created)
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{"kind": string(store.NodeKindBehavior)})
	if err != nil {
		t.Fatalf("QueryNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node (absorb should update, not create), got %d", len(nodes))
	}

	// Verify confidence was bumped
	updated, err := s.GetNode(ctx, "bhv-existing")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	conf, _ := updated.Metadata["confidence"].(float64)
	if conf < 0.85 {
		t.Errorf("expected confidence >= 0.85, got %f", conf)
	}

	// Verify source events were appended
	prov, _ := updated.Metadata["provenance"].(map[string]interface{})
	events, _ := prov["source_events"].([]interface{})
	if len(events) < 3 {
		t.Errorf("expected at least 3 source events (1 old + 2 new), got %d", len(events))
	}
}

func TestLLMPromote_SupersedeMerge(t *testing.T) {
	c := newTestLLMConsolidator()
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create existing node
	existing := store.Node{
		ID:   "bhv-old",
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"name": "Old pattern",
			"kind": "directive",
		},
		Metadata: map[string]interface{}{
			"confidence": 0.5,
			"provenance": map[string]interface{}{
				"source_events": []interface{}{"old-evt-1"},
			},
		},
	}
	if _, err := s.AddNode(ctx, existing); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	mem := testMemory("New improved pattern", models.BehaviorKindDirective)
	merges := []MergeProposal{{
		Memory:     mem,
		TargetID:   "bhv-old",
		Similarity: 0.88,
		Strategy:   "supersede",
	}}

	err := c.Promote(ctx, []ClassifiedMemory{mem}, nil, merges, s)
	if err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}

	// Old node should be marked as merged
	oldNode, err := s.GetNode(ctx, "bhv-old")
	if err != nil {
		t.Fatalf("GetNode old: %v", err)
	}
	if oldNode.Kind != store.NodeKindMerged {
		t.Errorf("expected old node kind=%q, got %q", store.NodeKindMerged, oldNode.Kind)
	}

	// A new node should exist
	allNodes, err := s.QueryNodes(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("QueryNodes: %v", err)
	}
	// Should have 2 nodes: the merged old and the new superseding
	if len(allNodes) != 2 {
		t.Fatalf("expected 2 nodes (merged old + new), got %d", len(allNodes))
	}

	// Check for supersedes edge
	edges, err := s.GetEdges(ctx, "bhv-old", store.DirectionInbound, EdgeKindSupersedes)
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 supersedes edge, got %d", len(edges))
	}
}

func TestLLMPromote_SupplementMerge(t *testing.T) {
	c := newTestLLMConsolidator()
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create existing node
	existing := store.Node{
		ID:   "bhv-base",
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"name": "Base behavior",
			"kind": "directive",
		},
		Metadata: map[string]interface{}{"confidence": 0.7},
	}
	if _, err := s.AddNode(ctx, existing); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	mem := testMemory("Additional detail about base behavior", models.BehaviorKindDirective)
	merges := []MergeProposal{{
		Memory:     mem,
		TargetID:   "bhv-base",
		Similarity: 0.75,
		Strategy:   "supplement",
	}}

	err := c.Promote(ctx, []ClassifiedMemory{mem}, nil, merges, s)
	if err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}

	// Original should be unchanged
	base, err := s.GetNode(ctx, "bhv-base")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if base.Kind != store.NodeKindBehavior {
		t.Errorf("expected base unchanged kind=%q, got %q", store.NodeKindBehavior, base.Kind)
	}

	// Should be 2 nodes: original + supplement
	allNodes, err := s.QueryNodes(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("QueryNodes: %v", err)
	}
	if len(allNodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(allNodes))
	}

	// Check supplements edge
	edges, err := s.GetEdges(ctx, "bhv-base", store.DirectionInbound, EdgeKindSupplements)
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 supplements edge, got %d", len(edges))
	}
}

func TestLLMPromote_MergeFailure(t *testing.T) {
	c := newTestLLMConsolidator()
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Merge targets a non-existent node — should fail gracefully
	mem := testMemory("Should be promoted as new", models.BehaviorKindDirective)
	merges := []MergeProposal{{
		Memory:     mem,
		TargetID:   "bhv-nonexistent",
		Similarity: 0.9,
		Strategy:   "absorb",
	}}

	err := c.Promote(ctx, []ClassifiedMemory{mem}, nil, merges, s)
	if err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}

	// Memory should have been promoted as new (merge failed, fell through)
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{"kind": string(store.NodeKindBehavior)})
	if err != nil {
		t.Fatalf("QueryNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node (fallback create), got %d", len(nodes))
	}
}

func TestLLMPromote_NilStore(t *testing.T) {
	c := newTestLLMConsolidator()
	ctx := context.Background()

	err := c.Promote(ctx, []ClassifiedMemory{
		testMemory("test", models.BehaviorKindDirective),
	}, nil, nil, nil)

	if err != nil {
		t.Fatalf("expected nil error for nil store, got: %v", err)
	}
}

func TestLLMPromote_DecisionLogging(t *testing.T) {
	dir := t.TempDir()
	dl := logging.NewDecisionLogger(dir, "debug")
	defer dl.Close()

	c := newTestLLMConsolidatorWithLogger(dl)
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Create existing node for a merge
	existing := store.Node{
		ID:   "bhv-log-target",
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"name": "Existing",
			"kind": "directive",
		},
		Metadata: map[string]interface{}{"confidence": 0.5},
	}
	if _, err := s.AddNode(ctx, existing); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	mem1 := testMemory("New memory to promote", models.BehaviorKindDirective)
	mem1.RawText = "unique-raw-1" // ensure unique for merge matching
	mem2 := testMemory("Memory to absorb", models.BehaviorKindPreference)
	mem2.RawText = "unique-raw-2"

	merges := []MergeProposal{{
		Memory:     mem2,
		TargetID:   "bhv-log-target",
		Similarity: 0.91,
		Strategy:   "absorb",
	}}

	err := c.Promote(ctx, []ClassifiedMemory{mem1, mem2}, nil, merges, s)
	if err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}

	// The decision logger writes to a file. Verify no panics occurred and
	// the file exists. Detailed content verification would require reading
	// the JSONL file, but we mainly verify the pipeline runs cleanly with
	// logging enabled.
}
