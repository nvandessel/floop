package consolidation

import (
	"context"
	"testing"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/llm"
)

func TestNewConsolidator_HeuristicDefault(t *testing.T) {
	c := NewConsolidator("heuristic", nil, nil)
	if _, ok := c.(*HeuristicConsolidator); !ok {
		t.Errorf("expected *HeuristicConsolidator, got %T", c)
	}
}

func TestNewConsolidator_UnknownFallsBackToHeuristic(t *testing.T) {
	c := NewConsolidator("unknown", nil, nil)
	if _, ok := c.(*HeuristicConsolidator); !ok {
		t.Errorf("expected *HeuristicConsolidator, got %T", c)
	}
}

func TestNewConsolidator_EmptyFallsBackToHeuristic(t *testing.T) {
	c := NewConsolidator("", nil, nil)
	if _, ok := c.(*HeuristicConsolidator); !ok {
		t.Errorf("expected *HeuristicConsolidator, got %T", c)
	}
}

func TestNewConsolidator_LLM(t *testing.T) {
	mock := llm.NewMockClient()
	c := NewConsolidator("llm", mock, nil)
	if _, ok := c.(*LLMConsolidator); !ok {
		t.Errorf("expected *LLMConsolidator, got %T", c)
	}
}

func TestNewConsolidator_Local(t *testing.T) {
	c := NewConsolidator("local", nil, nil)
	// v2 not implemented, should fall back to heuristic
	if _, ok := c.(*HeuristicConsolidator); !ok {
		t.Errorf("expected *HeuristicConsolidator for 'local' (not yet implemented), got %T", c)
	}
}

func TestLLMConsolidator_DelegatesToHeuristic(t *testing.T) {
	mock := llm.NewMockClient()
	c := NewLLMConsolidator(mock, nil, DefaultLLMConsolidatorConfig())
	ctx := context.Background()

	// Create test events with a correction pattern
	evts := []events.Event{
		{
			ID:      "evt-1",
			Actor:   events.ActorUser,
			Content: "no, don't use pip, use uv instead",
		},
	}

	// Extract should work (delegated to heuristic)
	candidates, err := c.Extract(ctx, evts)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(candidates) == 0 {
		t.Error("expected at least one candidate from heuristic extraction")
	}

	// Classify should work
	classified, err := c.Classify(ctx, candidates)
	if err != nil {
		t.Fatalf("Classify failed: %v", err)
	}
	if len(classified) != len(candidates) {
		t.Errorf("expected %d classified, got %d", len(candidates), len(classified))
	}

	// Relate should work (returns nil for heuristic)
	edges, merges, err := c.Relate(ctx, classified, nil)
	if err != nil {
		t.Fatalf("Relate failed: %v", err)
	}
	if edges != nil {
		t.Error("expected nil edges from heuristic relate")
	}
	if merges != nil {
		t.Error("expected nil merges from heuristic relate")
	}

	// Promote with nil store should be a no-op
	err = c.Promote(ctx, classified, edges, merges, nil)
	if err != nil {
		t.Fatalf("Promote failed: %v", err)
	}
}

func TestDefaultLLMConsolidatorConfig(t *testing.T) {
	cfg := DefaultLLMConsolidatorConfig()

	if cfg.ChunkSize != 20 {
		t.Errorf("ChunkSize = %d, want 20", cfg.ChunkSize)
	}
	if cfg.MaxCandidates != 30 {
		t.Errorf("MaxCandidates = %d, want 30", cfg.MaxCandidates)
	}
	if cfg.TopK != 5 {
		t.Errorf("TopK = %d, want 5", cfg.TopK)
	}
	if !cfg.RetryOnce {
		t.Error("RetryOnce should be true by default")
	}
}
