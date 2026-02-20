package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// preSchemaVersionDDL simulates a database created before schema_version
// tracking was added. It has the behaviors table but WITHOUT metadata_extra,
// behavior_type, or content_hash columns.
const preSchemaVersionDDL = `
CREATE TABLE behaviors (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    content_canonical TEXT NOT NULL,
    content_expanded TEXT,
    content_summary TEXT,
    content_structured TEXT,
    content_tags TEXT,
    provenance_source_type TEXT,
    provenance_correction_id TEXT,
    provenance_created_at TEXT,
    requires TEXT,
    overrides TEXT,
    conflicts TEXT,
    confidence REAL DEFAULT 0.6,
    priority INTEGER DEFAULT 0,
    scope TEXT DEFAULT 'local',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE behavior_when (
    behavior_id TEXT NOT NULL REFERENCES behaviors(id) ON DELETE CASCADE,
    field TEXT NOT NULL,
    value TEXT NOT NULL,
    value_type TEXT DEFAULT 'string',
    PRIMARY KEY (behavior_id, field)
);
CREATE TABLE behavior_stats (
    behavior_id TEXT PRIMARY KEY REFERENCES behaviors(id) ON DELETE CASCADE,
    times_activated INTEGER DEFAULT 0,
    times_followed INTEGER DEFAULT 0,
    times_overridden INTEGER DEFAULT 0,
    times_confirmed INTEGER DEFAULT 0,
    last_activated TEXT,
    last_confirmed TEXT
);
CREATE TABLE corrections (
    id TEXT PRIMARY KEY,
    timestamp TEXT NOT NULL,
    agent_action TEXT NOT NULL,
    corrected_action TEXT NOT NULL,
    human_response TEXT,
    context TEXT,
    conversation_id TEXT,
    turn_number INTEGER,
    corrector TEXT,
    processed INTEGER DEFAULT 0,
    processed_at TEXT
);
CREATE TABLE edges (
    source TEXT NOT NULL,
    target TEXT NOT NULL,
    kind TEXT NOT NULL,
    PRIMARY KEY (source, target, kind)
);
CREATE TABLE dirty_behaviors (
    behavior_id TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    dirty_at TEXT NOT NULL
);
CREATE TABLE export_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    last_export_time TEXT,
    jsonl_hash TEXT
);
CREATE TABLE config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

func TestInitSchema_PreSchemaVersionDB(t *testing.T) {
	// Simulate a database created before schema_version was introduced.
	// This DB has tables but no schema_version table and is missing
	// metadata_extra, behavior_type, content_hash columns.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Create the pre-schema_version tables
	if _, err := db.ExecContext(ctx, preSchemaVersionDDL); err != nil {
		t.Fatalf("create pre-schema tables: %v", err)
	}

	// Verify metadata_extra doesn't exist
	cols := getColumns(t, db, "behaviors")
	if cols["metadata_extra"] {
		t.Fatal("metadata_extra should not exist in pre-schema DB")
	}

	// Run InitSchema — this should detect the existing tables and migrate
	if err := InitSchema(ctx, db); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	// Verify metadata_extra was added by migration
	cols = getColumns(t, db, "behaviors")
	if !cols["metadata_extra"] {
		t.Error("metadata_extra column was not added after InitSchema")
	}
	if !cols["behavior_type"] {
		t.Error("behavior_type column was not added after InitSchema")
	}
	if !cols["content_hash"] {
		t.Error("content_hash column was not added after InitSchema")
	}

	// Verify edges table got v2→v3 migration columns
	edgeCols := getColumns(t, db, "edges")
	if !edgeCols["weight"] {
		t.Error("weight column was not added to edges after InitSchema")
	}
	if !edgeCols["created_at"] {
		t.Error("created_at column was not added to edges after InitSchema")
	}

	// Verify schema version was recorded
	var version int
	err = db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatalf("get schema version: %v", err)
	}
	if version != SchemaVersion {
		t.Errorf("schema version = %d, want %d", version, SchemaVersion)
	}

	// Verify we can INSERT with metadata_extra (the original bug)
	_, err = db.ExecContext(ctx, `
		INSERT INTO behaviors (id, name, kind, content_canonical, metadata_extra, created_at, updated_at)
		VALUES ('test-1', 'test', 'behavior', 'test content', '{}', '2024-01-01', '2024-01-01')
	`)
	if err != nil {
		t.Errorf("INSERT with metadata_extra failed (original bug): %v", err)
	}
}

func TestInitSchema_MigratesDespiteIntegrityFailure(t *testing.T) {
	// Scenario: DB at schema v1 WITH schema_version table but has FK violations.
	// ValidateIntegrity would fail, which previously blocked migrations entirely.
	// Migrations should still run — integrity issues don't prevent schema changes.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Create a v1 DB: has schema_version at 1, but missing v2 columns
	// Use the pre-schema DDL (no metadata_extra) + schema_version table
	if _, err := db.ExecContext(ctx, preSchemaVersionDDL); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_version (version, applied_at) VALUES (1, '2024-01-01')`); err != nil {
		t.Fatalf("insert version: %v", err)
	}

	// Create an orphaned behavior_stats row → FK violation
	if _, err := db.ExecContext(ctx, `INSERT INTO behavior_stats (behavior_id) VALUES ('nonexistent-id')`); err != nil {
		t.Fatalf("insert orphan: %v", err)
	}

	// Verify FK violation exists
	if err := ValidateIntegrity(ctx, db); err == nil {
		t.Fatal("expected ValidateIntegrity to fail due to FK violation")
	}

	// Verify metadata_extra doesn't exist yet
	cols := getColumns(t, db, "behaviors")
	if cols["metadata_extra"] {
		t.Fatal("metadata_extra should not exist before migration")
	}

	// InitSchema should still succeed and apply migrations
	if err := InitSchema(ctx, db); err != nil {
		t.Fatalf("InitSchema should migrate despite integrity issues, got: %v", err)
	}

	// Verify migration ran
	cols = getColumns(t, db, "behaviors")
	if !cols["metadata_extra"] {
		t.Error("metadata_extra column was not added")
	}
	if !cols["behavior_type"] {
		t.Error("behavior_type column was not added")
	}

	// Verify schema version updated
	var version int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("get version: %v", err)
	}
	if version != SchemaVersion {
		t.Errorf("schema version = %d, want %d", version, SchemaVersion)
	}
}

func TestMigrateV4ToV5(t *testing.T) {
	// Scenario: DB at schema v4 (stats trigger already applied).
	// After InitSchema, behaviors table should have embedding and embedding_model columns.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Create the pre-schema_version tables (v1 base)
	if _, err := db.ExecContext(ctx, preSchemaVersionDDL); err != nil {
		t.Fatalf("create pre-schema tables: %v", err)
	}

	// Add schema_version table
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}

	// Add v2 columns manually (behavior_type, metadata_extra, content_hash)
	for _, col := range []string{
		"ALTER TABLE behaviors ADD COLUMN behavior_type TEXT",
		"ALTER TABLE behaviors ADD COLUMN metadata_extra TEXT",
		"ALTER TABLE behaviors ADD COLUMN content_hash TEXT",
	} {
		if _, err := db.ExecContext(ctx, col); err != nil {
			t.Fatalf("add v2 column: %v", err)
		}
	}

	// Add v3 columns to edges (weight, created_at, last_activated)
	for _, col := range []string{
		"ALTER TABLE edges ADD COLUMN weight REAL DEFAULT 1.0",
		"ALTER TABLE edges ADD COLUMN created_at TEXT",
		"ALTER TABLE edges ADD COLUMN last_activated TEXT",
	} {
		if _, err := db.ExecContext(ctx, col); err != nil {
			t.Fatalf("add v3 column: %v", err)
		}
	}

	// Add v4 stats trigger
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER IF NOT EXISTS behavior_stats_dirty
		AFTER UPDATE ON behavior_stats
		BEGIN
		    INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at)
		    VALUES (NEW.behavior_id, 'update', datetime('now'));
		END
	`); err != nil {
		t.Fatalf("add v4 trigger: %v", err)
	}

	// Record as v4
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_version (version, applied_at) VALUES (4, datetime('now'))`); err != nil {
		t.Fatalf("insert version: %v", err)
	}

	// Verify embedding columns don't exist yet
	cols := getColumns(t, db, "behaviors")
	if cols["embedding"] {
		t.Fatal("embedding should not exist before migration")
	}
	if cols["embedding_model"] {
		t.Fatal("embedding_model should not exist before migration")
	}

	// Run InitSchema — this should detect v4 and migrate to v5
	if err := InitSchema(ctx, db); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	// Verify embedding and embedding_model were added
	cols = getColumns(t, db, "behaviors")
	if !cols["embedding"] {
		t.Error("embedding column was not added after InitSchema")
	}
	if !cols["embedding_model"] {
		t.Error("embedding_model column was not added after InitSchema")
	}

	// Verify schema version was updated to 5
	var version int
	err = db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatalf("get schema version: %v", err)
	}
	if version != SchemaVersion {
		t.Errorf("schema version = %d, want %d", version, SchemaVersion)
	}

	// Verify we can INSERT with embedding columns
	_, err = db.ExecContext(ctx, `
		INSERT INTO behaviors (id, name, kind, content_canonical, embedding, embedding_model, created_at, updated_at)
		VALUES ('test-v5', 'test', 'behavior', 'test content', X'0000803F', 'text-embedding-3-small', '2024-01-01', '2024-01-01')
	`)
	if err != nil {
		t.Errorf("INSERT with embedding columns failed: %v", err)
	}

	// Verify nullable: embedding can be NULL
	_, err = db.ExecContext(ctx, `
		INSERT INTO behaviors (id, name, kind, content_canonical, created_at, updated_at)
		VALUES ('test-v5-null', 'test2', 'behavior', 'test content null', '2024-01-01', '2024-01-01')
	`)
	if err != nil {
		t.Errorf("INSERT with NULL embedding failed: %v", err)
	}
}

// getColumns returns a map of column names for the given table.
func getColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = true
	}
	return cols
}
