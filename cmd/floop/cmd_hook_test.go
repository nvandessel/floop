package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// TestNewHookCmd verifies the parent hook command has the expected subcommands.
func TestNewHookCmd(t *testing.T) {
	cmd := newHookCmd()
	if cmd.Use != "hook" {
		t.Errorf("Use = %q, want %q", cmd.Use, "hook")
	}

	// Should have 4 subcommands
	subs := cmd.Commands()
	want := map[string]bool{
		"session-start":     false,
		"first-prompt":      false,
		"dynamic-context":   false,
		"detect-correction": false,
	}
	for _, sub := range subs {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

// TestHookSessionStart verifies session-start produces markdown output when behaviors exist.
func TestHookSessionStart(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize globally to seed meta-behaviors (unconditional, always active)
	initGlobal := newTestRootCmd()
	initGlobal.AddCommand(newInitCmd())
	initGlobal.SetArgs([]string{"init", "--global"})
	initGlobal.SetOut(&bytes.Buffer{})
	if err := initGlobal.Execute(); err != nil {
		t.Fatalf("global init failed: %v", err)
	}

	// Initialize project
	initRoot := newTestRootCmd()
	initRoot.AddCommand(newInitCmd())
	initRoot.SetArgs([]string{"init", "--root", tmpDir})
	initRoot.SetOut(&bytes.Buffer{})
	if err := initRoot.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Run session-start hook
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"hook", "session-start", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("hook session-start failed: %v", err)
	}

	output := out.String()
	if output == "" {
		t.Error("expected non-empty output from session-start when meta-behaviors exist")
	}
}

// TestHookSessionStartNoFloop verifies session-start is silent when .floop is missing.
func TestHookSessionStartNoFloop(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"hook", "session-start", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("hook session-start should not error when .floop missing: %v", err)
	}

	if out.String() != "" {
		t.Errorf("expected empty output when .floop missing, got: %q", out.String())
	}
}

// TestHookFirstPrompt verifies first-prompt dedup behavior.
func TestHookFirstPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize globally to seed meta-behaviors (unconditional, always active)
	initGlobal := newTestRootCmd()
	initGlobal.AddCommand(newInitCmd())
	initGlobal.SetArgs([]string{"init", "--global"})
	initGlobal.SetOut(&bytes.Buffer{})
	if err := initGlobal.Execute(); err != nil {
		t.Fatalf("global init failed: %v", err)
	}

	// Initialize project
	initRoot := newTestRootCmd()
	initRoot.AddCommand(newInitCmd())
	initRoot.SetArgs([]string{"init", "--root", tmpDir})
	initRoot.SetOut(&bytes.Buffer{})
	if err := initRoot.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Use a unique session ID to avoid collisions with previous test runs
	sessionID := fmt.Sprintf("test-dedup-%d", time.Now().UnixNano())
	stdinJSON := fmt.Sprintf(`{"session_id":"%s"}`, sessionID)

	// Clean up marker if it exists from a prior run
	marker := filepath.Join(os.TempDir(), fmt.Sprintf("floop-session-%s", sessionID))
	os.Remove(marker)
	t.Cleanup(func() { os.Remove(marker) })

	// First call should produce output
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	var out1 bytes.Buffer
	rootCmd.SetOut(&out1)
	rootCmd.SetIn(bytes.NewReader([]byte(stdinJSON)))
	rootCmd.SetArgs([]string{"hook", "first-prompt", "--root", tmpDir})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first-prompt first call failed: %v", err)
	}

	if out1.String() == "" {
		t.Error("expected non-empty output from first call")
	}

	// Second call with same session_id should be silent (dedup)
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newHookCmd())
	var out2 bytes.Buffer
	rootCmd2.SetOut(&out2)
	rootCmd2.SetIn(bytes.NewReader([]byte(stdinJSON)))
	rootCmd2.SetArgs([]string{"hook", "first-prompt", "--root", tmpDir})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("first-prompt second call failed: %v", err)
	}

	if out2.String() != "" {
		t.Errorf("expected empty output on second call (dedup), got: %q", out2.String())
	}
}

// TestHookFirstPromptMissingSessionID verifies first-prompt handles missing session_id.
func TestHookFirstPromptMissingSessionID(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetIn(bytes.NewReader([]byte(`{}`)))
	rootCmd.SetArgs([]string{"hook", "first-prompt", "--root", tmpDir})

	// Should not error — just exit silently or use "unknown"
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first-prompt should not error on missing session_id: %v", err)
	}
}

// TestHookDynamicContextReadTool verifies dynamic-context routes Read to file activation.
func TestHookDynamicContextReadTool(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	initRoot := newTestRootCmd()
	initRoot.AddCommand(newInitCmd())
	initRoot.SetArgs([]string{"init", "--root", tmpDir})
	initRoot.SetOut(&bytes.Buffer{})
	if err := initRoot.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	input := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "main.go"},
		"session_id": "s1",
	}
	stdinJSON, _ := json.Marshal(input)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetIn(bytes.NewReader(stdinJSON))
	rootCmd.SetArgs([]string{"hook", "dynamic-context", "--root", tmpDir})

	// Should not error — may produce output or be silent depending on graph
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dynamic-context Read failed: %v", err)
	}
}

// TestHookDynamicContextBashTool verifies dynamic-context routes Bash to task detection.
func TestHookDynamicContextBashTool(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	initRoot := newTestRootCmd()
	initRoot.AddCommand(newInitCmd())
	initRoot.SetArgs([]string{"init", "--root", tmpDir})
	initRoot.SetOut(&bytes.Buffer{})
	if err := initRoot.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	input := map[string]interface{}{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "go test ./..."},
		"session_id": "s2",
	}
	stdinJSON, _ := json.Marshal(input)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetIn(bytes.NewReader(stdinJSON))
	rootCmd.SetArgs([]string{"hook", "dynamic-context", "--root", tmpDir})

	// Should not error
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dynamic-context Bash failed: %v", err)
	}
}

// TestHookDynamicContextUnknownTool verifies dynamic-context is silent on unknown tools.
func TestHookDynamicContextUnknownTool(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	input := map[string]interface{}{
		"tool_name":  "WebSearch",
		"tool_input": map[string]interface{}{"query": "test"},
		"session_id": "s3",
	}
	stdinJSON, _ := json.Marshal(input)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetIn(bytes.NewReader(stdinJSON))
	rootCmd.SetArgs([]string{"hook", "dynamic-context", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dynamic-context should not error on unknown tool: %v", err)
	}

	if out.String() != "" {
		t.Errorf("expected empty output for unknown tool, got: %q", out.String())
	}
}

// TestHookDetectCorrection verifies detect-correction reads prompt from stdin.
func TestHookDetectCorrection(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	stdinJSON := `{"prompt":"No, don't use print"}`

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetIn(bytes.NewReader([]byte(stdinJSON)))
	rootCmd.SetArgs([]string{"hook", "detect-correction", "--root", tmpDir})

	// Should not error — correction detection runs silently in hook context
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("detect-correction should not error: %v", err)
	}
}

// TestHookDetectCorrectionEmptyPrompt verifies detect-correction is silent on empty prompt.
func TestHookDetectCorrectionEmptyPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	stdinJSON := `{"prompt":""}`

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetIn(bytes.NewReader([]byte(stdinJSON)))
	rootCmd.SetArgs([]string{"hook", "detect-correction", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("detect-correction should not error on empty prompt: %v", err)
	}

	if out.String() != "" {
		t.Errorf("expected empty output for empty prompt, got: %q", out.String())
	}
}

// TestDetectTaskFromCommand verifies task detection from bash commands.
func TestDetectTaskFromCommand(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"git commit -m 'fix bug'", "committing"},
		{"git push origin main", "committing"},
		{"git merge feature", "committing"},
		{"git status", "git-operations"},
		{"git diff", "git-operations"},
		{"go test ./...", "testing"},
		{"pytest -v", "testing"},
		{"npm test", "testing"},
		{"jest --watch", "testing"},
		{"go build ./cmd/floop", "building"},
		{"npm run build", "building"},
		{"make all", "building"},
		{"docker build .", "deployment"},
		{"kubectl apply -f deploy.yaml", "deployment"},
		{"echo hello", ""},
		{"ls -la", ""},
		{"cat file.txt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := detectTaskFromCommand(tt.command)
			if got != tt.want {
				t.Errorf("detectTaskFromCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

// TestHookDynamicContextEditWriteTool verifies Edit and Write route to file activation.
func TestHookDynamicContextEditWriteTool(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	initRoot := newTestRootCmd()
	initRoot.AddCommand(newInitCmd())
	initRoot.SetArgs([]string{"init", "--root", tmpDir})
	initRoot.SetOut(&bytes.Buffer{})
	if err := initRoot.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	for _, tool := range []string{"Edit", "Write"} {
		t.Run(tool, func(t *testing.T) {
			input := map[string]interface{}{
				"tool_name":  tool,
				"tool_input": map[string]interface{}{"file_path": "main.go"},
				"session_id": "s-edit-write",
			}
			stdinJSON, _ := json.Marshal(input)

			rootCmd := newTestRootCmd()
			rootCmd.AddCommand(newHookCmd())
			var out bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetIn(bytes.NewReader(stdinJSON))
			rootCmd.SetArgs([]string{"hook", "dynamic-context", "--root", tmpDir})

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("dynamic-context %s failed: %v", tool, err)
			}
		})
	}
}

// TestHookDynamicContextNoFilePath verifies dynamic-context is silent when file_path is missing.
func TestHookDynamicContextNoFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	input := map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{},
		"session_id": "s4",
	}
	stdinJSON, _ := json.Marshal(input)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetIn(bytes.NewReader(stdinJSON))
	rootCmd.SetArgs([]string{"hook", "dynamic-context", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dynamic-context should not error on missing file_path: %v", err)
	}

	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("expected empty output for missing file_path, got: %q", out.String())
	}
}

// TestProjectTypeToLanguage verifies the mapping from project type to language.
func TestProjectTypeToLanguage(t *testing.T) {
	tests := []struct {
		pt   models.ProjectType
		want string
	}{
		{models.ProjectTypeGo, "go"},
		{models.ProjectTypePython, "python"},
		{models.ProjectTypeNode, "javascript"},
		{models.ProjectTypeRust, "rust"},
		{models.ProjectTypeUnknown, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.pt), func(t *testing.T) {
			got := projectTypeToLanguage(tt.pt)
			if got != tt.want {
				t.Errorf("projectTypeToLanguage(%v) = %q, want %q", tt.pt, got, tt.want)
			}
		})
	}
}
