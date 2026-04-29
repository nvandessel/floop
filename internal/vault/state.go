package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SyncState tracks the last sync state for vault operations.
type SyncState struct {
	MachineID        string    `json:"machine_id"`
	LastPush         time.Time `json:"last_push,omitempty"`
	LastPull         time.Time `json:"last_pull,omitempty"`
	LocalVectorRows  int       `json:"local_vector_rows"`
	RemoteVectorRows int       `json:"remote_vector_rows"`
	PushCount        int       `json:"push_count"`
	PullCount        int       `json:"pull_count"`
	PendingPush      bool      `json:"pending_push,omitempty"`
}

// LoadState reads sync state from path. Returns zero state if file is missing.
func LoadState(path string) (*SyncState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncState{}, nil
		}
		return nil, fmt.Errorf("reading sync state: %w", err)
	}
	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing sync state: %w", err)
	}
	return &state, nil
}

// SaveState writes sync state atomically (write to temp, then rename).
func SaveState(path string, state *SyncState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling sync state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("writing temp state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming state file: %w", err)
	}
	return nil
}

// Staleness returns the staleness category based on the last push time.
// Takes now as a parameter for deterministic testing.
func (s *SyncState) Staleness(now time.Time) string {
	if s.LastPush.IsZero() {
		return "very_stale"
	}
	age := now.Sub(s.LastPush)
	switch {
	case age > 7*24*time.Hour:
		return "very_stale"
	case age > 24*time.Hour:
		return "stale"
	default:
		return "fresh"
	}
}
