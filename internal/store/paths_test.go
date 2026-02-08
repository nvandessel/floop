package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalFloopPath(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"should return valid path", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GlobalFloopPath()
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobalFloopPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// Verify path ends with .floop
				if !strings.HasSuffix(got, ".floop") {
					t.Errorf("GlobalFloopPath() = %v, should end with .floop", got)
				}
				// Verify path is absolute
				if !filepath.IsAbs(got) {
					t.Errorf("GlobalFloopPath() = %v, should be absolute path", got)
				}
				// Verify path contains home directory
				homeDir, _ := os.UserHomeDir()
				if !strings.HasPrefix(got, homeDir) {
					t.Errorf("GlobalFloopPath() = %v, should start with home directory %v", got, homeDir)
				}
			}
		})
	}
}

func TestLocalFloopPath(t *testing.T) {
	tests := []struct {
		name        string
		projectRoot string
		want        string
	}{
		{
			name:        "unix path",
			projectRoot: "/home/user/project",
			want:        "/home/user/project/.floop",
		},
		{
			name:        "relative path",
			projectRoot: ".",
			want:        ".floop",
		},
		{
			name:        "nested project",
			projectRoot: "/var/projects/deep/nested/path",
			want:        "/var/projects/deep/nested/path/.floop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LocalFloopPath(tt.projectRoot)
			// Use filepath.ToSlash for cross-platform comparison
			gotNorm := filepath.ToSlash(got)
			wantNorm := filepath.ToSlash(tt.want)
			if gotNorm != wantNorm {
				t.Errorf("LocalFloopPath() = %v, want %v", gotNorm, wantNorm)
			}
		})
	}
}

func TestEnsureGlobalFloopDir(t *testing.T) {
	// Save original home directory
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	tests := []struct {
		name    string
		setup   func() (string, func())
		wantErr bool
	}{
		{
			name: "creates directory when it doesn't exist",
			setup: func() (string, func()) {
				// Create a temporary directory to use as fake home
				tmpHome, err := os.MkdirTemp("", "floop-test-home-*")
				if err != nil {
					t.Fatalf("failed to create temp dir: %v", err)
				}
				os.Setenv("HOME", tmpHome)
				cleanup := func() {
					os.RemoveAll(tmpHome)
				}
				return tmpHome, cleanup
			},
			wantErr: false,
		},
		{
			name: "succeeds when directory already exists",
			setup: func() (string, func()) {
				// Create a temporary directory with .floop already present
				tmpHome, err := os.MkdirTemp("", "floop-test-home-*")
				if err != nil {
					t.Fatalf("failed to create temp dir: %v", err)
				}
				os.Setenv("HOME", tmpHome)
				floopDir := filepath.Join(tmpHome, ".floop")
				os.MkdirAll(floopDir, 0700)
				cleanup := func() {
					os.RemoveAll(tmpHome)
				}
				return tmpHome, cleanup
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpHome, cleanup := tt.setup()
			defer cleanup()

			err := EnsureGlobalFloopDir()
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureGlobalFloopDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify the directory was created
				floopDir := filepath.Join(tmpHome, ".floop")
				info, err := os.Stat(floopDir)
				if err != nil {
					t.Errorf("EnsureGlobalFloopDir() did not create directory: %v", err)
					return
				}
				if !info.IsDir() {
					t.Errorf("EnsureGlobalFloopDir() created a file instead of directory")
				}
			}
		})
	}
}
