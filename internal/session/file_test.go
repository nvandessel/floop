package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestSaveAndLoadState(t *testing.T) {
	dir := t.TempDir()

	// Create state with some data.
	s := NewState(DefaultConfig())
	s.RecordInjection("b1", models.TierFull, 0.9, 100)
	s.RecordInjection("b2", models.TierSummary, 0.5, 50)
	s.IncrementPromptCount()
	s.IncrementPromptCount()

	// Save.
	if err := SaveState(s, dir); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	// Verify file exists.
	path := StateFilePath(dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("State file not found at %s: %v", path, err)
	}

	// Load.
	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}

	// Verify loaded state matches.
	if got := loaded.TotalTokensUsed(); got != 150 {
		t.Errorf("TotalTokensUsed() = %d, want 150", got)
	}
	if got := loaded.PromptCount(); got != 2 {
		t.Errorf("PromptCount() = %d, want 2", got)
	}
	if got := loaded.RemainingBudget(); got != 2850 {
		t.Errorf("RemainingBudget() = %d, want 2850", got)
	}

	rec := loaded.GetInjection("b1")
	if rec == nil {
		t.Fatal("GetInjection(b1) = nil after load")
	}
	if rec.Count != 1 {
		t.Errorf("b1 Count = %d, want 1", rec.Count)
	}
	if rec.Tier != models.TierFull {
		t.Errorf("b1 Tier = %v, want TierFull", rec.Tier)
	}

	rec = loaded.GetInjection("b2")
	if rec == nil {
		t.Fatal("GetInjection(b2) = nil after load")
	}
	if rec.Tier != models.TierSummary {
		t.Errorf("b2 Tier = %v, want TierSummary", rec.Tier)
	}
}

func TestLoadState_FileNotExist(t *testing.T) {
	dir := t.TempDir()

	s, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState() error = %v, want nil for missing file", err)
	}
	if s == nil {
		t.Fatal("LoadState() returned nil state, want default state")
	}
	if got := s.RemainingBudget(); got != 3000 {
		t.Errorf("RemainingBudget() = %d, want 3000 (default)", got)
	}
}

func TestLoadState_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, stateFile)

	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}

	_, err := LoadState(dir)
	if err == nil {
		t.Fatal("LoadState() error = nil for corrupt file, want error")
	}
}

func TestRemoveState(t *testing.T) {
	dir := t.TempDir()

	// Save some state.
	s := NewState(DefaultConfig())
	s.RecordInjection("b1", models.TierFull, 0.9, 100)
	if err := SaveState(s, dir); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	// Remove.
	if err := RemoveState(dir); err != nil {
		t.Fatalf("RemoveState() error = %v", err)
	}

	// Verify file gone.
	path := StateFilePath(dir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("State file still exists after remove at %s", path)
	}

	// Remove again should not error.
	if err := RemoveState(dir); err != nil {
		t.Errorf("RemoveState() on already-removed = %v, want nil", err)
	}
}
