# v1 Consolidation Integration Tests — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add end-to-end integration tests that validate the v1 LLM consolidation pipeline (Extract → Classify → Relate → Promote) produces correct graph state with scripted mock LLM responses.

**Architecture:** 8 test scenarios in a single test file exercising the `Runner.Run` pipeline with a `mockLLMClient` that replays canned JSON responses. Each scenario gets an isolated SQLite store via `t.TempDir()`. Tests validate node/edge state post-promotion, fallback behavior, and pipeline plumbing.

**Tech Stack:** Go 1.25+, `testing` stdlib, `store.NewSQLiteGraphStore`, existing `mockLLMClient` pattern

**Spec:** `docs/superpowers/specs/2026-03-18-v1-consolidation-integration-tests-design.md`

---

## Prerequisites

The PR stack (#226-#230) must be merged before this work begins. The integration test imports code from all four stage branches (extract, classify, relate, integration). After merge:

1. Verify `go build ./internal/consolidation/...` compiles cleanly
2. Verify `go test ./internal/consolidation/...` passes (existing unit tests)
3. If `mockLLMClient` exists in both `test_helpers_test.go` AND `extract_test.go`, remove the duplicate from `extract_test.go` (keep the shared one in `test_helpers_test.go`)

## File Structure

| File | Responsibility |
|---|---|
| `internal/consolidation/integration_test.go` | **Create.** All 8 integration test scenarios |
| `internal/consolidation/integration_testdata_test.go` | **Create.** Scripted mock JSON response constants |
| `internal/consolidation/test_helpers_test.go` | **Modify.** Add `makeEventsWithSignals`, `newTestStore`, assertion helpers |

The `test_helpers_test.go` file already exists in `feat/v1/integration` with `mockLLMClient`, `newTestLLMConsolidator`, and `makeCandidates`. We extend it with integration-test-specific helpers.

---

### Task 1: Extend Test Helpers

**Files:**
- Modify: `internal/consolidation/test_helpers_test.go`

- [ ] **Step 1: Write failing test for `makeEventsWithSignals`**

```go
// In integration_test.go (create minimal file to test the helper)
func TestMakeEventsWithSignals(t *testing.T) {
	evts := makeEventsWithSignals(40,
		withCorrection(5, "No don't mock the database, we got burned last quarter"),
		withPreference(15, "I prefer tabs over spaces"),
		withSessionID("sess-test"),
		withProjectID("proj-test"),
	)
	if len(evts) != 40 {
		t.Fatalf("expected 40 events, got %d", len(evts))
	}
	if evts[4].Content != "No don't mock the database, we got burned last quarter" {
		t.Fatalf("correction not injected at index 4 (0-based), got %q", evts[4].Content)
	}
	if evts[4].Kind != events.KindCorrection {
		t.Fatalf("expected correction kind at index 4, got %s", evts[4].Kind)
	}
	if evts[14].Content != "I prefer tabs over spaces" {
		t.Fatalf("preference not injected at index 14")
	}
	if evts[0].SessionID != "sess-test" {
		t.Fatalf("session ID not set")
	}
	if evts[0].ProjectID != "proj-test" {
		t.Fatalf("project ID not set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/consolidation/ -run TestMakeEventsWithSignals -v`
Expected: FAIL — `makeEventsWithSignals` undefined

- [ ] **Step 3: Implement event factory in test_helpers_test.go**

```go
type eventOpt func([]events.Event)

func withCorrection(idx int, text string) eventOpt {
	return func(evts []events.Event) {
		if idx > 0 && idx <= len(evts) {
			evts[idx-1].Content = text
			evts[idx-1].Kind = events.KindCorrection
			evts[idx-1].Actor = events.ActorUser
		}
	}
}

func withPreference(idx int, text string) eventOpt {
	return func(evts []events.Event) {
		if idx > 0 && idx <= len(evts) {
			evts[idx-1].Content = text
			evts[idx-1].Kind = events.KindMessage
			evts[idx-1].Actor = events.ActorUser
		}
	}
}

func withSessionID(id string) eventOpt {
	return func(evts []events.Event) {
		for i := range evts {
			evts[i].SessionID = id
		}
	}
}

func withProjectID(id string) eventOpt {
	return func(evts []events.Event) {
		for i := range evts {
			evts[i].ProjectID = id
		}
	}
}

// makeEventsWithSignals creates n events with optional injected signals.
// Uses 1-based indexing for signal injection (idx=5 means the 5th event).
func makeEventsWithSignals(n int, opts ...eventOpt) []events.Event {
	evts := make([]events.Event, n)
	for i := range n {
		actor := events.ActorUser
		if i%2 == 1 {
			actor = events.ActorAgent
		}
		evts[i] = events.Event{
			ID:        fmt.Sprintf("evt-%d", i+1),
			SessionID: "sess-1",
			ProjectID: "proj-1",
			Actor:     actor,
			Kind:      events.KindMessage,
			Content:   fmt.Sprintf("Test message %d with enough content to pass filters", i+1),
		}
	}
	for _, opt := range opts {
		opt(evts)
	}
	return evts
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/consolidation/ -run TestMakeEventsWithSignals -v`
Expected: PASS

- [ ] **Step 5: Add store and assertion helpers**

```go
func newTestStore(t *testing.T) store.GraphStore {
	t.Helper()
	s, err := store.NewSQLiteGraphStore(t.TempDir())
	if err != nil {
		t.Fatalf("newTestStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func queryBehaviorNodes(t *testing.T, s store.GraphStore) []store.Node {
	t.Helper()
	nodes, err := s.QueryNodes(context.Background(), map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("queryBehaviorNodes: %v", err)
	}
	return nodes
}

func assertNodeCount(t *testing.T, s store.GraphStore, kind string, expected int) {
	t.Helper()
	nodes, err := s.QueryNodes(context.Background(), map[string]interface{}{"kind": kind})
	if err != nil {
		t.Fatalf("assertNodeCount: %v", err)
	}
	if len(nodes) != expected {
		t.Errorf("expected %d nodes of kind %q, got %d", expected, kind, len(nodes))
	}
}

func assertEdgeExists(t *testing.T, s store.GraphStore, source string, kind store.EdgeKind) {
	t.Helper()
	edges, err := s.GetEdges(context.Background(), source, store.DirectionOutbound, kind)
	if err != nil {
		t.Fatalf("assertEdgeExists: %v", err)
	}
	if len(edges) == 0 {
		t.Errorf("expected at least one %q edge from %q, found none", kind, source)
	}
}
```

- [ ] **Step 6: Run all existing tests to verify no regressions**

Run: `go test ./internal/consolidation/ -v -count=1`
Expected: All existing tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/consolidation/test_helpers_test.go internal/consolidation/integration_test.go
git commit -m "test: add integration test helpers for v1 consolidation pipeline"
```

---

### Task 2: Scripted Mock Response Constants

**Files:**
- Create: `internal/consolidation/integration_testdata_test.go`

- [ ] **Step 1: Create the test data file with happy path responses**

This file contains `const` blocks for each LLM call in the happy path scenario. The JSON must match the schemas in `extract_prompts.go`, `classify_prompts.go`, and `relate_prompts.go`.

```go
package consolidation

// --- Happy Path: 2 chunks of 20 events, 4 candidates ---

const happyPathSummarizeChunk1 = `{
	"summary": "User corrected agent on database mocking approach and discussed test strategy",
	"tone": "frustrated",
	"phase": "stuck",
	"pattern": "debugging",
	"key_moments": [
		{"event_id": "evt-5", "type": "correction", "brief": "User rejected DB mocking"},
		{"event_id": "evt-10", "type": "decision", "brief": "Decided on integration tests"}
	],
	"open_threads": ["test isolation strategy unresolved"]
}`

const happyPathSummarizeChunk2 = `{
	"summary": "User expressed preference for direct error handling and corrected agent's abstraction",
	"tone": "neutral",
	"phase": "building",
	"pattern": "collaborating",
	"key_moments": [
		{"event_id": "evt-25", "type": "correction", "brief": "No wrapper functions"},
		{"event_id": "evt-30", "type": "preference", "brief": "Prefers explicit error returns"}
	],
	"open_threads": []
}`

const happyPathArcSynthesis = `{
	"arc": "User started frustrated with test mocking approach, corrected agent multiple times, then settled into collaborative building with clear preferences for directness",
	"dominant_tone": "frustrated->neutral",
	"session_outcome": "resolved",
	"themes": ["testing", "error-handling", "code-style"],
	"behavioral_signals": [
		"User strongly prefers integration tests over mocked tests",
		"User wants explicit error handling, no abstractions"
	]
}`

const happyPathExtractChunk1 = `{
	"candidates": [
		{
			"source_events": ["evt-5", "evt-6"],
			"raw_text": "No don't mock the database, we got burned last quarter when mocked tests passed but the prod migration failed",
			"candidate_type": "correction",
			"confidence": 0.92,
			"sentiment": "frustrated",
			"session_phase": "stuck",
			"interaction_pattern": "teaching",
			"rationale": "Explicit correction with historical context about prior incident",
			"already_captured": false
		},
		{
			"source_events": ["evt-10"],
			"raw_text": "Let's use integration tests that hit a real database instead",
			"candidate_type": "decision",
			"confidence": 0.78,
			"sentiment": "neutral",
			"session_phase": "resolving",
			"interaction_pattern": "collaborating",
			"rationale": "Clear decision following correction",
			"already_captured": false
		}
	]
}`

const happyPathExtractChunk2 = `{
	"candidates": [
		{
			"source_events": ["evt-25"],
			"raw_text": "Don't wrap errors in helper functions, just return them directly with context",
			"candidate_type": "correction",
			"confidence": 0.85,
			"sentiment": "neutral",
			"session_phase": "building",
			"interaction_pattern": "teaching",
			"rationale": "Style correction on error handling approach",
			"already_captured": false
		},
		{
			"source_events": ["evt-30"],
			"raw_text": "I prefer explicit error returns over panic-based patterns",
			"candidate_type": "preference",
			"confidence": 0.70,
			"sentiment": "neutral",
			"session_phase": "building",
			"interaction_pattern": "collaborating",
			"rationale": "Stated preference for error handling style",
			"already_captured": false
		}
	]
}`

const happyPathClassifyBatch = `{
	"classified": [
		{
			"index": 0,
			"source_events": ["evt-5", "evt-6"],
			"kind": "directive",
			"memory_type": "semantic",
			"scope": "universal",
			"importance": 0.90,
			"content": {
				"canonical": "Never mock the database in integration tests — use a real database to catch migration and schema issues",
				"summary": "No DB mocks in integration tests",
				"tags": ["testing", "database", "integration"]
			},
			"episode_data": null,
			"workflow_data": null
		},
		{
			"index": 1,
			"source_events": ["evt-10"],
			"kind": "preference",
			"memory_type": "semantic",
			"scope": "universal",
			"importance": 0.65,
			"content": {
				"canonical": "Prefer integration tests with real database connections over mocked database layers",
				"summary": "Real DB in integration tests",
				"tags": ["testing", "database"]
			},
			"episode_data": null,
			"workflow_data": null
		},
		{
			"index": 2,
			"source_events": ["evt-25"],
			"kind": "directive",
			"memory_type": "semantic",
			"scope": "universal",
			"importance": 0.80,
			"content": {
				"canonical": "Return errors directly with fmt.Errorf context wrapping — do not create error helper functions",
				"summary": "Direct error returns, no helpers",
				"tags": ["error-handling", "go", "style"]
			},
			"episode_data": null,
			"workflow_data": null
		},
		{
			"index": 3,
			"source_events": ["evt-30"],
			"kind": "preference",
			"memory_type": "semantic",
			"scope": "universal",
			"importance": 0.60,
			"content": {
				"canonical": "Prefer explicit error returns over panic-recover patterns in application code",
				"summary": "Errors over panics",
				"tags": ["error-handling", "go"]
			},
			"episode_data": null,
			"workflow_data": null
		}
	]
}`

// happyPathRelateBatch: index 0 = create with edge, index 1 = merge (absorb),
// index 2 = create, index 3 = skip (already captured).
// Pre-seed store with "bhv-existing-1" for the merge target.
const happyPathRelateBatch = `{
	"relationships": [
		{
			"memory_index": 0,
			"action": "create",
			"edges": [{"target": "bhv-existing-1", "kind": "similar-to", "weight": 0.72}],
			"merge_into": null,
			"rationale": "Related to existing DB testing behavior but distinct (no-mock vs real-DB)"
		},
		{
			"memory_index": 1,
			"action": "merge",
			"edges": [],
			"merge_into": {
				"target_id": "bhv-existing-1",
				"strategy": "absorb",
				"merged_content": {
					"canonical": "Use real database connections in integration tests — never mock the database layer",
					"summary": "Real DB, no mocks in tests",
					"tags": ["testing", "database", "integration"]
				}
			},
			"rationale": "Near-duplicate of existing behavior, adds integration test context"
		},
		{
			"memory_index": 2,
			"action": "create",
			"edges": [],
			"merge_into": null,
			"rationale": "New behavioral directive about error handling style"
		},
		{
			"memory_index": 3,
			"action": "skip",
			"edges": [],
			"merge_into": null,
			"rationale": "Already captured as preference in existing behavior set"
		}
	]
}`

// --- Empty session: no candidates extracted ---

const emptyExtractChunk1 = `{"candidates": []}`

// --- Incremental run 2: 3 candidates, 2 skipped ---

const incrementalRun2RelateBatch = `{
	"relationships": [
		{
			"memory_index": 0,
			"action": "skip",
			"edges": [],
			"merge_into": null,
			"rationale": "Already captured"
		},
		{
			"memory_index": 1,
			"action": "skip",
			"edges": [],
			"merge_into": null,
			"rationale": "Already captured"
		},
		{
			"memory_index": 2,
			"action": "create",
			"edges": [],
			"merge_into": null,
			"rationale": "New behavioral signal"
		}
	]
}`
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/consolidation/`
Expected: Success (constants are just strings, no logic)

- [ ] **Step 3: Commit**

```bash
git add internal/consolidation/integration_testdata_test.go
git commit -m "test: add scripted mock response constants for integration tests"
```

---

### Task 3: Happy Path Integration Test (Scenario 1)

**Files:**
- Create: `internal/consolidation/integration_test.go`

- [ ] **Step 1: Write the happy path test**

```go
package consolidation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/store"
)

func TestIntegration_HappyPath(t *testing.T) {
	// Setup: pre-seed store with an existing behavior (merge target)
	s := newTestStore(t)
	ctx := context.Background()

	existingNode := store.Node{
		ID:   "bhv-existing-1",
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"canonical": "Use real databases in tests",
			"summary":   "Real DB in tests",
			"tags":      []interface{}{"testing", "database"},
		},
		Metadata: map[string]interface{}{
			"source_type": "consolidated",
			"confidence":  0.7,
		},
	}
	if _, err := s.AddNode(ctx, existingNode); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	// Mock LLM: 7 calls in order
	mock := &mockLLMClient{
		responses: []string{
			happyPathSummarizeChunk1, // Pass 1, chunk 1
			happyPathSummarizeChunk2, // Pass 1, chunk 2
			happyPathArcSynthesis,    // Pass 2
			happyPathExtractChunk1,   // Pass 3, chunk 1
			happyPathExtractChunk2,   // Pass 3, chunk 2
			happyPathClassifyBatch,   // Classify
			happyPathRelateBatch,     // Relate
		},
	}

	// 40 events = 2 chunks of 20
	evts := makeEventsWithSignals(40,
		withCorrection(5, "No don't mock the database, we got burned last quarter"),
		withCorrection(10, "Let's use integration tests that hit a real database"),
		withCorrection(25, "Don't wrap errors in helper functions, just return them directly"),
		withPreference(30, "I prefer explicit error returns over panic-based patterns"),
		withSessionID("sess-happy"),
		withProjectID("proj-happy"),
	)

	// Run pipeline
	consolidator := NewLLMConsolidator(mock, nil, DefaultLLMConsolidatorConfig())
	runner := NewRunner(consolidator)
	result, err := runner.Run(ctx, evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Assert: 4 candidates extracted
	if len(result.Candidates) != 4 {
		t.Errorf("expected 4 candidates, got %d", len(result.Candidates))
	}

	// Assert: 4 classified
	if len(result.Classified) != 4 {
		t.Errorf("expected 4 classified, got %d", len(result.Classified))
	}

	// Assert: 1 merge, 1 skip
	if len(result.Merges) != 1 {
		t.Errorf("expected 1 merge, got %d", len(result.Merges))
	}
	if len(result.Skips) != 1 {
		t.Errorf("expected 1 skip, got %d", len(result.Skips))
	}

	// Assert: promoted = 4 - 1 merge - 1 skip = 2
	if result.Promoted != 2 {
		t.Errorf("expected 2 promoted, got %d", result.Promoted)
	}

	// Assert: 2 new behavior nodes + 1 existing = 3 total behaviors
	// (the existing node may have been modified by absorb but is still kind=behavior)
	behaviors := queryBehaviorNodes(t, s)
	if len(behaviors) != 3 {
		t.Errorf("expected 3 behavior nodes in store, got %d", len(behaviors))
	}

	// Assert: the merge target was updated (absorb).
	// Absorb uses merge.Memory.Content.Canonical from the Classify output
	// (index 1 in happyPathClassifyBatch), not merged_content from Relate.
	mergedNode, err := s.GetNode(ctx, "bhv-existing-1")
	if err != nil {
		t.Fatalf("get merged node: %v", err)
	}
	// After absorb, the canonical should reflect the classified memory's content.
	// The exact text depends on executeAbsorb's merge logic (prefers longer canonical).
	// Just verify it was modified from the original.
	if mergedNode.Content["canonical"] == "Use real databases in tests" {
		t.Error("absorb merge did not update the existing node's canonical text")
	}

	// Assert: mock received exactly 7 calls
	if mock.callIndex != 7 {
		t.Errorf("expected 7 LLM calls, got %d", mock.callIndex)
	}

	// Assert: source events captured
	if len(result.SourceEventIDs) != 40 {
		t.Errorf("expected 40 source event IDs, got %d", len(result.SourceEventIDs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails (or passes if plumbing is correct)**

Run: `go test ./internal/consolidation/ -run TestIntegration_HappyPath -v -count=1`
Expected: Either PASS (plumbing works) or specific failure pointing to a real issue

- [ ] **Step 3: Debug and fix any failures**

The test should pass if the pipeline plumbing is correct. If it fails, the failure tells us where the plumbing is broken — that's the point of the test. Fix the test assertions to match actual behavior, or fix the code if the behavior is wrong.

- [ ] **Step 4: Commit**

```bash
git add internal/consolidation/integration_test.go
git commit -m "test: add happy path integration test for v1 consolidation pipeline"
```

---

### Task 4: Incremental Consolidation Test (Scenario 2)

**Files:**
- Modify: `internal/consolidation/integration_test.go`

- [ ] **Step 1: Write the incremental consolidation test**

```go
func TestIntegration_IncrementalNoDuplicates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// --- Run 1: 20 events, 2 candidates → 2 behaviors ---
	// Classify response must match candidate count (2, not 4)
	classifyRun1 := `{
		"classified": [
			{"index": 0, "source_events": ["evt-5", "evt-6"], "kind": "directive", "memory_type": "semantic", "scope": "universal", "importance": 0.90, "content": {"canonical": "Never mock the database in integration tests", "summary": "No DB mocks in integration tests", "tags": ["testing", "database", "integration"]}, "episode_data": null, "workflow_data": null},
			{"index": 1, "source_events": ["evt-10"], "kind": "preference", "memory_type": "semantic", "scope": "universal", "importance": 0.65, "content": {"canonical": "Prefer integration tests with real database connections", "summary": "Real DB in integration tests", "tags": ["testing", "database"]}, "episode_data": null, "workflow_data": null}
		]
	}`
	mock1 := &mockLLMClient{
		responses: []string{
			happyPathSummarizeChunk1,  // Summarize
			happyPathArcSynthesis,     // Arc
			happyPathExtractChunk1,    // Extract (returns 2 candidates)
			classifyRun1,              // Classify (2 entries matching 2 candidates)
			`{"relationships": [
				{"memory_index": 0, "action": "create", "edges": [], "merge_into": null, "rationale": "New"},
				{"memory_index": 1, "action": "create", "edges": [], "merge_into": null, "rationale": "New"}
			]}`, // Relate: both create
		},
	}

	evts1 := makeEventsWithSignals(20,
		withCorrection(5, "No don't mock the database"),
		withCorrection(10, "Use integration tests instead"),
		withSessionID("sess-inc"),
	)

	c1 := NewLLMConsolidator(mock1, nil, DefaultLLMConsolidatorConfig())
	r1 := NewRunner(c1)
	res1, err := r1.Run(ctx, evts1, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	if res1.Promoted != 2 {
		t.Fatalf("Run 1: expected 2 promoted, got %d", res1.Promoted)
	}

	behaviorsAfterRun1 := queryBehaviorNodes(t, s)
	if len(behaviorsAfterRun1) != 2 {
		t.Fatalf("Run 1: expected 2 behaviors, got %d", len(behaviorsAfterRun1))
	}

	// --- Run 2: 30 events, 3 candidates, but 2 are skipped by Relate ---
	mock2 := &mockLLMClient{
		responses: []string{
			happyPathSummarizeChunk1,         // Summarize chunk 1
			happyPathSummarizeChunk2,         // Summarize chunk 2
			happyPathArcSynthesis,            // Arc
			happyPathExtractChunk1,           // Extract chunk 1 (2 candidates)
			happyPathExtractChunk2,           // Extract chunk 2 (2 candidates, but we only need 1 new)
			happyPathClassifyBatch,           // Classify all
			incrementalRun2RelateBatch,       // Relate: skip 0,1; create 2
		},
	}

	evts2 := makeEventsWithSignals(30,
		withCorrection(5, "No don't mock the database"),
		withCorrection(10, "Use integration tests instead"),
		withCorrection(25, "Don't wrap errors in helpers"),
		withSessionID("sess-inc"),
	)

	c2 := NewLLMConsolidator(mock2, nil, DefaultLLMConsolidatorConfig())
	r2 := NewRunner(c2)
	res2, err := r2.Run(ctx, evts2, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}

	// Assert: 2 skips in run 2
	if len(res2.Skips) != 2 {
		t.Errorf("Run 2: expected 2 skips, got %d", len(res2.Skips))
	}

	// Assert: 3 total behaviors (not 4 or 5)
	behaviorsAfterRun2 := queryBehaviorNodes(t, s)
	if len(behaviorsAfterRun2) != 3 {
		t.Errorf("Run 2: expected 3 total behaviors, got %d", len(behaviorsAfterRun2))
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/consolidation/ -run TestIntegration_IncrementalNoDuplicates -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/consolidation/integration_test.go
git commit -m "test: add incremental consolidation dedup integration test"
```

---

### Task 5: Full Fallback Test (Scenario 3)

**Files:**
- Modify: `internal/consolidation/integration_test.go`

- [ ] **Step 1: Write the full fallback test**

```go
func TestIntegration_FullFallback_LLMErrors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Every Complete() call returns an error
	llmErr := fmt.Errorf("LLM unavailable")
	mock := &mockLLMClient{
		responses: nil,
		errors: []error{
			llmErr, llmErr, llmErr, llmErr, llmErr,
			llmErr, llmErr, llmErr, llmErr, llmErr,
		},
	}

	// Events with correction signals detectable by v0 heuristic
	evts := makeEventsWithSignals(20,
		withCorrection(5, "no, don't do that — use real database connections instead of mocks"),
		withCorrection(10, "instead of wrapping, just return the error directly"),
		withSessionID("sess-fallback"),
	)

	consolidator := NewLLMConsolidator(mock, nil, DefaultLLMConsolidatorConfig())
	runner := NewRunner(consolidator)
	result, err := runner.Run(ctx, evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run should not error on LLM failure (fallback): %v", err)
	}

	// Assert: LLM was attempted (Extract tried Complete())
	if mock.callIndex == 0 {
		t.Error("expected at least one LLM call attempt")
	}

	// Assert: heuristic fallback produced behaviors
	behaviors := queryBehaviorNodes(t, s)
	if len(behaviors) == 0 {
		t.Error("expected heuristic fallback to create at least one behavior")
	}

	// Assert: promoted count > 0
	if result.Promoted <= 0 {
		t.Error("expected at least one promoted behavior from heuristic fallback")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/consolidation/ -run TestIntegration_FullFallback -v -count=1`
Expected: PASS — heuristic fallback handles all stages

- [ ] **Step 3: Commit**

```bash
git add internal/consolidation/integration_test.go
git commit -m "test: add full fallback integration test (LLM errors → heuristic)"
```

---

### Task 6: Per-Stage Fallback Test (Scenario 4)

**Files:**
- Modify: `internal/consolidation/integration_test.go`

- [ ] **Step 1: Write the per-stage fallback test**

```go
func TestIntegration_PerStageFallback_ClassifyFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Extract succeeds (5 calls), Classify fails, Relate succeeds
	mock := &mockLLMClient{
		responses: []string{
			happyPathSummarizeChunk1, // Summarize chunk 1
			happyPathSummarizeChunk2, // Summarize chunk 2
			happyPathArcSynthesis,    // Arc synthesis
			happyPathExtractChunk1,   // Extract chunk 1
			happyPathExtractChunk2,   // Extract chunk 2
			"",                       // Classify — will error (see errs below)
			`{"relationships": [
				{"memory_index": 0, "action": "create", "edges": [], "merge_into": null, "rationale": "New"},
				{"memory_index": 1, "action": "create", "edges": [], "merge_into": null, "rationale": "New"},
				{"memory_index": 2, "action": "create", "edges": [], "merge_into": null, "rationale": "New"},
				{"memory_index": 3, "action": "create", "edges": [], "merge_into": null, "rationale": "New"}
			]}`, // Relate
		},
		errors: []error{
			nil, nil, nil, nil, nil,          // Extract calls succeed
			fmt.Errorf("classify LLM error"), // Classify fails
			nil,                              // Relate succeeds
		},
	}

	evts := makeEventsWithSignals(40,
		withCorrection(5, "no, don't mock the database"),
		withCorrection(25, "don't wrap errors in helpers"),
		withSessionID("sess-perstage"),
	)

	consolidator := NewLLMConsolidator(mock, nil, DefaultLLMConsolidatorConfig())
	runner := NewRunner(consolidator)
	result, err := runner.Run(ctx, evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run should succeed with per-stage fallback: %v", err)
	}

	// Assert: Extract used LLM (5 calls for 2 chunks)
	// Classify fell back to heuristic, but Relate still used LLM
	if mock.callIndex < 6 {
		t.Errorf("expected at least 6 LLM calls (5 extract + 1 failed classify), got %d", mock.callIndex)
	}

	// Assert: candidates were classified (via heuristic fallback)
	if len(result.Classified) == 0 {
		t.Error("expected classified memories from heuristic fallback")
	}

	// Assert: behaviors created in store
	behaviors := queryBehaviorNodes(t, s)
	if len(behaviors) == 0 {
		t.Error("expected behaviors in store after per-stage fallback")
	}
}
```

- [ ] **Step 2: Run test**

Run: `go test ./internal/consolidation/ -run TestIntegration_PerStageFallback -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/consolidation/integration_test.go
git commit -m "test: add per-stage fallback integration test (classify fails)"
```

---

### Task 7: Empty Session + Executor Config Gate (Scenarios 5 & 6)

**Files:**
- Modify: `internal/consolidation/integration_test.go`

- [ ] **Step 1: Write the empty session test**

```go
func TestIntegration_EmptySession_EarlyExit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mock := &mockLLMClient{
		responses: []string{
			`{"summary":"General Q&A","tone":"neutral","phase":"exploring","pattern":"collaborating","key_moments":[],"open_threads":[]}`,
			`{"arc":"Simple Q&A session","dominant_tone":"neutral","session_outcome":"resolved","themes":["general"],"behavioral_signals":[]}`,
			emptyExtractChunk1, // Extract returns no candidates
		},
	}

	evts := makeEventsWithSignals(20, withSessionID("sess-empty"))

	consolidator := NewLLMConsolidator(mock, nil, DefaultLLMConsolidatorConfig())
	runner := NewRunner(consolidator)
	result, err := runner.Run(ctx, evts, s, RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Assert: no candidates
	if len(result.Candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result.Candidates))
	}

	// Assert: no nodes created
	behaviors := queryBehaviorNodes(t, s)
	if len(behaviors) != 0 {
		t.Errorf("expected 0 behaviors, got %d", len(behaviors))
	}

	// Assert: source events still captured (even with 0 candidates)
	if len(result.SourceEventIDs) != 20 {
		t.Errorf("expected 20 source event IDs, got %d", len(result.SourceEventIDs))
	}

	// Assert: only Extract-phase LLM calls (no classify/relate)
	// 1 summarize + 1 arc + 1 extract = 3 calls for single chunk
	if mock.callIndex != 3 {
		t.Errorf("expected 3 LLM calls (extract only), got %d", mock.callIndex)
	}
}

func TestIntegration_ExecutorConfigGate(t *testing.T) {
	mock := &mockLLMClient{}

	tests := []struct {
		name     string
		executor string
		client   llm.Client
		wantType string
	}{
		{"heuristic", "heuristic", mock, "*consolidation.HeuristicConsolidator"},
		{"llm", "llm", mock, "*consolidation.LLMConsolidator"},
		{"default_empty", "", mock, "*consolidation.HeuristicConsolidator"},
		{"llm_nil_client", "llm", nil, "*consolidation.HeuristicConsolidator"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewConsolidator(tt.executor, tt.client, nil)
			gotType := fmt.Sprintf("%T", c)
			if gotType != tt.wantType {
				t.Errorf("NewConsolidator(%q): got %s, want %s", tt.executor, gotType, tt.wantType)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/consolidation/ -run "TestIntegration_EmptySession|TestIntegration_ExecutorConfigGate" -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/consolidation/integration_test.go
git commit -m "test: add empty session and executor config gate integration tests"
```

---

### Task 8: Context Cancellation + Merge Target Not Found (Scenarios 7 & 8)

**Files:**
- Modify: `internal/consolidation/integration_test.go`

- [ ] **Step 1: Write the context cancellation test**

```go
func TestIntegration_ContextCancellation(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())

	// Mock returns valid Extract responses. We cancel after Extract completes.
	mock := &mockLLMClient{
		responses: []string{
			happyPathSummarizeChunk1,
			happyPathArcSynthesis,
			happyPathExtractChunk1,
			happyPathClassifyBatch, // Should never be reached
		},
	}

	// Use a wrapper that cancels after Extract phase (3 calls for 1 chunk)
	wrappedMock := &cancelAfterNMock{
		inner:      mock,
		cancelAt:   3, // Cancel after 3rd call (extract done)
		cancelFunc: cancel,
	}

	evts := makeEventsWithSignals(20,
		withCorrection(5, "no, don't do that"),
		withSessionID("sess-cancel"),
	)

	consolidator := NewLLMConsolidator(wrappedMock, nil, DefaultLLMConsolidatorConfig())
	runner := NewRunner(consolidator)
	result, err := runner.Run(ctx, evts, s, RunOptions{})

	// Assert: context error returned
	// The pipeline may or may not error depending on where cancellation is checked.
	// If Extract completed and Classify hasn't started, the Runner's ctx.Err() check
	// between stages catches it. Accept either: error returned, or result with
	// no classified memories.

	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	// Assert: no nodes in store (Promote never ran)
	behaviors := queryBehaviorNodes(t, s)
	if len(behaviors) != 0 {
		t.Errorf("expected 0 behaviors (promote never ran), got %d", len(behaviors))
	}

	// If result is non-nil, it should have candidates but no classified memories
	if result != nil && len(result.Classified) > 0 {
		t.Errorf("expected no classified memories after cancellation, got %d", len(result.Classified))
	}
}
```

And the helper mock wrapper in `test_helpers_test.go`:

```go
// cancelAfterNMock wraps a mockLLMClient and cancels a context after N calls.
type cancelAfterNMock struct {
	inner      *mockLLMClient
	cancelAt   int
	cancelFunc context.CancelFunc
	count      int
}

func (m *cancelAfterNMock) Complete(ctx context.Context, msgs []llm.Message) (string, error) {
	resp, err := m.inner.Complete(ctx, msgs)
	m.count++
	if m.count >= m.cancelAt {
		m.cancelFunc()
	}
	return resp, err
}

func (m *cancelAfterNMock) Available() bool { return m.inner.Available() }
```

- [ ] **Step 2: Write the merge-target-not-found test**

```go
func TestIntegration_MergeTargetNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Relate proposes a merge into a non-existent node
	relateResponse := `{
		"relationships": [
			{
				"memory_index": 0,
				"action": "merge",
				"edges": [],
				"merge_into": {
					"target_id": "bhv-nonexistent",
					"strategy": "absorb",
					"merged_content": {"canonical": "Merged text", "summary": "Merged", "tags": ["test"]}
				},
				"rationale": "Merge into existing"
			},
			{
				"memory_index": 1,
				"action": "create",
				"edges": [],
				"merge_into": null,
				"rationale": "New behavior"
			}
		]
	}`

	mock := &mockLLMClient{
		responses: []string{
			happyPathSummarizeChunk1,
			happyPathArcSynthesis,
			happyPathExtractChunk1, // 2 candidates
			happyPathClassifyBatch, // Classify
			relateResponse,         // Relate with bad merge target
		},
	}

	evts := makeEventsWithSignals(20,
		withCorrection(5, "no, don't do that"),
		withCorrection(10, "use this approach instead"),
		withSessionID("sess-badmerge"),
	)

	consolidator := NewLLMConsolidator(mock, nil, DefaultLLMConsolidatorConfig())
	runner := NewRunner(consolidator)
	result, err := runner.Run(ctx, evts, s, RunOptions{})

	// Assert: pipeline completes (merge failure is not fatal)
	if err != nil {
		t.Fatalf("Run should not fail on bad merge target: %v", err)
	}

	// Assert: at least 1 behavior created (the "create" action succeeded,
	// and the failed merge may fall through to create-as-new)
	behaviors := queryBehaviorNodes(t, s)
	if len(behaviors) == 0 {
		t.Error("expected at least one behavior in store")
	}

	// Assert: promoted > 0
	if result.Promoted <= 0 {
		t.Error("expected at least one promoted behavior")
	}
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/consolidation/ -run "TestIntegration_" -v -count=1`
Expected: All 8 integration tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/consolidation/integration_test.go internal/consolidation/test_helpers_test.go
git commit -m "test: add context cancellation and merge-target-not-found integration tests"
```

---

### Task 9: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test ./internal/consolidation/ -v -count=1`
Expected: All tests PASS (existing unit tests + new integration tests)

- [ ] **Step 2: Run linter**

Run: `golangci-lint run ./internal/consolidation/...`
Expected: No issues

- [ ] **Step 3: Run gofmt**

Run: `gofmt -l internal/consolidation/`
Expected: No files listed (all formatted)

- [ ] **Step 4: Verify test count**

Run: `go test ./internal/consolidation/ -v -count=1 2>&1 | grep -c "=== RUN.*TestIntegration_"`
Expected: 9 (8 scenarios + 1 helper test)

- [ ] **Step 5: Commit any final fixes**

```bash
git add internal/consolidation/
git commit -m "test: complete v1 consolidation integration test suite"
```
