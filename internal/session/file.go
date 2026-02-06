package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// stateFile is the default session state filename.
const stateFile = "session-state.json"

// persistedState is the on-disk representation of session state.
// It captures only the data needed to resume a session across CLI hook invocations.
type persistedState struct {
	Config          Config                      `json:"config"`
	Injections      map[string]*InjectionRecord `json:"injections"`
	TotalTokensUsed int                         `json:"total_tokens_used"`
	PromptCount     int                         `json:"prompt_count"`
}

// SaveState persists the session state to a JSON file in the given directory.
// The directory must already exist.
func SaveState(s *State, dir string) error {
	s.mu.RLock()
	ps := persistedState{
		Config:          s.config,
		Injections:      s.injections,
		TotalTokensUsed: s.totalTokensUsed,
		PromptCount:     s.promptCount,
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling session state: %w", err)
	}

	path := filepath.Join(dir, stateFile)

	// Write atomically via temp file + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing session state temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Clean up temp file on rename failure.
		os.Remove(tmp)
		return fmt.Errorf("renaming session state file: %w", err)
	}

	return nil
}

// LoadState reads session state from a JSON file in the given directory.
// If the file does not exist, it returns a new State with the default config.
func LoadState(dir string) (*State, error) {
	path := filepath.Join(dir, stateFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewState(DefaultConfig()), nil
		}
		return nil, fmt.Errorf("reading session state: %w", err)
	}

	var ps persistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("unmarshaling session state: %w", err)
	}

	// Rebuild State from persisted data.
	injections := ps.Injections
	if injections == nil {
		injections = make(map[string]*InjectionRecord)
	}

	return &State{
		config:          ps.Config,
		injections:      injections,
		totalTokensUsed: ps.TotalTokensUsed,
		promptCount:     ps.PromptCount,
	}, nil
}

// StateFilePath returns the expected path for the session state file in the given directory.
func StateFilePath(dir string) string {
	return filepath.Join(dir, stateFile)
}

// RemoveState removes the session state file from the given directory.
// It is not an error if the file does not exist.
func RemoveState(dir string) error {
	path := filepath.Join(dir, stateFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing session state: %w", err)
	}
	return nil
}
