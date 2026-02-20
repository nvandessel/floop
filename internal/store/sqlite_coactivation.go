package store

import (
	"context"
	"fmt"
	"time"
)

// RecordCoActivation records a co-activation timestamp for a behavior pair.
func (s *SQLiteGraphStore) RecordCoActivation(ctx context.Context, pairKey string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO co_activations (pair_key, activated_at) VALUES (?, ?)`,
		pairKey, at.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record co-activation: %w", err)
	}
	return nil
}

// GetCoActivations returns co-activation timestamps for a pair since the cutoff.
func (s *SQLiteGraphStore) GetCoActivations(ctx context.Context, pairKey string, since time.Time) ([]time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT activated_at FROM co_activations WHERE pair_key = ? AND activated_at > ? ORDER BY activated_at`,
		pairKey, since.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("get co-activations: %w", err)
	}
	defer rows.Close()

	var times []time.Time
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			return nil, fmt.Errorf("scan co-activation: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, fmt.Errorf("parse co-activation time: %w", err)
		}
		times = append(times, t)
	}
	return times, nil
}

// PruneCoActivations removes entries older than the given cutoff. Returns count removed.
func (s *SQLiteGraphStore) PruneCoActivations(ctx context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`DELETE FROM co_activations WHERE activated_at < ?`,
		before.Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("prune co-activations: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune co-activations rows affected: %w", err)
	}
	return int(n), nil
}
