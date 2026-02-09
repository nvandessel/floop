package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractScripts(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "hooks")

	extracted, err := ExtractScripts(targetDir, "1.2.3", 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(extracted) != 4 {
		t.Fatalf("expected 4 scripts, got %d", len(extracted))
	}

	// Verify all files exist and are executable
	for _, path := range extracted {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("script not found: %s", path)
			continue
		}
		if info.Mode().Perm()&0100 == 0 {
			t.Errorf("script not executable: %s (mode %o)", path, info.Mode().Perm())
		}
	}
}

func TestExtractScriptsVersionInjection(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "hooks")

	_, err := ExtractScripts(targetDir, "4.5.6", 3000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "floop-session-start.sh"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "# version: 4.5.6") {
		t.Error("expected version header to be injected")
	}
	if strings.Contains(string(content), "{{VERSION}}") {
		t.Error("template placeholder {{VERSION}} was not replaced")
	}
}

func TestExtractScriptsTokenBudgetInjection(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "hooks")

	_, err := ExtractScripts(targetDir, "1.0.0", 1500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "floop-session-start.sh"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "--token-budget 1500") {
		t.Error("expected token budget 1500 to be injected")
	}
	if strings.Contains(string(content), "{{TOKEN_BUDGET}}") {
		t.Error("template placeholder {{TOKEN_BUDGET}} was not replaced")
	}
}

func TestExtractScriptsIdempotency(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "hooks")

	// Extract twice with different versions
	_, err := ExtractScripts(targetDir, "1.0.0", 2000)
	if err != nil {
		t.Fatalf("first extraction failed: %v", err)
	}

	extracted, err := ExtractScripts(targetDir, "2.0.0", 2000)
	if err != nil {
		t.Fatalf("second extraction failed: %v", err)
	}

	if len(extracted) != 4 {
		t.Fatalf("expected 4 scripts after re-extraction, got %d", len(extracted))
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "floop-session-start.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "# version: 2.0.0") {
		t.Error("expected version 2.0.0 after re-extraction")
	}
}

func TestExtractScriptsCreatesDir(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "nested", "deep", "hooks")

	_, err := ExtractScripts(targetDir, "1.0.0", 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(targetDir)
	if err != nil {
		t.Fatalf("target dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected target to be a directory")
	}
}

func TestExtractScriptsPATHResolution(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "hooks")

	_, err := ExtractScripts(targetDir, "1.0.0", 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, _ := os.ReadDir(targetDir)
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(targetDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		s := string(content)
		if strings.Contains(s, "CLAUDE_PROJECT_DIR") {
			t.Errorf("script %s still references CLAUDE_PROJECT_DIR", entry.Name())
		}
		if !strings.Contains(s, "command -v floop") {
			t.Errorf("script %s does not use 'command -v floop' for PATH resolution", entry.Name())
		}
	}
}

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
