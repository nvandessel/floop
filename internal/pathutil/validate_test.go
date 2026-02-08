package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidatePath(t *testing.T) {
	// Create temp dirs to use as allowed dirs
	allowedDir := t.TempDir()
	otherDir := t.TempDir()

	// Create a subdirectory inside the allowed dir
	subDir := filepath.Join(allowedDir, "subdir")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	tests := []struct {
		name        string
		path        string
		allowedDirs []string
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid path inside allowed dir",
			path:        filepath.Join(allowedDir, "backup.json"),
			allowedDirs: []string{allowedDir},
			wantErr:     false,
		},
		{
			name:        "valid path in subdirectory of allowed dir",
			path:        filepath.Join(subDir, "backup.json"),
			allowedDirs: []string{allowedDir},
			wantErr:     false,
		},
		{
			name:        "path that is exactly the allowed dir",
			path:        allowedDir,
			allowedDirs: []string{allowedDir},
			wantErr:     false,
		},
		{
			name:        "path traversal with dot-dot",
			path:        filepath.Join(allowedDir, "..", "etc", "passwd"),
			allowedDirs: []string{allowedDir},
			wantErr:     true,
			errContains: "outside allowed directories",
		},
		{
			name:        "absolute path outside allowed dir",
			path:        filepath.Join(otherDir, "backup.json"),
			allowedDirs: []string{allowedDir},
			wantErr:     true,
			errContains: "outside allowed directories",
		},
		{
			name:        "null bytes in path",
			path:        filepath.Join(allowedDir, "back\x00up.json"),
			allowedDirs: []string{allowedDir},
			wantErr:     true,
			errContains: "null byte",
		},
		{
			name:        "path with redundant separators is cleaned",
			path:        allowedDir + string(os.PathSeparator) + string(os.PathSeparator) + "backup.json",
			allowedDirs: []string{allowedDir},
			wantErr:     false,
		},
		{
			name:        "empty path",
			path:        "",
			allowedDirs: []string{allowedDir},
			wantErr:     true,
			errContains: "empty",
		},
		{
			name:        "no allowed dirs",
			path:        filepath.Join(allowedDir, "backup.json"),
			allowedDirs: []string{},
			wantErr:     true,
			errContains: "no allowed directories",
		},
		{
			name:        "multiple allowed dirs - matches second",
			path:        filepath.Join(otherDir, "backup.json"),
			allowedDirs: []string{allowedDir, otherDir},
			wantErr:     false,
		},
		{
			name:        "path traversal with embedded dot-dot",
			path:        filepath.Join(allowedDir, "subdir", "..", "..", "etc", "passwd"),
			allowedDirs: []string{allowedDir},
			wantErr:     true,
			errContains: "outside allowed directories",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path, tt.allowedDirs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidatePath() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidatePath_SymlinkOutsideAllowedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	allowedDir := t.TempDir()
	outsideDir := t.TempDir()

	// Create a symlink inside the allowed dir that points outside
	symlinkPath := filepath.Join(allowedDir, "escape")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// A path through the symlink should be rejected
	err := ValidatePath(filepath.Join(symlinkPath, "backup.json"), []string{allowedDir})
	if err == nil {
		t.Error("ValidatePath() should reject symlink pointing outside allowed dir")
	}
	if err != nil && !strings.Contains(err.Error(), "outside allowed directories") {
		t.Errorf("ValidatePath() error = %v, want error about outside allowed directories", err)
	}
}

func TestValidatePath_SymlinkInsideAllowedDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	allowedDir := t.TempDir()

	// Create a subdirectory and a symlink to it (both inside allowed dir)
	realSubDir := filepath.Join(allowedDir, "real")
	if err := os.MkdirAll(realSubDir, 0700); err != nil {
		t.Fatalf("failed to create real subdir: %v", err)
	}

	symlinkPath := filepath.Join(allowedDir, "link")
	if err := os.Symlink(realSubDir, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// A path through a symlink that stays inside allowed dir should be OK
	err := ValidatePath(filepath.Join(symlinkPath, "backup.json"), []string{allowedDir})
	if err != nil {
		t.Errorf("ValidatePath() should accept symlink staying inside allowed dir, got: %v", err)
	}
}

func TestRedactPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"simple", "/home/user/.floop/config.yaml", ".../.floop/config.yaml"},
		{"deep", "/a/b/c/d/e.txt", ".../d/e.txt"},
		{"root file", "/file.txt", "file.txt"},
		{"relative", "dir/file.txt", ".../dir/file.txt"},
		{"just filename", "file.txt", "file.txt"},
		{"trailing slash cleaned", "/home/user/.floop/", ".../user/.floop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactPath(tt.input)
			if got != tt.want {
				t.Errorf("RedactPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultAllowedBackupDirs(t *testing.T) {
	dirs, err := DefaultAllowedBackupDirs()
	if err != nil {
		t.Fatalf("DefaultAllowedBackupDirs() error = %v", err)
	}

	if len(dirs) == 0 {
		t.Fatal("DefaultAllowedBackupDirs() returned no directories")
	}

	// First directory should be ~/.floop/backups/
	homeDir, _ := os.UserHomeDir()
	expectedGlobal := filepath.Join(homeDir, ".floop", "backups")
	if dirs[0] != expectedGlobal {
		t.Errorf("dirs[0] = %s, want %s", dirs[0], expectedGlobal)
	}
}

func TestDefaultAllowedBackupDirsWithProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()

	dirs, err := DefaultAllowedBackupDirsWithProjectRoot(projectRoot)
	if err != nil {
		t.Fatalf("DefaultAllowedBackupDirsWithProjectRoot() error = %v", err)
	}

	if len(dirs) < 2 {
		t.Fatalf("expected at least 2 directories, got %d", len(dirs))
	}

	// Should include the project-local .floop/backups/ dir
	expectedLocal := filepath.Join(projectRoot, ".floop", "backups")
	found := false
	for _, d := range dirs {
		if d == expectedLocal {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("dirs should contain %s, got %v", expectedLocal, dirs)
	}
}
