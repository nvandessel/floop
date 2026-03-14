# Memory Consolidation v0 Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the v0 (heuristic) memory consolidation system — raw event buffer, ingestion surface, expanded memory types, single-store migration, and heuristic consolidator.

**Architecture:** CLS-inspired two-phase memory: a fast append-only event buffer captures raw conversation data, then a heuristic consolidation pipeline (Extract → Classify → Relate → Promote) processes events into typed behaviors in the existing graph store. Single global store replaces dual local/global stores, with project identity via namespace/name IDs.

**Tech Stack:** Go 1.25+, SQLite (existing), LanceDB (PR #208), Cobra CLI, MCP go-sdk, nomic-embed-text via yzma (existing)

**Spec:** `docs/superpowers/specs/2026-03-13-memory-consolidation-design.md`

**Prerequisite:** PR #208 (LanceDB) must be merged. Branch from `main` post-merge or from `feat/lancedb`.

---

## Branch Strategy (frond stacked PRs)

Each chunk becomes a stacked branch for incremental review. Review bottom-up; once all PRs merge, the full feature lands.

```
main (post PR #208 merge)
  └── feat/consolidation/data-model       ← Chunk 1 (Tasks 1-3)
       └── feat/consolidation/event-buffer ← Chunk 2 (Tasks 4-6)
            └── feat/consolidation/pipeline ← Chunk 3 (Tasks 7-10)
                 └── feat/consolidation/cli-mcp ← Chunk 4 (Tasks 11-18)
```

### Setup (run once at start of implementation)

```bash
frond sync

# Create the stack — each branch targets the one below it
frond new feat/consolidation/data-model --on main
frond new feat/consolidation/event-buffer --on feat/consolidation/data-model
frond new feat/consolidation/pipeline --on feat/consolidation/event-buffer
frond new feat/consolidation/cli-mcp --on feat/consolidation/pipeline
```

### Per-chunk workflow

```bash
# Switch to chunk's branch
git checkout feat/consolidation/<chunk>

# Do the work (Tasks N-M), commit as you go

# Push and create PR when chunk is complete
frond push -t "feat: <chunk description>"

# Move to next chunk
git checkout feat/consolidation/<next-chunk>
```

### PR titles

| Branch | PR Title |
|--------|----------|
| `feat/consolidation/data-model` | `feat: expand behavior model with episodic/workflow types and V9 schema migration` |
| `feat/consolidation/event-buffer` | `feat: raw event buffer — storage, transcript adapters, ingestion surface` |
| `feat/consolidation/pipeline` | `feat: heuristic consolidation pipeline (extract → classify → relate → promote)` |
| `feat/consolidation/cli-mcp` | `feat: consolidate/ingest/events/migrate CLI commands and MCP tools` |

### Review order

1. Review `data-model` PR first (smallest, foundation)
2. Review `event-buffer` (builds on data-model)
3. Review `pipeline` (builds on event-buffer)
4. Review `cli-mcp` (wires everything together)
5. Merge bottom-up: data-model → event-buffer → pipeline → cli-mcp

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/events/event.go` | Event, EventProvenance, EventActor, EventKind types |
| `internal/events/store.go` | EventStore interface + SQLite implementation (CRUD, query, prune) |
| `internal/events/store_test.go` | EventStore tests |
| `internal/events/adapter.go` | TranscriptAdapter interface |
| `internal/events/adapter_markdown.go` | Markdown/plaintext transcript parser |
| `internal/events/adapter_markdown_test.go` | Markdown adapter tests |
| `internal/events/adapter_jsonl.go` | Claude Code JSONL transcript parser |
| `internal/events/adapter_jsonl_test.go` | JSONL adapter tests |
| `internal/events/adapter_json.go` | Generic JSON event parser |
| `internal/events/adapter_json_test.go` | JSON adapter tests |
| `internal/consolidation/consolidator.go` | Consolidator interface + pipeline types (Candidate, ClassifiedMemory, MergeProposal) |
| `internal/consolidation/heuristic.go` | HeuristicConsolidator implementation |
| `internal/consolidation/heuristic_test.go` | HeuristicConsolidator tests |
| `internal/consolidation/runner.go` | Pipeline orchestrator (wires Extract→Classify→Relate→Promote, handles dry-run) |
| `internal/consolidation/runner_test.go` | Runner tests |
| `cmd/floop/cmd_ingest.go` | `floop ingest` CLI command |
| `cmd/floop/cmd_ingest_test.go` | Ingest CLI tests |
| `cmd/floop/cmd_consolidate.go` | `floop consolidate` CLI command |
| `cmd/floop/cmd_consolidate_test.go` | Consolidate CLI tests |
| `cmd/floop/cmd_events.go` | `floop events` CLI command |
| `cmd/floop/cmd_events_test.go` | Events CLI tests |
| `cmd/floop/cmd_migrate.go` | `floop migrate` CLI command |
| `cmd/floop/cmd_migrate_test.go` | Migrate CLI tests |
| `internal/project/identity.go` | Project ID resolution (walk up dirs, read config) |
| `internal/project/identity_test.go` | Project identity tests |

### Modified Files

| File | Change |
|------|--------|
| `internal/models/behavior.go` | Add `KindEpisodic`, `KindWorkflow`, `MemoryType` constants, `EpisodeData`, `WorkflowData` structs |
| `internal/models/provenance.go` | Add `SourceTypeConsolidated`, new consolidation provenance fields |
| `internal/store/schema.go` | Add V9 migration (events table, memory_type column, scope value transforms) |
| `internal/store/sqlite.go` | Event storage methods (EventStore interface) |
| `internal/config/config.go` | Add `ConsolidationConfig`, `ProjectConfig`, `EventsConfig` sections |
| `internal/mcp/server.go` | Register `floop_observe` and `floop_consolidate` tools |
| `internal/mcp/handler_observe.go` | New: `floop_observe` MCP handler |
| `internal/mcp/handler_consolidate.go` | New: `floop_consolidate` MCP handler |
| `internal/tiering/activation_tiers.go` | Handle episodic/workflow tiers |
| `cmd/floop/main.go` | Register new CLI commands |

---

## Chunk 1: Data Model Expansion & Schema Migration

**Branch:** `feat/consolidation/data-model` (on `main`)

### Task 1: Expand BehaviorKind and Add Memory Types

**Files:**
- Modify: `internal/models/behavior.go`
- Modify: `internal/models/provenance.go`

- [ ] **Step 1: Write test for new behavior kinds**

In `internal/models/behavior_test.go`, add:

```go
func TestNewBehaviorKinds(t *testing.T) {
	tests := []struct {
		kind     BehaviorKind
		wantType string
	}{
		{BehaviorKindDirective, MemoryTypeSemantic},
		{BehaviorKindConstraint, MemoryTypeSemantic},
		{BehaviorKindPreference, MemoryTypeSemantic},
		{BehaviorKindProcedure, MemoryTypeProcedural},
		{BehaviorKindEpisodic, MemoryTypeEpisodic},
		{BehaviorKindWorkflow, MemoryTypeProcedural},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got := MemoryTypeForKind(tt.kind)
			if got != tt.wantType {
				t.Errorf("MemoryTypeForKind(%s) = %s, want %s", tt.kind, got, tt.wantType)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models/ -run TestNewBehaviorKinds -v`
Expected: FAIL — `BehaviorKindEpisodic` and `MemoryTypeForKind` undefined

- [ ] **Step 3: Add new kinds, memory types, and type-specific data to models**

In `internal/models/behavior.go`, add:

```go
const (
	BehaviorKindEpisodic BehaviorKind = "episodic"
	BehaviorKindWorkflow BehaviorKind = "workflow"
)

const (
	MemoryTypeSemantic   = "semantic"
	MemoryTypeEpisodic   = "episodic"
	MemoryTypeProcedural = "procedural"
)

// MemoryTypeForKind returns the coarse memory family for a behavior kind.
func MemoryTypeForKind(kind BehaviorKind) string {
	switch kind {
	case BehaviorKindEpisodic:
		return MemoryTypeEpisodic
	case BehaviorKindProcedure, BehaviorKindWorkflow:
		return MemoryTypeProcedural
	default:
		return MemoryTypeSemantic
	}
}

// EpisodeData holds episodic memory context (nil for non-episodic behaviors).
type EpisodeData struct {
	SessionID string   `json:"session_id"`
	Timeframe string   `json:"timeframe"`
	Actors    []string `json:"actors"`
	Outcome   string   `json:"outcome"`
}

// WorkflowData holds branching workflow definitions (nil for non-workflow behaviors).
type WorkflowData struct {
	Steps    []WorkflowStep `json:"steps"`
	Trigger  string         `json:"trigger"`
	Verified bool           `json:"verified"`
}

type WorkflowStep struct {
	Action    string `json:"action"`
	Condition string `json:"condition,omitempty"`
	OnFailure string `json:"on_failure,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/models/ -run TestNewBehaviorKinds -v`
Expected: PASS

- [ ] **Step 5: Add SourceTypeConsolidated and new provenance fields**

In `internal/models/provenance.go`, add:

```go
const (
	SourceTypeConsolidated SourceType = "consolidated"
)
```

Add new fields to the `Provenance` struct:

```go
// Consolidation lineage
ConsolidatedBy string    `json:"consolidated_by,omitempty"`
ConsolidatedAt time.Time `json:"consolidated_at,omitempty"`
SourceEvents   []string  `json:"source_events,omitempty"`
Confidence     float64   `json:"confidence,omitempty"`

// Agent provenance (optional)
SourceModel   string `json:"source_model,omitempty"`
SourceAgent   string `json:"source_agent,omitempty"`
SourceProject string `json:"source_project,omitempty"`
SourceBranch  string `json:"source_branch,omitempty"`
```

- [ ] **Step 6: Run full model tests**

Run: `go test ./internal/models/ -v`
Expected: PASS — no existing tests broken

- [ ] **Step 7: Commit**

```bash
git add internal/models/behavior.go internal/models/behavior_test.go internal/models/provenance.go
git commit -m "feat: add episodic/workflow memory types and consolidation provenance"
```

---

### Task 2: Schema Migration V9

**Files:**
- Modify: `internal/store/schema.go`
- Test: `internal/store/schema_test.go`

- [ ] **Step 1: Write migration test**

In `internal/store/schema_test.go`, add:

```go
func TestMigrateV8ToV9(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create V8 schema
	ctx := context.Background()
	if err := initializeSchema(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Insert a behavior with old scope values
	_, err = db.ExecContext(ctx,
		`INSERT INTO behaviors (id, name, kind, canonical, scope) VALUES (?, ?, ?, ?, ?)`,
		"b-local", "local behavior", "directive", "test", "local")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO behaviors (id, name, kind, canonical, scope) VALUES (?, ?, ?, ?, ?)`,
		"b-global", "global behavior", "directive", "test", "global")
	if err != nil {
		t.Fatal(err)
	}

	// Run migration
	if err := migrateV8ToV9(ctx, db, "test/project"); err != nil {
		t.Fatal(err)
	}

	// Verify scope values transformed
	var localScope, globalScope string
	db.QueryRowContext(ctx, `SELECT scope FROM behaviors WHERE id = ?`, "b-local").Scan(&localScope)
	db.QueryRowContext(ctx, `SELECT scope FROM behaviors WHERE id = ?`, "b-global").Scan(&globalScope)

	if localScope != "project:test/project" {
		t.Errorf("local scope = %q, want %q", localScope, "project:test/project")
	}
	if globalScope != "universal" {
		t.Errorf("global scope = %q, want %q", globalScope, "universal")
	}

	// Verify events table exists
	var tableName string
	err = db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='events'`).Scan(&tableName)
	if err != nil {
		t.Errorf("events table not created: %v", err)
	}

	// Verify memory_type column exists with default
	var memType string
	db.QueryRowContext(ctx, `SELECT memory_type FROM behaviors WHERE id = ?`, "b-local").Scan(&memType)
	if memType != "semantic" {
		t.Errorf("memory_type = %q, want %q", memType, "semantic")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestMigrateV8ToV9 -v`
Expected: FAIL — `migrateV8ToV9` undefined

- [ ] **Step 3: Implement V9 migration**

In `internal/store/schema.go`:

1. Bump `SchemaVersion` to `9`
2. Add `migrateV8ToV9` function:

```go
func migrateV8ToV9(ctx context.Context, db *sql.DB, projectID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Transform scope values
	if _, err := tx.ExecContext(ctx,
		`UPDATE behaviors SET scope = 'universal' WHERE scope = 'global'`); err != nil {
		return fmt.Errorf("migrate global scope: %w", err)
	}
	if projectID != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE behaviors SET scope = 'project:' || ? WHERE scope = 'local'`, projectID); err != nil {
			return fmt.Errorf("migrate local scope: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE behaviors SET scope = 'universal' WHERE scope = 'local'`); err != nil {
			return fmt.Errorf("migrate local scope fallback: %w", err)
		}
	}

	// Add memory_type column
	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE behaviors ADD COLUMN memory_type TEXT DEFAULT 'semantic'`); err != nil {
		return fmt.Errorf("add memory_type column: %w", err)
	}

	// Add type-specific data columns
	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE behaviors ADD COLUMN episode_data TEXT`); err != nil {
		return fmt.Errorf("add episode_data column: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE behaviors ADD COLUMN workflow_data TEXT`); err != nil {
		return fmt.Errorf("add workflow_data column: %w", err)
	}

	// Create events table
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			source TEXT NOT NULL,
			actor TEXT NOT NULL CHECK(actor IN ('user', 'agent', 'tool', 'system')),
			kind TEXT NOT NULL CHECK(kind IN ('message', 'action', 'result', 'error', 'correction')),
			content TEXT NOT NULL,
			metadata TEXT,
			project_id TEXT,
			provenance TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create events table: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id)`); err != nil {
		return fmt.Errorf("create events session index: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp)`); err != nil {
		return fmt.Errorf("create events timestamp index: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id)`); err != nil {
		return fmt.Errorf("create events project index: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`, 9)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}
```

3. Add the V9 migration call in `migrateSchema()`:

```go
if currentVersion < 9 {
    if err := migrateV8ToV9(ctx, db, projectID); err != nil {
        return fmt.Errorf("migrate v8 to v9: %w", err)
    }
}
```

**Important: Cascading signature change.** `migrateSchema` needs a `projectID string` parameter:
1. Add `projectID string` param to `migrateSchema()` in `schema.go`
2. Add `projectID string` param to `InitSchema()` (called from store constructor)
3. Update `NewSQLiteGraphStore()` to accept or resolve `projectID` (use `project.ResolveProjectID` from Task 3, or accept as parameter)
4. Update all callers of `NewSQLiteGraphStore` (in `multi.go`, `mcp/server.go`, tests)
5. Tests that create stores with `:memory:` can pass `""` for projectID

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestMigrateV8ToV9 -v`
Expected: PASS

- [ ] **Step 5: Run full store tests**

Run: `go test ./internal/store/ -v -count=1`
Expected: PASS — no regressions

- [ ] **Step 6: Commit**

```bash
git add internal/store/schema.go internal/store/schema_test.go
git commit -m "feat: V9 schema migration — events table, memory_type, scope transform"
```

---

### Task 3: Project Identity Resolution

**Files:**
- Create: `internal/project/identity.go`
- Create: `internal/project/identity_test.go`

- [ ] **Step 1: Write project identity tests**

```go
// internal/project/identity_test.go
package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectID(t *testing.T) {
	t.Run("finds config in current dir", func(t *testing.T) {
		dir := t.TempDir()
		floopDir := filepath.Join(dir, ".floop")
		os.MkdirAll(floopDir, 0o755)
		os.WriteFile(filepath.Join(floopDir, "config.yaml"), []byte("project:\n  id: \"test/myproject\"\n  name: \"myproject\"\n"), 0o644)

		id, err := ResolveProjectID(dir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "test/myproject" {
			t.Errorf("got %q, want %q", id, "test/myproject")
		}
	})

	t.Run("walks up directories", func(t *testing.T) {
		dir := t.TempDir()
		floopDir := filepath.Join(dir, ".floop")
		os.MkdirAll(floopDir, 0o755)
		os.WriteFile(filepath.Join(floopDir, "config.yaml"), []byte("project:\n  id: \"org/repo\"\n  name: \"repo\"\n"), 0o644)

		subdir := filepath.Join(dir, "src", "pkg")
		os.MkdirAll(subdir, 0o755)

		id, err := ResolveProjectID(subdir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "org/repo" {
			t.Errorf("got %q, want %q", id, "org/repo")
		}
	})

	t.Run("returns empty when no config", func(t *testing.T) {
		dir := t.TempDir()
		id, err := ResolveProjectID(dir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "" {
			t.Errorf("got %q, want empty", id)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestResolveProjectID -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement project identity**

```go
// internal/project/identity.go
package project

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Project struct {
		ID   string `yaml:"id"`
		Name string `yaml:"name"`
	} `yaml:"project"`
}

// ResolveProjectID walks up from startDir looking for .floop/config.yaml
// and returns the project ID. Returns "" if no config found.
func ResolveProjectID(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	for {
		configPath := filepath.Join(dir, ".floop", "config.yaml")
		data, err := os.ReadFile(configPath)
		if err == nil {
			var cfg Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return "", fmt.Errorf("parse %s: %w", configPath, err)
			}
			return cfg.Project.ID, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil // reached filesystem root
		}
		dir = parent
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project/ -run TestResolveProjectID -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/project/
git commit -m "feat: project identity resolution from .floop/config.yaml"
```

---

## Chunk 2: Event Buffer & Ingestion

**Branch:** `feat/consolidation/event-buffer` (on `feat/consolidation/data-model`)

### Task 4: Event Types

**Files:**
- Create: `internal/events/event.go`

- [ ] **Step 1: Create event types**

```go
// internal/events/event.go
package events

import (
	"time"
)

type EventActor string

const (
	ActorUser   EventActor = "user"
	ActorAgent  EventActor = "agent"
	ActorTool   EventActor = "tool"
	ActorSystem EventActor = "system"
)

type EventKind string

const (
	KindMessage    EventKind = "message"
	KindAction     EventKind = "action"
	KindResult     EventKind = "result"
	KindError      EventKind = "error"
	KindCorrection EventKind = "correction"
)

type Event struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"session_id"`
	Timestamp  time.Time         `json:"timestamp"`
	Source     string            `json:"source"`
	Actor      EventActor        `json:"actor"`
	Kind       EventKind         `json:"kind"`
	Content    string            `json:"content"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
	ProjectID  string            `json:"project_id,omitempty"`
	Provenance *EventProvenance  `json:"provenance,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

type EventProvenance struct {
	Model        string `json:"model,omitempty"`
	ModelVersion string `json:"model_version,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	Branch       string `json:"branch,omitempty"`
	TaskContext  string `json:"task_context,omitempty"`
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/events/event.go
git commit -m "feat: event types for raw conversation buffer"
```

---

### Task 5: Event Store

**Files:**
- Create: `internal/events/store.go`
- Create: `internal/events/store_test.go`

- [ ] **Step 1: Write EventStore tests**

```go
// internal/events/store_test.go
package events

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Create events table directly for unit testing
	_, err = db.Exec(`CREATE TABLE events (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		source TEXT NOT NULL,
		actor TEXT NOT NULL,
		kind TEXT NOT NULL,
		content TEXT NOT NULL,
		metadata TEXT,
		project_id TEXT,
		provenance TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestEventStore_AddAndGet(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLiteEventStore(db)
	ctx := context.Background()

	evt := Event{
		ID:        "evt-1",
		SessionID: "session-1",
		Timestamp: time.Now(),
		Source:    "claude-code",
		Actor:    ActorAgent,
		Kind:     KindMessage,
		Content:  "Hello world",
		ProjectID: "test/project",
	}

	if err := store.Add(ctx, evt); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetBySession(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Content != "Hello world" {
		t.Errorf("content = %q, want %q", got[0].Content, "Hello world")
	}
}

func TestEventStore_Prune(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLiteEventStore(db)
	ctx := context.Background()

	old := Event{
		ID: "evt-old", SessionID: "s1",
		Timestamp: time.Now().Add(-100 * 24 * time.Hour),
		Source: "test", Actor: ActorAgent, Kind: KindMessage, Content: "old",
	}
	recent := Event{
		ID: "evt-new", SessionID: "s2",
		Timestamp: time.Now(),
		Source: "test", Actor: ActorAgent, Kind: KindMessage, Content: "new",
	}

	store.Add(ctx, old)
	store.Add(ctx, recent)

	pruned, err := store.Prune(ctx, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Errorf("pruned %d, want 1", pruned)
	}

	all, _ := store.GetBySession(ctx, "s2")
	if len(all) != 1 {
		t.Errorf("remaining events = %d, want 1", len(all))
	}
}

func TestEventStore_AddBatch(t *testing.T) {
	db := setupTestDB(t)
	store := NewSQLiteEventStore(db)
	ctx := context.Background()

	events := []Event{
		{ID: "e1", SessionID: "s1", Timestamp: time.Now(), Source: "test", Actor: ActorUser, Kind: KindMessage, Content: "one"},
		{ID: "e2", SessionID: "s1", Timestamp: time.Now(), Source: "test", Actor: ActorAgent, Kind: KindMessage, Content: "two"},
		{ID: "e3", SessionID: "s1", Timestamp: time.Now(), Source: "test", Actor: ActorTool, Kind: KindResult, Content: "three"},
	}

	if err := store.AddBatch(ctx, events); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetBySession(ctx, "s1")
	if len(got) != 3 {
		t.Errorf("got %d events, want 3", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/events/ -v`
Expected: FAIL — `NewSQLiteEventStore` undefined

- [ ] **Step 3: Implement EventStore**

```go
// internal/events/store.go
package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type EventStore interface {
	Add(ctx context.Context, event Event) error
	AddBatch(ctx context.Context, events []Event) error
	GetBySession(ctx context.Context, sessionID string) ([]Event, error)
	GetSince(ctx context.Context, since time.Time) ([]Event, error)
	GetUnconsolidated(ctx context.Context) ([]Event, error)
	Prune(ctx context.Context, olderThan time.Duration) (int, error)
	Count(ctx context.Context) (int, error)
}

type SQLiteEventStore struct {
	db *sql.DB
}

func NewSQLiteEventStore(db *sql.DB) *SQLiteEventStore {
	return &SQLiteEventStore{db: db}
}

func (s *SQLiteEventStore) Add(ctx context.Context, event Event) error {
	metadata, _ := json.Marshal(event.Metadata)
	provenance, _ := json.Marshal(event.Provenance)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.SessionID, event.Timestamp, event.Source,
		string(event.Actor), string(event.Kind), event.Content,
		string(metadata), event.ProjectID, string(provenance))
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (s *SQLiteEventStore) AddBatch(ctx context.Context, events []Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO events (id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, evt := range events {
		metadata, _ := json.Marshal(evt.Metadata)
		provenance, _ := json.Marshal(evt.Provenance)
		if _, err := stmt.ExecContext(ctx,
			evt.ID, evt.SessionID, evt.Timestamp, evt.Source,
			string(evt.Actor), string(evt.Kind), evt.Content,
			string(metadata), evt.ProjectID, string(provenance)); err != nil {
			return fmt.Errorf("insert event %s: %w", evt.ID, err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteEventStore) GetBySession(ctx context.Context, sessionID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance, created_at
		 FROM events WHERE session_id = ? ORDER BY timestamp ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *SQLiteEventStore) GetSince(ctx context.Context, since time.Time) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance, created_at
		 FROM events WHERE timestamp >= ? ORDER BY timestamp ASC`, since)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// GetUnconsolidated returns events that haven't been processed yet.
// v0 simplification: returns ALL events. v1 will track consolidation status
// per-event (e.g., via a consolidated_at column) and filter properly.
func (s *SQLiteEventStore) GetUnconsolidated(ctx context.Context) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance, created_at
		 FROM events ORDER BY timestamp ASC`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *SQLiteEventStore) Prune(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM events WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune events: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (s *SQLiteEventStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count)
	return count, err
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var evt Event
		var actor, kind string
		var metadata, provenance sql.NullString
		var projectID sql.NullString

		if err := rows.Scan(
			&evt.ID, &evt.SessionID, &evt.Timestamp, &evt.Source,
			&actor, &kind, &evt.Content,
			&metadata, &projectID, &provenance, &evt.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		evt.Actor = EventActor(actor)
		evt.Kind = EventKind(kind)
		if projectID.Valid {
			evt.ProjectID = projectID.String
		}
		if metadata.Valid && metadata.String != "null" {
			json.Unmarshal([]byte(metadata.String), &evt.Metadata)
		}
		if provenance.Valid && provenance.String != "null" {
			var prov EventProvenance
			json.Unmarshal([]byte(provenance.String), &prov)
			evt.Provenance = &prov
		}

		events = append(events, evt)
	}
	return events, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/events/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/events/
git commit -m "feat: event store — append-only SQLite storage for raw conversation events"
```

---

### Task 6: Transcript Adapters

**Files:**
- Create: `internal/events/adapter.go`
- Create: `internal/events/adapter_markdown.go`
- Create: `internal/events/adapter_markdown_test.go`
- Create: `internal/events/adapter_jsonl.go`
- Create: `internal/events/adapter_jsonl_test.go`
- Create: `internal/events/adapter_json.go`
- Create: `internal/events/adapter_json_test.go`

- [ ] **Step 1: Create adapter interface**

```go
// internal/events/adapter.go
package events

import "io"

// TranscriptAdapter parses agent-specific transcript formats into Events.
type TranscriptAdapter interface {
	Parse(reader io.Reader) ([]Event, error)
	Format() string
}

// AdapterRegistry maps format names to adapters.
var adapters = map[string]TranscriptAdapter{}

func RegisterAdapter(a TranscriptAdapter) {
	adapters[a.Format()] = a
}

func GetAdapter(format string) (TranscriptAdapter, bool) {
	a, ok := adapters[format]
	return a, ok
}

func AvailableFormats() []string {
	formats := make([]string, 0, len(adapters))
	for f := range adapters {
		formats = append(formats, f)
	}
	return formats
}
```

- [ ] **Step 2: Write markdown adapter test**

```go
// internal/events/adapter_markdown_test.go
package events

import (
	"strings"
	"testing"
)

func TestMarkdownAdapter_Parse(t *testing.T) {
	input := "User: How do I fix this bug?\n\nAssistant: Check the error log first.\n\nUser: That worked, thanks!\n"

	adapter := &MarkdownAdapter{Source: "test-agent"}
	events, err := adapter.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Actor != ActorUser {
		t.Errorf("event[0].Actor = %q, want %q", events[0].Actor, ActorUser)
	}
	if events[1].Actor != ActorAgent {
		t.Errorf("event[1].Actor = %q, want %q", events[1].Actor, ActorAgent)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/events/ -run TestMarkdownAdapter -v`
Expected: FAIL — `MarkdownAdapter` undefined

- [ ] **Step 4: Implement markdown adapter**

Create `internal/events/adapter_markdown.go` — a line-based parser that detects "User:", "Assistant:", "Tool:", "System:" prefixes and groups consecutive lines into events. Each event gets a ULID, shared session ID, and the configured source.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/events/ -run TestMarkdownAdapter -v`
Expected: PASS

- [ ] **Step 6: Implement JSONL adapter (Claude Code format)**

Create `internal/events/adapter_jsonl.go` — parses Claude Code session JSONL format. Each line is a JSON object with `type`, `role`, `content` fields. Map `role: "user"` → `ActorUser`, `role: "assistant"` → `ActorAgent`, tool calls → `ActorTool`.

Write corresponding test in `internal/events/adapter_jsonl_test.go`.

- [ ] **Step 7: Implement generic JSON adapter**

Create `internal/events/adapter_json.go` — accepts JSON arrays of objects with `actor`, `kind`, `content` fields (passthrough format for agents that can emit structured events).

Write corresponding test in `internal/events/adapter_json_test.go`.

- [ ] **Step 8: Run all adapter tests**

Run: `go test ./internal/events/ -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/events/adapter*.go
git commit -m "feat: transcript adapters — markdown, JSONL (Claude Code), generic JSON"
```

---

## Chunk 3: Consolidation Pipeline

**Branch:** `feat/consolidation/pipeline` (on `feat/consolidation/event-buffer`)

### Task 7: Consolidator Interface & Pipeline Types

**Files:**
- Create: `internal/consolidation/consolidator.go`

- [ ] **Step 1: Create interface and types**

```go
// internal/consolidation/consolidator.go
package consolidation

import (
	"context"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// Candidate is a memory candidate extracted from raw events.
type Candidate struct {
	SourceEvents  []string       // event IDs
	RawText       string         // relevant excerpt
	CandidateType string         // correction, discovery, decision, failure, workflow, context
	Confidence    float64        // 0.0-1.0
	SessionContext map[string]any // project, file, task, branch, model
}

// ClassifiedMemory is a typed, classified memory ready for graph insertion.
type ClassifiedMemory struct {
	Candidate
	Kind         models.BehaviorKind
	MemoryType   string // semantic, episodic, procedural
	Scope        string // "universal" or "project:namespace/name"
	Importance   float64
	Content      models.BehaviorContent
	EpisodeData  *models.EpisodeData
	WorkflowData *models.WorkflowData
}

// MergeProposal proposes merging a new memory into an existing behavior.
type MergeProposal struct {
	Memory     ClassifiedMemory
	TargetID   string  // existing behavior ID
	Similarity float64 // cosine similarity
	Strategy   string  // "absorb", "supersede", "supplement"
}

// Consolidator defines the four-stage consolidation pipeline.
type Consolidator interface {
	Extract(ctx context.Context, events []events.Event) ([]Candidate, error)
	Classify(ctx context.Context, candidates []Candidate) ([]ClassifiedMemory, error)
	Relate(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore) ([]store.Edge, []MergeProposal, error)
	Promote(ctx context.Context, memories []ClassifiedMemory, edges []store.Edge, merges []MergeProposal, s store.GraphStore) error
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/consolidation/consolidator.go
git commit -m "feat: consolidator interface and pipeline types"
```

---

### Task 8: Heuristic Consolidator — Extract Stage

**Files:**
- Create: `internal/consolidation/heuristic.go`
- Create: `internal/consolidation/heuristic_test.go`

- [ ] **Step 1: Write extraction tests**

```go
// internal/consolidation/heuristic_test.go
package consolidation

import (
	"context"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/events"
)

func TestHeuristicExtract_Correction(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	evts := []events.Event{
		{ID: "e1", Actor: events.ActorUser, Kind: events.KindMessage, Content: "No, don't do that. Instead use fmt.Errorf to wrap errors.", Timestamp: time.Now()},
	}

	candidates, err := h.Extract(ctx, evts)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate from correction pattern")
	}
	if candidates[0].CandidateType != "correction" {
		t.Errorf("type = %q, want %q", candidates[0].CandidateType, "correction")
	}
}

func TestHeuristicExtract_NoSignal(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	evts := []events.Event{
		{ID: "e1", Actor: events.ActorAgent, Kind: events.KindMessage, Content: "Here is the code you requested.", Timestamp: time.Now()},
	}

	candidates, err := h.Extract(ctx, evts)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Errorf("got %d candidates, want 0 for no-signal content", len(candidates))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/consolidation/ -run TestHeuristicExtract -v`
Expected: FAIL — `NewHeuristicConsolidator` undefined

- [ ] **Step 3: Implement heuristic extraction**

In `internal/consolidation/heuristic.go`:

```go
package consolidation

import (
	"context"
	"regexp"
	"strings"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/store"
)

// HeuristicConsolidator is the v0 consolidation engine using rules and patterns.
type HeuristicConsolidator struct{}

func NewHeuristicConsolidator() *HeuristicConsolidator {
	return &HeuristicConsolidator{}
}

// Correction detection patterns
var correctionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(no|don'?t|stop|instead|rather|actually|wrong)\b.*\b(use|do|try|should)\b`),
	regexp.MustCompile(`(?i)\b(not that|not like that)\b`),
	regexp.MustCompile(`(?i)\binstead of\b`),
}

// Decision detection patterns
var decisionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(let'?s go with|we'?ll use|decided on|choosing|picked)\b`),
}

// Failure detection patterns
var failurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(didn'?t work|failed|broken|bug|error|crash)\b.*\b(because|due to|since)\b`),
}

func (h *HeuristicConsolidator) Extract(ctx context.Context, evts []events.Event) ([]Candidate, error) {
	var candidates []Candidate

	for _, evt := range evts {
		// Only extract from user messages (corrections come from users)
		if evt.Actor != events.ActorUser {
			continue
		}

		content := evt.Content
		if len(content) < 10 {
			continue
		}

		candidate := Candidate{
			SourceEvents: []string{evt.ID},
			RawText:      content,
			Confidence:   0.5,
		}

		if matchesAny(content, correctionPatterns) {
			candidate.CandidateType = "correction"
			candidate.Confidence = 0.7
			candidates = append(candidates, candidate)
		} else if matchesAny(content, decisionPatterns) {
			candidate.CandidateType = "decision"
			candidate.Confidence = 0.5
			candidates = append(candidates, candidate)
		} else if matchesAny(content, failurePatterns) {
			candidate.CandidateType = "failure"
			candidate.Confidence = 0.6
			candidates = append(candidates, candidate)
		}
	}

	return candidates, nil
}

func matchesAny(text string, patterns []*regexp.Regexp) bool {
	lower := strings.ToLower(text)
	for _, p := range patterns {
		if p.MatchString(lower) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/consolidation/ -run TestHeuristicExtract -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/consolidation/
git commit -m "feat: heuristic consolidator — extract stage with correction/decision/failure patterns"
```

---

### Task 9: Heuristic Consolidator — Classify, Relate, Promote Stages

**Files:**
- Modify: `internal/consolidation/heuristic.go`
- Modify: `internal/consolidation/heuristic_test.go`

- [ ] **Step 1: Write classify test**

```go
func TestHeuristicClassify(t *testing.T) {
	h := NewHeuristicConsolidator()
	ctx := context.Background()

	candidates := []Candidate{
		{CandidateType: "correction", RawText: "Don't use fmt.Println, use the logger instead", Confidence: 0.7},
		{CandidateType: "failure", RawText: "The auth test failed because the token was expired", Confidence: 0.6},
	}

	classified, err := h.Classify(ctx, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(classified) != 2 {
		t.Fatalf("got %d classified, want 2", len(classified))
	}
	if classified[0].Kind != models.BehaviorKindDirective {
		t.Errorf("correction classified as %q, want %q", classified[0].Kind, models.BehaviorKindDirective)
	}
	if classified[0].MemoryType != models.MemoryTypeSemantic {
		t.Errorf("memory type = %q, want %q", classified[0].MemoryType, models.MemoryTypeSemantic)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/consolidation/ -run TestHeuristicClassify -v`
Expected: FAIL — `Classify` not implemented

- [ ] **Step 3: Implement Classify**

In `internal/consolidation/heuristic.go`, add `Classify` method:

- Corrections → `BehaviorKindDirective` (semantic)
- Decisions → `BehaviorKindPreference` (semantic)
- Failures → `BehaviorKindEpisodic` (episodic)
- Workflows → `BehaviorKindProcedure` (procedural)
- Generate `Content.Canonical` from raw text (strip noise, keep substance)
- Generate `Content.Summary` (truncate to ~60 chars)
- Auto-tag from content keywords
- Default scope: `"universal"`

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/consolidation/ -run TestHeuristicClassify -v`
Expected: PASS

- [ ] **Step 5: Implement Relate (stub for v0)**

The heuristic Relate stage:
1. If embedder available: embed the memory, search LanceDB for neighbors, propose `similar-to` edges above threshold
2. If no embedder: use tag-based Jaccard similarity only
3. Check for merges: if similarity > `DefaultAutoMergeThreshold` (0.9), create MergeProposal
4. Return edges and merge proposals

For v0, this can be minimal — even returning empty edges is acceptable. The consolidation runner handles graceful degradation.

- [ ] **Step 6: Implement Promote**

The Promote stage:
1. For each merge proposal: use existing dedup/merge logic from `internal/dedup/`
2. For each new memory: convert `ClassifiedMemory` → `models.Behavior` → `store.Node`, call `store.AddNode`
3. For each edge: call `store.AddEdge`
4. All inside a store transaction where possible

- [ ] **Step 7: Run all consolidation tests**

Run: `go test ./internal/consolidation/ -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/consolidation/
git commit -m "feat: heuristic consolidator — classify, relate, promote stages"
```

---

### Task 10: Consolidation Runner

**Files:**
- Create: `internal/consolidation/runner.go`
- Create: `internal/consolidation/runner_test.go`

- [ ] **Step 1: Write runner test**

Test that the runner wires Extract→Classify→Relate→Promote and supports dry-run mode:

```go
func TestRunner_DryRun(t *testing.T) {
	runner := NewRunner(NewHeuristicConsolidator())
	ctx := context.Background()

	evts := []events.Event{
		{ID: "e1", Actor: events.ActorUser, Kind: events.KindMessage,
			Content: "No, don't use fmt.Println. Use the structured logger instead.",
			Timestamp: time.Now()},
	}

	result, err := runner.Run(ctx, evts, nil, RunOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) == 0 {
		t.Fatal("expected candidates in dry run")
	}
	if result.Promoted != 0 {
		t.Error("dry run should not promote anything")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/consolidation/ -run TestRunner -v`
Expected: FAIL

- [ ] **Step 3: Implement runner**

```go
// internal/consolidation/runner.go
package consolidation

import (
	"context"
	"fmt"
	"time"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/store"
)

type RunOptions struct {
	DryRun    bool
	ProjectID string
}

type RunResult struct {
	Candidates []Candidate
	Classified []ClassifiedMemory
	Edges      []store.Edge
	Merges     []MergeProposal
	Promoted   int
	Duration   time.Duration
}

type Runner struct {
	consolidator Consolidator
}

func NewRunner(c Consolidator) *Runner {
	return &Runner{consolidator: c}
}

func (r *Runner) Run(ctx context.Context, evts []events.Event, s store.GraphStore, opts RunOptions) (*RunResult, error) {
	start := time.Now()
	result := &RunResult{}

	candidates, err := r.consolidator.Extract(ctx, evts)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	result.Candidates = candidates

	if len(candidates) == 0 {
		result.Duration = time.Since(start)
		return result, nil
	}

	classified, err := r.consolidator.Classify(ctx, candidates)
	if err != nil {
		return nil, fmt.Errorf("classify: %w", err)
	}
	result.Classified = classified

	if opts.DryRun || s == nil {
		result.Duration = time.Since(start)
		return result, nil
	}

	edges, merges, err := r.consolidator.Relate(ctx, classified, s)
	if err != nil {
		return nil, fmt.Errorf("relate: %w", err)
	}
	result.Edges = edges
	result.Merges = merges

	if err := r.consolidator.Promote(ctx, classified, edges, merges, s); err != nil {
		return nil, fmt.Errorf("promote: %w", err)
	}
	result.Promoted = len(classified) // all memories are promoted (new behaviors + merges into existing)
	result.Duration = time.Since(start)

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/consolidation/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/consolidation/runner.go internal/consolidation/runner_test.go
git commit -m "feat: consolidation runner — pipeline orchestrator with dry-run support"
```

---

## Chunk 4: CLI Commands & MCP Tools

**Branch:** `feat/consolidation/cli-mcp` (on `feat/consolidation/pipeline`)

### Task 11: `floop ingest` CLI Command

**Files:**
- Create: `cmd/floop/cmd_ingest.go`
- Create: `cmd/floop/cmd_ingest_test.go`
- Modify: `cmd/floop/main.go` (register command)

- [ ] **Step 1: Write CLI test**

Test that `floop ingest --format markdown --source test` reads stdin and writes events.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/floop/ -run TestIngestCmd -v`
Expected: FAIL

- [ ] **Step 3: Implement `floop ingest`**

```go
func newIngestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest [file]",
		Short: "Import conversation transcript into event buffer",
		Long:  `Parses a transcript file (or stdin) and stores events for consolidation.`,
		RunE:  runIngest,
	}
	cmd.Flags().String("format", "markdown", "Transcript format (markdown, claude-code-jsonl, generic-json)")
	cmd.Flags().String("source", "", "Agent source identifier (e.g., claude-code, gemini)")
	cmd.Flags().String("session", "", "Session ID (auto-generated if empty)")
	cmd.Flags().Bool("json", false, "Output results as JSON")
	return cmd
}
```

Handler: resolve adapter from `--format`, read file or stdin, parse events, stamp session ID and source, write to EventStore via the SQLite DB at `~/.floop/floop.db`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/floop/ -run TestIngestCmd -v`
Expected: PASS

- [ ] **Step 5: Register command in main.go**

Add `rootCmd.AddCommand(newIngestCmd())` in `main.go`.

- [ ] **Step 6: Commit**

```bash
git add cmd/floop/cmd_ingest.go cmd/floop/cmd_ingest_test.go cmd/floop/main.go
git commit -m "feat: floop ingest CLI command"
```

---

### Task 12: `floop consolidate` CLI Command

**Files:**
- Create: `cmd/floop/cmd_consolidate.go`
- Create: `cmd/floop/cmd_consolidate_test.go`

- [ ] **Step 1: Write CLI test**

Test `floop consolidate --dry-run` produces output without mutating store.

- [ ] **Step 2: Run test to verify it fails**

- [ ] **Step 3: Implement `floop consolidate`**

```go
func newConsolidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consolidate",
		Short: "Run consolidation pipeline on raw events",
		Long:  `Extracts, classifies, and promotes memories from the event buffer.`,
		RunE:  runConsolidate,
	}
	cmd.Flags().String("session", "", "Consolidate specific session only")
	cmd.Flags().String("since", "", "Consolidate events since duration (e.g., 24h)")
	cmd.Flags().Bool("dry-run", false, "Show what would be extracted without promoting")
	cmd.Flags().Bool("json", false, "Output results as JSON")
	return cmd
}
```

Handler: open EventStore, query events (by session, since, or all unconsolidated), create `Runner` with `HeuristicConsolidator`, run pipeline, report results. Prune old events after successful consolidation.

- [ ] **Step 4: Run tests to verify they pass**

- [ ] **Step 5: Register command and commit**

```bash
git add cmd/floop/cmd_consolidate.go cmd/floop/cmd_consolidate_test.go cmd/floop/main.go
git commit -m "feat: floop consolidate CLI command with dry-run support"
```

---

### Task 13: `floop events` CLI Command

**Files:**
- Create: `cmd/floop/cmd_events.go`
- Create: `cmd/floop/cmd_events_test.go`

- [ ] **Step 1: Implement `floop events`**

```go
func newEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Inspect and manage the raw event buffer",
		RunE:  runEvents,
	}
	cmd.Flags().String("session", "", "Filter by session ID")
	cmd.Flags().String("prune", "", "Delete events older than duration (e.g., 90d)")
	cmd.Flags().Bool("json", false, "Output as JSON")
	cmd.Flags().Bool("count", false, "Show event count only")
	return cmd
}
```

Handler: if `--prune`, run prune and report count. If `--count`, report total. Otherwise list events (with optional session filter), formatted as table or JSON.

- [ ] **Step 2: Write test, run, verify pass**

- [ ] **Step 3: Register and commit**

```bash
git add cmd/floop/cmd_events.go cmd/floop/cmd_events_test.go cmd/floop/main.go
git commit -m "feat: floop events CLI command — inspect and prune event buffer"
```

---

### Task 14: `floop migrate` CLI Command

**Files:**
- Create: `cmd/floop/cmd_migrate.go`
- Create: `cmd/floop/cmd_migrate_test.go`

- [ ] **Step 1: Implement `floop migrate --merge-local-to-global`**

Handler:
1. Resolve project ID from `.floop/config.yaml`
2. Open local store (`.floop/floop.db`)
3. Read all behaviors
4. Stamp `scope: "project:<id>"` on each
5. Open global store (`~/.floop/floop.db`)
6. Insert (skip duplicates by content hash)
7. Report count merged
8. Keep local DB as backup

- [ ] **Step 2: Write test, run, verify pass**

- [ ] **Step 3: Register and commit**

```bash
git add cmd/floop/cmd_migrate.go cmd/floop/cmd_migrate_test.go cmd/floop/main.go
git commit -m "feat: floop migrate CLI — opt-in local-to-global store merge"
```

---

### Task 15: MCP Tools — `floop_observe` and `floop_consolidate`

**Files:**
- Create: `internal/mcp/handler_observe.go`
- Create: `internal/mcp/handler_consolidate.go`
- Modify: `internal/mcp/server.go`

- [ ] **Step 1: Implement `floop_observe` handler**

Fire-and-forget event ingestion. Accepts `source`, `content`, `actor`, `kind`, optional `metadata`. Creates an Event and stores it. Returns success acknowledgment.

- [ ] **Step 2: Implement `floop_consolidate` handler**

Accepts `session` (optional), `since` (optional), `dry_run` (bool). Runs the consolidation pipeline. Returns result summary.

- [ ] **Step 3: Register both tools in `registerTools()`**

Add tool registrations with JSON schema definitions following the existing pattern.

- [ ] **Step 4: Write tests for both handlers**

Follow existing handler test patterns in `internal/mcp/`.

- [ ] **Step 5: Run MCP tests**

Run: `go test ./internal/mcp/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/handler_observe.go internal/mcp/handler_consolidate.go internal/mcp/server.go
git commit -m "feat: floop_observe and floop_consolidate MCP tools"
```

---

### Task 16: Update Tiering for New Memory Types

**Files:**
- Modify: `internal/tiering/activation_tiers.go`

- [ ] **Step 1: Add tiering rules for episodic and workflow kinds**

In the tier assignment logic, add cases for `KindEpisodic` and `KindWorkflow`:
- Episodic at `TierFull`: include full episode context
- Episodic at `TierSummary`: "When X happened, learned Y" format
- Workflow at `TierFull`: all steps with conditions
- Workflow at `TierSummary`: trigger + step count

- [ ] **Step 2: Write tests for new tier assignments**

- [ ] **Step 3: Run tiering tests**

Run: `go test ./internal/tiering/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tiering/
git commit -m "feat: tiering support for episodic and workflow memory types"
```

---

### Task 17: Config Expansion

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add consolidation and events config sections**

```go
type ConsolidationConfig struct {
	AutoConsolidate bool   `json:"auto_consolidate" yaml:"auto_consolidate"` // trigger after session
	Executor        string `json:"executor" yaml:"executor"`                 // "heuristic" (v0), "llm" (v1), "local" (v2)
}

type EventsConfig struct {
	RetentionDays int `json:"retention_days" yaml:"retention_days"` // default: 90
}
```

Add to `FloopConfig`:
```go
Consolidation ConsolidationConfig `json:"consolidation" yaml:"consolidation"`
Events        EventsConfig        `json:"events" yaml:"events"`
```

- [ ] **Step 2: Run config tests**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: consolidation and events config sections"
```

---

### Task 18: Integration Test & Final Verification

**Files:**
- No new files — verification only

- [ ] **Step 1: Run full test suite**

Run: `go test -race ./... 2>&1 | tail -20`
Expected: All PASS, no race conditions

- [ ] **Step 2: Build the binary**

Run: `go build ./cmd/floop`
Expected: Builds successfully

- [ ] **Step 3: Manual smoke test**

```bash
# Ingest a test transcript
echo "User: Don't use fmt.Println, use the logger.\nAssistant: Got it, I'll use the logger." | ./floop ingest --format markdown --source test

# Check events were stored
./floop events --count

# Run consolidation (dry run)
./floop consolidate --dry-run

# Run consolidation (real)
./floop consolidate

# Verify behavior was created
./floop list
```

- [ ] **Step 4: Commit any fixes from smoke test**

- [ ] **Step 5: Final commit — update CLI reference docs**

```bash
git add docs/CLI_REFERENCE.md
git commit -m "docs: add ingest, consolidate, events, migrate commands to CLI reference"
```