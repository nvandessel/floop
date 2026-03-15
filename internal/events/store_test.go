package events

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStore(t *testing.T) *SQLiteEventStore {
	t.Helper()
	db := newTestDB(t)
	store := NewSQLiteEventStore(db)
	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	return store
}

func TestAddAndGetBySession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	event := Event{
		ID:        "evt-1",
		SessionID: "session-abc",
		Timestamp: now,
		Source:    "test",
		Actor:     ActorUser,
		Kind:      KindMessage,
		Content:   "hello world",
		Metadata:  map[string]any{"key": "value"},
		ProjectID: "proj-1",
		Provenance: &EventProvenance{
			Model:  "gpt-4",
			Branch: "main",
		},
		CreatedAt: now,
	}

	if err := store.Add(ctx, event); err != nil {
		t.Fatalf("Add: %v", err)
	}

	events, err := store.GetBySession(ctx, "session-abc")
	if err != nil {
		t.Fatalf("GetBySession: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	got := events[0]
	if got.ID != "evt-1" {
		t.Errorf("ID = %q, want %q", got.ID, "evt-1")
	}
	if got.SessionID != "session-abc" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "session-abc")
	}
	if got.Actor != ActorUser {
		t.Errorf("Actor = %q, want %q", got.Actor, ActorUser)
	}
	if got.Kind != KindMessage {
		t.Errorf("Kind = %q, want %q", got.Kind, KindMessage)
	}
	if got.Content != "hello world" {
		t.Errorf("Content = %q, want %q", got.Content, "hello world")
	}
	if got.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, "proj-1")
	}
	if got.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %v, want %q", got.Metadata["key"], "value")
	}
	if got.Provenance == nil {
		t.Fatal("Provenance is nil, expected non-nil")
	}
	if got.Provenance.Model != "gpt-4" {
		t.Errorf("Provenance.Model = %q, want %q", got.Provenance.Model, "gpt-4")
	}
	if got.Provenance.Branch != "main" {
		t.Errorf("Provenance.Branch = %q, want %q", got.Provenance.Branch, "main")
	}
}

func TestAddAndGetBySession_NullOptionalFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	event := Event{
		ID:        "evt-null",
		SessionID: "session-null",
		Timestamp: now,
		Source:    "test",
		Actor:     ActorAgent,
		Kind:      KindAction,
		Content:   "no metadata",
		CreatedAt: now,
	}

	if err := store.Add(ctx, event); err != nil {
		t.Fatalf("Add: %v", err)
	}

	events, err := store.GetBySession(ctx, "session-null")
	if err != nil {
		t.Fatalf("GetBySession: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	got := events[0]
	if got.Metadata != nil {
		t.Errorf("Metadata = %v, want nil", got.Metadata)
	}
	if got.Provenance != nil {
		t.Errorf("Provenance = %v, want nil", got.Provenance)
	}
	if got.ProjectID != "" {
		t.Errorf("ProjectID = %q, want empty", got.ProjectID)
	}
}

func TestAddBatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	events := []Event{
		{
			ID:        "evt-batch-1",
			SessionID: "session-batch",
			Timestamp: now,
			Source:    "test",
			Actor:     ActorUser,
			Kind:      KindMessage,
			Content:   "first message",
			CreatedAt: now,
		},
		{
			ID:        "evt-batch-2",
			SessionID: "session-batch",
			Timestamp: now.Add(time.Second),
			Source:    "test",
			Actor:     ActorAgent,
			Kind:      KindMessage,
			Content:   "second message",
			CreatedAt: now,
		},
		{
			ID:        "evt-batch-3",
			SessionID: "session-batch",
			Timestamp: now.Add(2 * time.Second),
			Source:    "test",
			Actor:     ActorTool,
			Kind:      KindResult,
			Content:   "tool output",
			CreatedAt: now,
		},
	}

	if err := store.AddBatch(ctx, events); err != nil {
		t.Fatalf("AddBatch: %v", err)
	}

	got, err := store.GetBySession(ctx, "session-batch")
	if err != nil {
		t.Fatalf("GetBySession: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}

	// Verify ordering by timestamp
	if got[0].ID != "evt-batch-1" {
		t.Errorf("first event ID = %q, want %q", got[0].ID, "evt-batch-1")
	}
	if got[1].ID != "evt-batch-2" {
		t.Errorf("second event ID = %q, want %q", got[1].ID, "evt-batch-2")
	}
	if got[2].ID != "evt-batch-3" {
		t.Errorf("third event ID = %q, want %q", got[2].ID, "evt-batch-3")
	}
}

func TestPrune(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	old := now.Add(-48 * time.Hour)   // 2 days ago
	recent := now.Add(-1 * time.Hour) // 1 hour ago

	events := []Event{
		{
			ID:        "evt-old-1",
			SessionID: "session-prune",
			Timestamp: old,
			Source:    "test",
			Actor:     ActorUser,
			Kind:      KindMessage,
			Content:   "old message 1",
			CreatedAt: old,
		},
		{
			ID:        "evt-old-2",
			SessionID: "session-prune",
			Timestamp: old.Add(time.Second),
			Source:    "test",
			Actor:     ActorAgent,
			Kind:      KindMessage,
			Content:   "old message 2",
			CreatedAt: old,
		},
		{
			ID:        "evt-recent",
			SessionID: "session-prune",
			Timestamp: recent,
			Source:    "test",
			Actor:     ActorUser,
			Kind:      KindMessage,
			Content:   "recent message",
			CreatedAt: recent,
		},
	}

	if err := store.AddBatch(ctx, events); err != nil {
		t.Fatalf("AddBatch: %v", err)
	}

	// Mark old events as consolidated so they can be pruned
	if err := store.MarkConsolidated(ctx, []string{"evt-old-1", "evt-old-2"}); err != nil {
		t.Fatalf("MarkConsolidated: %v", err)
	}

	// Prune consolidated events older than 24 hours
	pruned, err := store.Prune(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2", pruned)
	}

	// Verify only recent event remains
	remaining, err := store.GetBySession(ctx, "session-prune")
	if err != nil {
		t.Fatalf("GetBySession after prune: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining event, got %d", len(remaining))
	}
	if remaining[0].ID != "evt-recent" {
		t.Errorf("remaining event ID = %q, want %q", remaining[0].ID, "evt-recent")
	}
}

func TestCount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Empty store
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("empty count = %d, want 0", count)
	}

	// Add events
	now := time.Now().Truncate(time.Microsecond)
	events := []Event{
		{
			ID:        "evt-count-1",
			SessionID: "session-count",
			Timestamp: now,
			Source:    "test",
			Actor:     ActorUser,
			Kind:      KindMessage,
			Content:   "msg 1",
			CreatedAt: now,
		},
		{
			ID:        "evt-count-2",
			SessionID: "session-count",
			Timestamp: now.Add(time.Second),
			Source:    "test",
			Actor:     ActorAgent,
			Kind:      KindMessage,
			Content:   "msg 2",
			CreatedAt: now,
		},
	}

	if err := store.AddBatch(ctx, events); err != nil {
		t.Fatalf("AddBatch: %v", err)
	}

	count, err = store.Count(ctx)
	if err != nil {
		t.Fatalf("Count after add: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestGetSince(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	events := []Event{
		{
			ID:        "evt-since-1",
			SessionID: "session-since",
			Timestamp: now.Add(-2 * time.Hour),
			Source:    "test",
			Actor:     ActorUser,
			Kind:      KindMessage,
			Content:   "early message",
			CreatedAt: now,
		},
		{
			ID:        "evt-since-2",
			SessionID: "session-since",
			Timestamp: now.Add(-30 * time.Minute),
			Source:    "test",
			Actor:     ActorAgent,
			Kind:      KindMessage,
			Content:   "later message",
			CreatedAt: now,
		},
	}

	if err := store.AddBatch(ctx, events); err != nil {
		t.Fatalf("AddBatch: %v", err)
	}

	// Get events since 1 hour ago
	got, err := store.GetSince(ctx, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("GetSince: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].ID != "evt-since-2" {
		t.Errorf("event ID = %q, want %q", got[0].ID, "evt-since-2")
	}
}

func TestGetUnconsolidated(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	event := Event{
		ID:        "evt-uncons",
		SessionID: "session-uncons",
		Timestamp: now,
		Source:    "test",
		Actor:     ActorUser,
		Kind:      KindMessage,
		Content:   "unconsolidated event",
		CreatedAt: now,
	}

	if err := store.Add(ctx, event); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := store.GetUnconsolidated(ctx)
	if err != nil {
		t.Fatalf("GetUnconsolidated: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 unconsolidated event, got %d", len(got))
	}
	if got[0].ID != "evt-uncons" {
		t.Errorf("event ID = %q, want %q", got[0].ID, "evt-uncons")
	}
}

func TestMarkConsolidated(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Microsecond)
	event := Event{
		ID:        "evt-mark",
		SessionID: "session-mark",
		Timestamp: now,
		Source:    "test",
		Actor:     ActorUser,
		Kind:      KindMessage,
		Content:   "to be consolidated",
		CreatedAt: now,
	}

	if err := store.Add(ctx, event); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Verify it appears as unconsolidated
	got, err := store.GetUnconsolidated(ctx)
	if err != nil {
		t.Fatalf("GetUnconsolidated before mark: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 unconsolidated event, got %d", len(got))
	}

	// Mark it as consolidated
	if err := store.MarkConsolidated(ctx, []string{"evt-mark"}); err != nil {
		t.Fatalf("MarkConsolidated: %v", err)
	}

	// Verify GetUnconsolidated returns empty
	got, err = store.GetUnconsolidated(ctx)
	if err != nil {
		t.Fatalf("GetUnconsolidated after mark: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 unconsolidated events after mark, got %d", len(got))
	}
}

func TestMarkConsolidated_Empty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Should be a no-op, not an error
	if err := store.MarkConsolidated(ctx, []string{}); err != nil {
		t.Fatalf("MarkConsolidated with empty slice: %v", err)
	}
}
