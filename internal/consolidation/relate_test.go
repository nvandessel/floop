package consolidation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// mockLLMClient implements llm.Client for testing.
type mockLLMClient struct {
	response  string
	err       error
	available bool
}

func (m *mockLLMClient) Complete(_ context.Context, _ []llm.Message) (string, error) {
	return m.response, m.err
}

func (m *mockLLMClient) Available() bool {
	return m.available
}

// mockEmbeddingClient implements both llm.Client and llm.EmbeddingComparer.
type mockEmbeddingClient struct {
	mockLLMClient
	embeddings map[string][]float32
}

func (m *mockEmbeddingClient) Embed(_ context.Context, text string) ([]float32, error) {
	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}
	// Return a default embedding.
	return []float32{0.5, 0.5, 0.5}, nil
}

func (m *mockEmbeddingClient) CompareEmbeddings(_ context.Context, _, _ string) (float64, error) {
	return 0.8, nil
}

// testMemories returns a slice of classified memories for testing.
func testMemories(sessionID string) []ClassifiedMemory {
	return []ClassifiedMemory{
		{
			Candidate: Candidate{
				SourceEvents:  []string{"evt-1"},
				RawText:       "Use fmt.Errorf to wrap errors",
				CandidateType: "correction",
				Confidence:    0.7,
				SessionContext: map[string]any{
					"session_id": sessionID,
				},
			},
			Kind:       models.BehaviorKindDirective,
			MemoryType: models.MemoryTypeSemantic,
			Scope:      "universal",
			Content: models.BehaviorContent{
				Canonical: "Use fmt.Errorf to wrap errors",
				Summary:   "Use fmt.Errorf to wrap errors",
				Tags:      []string{"errors", "fmt"},
			},
		},
	}
}

// testMemoriesMultiSession returns memories across sessions for co-occurrence testing.
func testMemoriesMultiSession() []ClassifiedMemory {
	return []ClassifiedMemory{
		{
			Candidate: Candidate{
				SourceEvents:   []string{"evt-1"},
				RawText:        "Use fmt.Errorf to wrap errors",
				CandidateType:  "correction",
				Confidence:     0.7,
				SessionContext: map[string]any{"session_id": "sess-1"},
			},
			Kind:       models.BehaviorKindDirective,
			MemoryType: models.MemoryTypeSemantic,
			Content:    models.BehaviorContent{Canonical: "Use fmt.Errorf to wrap errors"},
		},
		{
			Candidate: Candidate{
				SourceEvents:   []string{"evt-2"},
				RawText:        "Prefer table-driven tests",
				CandidateType:  "correction",
				Confidence:     0.6,
				SessionContext: map[string]any{"session_id": "sess-1"},
			},
			Kind:       models.BehaviorKindDirective,
			MemoryType: models.MemoryTypeSemantic,
			Content:    models.BehaviorContent{Canonical: "Prefer table-driven tests"},
		},
		{
			Candidate: Candidate{
				SourceEvents:   []string{"evt-3"},
				RawText:        "Always check context cancellation",
				CandidateType:  "correction",
				Confidence:     0.8,
				SessionContext: map[string]any{"session_id": "sess-1"},
			},
			Kind:       models.BehaviorKindDirective,
			MemoryType: models.MemoryTypeSemantic,
			Content:    models.BehaviorContent{Canonical: "Always check context cancellation"},
		},
	}
}

// makeLLMResponse creates a valid LLM JSON response for testing.
func makeLLMResponse(proposals []relateProposal) string {
	resp := relateResponse{Relationships: proposals}
	b, _ := json.Marshal(resp)
	return string(b)
}

// seedStore adds a behavior node to the store and returns the store.
func seedStore(ctx context.Context, t *testing.T, s *store.InMemoryGraphStore, id, canonical string) {
	t.Helper()
	_, err := s.AddNode(ctx, store.Node{
		ID:   id,
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": canonical,
			},
		},
	})
	if err != nil {
		t.Fatalf("seeding store: %v", err)
	}
}

func TestLLMRelate_WithNeighbors(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()
	seedStore(ctx, t, s, "bhv-100", "Always wrap errors with context")

	// Store an embedding for the existing behavior.
	emb := []float32{0.9, 0.1, 0.0}
	if err := s.StoreEmbedding(ctx, "bhv-100", emb, "test-model"); err != nil {
		t.Fatalf("storing embedding: %v", err)
	}

	response := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges: []proposedEdge{
				{Target: "bhv-100", Kind: "similar-to", Weight: 0.85},
			},
			Rationale: "Related error handling pattern",
		},
	})

	client := &mockEmbeddingClient{
		mockLLMClient: mockLLMClient{
			response:  response,
			available: true,
		},
		embeddings: map[string][]float32{
			"Use fmt.Errorf to wrap errors": {0.8, 0.2, 0.0},
		},
	}

	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
	memories := testMemories("sess-1")

	edges, merges, _, err := c.Relate(ctx, memories, s)
	if err != nil {
		t.Fatalf("Relate returned error: %v", err)
	}

	// Should have at least 1 LLM-proposed edge.
	var llmEdges int
	for _, e := range edges {
		if e.Kind == store.EdgeKindSimilarTo {
			llmEdges++
		}
	}
	if llmEdges == 0 {
		t.Error("expected at least one similar-to edge from LLM proposals")
	}

	if len(merges) != 0 {
		t.Errorf("expected 0 merges, got %d", len(merges))
	}
}

func TestLLMRelate_NoEmbeddings(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()
	seedStore(ctx, t, s, "bhv-200", "Use context.Context everywhere")

	// Client without EmbeddingComparer — should fall back to QueryNodes.
	response := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges: []proposedEdge{
				{Target: "bhv-200", Kind: "similar-to", Weight: 0.7},
			},
			Rationale: "Falls back to unranked neighbors",
		},
	})

	client := &mockLLMClient{
		response:  response,
		available: true,
	}

	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
	memories := testMemories("sess-2")

	edges, _, _, err := c.Relate(ctx, memories, s)
	if err != nil {
		t.Fatalf("Relate returned error: %v", err)
	}

	// Should still produce edges from LLM + co-occurrence.
	if len(edges) == 0 {
		t.Error("expected edges even without embeddings")
	}
}

func TestLLMRelate_LLMFailure(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	client := &mockLLMClient{
		err:       errors.New("API rate limited"),
		available: true,
	}

	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
	memories := testMemoriesMultiSession() // 3 memories, same session

	edges, merges, _, err := c.Relate(ctx, memories, s)
	if err != nil {
		t.Fatalf("Relate should not return error on LLM failure, got: %v", err)
	}

	// LLM failed, so only co-occurrence edges should exist.
	if len(merges) != 0 {
		t.Errorf("expected 0 merges on LLM failure, got %d", len(merges))
	}

	// 3 memories in same session → 3 co-occurrence edges (3 choose 2).
	var coEdges int
	for _, e := range edges {
		if e.Kind == store.EdgeKindCoActivated {
			coEdges++
		}
	}
	if coEdges != 3 {
		t.Errorf("expected 3 co-occurrence edges, got %d", coEdges)
	}
}

func TestLLMRelate_MergeProposal(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()
	seedStore(ctx, t, s, "bhv-300", "Wrap errors with fmt.Errorf")

	response := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "merge",
			MergeInto: &mergeInfo{
				TargetID: "bhv-300",
				Strategy: "absorb",
			},
			Rationale: "Near-duplicate of existing behavior",
		},
	})

	client := &mockLLMClient{
		response:  response,
		available: true,
	}

	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
	memories := testMemories("sess-3")

	_, merges, _, err := c.Relate(ctx, memories, s)
	if err != nil {
		t.Fatalf("Relate returned error: %v", err)
	}

	if len(merges) != 1 {
		t.Fatalf("expected 1 merge, got %d", len(merges))
	}

	merge := merges[0]
	if merge.TargetID != "bhv-300" {
		t.Errorf("expected target bhv-300, got %q", merge.TargetID)
	}
	if merge.Strategy != "absorb" {
		t.Errorf("expected absorb strategy, got %q", merge.Strategy)
	}
	if merge.Memory.RawText != "Use fmt.Errorf to wrap errors" {
		t.Errorf("unexpected memory text: %q", merge.Memory.RawText)
	}
}

func TestLLMRelate_MergeProposalCarriesCosineSimilarity(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()
	seedStore(ctx, t, s, "bhv-400", "Wrap errors with fmt.Errorf")

	// Store embedding so vector search finds the neighbor.
	if err := s.StoreEmbedding(ctx, "bhv-400", []float32{0.9, 0.1, 0.0}, "test-model"); err != nil {
		t.Fatalf("storing embedding: %v", err)
	}

	response := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "merge",
			MergeInto:   &mergeInfo{TargetID: "bhv-400", Strategy: "absorb"},
			Rationale:   "Near-duplicate",
		},
	})

	client := &mockEmbeddingClient{
		mockLLMClient: mockLLMClient{response: response, available: true},
		embeddings: map[string][]float32{
			"Use fmt.Errorf to wrap errors": {0.8, 0.2, 0.0},
		},
	}

	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
	memories := testMemories("sess-sim")

	_, merges, _, err := c.Relate(ctx, memories, s)
	if err != nil {
		t.Fatalf("Relate returned error: %v", err)
	}

	if len(merges) != 1 {
		t.Fatalf("expected 1 merge, got %d", len(merges))
	}

	// Similarity should be the actual cosine score, not 0.0 or a hardcoded value.
	if merges[0].Similarity <= 0.0 {
		t.Errorf("expected positive similarity from cosine score, got %f", merges[0].Similarity)
	}
	if merges[0].Similarity == 0.5 {
		t.Errorf("similarity should be actual cosine score, not hardcoded 0.5; got %f", merges[0].Similarity)
	}
}

func TestLLMRelate_CoOccurrence(t *testing.T) {
	memories := testMemoriesMultiSession() // 3 memories, same session
	edges := buildCoOccurrenceEdges(memories)

	// 3 memories in same session → C(3,2) = 3 co-activated edges.
	if len(edges) != 3 {
		t.Fatalf("expected 3 co-occurrence edges, got %d", len(edges))
	}

	for _, e := range edges {
		if e.Kind != store.EdgeKindCoActivated {
			t.Errorf("expected co-activated edge, got %q", e.Kind)
		}
		if e.Weight != 0.5 {
			t.Errorf("expected weight 0.5, got %f", e.Weight)
		}
		if e.CreatedAt.IsZero() {
			t.Error("expected non-zero CreatedAt")
		}
	}
}

func TestLLMRelate_CoOccurrence_DifferentSessions(t *testing.T) {
	memories := []ClassifiedMemory{
		{
			Candidate: Candidate{
				SourceEvents:   []string{"evt-a"},
				SessionContext: map[string]any{"session_id": "sess-A"},
			},
		},
		{
			Candidate: Candidate{
				SourceEvents:   []string{"evt-b"},
				SessionContext: map[string]any{"session_id": "sess-B"},
			},
		},
	}

	edges := buildCoOccurrenceEdges(memories)
	if len(edges) != 0 {
		t.Errorf("expected 0 co-occurrence edges for different sessions, got %d", len(edges))
	}
}

func TestLLMRelate_SkipAction(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	response := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "skip",
			Rationale:   "Already captured by existing behavior",
		},
	})

	client := &mockLLMClient{
		response:  response,
		available: true,
	}

	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
	memories := testMemories("sess-4")

	edges, merges, skips, err := c.Relate(ctx, memories, s)
	if err != nil {
		t.Fatalf("Relate returned error: %v", err)
	}

	// Skip should produce no LLM edges and no merges.
	for _, e := range edges {
		if e.Kind != store.EdgeKindCoActivated {
			t.Errorf("expected only co-occurrence edges for skip, got %q", e.Kind)
		}
	}
	if len(merges) != 0 {
		t.Errorf("expected 0 merges for skip, got %d", len(merges))
	}
	// Should report the skipped memory index.
	if len(skips) != 1 {
		t.Fatalf("expected 1 skip, got %d", len(skips))
	}
	if skips[0] != 0 {
		t.Errorf("expected skip index 0, got %d", skips[0])
	}
}

func TestLLMRelate_EmptyMemories(t *testing.T) {
	ctx := context.Background()
	client := &mockLLMClient{available: true}
	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())

	edges, merges, skips, err := c.Relate(ctx, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edges != nil {
		t.Errorf("expected nil edges, got %v", edges)
	}
	if merges != nil {
		t.Errorf("expected nil merges, got %v", merges)
	}
	if skips != nil {
		t.Errorf("expected nil skips, got %v", skips)
	}
}

func TestLLMRelate_NilStore(t *testing.T) {
	ctx := context.Background()

	// With nil store, there are no neighbors, so edge targets cannot be validated.
	// The LLM proposes an edge to "bhv-1" which is not in the (empty) neighbor set.
	// convertProposals correctly drops it as a hallucinated target.
	response := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges:       []proposedEdge{{Target: "bhv-1", Kind: "similar-to", Weight: 0.8}},
		},
	})

	client := &mockLLMClient{response: response, available: true}
	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
	memories := testMemories("sess-5")

	_, _, _, err := c.Relate(ctx, memories, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With no neighbors and no co-occurrence (single memory), no edges are expected.
}

func TestParseRelationships_ValidJSON(t *testing.T) {
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges:       []proposedEdge{{Target: "bhv-1", Kind: "similar-to", Weight: 0.9}},
			Rationale:   "test",
		},
		{
			MemoryIndex: 1,
			Action:      "merge",
			MergeInto:   &mergeInfo{TargetID: "bhv-2", Strategy: "supersede"},
			Rationale:   "duplicate",
		},
		{
			MemoryIndex: 2,
			Action:      "skip",
			Rationale:   "already exists",
		},
	})

	proposals, err := ParseRelationships(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proposals) != 3 {
		t.Fatalf("expected 3 proposals, got %d", len(proposals))
	}

	if proposals[0].Action != "create" {
		t.Errorf("proposal 0: expected create, got %q", proposals[0].Action)
	}
	if proposals[1].Action != "merge" {
		t.Errorf("proposal 1: expected merge, got %q", proposals[1].Action)
	}
	if proposals[1].MergeInto.Strategy != "supersede" {
		t.Errorf("proposal 1: expected supersede, got %q", proposals[1].MergeInto.Strategy)
	}
	if proposals[2].Action != "skip" {
		t.Errorf("proposal 2: expected skip, got %q", proposals[2].Action)
	}
}

func TestParseRelationships_MarkdownFence(t *testing.T) {
	inner := makeLLMResponse([]relateProposal{
		{MemoryIndex: 0, Action: "skip", Rationale: "test"},
	})
	fenced := "```json\n" + inner + "\n```"

	proposals, err := ParseRelationships(fenced)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
}

func TestParseRelationships_InvalidAction(t *testing.T) {
	input := makeLLMResponse([]relateProposal{
		{MemoryIndex: 0, Action: "destroy"},
	})
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for invalid action")
	}
}

func TestParseRelationships_MergeWithoutTarget(t *testing.T) {
	input := makeLLMResponse([]relateProposal{
		{MemoryIndex: 0, Action: "merge"},
	})
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for merge without merge_into")
	}
}

func TestParseRelationships_InvalidEdgeKind(t *testing.T) {
	// Invalid edge kind no longer fails ParseRelationships — it is filtered
	// per-edge in convertProposals so one bad edge doesn't discard all proposals.
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges:       []proposedEdge{{Target: "bhv-1", Kind: "invented-kind", Weight: 0.5}},
		},
	})
	proposals, err := ParseRelationships(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
	// The bad edge should be dropped by convertProposals, not ParseRelationships.
	memories := testMemories("test")
	neighbors := map[int][]scoredNode{0: {{Node: store.Node{ID: "bhv-1"}}}}
	edges, _, _ := convertProposals(proposals, memories, neighbors)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after filtering invalid kind, got %d", len(edges))
	}
}

func TestParseRelationships_InvalidWeight(t *testing.T) {
	// Zero weight no longer fails ParseRelationships — it is filtered per-edge.
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges:       []proposedEdge{{Target: "bhv-1", Kind: "similar-to", Weight: 0.0}},
		},
	})
	proposals, err := ParseRelationships(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	memories := testMemories("test")
	neighbors := map[int][]scoredNode{0: {{Node: store.Node{ID: "bhv-1"}}}}
	edges, _, _ := convertProposals(proposals, memories, neighbors)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after filtering zero weight, got %d", len(edges))
	}
}

func TestParseRelationships_InvalidMergeStrategy(t *testing.T) {
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "merge",
			MergeInto:   &mergeInfo{TargetID: "bhv-1", Strategy: "destroy"},
		},
	})
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for invalid merge strategy")
	}
}

func TestParseRelationships_AllMergeStrategies(t *testing.T) {
	strategies := []string{"absorb", "supersede", "supplement"}
	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			input := makeLLMResponse([]relateProposal{
				{
					MemoryIndex: 0,
					Action:      "merge",
					MergeInto:   &mergeInfo{TargetID: "bhv-1", Strategy: strategy},
				},
			})
			proposals, err := ParseRelationships(input)
			if err != nil {
				t.Fatalf("unexpected error for strategy %q: %v", strategy, err)
			}
			if proposals[0].MergeInto.Strategy != strategy {
				t.Errorf("expected strategy %q, got %q", strategy, proposals[0].MergeInto.Strategy)
			}
		})
	}
}

func TestParseRelationships_EmptyMergeTargetID(t *testing.T) {
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "merge",
			MergeInto:   &mergeInfo{TargetID: "", Strategy: "absorb"},
		},
	})
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for empty merge target_id")
	}
}

func TestParseRelationships_EmptyEdgeTarget(t *testing.T) {
	// Empty edge target no longer fails ParseRelationships — it is filtered per-edge.
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges:       []proposedEdge{{Target: "", Kind: "similar-to", Weight: 0.8}},
		},
	})
	proposals, err := ParseRelationships(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	memories := testMemories("test")
	edges, _, _ := convertProposals(proposals, memories, nil)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after filtering empty target, got %d", len(edges))
	}
}

func TestParseRelationships_DuplicateMemoryIndex(t *testing.T) {
	input := makeLLMResponse([]relateProposal{
		{MemoryIndex: 0, Action: "skip", Rationale: "first"},
		{MemoryIndex: 0, Action: "skip", Rationale: "duplicate"},
	})
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for duplicate memory_index")
	}
}

func TestParseRelationships_FenceMissingClose(t *testing.T) {
	// LLM returns opening fence + JSON, no closing fence.
	inner := makeLLMResponse([]relateProposal{
		{MemoryIndex: 0, Action: "skip", Rationale: "test"},
	})
	fenced := "```json\n" + inner

	proposals, err := ParseRelationships(fenced)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}
}

func TestConvertProposals_PartialEdgeValidity(t *testing.T) {
	// One valid edge + one bad edge (invalid kind) → only the valid edge survives.
	proposals := []relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges: []proposedEdge{
				{Target: "bhv-1", Kind: "similar-to", Weight: 0.8},
				{Target: "bhv-1", Kind: "invented-kind", Weight: 0.5},
			},
		},
	}
	memories := testMemories("test")
	neighbors := map[int][]scoredNode{0: {{Node: store.Node{ID: "bhv-1"}}}}
	edges, _, _ := convertProposals(proposals, memories, neighbors)
	if len(edges) != 1 {
		t.Fatalf("expected 1 valid edge, got %d", len(edges))
	}
	if edges[0].Kind != store.EdgeKindSimilarTo {
		t.Errorf("expected similar-to edge, got %q", edges[0].Kind)
	}
}

func TestConvertProposals_HallucinatedEdgeTarget(t *testing.T) {
	// LLM invents a target ID that wasn't in the neighbor set → edge dropped.
	proposals := []relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges: []proposedEdge{
				{Target: "bhv-999-hallucinated", Kind: "similar-to", Weight: 0.8},
			},
		},
	}
	memories := testMemories("test")
	neighbors := map[int][]scoredNode{0: {{Node: store.Node{ID: "bhv-1"}}}}
	edges, _, _ := convertProposals(proposals, memories, neighbors)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for hallucinated target, got %d", len(edges))
	}
}

func TestConvertProposals_HallucinatedMergeTarget(t *testing.T) {
	// LLM proposes merging into a node that wasn't a neighbor → merge dropped.
	proposals := []relateProposal{
		{
			MemoryIndex: 0,
			Action:      "merge",
			MergeInto:   &mergeInfo{TargetID: "bhv-999-hallucinated", Strategy: "absorb"},
		},
	}
	memories := testMemories("test")
	neighbors := map[int][]scoredNode{0: {{Node: store.Node{ID: "bhv-1"}}}}
	_, merges, _ := convertProposals(proposals, memories, neighbors)
	if len(merges) != 0 {
		t.Errorf("expected 0 merges for hallucinated target, got %d", len(merges))
	}
}

func TestConvertProposals_PendingIDEdgeTarget(t *testing.T) {
	// LLM references another pending memory as edge target → allowed.
	proposals := []relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges: []proposedEdge{
				{Target: "pending-1", Kind: "similar-to", Weight: 0.7},
			},
		},
	}
	memories := testMemoriesMultiSession()[:2]
	edges, _, _ := convertProposals(proposals, memories, nil)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge for pending-N target, got %d", len(edges))
	}
	if edges[0].Target != "pending-1" {
		t.Errorf("expected target pending-1, got %q", edges[0].Target)
	}
}

func TestParseRelationships_NegativeMemoryIndex(t *testing.T) {
	input := `{"relationships":[{"memory_index":-1,"action":"skip","rationale":"test"}]}`
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for negative memory_index")
	}
}

func TestConvertProposals_MissingWeight(t *testing.T) {
	// LLM omits weight field → unmarshals to 0.0 → edge skipped (not all proposals lost).
	proposals := []relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges: []proposedEdge{
				{Target: "bhv-1", Kind: "similar-to", Weight: 0.0}, // omitted weight
				{Target: "bhv-1", Kind: "overrides", Weight: 0.9},  // valid
			},
		},
	}
	memories := testMemories("test")
	neighbors := map[int][]scoredNode{0: {{Node: store.Node{ID: "bhv-1"}}}}
	edges, _, _ := convertProposals(proposals, memories, neighbors)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge (bad weight filtered), got %d", len(edges))
	}
	if edges[0].Kind != store.EdgeKindOverrides {
		t.Errorf("expected overrides edge to survive, got %q", edges[0].Kind)
	}
}

func TestLLMRelate_EmbeddingFiltersBehaviorKind(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	// Seed a behavior node and a non-behavior (context-snapshot) node.
	seedStore(ctx, t, s, "bhv-500", "Use context.Context everywhere")
	_, err := s.AddNode(ctx, store.Node{
		ID:   "ctx-snap-1",
		Kind: store.NodeKindContextSnapshot,
		Content: map[string]interface{}{
			"kind":    "context-snapshot",
			"content": map[string]interface{}{"canonical": "Snapshot of session state"},
		},
	})
	if err != nil {
		t.Fatalf("adding context-snapshot node: %v", err)
	}

	// Store embeddings for both — the non-behavior node should be filtered out.
	if err := s.StoreEmbedding(ctx, "bhv-500", []float32{0.9, 0.1, 0.0}, "test-model"); err != nil {
		t.Fatalf("storing bhv embedding: %v", err)
	}
	if err := s.StoreEmbedding(ctx, "ctx-snap-1", []float32{0.85, 0.15, 0.0}, "test-model"); err != nil {
		t.Fatalf("storing ctx-snap embedding: %v", err)
	}

	response := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges: []proposedEdge{
				{Target: "bhv-500", Kind: "similar-to", Weight: 0.8},
			},
		},
	})

	client := &mockEmbeddingClient{
		mockLLMClient: mockLLMClient{response: response, available: true},
		embeddings: map[string][]float32{
			"Use fmt.Errorf to wrap errors": {0.8, 0.2, 0.0},
		},
	}

	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
	memories := testMemories("sess-kind-filter")

	edges, _, _, err := c.Relate(ctx, memories, s)
	if err != nil {
		t.Fatalf("Relate returned error: %v", err)
	}

	// Verify only behavior nodes ended up as neighbors — the context-snapshot
	// should have been filtered out. The LLM response only references bhv-500,
	// so we should get exactly 1 LLM edge.
	var llmEdges int
	for _, e := range edges {
		if e.Kind == store.EdgeKindSimilarTo {
			llmEdges++
			if e.Target != "bhv-500" {
				t.Errorf("expected edge to bhv-500, got %q", e.Target)
			}
		}
	}
	if llmEdges != 1 {
		t.Errorf("expected 1 similar-to edge, got %d", llmEdges)
	}
}

func TestRelateMemoriesPrompt(t *testing.T) {
	memories := testMemories("sess-1")
	neighbors := map[int][]scoredNode{
		0: {
			{
				Node: store.Node{
					ID:   "bhv-50",
					Kind: store.NodeKindBehavior,
					Content: map[string]interface{}{
						"kind": "directive",
						"content": map[string]interface{}{
							"canonical": "Always wrap errors",
						},
					},
				},
				Score: 0.85,
			},
		},
	}

	msgs, err := RelateMemoriesPrompt(memories, neighbors)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected system role, got %q", msgs[0].Role)
	}
	if msgs[1].Role != "user" {
		t.Errorf("expected user role, got %q", msgs[1].Role)
	}

	// Verify user message contains memory and neighbor data.
	if len(msgs[1].Content) == 0 {
		t.Error("user message content is empty")
	}

	// Verify system prompt contains merge_into example with target_id field
	// so the LLM emits the correct JSON schema that ParseRelationships expects.
	sysContent := msgs[0].Content
	if !strings.Contains(sysContent, `"merge_into"`) {
		t.Error("system prompt missing merge_into example")
	}
	if !strings.Contains(sysContent, `"target_id"`) {
		t.Error("system prompt missing target_id in merge_into example")
	}
	if !strings.Contains(sysContent, `"strategy"`) {
		t.Error("system prompt missing strategy in merge_into example")
	}
	if !strings.Contains(sysContent, `"action": "merge"`) {
		t.Error("system prompt missing merge action example")
	}
}
