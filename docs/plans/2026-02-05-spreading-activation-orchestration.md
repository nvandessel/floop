# Spreading Activation Memory System — Orchestration Plan

## Prerequisites

### Permission Setup (CRITICAL)
Before starting, ensure your permission mode allows subagents to complete work without interactive prompts. Background subagents CANNOT request permissions — they will fail silently with "Permission auto-denied (prompts unavailable)".

**Required permissions for subagents:**
- `Read` — reading source files
- `Write` — creating new files
- `Edit` — modifying existing files
- `Bash` — running `go test`, `go fmt`, `go build`, `bd` commands
- `Glob` / `Grep` — searching codebase

**Recommended:** Set permission mode to auto-allow for this session, or pre-approve the tools above. Verify by running a test subagent before starting real work.

### Environment Verification
```bash
# Verify toolchain
go version          # Must be 1.25+
go test ./...       # All tests must pass before starting
go fmt ./...        # Must produce no changes
bd show feedback-loop-chy  # Epic must exist with 9 children
```

---

## Architecture Overview

You are orchestrating the implementation of a spreading activation memory system for floop. The work is organized as epic `feedback-loop-chy` with 9 subtasks in a dependency tree.

### Dependency Tree
```
Wave 1:  .1 Schema v3 (edge weights + temporal metadata)
              │
Wave 2:  .2 Core spreading activation engine
              │
              ├─────────────┬──────────────┬──────────────┐
Wave 3:  .3 Seeds     .6 Multi-res    .7 Inhibition  .8 Hybrid scoring
              │
Wave 4:  .4 Session state
              │
              ├──────────────┐
Wave 5:  .5 Dynamic hooks  .9 Reinforcement (.4 + .6)
```

### Worktree Strategy
Each wave uses isolated git worktrees in `.worktrees/` at repo root. Each subagent works in its own worktree on its own branch. After each wave, the orchestrator merges completed branches into `main` before the next wave starts.

---

## Orchestration Prompt

Use this prompt when spawning the orchestration agent:

```
You are an orchestration agent implementing the Spreading Activation Memory System for the floop project at /home/nvandessel/repos/feedback-loop.

## Your Role
You coordinate implementation across 5 sequential waves, spawning subagents for parallel work within each wave. You are responsible for:
1. Creating git worktrees for each task
2. Spawning subagents with complete context (subagents own their bead lifecycle)
3. Reviewing completed work
4. Merging branches back to main (merge carries bead state with it)
5. Closing the epic and syncing beads at the end

## Bead Lifecycle (IMPORTANT)
Subagents own claiming and closing their beads, but do NOT commit `.beads/`:
- Subagent CLAIMS the bead (`bd update <id> --status in_progress`) as their FIRST action
- Subagent CLOSES the bead (`bd close <id> --reason "..."`) after tests pass
- Subagent commits CODE ONLY — do NOT include `.beads/` in commits
- WHY: `issues.jsonl` is a single file containing ALL issues. Parallel subagents modifying it = guaranteed merge conflicts.
- After merging all code for a wave, the orchestrator runs `bd sync` on main to re-export a clean snapshot from the DB, then commits `.beads/` once per wave.
- The DB (beads.db) is the source of truth; the JSONL export is just a git-friendly view.

## CRITICAL RULES
- NEVER start a wave until all tasks from the previous wave are merged into main
- ALWAYS run `go test ./...` on main after merging each wave
- ALWAYS run code review (superpowers:code-reviewer) after each wave
- If a subagent fails, investigate the failure, fix it yourself, then continue
- All work happens in .worktrees/ directory (e.g., .worktrees/chy-1-schema-v3)

## Project Context
- Language: Go 1.25+
- Read docs/GO_GUIDELINES.md before starting
- Read AGENTS.md for project conventions
- Run `bd show feedback-loop-chy` to see the full epic
- All code conventions: table-driven tests, error wrapping with %w, cobra CLI patterns

## Wave Execution Protocol

For EACH wave, follow this exact sequence:

### Step 1: Pre-flight
```bash
# Ensure main is clean
git checkout main
git pull --rebase
go test ./...        # Must pass
go fmt ./...         # Must be clean
```

### Step 2: Create worktrees
For each task in the wave:
```bash
git worktree add .worktrees/<short-name> -b feat/chy-<N>-<short-name>
```

### Step 3: Spawn subagents
For parallel tasks, spawn all subagents simultaneously. Each subagent gets:
1. The FULL ticket description (from `bd show <task-id>`)
2. The worktree path to work in
3. Instructions to claim the bead, do the work, close the bead, and commit everything
4. Instructions to run tests before finishing

### Step 4: Review completed work
After all subagents complete:
1. For each worktree, review the diff: `git -C .worktrees/<name> diff main`
2. Run tests in each worktree: `cd .worktrees/<name> && go test ./...`
3. Use superpowers:code-reviewer for non-trivial changes
4. Fix any issues yourself if needed

### Step 5: Merge to main
For each completed task (bead state is already closed in the branch):
```bash
git checkout main
git merge --no-ff feat/chy-<N>-<short-name> -m "feat(spreading): <description>"
```

### Step 6: Post-merge verification
```bash
go test ./...        # ALL tests must pass on main
go fmt ./...         # Must be clean
```

### Step 7: Sync beads and commit
After all code is merged, export clean bead state from the DB:
```bash
bd sync
git add .beads/
git commit -m "chore(beads): sync state for wave N tasks"
```

### Step 8: Clean up worktrees
```bash
git worktree remove .worktrees/<short-name>
```

---

## Wave 1: Schema Foundation (Sequential — 1 task)

### Task: feedback-loop-chy.1 — Edge weights + temporal metadata

**Worktree:** `.worktrees/chy-1-schema`
**Branch:** `feat/chy-1-schema-v3`

**Subagent prompt:**
```
You are implementing schema v3 for the floop project.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-1-schema

## First: Claim the bead
```bash
bd update feedback-loop-chy.1 --status in_progress
```

## Task
Read the full task specification:
```bash
bd show feedback-loop-chy.1
```

## Key files to modify:
- internal/store/store.go — Add Weight, CreatedAt, LastActivated to Edge struct
- internal/store/schema.go — Bump SchemaVersion to 3, add migration v2→v3, update base schema
- internal/store/sqlite.go — Update AddEdge, GetEdges to read/write new columns, update JSONL export/import
- internal/store/memory.go — Update AddEdge to validate that new Edge fields are set (no hidden defaults)
- internal/ranking/decay.go — **EXISTING FILE** with ExponentialDecay, LinearDecay, StepDecay, BoostedDecay. ADD EdgeDecay function to this file; do NOT replace it.

## Key files to create:
- internal/ranking/decay_test.go — New test file (decay.go currently has no tests)

## Tests to add:
- internal/store/sqlite_test.go — TestSQLiteGraphStore_EdgeWeights, TestSQLiteGraphStore_SchemaV3Migration, TestSQLiteGraphStore_EdgeJSONLRoundTrip
- internal/ranking/decay_test.go — TestEdgeDecay (and consider tests for existing decay functions)

## Edge field defaults — CALLER RESPONSIBILITY:
- Do NOT set hidden defaults in AddEdge (neither SQLite nor InMemory stores)
- Callers must explicitly set Weight, CreatedAt when creating edges
- The spreading engine should validate/reject edges with zero Weight rather than silently substituting defaults
- This prevents stale data from accumulating with values that never get updated

## Coding standards:
- Read docs/GO_GUIDELINES.md first
- Table-driven tests with t.Run()
- Error wrapping: fmt.Errorf("context: %w", err)
- Use sql.NullString / sql.NullFloat64 for nullable columns
- Follow existing migration pattern in migrateV1ToV2

## Verification:
Before committing, run:
```bash
go test ./internal/store/... ./internal/ranking/...
go fmt ./...
go build ./cmd/floop
```

## Close bead and commit:
```bash
bd close feedback-loop-chy.1 --reason "Schema v3 implemented: edge weights, temporal metadata, migration, decay function. Tests pass."
git add -A ':!.beads' && git commit -m "feat(store): add edge weights and temporal decay for spreading activation"
```
```

**After merge:** Verify `go test ./...` passes on main.

---

## Wave 2: Core Engine (Sequential — 1 task)

### Task: feedback-loop-chy.2 — Spreading activation engine

**Worktree:** `.worktrees/chy-2-engine`
**Branch:** `feat/chy-2-activation-engine`

**Subagent prompt:**
```
You are implementing the core spreading activation engine for floop.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-2-engine

## First: Claim the bead
```bash
bd update feedback-loop-chy.2 --status in_progress
```

## Task
Read the full task specification:
```bash
bd show feedback-loop-chy.2
```

## Key files to create:
- internal/spreading/engine.go — Engine struct, Config, Seed, Result types, Activate method
- internal/spreading/engine_test.go — 11+ test cases covering propagation, decay, fan-out, cycles, sigmoid

## Integration points:
- Uses store.GraphStore.GetEdges() for graph traversal
- Uses store.Edge.Weight, LastActivated fields (from wave 1)
- Uses ranking.EdgeDecay() for temporal decay (from wave 1)

## Algorithm summary:
1. Seed initialization (activation map from seeds)
2. Propagation loop (T=3 iterations, fan-out normalization, max not sum)
3. Sigmoid squashing
4. Filter by MinActivation threshold
5. Sort by activation descending

## Coding standards:
- Read docs/GO_GUIDELINES.md first
- Package name: spreading
- Use store.NewInMemoryGraphStore() for tests
- Table-driven tests
- Keep engine stateless — all state in activation maps during Activate()

## Verification:
```bash
go test ./internal/spreading/...
go fmt ./...
go build ./cmd/floop
```

## Close bead and commit:
```bash
bd close feedback-loop-chy.2 --reason "Core spreading activation engine implemented with propagation, decay, fan-out normalization, sigmoid squashing. Tests pass."
git add -A ':!.beads' && git commit -m "feat(spreading): implement core spreading activation engine"
```
```

**After merge:** Verify `go test ./...` passes on main.

---

## Wave 3: Parallel Expansion (4 tasks in parallel)

Create 4 worktrees and spawn 4 subagents simultaneously.

### Task .3: Seed selection
**Worktree:** `.worktrees/chy-3-seeds`
**Branch:** `feat/chy-3-seed-selection`

**Subagent prompt:**
```
You are implementing seed node selection for the floop spreading activation system.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-3-seeds

First: `bd update feedback-loop-chy.3 --status in_progress`
Read the full task: `bd show feedback-loop-chy.3`

Create:
- internal/spreading/seeds.go — SeedSelector, NewSeedSelector, SelectSeeds
- internal/spreading/seeds_test.go — 6 test cases
- internal/spreading/pipeline.go — Pipeline orchestrating selector → engine
- internal/spreading/pipeline_test.go — end-to-end integration test

Uses existing: activation.NewEvaluator(), learning.NodeToBehavior()
Uses from wave 2: spreading.Engine, spreading.Seed, spreading.Result

Verify: `go test ./internal/spreading/... && go fmt ./...`
Close bead: `bd close feedback-loop-chy.3 --reason "Seed selection and activation pipeline implemented. Tests pass."`
Commit (code only, NOT .beads/): `git add -A ':!.beads' && git commit -m "feat(spreading): implement seed selection and activation pipeline"`
```

### Task .6: Multi-resolution output
**Worktree:** `.worktrees/chy-6-tiers`
**Branch:** `feat/chy-6-multi-resolution`

**Subagent prompt:**
```
You are implementing activation-distance-based tiering for floop.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-6-tiers

First: `bd update feedback-loop-chy.6 --status in_progress`
Read the full task: `bd show feedback-loop-chy.6`

IMPORTANT: The `internal/tiering/` package ALREADY EXISTS with a `TierAssigner` (in assigner.go) that assigns tiers based on token budget and relevance scoring. Your `ActivationTierMapper` is a DIFFERENT concept — it maps spreading activation distances to tiers. These two coexist: ActivationTierMapper suggests tiers based on activation distance, TierAssigner adjusts based on token budget. Read the existing code before writing.

Create:
- internal/tiering/activation_tiers.go — ActivationTierMapper, MapTier, MapResults
- internal/tiering/activation_tiers_test.go — threshold, demotion, constraint tests

Modify:
- internal/models/injection.go — Add TierNameOnly constant (existing constants: TierFull, TierSummary, TierOmitted)
- internal/assembly/compile.go — Handle TierNameOnly in CompileTiered (name + tags format)

Uses from wave 2: spreading.Result type

Verify: `go test ./internal/tiering/... ./internal/assembly/... ./internal/models/... && go fmt ./...`
Close bead: `bd close feedback-loop-chy.6 --reason "Activation-distance tiering implemented with TierNameOnly support. Tests pass."`
Commit (code only, NOT .beads/): `git add -A ':!.beads' && git commit -m "feat(tiering): add activation-distance-based multi-resolution output"`
```

### Task .7: Lateral inhibition
**Worktree:** `.worktrees/chy-7-inhibition`
**Branch:** `feat/chy-7-lateral-inhibition`

**Subagent prompt:**
```
You are implementing lateral inhibition for the floop spreading activation engine.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-7-inhibition

First: `bd update feedback-loop-chy.7 --status in_progress`
Read the full task: `bd show feedback-loop-chy.7`

Create:
- internal/spreading/inhibition.go — InhibitionConfig, ApplyInhibition (pure function)
- internal/spreading/inhibition_test.go — 7 test cases

Modify:
- internal/spreading/engine.go — Add InhibitionConfig to Config, integrate into Activate() pipeline between propagation and sigmoid squashing

IMPORTANT: The engine.go file was created in wave 2. Your worktree has it merged from main. Add the Inhibition field to Config and call ApplyInhibition in the Activate method.

Verify: `go test ./internal/spreading/... && go fmt ./...`
Close bead: `bd close feedback-loop-chy.7 --reason "Lateral inhibition implemented and integrated into engine. Tests pass."`
Commit (code only, NOT .beads/): `git add -A ':!.beads' && git commit -m "feat(spreading): add lateral inhibition for activation focus"`
```

### Task .8: Hybrid scoring
**Worktree:** `.worktrees/chy-8-scoring`
**Branch:** `feat/chy-8-hybrid-scoring`

**Subagent prompt:**
```
You are implementing hybrid scoring (context + activation + PageRank) for floop.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-8-scoring

First: `bd update feedback-loop-chy.8 --status in_progress`
Read the full task: `bd show feedback-loop-chy.8`

Create:
- internal/ranking/pagerank.go — ComputePageRank using power iteration
- internal/ranking/pagerank_test.go — 6 test cases
- internal/ranking/hybrid.go — HybridScorer combining 3 signals
- internal/ranking/hybrid_test.go — weighted combination and batch tests

Uses existing: ranking.RelevanceScorer (for context signal)
Uses: store.GraphStore for PageRank computation

Verify: `go test ./internal/ranking/... && go fmt ./...`
Close bead: `bd close feedback-loop-chy.8 --reason "PageRank and hybrid scoring implemented. Tests pass."`
Commit (code only, NOT .beads/): `git add -A ':!.beads' && git commit -m "feat(ranking): add PageRank and hybrid scoring for spreading activation"`
```

**Wave 3 merge order (IMPORTANT):**
Merge in this order to minimize conflicts:
1. `.3` (seeds) — creates new files in `internal/spreading/`, no overlap
2. `.6` (tiers) — creates new files in `internal/tiering/`, modifies `models/injection.go` and `assembly/compile.go`
3. `.8` (scoring) — creates new files in `internal/ranking/`, no overlap
4. `.7` (inhibition) — **LAST** because it modifies `internal/spreading/engine.go` (created in wave 2). Merging .7 last ensures its engine.go changes are the final version on main.

Run `go test ./...` on main after each merge. Run code review after all 4 are merged.

---

## Wave 4: Session State (Sequential — 1 task)

### Task: feedback-loop-chy.4 — Session state tracking

**Worktree:** `.worktrees/chy-4-session`
**Branch:** `feat/chy-4-session-state`

**Subagent prompt:**
```
You are implementing session state tracking for floop injection management.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-4-session

First: `bd update feedback-loop-chy.4 --status in_progress`
Read the full task: `bd show feedback-loop-chy.4`

Create:
- internal/session/state.go — State struct, InjectionRecord, ShouldInject, FilterResults
- internal/session/state_test.go — 9 test cases including thread safety
- internal/session/file.go — LoadState, SaveState for CLI hook persistence

Modify:
- internal/mcp/server.go — Add session *session.State to Server struct, initialize in NewServer

IMPORTANT: Thread safety is critical. Use sync.RWMutex. Run tests with `-race` flag.

Verify: `go test -race ./internal/session/... ./internal/mcp/... && go fmt ./...`
Close bead: `bd close feedback-loop-chy.4 --reason "Session state tracking with thread-safe injection management. Race-free tests pass."`
Commit (code only, NOT .beads/): `git add -A ':!.beads' && git commit -m "feat(session): add session state tracking for injection management"`
```

---

## Wave 5: Dynamic Integration (2 tasks in parallel)

### Task .5: Dynamic hooks
**Worktree:** `.worktrees/chy-5-hooks`
**Branch:** `feat/chy-5-dynamic-hooks`

**Subagent prompt:**
```
You are implementing hook-based dynamic context detection for floop.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-5-hooks

First: `bd update feedback-loop-chy.5 --status in_progress`
Read the full task: `bd show feedback-loop-chy.5`

Create:
- .claude/hooks/dynamic-context.sh — Hook script for PreToolUse (Read, Bash)
- cmd/floop/activate.go — New 'activate' CLI command

Modify:
- .claude/settings.json — Add PreToolUse hooks for Read and Bash matchers
- internal/hooks/claude.go — Update GenerateHookConfig: currently only adds a "Read" matcher with `floop prompt --format markdown`. EXTEND (not replace) to add a "Bash" matcher for the new `floop activate` command. Read the existing GenerateHookConfig implementation before modifying.
- cmd/floop/main.go — Register activate command

Uses from wave 3: spreading.Pipeline
Uses from wave 4: session.State, session.LoadState, session.SaveState

NOTE: The existing hook system uses `floop prompt --format markdown` for static behavior injection on Read events. The new dynamic-context.sh hook is a DIFFERENT mechanism — it runs `floop activate` on file/command events to trigger spreading activation dynamically. Both hooks coexist.

Make the hook script executable: chmod +x .claude/hooks/dynamic-context.sh

Verify: `go build ./cmd/floop && go test ./internal/hooks/... && go fmt ./...`
Close bead: `bd close feedback-loop-chy.5 --reason "Dynamic hooks and activate CLI command implemented. Tests pass."`
Commit (code only, NOT .beads/): `git add -A ':!.beads' && git commit -m "feat(hooks): add dynamic context detection and activation command"`
```

### Task .9: Reinforcement and coalescing
**Worktree:** `.worktrees/chy-9-reinforce`
**Branch:** `feat/chy-9-reinforcement`

**Subagent prompt:**
```
You are implementing smart reinforcement and coalescing logic for floop.

Working directory: /home/nvandessel/repos/feedback-loop/.worktrees/chy-9-reinforce

First: `bd update feedback-loop-chy.9 --status in_progress`
Read the full task: `bd show feedback-loop-chy.9`

Create:
- internal/session/reinforcement.go — ReinforcementConfig, ShouldReinforce
- internal/session/reinforcement_test.go — 8 test cases
- internal/assembly/coalesce.go — Coalescer, BehaviorCluster, Coalesce
- internal/assembly/coalesce_test.go — 6 test cases

Modify:
- internal/assembly/compile.go — Add CompileCoalesced method

Uses from wave 4: session.InjectionRecord, session.InjectionTier
Uses from wave 3 (.6): models.InjectionTier (including TierNameOnly)

Verify: `go test ./internal/session/... ./internal/assembly/... && go fmt ./...`
Close bead: `bd close feedback-loop-chy.9 --reason "Smart reinforcement and behavior coalescing implemented. Tests pass."`
Commit (code only, NOT .beads/): `git add -A ':!.beads' && git commit -m "feat(session): add smart reinforcement and behavior coalescing"`
```

---

## Final Wrap-Up Protocol

After all 5 waves are merged and verified:

### 1. Final verification
```bash
git checkout main
go test ./...          # ALL tests must pass
go fmt ./...           # Must be clean
go build ./cmd/floop   # Must build
go vet ./...           # No warnings
```

### 2. Close epic and sync
Individual task beads are already closed (subagents closed them, merges carried the state to main).
Only the parent epic needs closing:
```bash
bd close feedback-loop-chy --reason "All 9 subtasks implemented and merged. Spreading activation memory system complete with: schema v3, activation engine, seed selection, session state, dynamic hooks, multi-resolution output, lateral inhibition, hybrid scoring, and smart reinforcement."
bd sync
```

### 3. Commit and push
```bash
git add .beads/
git commit -m "chore(beads): close spreading activation epic feedback-loop-chy"
git pull --rebase
git push
git status  # Must show "up to date with origin"
```

### 4. Clean up worktrees
```bash
# Remove all worktrees (should already be done per-wave, but verify)
git worktree list
# Remove any remaining:
# git worktree remove .worktrees/<name>
# Prune stale worktree references:
git worktree prune
```

### 5. Verify branches merged
```bash
git branch --merged main | grep feat/chy
# All feat/chy-* branches should appear
# Delete merged branches:
git branch -d feat/chy-1-schema-v3 feat/chy-2-activation-engine ...
```

---

## Error Recovery

### Subagent fails with permission error
The subagent cannot request interactive permissions. Options:
1. Set permission mode to auto-allow before starting
2. Run the failing task yourself (foreground) instead of as a subagent
3. Check that the subagent's worktree path is inside the repo root

### Merge conflict
1. Resolve manually on main
2. Re-run `go test ./...` after resolution
3. Continue with next wave

### Test failure after merge
1. Identify which wave's code caused the failure
2. Fix on main directly
3. Commit as `fix(spreading): resolve <issue>`
4. Do NOT revert the merge — fix forward

### Subagent produces incorrect implementation
1. Review the diff carefully
2. Fix in the worktree before merging: `cd .worktrees/<name> && <fix> && git add -A && git commit --amend`
3. Or fix on main after merge
4. If the subagent closed the bead prematurely and the fix is non-trivial, reopen it: `bd update <task-id> --status in_progress`, fix, then close again

---

## Checklist

- [ ] Wave 1: .1 claimed → implemented → reviewed → merged → closed
- [ ] Wave 2: .2 claimed → implemented → reviewed → merged → closed
- [ ] Wave 3: .3, .6, .7, .8 claimed → implemented → reviewed → merged → closed
- [ ] Wave 4: .4 claimed → implemented → reviewed → merged → closed
- [ ] Wave 5: .5, .9 claimed → implemented → reviewed → merged → closed
- [ ] All tests pass on main
- [ ] Epic feedback-loop-chy closed
- [ ] Beads synced
- [ ] All changes pushed to remote
- [ ] All worktrees cleaned up
- [ ] All feature branches deleted
