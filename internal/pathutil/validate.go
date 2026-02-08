// Package pathutil provides path validation utilities for securing file operations.
package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RedactPath reduces a full path to .../<parent>/<basename> for safe error messages.
// For example, "/home/user/.floop/config.yaml" becomes ".../.floop/config.yaml".
func RedactPath(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	dir := filepath.Dir(cleaned)
	base := filepath.Base(cleaned)
	parent := filepath.Base(dir)
	if parent == "." || parent == string(filepath.Separator) {
		return base
	}
	return ".../" + parent + "/" + base
}

// ValidatePath checks that a file path is within one of the allowed directories.
// It resolves symlinks, cleans the path, and rejects traversal attempts.
func ValidatePath(path string, allowedDirs []string) error {
	if path == "" {
		return fmt.Errorf("path validation failed: path is empty")
	}

	if len(allowedDirs) == 0 {
		return fmt.Errorf("path validation failed: no allowed directories configured")
	}

	// Check for null bytes (common injection vector)
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("path validation failed: path contains null byte")
	}

	// Clean and make absolute
	cleaned := filepath.Clean(path)
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return fmt.Errorf("path validation failed: cannot resolve absolute path: %w", err)
	}

	// Resolve symlinks on the parent directory (the file itself may not exist yet).
	// This prevents symlink-based escapes where a directory inside the allowed
	// tree is actually a symlink pointing outside.
	dir := filepath.Dir(absPath)
	resolvedDir, err := resolveExistingParent(dir)
	if err != nil {
		return fmt.Errorf("path validation failed: cannot resolve parent directory: %w", err)
	}

	// Reconstruct the full resolved path
	resolvedPath := filepath.Join(resolvedDir, filepath.Base(absPath))

	// Check that the resolved path is inside one of the allowed directories
	for _, allowed := range allowedDirs {
		allowedClean := filepath.Clean(allowed)
		allowedAbs, err := filepath.Abs(allowedClean)
		if err != nil {
			continue
		}
		// Also resolve symlinks in the allowed directory itself
		allowedResolved, err := resolveExistingParent(allowedAbs)
		if err != nil {
			continue
		}

		if isSubpath(resolvedPath, allowedResolved) {
			return nil
		}
	}

	return fmt.Errorf("path validation failed: %q is outside allowed directories", RedactPath(absPath))
}

// resolveExistingParent walks up the directory tree to find the deepest existing
// ancestor, resolves symlinks on it, then re-appends the non-existent tail.
// This handles cases where the target file or some parent directories don't exist yet.
func resolveExistingParent(dir string) (string, error) {
	// Try to resolve the full path first
	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil {
		return resolved, nil
	}

	// Walk up until we find an existing directory
	parent := filepath.Dir(dir)
	if parent == dir {
		// We've hit the root and it doesn't exist -- give up
		return "", fmt.Errorf("cannot resolve path: %s", RedactPath(dir))
	}

	resolvedParent, err := resolveExistingParent(parent)
	if err != nil {
		return "", err
	}

	return filepath.Join(resolvedParent, filepath.Base(dir)), nil
}

// isSubpath checks whether path is equal to or a subdirectory of base.
func isSubpath(path, base string) bool {
	if path == base {
		return true
	}
	// Ensure base ends with separator so "/tmp/foo" doesn't match "/tmp/foobar"
	prefix := base + string(os.PathSeparator)
	return strings.HasPrefix(path, prefix)
}

// DefaultAllowedBackupDirs returns the directories where backups are allowed.
// Returns: ~/.floop/backups/
func DefaultAllowedBackupDirs() ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return []string{
		filepath.Join(homeDir, ".floop", "backups"),
	}, nil
}

// DefaultAllowedBackupDirsWithProjectRoot returns the directories where backups
// are allowed, including a project-local directory.
// Returns: ~/.floop/backups/ and <projectRoot>/.floop/backups/
func DefaultAllowedBackupDirsWithProjectRoot(projectRoot string) ([]string, error) {
	dirs, err := DefaultAllowedBackupDirs()
	if err != nil {
		return nil, err
	}
	dirs = append(dirs, filepath.Join(projectRoot, ".floop", "backups"))
	return dirs, nil
}
