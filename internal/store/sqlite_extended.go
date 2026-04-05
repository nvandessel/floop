package store

import (
	"context"
	"fmt"
	"time"
)

// UpdateConfidence efficiently updates just the confidence value for a behavior.
func (s *SQLiteGraphStore) UpdateConfidence(ctx context.Context, behaviorID string, newConfidence float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`UPDATE behaviors SET confidence = ?, updated_at = ? WHERE id = ?`,
		newConfidence, time.Now().Format(time.RFC3339), behaviorID)
	if err != nil {
		return fmt.Errorf("failed to update confidence for %s: %w", behaviorID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("behavior not found: %s", behaviorID)
	}

	return nil
}

// RecordActivationHit increments times_activated and updates last_activated
// for a behavior. This is called as a background side-effect of floop_active.
func (s *SQLiteGraphStore) RecordActivationHit(ctx context.Context, behaviorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE behavior_stats SET times_activated = times_activated + 1, last_activated = ? WHERE behavior_id = ?`,
		now, behaviorID)
	if err != nil {
		return fmt.Errorf("failed to record activation hit for %s: %w", behaviorID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("behavior not found: %s", behaviorID)
	}

	return nil
}

// RecordConfirmed increments times_confirmed and updates last_confirmed for a behavior.
// This is called when the user explicitly confirms or implicitly continues using a behavior.
func (s *SQLiteGraphStore) RecordConfirmed(ctx context.Context, behaviorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE behavior_stats SET times_confirmed = times_confirmed + 1, last_confirmed = ? WHERE behavior_id = ?`,
		now, behaviorID)
	if err != nil {
		return fmt.Errorf("failed to record confirmed for %s: %w", behaviorID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("behavior not found: %s", behaviorID)
	}

	return nil
}

// RecordOverridden increments times_overridden for a behavior.
// This is called when the user or agent contradicted the behavior.
func (s *SQLiteGraphStore) RecordOverridden(ctx context.Context, behaviorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`UPDATE behavior_stats SET times_overridden = times_overridden + 1 WHERE behavior_id = ?`,
		behaviorID)
	if err != nil {
		return fmt.Errorf("failed to record overridden for %s: %w", behaviorID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("behavior not found: %s", behaviorID)
	}

	return nil
}

// TouchEdges updates last_activated on all edges where the source or target
// is one of the given behavior IDs. This enables temporal decay on edge
// weights in the spreading activation engine.
func (s *SQLiteGraphStore) TouchEdges(ctx context.Context, behaviorIDs []string) error {
	if len(behaviorIDs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build parameterized IN clause
	placeholders := make([]string, len(behaviorIDs))
	args := make([]interface{}, 0, 1+2*len(behaviorIDs))
	now := time.Now().Format(time.RFC3339)
	args = append(args, now)
	for i, id := range behaviorIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	inClause := joinStrings(placeholders, ",")

	// Duplicate the IDs for the OR target IN clause
	for _, id := range behaviorIDs {
		args = append(args, id)
	}

	// G201: inClause is only "?,?,?" placeholders built from len(behaviorIDs) — no user input.
	query := fmt.Sprintf( //nolint:gosec // placeholder-only IN clause
		`UPDATE edges SET last_activated = ? WHERE source IN (%s) OR target IN (%s)`,
		inClause, inClause)

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to touch edges: %w", err)
	}

	return nil
}

// BatchUpdateEdgeWeights updates the weights of multiple edges in a single transaction.
// Only existing edges matching (source, target, kind) are updated.
func (s *SQLiteGraphStore) BatchUpdateEdgeWeights(ctx context.Context, updates []EdgeWeightUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin batch edge weight update: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE edges SET weight = ? WHERE source = ? AND target = ? AND kind = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare batch edge weight update: %w", err)
	}
	defer stmt.Close()

	for _, u := range updates {
		if _, err := stmt.ExecContext(ctx, u.NewWeight, u.Source, u.Target, u.Kind); err != nil {
			return fmt.Errorf("update edge weight (%s→%s, %s): %w", u.Source, u.Target, u.Kind, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.bumpVersion()
	return nil
}

// PruneWeakEdges removes all edges of the given kind whose weight is at or below the threshold.
func (s *SQLiteGraphStore) PruneWeakEdges(ctx context.Context, kind EdgeKind, threshold float64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM edges WHERE kind = ? AND weight <= ?
	`, kind, threshold)
	if err != nil {
		return 0, fmt.Errorf("prune weak edges: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune weak edges rows affected: %w", err)
	}
	s.bumpVersion()
	return int(n), nil
}
