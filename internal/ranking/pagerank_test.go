package ranking

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// addBehaviorNode is a test helper that adds a behavior node to the store.
func addBehaviorNode(t *testing.T, s store.GraphStore, id string) {
	t.Helper()
	_, err := s.AddNode(context.Background(), store.Node{
		ID:      id,
		Kind:    "behavior",
		Content: map[string]interface{}{"name": id},
	})
	if err != nil {
		t.Fatalf("failed to add node %s: %v", id, err)
	}
}

// addEdge is a test helper that adds an edge to the store.
func addEdge(t *testing.T, s store.GraphStore, source, target, kind string) {
	t.Helper()
	err := s.AddEdge(context.Background(), store.Edge{
		Source:    source,
		Target:    target,
		Kind:      kind,
		Weight:    1.0,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to add edge %s->%s: %v", source, target, err)
	}
}

func TestComputePageRank_EmptyGraph(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	scores, err := ComputePageRank(ctx, s, DefaultPageRankConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scores) != 0 {
		t.Errorf("expected empty map for empty graph, got %d entries", len(scores))
	}
}

func TestComputePageRank_SingleNode(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	addBehaviorNode(t, s, "A")

	scores, err := ComputePageRank(ctx, s, DefaultPageRankConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}

	// Single node should have PageRank = 1.0 (normalized max).
	if math.Abs(scores["A"]-1.0) > 0.001 {
		t.Errorf("single node PageRank = %f, want 1.0", scores["A"])
	}
}

func TestComputePageRank_LinearChain(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// A -> B -> C
	addBehaviorNode(t, s, "A")
	addBehaviorNode(t, s, "B")
	addBehaviorNode(t, s, "C")
	addEdge(t, s, "A", "B", "requires")
	addEdge(t, s, "B", "C", "requires")

	scores, err := ComputePageRank(ctx, s, DefaultPageRankConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scores) != 3 {
		t.Fatalf("expected 3 scores, got %d", len(scores))
	}

	// In a bidirectional linear chain A--B--C, the middle node B
	// should have the highest PageRank (more connections).
	if scores["B"] < scores["A"] {
		t.Errorf("middle node B (%f) should have higher PageRank than end node A (%f)",
			scores["B"], scores["A"])
	}
	if scores["B"] < scores["C"] {
		t.Errorf("middle node B (%f) should have higher PageRank than end node C (%f)",
			scores["B"], scores["C"])
	}

	// End nodes should have roughly equal scores due to symmetry.
	if math.Abs(scores["A"]-scores["C"]) > 0.01 {
		t.Errorf("end nodes A (%f) and C (%f) should have roughly equal PageRank",
			scores["A"], scores["C"])
	}
}

func TestComputePageRank_Hub(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Hub node connected to 5 leaf nodes.
	addBehaviorNode(t, s, "hub")
	for i := 0; i < 5; i++ {
		leafID := string(rune('a' + i))
		addBehaviorNode(t, s, leafID)
		addEdge(t, s, "hub", leafID, "requires")
	}

	scores, err := ComputePageRank(ctx, s, DefaultPageRankConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scores) != 6 {
		t.Fatalf("expected 6 scores, got %d", len(scores))
	}

	// Hub should have the highest PageRank.
	hubScore := scores["hub"]
	for i := 0; i < 5; i++ {
		leafID := string(rune('a' + i))
		if hubScore < scores[leafID] {
			t.Errorf("hub (%f) should have higher PageRank than leaf %s (%f)",
				hubScore, leafID, scores[leafID])
		}
	}

	// Hub should be normalized to 1.0 (max node).
	if math.Abs(hubScore-1.0) > 0.001 {
		t.Errorf("hub PageRank = %f, want 1.0 (normalized)", hubScore)
	}
}

func TestComputePageRank_Convergence(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Build a ring of 10 nodes.
	numNodes := 10
	ids := make([]string, numNodes)
	for i := 0; i < numNodes; i++ {
		ids[i] = string(rune('A' + i))
		addBehaviorNode(t, s, ids[i])
	}
	for i := 0; i < numNodes; i++ {
		addEdge(t, s, ids[i], ids[(i+1)%numNodes], "similar-to")
	}

	scores, err := ComputePageRank(ctx, s, DefaultPageRankConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scores) != numNodes {
		t.Fatalf("expected %d scores, got %d", numNodes, len(scores))
	}

	// In a ring, all nodes should have equal PageRank (symmetry).
	// After normalization, all should be ~1.0.
	for _, id := range ids {
		if math.Abs(scores[id]-1.0) > 0.01 {
			t.Errorf("ring node %s PageRank = %f, want ~1.0", id, scores[id])
		}
	}

	// Verify scores are in [0, 1].
	for id, score := range scores {
		if score < 0 || score > 1.0+0.001 {
			t.Errorf("score for %s = %f, want in [0, 1]", id, score)
		}
	}
}

func TestComputePageRank_Disconnected(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Subgraph 1: A -- B
	addBehaviorNode(t, s, "A")
	addBehaviorNode(t, s, "B")
	addEdge(t, s, "A", "B", "requires")

	// Subgraph 2: C -- D -- E
	addBehaviorNode(t, s, "C")
	addBehaviorNode(t, s, "D")
	addBehaviorNode(t, s, "E")
	addEdge(t, s, "C", "D", "similar-to")
	addEdge(t, s, "D", "E", "similar-to")

	scores, err := ComputePageRank(ctx, s, DefaultPageRankConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scores) != 5 {
		t.Fatalf("expected 5 scores, got %d", len(scores))
	}

	// All scores should be positive (teleportation ensures no zero scores).
	for id, score := range scores {
		if score <= 0 {
			t.Errorf("score for %s = %f, want positive", id, score)
		}
	}

	// Within subgraph 1, A and B should have similar scores (symmetric pair).
	if math.Abs(scores["A"]-scores["B"]) > 0.01 {
		t.Errorf("A (%f) and B (%f) should have roughly equal PageRank",
			scores["A"], scores["B"])
	}

	// D (middle of 3-chain) should score higher than C and E.
	if scores["D"] < scores["C"] {
		t.Errorf("D (%f) should have higher PageRank than C (%f)",
			scores["D"], scores["C"])
	}
	if scores["D"] < scores["E"] {
		t.Errorf("D (%f) should have higher PageRank than E (%f)",
			scores["D"], scores["E"])
	}
}
