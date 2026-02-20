package store

import (
	"context"
	"testing"
	"time"
)

func TestRecordCoActivation_Basic(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLiteStore(t)

	now := time.Now()
	err := s.RecordCoActivation(ctx, "a:b", now)
	if err != nil {
		t.Fatalf("RecordCoActivation failed: %v", err)
	}

	// Should be retrievable
	times, err := s.GetCoActivations(ctx, "a:b", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("GetCoActivations failed: %v", err)
	}
	if len(times) != 1 {
		t.Fatalf("expected 1 co-activation, got %d", len(times))
	}
}

func TestRecordCoActivation_MultipleSamePair(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLiteStore(t)

	now := time.Now()
	for i := range 3 {
		err := s.RecordCoActivation(ctx, "a:b", now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("RecordCoActivation %d failed: %v", i, err)
		}
	}

	times, err := s.GetCoActivations(ctx, "a:b", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("GetCoActivations failed: %v", err)
	}
	if len(times) != 3 {
		t.Fatalf("expected 3 co-activations, got %d", len(times))
	}
}

func TestGetCoActivations_WindowFiltering(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLiteStore(t)

	now := time.Now()
	// Insert one old and one recent
	err := s.RecordCoActivation(ctx, "a:b", now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("RecordCoActivation (old) failed: %v", err)
	}
	err = s.RecordCoActivation(ctx, "a:b", now)
	if err != nil {
		t.Fatalf("RecordCoActivation (new) failed: %v", err)
	}

	// Query with 1-hour window — should only get the recent one
	times, err := s.GetCoActivations(ctx, "a:b", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("GetCoActivations failed: %v", err)
	}
	if len(times) != 1 {
		t.Fatalf("expected 1 co-activation within window, got %d", len(times))
	}
}

func TestGetCoActivations_EmptyResult(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLiteStore(t)

	times, err := s.GetCoActivations(ctx, "nonexistent:pair", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("GetCoActivations failed: %v", err)
	}
	if len(times) != 0 {
		t.Fatalf("expected 0 co-activations for unknown pair, got %d", len(times))
	}
}

func TestPruneCoActivations_RemovesOld(t *testing.T) {
	ctx := context.Background()
	s := newTestSQLiteStore(t)

	now := time.Now()
	// Insert old and new entries
	_ = s.RecordCoActivation(ctx, "a:b", now.Add(-48*time.Hour))
	_ = s.RecordCoActivation(ctx, "a:b", now.Add(-25*time.Hour))
	_ = s.RecordCoActivation(ctx, "a:b", now)
	_ = s.RecordCoActivation(ctx, "c:d", now.Add(-48*time.Hour))

	// Prune entries older than 24 hours
	n, err := s.PruneCoActivations(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneCoActivations failed: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 pruned, got %d", n)
	}

	// Only the recent one should remain
	times, err := s.GetCoActivations(ctx, "a:b", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("GetCoActivations failed: %v", err)
	}
	if len(times) != 1 {
		t.Errorf("expected 1 remaining co-activation, got %d", len(times))
	}
}

// newTestSQLiteStore creates a SQLiteGraphStore backed by a temp directory for testing.
func newTestSQLiteStore(t *testing.T) *SQLiteGraphStore {
	t.Helper()
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
