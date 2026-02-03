# Feedback Loop - Implementation Plan

## Overview

This document tracks the implementation plan for `floop`, broken down into phases with concrete tasks and acceptance criteria.

## Current Status

**Phase**: Complete (Phases 1-5 done)
**Next**: Additional features (curation commands, package import/export)

## Phase 1: Foundation ✅ COMPLETE

### Task Order (with dependencies)

```
feedback-loop-mz2: Core data models
         ↓
feedback-loop-0k8: GraphStore + InMemoryGraphStore
         ↓
feedback-loop-0z3: CLI (version, init, list)
         ↓
feedback-loop-jq3: Minimal dogfooding skeleton (learn stub)
```

### 1. Core Data Models (`feedback-loop-mz2`)

**Files**:
- `internal/models/behavior.go`
- `internal/models/correction.go`
- `internal/models/context.go`
- `internal/models/provenance.go`

**Acceptance Criteria**:
- [x] `Behavior` struct with ID, Name, Kind, When, Content, Provenance, Confidence, Priority, relationships
- [x] `BehaviorKind` enum: directive, constraint, procedure, preference
- [x] `BehaviorContent` with Canonical, Expanded, Structured fields
- [x] `Correction` struct with context, agent action, human response, corrected action
- [x] `ContextSnapshot` with Matches() method for predicate evaluation
- [x] `Provenance` with SourceType enum (authored, learned, imported)
- [x] All structs have proper JSON/YAML tags

### 2. GraphStore Interface (`feedback-loop-0k8`)

**Files**:
- `internal/store/store.go`
- `internal/store/memory.go`
- `internal/store/memory_test.go`

**Acceptance Criteria**:
- [x] `GraphStore` interface with Node/Edge CRUD operations
- [x] `Node` and `Edge` types for graph storage
- [x] `Direction` type for edge traversal
- [x] `InMemoryGraphStore` implementation passing basic tests
- [x] Tests for: AddNode, GetNode, QueryNodes, AddEdge, GetEdges, Traverse

### 3. CLI Commands (`feedback-loop-0z3`)

**Files**:
- `cmd/floop/main.go`

**Acceptance Criteria**:
- [x] `floop version` prints version string
- [x] `floop init` creates `.floop/` directory with `manifest.yaml`
- [x] `floop list` shows all behaviors (empty initially)
- [x] `floop list --json` outputs JSON for agent consumption
- [x] Global `--json` flag support
- [x] Global `--root` flag for project root

**Success Test**:
```bash
go build ./cmd/floop && ./floop init && ./floop list --json
```

### 4. Dogfooding Skeleton (`feedback-loop-jq3`)

**Purpose**: Enable using floop during development before full implementation.

**Acceptance Criteria**:
- [x] `floop learn --wrong "X" --right "Y"` captures correction (stub - logs to file)
- [x] Can be invoked by agent during development
- [x] Foundation for iterative enhancement

---

## Phase 2: Learning Loop ✅ COMPLETE

**Tasks**:
- [x] `internal/learning/capture.go` - CorrectionCapture
- [x] `internal/learning/extract.go` - BehaviorExtractor
- [x] `internal/learning/place.go` - GraphPlacer
- [x] `internal/learning/loop.go` - LearningLoop orchestrator
- [x] `floop learn` command fully implemented
- [x] Behaviors cache for fast reads (corrections.jsonl → behaviors.json)

**Success Criteria**: ✅
```bash
floop learn --wrong "used pip" --right "use uv instead" --file "setup.py"
floop list  # Shows the learned behavior
```

---

## Phase 3: Activation ✅ COMPLETE

Dependencies: Phase 2 complete

**Tasks**:
- [x] `internal/activation/context.go` - ContextBuilder
- [x] `internal/activation/evaluate.go` - Predicate evaluation
- [x] `internal/activation/resolve.go` - Conflict resolution
- [x] `floop active`, `floop show`, `floop why` commands

**Success Criteria**: ✅
```bash
floop learn --wrong "used pip" --right "use uv instead" --file "setup.py"
floop active --file "setup.py"  # Shows the behavior (python context)
floop active --file "main.go"   # No behaviors (go context doesn't match)
floop why behavior-xxx --file "setup.py"  # Explains why active
```

---

## Phase 4: Persistence ✅ COMPLETE

Dependencies: Phase 3 complete

**Tasks**:
- [x] `internal/store/file.go` - FileGraphStore implementation
- [x] `internal/store/file_test.go` - Test coverage (85.4%)
- [x] Switch CLI from InMemoryGraphStore to FileGraphStore
- [x] Behaviors persist to `.floop/nodes.jsonl`
- [x] Edges persist to `.floop/edges.jsonl`

**Success Criteria**: ✅
```bash
floop learn --wrong "used print" --right "use logger" --file "test.py"
floop list  # Shows the learned behavior
# Restart CLI - behavior still exists
floop list  # Still shows the learned behavior
floop active --file "test.py"  # Shows behavior (python context)
```

---

## Phase 5: Assembly ✅ COMPLETE

Dependencies: Phase 4 complete

**Tasks**:
- [x] `internal/assembly/compile.go` - BehaviorCompiler with markdown/xml/plain formats
- [x] `internal/assembly/optimize.go` - TokenOptimizer with priority-based inclusion
- [x] `floop prompt` command - Generate agent prompt section

**Success Criteria**: ✅
```bash
floop learn --wrong "used pip" --right "use uv instead" --file "test.py"
floop prompt --file "test.py"           # Outputs markdown prompt section
floop prompt --file "test.py" --format xml  # XML format
floop prompt --file "test.py" --max-tokens 500  # Token-limited output
floop prompt --file "test.py" --json    # JSON for agent consumption
```

---

## Notes

- **Dogfooding**: We use floop while building floop
- **Beads**: All work tracked via `bd` commands
- **Iteration**: Each phase builds on the previous, tested incrementally
