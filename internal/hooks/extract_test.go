package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptVersion(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "version header present",
			content: "#!/bin/bash\n# version: 1.2.3\n# comment\n",
			want:    "1.2.3",
		},
		{
			name:    "no version header",
			content: "#!/bin/bash\n# comment\necho hello\n",
			want:    "",
		},
		{
			name:    "version on line 2",
			content: "#!/bin/bash\n# version: 0.9.0\necho hi\n",
			want:    "0.9.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.sh")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			got, err := ScriptVersion(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ScriptVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScriptVersionFileNotFound(t *testing.T) {
	_, err := ScriptVersion("/nonexistent/path.sh")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestInstalledScripts(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"floop-session-start.sh", "floop-detect-correction.sh", "other-hook.sh"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	found, err := InstalledScripts(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(found) != 2 {
		t.Errorf("expected 2 floop scripts, got %d: %v", len(found), found)
	}
}

func TestInstalledScriptsEmptyDir(t *testing.T) {
	dir := t.TempDir()

	found, err := InstalledScripts(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(found) != 0 {
		t.Errorf("expected 0 scripts in empty dir, got %d", len(found))
	}
}
