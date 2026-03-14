# Memory Consolidation v0 — Implementation Brief

> Start here in a new session. This brief contains everything needed to begin implementation.

## What You're Building

A CLS-inspired (Complementary Learning Systems) memory consolidation system for floop. Raw conversation events get captured into an append-only buffer, then a heuristic pipeline extracts, classifies, and promotes them into the existing behavior graph as typed memories (semantic, episodic, procedural).

## Required Reading

Read these in order before touching code:

1. **Spec** — `docs/superpowers/specs/2026-03-13-memory-consolidation-design.md`
   - Full design with scientific lineage, architecture, data model, migration path
2. **Plan** — `docs/superpowers/plans/2026-03-14-memory-consolidation-v0.md`
   - 18 tasks across 4 chunks, TDD, frond stacking strategy, exact file paths and code
3. **Go Guidelines** — `docs/GO_GUIDELINES.md`
   - Coding standards for this project
4. **AGENTS.md** — project-level agent instructions (floop dogfooding, issue tracking, etc.)

## Prerequisites

- PR #208 (LanceDB, `feat/lancedb`) must be merged into `main` before starting
- If not yet merged, wait or coordinate with Nic

## Branch Setup

Use frond to create the stacked PR branches:

```bash
frond sync

frond new feat/consolidation/data-model --on main
frond new feat/consolidation/event-buffer --on feat/consolidation/data-model
frond new feat/consolidation/pipeline --on feat/consolidation/event-buffer
frond new feat/consolidation/cli-mcp --on feat/consolidation/pipeline
```

## Execution Order

| Order | Branch | Chunk | Tasks | What |
|-------|--------|-------|-------|------|
| 1 | `feat/consolidation/data-model` | Chunk 1 | 1-3 | BehaviorKind expansion, V9 schema migration, project identity |
| 2 | `feat/consolidation/event-buffer` | Chunk 2 | 4-6 | Event types, EventStore, transcript adapters |
| 3 | `feat/consolidation/pipeline` | Chunk 3 | 7-10 | Consolidator interface, heuristic implementation, runner |
| 4 | `feat/consolidation/cli-mcp` | Chunk 4 | 11-18 | CLI commands, MCP tools, tiering, config, integration test |

After each chunk: `frond push -t "<PR title>"` (titles are in the plan).

## Key Codebase Patterns

- **CLI commands**: See any `cmd/floop/cmd_*.go` — Cobra pattern with `newXxxCmd()` + `runXxx()` handler
- **Schema migrations**: `internal/store/schema.go` — sequential `migrateVxToVy()` in transactions
- **MCP tools**: `internal/mcp/server.go` `registerTools()` — tools registered with JSON schema
- **Store**: `internal/store/store.go` — `GraphStore` interface, `SQLiteGraphStore` implementation
- **Models**: `internal/models/behavior.go` — `Behavior`, `BehaviorKind`, `BehaviorContent`, `Provenance`

## Reviewer Notes

The plan reviewers flagged these items for awareness during implementation:

1. **Task 2 (V9 migration)**: `migrateSchema` signature changes cascade to `InitSchema`, `NewSQLiteGraphStore`, and all callers. The plan documents the chain but the implementor must thread the `projectID` parameter carefully.
2. **Task 6 (adapters)**: Markdown/JSONL/JSON adapter implementations are described structurally, not with full code. Write them from the test contracts.
3. **Task 9 (Relate/Promote)**: Higher-level descriptions than other tasks. The `ClassifiedMemory → Behavior` conversion has non-trivial field mapping — pay attention to Provenance construction.
4. **Tasks 11-18 (CLI/MCP)**: Follow established patterns in the codebase. Less prescriptive than earlier tasks because the patterns are well-established.

## Quality Gates

Before pushing each chunk:

```bash
go test -race ./...        # all tests pass, no races
go fmt ./...               # formatted
go build ./cmd/floop       # builds
```

## Session Completion

When done with implementation:
- Follow AGENTS.md session completion workflow
- Each chunk should have its own PR via frond
- Do not merge PRs — leave for Nic to review bottom-up
