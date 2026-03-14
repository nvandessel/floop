# Memory Consolidation System Design

**Date:** 2026-03-13
**Status:** Draft
**Authors:** Nic van Dessel, Claude (brainstorming partner)
**Depends on:** PR #208 (LanceDB) — branch from `feat/lancedb` or `main` post-merge

## Vision

Transform floop from a correction-capture system into a full experiential memory system inspired by cognitive neuroscience. The system should passively capture rich context from agent conversations, automatically consolidate it into typed memories, and continuously validate its own effectiveness through automated experimentation.

The human brain captures everything — episodic events, procedural workflows, semantic facts, associative connections — then consolidates useful patterns during sleep. Floop should do the same.

## Scientific Lineage

This design draws from established research in cognitive science and AI memory systems:

| Source | Key Contribution | How It Informs Floop |
|--------|-----------------|---------------------|
| [Complementary Learning Systems (McClelland et al., 1995)](https://www.researchgate.net/publication/15575602) | Brain has two learning systems: hippocampus (fast, episodic) and neocortex (slow, semantic). Consolidation transfers between them. | Two-phase architecture: fast capture buffer + slow consolidation pipeline |
| [Generative Agents (Park et al., 2023)](https://arxiv.org/abs/2304.03442) | Memory stream + reflection produces higher-level abstractions from raw observations | Consolidation pipeline extracts abstract behaviors from raw conversation events |
| [MemGPT / Letta (Packer et al., 2023)](https://arxiv.org/abs/2310.08560) | OS-inspired memory management: tiered storage with LLM as memory manager | Tiered injection (already in floop), LLM-driven consolidation decisions |
| [Memory in the Age of AI Agents (2025)](https://arxiv.org/abs/2512.13564) | Taxonomy: factual vs experiential memory. Experiential = case-based, strategy-based, skill-based | Expanded memory types. Floop is experiential memory; code indexing (Serena, lilbee) is factual |
| [SYNAPSE (2025)](https://arxiv.org/abs/2601.02744) | Spreading activation for knowledge retrieval in agent systems | Already implemented in floop. Foundation for retrieval over expanded memory types |
| [Autoresearch (Karpathy, 2026)](https://github.com/karpathy/autoresearch) | Autonomous experimentation loop: hypothesize, modify, run, measure, keep/discard | floop-bench evolves into automated validation loop for consolidation quality |
| Small Language Models for Agents (various, 2025-2026) | SLMs (1-8B) match LLMs on focused classification with ~100 labeled examples | Distillation path: strong LLM consolidator generates training data for local model |

## Principles

1. **Capture everything, consolidate selectively.** The raw buffer is dumb and greedy. Intelligence lives in the consolidation pipeline.
2. **Agent-agnostic.** Works with any agent that produces conversation transcripts. No agent cooperation required.
3. **Science-grounded.** Every architectural decision traces to established research. This is what distinguishes floop from the mass of tools people are building.
4. **Measurably better.** Every change validated through the autoresearch loop. No vibes-only improvements.
5. **Phased delivery.** v0 ships with heuristics. v1 adds LLM consolidation. v2 distills to local model. Each phase generates training data for the next.

## Architecture Overview

```
                        CAPTURE SURFACE
                    (agent-agnostic ingestion)
                              |
              ┌───────────────┼───────────────┐
              |               |               |
        Session Hooks    CLI Ingest      MCP Observe
        (automatic)    (floop ingest)   (floop_observe)
              |               |               |
              └───────────────┼───────────────┘
                              |
                              v
                    ┌─────────────────┐
                    |   RAW BUFFER    |  Append-only event log
                    |  (events table) |  SQLite, ephemeral (90d)
                    └────────┬────────┘
                             |
                    CONSOLIDATION PIPELINE
                      ("The Hippocampus")
                             |
              ┌──────────────┼──────────────┐
              v              v              v
          [Extract]     [Classify]     [Relate]
          candidates    typed memories  graph edges
              |              |              |
              └──────────────┼──────────────┘
                             |
                          [Promote]
                             |
                             v
                    ┌─────────────────┐
                    |  BEHAVIOR GRAPH |  Single global store
                    | (SQLite + Lance |  Spreading activation
                    |   DB vectors)   |  Tiered injection
                    └────────┬────────┘
                             |
                    AUTORESEARCH LOOP
                    (floop-bench evolved)
                             |
              ┌──────────────┼──────────────┐
              v              v              v
          Tier 1:        Tier 2:        Experiments
          Unit bench     Integration    Parameter sweep
          (retrieval)    (SWE-bench)    (overnight)
              |              |              |
              └──────────────┼──────────────┘
                             |
                    Feeds back into consolidation
                    parameters and training data
```

## Section 1: Raw Buffer & Ingestion Surface

### Event Schema

The raw buffer is an append-only event log. It does not interpret, classify, or judge. It records what happened.

```go
type Event struct {
    ID          string            `json:"id"`          // ULID
    SessionID   string            `json:"session_id"`  // groups events into conversations
    Timestamp   time.Time         `json:"timestamp"`
    Source      string            `json:"source"`      // "claude-code", "gemini", "codex", etc.
    Actor       EventActor        `json:"actor"`       // user | agent | tool | system
    Kind        EventKind         `json:"kind"`        // message | action | result | error | correction
    Content     string            `json:"content"`     // raw content
    Metadata    map[string]any    `json:"metadata"`    // optional context
    ProjectID   string            `json:"project_id"`  // from .floop/config.yaml, nullable
    Provenance  *EventProvenance  `json:"provenance"`  // optional rich provenance
}

type EventProvenance struct {
    // Agent details (optional)
    Model        string `json:"model,omitempty"`         // "claude-sonnet-4-6"
    ModelVersion string `json:"model_version,omitempty"` // specific checkpoint
    AgentVersion string `json:"agent_version,omitempty"` // "claude-code@1.2.3"

    // Context (optional)
    Branch       string `json:"branch,omitempty"`        // git branch at capture time
    TaskContext  string `json:"task_context,omitempty"`   // what the agent was working on
}
```

### Ingestion Methods

Four methods, all producing the same `Event` records:

| Method | How | Best For |
|--------|-----|----------|
| **Session hooks** | floop's existing hook system. On session-end, auto-import the agent's transcript | Claude Code (already has hooks). Zero agent effort |
| **`floop ingest <file>`** | CLI command. Format adapters parse agent-specific transcript formats | Post-session batch import. Any agent that produces logs |
| **Stdin pipe** | `cat transcript.md \| floop ingest --format markdown --source gemini` | Unix composability. Script into any workflow |
| **`floop_observe` MCP tool** | Fire-and-forget event push. No response needed | Cooperative agents. Low friction |

### Format Adapters

Adding support for a new agent = one adapter function:

```go
type TranscriptAdapter interface {
    Parse(reader io.Reader) ([]Event, error)
    Format() string // "claude-code-jsonl", "markdown", "generic-json"
}
```

Initial adapters:
- **Claude Code JSONL** — native session log format
- **Plain text / Markdown** — universal fallback, heuristic actor detection
- **Generic JSON events** — structured logs from any agent that can emit JSON

### Storage

SQLite table in `~/.floop/floop.db` (same database as behaviors, separate table):

```sql
CREATE TABLE events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    source TEXT NOT NULL,
    actor TEXT NOT NULL CHECK(actor IN ('user', 'agent', 'tool', 'system')),
    kind TEXT NOT NULL CHECK(kind IN ('message', 'action', 'result', 'error', 'correction')),
    content TEXT NOT NULL,
    metadata TEXT,        -- JSON
    project_id TEXT,      -- nullable
    provenance TEXT,      -- JSON, nullable
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_events_session ON events(session_id);
CREATE INDEX idx_events_timestamp ON events(timestamp);
CREATE INDEX idx_events_project ON events(project_id);
```

### Retention

Events older than a configurable retention window (default: 90 days) get pruned. The consolidation pipeline has already extracted what matters. Raw events are ephemeral; consolidated memories are permanent.

```bash
floop events --prune --older 90d   # manual cleanup
# Also runs automatically during consolidation
```

## Section 2: Single-Store Architecture

### Motivation

The current dual-store model (local `.floop/floop.db` + global `~/.floop/floop.db`) introduces routing complexity: which store to write to, which to read from, how to dedup across stores. The new architecture uses a single global store with project scoping via metadata.

The brain doesn't have separate memory stores for "work" and "home." It has one store with contextual associations. When you're at work, work-related memories activate through context. That's what spreading activation already does.

### Store Layout

```
~/.floop/
  floop.db          # all behaviors, all projects, all events
  vectors/           # LanceDB embeddings (from PR #208)
  models/            # local model files (yzma, future consolidation model)
  cache/             # pack downloads, etc.

/path/to/project/.floop/
  config.yaml        # project identity + settings (tracked in git)
```

### Project Identity

Projects are identified by stable `namespace/name` IDs, not filesystem paths. The path is a lookup mechanism — floop walks up directories to find `.floop/config.yaml`, reads the project ID, and queries the store accordingly.

```yaml
# .floop/config.yaml (tracked in git)
project:
  id: "nic/floop"         # stable identity, same format as pack IDs
  name: "floop"           # human-readable display name
```

This survives repo moves, renames, and collaborator clones. Every clone of the same repo resolves to the same project ID.

### Scope Model

Every behavior has a `scope` field:

```
scope: "universal"          # activates anywhere context matches
scope: "project:nic/floop"  # ONLY activates in this project
```

- **Universal behaviors**: activate through normal spreading activation (soft, context-dependent)
- **Project-scoped behaviors**: hard-filtered. When `floop_active` fires in a project, all behaviors scoped to that project activate at minimum `TierSummary`. These are project rules, not suggestions.

This enables the AGENTS.md replacement vision: project-specific directives live as project-scoped behaviors in floop, not as markdown files scattered across repos.

### Resolution Chain

1. Agent connects to floop MCP (or CLI runs)
2. Floop walks up from cwd, finds `.floop/config.yaml`
3. Reads `project.id` (e.g., `"nic/floop"`)
4. Queries global store: behaviors where `scope = "universal"` OR `scope = "project:nic/floop"`
5. Spreading activation runs over that set
6. No config found? Falls back to universal behaviors only (graceful degradation)

### Pack Compatibility

Packs continue to work unchanged. They can contain both universal and project-scoped behaviors. The pack format (`.fpack`) already serializes behaviors as JSON nodes — new fields serialize naturally with defaults for backward compatibility.

```bash
# Create a pack of project rules
floop pack create ./my-project-rules.fpack \
  --filter-scope project --id "nic/floop-rules" --version "1.0.0"

# Teammate installs it — behaviors arrive scoped to project:nic/floop
floop pack install ./my-project-rules.fpack
```

### Rich Provenance

Behaviors carry detailed provenance for scientific rigor and historical comparison:

```go
type Provenance struct {
    // Existing fields (from internal/models/provenance.go)
    SourceType     SourceType `json:"source_type"`                            // authored | learned | imported
    CreatedAt      time.Time  `json:"created_at"`
    Author         string     `json:"author,omitempty"`                       // for authored behaviors
    CorrectionID   string     `json:"correction_id,omitempty"`               // for learned behaviors
    Package        string     `json:"package,omitempty"`                      // for imported (pack system)
    PackageVersion string     `json:"package_version,omitempty"`

    // New SourceType value: "consolidated" (added to SourceType enum)

    // New - consolidation lineage
    ConsolidatedBy string    `json:"consolidated_by,omitempty"` // model that did consolidation
    ConsolidatedAt time.Time `json:"consolidated_at,omitempty"`
    SourceEvents   []string  `json:"source_events,omitempty"`   // raw event IDs
    Confidence     float64   `json:"confidence,omitempty"`      // consolidator confidence

    // New - agent provenance (optional, from event metadata)
    SourceModel    string `json:"source_model,omitempty"`    // "claude-sonnet-4-6"
    SourceAgent    string `json:"source_agent,omitempty"`    // "claude-code@1.2.3"
    SourceProject  string `json:"source_project,omitempty"`  // "nic/floop"
    SourceBranch   string `json:"source_branch,omitempty"`   // "feat/auth-rewrite"
}
```

All new fields are optional. A bare `floop learn --right "wrap errors"` still works with zero ceremony. Full pipeline runs produce rich lineage automatically.

## Section 3: Consolidation Pipeline ("The Hippocampus")

### Overview

The consolidation pipeline transforms raw events into typed memories. Inspired by CLS theory: the hippocampus (fast capture) transfers useful patterns to the neocortex (long-term store) during sleep.

```
Raw Events → [Extract] → [Classify] → [Relate] → [Promote]
                ↓             ↓            ↓           ↓
           Candidates    Typed memories  Graph edges  Behaviors
```

### Stage 1: Extract

The consolidator reads a session's events and identifies **memory candidates** — moments worth remembering:

- **Corrections** ("no, do X instead of Y") — already flow through `floop_learn`, but catches ones the agent missed
- **Discoveries** ("oh, that's how this works") — insight moments
- **Decisions** ("let's go with approach B") — choices with rationale
- **Failures** ("that didn't work because...") — negative signal
- **Workflows** ("first I did X, then Y, then Z, and it worked") — procedural sequences
- **Context shifts** ("switching to work on the auth module") — episodic markers

```go
type Candidate struct {
    SourceEvents    []string          // which raw events this was extracted from
    RawText         string            // relevant excerpt
    CandidateType   string            // hint: correction, discovery, decision, failure, workflow, context
    Confidence      float64           // extractor confidence (0.0-1.0)
    SessionContext  map[string]any    // project, file, task, branch, model, etc.
}
```

### Pipeline Types

The stages communicate through these types:

```go
// Output of Classify, input to Relate and Promote
type ClassifiedMemory struct {
    Candidate                           // embeds the original candidate
    Kind         BehaviorKind           // episodic, directive, workflow, etc.
    MemoryType   string                 // semantic, episodic, procedural
    Scope        string                 // "universal" or "project:namespace/name"
    Importance   float64                // 0.0-1.0, influences decay rate
    Content      BehaviorContent        // canonical text, summary, tags
    EpisodeData  *EpisodeData           // non-nil for episodic memories
    WorkflowData *WorkflowData          // non-nil for workflow memories
}

// Edge proposals from the Relate stage. Reuses the existing edge schema
// (internal/store: source, target, kind, weight columns). The pipeline
// proposes edges using existing edge kinds (similar-to, requires, conflicts,
// co-activated) — no new edge types are introduced.
// The store.Edge type is the canonical definition; Relate returns []store.Edge.

// Proposed merge between a new memory and an existing behavior
type MergeProposal struct {
    Memory       ClassifiedMemory       // the new memory
    TargetID     string                 // existing behavior ID to merge into
    Similarity   float64                // cosine similarity score
    Strategy     string                 // "absorb" (update existing) | "supersede" (replace) | "supplement" (add detail)
}
```

### Stage 2: Classify

Each candidate gets typed into the expanded memory taxonomy:

| MemoryType | Kind | What It Captures | Example |
|-----------|------|-----------------|---------|
| `semantic` | `directive` | Rules, principles | "Always wrap Go errors with context" |
| `semantic` | `constraint` | Hard boundaries | "Never commit to main" |
| `semantic` | `preference` | Soft preferences | "Prefer Firefox over Chrome" |
| `episodic` | `episodic` | What happened, when, in what context | "Debugging auth took 3 sessions because the middleware was storing tokens wrong" |
| `procedural` | `procedure` | Linear multi-step how-to | "To deploy: test, build, push, verify" |
| `procedural` | `workflow` | Branching workflows with conditions | "If tests fail, check fixtures first; if they pass locally but fail in CI, check env vars" |

Classification is two-stage: `MemoryType` first (coarse, high accuracy), then `Kind` (fine-grained). Two-stage classification is more reliable than jumping directly to specific types.

The classifier also determines:
- **Scope**: universal or project-specific?
- **Importance**: decay rate (episodic memories decay faster than semantic)
- **Merge target**: overlap with existing behavior? (dedup)

### Stage 3: Relate

For each classified memory:

1. **Embed** using nomic-embed-text via yzma (existing infrastructure)
2. **Vector search** existing behaviors in LanceDB for semantic neighbors
3. **Propose edges**: `similar-to`, `requires`, `overrides`, `conflicts`
4. **Check for merges**: if similarity > threshold, merge rather than create new
5. **Associative signals become edges directly** — they don't create new nodes, they strengthen connections between existing ones (consistent with Hebbian co-activation system)

### Stage 4: Promote

Surviving candidates become full behaviors in the graph:

```go
Behavior {
    Kind:       "episodic"                    // expanded taxonomy
    MemoryType: "episodic"                    // coarse family
    Scope:      "project:nic/floop"           // or "universal"
    Content:    { Canonical, Summary, Tags }
    When:       { /* activation predicates */ }
    Provenance: {
        SourceType:     "consolidated",
        ConsolidatedBy: "claude-sonnet-4-6",
        SourceEvents:   ["evt_01J...", "evt_01J..."],
        Confidence:     0.85,
    }
}
```

Associative candidates promote to **edges** (not nodes) between existing behaviors, feeding the Hebbian co-activation system.

### Graceful Degradation

The consolidation pipeline degrades gracefully when components are unavailable:

| Missing Component | Impact | Fallback |
|---|---|---|
| **LanceDB (no CGO)** | Relate stage can't do ANN vector search | Falls back to `BruteForceIndex` (already implemented in PR #208's `lancedb_nocgo.go`). O(n) but functional. |
| **Embedder (no yzma/model)** | Can't embed memories for vector search or dedup | Relate stage uses tag-based Jaccard similarity and exact content matching only. No vector edges proposed. |
| **LLM API (v1 consolidator)** | Can't run LLM-based extraction/classification | Falls back to v0 `HeuristicConsolidator`. |
| **No `.floop/config.yaml`** | Can't determine project ID | All memories scoped as `"universal"`. Project-specific scoping unavailable. |

### Consolidation Executor

The pipeline interface is executor-agnostic:

```go
type Consolidator interface {
    Extract(ctx context.Context, events []Event) ([]Candidate, error)
    Classify(ctx context.Context, candidates []Candidate) ([]ClassifiedMemory, error)
    Relate(ctx context.Context, memories []ClassifiedMemory, store GraphStore) ([]Edge, []MergeProposal, error)
    Promote(ctx context.Context, memories []ClassifiedMemory, edges []Edge, merges []MergeProposal, store GraphStore) error
}
```

Implementations are swapped by phase:

| Phase | Executor | Description |
|-------|----------|-------------|
| v0 | `HeuristicConsolidator` | Rules + regex + embedding similarity. Ships immediately. |
| v1 | `LLMConsolidator` | Strong model (Sonnet/Opus) via API. High-quality. Generates training data. |
| v2 | `LocalModelConsolidator` | Distilled small model via yzma. No API dependency. |

### Trigger Modes

```bash
floop consolidate                    # manual, run now
floop consolidate --session latest   # just the most recent session
floop consolidate --since 24h        # everything from last 24 hours
floop consolidate --dry-run          # show what would be extracted, don't promote
```

Session hooks can trigger consolidation automatically post-session (opt-in via config).

The "nightly consolidation" is literal sleep consolidation: raw events accumulate during the day, the consolidator runs overnight, memories integrate into the graph, old events get pruned. The CLS metaphor is the architecture.

## Section 4: Expanded Behavior Data Model

### New Memory Types

```go
const (
    // Existing (semantic family)
    KindDirective   BehaviorKind = "directive"
    KindConstraint  BehaviorKind = "constraint"
    KindProcedure   BehaviorKind = "procedure"
    KindPreference  BehaviorKind = "preference"

    // New
    KindEpisodic    BehaviorKind = "episodic"
    KindWorkflow    BehaviorKind = "workflow"
)

const (
    MemoryTypeSemantic   = "semantic"    // directive, constraint, preference
    MemoryTypeEpisodic   = "episodic"    // episodic
    MemoryTypeProcedural = "procedural"  // procedure, workflow
)
```

### Type-Specific Data

```go
// Episodic-specific (nil for other types)
type EpisodeData struct {
    SessionID  string   `json:"session_id"`
    Timeframe  string   `json:"timeframe"`   // "2026-03-13 afternoon"
    Actors     []string `json:"actors"`       // ["user", "claude-sonnet-4-6"]
    Outcome    string   `json:"outcome"`      // "resolved" | "abandoned" | "escalated"
    Affect     string   `json:"affect"`       // "frustrating" | "breakthrough" | "routine"
}

// Workflow-specific (nil for other types)
type WorkflowData struct {
    Steps   []WorkflowStep `json:"steps"`
    Trigger string         `json:"trigger"`  // what kicks off this workflow
    Verified bool          `json:"verified"` // confirmed to work?
}

type WorkflowStep struct {
    Action    string `json:"action"`
    Condition string `json:"condition,omitempty"` // "if X" (empty = unconditional)
    OnFailure string `json:"on_failure,omitempty"` // "try Y" | "abort" | "skip"
}
```

### Tiering by Type

| Type | TierFull | TierSummary | TierNameOnly |
|------|----------|-------------|--------------|
| Directive | Full canonical text | One-line summary | Name |
| Constraint | Full text (min tier: Summary) | Summary (guaranteed) | *Never* |
| Episodic | Full episode with context | "When X happened, learned Y" | "Episode: X" |
| Workflow | All steps with conditions | Trigger + step count | "Workflow: X" |
| Procedure | Full steps | Abbreviated steps | "Procedure: X" |

### Decay Rates

Episodic memories decay faster than semantic ones (matching neuroscience):

| MemoryType | Default Decay | Rationale |
|-----------|--------------|-----------|
| Semantic | Very slow (months) | Facts and rules remain relevant long-term |
| Episodic | Fast (weeks) | "What happened Tuesday" loses relevance quickly |
| Procedural | Slow (months) | Workflows stay useful as long as the system hasn't changed |

Decay is counteracted by activation — an episodic memory that keeps getting retrieved persists. An episodic memory that's never relevant fades.

### Schema Changes

The behaviors table already has `scope TEXT DEFAULT 'local'` and `behavior_type TEXT` columns. The migration transforms existing values and adds new columns:

```sql
-- Existing columns: transform values
-- scope: 'local' → 'project:<id>' (from config), 'global' → 'universal'
UPDATE behaviors SET scope = 'universal' WHERE scope = 'global';
UPDATE behaviors SET scope = 'project:' || :project_id WHERE scope = 'local';

-- behavior_type: maps to memory_type (coarse family)
-- existing values (directive, constraint, procedure, preference) all map to 'semantic'
-- new values (episodic, workflow) will be set by consolidation pipeline
-- The existing behavior_type column continues to hold the fine-grained Kind
-- (directive, constraint, procedure, preference, + new: episodic, workflow).
-- memory_type is a new parallel column for the coarse grouping
ALTER TABLE behaviors ADD COLUMN memory_type TEXT DEFAULT 'semantic';

-- New type-specific columns
ALTER TABLE behaviors ADD COLUMN episode_data TEXT;    -- JSON, nullable
ALTER TABLE behaviors ADD COLUMN workflow_data TEXT;   -- JSON, nullable
```

## Section 5: Autoresearch Loop

### Purpose

Validate that the consolidation pipeline actually improves floop's effectiveness. Without measurement, we're guessing.

### Success Criteria

| Criterion | Metric | Measurement |
|-----------|--------|-------------|
| Retrieval quality | Precision@K, NDCG | Curated (context → expected behaviors) test scenarios |
| Capture completeness | Correction-before-needed rate | Did consolidator extract insights before user had to teach manually? |
| Emergent connections | Novel-edge utility | Do consolidator-created edges improve co-activation patterns? |
| Reduced noise | Signal-to-noise at injection | Despite more behaviors, does token budget stay tight? |

### Two-Tier Benchmarks

**Tier 1: Unit benchmarks** (fast, run after every consolidation)
- Curated (context → expected behaviors) pairs
- Run `floop active` against scenarios, score retrieval quality
- No agent involved — just retrieval evaluation
- Catches regressions immediately

**Tier 2: Integration benchmarks** (slow, run nightly/weekly)
- floop-bench SWE-bench harness
- Agent runs real tasks with/without consolidated behaviors
- Measures resolution rate, token efficiency, cost
- Full end-to-end validation

### Experiment Structure

```yaml
hypothesis: "Extracting procedural memories from debugging sessions
             improves resolution rate on similar bugs"
variable: consolidation.extract.procedural_threshold
baseline: 0.7
variants: [0.5, 0.6, 0.7, 0.8, 0.9]
tier1_gate: "retrieval_precision >= 0.75"
tier2_eval: "swebench_subset:debugging"
success: "resolution_rate > baseline + 0.05"
```

### Automation Levels

| Level | Automated | Manual | Phase |
|-------|-----------|--------|-------|
| Manual | Nothing | Everything | Now (floop-bench Runs 1-11) |
| Semi-auto | Tier 1 post-consolidation | Human reviews results | v0 |
| Auto + guardrails | Full loop overnight, proposes changes | Human approves | v1 |
| Full auto | Karpathy-style: hypothesize → test → keep/discard | Weekly summary review | v2 |

### floop-bench Evolution

```bash
# Today (manual)
floop-bench run --arm flash_floop --tasks eval_set

# Future (automated)
floop-research run --experiment consolidation_threshold \
  --variants 0.5,0.6,0.7,0.8,0.9 \
  --tier1-gate "precision >= 0.75" \
  --tier2-eval swebench_subset \
  --overnight
```

### Feedback Loop

Autoresearch results feed back into the consolidation model:

- **v1** (LLM consolidator): results inform prompt engineering
- **v2** (distilled model): results become training signal

## Section 6: Distillation Path

Each phase generates training data for the next. The system bootstraps itself.

```
v0 (heuristics)
  ├─ Ships immediately, uses existing infrastructure
  ├─ Every extraction logged with inputs + outputs
  ├─ Human corrections = negative examples
  ├─ floop_feedback confirmations = positive examples
  └─ Generates: noisy but real labeled data
        ↓
v1 (LLM consolidator)
  ├─ Strong model (Sonnet/Opus) via API
  ├─ Runs batch-style: end of session or nightly
  ├─ Every decision logged: "given events X, extracted Y, classified as Z"
  ├─ Autoresearch validates which decisions improved retrieval
  └─ Generates: clean, validated labeled data
        ↓
v2 (local model)
  ├─ Fine-tuned small model (1-3B, GGUF) or classification head on nomic embeddings
  ├─ Trained on v1's validated output
  ├─ Runs via yzma (already supports GGUF inference)
  ├─ Autoresearch validates parity with LLM consolidator
  └─ Outcome: fully local, no API dependency
```

## Section 7: Migration Path

### Principles

- **Additive, never destructive.** Existing data is never deleted or modified without explicit user action.
- **Backward compatible.** Old floop versions continue to work. New features are opt-in.
- **Graceful degradation.** Missing components (no embedder, no consolidator, no project config) reduce functionality but don't break anything.

### Schema Migration (automated)

On first run with new version:

1. Add new columns to behaviors table with safe defaults
2. Create events table (empty)
3. Existing behaviors unchanged — they gain optional fields

```
scope         → defaults to "universal"
memory_type   → defaults to "semantic"
episode_data  → defaults to NULL
workflow_data → defaults to NULL
```

Existing scope values are migrated:
- `constants.ScopeLocal` ("local") → `"project:<project-id>"` (read from `.floop/config.yaml`; if no config exists, falls back to `"universal"`)
- `constants.ScopeGlobal` ("global") → `"universal"`
- `constants.ScopeBoth` is a query filter, not a storage value — no migration needed

### Local-to-Global Store Merge (opt-in)

```bash
floop migrate --merge-local-to-global
```

1. Read all behaviors from `.floop/floop.db`
2. Stamp with `scope: "project:<project-id>"` (from config)
3. Insert into `~/.floop/floop.db` (skip duplicates by content hash)
4. Keep `.floop/floop.db` as read-only backup for 30 days
5. Update `.floop/config.yaml` to point to global store

Users who prefer dual stores can continue using them. New installations default to single-store.

### MCP Tool Evolution

| Tool | Change |
|------|--------|
| `floop_learn` | Unchanged. Consolidator catches what it misses. |
| `floop_active` | Unchanged interface. Queries expanded types behind the scenes. |
| `floop_feedback` | Unchanged. |
| `floop_list` | Add `--type` filter (semantic, episodic, procedural). |
| `floop_observe` | **New.** Fire-and-forget event ingestion. |
| `floop_consolidate` | **New.** Trigger consolidation or check status. |

### CLI Additions

```bash
floop ingest <file>                    # import transcript
floop consolidate [--dry-run]          # run consolidation
floop consolidate --status             # last consolidation stats
floop events [--session X]             # inspect raw buffer
floop events --prune --older 90d       # manual cleanup
floop migrate --merge-local-to-global  # opt-in store merge
```

### Rollback

If anything goes wrong:
- Original `.floop/floop.db` files are untouched until explicit merge
- New features are additive — disabling them means tables sit empty
- Floop works exactly as before without events or consolidation

## Scope Boundaries

### In Scope

- Raw event buffer + ingestion surface (hooks, CLI, MCP, stdin)
- Consolidation pipeline (extract → classify → relate → promote)
- Expanded memory taxonomy (episodic, workflow)
- Single-store architecture with project identity
- Autoresearch loop evolution (floop-bench → floop-research)
- Distillation path (heuristic → LLM → local model)
- Migration path from current architecture

### Out of Scope (Future Work)

- **Code indexing** — factual memory (what code IS) is a separate feature that rides the same LanceDB infrastructure. Phase 2 work.
- **Multi-user memory sharing** — multiple humans sharing a floop instance. Requires trust boundaries, access control, and conflict resolution. Multi-*agent* sharing is already the architecture (single global store, any agent reads/writes via MCP or CLI).
- **Real-time consolidation** — v0-v1 are batch/post-session. Streaming consolidation is v3+. See `docs/superpowers/specs/2026-03-14-future-vision.md`.
- **Custom model training infrastructure** — v2 distillation uses existing yzma. Custom training pipelines are separate tooling. See `docs/superpowers/specs/2026-03-14-future-vision.md`.

## Resolved Questions

1. **Consolidation prompt engineering** — Human-curated prompt engineering up to ~100 items, then hand off to autoresearch for iteration.
2. **Affect tagging** — Defer `EpisodeData.Affect` population to v1 (LLM consolidator), where classification is more reliable than heuristics.
3. **floop-bench agent cooperation** — floop-bench already supports targeting different models. Changes needed on the floop-bench side to support the autoresearch loop will be covered in a **separate spec**.
4. **Naming** — "consolidator" / `floop consolidate`. Explicit over implicit.
