# v1 LLM Consolidator Design

**Date:** 2026-03-16
**Status:** Draft
**Authors:** Nic van Dessel, Claude (brainstorming partner)
**Epic:** floop-23x
**Depends on:** v0 heuristic pipeline (merged), consolidation CLI/MCP (PR #215)

## Overview

Replace v0's regex heuristics with a strong LLM (Sonnet/Opus) across all four consolidation stages. Same `Consolidator` interface — no interface changes. Every decision logged for v2 distillation training data.

The core insight: consolidation is compaction. The same problem LLM agents solve when compressing conversation history — understanding trajectory, extracting what matters, discarding noise. v1 applies that to behavioral memory extraction.

## Prerequisite: `llm.Client` Interface Refactor

### Motivation

The current `llm.Client` interface bakes domain-specific methods (`CompareBehaviors`, `MergeBehaviors`) into what should be a generic LLM transport. The v1 consolidator needs raw prompt→response capability, not behavior-comparison wrappers.

### New Interface

```go
// Message represents a chat message for LLM completion.
type Message struct {
    Role    string // "system", "user", "assistant"
    Content string
}

// Client is a generic LLM prompt runner. Domain-specific logic
// (prompts, parsing, validation) lives in the consuming package.
type Client interface {
    Complete(ctx context.Context, messages []Message) (string, error)
    Available() bool
}

// Optional interfaces (type-assert to check support)
type EmbeddingComparer interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    CompareEmbeddings(ctx context.Context, a, b string) (float64, error)
}

type Closer interface {
    Close() error
}
```

### Migration

| From `llm/` | To |
|---|---|
| `ComparisonPrompt`, `ParseComparisonResponse`, `ComparisonResult` | `dedup/` |
| `MergePrompt`, `ParseMergeResponse`, `MergeResult` | `dedup/` |
| `CorrectionExtractionPrompt`, `ParseCorrectionExtractionResponse` | `learning/` |
| `ExtractJSON` | `llm/` (stays — generic utility) |

### Deletions

- `FallbackClient` — removed entirely. Fallback logic belongs in domain packages (dedup falls back to Jaccard, consolidation falls back to v0 heuristic). The transport layer should not pretend it has an LLM when it doesn't.
- `CompareBehaviors` and `MergeBehaviors` methods on all client implementations
- `sendRequest`/`callAPI`/`runSubagent` become the backing for `Complete`

Each client (`AnthropicClient`, `OpenAIClient`, `SubagentClient`, `LocalClient`) implements only `Complete` + `Available`.

## `LLMConsolidator` Struct

```go
type LLMConsolidator struct {
    client    llm.Client                // thin Complete interface
    heuristic *HeuristicConsolidator    // v0 fallback
    decisions *logging.DecisionLogger   // JSONL decision log
    config    LLMConsolidatorConfig
}

type LLMConsolidatorConfig struct {
    Model          string  // e.g., "claude-sonnet-4-6"
    ChunkSize      int     // events per chunk (default: 20)
    MaxCandidates  int     // batch size for Classify (default: 30)
    TopK           int     // vector search neighbors for Relate (default: 5)
    RetryOnce      bool    // retry LLM call once on failure (default: true)
}
```

Implements the existing `Consolidator` interface. No interface changes.

## Stage 1: Extract — Three-Pass Chunked Architecture

### Motivation

A session may be 50-200+ events. We cannot assume the processing LLM has a large context window — the v2 distilled model will be 1-3B params with 4-8k context. The architecture must work at small windows while preserving session trajectory understanding.

### Pass 1: Chunk & Summarize

Split the session into chunks (~20 events each, configurable). Per-chunk LLM call produces a structured summary.

**Prompt structure:**
```
System: You are summarizing a segment of an AI agent conversation.

User: [20 events as structured JSON]

Output (structured JSON):
{
  "summary": "User was debugging auth middleware...",
  "tone": "frustrated",
  "phase": "stuck",
  "pattern": "debugging",
  "key_moments": [
    {"event_id": "evt-42", "type": "correction", "brief": "User rejected agent's approach"}
  ],
  "open_threads": ["auth token storage still unresolved"]
}
```

**Fields:**
- `tone`: neutral, curious, frustrated, satisfied, breakthrough
- `phase`: opening, exploring, building, stuck, resolving, wrapping-up
- `pattern`: teaching, collaborating, debugging, reviewing, planning
- `key_moments`: pointers to events worth deeper extraction in Pass 3
- `open_threads`: unresolved topics (feeds into resumption awareness)

Key moments are *pointers*, not extractions. Pass 1 flags where to look. Pass 3 does the real extraction.

### Pass 2: Arc Synthesis

One LLM call per session. Input: all chunk summaries + previous arc (if resuming an ongoing project).

**Prompt structure:**
```
System: You are analyzing the trajectory of a work session.

User:
  Previous arc: [null or prior arc summary from consolidation_runs]
  Chunk summaries: [array of Pass 1 outputs]

Output (structured JSON):
{
  "arc": "User spent first 30min setting up auth middleware...",
  "dominant_tone": "frustrated→resolved",
  "session_outcome": "resolved",
  "themes": ["auth", "sqlite", "token-storage"],
  "behavioral_signals": [
    "User strongly prefers direct approaches over abstractions",
    "User corrected agent 3 times on error handling style"
  ]
}
```

**Fields:**
- `session_outcome`: resolved, abandoned, paused, escalated
- `behavioral_signals`: high-level patterns the LLM noticed across the full session

### Pass 3: Extract

Per-chunk LLM call. Each chunk gets the full arc + existing behaviors as context.

**Prompt structure:**
```
System: You are extracting behavioral memories from a conversation segment.
        You understand the full session trajectory and existing knowledge base.

User:
  Session arc: [Pass 2 output]
  Existing behaviors (relevant): [top-N similar behaviors from vector search]
  Events: [chunk of ~20 events]

Output (structured JSON):
{
  "candidates": [
    {
      "source_events": ["evt-42", "evt-43"],
      "raw_text": "No don't mock the database, we got burned...",
      "candidate_type": "correction",
      "confidence": 0.92,
      "sentiment": "frustrated",
      "session_phase": "stuck",
      "interaction_pattern": "teaching",
      "rationale": "User explicitly corrected agent with historical context",
      "already_captured": false
    }
  ]
}
```

The `already_captured` field handles resumption — the LLM sees existing behaviors and identifies what's already known.

### Token Budget (Sonnet, ~100-event session)

| Pass | Calls | Input/call | Output/call | Total |
|---|---|---|---|---|
| Summarize | 5 chunks | ~4k | ~500 | ~22k |
| Arc | 1 | ~3k | ~500 | ~3.5k |
| Extract | 5 chunks | ~6k | ~1k | ~35k |
| **Total** | **11 calls** | | | **~60k tokens** |

At Sonnet pricing: ~$0.20 per session. At 5 sessions/day: ~$1/day. Temporary cost — v2 distilled model eliminates API dependency.

### Fallback Behavior

- Pass 1 fails → Pass 3 still runs with raw events, no summary context
- Pass 2 fails → Pass 3 runs without arc context (degraded but functional)
- Pass 3 fails for a chunk → fall back to `HeuristicConsolidator.Extract` for that chunk
- All passes fail → full fallback to v0

## Stage 2: Classify — Batched Labeling

### Approach

Single batched LLM call with all candidates. Classification benefits from seeing all candidates together — the LLM can notice duplicates across candidates and deduplicate at classification time.

**Prompt structure:**
```
System: You are classifying behavioral memories into a typed taxonomy.

User:
  Candidates: [array of all candidates from Extract]

Output (structured JSON):
{
  "classified": [
    {
      "source_events": ["evt-42", "evt-43"],
      "kind": "directive",
      "memory_type": "semantic",
      "scope": "universal",
      "importance": 0.85,
      "content": {
        "canonical": "Never mock the database in integration tests...",
        "summary": "No DB mocks in integration tests",
        "tags": ["testing", "database", "integration"]
      },
      "episode_data": null,
      "workflow_data": null,
      "rationale": "Explicit correction with historical justification."
    }
  ]
}
```

### What v1 Classify Does That v0 Cannot

- **Writes canonical form**: distills raw correction text into token-efficient behavioral directive
- **Generates summary**: 60-char compressed form for tiered injection
- **Infers scope**: "mentions our CI pipeline" → project-scoped; "always wrap errors" → universal
- **Assesses importance**: repeated corrections > offhand preferences
- **Extracts semantic tags**: understands meaning, not just keyword splitting
- **Structures workflows**: recognizes multi-step sequences, produces `WorkflowData`

### Size Guard

If candidates exceed 30, chunk into batches of 20.

### Fallback

If LLM returns bad JSON or errors, fall back to `HeuristicConsolidator.Classify` for failed candidates. Per-candidate fallback, not per-batch.

## Stage 3: Relate — Vector Search + LLM Relationship Proposals

### Three Operations

**1. Vector search (local, no LLM):**
Embed each memory's canonical text via existing `EmbeddingComparer` interface. Query LanceDB for top-K nearest neighbors. Filter by similarity threshold.

**2. LLM relationship proposals (one batched call):**

```
System: You are analyzing relationships between new memories and existing behaviors.

User:
  New memories: [classified memories]
  For each memory, nearest neighbors: [top-5 per memory from vector search]

Output (structured JSON):
{
  "relationships": [
    {
      "memory_index": 0,
      "action": "create",
      "edges": [
        {"target": "bhv-123", "kind": "similar-to", "weight": 0.82}
      ],
      "merge_into": null,
      "rationale": "Related but distinct focus"
    },
    {
      "memory_index": 1,
      "action": "merge",
      "edges": [],
      "merge_into": {
        "target_id": "bhv-789",
        "strategy": "absorb",
        "merged_content": { "canonical": "...", "summary": "...", "tags": [...] }
      },
      "rationale": "Near-duplicate, adds historical context"
    }
  ]
}
```

**Merge strategies:**
- `absorb`: update existing behavior's content. New memory is absorbed.
- `supersede`: replace existing behavior. New memory is more accurate/complete.
- `supplement`: keep both. New memory adds detail via `supplements` edge.

**3. Co-occurrence edges (local, no LLM):**
Same-session memories get `co-activated` edges for the existing Hebbian system.

### Token Budget

~15k tokens total (one batched LLM call + local vector/edge work).

### Fallback Chain

| Missing Component | Impact | Behavior |
|---|---|---|
| No embeddings (no CGO/yzma) | Can't do vector search | LLM still gets behaviors from QueryNodes, unranked |
| LLM fails | Can't propose relationships | Vector search + co-occurrence edges only |
| Both | Minimal relating | Co-occurrence edges only |

## Stage 4: Promote — Write to Graph

### Merge Execution

- **absorb**: Update existing behavior's content with merged version. Bump confidence. Add provenance entry.
- **supersede**: Soft-delete old behavior (mark as `merged` kind). Create new with combined lineage.
- **supplement**: Keep existing unchanged. Create `supplements` edge to new behavior.

### Provenance

```go
Provenance{
    SourceType:     "consolidated",
    ConsolidatedBy: "claude-sonnet-4-6",
    ConsolidatedAt: time.Now(),
    SourceEvents:   candidate.SourceEvents,
    Confidence:     candidate.Confidence,
}

Metadata: map[string]any{
    "consolidation_run":     runID,
    "sentiment":             candidate.Sentiment,
    "session_phase":         candidate.SessionPhase,
    "extraction_rationale":  candidate.Rationale,
}
```

### Arc Persistence

After promotion, persist the arc summary for incremental resumption.

```sql
CREATE TABLE consolidation_runs (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    session_id TEXT,
    arc_summary TEXT,
    candidates_found INTEGER,
    memories_promoted INTEGER,
    merges_executed INTEGER,
    model TEXT,
    tokens_used INTEGER,
    duration_ms INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Decision Logging

Every decision logged to `DecisionLogger` JSONL:

```json
{"event": "promote", "run_id": "...", "memory_id": "...", "kind": "directive", "confidence": 0.92}
{"event": "merge", "run_id": "...", "memory_id": "...", "into": "bhv-789", "strategy": "absorb"}
{"event": "skip", "run_id": "...", "memory_id": "...", "reason": "already_captured"}
```

## Candidate Struct Additions

```go
type Candidate struct {
    SourceEvents       []string       // existing
    RawText            string         // existing
    CandidateType      string         // existing
    Confidence         float64        // existing
    SessionContext     map[string]any  // existing

    // v1 additions
    Sentiment          string  // frustrated, neutral, satisfied, breakthrough
    SessionPhase       string  // opening, exploring, building, stuck, resolving, wrapping-up
    InteractionPattern string  // teaching, collaborating, debugging, reviewing, planning
    Rationale          string  // LLM's reasoning for extraction
    AlreadyCaptured    bool    // true if duplicates existing behavior
}
```

These are training signals for v2 distillation and autoresearch correlation, not user-facing features.

## Error Handling

### Per-Stage Fallback

| Stage | LLM Failure | Fallback |
|---|---|---|
| Extract Pass 1 | API error, timeout, bad JSON | Skip summarization, pass raw events to Pass 3 |
| Extract Pass 2 | Same | No arc context, Pass 3 still runs |
| Extract Pass 3 | Same | v0 `HeuristicConsolidator.Extract` for that chunk |
| Classify | Bad JSON, missing fields | v0 `HeuristicConsolidator.Classify` for failed candidates |
| Relate LLM | LLM call fails | Vector search + co-occurrence edges only |
| Promote | Store write fails | Return error, no partial writes |

Key principle: **degrade per-stage, not per-run.** A failed Pass 2 doesn't kill extraction.

### JSON Validation

1. `ExtractJSON` (handles markdown code blocks)
2. `json.Unmarshal` into typed struct
3. Field validation (confidence in range, valid enum values, required fields)

One retry per failed LLM call with tighter prompt. Then fallback.

## Tiered Retention

| Artifact | Retention | Rationale |
|---|---|---|
| Raw events | 90 days (existing) | Ephemeral, designed for pruning |
| Arc summaries | Latest per project + last 5 | Only recent context matters for resumption |
| Decision logs | Until v2 training, then archive | This IS the training data |
| Consolidated behaviors | Permanent | The output |

## Testing Strategy

### Unit Tests (per stage, mock LLM)

```go
type MockClient struct {
    Responses []string
    Calls     [][]llm.Message
}
```

- Extract: feed known events, mock returns known candidates. Verify structure, source linkage, sentiment/phase.
- Classify: feed known candidates, mock returns classifications. Verify kind/type/scope.
- Relate: feed known memories + pre-populated store. Verify edges and merge proposals.
- Promote: feed memories + edges + merges into in-memory store. Verify nodes/edges/provenance.

### Fallback Tests

- Mock returns garbage JSON → v0 fallback activates
- Mock returns error → v0 fallback activates
- Mock returns partial valid JSON → validation catches it
- Per-stage independent fallback verification

### Golden File Tests

Store example prompt inputs and expected outputs as golden files. Prompt changes require explicit golden file updates.

### Integration Tests

`_integration_test.go` with build tags for live API calls. Not in CI — run manually or in separate job with API keys.

### Quality Validation

Real quality signal comes from autoresearch/floop-bench, not unit tests. Unit tests verify mechanics (valid output). Autoresearch verifies quality (useful behaviors). Decision logs must be structured correctly for this pipeline.

## Dependency Graph

```
floop-23x.2 (interface refactor)
  → floop-23x.1 (wire executor config)
    → floop-670 (Extract)
      → floop-9pn (decision logging)
  → floop-w3b (Classify)        [parallel with Extract]
  → floop-j99 (Relate)          [parallel with Extract]
    → floop-23x.3 (Promote)
```

Interface refactor unblocks the three core stages. Extract/Classify/Relate can parallelize. Promote waits on Relate. Decision logging waits on Extract.
