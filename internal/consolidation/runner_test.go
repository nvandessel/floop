package consolidation

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/logging"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
	_ "modernc.org/sqlite"
)

// failOnSessionConsolidator wraps HeuristicConsolidator but fails Extract
// when it sees events from a specific session ID.
type failOnSessionConsolidator struct {
	HeuristicConsolidator
	failSession string
}

func (f *failOnSessionConsolidator) Extract(ctx context.Context, evts []events.Event) ([]Candidate, error) {
	if len(evts) > 0 && evts[0].SessionID == f.failSession {
		return nil, fmt.Errorf("simulated failure for session %s", f.failSession)
	}
	return f.HeuristicConsolidator.Extract(ctx, evts)
}

// stubConsolidator is a fully controllable test double for the Consolidator
// interface. Each stage can be configured via callbacks; nil callbacks use
// sensible defaults. It also implements ModelProvider and RunIDSetter.
type stubConsolidator struct {
	extractFn  func(ctx context.Context, evts []events.Event) ([]Candidate, error)
	classifyFn func(ctx context.Context, candidates []Candidate) ([]ClassifiedMemory, error)
	relateFn   func(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore) ([]store.Edge, []MergeProposal, []int, error)
	promoteFn  func(ctx context.Context, memories []ClassifiedMemory, edges []store.Edge, merges []MergeProposal, skips []int, s store.GraphStore) (PromoteResult, error)
	modelStr   string // returned by Model(); empty = "unknown"
	runID      string // set by SetRunID
}

func (s *stubConsolidator) Extract(ctx context.Context, evts []events.Event) ([]Candidate, error) {
	if s.extractFn != nil {
		return s.extractFn(ctx, evts)
	}
	// Default: one candidate per event
	var out []Candidate
	for _, evt := range evts {
		out = append(out, Candidate{
			SourceEvents:  []string{evt.ID},
			RawText:       evt.Content,
			CandidateType: "correction",
			Confidence:    0.8,
			SessionContext: map[string]any{
				"session_id": evt.SessionID,
				"project_id": evt.ProjectID,
			},
		})
	}
	return out, nil
}

func (s *stubConsolidator) Classify(_ context.Context, candidates []Candidate) ([]ClassifiedMemory, error) {
	if s.classifyFn != nil {
		return s.classifyFn(context.Background(), candidates)
	}
	var out []ClassifiedMemory
	for _, c := range candidates {
		out = append(out, ClassifiedMemory{
			Candidate:  c,
			Kind:       models.BehaviorKindPreference,
			MemoryType: models.MemoryTypeSemantic,
		})
	}
	return out, nil
}

func (s *stubConsolidator) Relate(_ context.Context, _ []ClassifiedMemory, _ store.GraphStore) ([]store.Edge, []MergeProposal, []int, error) {
	if s.relateFn != nil {
		return s.relateFn(context.Background(), nil, nil)
	}
	return nil, nil, nil, nil
}

func (s *stubConsolidator) Promote(_ context.Context, classified []ClassifiedMemory, _ []store.Edge, _ []MergeProposal, _ []int, _ store.GraphStore) (PromoteResult, error) {
	if s.promoteFn != nil {
		return s.promoteFn(context.Background(), classified, nil, nil, nil, nil)
	}
	return PromoteResult{Promoted: len(classified)}, nil
}

func (s *stubConsolidator) Model() string {
	if s.modelStr != "" {
		return s.modelStr
	}
	return ""
}

func (s *stubConsolidator) SetRunID(id string) { s.runID = id }

// newTestEventStore creates a real SQLite event store for integration tests.
func newTestEventStore(t *testing.T) *events.SQLiteEventStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	es := events.NewSQLiteEventStore(db)
	if err := es.InitSchema(context.Background()); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	return es
}

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

func TestGroupBySession(t *testing.T) {
	evts := []events.Event{
		{ID: "e1", SessionID: "sess-a"},
		{ID: "e2", SessionID: "sess-b"},
		{ID: "e3", SessionID: "sess-a"},
		{ID: "e4", SessionID: "sess-b"},
		{ID: "e5", SessionID: ""},
	}

	groups := groupBySession(evts)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	// First group should be sess-a (first seen)
	if len(groups[0]) != 2 || groups[0][0].ID != "e1" || groups[0][1].ID != "e3" {
		t.Errorf("group 0 (sess-a): got %v", groups[0])
	}

	// Second group should be sess-b
	if len(groups[1]) != 2 || groups[1][0].ID != "e2" || groups[1][1].ID != "e4" {
		t.Errorf("group 1 (sess-b): got %v", groups[1])
	}

	// Third group should be the empty-session event
	if len(groups[2]) != 1 || groups[2][0].ID != "e5" {
		t.Errorf("group 2 (empty): got %v", groups[2])
	}
}

func TestGroupBySession_SingleSession(t *testing.T) {
	evts := []events.Event{
		{ID: "e1", SessionID: "sess-a"},
		{ID: "e2", SessionID: "sess-a"},
	}

	groups := groupBySession(evts)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0]) != 2 {
		t.Errorf("expected 2 events in group, got %d", len(groups[0]))
	}
}

func TestRunner_MultiSession(t *testing.T) {
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
			SessionID: "sess-2",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "That's wrong, use context.WithTimeout instead.",
			ProjectID: "proj-1",
		},
	}

	result, err := runner.Run(ctx, evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// Both sessions should produce candidates independently
	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 candidates (one per session), got %d", len(result.Candidates))
	}

	if result.Promoted != 2 {
		t.Errorf("expected 2 promoted, got %d", result.Promoted)
	}

	// All events should be marked as source
	if len(result.SourceEventIDs) != 2 {
		t.Errorf("expected 2 source event IDs, got %d", len(result.SourceEventIDs))
	}
}

func TestGroupBySession_Empty(t *testing.T) {
	groups := groupBySession(nil)
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups for nil input, got %d", len(groups))
	}

	groups = groupBySession([]events.Event{})
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups for empty input, got %d", len(groups))
	}
}

func TestRunner_EmptyInput(t *testing.T) {
	h := NewHeuristicConsolidator()
	runner := NewRunner(h)

	result, err := runner.Run(context.Background(), nil, nil, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result.Candidates))
	}
	if len(result.SourceEventIDs) != 0 {
		t.Errorf("expected 0 source event IDs, got %d", len(result.SourceEventIDs))
	}
}

func TestRunner_MultiSession_MixedSignal(t *testing.T) {
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
			// sess-2 has no correction signal — should produce no candidates
			ID:        "evt-2",
			SessionID: "sess-2",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "Here is the code you requested.",
			ProjectID: "proj-1",
		},
		{
			ID:        "evt-3",
			SessionID: "sess-3",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "That's wrong, use context.WithTimeout instead.",
			ProjectID: "proj-2",
		},
	}

	result, err := runner.Run(ctx, evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// sess-1 and sess-3 should produce candidates; sess-2 should not
	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 candidates (sess-1 + sess-3), got %d", len(result.Candidates))
	}

	if result.Promoted != 2 {
		t.Errorf("expected 2 promoted, got %d", result.Promoted)
	}

	// All 3 events should be marked as source (even the no-signal one)
	if len(result.SourceEventIDs) != 3 {
		t.Errorf("expected 3 source event IDs, got %d", len(result.SourceEventIDs))
	}
}

func TestRunner_MultiSession_SessionContextPreserved(t *testing.T) {
	h := NewHeuristicConsolidator()
	runner := NewRunner(h)
	ctx := context.Background()

	evts := []events.Event{
		{
			ID:        "evt-1",
			SessionID: "sess-alpha",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "No, don't do that. Instead use fmt.Errorf to wrap errors.",
			ProjectID: "proj-A",
		},
		{
			ID:        "evt-2",
			SessionID: "sess-beta",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "That's wrong, use context.WithTimeout instead.",
			ProjectID: "proj-B",
		},
	}

	result, err := runner.Run(ctx, evts, nil, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(result.Classified) != 2 {
		t.Fatalf("expected 2 classified, got %d", len(result.Classified))
	}

	// Verify each classified memory has the correct session context
	for _, mem := range result.Classified {
		sid, _ := mem.SessionContext["session_id"].(string)
		pid, _ := mem.SessionContext["project_id"].(string)
		switch sid {
		case "sess-alpha":
			if pid != "proj-A" {
				t.Errorf("sess-alpha: expected project_id=proj-A, got %q", pid)
			}
		case "sess-beta":
			if pid != "proj-B" {
				t.Errorf("sess-beta: expected project_id=proj-B, got %q", pid)
			}
		default:
			t.Errorf("unexpected session_id %q in classified memory", sid)
		}
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

func TestRunner_MultiSession_PartialFailure(t *testing.T) {
	// Session 1 should succeed, session 2 should fail.
	// Verify that session 1's SourceEventIDs are preserved in the result.
	c := &failOnSessionConsolidator{failSession: "sess-fail"}
	runner := NewRunner(c)

	evts := []events.Event{
		{ID: "e1", SessionID: "sess-ok", Actor: "user", Kind: "correction", Content: "do X not Y"},
		{ID: "e2", SessionID: "sess-ok", Actor: "agent", Kind: "message", Content: "ok"},
		{ID: "e3", SessionID: "sess-fail", Actor: "user", Kind: "correction", Content: "fail here"},
	}

	result, err := runner.Run(context.Background(), evts, nil, RunOptions{DryRun: true})
	if err == nil {
		t.Fatal("expected error from failing session")
	}
	if result == nil {
		t.Fatal("expected non-nil result with prior session's data")
	}
	// Session 1's event IDs should be preserved
	if len(result.SourceEventIDs) != 2 {
		t.Errorf("expected 2 source event IDs from successful session, got %d", len(result.SourceEventIDs))
	}
	if result.RunID == "" {
		t.Error("expected RunID from successful first session")
	}
}

func TestRunner_EmptyInput_RunID(t *testing.T) {
	h := NewHeuristicConsolidator()
	runner := NewRunner(h)

	result, err := runner.Run(context.Background(), nil, nil, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// With no sessions, RunID should be empty (no runSession called)
	if result.RunID != "" {
		t.Errorf("expected empty RunID for no-session input, got %q", result.RunID)
	}
}

func TestRunner_ContextCancelMidSession(t *testing.T) {
	// Two sessions: after the first succeeds, cancel the context so the
	// second session is never started. Verify partial results are returned.
	callCount := 0
	stub := &stubConsolidator{
		extractFn: func(ctx context.Context, evts []events.Event) ([]Candidate, error) {
			callCount++
			var out []Candidate
			for _, evt := range evts {
				out = append(out, Candidate{
					SourceEvents:  []string{evt.ID},
					RawText:       evt.Content,
					CandidateType: "correction",
					Confidence:    0.8,
					SessionContext: map[string]any{
						"session_id": evt.SessionID,
					},
				})
			}
			return out, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first session's Promote completes
	stub.promoteFn = func(_ context.Context, classified []ClassifiedMemory, _ []store.Edge, _ []MergeProposal, _ []int, _ store.GraphStore) (PromoteResult, error) {
		cancel() // cancel before second session starts
		return PromoteResult{Promoted: len(classified)}, nil
	}

	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{
		{ID: "e1", SessionID: "sess-1", Actor: "user", Kind: "correction", Content: "fix A"},
		{ID: "e2", SessionID: "sess-2", Actor: "user", Kind: "correction", Content: "fix B"},
	}

	result, err := runner.Run(ctx, evts, s, RunOptions{})
	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
	if result == nil {
		t.Fatal("expected non-nil result with partial data")
	}
	// First session's events should be preserved
	if len(result.SourceEventIDs) != 1 {
		t.Errorf("expected 1 source event ID from first session, got %d", len(result.SourceEventIDs))
	}
	if result.Promoted != 1 {
		t.Errorf("expected 1 promoted from first session, got %d", result.Promoted)
	}
	if result.Duration < 0 {
		t.Error("expected non-negative Duration on context-cancel exit")
	}
}

func TestRunner_AllEventsSameSession(t *testing.T) {
	// Regression test: when all events share a session ID, behavior should
	// be identical to pre-per-session code — one pipeline invocation.
	extractCalls := 0
	stub := &stubConsolidator{
		extractFn: func(_ context.Context, evts []events.Event) ([]Candidate, error) {
			extractCalls++
			var out []Candidate
			for _, evt := range evts {
				out = append(out, Candidate{
					SourceEvents:   []string{evt.ID},
					RawText:        evt.Content,
					CandidateType:  "correction",
					Confidence:     0.8,
					SessionContext: map[string]any{"session_id": evt.SessionID},
				})
			}
			return out, nil
		},
	}

	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{
		{ID: "e1", SessionID: "sess-only", Actor: "user", Kind: "correction", Content: "fix A"},
		{ID: "e2", SessionID: "sess-only", Actor: "user", Kind: "correction", Content: "fix B"},
		{ID: "e3", SessionID: "sess-only", Actor: "user", Kind: "correction", Content: "fix C"},
	}

	result, err := runner.Run(context.Background(), evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if extractCalls != 1 {
		t.Errorf("expected exactly 1 Extract call (single session), got %d", extractCalls)
	}
	if len(result.Candidates) != 3 {
		t.Errorf("expected 3 candidates, got %d", len(result.Candidates))
	}
	if len(result.SourceEventIDs) != 3 {
		t.Errorf("expected 3 source event IDs, got %d", len(result.SourceEventIDs))
	}
}

func TestRunner_EmptySessionIDGrouped(t *testing.T) {
	// Events with empty SessionID should be grouped and processed together.
	extractCalls := 0
	stub := &stubConsolidator{
		extractFn: func(_ context.Context, evts []events.Event) ([]Candidate, error) {
			extractCalls++
			var out []Candidate
			for _, evt := range evts {
				out = append(out, Candidate{
					SourceEvents:  []string{evt.ID},
					RawText:       evt.Content,
					CandidateType: "correction",
					Confidence:    0.8,
				})
			}
			return out, nil
		},
	}
	runner := NewRunner(stub)

	evts := []events.Event{
		{ID: "e1", SessionID: "", Actor: "user", Kind: "correction", Content: "fix A"},
		{ID: "e2", SessionID: "", Actor: "user", Kind: "correction", Content: "fix B"},
	}

	result, err := runner.Run(context.Background(), evts, nil, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if extractCalls != 1 {
		t.Errorf("expected 1 Extract call (empty session IDs grouped), got %d", extractCalls)
	}
	if len(result.Candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(result.Candidates))
	}
}

func TestRunner_ManySessions(t *testing.T) {
	// 15 sessions with one event each — verify all are processed and
	// aggregated correctly.
	const numSessions = 15
	stub := &stubConsolidator{}
	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	var evts []events.Event
	for i := range numSessions {
		evts = append(evts, events.Event{
			ID:        fmt.Sprintf("e%d", i),
			SessionID: fmt.Sprintf("sess-%d", i),
			Actor:     "user",
			Kind:      "correction",
			Content:   fmt.Sprintf("fix issue %d", i),
		})
	}

	result, err := runner.Run(context.Background(), evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Candidates) != numSessions {
		t.Errorf("expected %d candidates, got %d", numSessions, len(result.Candidates))
	}
	if result.Promoted != numSessions {
		t.Errorf("expected %d promoted, got %d", numSessions, result.Promoted)
	}
	if len(result.SourceEventIDs) != numSessions {
		t.Errorf("expected %d source event IDs, got %d", numSessions, len(result.SourceEventIDs))
	}
	// RunID should come from the first session
	if result.RunID == "" {
		t.Error("expected non-empty RunID")
	}
}

func TestRunner_SessionOrderDeterministic(t *testing.T) {
	// Verify that sessions are processed in first-seen order by recording
	// the session IDs passed to Extract.
	var extractOrder []string
	stub := &stubConsolidator{
		extractFn: func(_ context.Context, evts []events.Event) ([]Candidate, error) {
			if len(evts) > 0 {
				extractOrder = append(extractOrder, evts[0].SessionID)
			}
			return nil, nil // no candidates — just track order
		},
	}
	runner := NewRunner(stub)

	evts := []events.Event{
		{ID: "e1", SessionID: "charlie"},
		{ID: "e2", SessionID: "alpha"},
		{ID: "e3", SessionID: "charlie"},
		{ID: "e4", SessionID: "bravo"},
		{ID: "e5", SessionID: "alpha"},
	}

	_, err := runner.Run(context.Background(), evts, nil, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	expected := []string{"charlie", "alpha", "bravo"}
	if len(extractOrder) != len(expected) {
		t.Fatalf("expected %d Extract calls, got %d", len(expected), len(extractOrder))
	}
	for i, want := range expected {
		if extractOrder[i] != want {
			t.Errorf("Extract call %d: expected session %q, got %q", i, want, extractOrder[i])
		}
	}
}

func TestRunner_SkipsAggregation(t *testing.T) {
	// Two sessions each return skips — verify they are aggregated.
	stub := &stubConsolidator{
		relateFn: func(_ context.Context, _ []ClassifiedMemory, _ store.GraphStore) ([]store.Edge, []MergeProposal, []int, error) {
			return nil, nil, []int{0}, nil // each session skips index 0
		},
		promoteFn: func(_ context.Context, _ []ClassifiedMemory, _ []store.Edge, _ []MergeProposal, _ []int, _ store.GraphStore) (PromoteResult, error) {
			return PromoteResult{Promoted: 0}, nil
		},
	}
	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{
		{ID: "e1", SessionID: "sess-1", Actor: "user", Kind: "correction", Content: "fix A"},
		{ID: "e2", SessionID: "sess-2", Actor: "user", Kind: "correction", Content: "fix B"},
	}

	result, err := runner.Run(context.Background(), evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// Each session returns []int{0}, so aggregated should be [0, 0]
	if len(result.Skips) != 2 {
		t.Errorf("expected 2 aggregated skips, got %d", len(result.Skips))
	}
}

func TestRunner_DurationSetOnSuccess(t *testing.T) {
	stub := &stubConsolidator{}
	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{
		{ID: "e1", SessionID: "s1", Actor: "user", Kind: "correction", Content: "fix"},
	}
	result, err := runner.Run(context.Background(), evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Duration < 0 {
		t.Error("expected non-negative Duration on success path")
	}
}

func TestRunner_DurationSetOnError(t *testing.T) {
	stub := &stubConsolidator{
		extractFn: func(_ context.Context, _ []events.Event) ([]Candidate, error) {
			return nil, fmt.Errorf("boom")
		},
	}
	runner := NewRunner(stub)

	evts := []events.Event{
		{ID: "e1", SessionID: "s1", Actor: "user", Kind: "correction", Content: "fix"},
	}
	result, err := runner.Run(context.Background(), evts, nil, RunOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if result == nil {
		t.Fatal("expected non-nil result on error")
	}
	if result.Duration < 0 {
		t.Error("expected non-negative Duration on error path")
	}
}

func TestRunner_ModelUnknownFallback(t *testing.T) {
	// A consolidator that does NOT implement ModelProvider should result
	// in model() returning "unknown".
	h := NewHeuristicConsolidator()
	runner := NewRunner(h)
	if m := runner.model(); m != "unknown" {
		t.Errorf("expected 'unknown' for non-ModelProvider consolidator, got %q", m)
	}
}

func TestRunner_ModelFromProvider(t *testing.T) {
	stub := &stubConsolidator{modelStr: "gpt-test-42"}
	runner := NewRunner(stub)
	if m := runner.model(); m != "gpt-test-42" {
		t.Errorf("expected 'gpt-test-42', got %q", m)
	}
}

func TestRunner_ModelProviderEmptyString(t *testing.T) {
	// ModelProvider that returns "" should fall back to "unknown".
	stub := &stubConsolidator{modelStr: ""}
	runner := NewRunner(stub)
	if m := runner.model(); m != "unknown" {
		t.Errorf("expected 'unknown' for empty model string, got %q", m)
	}
}

func TestRunner_ContextCancelAfterExtract(t *testing.T) {
	// Cancel context right after Extract succeeds — Classify should not run.
	ctx, cancel := context.WithCancel(context.Background())
	stub := &stubConsolidator{
		extractFn: func(_ context.Context, evts []events.Event) ([]Candidate, error) {
			cancel() // cancel after extract
			return []Candidate{{SourceEvents: []string{"e1"}, RawText: "fix", CandidateType: "correction", Confidence: 0.8}}, nil
		},
	}
	runner := NewRunner(stub)

	evts := []events.Event{{ID: "e1", SessionID: "s1", Actor: "user", Kind: "correction", Content: "fix"}}
	_, err := runner.Run(ctx, evts, nil, RunOptions{DryRun: true})
	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
}

func TestRunner_ClassifyError(t *testing.T) {
	stub := &stubConsolidator{
		classifyFn: func(_ context.Context, _ []Candidate) ([]ClassifiedMemory, error) {
			return nil, fmt.Errorf("classify boom")
		},
	}
	runner := NewRunner(stub)

	evts := []events.Event{{ID: "e1", SessionID: "s1", Actor: "user", Kind: "correction", Content: "fix"}}
	_, err := runner.Run(context.Background(), evts, nil, RunOptions{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "classify") {
		t.Fatalf("expected classify error, got %v", err)
	}
}

func TestRunner_ContextCancelAfterClassify(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := &stubConsolidator{
		classifyFn: func(_ context.Context, candidates []Candidate) ([]ClassifiedMemory, error) {
			cancel()
			var out []ClassifiedMemory
			for _, c := range candidates {
				out = append(out, ClassifiedMemory{Candidate: c, Kind: models.BehaviorKindPreference, MemoryType: models.MemoryTypeSemantic})
			}
			return out, nil
		},
	}
	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{{ID: "e1", SessionID: "s1", Actor: "user", Kind: "correction", Content: "fix"}}
	_, err := runner.Run(ctx, evts, s, RunOptions{})
	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
}

func TestRunner_RelateError(t *testing.T) {
	stub := &stubConsolidator{
		relateFn: func(_ context.Context, _ []ClassifiedMemory, _ store.GraphStore) ([]store.Edge, []MergeProposal, []int, error) {
			return nil, nil, nil, fmt.Errorf("relate boom")
		},
	}
	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{{ID: "e1", SessionID: "s1", Actor: "user", Kind: "correction", Content: "fix"}}
	_, err := runner.Run(context.Background(), evts, s, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "relate") {
		t.Fatalf("expected relate error, got %v", err)
	}
}

func TestRunner_ContextCancelAfterRelate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := &stubConsolidator{
		relateFn: func(_ context.Context, _ []ClassifiedMemory, _ store.GraphStore) ([]store.Edge, []MergeProposal, []int, error) {
			cancel()
			return nil, nil, nil, nil
		},
	}
	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{{ID: "e1", SessionID: "s1", Actor: "user", Kind: "correction", Content: "fix"}}
	_, err := runner.Run(ctx, evts, s, RunOptions{})
	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
}

// TestRunner_MarkConsolidatedOnPartialFailure simulates the caller pattern from
// cmd_consolidate.go and handler_consolidate.go: when runner.Run returns both a
// result and an error, MarkConsolidated should still be called with the
// successful session's event IDs before the error is propagated.
func TestRunner_MarkConsolidatedOnPartialFailure(t *testing.T) {
	c := &failOnSessionConsolidator{failSession: "sess-fail"}
	runner := NewRunner(c)

	evts := []events.Event{
		{ID: "e1", SessionID: "sess-ok", Actor: "user", Kind: "correction", Content: "fix A"},
		{ID: "e2", SessionID: "sess-ok", Actor: "user", Kind: "correction", Content: "fix B"},
		{ID: "e3", SessionID: "sess-fail", Actor: "user", Kind: "correction", Content: "fail here"},
	}

	result, runErr := runner.Run(context.Background(), evts, nil, RunOptions{DryRun: true})
	if runErr == nil {
		t.Fatal("expected error from failing session")
	}

	// Simulate the fixed caller pattern: mark consolidated BEFORE checking error.
	// This is what cmd_consolidate.go and handler_consolidate.go now do.
	var markedIDs []string
	if result != nil && len(result.SourceEventIDs) > 0 {
		markedIDs = result.SourceEventIDs
	}

	// Successful session's events should be available for marking
	if len(markedIDs) != 2 {
		t.Fatalf("expected 2 event IDs from successful session, got %d: %v", len(markedIDs), markedIDs)
	}

	// Verify the correct IDs are present
	idSet := map[string]bool{}
	for _, id := range markedIDs {
		idSet[id] = true
	}
	if !idSet["e1"] || !idSet["e2"] {
		t.Errorf("expected e1 and e2 in marked IDs, got %v", markedIDs)
	}
	// e3 (from failed session) should NOT be in the list
	if idSet["e3"] {
		t.Error("e3 from failed session should not be in marked IDs")
	}

	// The original error should still propagate
	if !strings.Contains(runErr.Error(), "sess-fail") {
		t.Errorf("expected error mentioning sess-fail, got: %v", runErr)
	}
}

// TestRunner_MarkConsolidatedOnPartialFailure_WithEventStore is an integration
// test that uses a real SQLite event store to verify the end-to-end flow:
// events from successful sessions get marked consolidated even when a later
// session fails.
func TestRunner_MarkConsolidatedOnPartialFailure_WithEventStore(t *testing.T) {
	// Set up a real event store
	es := newTestEventStore(t)

	evts := []events.Event{
		{ID: "e1", SessionID: "sess-ok", Actor: "user", Kind: "correction", Content: "fix A"},
		{ID: "e2", SessionID: "sess-ok", Actor: "user", Kind: "correction", Content: "fix B"},
		{ID: "e3", SessionID: "sess-fail", Actor: "user", Kind: "correction", Content: "fail here"},
	}

	ctx := context.Background()
	for _, evt := range evts {
		if err := es.Add(ctx, evt); err != nil {
			t.Fatalf("Add(%s): %v", evt.ID, err)
		}
	}

	// Run with a consolidator that fails on sess-fail
	c := &failOnSessionConsolidator{failSession: "sess-fail"}
	runner := NewRunner(c)
	result, runErr := runner.Run(ctx, evts, nil, RunOptions{DryRun: true})

	// Apply the fixed caller pattern
	if result != nil && len(result.SourceEventIDs) > 0 {
		if markErr := es.MarkConsolidated(ctx, result.SourceEventIDs); markErr != nil {
			t.Fatalf("MarkConsolidated: %v", markErr)
		}
	}

	// Verify the original error is still returned
	if runErr == nil {
		t.Fatal("expected error from failing session")
	}

	// Verify sess-ok events are marked consolidated
	unconsolidated, err := es.GetUnconsolidated(ctx)
	if err != nil {
		t.Fatalf("GetUnconsolidated: %v", err)
	}

	unconsolidatedIDs := map[string]bool{}
	for _, evt := range unconsolidated {
		unconsolidatedIDs[evt.ID] = true
	}

	// e1 and e2 should be consolidated (not in unconsolidated list)
	if unconsolidatedIDs["e1"] {
		t.Error("e1 should be consolidated but is still unconsolidated")
	}
	if unconsolidatedIDs["e2"] {
		t.Error("e2 should be consolidated but is still unconsolidated")
	}
	// e3 should still be unconsolidated
	if !unconsolidatedIDs["e3"] {
		t.Error("e3 should still be unconsolidated (from failed session)")
	}
}

func TestRunner_PromoteError(t *testing.T) {
	stub := &stubConsolidator{
		promoteFn: func(_ context.Context, _ []ClassifiedMemory, _ []store.Edge, _ []MergeProposal, _ []int, _ store.GraphStore) (PromoteResult, error) {
			return PromoteResult{}, fmt.Errorf("promote boom")
		},
	}
	runner := NewRunner(stub)
	s := store.NewInMemoryGraphStore()

	evts := []events.Event{{ID: "e1", SessionID: "s1", Actor: "user", Kind: "correction", Content: "fix"}}
	_, err := runner.Run(context.Background(), evts, s, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "promote") {
		t.Fatalf("expected promote error, got %v", err)
	}
}
