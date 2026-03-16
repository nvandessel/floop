package consolidation

import (
	"context"
	"encoding/json"
	"errors"
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

	edges, merges, err := c.Relate(ctx, memories, s)
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

	edges, _, err := c.Relate(ctx, memories, s)
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

	edges, merges, err := c.Relate(ctx, memories, s)
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

	_, merges, err := c.Relate(ctx, memories, s)
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

	edges, merges, err := c.Relate(ctx, memories, s)
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
}

func TestLLMRelate_EmptyMemories(t *testing.T) {
	ctx := context.Background()
	client := &mockLLMClient{available: true}
	c := NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())

	edges, merges, err := c.Relate(ctx, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edges != nil {
		t.Errorf("expected nil edges, got %v", edges)
	}
	if merges != nil {
		t.Errorf("expected nil merges, got %v", merges)
	}
}

func TestLLMRelate_NilStore(t *testing.T) {
	ctx := context.Background()

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

	edges, _, err := c.Relate(ctx, memories, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still work — just no neighbor search.
	if len(edges) == 0 {
		t.Error("expected at least LLM-proposed edges")
	}
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
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges:       []proposedEdge{{Target: "bhv-1", Kind: "invented-kind", Weight: 0.5}},
		},
	})
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for invalid edge kind")
	}
}

func TestParseRelationships_InvalidWeight(t *testing.T) {
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges:       []proposedEdge{{Target: "bhv-1", Kind: "similar-to", Weight: 0.0}},
		},
	})
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for zero weight")
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
	input := makeLLMResponse([]relateProposal{
		{
			MemoryIndex: 0,
			Action:      "create",
			Edges:       []proposedEdge{{Target: "", Kind: "similar-to", Weight: 0.8}},
		},
	})
	_, err := ParseRelationships(input)
	if err == nil {
		t.Error("expected error for empty edge target")
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

func TestRelateMemoriesPrompt(t *testing.T) {
	memories := testMemories("sess-1")
	neighbors := map[int][]store.Node{
		0: {
			{
				ID:   "bhv-50",
				Kind: store.NodeKindBehavior,
				Content: map[string]interface{}{
					"kind": "directive",
					"content": map[string]interface{}{
						"canonical": "Always wrap errors",
					},
				},
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
}
