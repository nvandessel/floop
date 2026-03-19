package consolidation

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/logging"
	"github.com/nvandessel/floop/internal/store"
)

func TestRunner_DryRun(t *testing.T) {
	h := NewHeuristicConsolidator()
	runner := NewRunner(h)
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

	result, err := runner.Run(ctx, evts, nil, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(result.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result.Candidates))
	}

	if result.Candidates[0].CandidateType != "correction" {
		t.Errorf("expected correction candidate, got %q", result.Candidates[0].CandidateType)
	}

	if len(result.Classified) != 1 {
		t.Fatalf("expected 1 classified memory, got %d", len(result.Classified))
	}

	if result.Promoted != 0 {
		t.Errorf("expected 0 promoted in dry-run, got %d", result.Promoted)
	}

	if result.Duration < 0 {
		t.Error("expected non-negative duration")
	}
}

func TestRunner_NoSignal(t *testing.T) {
	h := NewHeuristicConsolidator()
	runner := NewRunner(h)
	ctx := context.Background()

	evts := []events.Event{
		{
			ID:      "evt-1",
			Actor:   events.ActorUser,
			Kind:    events.KindMessage,
			Content: "Here is the code you requested.",
		},
	}

	result, err := runner.Run(ctx, evts, nil, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(result.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result.Candidates))
	}
	if len(result.Classified) != 0 {
		t.Errorf("expected 0 classified, got %d", len(result.Classified))
	}
}

func TestRunner_FullPipeline(t *testing.T) {
	h := NewHeuristicConsolidator()
	runner := NewRunner(h)
	ctx := context.Background()
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{
		{
			ID:        "evt-1",
			SessionID: "sess-1",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "No, don't do that. Instead use fmt.Errorf to wrap errors.",
			ProjectID: "proj-1",
		},
		{
			ID:        "evt-2",
			SessionID: "sess-1",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "That didn't work because the import path was wrong.",
			ProjectID: "proj-1",
		},
	}

	result, err := runner.Run(ctx, evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result.Candidates))
	}

	if len(result.Classified) != 2 {
		t.Fatalf("expected 2 classified, got %d", len(result.Classified))
	}

	if result.Promoted != 2 {
		t.Errorf("expected 2 promoted, got %d", result.Promoted)
	}

	// Verify nodes were created in the store
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{
		"kind": "behavior",
	})
	if err != nil {
		t.Fatalf("QueryNodes error: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes in store, got %d", len(nodes))
	}
}

func TestRunner_RunIDThreadedToDecisionLog(t *testing.T) {
	dir := t.TempDir()
	dl := logging.NewDecisionLogger(dir, "debug")
	defer dl.Close()

	cfg := DefaultLLMConsolidatorConfig()
	cfg.Model = "test-model-abc"
	// Use a mock client that returns empty JSON — Extract will fall back to
	// heuristic per chunk, but decision log entries are still emitted.
	mock := &mockLLMClient{responses: []string{"{}", "{}", "{}"}}
	c := NewLLMConsolidator(mock, dl, cfg)
	runner := NewRunner(c)

	evts := []events.Event{
		{
			ID:      "evt-1",
			Actor:   events.ActorUser,
			Kind:    events.KindMessage,
			Content: "no, don't use pip, use uv instead for package management",
		},
	}

	_, err := runner.Run(context.Background(), evts, nil, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	dl.Close()

	// Read the JSONL and verify every entry has run_id and model
	path := filepath.Join(dir, "decisions.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open decisions.jsonl: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		lines++
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("line %d: bad JSON: %v", lines, err)
		}
		runID, _ := entry["run_id"].(string)
		if !strings.HasPrefix(runID, "run-") {
			t.Errorf("line %d: expected run_id starting with 'run-', got %q", lines, runID)
		}
		model, _ := entry["model"].(string)
		if model != "test-model-abc" {
			t.Errorf("line %d: expected model 'test-model-abc', got %q", lines, model)
		}
	}
	if lines == 0 {
		t.Fatal("expected at least one decision log entry")
	}
}
