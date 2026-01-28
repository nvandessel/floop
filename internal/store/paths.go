// Package store provides graph storage implementations.
package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// GlobalFloopPath returns the path to the global .floop directory.
// On Unix: ~/.floop
// On Windows: %USERPROFILE%\.floop
func GlobalFloopPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".floop"), nil
}

// LocalFloopPath returns the path to the local .floop directory
// for the given project root.
func LocalFloopPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".floop")
}

// EnsureGlobalFloopDir creates the global .floop directory if it doesn't exist.
// Returns nil if the directory already exists or was successfully created.
func EnsureGlobalFloopDir() error {
	globalPath, err := GlobalFloopPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(globalPath, 0755); err != nil {
		return fmt.Errorf("failed to create global .floop directory: %w", err)
	}

	return nil
}
