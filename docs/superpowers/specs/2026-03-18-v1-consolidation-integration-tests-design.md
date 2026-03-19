# v1 Consolidation Pipeline Integration Tests

**Date:** 2026-03-18
**Status:** Draft
**Authors:** Nic van Dessel, Claude (brainstorming partner)
**Epic:** floop-23x (v1 consolidator)
**Related:** floop-cye (Tier 1 benchmarks — separate project, consumes this)

## Purpose

Validate that the v1 LLM consolidation pipeline is mechanically correct: stages connect, data flows through, graph state is correct after promotion, fallbacks activate when expected. This is plumbing validation, not quality assessment.

Quality validation (does v1 produce *better* behaviors than v0?) is out of scope — that's floop-cye's job, exercising the pipeline as a consumer.

## Target Interface

These tests target the interface after the full PR stack (#226-#230) merges. Key signatures:

```go
// Consolidator interface (post-merge, includes skips from feat/v1/relate)
Extract(ctx, events) ([]Candidate, error)
Classify(ctx, candidates) ([]ClassifiedMemory, error)
Relate(ctx, memories, store) ([]Edge, []MergeProposal, []int, error)  // []int = skip indices
Promote(ctx, memories, edges, merges, skips, store) error
```

The `skips` parameter (indices of memories to skip as already-captured) flows from Relate through to Promote via the Runner. `Promoted = len(classified) - len(merges) - len(skips)`.

**Prerequisite:** The `feat/v1/relate` branch introduced `skips` to the interface, but `LLMConsolidator.Promote` in `promote.go` has not been updated to accept the `skips` parameter yet. This must be fixed in the PR stack before the integration tests can compile. The fix is mechanical: add `skips []int` param, filter skipped indices in the promote loop.

## Approach

- **Scripted mock LLM**: Pre-defined JSON responses for each LLM call in sequence. No real API calls, no network, no build tags. Runs in CI with `go test ./...`.
- **In-memory store**: `store.NewSQLiteGraphStore(t.TempDir())` — uses real SQLite for full feature coverage (co-activation, edge weights, etc.). Existing test pattern throughout the codebase.
- **Synthetic events**: Realistic but hand-crafted `events.Event` slices with known behavioral signals.
- **File**: `internal/consolidation/integration_test.go`

## Test Infrastructure

### Mock LLM Client

Reuse the `mockLLMClient` pattern already established in `extract_test.go` — a package-internal test double with per-call response/error sequences:

```go
type mockLLMClient struct {
    responses []string        // returned in order per Complete() call
    errors    []error         // optional per-call errors (nil = success)
    callIndex int             // incremented on each Complete()
    calls     [][]llm.Message // captured inputs for assertion
    available bool
}
```

This already exists in `extract_test.go`. For the integration test, either promote it to a shared `test_helpers_test.go` file or duplicate it (it's 15 lines). Do NOT use the separate `llm.MockClient` — it lacks per-call error injection and uses the old `CompareBehaviors`/`MergeBehaviors` interface alongside `Complete`.

### Synthetic Event Factory

Build on the existing `makeEvents(n int)` from `extract_test.go` by adding functional options (the existing version takes only `n`, these options are new):

```go
func makeEvents(n int, opts ...eventOpt) []events.Event
func withCorrection(idx int, text string) eventOpt
func withPreference(idx int, text string) eventOpt
func withSessionID(id string) eventOpt
func withProjectID(id string) eventOpt
```

Generates `n` filler events (user messages + agent responses) with injected behavioral signals at specific indices.

### Store Setup

```go
func newTestStore(t *testing.T) store.GraphStore {
    s, err := store.NewSQLiteGraphStore(t.TempDir())
    require.NoError(t, err)
    t.Cleanup(func() { s.Close() })
    return s
}
```

Uses real SQLite (matching existing test patterns in `store/sqlite_test.go`). This gives full feature coverage including co-activation tracking, edge weight updates, and schema migrations.

### Assertion Helpers

```go
// Query all nodes of a kind, assert count
func assertNodeCount(t *testing.T, s store.GraphStore, kind string, expected int)

// Query all nodes, filter with predicate, assert at least one matches
func assertNodeExists(t *testing.T, s store.GraphStore, predicate func(store.Node) bool)

// GetEdges(source, DirectionOutbound, kind), filter for target
func assertEdgeExists(t *testing.T, s store.GraphStore, source, target, kind string)
func assertNoEdge(t *testing.T, s store.GraphStore, source, target, kind string)

// Check node.Metadata[key] == expected
func assertProvenance(t *testing.T, node store.Node, key string, expected interface{})
```

Note: `assertNodeExists` uses `QueryNodes(ctx, map[string]interface{}{})` to enumerate all nodes, then applies the predicate. This is fine for test stores with <100 nodes.

## Test Scenarios

### 1. Happy Path — Full Pipeline

**Setup:**
- 40 synthetic events (2 chunks of 20) with 3 corrections and 1 preference injected
- Pre-seed store with 1 existing behavior node (merge target for absorb test)
- Mock responses for 7 calls: summarize chunk 1, summarize chunk 2, arc synthesis, extract chunk 1, extract chunk 2, classify batch, relate batch
- Mock responses: Extract returns 4 candidates, Classify labels them, Relate proposes 1 merge (absorb) + 1 skip (already captured) + 2 creates with edges

**Assertions:**
- 2 new behavior nodes created in store (4 candidates - 1 merge - 1 skip)
- 1 existing node updated via absorb merge (content merged, confidence bumped)
- Edges created: similar-to, co-activated between same-session behaviors
- Each new node has provenance: `source_type: "consolidated"`, `consolidated_by` set to model name
- Source events traced back to original event IDs
- `RunResult.Promoted == len(classified) - len(merges) - len(skips)`

**Call count:** Verify mock received exactly 7 Complete() calls (2 summarize + 1 arc + 2 extract + 1 classify + 1 relate). This count is specific to the happy path config (ChunkSize=20, no retries, single classify batch). Document it as fragile if config changes.

### 2. Incremental Consolidation — No Duplicates

**Setup:**
- Run 1: 20 events with 2 corrections → full pipeline creates 2 behavior nodes
- Run 2: 30 events (overlapping + new) with 3 total corrections
  - Extract mock returns 3 candidates (including the 2 previously extracted)
  - Classify mock labels all 3
  - Relate mock returns `skips: [0, 1]` for the 2 known candidates (action: "skip"), creates for index 2

Deduplication happens at the Relate stage: the LLM sees the new memories alongside existing neighbors from vector search and proposes "skip" for already-captured ones. The `skips []int` return propagates to Promote, which excludes those indices.

**Assertions:**
- After run 1: 2 behavior nodes in store
- After run 2: 3 behavior nodes total (not 5)
- The 2 original nodes unchanged (no duplicate writes)
- The 1 new node has correct provenance
- `RunResult.Skips` has length 2 in run 2

### 3. Full Fallback — LLM Returns Errors

**Setup:**
- Mock client with `available: true` but every `Complete()` call returns an error
- 20 events with 2 corrections (signals detectable by v0 heuristic patterns)

The v1 stages call `Complete()` directly without checking `Available()` first. Fallback is error-driven: when `Complete()` fails, each stage falls back to its heuristic equivalent. We simulate total LLM failure via errors, not unavailability.

**Assertions:**
- Pipeline completes without error (v0 heuristic handles all stages via per-stage fallback)
- Behavior nodes created in store (heuristic extraction found the corrections)
- Provenance does NOT contain `consolidated_by` model name (heuristic path)
- Mock `Complete()` was called (Extract attempted LLM before falling back)
- All calls errored (verify call count matches expected Extract attempts)

**Note:** There is an asymmetry — `relate.go` checks `c.client.Available()` before calling `Complete()`, while Extract and Classify do not. With `available: true` and all calls erroring, Relate will also attempt `Complete()` and fail. If testing with `available: false`, Relate would skip the LLM path entirely without calling `Complete()`. This scenario uses `available: true` + errors to exercise the error-driven fallback path consistently across all stages.

### 4. Per-Stage Fallback — Classify Fails

**Setup:**
- Mock returns valid responses for Extract passes, then returns error for the Classify call, then valid responses for Relate
- 20 events with 2 corrections

**Assertions:**
- Extract used LLM (mock received summarize + arc + extract calls)
- Classify fell back to heuristic (candidates still classified, but with heuristic-style kinds/tags)
- Relate still called with LLM (mock received relate call after classify error)
- Promote writes valid nodes to store
- Decision log contains a fallback entry for the Classify stage

### 5. Empty Session — Early Exit

**Setup:**
- 20 events with no behavioral signals (pure Q&A, no corrections/preferences)
- Extract mock returns `{"candidates": []}`

**Assertions:**
- Pipeline returns early after Extract
- No nodes created in store
- No Classify/Relate/Promote calls made
- Mock call count reflects only Extract passes (summarize + arc + extract, no classify/relate)

### 6. Executor Config Gate

**Setup:**
- Create `LLMConsolidatorConfig` with LLM client available
- Test factory with each executor value

**Assertions:**
- `executor: "heuristic"` → factory returns `*HeuristicConsolidator` (type assertion)
- `executor: "llm"` → factory returns `*LLMConsolidator`
- `executor: ""` (default) → factory returns `*HeuristicConsolidator`
- `executor: "llm"` with `client != nil` → factory returns `*LLMConsolidator` regardless of `Available()` state (per-stage fallback handles unavailability, not the factory)
- `executor: "llm"` with `client == nil` → factory returns `*HeuristicConsolidator` (nil guard)

### 7. Context Cancellation Mid-Pipeline

**Setup:**
- Mock returns valid Extract responses, then blocks on Classify (or use a context that cancels after a delay)
- Simpler: use `context.WithCancel`, cancel after Extract completes but before Classify runs
- The Runner checks `ctx.Err()` between stages

**Assertions:**
- Runner returns a context error
- Partial `RunResult` contains candidates from Extract but no classified memories
- No nodes created in store (Promote never ran)
- Mock call count shows only Extract-phase calls

### 8. Merge Target Not Found — Graceful Degradation

**Setup:**
- Full pipeline with mock responses where Relate proposes a merge targeting a non-existent node ID (`"bhv-nonexistent"`)
- Store does not contain that node

**Assertions:**
- Promote handles the missing target gracefully (logs error, skips the merge)
- The memory that was supposed to merge is created as a new node instead (fallthrough behavior)
- Other memories in the batch are promoted normally
- Pipeline does not return an error (partial failure is not fatal)

## Scripted Mock Response Templates

Each scenario needs hand-crafted JSON responses matching the prompt schemas. These live as `const` blocks in the test file (not golden files — they're part of the test logic, not captured from real runs).

See the implementation plan at `docs/superpowers/plans/2026-03-18-v1-integration-tests.md` for the full set of response constants.

## What This Does NOT Test

- **Prompt quality**: Whether prompts extract the right things from real sessions (→ floop-cye)
- **Real LLM parsing**: Whether actual Claude/GPT responses parse correctly (→ golden file tests, future)
- **MCP handler integration**: Whether `floop_consolidate` MCP tool invokes the pipeline correctly (→ existing `mcp/e2e_test.go`)
- **Vector search accuracy**: Whether embeddings find the right neighbors (→ floop-cye)
- **Performance**: Token budgets, latency (→ observability, not tests)

## Implementation Notes

- Use `require` (not `assert`) for setup steps — fail fast if store or mock setup is wrong
- Each `t.Run` gets its own store and mock — no shared state between scenarios
- Keep the test file under 500 lines. If it grows, extract helpers to `integration_helpers_test.go`
- The scripted mock responses must match the JSON schemas in `*_prompts.go` — if a prompt schema changes, the corresponding mock response must be updated. This is intentional coupling: if you change the prompt contract, you must update the integration test.
