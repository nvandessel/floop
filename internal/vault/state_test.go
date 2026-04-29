package vault

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault-state.json")

	want := &SyncState{
		MachineID:        "workstation",
		LastPush:         time.Date(2026, 4, 14, 10, 30, 0, 0, time.UTC),
		LastPull:         time.Date(2026, 4, 14, 8, 0, 0, 0, time.UTC),
		LocalVectorRows:  142,
		RemoteVectorRows: 140,
		PushCount:        42,
		PullCount:        38,
	}

	if err := SaveState(path, want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if got.MachineID != want.MachineID {
		t.Errorf("MachineID = %q, want %q", got.MachineID, want.MachineID)
	}
	if !got.LastPush.Equal(want.LastPush) {
		t.Errorf("LastPush = %v, want %v", got.LastPush, want.LastPush)
	}
	if !got.LastPull.Equal(want.LastPull) {
		t.Errorf("LastPull = %v, want %v", got.LastPull, want.LastPull)
	}
	if got.LocalVectorRows != want.LocalVectorRows {
		t.Errorf("LocalVectorRows = %d, want %d", got.LocalVectorRows, want.LocalVectorRows)
	}
	if got.PushCount != want.PushCount {
		t.Errorf("PushCount = %d, want %d", got.PushCount, want.PushCount)
	}
}

func TestSyncState_MissingFile(t *testing.T) {
	got, err := LoadState(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MachineID != "" {
		t.Errorf("expected zero state, got MachineID=%q", got.MachineID)
	}
	if got.PushCount != 0 {
		t.Errorf("expected zero state, got PushCount=%d", got.PushCount)
	}
}

func TestSyncState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault-state.json")

	state := &SyncState{MachineID: "test"}
	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Verify temp file is cleaned up
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("temp file should not exist after successful save")
	}

	// Verify final file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should exist: %v", err)
	}
}

func TestSyncState_Staleness(t *testing.T) {
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		lastPush time.Time
		want     string
	}{
		{"never pushed", time.Time{}, "very_stale"},
		{"30 min ago", now.Add(-30 * time.Minute), "fresh"},
		{"2 hours ago", now.Add(-2 * time.Hour), "fresh"},
		{"23 hours ago", now.Add(-23 * time.Hour), "fresh"},
		{"25 hours ago", now.Add(-25 * time.Hour), "stale"},
		{"3 days ago", now.Add(-3 * 24 * time.Hour), "stale"},
		{"8 days ago", now.Add(-8 * 24 * time.Hour), "very_stale"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SyncState{LastPush: tt.lastPush}
			got := s.Staleness(now)
			if got != tt.want {
				t.Errorf("Staleness() = %q, want %q", got, tt.want)
			}
		})
	}
}
