package consolidation

import (
	"context"
	"testing"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

func TestHeuristicExtract_Correction(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	evts := []events.Event{
		{
			ID:        "evt-1",
			SessionID: "sess-1",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "No, don't do that. Instead use fmt.Errorf to wrap errors.",
			ProjectID: "proj-1",
		},
	}

	candidates, err := h.Extract(ctx, evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	c := candidates[0]
	if c.CandidateType != "correction" {
		t.Errorf("expected candidate type 'correction', got %q", c.CandidateType)
	}
	if c.Confidence != 0.7 {
		t.Errorf("expected confidence 0.7, got %f", c.Confidence)
	}
	if len(c.SourceEvents) != 1 || c.SourceEvents[0] != "evt-1" {
		t.Errorf("expected source event 'evt-1', got %v", c.SourceEvents)
	}
}

func TestHeuristicExtract_NoSignal(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	evts := []events.Event{
		{
			ID:        "evt-2",
			SessionID: "sess-1",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "Here is the code you requested.",
		},
	}

	candidates, err := h.Extract(ctx, evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestHeuristicExtract_SkipsNonUser(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	evts := []events.Event{
		{
			ID:      "evt-3",
			Actor:   events.ActorAgent,
			Kind:    events.KindMessage,
			Content: "No, don't do that. Instead use fmt.Errorf to wrap errors.",
		},
	}

	candidates, err := h.Extract(ctx, evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates for agent message, got %d", len(candidates))
	}
}

func TestHeuristicExtract_SkipsShortMessages(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	evts := []events.Event{
		{
			ID:      "evt-4",
			Actor:   events.ActorUser,
			Kind:    events.KindMessage,
			Content: "wrong",
		},
	}

	candidates, err := h.Extract(ctx, evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 0 {
		t.Fatalf("expected 0 candidates for short message, got %d", len(candidates))
	}
}

func TestHeuristicExtract_Decision(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	evts := []events.Event{
		{
			ID:      "evt-5",
			Actor:   events.ActorUser,
			Kind:    events.KindMessage,
			Content: "Let's go with the SQLite approach for local storage.",
		},
	}

	candidates, err := h.Extract(ctx, evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].CandidateType != "decision" {
		t.Errorf("expected 'decision', got %q", candidates[0].CandidateType)
	}
	if candidates[0].Confidence != 0.5 {
		t.Errorf("expected confidence 0.5, got %f", candidates[0].Confidence)
	}
}

func TestHeuristicExtract_Failure(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	evts := []events.Event{
		{
			ID:      "evt-6",
			Actor:   events.ActorUser,
			Kind:    events.KindMessage,
			Content: "That didn't work because the module was missing.",
		},
	}

	candidates, err := h.Extract(ctx, evts)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].CandidateType != "failure" {
		t.Errorf("expected 'failure', got %q", candidates[0].CandidateType)
	}
	if candidates[0].Confidence != 0.6 {
		t.Errorf("expected confidence 0.6, got %f", candidates[0].Confidence)
	}
}

func TestHeuristicClassify(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	candidates := []Candidate{
		{
			SourceEvents:  []string{"evt-1"},
			RawText:       "No, don't do that. Instead use fmt.Errorf to wrap errors.",
			CandidateType: "correction",
			Confidence:    0.7,
		},
		{
			SourceEvents:  []string{"evt-2"},
			RawText:       "That didn't work because the module was missing.",
			CandidateType: "failure",
			Confidence:    0.6,
		},
		{
			SourceEvents:  []string{"evt-3"},
			RawText:       "Let's go with the SQLite approach for local storage.",
			CandidateType: "decision",
			Confidence:    0.5,
		},
	}

	memories, err := h.Classify(ctx, candidates)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}

	if len(memories) != 3 {
		t.Fatalf("expected 3 classified memories, got %d", len(memories))
	}

	// Correction -> directive, semantic
	if memories[0].Kind != models.BehaviorKindDirective {
		t.Errorf("correction: expected directive, got %q", memories[0].Kind)
	}
	if memories[0].MemoryType != models.MemoryTypeSemantic {
		t.Errorf("correction: expected semantic, got %q", memories[0].MemoryType)
	}

	// Failure -> episodic, episodic
	if memories[1].Kind != models.BehaviorKindEpisodic {
		t.Errorf("failure: expected episodic, got %q", memories[1].Kind)
	}
	if memories[1].MemoryType != models.MemoryTypeEpisodic {
		t.Errorf("failure: expected episodic memory type, got %q", memories[1].MemoryType)
	}

	// Decision -> preference, semantic
	if memories[2].Kind != models.BehaviorKindPreference {
		t.Errorf("decision: expected preference, got %q", memories[2].Kind)
	}

	// Check scope defaults to universal
	for i, mem := range memories {
		if mem.Scope != "universal" {
			t.Errorf("memory[%d]: expected scope 'universal', got %q", i, mem.Scope)
		}
	}

	// Check content generation
	if memories[0].Content.Canonical != candidates[0].RawText {
		t.Errorf("expected canonical to match raw text")
	}
	if len(memories[0].Content.Tags) == 0 {
		t.Errorf("expected tags to be extracted")
	}
}

func TestHeuristicPromote(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	memories := []ClassifiedMemory{
		{
			Candidate: Candidate{
				SourceEvents:  []string{"evt-1"},
				RawText:       "Use fmt.Errorf to wrap errors.",
				CandidateType: "correction",
				Confidence:    0.7,
			},
			Kind:       models.BehaviorKindDirective,
			MemoryType: models.MemoryTypeSemantic,
			Scope:      "universal",
			Content: models.BehaviorContent{
				Canonical: "Use fmt.Errorf to wrap errors.",
				Summary:   "Use fmt.Errorf to wrap errors.",
				Tags:      []string{"fmt.errorf", "wrap", "errors"},
			},
		},
	}

	s := store.NewInMemoryGraphStore()

	err := h.Promote(ctx, memories, nil, nil, s)
	if err != nil {
		t.Fatalf("Promote returned error: %v", err)
	}

	// Verify the node was created
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{
		"kind": "behavior",
	})
	if err != nil {
		t.Fatalf("QueryNodes returned error: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	// Verify nested content schema matches BehaviorToNode/NodeToBehavior
	if node.Content["kind"] != "directive" {
		t.Errorf("unexpected kind: %v", node.Content["kind"])
	}
	if node.Content["memory_type"] != "semantic" {
		t.Errorf("unexpected memory_type: %v", node.Content["memory_type"])
	}
	contentMap, ok := node.Content["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("content is not a map: %T", node.Content["content"])
	}
	if contentMap["canonical"] != "Use fmt.Errorf to wrap errors." {
		t.Errorf("unexpected canonical: %v", contentMap["canonical"])
	}
	provMap, ok := node.Metadata["provenance"].(map[string]interface{})
	if !ok {
		t.Fatalf("provenance is not a map: %T", node.Metadata["provenance"])
	}
	if provMap["consolidated_by"] != "heuristic-v0" {
		t.Errorf("unexpected consolidated_by: %v", provMap["consolidated_by"])
	}
}

func TestHeuristicPromote_NilStore(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	memories := []ClassifiedMemory{
		{
			Candidate: Candidate{RawText: "test"},
			Kind:      models.BehaviorKindDirective,
		},
	}

	err := h.Promote(ctx, memories, nil, nil, nil)
	if err != nil {
		t.Fatalf("Promote with nil store should not error, got: %v", err)
	}
}

func TestHeuristicRelate(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	memories := []ClassifiedMemory{
		{Kind: models.BehaviorKindDirective},
	}

	edges, merges, err := h.Relate(ctx, memories, nil)
	if err != nil {
		t.Fatalf("Relate returned error: %v", err)
	}
	if edges != nil {
		t.Errorf("expected nil edges, got %v", edges)
	}
	if merges != nil {
		t.Errorf("expected nil merges, got %v", merges)
	}
}
