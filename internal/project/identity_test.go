package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectID(t *testing.T) {
	t.Run("finds config in current dir", func(t *testing.T) {
		dir := t.TempDir()
		floopDir := filepath.Join(dir, ".floop")
		os.MkdirAll(floopDir, 0o755)
		os.WriteFile(filepath.Join(floopDir, "config.yaml"), []byte("project:\n  id: \"test/myproject\"\n  name: \"myproject\"\n"), 0o644)

		id, err := ResolveProjectID(dir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "test/myproject" {
			t.Errorf("got %q, want %q", id, "test/myproject")
		}
	})

	t.Run("walks up directories", func(t *testing.T) {
		dir := t.TempDir()
		floopDir := filepath.Join(dir, ".floop")
		os.MkdirAll(floopDir, 0o755)
		os.WriteFile(filepath.Join(floopDir, "config.yaml"), []byte("project:\n  id: \"org/repo\"\n  name: \"repo\"\n"), 0o644)

		subdir := filepath.Join(dir, "src", "pkg")
		os.MkdirAll(subdir, 0o755)

		id, err := ResolveProjectID(subdir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "org/repo" {
			t.Errorf("got %q, want %q", id, "org/repo")
		}
	})

	t.Run("returns empty when no config", func(t *testing.T) {
		dir := t.TempDir()
		id, err := ResolveProjectID(dir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "" {
			t.Errorf("got %q, want empty", id)
		}
	})

	t.Run("returns error for malformed yaml", func(t *testing.T) {
		dir := t.TempDir()
		floopDir := filepath.Join(dir, ".floop")
		os.MkdirAll(floopDir, 0o755)
		os.WriteFile(filepath.Join(floopDir, "config.yaml"), []byte("invalid: [yaml: {\n"), 0o644)

		_, err := ResolveProjectID(dir)
		if err == nil {
			t.Error("expected error for malformed yaml")
		}
	})

	t.Run("returns empty for missing project id", func(t *testing.T) {
		dir := t.TempDir()
		floopDir := filepath.Join(dir, ".floop")
		os.MkdirAll(floopDir, 0o755)
		os.WriteFile(filepath.Join(floopDir, "config.yaml"), []byte("other: value\n"), 0o644)

		id, err := ResolveProjectID(dir)
		if err != nil {
			t.Fatal(err)
		}
		if id != "" {
			t.Errorf("got %q, want empty", id)
		}
	})
}
