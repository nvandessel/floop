package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EventStore defines the interface for event persistence.
type EventStore interface {
	Add(ctx context.Context, event Event) error
	AddBatch(ctx context.Context, events []Event) error
	GetBySession(ctx context.Context, sessionID string) ([]Event, error)
	GetSince(ctx context.Context, since time.Time) ([]Event, error)
	GetUnconsolidated(ctx context.Context) ([]Event, error)
	MarkConsolidated(ctx context.Context, ids []string) error
	Prune(ctx context.Context, olderThan time.Duration) (int, error)
	Count(ctx context.Context) (int, error)
}

// SQLiteEventStore implements EventStore using SQLite.
type SQLiteEventStore struct {
	db *sql.DB
}

// NewSQLiteEventStore creates a new SQLiteEventStore backed by the given database.
func NewSQLiteEventStore(db *sql.DB) *SQLiteEventStore {
	return &SQLiteEventStore{db: db}
}

// InitSchema creates the events table if it does not exist.
func (s *SQLiteEventStore) InitSchema(ctx context.Context) error {
	// DDL matches internal/store/schema.go schemaV1 and migrateV8ToV9 — single source of truth.
	_, err := s.db.ExecContext(ctx, `
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
	`)
	if err != nil {
		return fmt.Errorf("initializing events schema: %w", err)
	}
	return nil
}

// Add inserts a single event into the store.
func (s *SQLiteEventStore) Add(ctx context.Context, event Event) error {
	metadataJSON, err := marshalNullableMap(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	provenanceJSON, err := marshalNullableProvenance(event.Provenance)
	if err != nil {
		return fmt.Errorf("marshaling provenance: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO events (id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance, consolidated, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
	`,
		event.ID,
		event.SessionID,
		event.Timestamp.Format(time.RFC3339Nano),
		event.Source,
		string(event.Actor),
		string(event.Kind),
		event.Content,
		metadataJSON,
		nullString(event.ProjectID),
		provenanceJSON,
		event.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("inserting event: %w", err)
	}
	return nil
}

// AddBatch inserts multiple events in a single transaction.
func (s *SQLiteEventStore) AddBatch(ctx context.Context, events []Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO events (id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance, consolidated, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, event := range events {
		metadataJSON, err := marshalNullableMap(event.Metadata)
		if err != nil {
			return fmt.Errorf("marshaling metadata for event %s: %w", event.ID, err)
		}

		provenanceJSON, err := marshalNullableProvenance(event.Provenance)
		if err != nil {
			return fmt.Errorf("marshaling provenance for event %s: %w", event.ID, err)
		}

		_, err = stmt.ExecContext(ctx,
			event.ID,
			event.SessionID,
			event.Timestamp.Format(time.RFC3339Nano),
			event.Source,
			string(event.Actor),
			string(event.Kind),
			event.Content,
			metadataJSON,
			nullString(event.ProjectID),
			provenanceJSON,
			event.CreatedAt.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("inserting event %s: %w", event.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing batch: %w", err)
	}
	return nil
}

// GetBySession retrieves all events for a given session, ordered by timestamp.
// Returns both consolidated and unconsolidated events — callers filter as needed.
func (s *SQLiteEventStore) GetBySession(ctx context.Context, sessionID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance, created_at
		FROM events
		WHERE session_id = ?
		ORDER BY timestamp ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("querying events by session: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// GetSince retrieves unconsolidated events since the given time, ordered by timestamp.
func (s *SQLiteEventStore) GetSince(ctx context.Context, since time.Time) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance, created_at
		FROM events
		WHERE timestamp >= ? AND consolidated = 0
		ORDER BY timestamp ASC
	`, since.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("querying events since %v: %w", since, err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// GetUnconsolidated retrieves all events that have not yet been consolidated.
func (s *SQLiteEventStore) GetUnconsolidated(ctx context.Context) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, timestamp, source, actor, kind, content, metadata, project_id, provenance, created_at
		FROM events
		WHERE consolidated = 0
		ORDER BY timestamp ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying unconsolidated events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// MarkConsolidated marks the given event IDs as consolidated.
func (s *SQLiteEventStore) MarkConsolidated(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	// Placeholders are all "?" — safe from injection. gosec flags fmt.Sprintf with SQL.
	query := "UPDATE events SET consolidated = 1 WHERE id IN (" + strings.Join(placeholders, ",") + ")" //nolint:gosec // parameterized placeholders only
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mark consolidated: %w", err)
	}
	return nil
}

// Prune deletes events older than the given duration and returns the count deleted.
// All events past the retention window are pruned regardless of consolidated status —
// the retention window is the safety net for events that never matched any heuristic.
func (s *SQLiteEventStore) Prune(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM events
		WHERE timestamp < ?
	`, cutoff.Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("pruning events: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}
	return int(n), nil
}

// Count returns the total number of events in the store.
func (s *SQLiteEventStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting events: %w", err)
	}
	return count, nil
}

// scanEvents reads rows into a slice of Event.
func scanEvents(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var (
			e             Event
			tsStr         string
			createdStr    string
			metadataRaw   sql.NullString
			projectID     sql.NullString
			provenanceRaw sql.NullString
		)

		err := rows.Scan(
			&e.ID,
			&e.SessionID,
			&tsStr,
			&e.Source,
			&e.Actor,
			&e.Kind,
			&e.Content,
			&metadataRaw,
			&projectID,
			&provenanceRaw,
			&createdStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}

		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			return nil, fmt.Errorf("parsing timestamp %q: %w", tsStr, err)
		}
		e.Timestamp = ts

		created, err := time.Parse(time.RFC3339Nano, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parsing created_at %q: %w", createdStr, err)
		}
		e.CreatedAt = created

		if projectID.Valid {
			e.ProjectID = projectID.String
		}

		if metadataRaw.Valid && metadataRaw.String != "" {
			var m map[string]any
			if err := json.Unmarshal([]byte(metadataRaw.String), &m); err != nil {
				return nil, fmt.Errorf("unmarshaling metadata: %w", err)
			}
			e.Metadata = m
		}

		if provenanceRaw.Valid && provenanceRaw.String != "" {
			var p EventProvenance
			if err := json.Unmarshal([]byte(provenanceRaw.String), &p); err != nil {
				return nil, fmt.Errorf("unmarshaling provenance: %w", err)
			}
			e.Provenance = &p
		}

		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating event rows: %w", err)
	}

	return events, nil
}

// marshalNullableMap returns a sql.NullString containing JSON for a map, or null if the map is nil.
func marshalNullableMap(m map[string]any) (sql.NullString, error) {
	if m == nil {
		return sql.NullString{}, nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}

// marshalNullableProvenance returns a sql.NullString containing JSON for provenance, or null if nil.
func marshalNullableProvenance(p *EventProvenance) (sql.NullString, error) {
	if p == nil {
		return sql.NullString{}, nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}

// nullString returns a sql.NullString that is valid only if s is non-empty.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
