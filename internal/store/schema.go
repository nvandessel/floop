// Package store provides graph storage implementations.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SchemaVersion is the current schema version.
const SchemaVersion = 9

// schemaV1 is the initial schema for the SQLite store.
const schemaV1 = `
-- Core behavior table (denormalized for single-query retrieval)
CREATE TABLE IF NOT EXISTS behaviors (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,  -- 'behavior', 'correction', etc. (node type)
    behavior_type TEXT,  -- 'directive', 'constraint', 'procedure', 'preference' (behavior type)

    -- Content (denormalized for single-query retrieval)
    content_canonical TEXT NOT NULL,
    content_expanded TEXT,
    content_summary TEXT,
    content_structured TEXT,  -- JSON
    content_tags TEXT,        -- JSON array

    -- Provenance
    provenance_source_type TEXT,
    provenance_correction_id TEXT,
    provenance_created_at TEXT,

    -- Relationships (JSON arrays)
    requires TEXT,
    overrides TEXT,
    conflicts TEXT,

    -- Metadata
    confidence REAL DEFAULT 0.6,
    priority INTEGER DEFAULT 0,
    scope TEXT DEFAULT 'local',
    metadata_extra TEXT,  -- JSON for arbitrary metadata (forget_reason, deprecation_reason, etc.)

    -- Embeddings (V6)
    embedding BLOB,           -- binary-encoded []float32 vector (little-endian)
    embedding_model TEXT,     -- model that produced the embedding

    -- Memory consolidation (V9)
    memory_type TEXT DEFAULT 'semantic',  -- 'semantic', 'episodic', 'procedural'
    episode_data TEXT,                    -- JSON for episodic memory data
    workflow_data TEXT,                   -- JSON for workflow memory data

    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    content_hash TEXT UNIQUE
);

-- Context conditions (enables indexed lookups)
CREATE TABLE IF NOT EXISTS behavior_when (
    behavior_id TEXT NOT NULL REFERENCES behaviors(id) ON DELETE CASCADE,
    field TEXT NOT NULL,       -- 'task', 'language', 'file_path'
    value TEXT NOT NULL,
    value_type TEXT DEFAULT 'string',  -- 'string', 'array', 'glob'
    PRIMARY KEY (behavior_id, field)
);
CREATE INDEX IF NOT EXISTS idx_when_field_value ON behavior_when(field, value);

-- Stats (frequently updated, kept separate)
CREATE TABLE IF NOT EXISTS behavior_stats (
    behavior_id TEXT PRIMARY KEY REFERENCES behaviors(id) ON DELETE CASCADE,
    times_activated INTEGER DEFAULT 0,
    times_followed INTEGER DEFAULT 0,
    times_overridden INTEGER DEFAULT 0,
    times_confirmed INTEGER DEFAULT 0,
    last_activated TEXT,
    last_confirmed TEXT
);

-- Corrections
CREATE TABLE IF NOT EXISTS corrections (
    id TEXT PRIMARY KEY,
    timestamp TEXT NOT NULL,
    agent_action TEXT NOT NULL,
    corrected_action TEXT NOT NULL,
    human_response TEXT,
    context TEXT,  -- JSON
    conversation_id TEXT,
    turn_number INTEGER,
    corrector TEXT,
    processed INTEGER DEFAULT 0,
    processed_at TEXT
);

-- Edges (graph relationships)
CREATE TABLE IF NOT EXISTS edges (
    source TEXT NOT NULL,
    target TEXT NOT NULL,
    kind TEXT NOT NULL,
    weight REAL DEFAULT 1.0,
    created_at TEXT DEFAULT (datetime('now')),
    last_activated TEXT,
    metadata TEXT,
    PRIMARY KEY (source, target, kind)
);
CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target);

-- Co-activation tracking for Hebbian learning (persisted across sessions)
CREATE TABLE IF NOT EXISTS co_activations (
    pair_key TEXT NOT NULL,
    activated_at TEXT NOT NULL,
    PRIMARY KEY (pair_key, activated_at)
);
CREATE INDEX IF NOT EXISTS idx_co_activations_pair ON co_activations(pair_key);

-- Dirty tracking for incremental export
CREATE TABLE IF NOT EXISTS dirty_behaviors (
    behavior_id TEXT PRIMARY KEY,
    operation TEXT NOT NULL,  -- 'insert', 'update', 'delete'
    dirty_at TEXT NOT NULL
);

-- Export state
CREATE TABLE IF NOT EXISTS export_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    last_export_time TEXT,
    jsonl_hash TEXT
);

-- Config
CREATE TABLE IF NOT EXISTS config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Events (V9) — episodic memory event buffer
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
    consolidated INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id);
CREATE INDEX IF NOT EXISTS idx_events_consolidated ON events(consolidated);

-- Schema version
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

-- Triggers for dirty tracking
CREATE TRIGGER IF NOT EXISTS behavior_insert_dirty
AFTER INSERT ON behaviors
BEGIN
    INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at)
    VALUES (NEW.id, 'insert', datetime('now'));
END;

CREATE TRIGGER IF NOT EXISTS behavior_update_dirty
AFTER UPDATE ON behaviors
BEGIN
    INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at)
    VALUES (NEW.id, 'update', datetime('now'));
END;

CREATE TRIGGER IF NOT EXISTS behavior_delete_dirty
AFTER DELETE ON behaviors
BEGIN
    INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at)
    VALUES (OLD.id, 'delete', datetime('now'));
END;

CREATE TRIGGER IF NOT EXISTS behavior_stats_dirty
AFTER UPDATE ON behavior_stats
BEGIN
    INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at)
    VALUES (NEW.behavior_id, 'update', datetime('now'));
END;
`

// InitSchema initializes the database schema.
// It creates all tables and applies migrations as needed.
// Runs integrity validation before migrations on existing databases.
// Uses an empty project ID — callers that need project-aware migration
// should use initSchemaWithProject directly.
func InitSchema(ctx context.Context, db *sql.DB) error {
	return initSchemaWithProject(ctx, db, "")
}

// initSchemaWithProject initializes the database schema with a project ID.
// The projectID is passed through to migrateSchema and used by the V9
// migration to transform scope values (local -> project:<id>).
func initSchemaWithProject(ctx context.Context, db *sql.DB, projectID string) error {
	// Check current schema version
	currentVersion, err := getSchemaVersion(ctx, db)
	if err != nil {
		// Schema version table doesn't exist. Check if this is a
		// pre-schema_version database (tables exist but no version tracking)
		// or a truly fresh database.
		if tableExists(ctx, db, "behaviors") {
			// Pre-schema_version DB: create the schema_version table and
			// run all migrations from version 0.
			if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
				version INTEGER PRIMARY KEY,
				applied_at TEXT NOT NULL
			)`); err != nil {
				return fmt.Errorf("failed to create schema_version table: %w", err)
			}
			if _, err := db.ExecContext(ctx,
				`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`, 1); err != nil {
				return fmt.Errorf("failed to record initial version: %w", err)
			}
			// Run all migrations from version 1
			if err := migrateSchema(ctx, db, 1, projectID); err != nil {
				return fmt.Errorf("failed to migrate pre-schema_version database: %w", err)
			}
			return nil
		}

		// Truly fresh database — create everything from scratch
		if err := createSchema(ctx, db); err != nil {
			return fmt.Errorf("failed to create schema: %w", err)
		}
		return nil
	}

	// Check structural integrity (corruption detection) before any writes.
	// Only runs PRAGMA integrity_check, NOT foreign_key_check — FK violations
	// are data-level issues that shouldn't block schema migration or startup.
	// Use ValidateIntegrity() or floop_validate for full validation including FK.
	if err := validateStructuralIntegrity(ctx, db); err != nil {
		return fmt.Errorf("database integrity check failed: %w", err)
	}

	// Apply migrations if needed
	if currentVersion < SchemaVersion {
		if err := migrateSchema(ctx, db, currentVersion, projectID); err != nil {
			return fmt.Errorf("failed to migrate schema: %w", err)
		}
	}

	return nil
}

// tableExists checks if a table exists in the database.
func tableExists(ctx context.Context, db *sql.DB, table string) bool {
	var name string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	return err == nil
}

// getSchemaVersion returns the current schema version from the database.
// Returns 0 and an error if the schema_version table doesn't exist.
func getSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// createSchema creates the initial database schema.
func createSchema(ctx context.Context, db *sql.DB) error {
	// Execute schema in a transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create all tables
	if _, err := tx.ExecContext(ctx, schemaV1); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Record schema version
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`,
		SchemaVersion); err != nil {
		return fmt.Errorf("failed to record schema version: %w", err)
	}

	return tx.Commit()
}

// migrateSchema applies migrations from currentVersion to SchemaVersion.
// The projectID is only used by the V9 migration to transform scope values.
func migrateSchema(ctx context.Context, db *sql.DB, currentVersion int, projectID string) error {
	// Apply migrations sequentially
	if currentVersion < 2 {
		if err := migrateV1ToV2(ctx, db); err != nil {
			return fmt.Errorf("migrate v1 to v2: %w", err)
		}
	}
	if currentVersion < 3 {
		if err := migrateV2ToV3(ctx, db); err != nil {
			return fmt.Errorf("migrate v2 to v3: %w", err)
		}
	}
	if currentVersion < 4 {
		if err := migrateV3ToV4(ctx, db); err != nil {
			return fmt.Errorf("migrate v3 to v4: %w", err)
		}
	}
	if currentVersion < 5 {
		if err := migrateV4ToV5(ctx, db); err != nil {
			return fmt.Errorf("migrate v4 to v5: %w", err)
		}
	}
	if currentVersion < 6 {
		if err := migrateV5ToV6(ctx, db); err != nil {
			return fmt.Errorf("migrate v5 to v6: %w", err)
		}
	}
	if currentVersion < 7 {
		if err := migrateV6ToV7(ctx, db); err != nil {
			return fmt.Errorf("migrate v6 to v7: %w", err)
		}
	}
	if currentVersion < 8 {
		if err := migrateV7ToV8(ctx, db); err != nil {
			return fmt.Errorf("migrate v7 to v8: %w", err)
		}
	}
	if currentVersion < 9 {
		if err := migrateV8ToV9(ctx, db, projectID); err != nil {
			return fmt.Errorf("migrate v8 to v9: %w", err)
		}
	}
	return nil
}

// migrateV1ToV2 adds missing columns to the behaviors table.
// Columns that may be missing from old v1 databases:
// - behavior_type: tracks behavior types (directive, constraint, etc.)
// - metadata_extra: JSON for arbitrary metadata (forget_reason, etc.)
// - content_hash: UNIQUE hash for deduplication
func migrateV1ToV2(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get existing columns
	existingCols := make(map[string]bool)
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(behaviors)`)
	if err != nil {
		return fmt.Errorf("check table info: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table info: %w", err)
		}
		existingCols[name] = true
	}

	// Add missing columns (idempotent)
	columnsToAdd := []struct {
		name string
		def  string
	}{
		{"behavior_type", "TEXT"},
		{"metadata_extra", "TEXT"},
		{"content_hash", "TEXT"},
	}

	for _, col := range columnsToAdd {
		if !existingCols[col.name] {
			_, err = tx.ExecContext(ctx, fmt.Sprintf(
				`ALTER TABLE behaviors ADD COLUMN %s %s`, col.name, col.def))
			if err != nil {
				return fmt.Errorf("add %s column: %w", col.name, err)
			}
		}
	}

	// Note: We cannot add UNIQUE constraint to content_hash via ALTER TABLE in SQLite.
	// The UNIQUE constraint will only apply to new databases created with the full schema.
	// For existing databases, deduplication logic handles uniqueness at the application level.

	// Record the new schema version
	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`,
		2)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}

// migrateV2ToV3 adds edge weight and temporal metadata columns to the edges table.
// Columns added:
// - weight: activation transmission factor (0.0-1.0)
// - created_at: when edge was created
// - last_activated: when activation last flowed through
func migrateV2ToV3(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get existing columns on edges table
	existingCols := make(map[string]bool)
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(edges)`)
	if err != nil {
		return fmt.Errorf("check table info: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table info: %w", err)
		}
		existingCols[name] = true
	}

	// Add missing columns (idempotent)
	columnsToAdd := []struct {
		name string
		def  string
	}{
		{"weight", "REAL DEFAULT 1.0"},
		{"created_at", "TEXT"},
		{"last_activated", "TEXT"},
	}

	for _, col := range columnsToAdd {
		if !existingCols[col.name] {
			_, err = tx.ExecContext(ctx, fmt.Sprintf(
				`ALTER TABLE edges ADD COLUMN %s %s`, col.name, col.def))
			if err != nil {
				return fmt.Errorf("add %s column: %w", col.name, err)
			}
		}
	}

	// Backfill existing edges: weight=1.0, created_at=now (RFC3339 format)
	now := time.Now().Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `UPDATE edges SET weight = 1.0 WHERE weight IS NULL`)
	if err != nil {
		return fmt.Errorf("backfill weight: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE edges SET created_at = ? WHERE created_at IS NULL`, now)
	if err != nil {
		return fmt.Errorf("backfill created_at: %w", err)
	}

	// Record the new schema version
	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`,
		3)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}

// migrateV3ToV4 adds a dirty tracking trigger on the behavior_stats table.
// Without this trigger, stats changes (times_activated, times_confirmed, etc.)
// are never exported to JSONL and lost on DB recreation.
func migrateV3ToV4(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		CREATE TRIGGER IF NOT EXISTS behavior_stats_dirty
		AFTER UPDATE ON behavior_stats
		BEGIN
		    INSERT OR REPLACE INTO dirty_behaviors (behavior_id, operation, dirty_at)
		    VALUES (NEW.behavior_id, 'update', datetime('now'));
		END
	`)
	if err != nil {
		return fmt.Errorf("create behavior_stats_dirty trigger: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`,
		4)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}

// migrateV4ToV5 adds the co_activations table for persistent Hebbian tracking.
func migrateV4ToV5(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS co_activations (
		    pair_key TEXT NOT NULL,
		    activated_at TEXT NOT NULL,
		    PRIMARY KEY (pair_key, activated_at)
		)
	`)
	if err != nil {
		return fmt.Errorf("create co_activations table: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_co_activations_pair ON co_activations(pair_key)
	`)
	if err != nil {
		return fmt.Errorf("create co_activations index: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`, 5)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}

// migrateV5ToV6 adds embedding vector columns to the behaviors table.
// Columns added:
// - embedding: binary-encoded []float32 vector (little-endian BLOB)
// - embedding_model: tracks which model produced the embedding
func migrateV5ToV6(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get existing columns on behaviors table
	existingCols := make(map[string]bool)
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(behaviors)`)
	if err != nil {
		return fmt.Errorf("check table info: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table info: %w", err)
		}
		existingCols[name] = true
	}

	// Add missing columns (idempotent)
	columnsToAdd := []struct {
		name string
		def  string
	}{
		{"embedding", "BLOB"},
		{"embedding_model", "TEXT"},
	}

	for _, col := range columnsToAdd {
		if !existingCols[col.name] {
			_, err = tx.ExecContext(ctx, fmt.Sprintf(
				`ALTER TABLE behaviors ADD COLUMN %s %s`, col.name, col.def))
			if err != nil {
				return fmt.Errorf("add %s column: %w", col.name, err)
			}
		}
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`,
		6)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}

// migrateV6ToV7 removes "avoid" from behavior content.
// Research (arxiv 2602.11988) found that including "what not to do" can prime
// LLMs to produce those patterns. The "wrong" action is now stored only as
// provenance on the Correction audit record, not in behavior content.
//
// Changes per behavior:
//   - content_structured: delete "avoid" key from JSON
//   - content_expanded: set to content_canonical (no more "avoid:..." text)
//   - mark as dirty so JSONL re-syncs
func migrateV6ToV7(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Query all behaviors that have structured content
	rows, err := tx.QueryContext(ctx,
		`SELECT id, content_structured FROM behaviors WHERE content_structured IS NOT NULL AND content_structured != ''`)
	if err != nil {
		return fmt.Errorf("query behaviors: %w", err)
	}

	type update struct {
		id         string
		structured string
	}
	var updates []update

	for rows.Next() {
		var id, structuredJSON string
		if err := rows.Scan(&id, &structuredJSON); err != nil {
			rows.Close()
			return fmt.Errorf("scan behavior: %w", err)
		}

		var structured map[string]interface{}
		if err := json.Unmarshal([]byte(structuredJSON), &structured); err != nil {
			continue // skip malformed JSON
		}

		if _, hasAvoid := structured["avoid"]; !hasAvoid {
			continue // nothing to migrate
		}

		delete(structured, "avoid")
		newJSON, err := json.Marshal(structured)
		if err != nil {
			continue
		}

		updates = append(updates, update{id: id, structured: string(newJSON)})
	}
	rows.Close()

	// Apply updates: strip avoid, set expanded = canonical, mark dirty
	for _, u := range updates {
		_, err = tx.ExecContext(ctx,
			`UPDATE behaviors SET content_structured = ?, content_expanded = content_canonical WHERE id = ?`,
			u.structured, u.id)
		if err != nil {
			return fmt.Errorf("update behavior %s: %w", u.id, err)
		}
	}

	// Also set expanded = canonical for all behaviors (even those without avoid),
	// since the expanded format has changed from "avoid: X\n\nInstead: Y" to just canonical.
	_, err = tx.ExecContext(ctx,
		`UPDATE behaviors SET content_expanded = content_canonical WHERE content_expanded IS NOT NULL AND content_expanded != content_canonical`)
	if err != nil {
		return fmt.Errorf("normalize expanded content: %w", err)
	}

	// Record schema version
	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`,
		7)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}

// migrateV7ToV8 removes the expanded content field.
// The Expanded field was made identical to Canonical in V7 and is now dead weight.
// SQLite cannot reliably DROP COLUMN, so the column stays in the DDL but is
// NULLed out and never read or written again.
func migrateV7ToV8(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// NULL out content_expanded (column kept for SQLite compat)
	_, err = tx.ExecContext(ctx,
		`UPDATE behaviors SET content_expanded = NULL WHERE content_expanded IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("null content_expanded: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`, 8)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}

// migrateV8ToV9 transforms scope values, adds memory type columns, and
// creates the events table for episodic memory.
//
// Scope changes:
//   - 'global' -> 'universal'
//   - 'local'  -> 'project:<projectID>' (or 'universal' if projectID is empty)
//
// New columns on behaviors:
//   - memory_type: 'semantic' (default), 'episodic', 'procedural'
//   - episode_data: JSON for episodic memory data
//   - workflow_data: JSON for workflow memory data
//
// New table: events (episodic memory event buffer)
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

	// Add new columns idempotently (safe if migration is retried after partial failure)
	existingCols := make(map[string]bool)
	colRows, err := tx.QueryContext(ctx, `PRAGMA table_info(behaviors)`)
	if err != nil {
		return fmt.Errorf("check behaviors columns: %w", err)
	}
	for colRows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue interface{}
		if err := colRows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			colRows.Close()
			return fmt.Errorf("scan column info: %w", err)
		}
		existingCols[name] = true
	}
	colRows.Close()
	if err := colRows.Err(); err != nil {
		return fmt.Errorf("iterating column info: %w", err)
	}

	v9Columns := []struct {
		name string
		def  string
	}{
		{"memory_type", "TEXT DEFAULT 'semantic'"},
		{"episode_data", "TEXT"},
		{"workflow_data", "TEXT"},
	}
	for _, col := range v9Columns {
		if !existingCols[col.name] {
			if _, err := tx.ExecContext(ctx,
				fmt.Sprintf(`ALTER TABLE behaviors ADD COLUMN %s %s`, col.name, col.def)); err != nil {
				return fmt.Errorf("add %s column: %w", col.name, err)
			}
		}
	}

	// Backfill memory_type for existing behavior types
	if _, err := tx.ExecContext(ctx,
		`UPDATE behaviors SET memory_type = 'procedural' WHERE behavior_type IN ('procedure', 'workflow')`); err != nil {
		return fmt.Errorf("backfill procedural memory_type: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE behaviors SET memory_type = 'episodic' WHERE behavior_type = 'episodic'`); err != nil {
		return fmt.Errorf("backfill episodic memory_type: %w", err)
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
			consolidated INTEGER DEFAULT 0,
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
	if _, err := tx.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_events_consolidated ON events(consolidated)`); err != nil {
		return fmt.Errorf("create events consolidated index: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_version (version, applied_at) VALUES (?, datetime('now'))`, 9)
	if err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit()
}

// validateStructuralIntegrity checks for SQLite database corruption.
// It only runs PRAGMA integrity_check — not foreign_key_check.
// Use ValidateIntegrity for full validation including FK checks.
func validateStructuralIntegrity(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("failed to run integrity_check: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("failed to scan integrity_check result: %w", err)
		}
		if result != "ok" {
			return fmt.Errorf("integrity_check failed: %s", result)
		}
	}
	return nil
}

// ValidateIntegrity runs SQLite integrity checks on the database.
// It runs PRAGMA integrity_check and PRAGMA foreign_key_check.
// Returns an error if any issues are found.
func ValidateIntegrity(ctx context.Context, db *sql.DB) error {
	// Run PRAGMA integrity_check
	rows, err := db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("failed to run integrity_check: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("failed to scan integrity_check result: %w", err)
		}
		if result != "ok" {
			return fmt.Errorf("integrity_check failed: %s", result)
		}
	}

	// Run PRAGMA foreign_key_check
	fkRows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("failed to run foreign_key_check: %w", err)
	}
	defer fkRows.Close()

	var fkErrors []string
	for fkRows.Next() {
		var table, rowid, parent, fkid string
		if err := fkRows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return fmt.Errorf("failed to scan foreign_key_check result: %w", err)
		}
		fkErrors = append(fkErrors, fmt.Sprintf("table=%s rowid=%s parent=%s fkid=%s", table, rowid, parent, fkid))
	}

	if len(fkErrors) > 0 {
		return fmt.Errorf("foreign_key_check failed: %v", fkErrors)
	}

	return nil
}

// ResetSchema drops all tables and recreates the schema.
// Only use for testing.
func ResetSchema(ctx context.Context, db *sql.DB) error {
	// Drop all tables
	tables := []string{
		"events",
		"co_activations",
		"dirty_behaviors",
		"behavior_stats",
		"behavior_when",
		"edges",
		"corrections",
		"behaviors",
		"export_state",
		"config",
		"schema_version",
	}

	// Drop triggers first
	triggers := []string{
		"behavior_insert_dirty",
		"behavior_update_dirty",
		"behavior_delete_dirty",
		"behavior_stats_dirty",
	}

	for _, trigger := range triggers {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP TRIGGER IF EXISTS %s", trigger)); err != nil {
			return fmt.Errorf("failed to drop trigger %s: %w", trigger, err)
		}
	}

	for _, table := range tables {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)); err != nil {
			return fmt.Errorf("failed to drop table %s: %w", table, err)
		}
	}

	// Recreate schema
	return InitSchema(ctx, db)
}
