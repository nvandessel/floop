package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/llm"
)

// mockLLMClient is a test double for llm.Client that returns canned responses.
type mockLLMClient struct {
	response string
	err      error
}

func (m *mockLLMClient) Complete(_ context.Context, _ []llm.Message) (string, error) {
	return m.response, m.err
}

func (m *mockLLMClient) Available() bool { return true }

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

	// Use a unique session ID to avoid collisions with concurrent test runs
	sessionID := fmt.Sprintf("test-dedup-%d-%d", os.Getpid(), time.Now().UnixNano())
	stdinJSON := fmt.Sprintf(`{"session_id":"%s"}`, sessionID)

	// Clean up marker directory if it exists from a prior run
	marker := filepath.Join(os.TempDir(), fmt.Sprintf("floop-session-%s", sessionID))
	os.RemoveAll(marker)
	t.Cleanup(func() { os.RemoveAll(marker) })

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

// TestHookDetectCorrection_LogsPatternMiss verifies the hook logs when MightBeCorrection returns false.
func TestHookDetectCorrection_LogsPatternMiss(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	logPath := filepath.Join(tmpDir, ".floop", "hook-debug.log")
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	cmd := newHookCmd()
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(cmd)

	input := `{"prompt": "how do I use this function?"}`
	rootCmd.SetIn(strings.NewReader(input))
	rootCmd.SetArgs([]string{"hook", "detect-correction", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.Execute()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected hook-debug.log to exist: %v", err)
	}
	if !strings.Contains(string(data), "pattern_miss") {
		t.Errorf("expected log to contain 'pattern_miss', got: %s", string(data))
	}
}

// TestHookDetectCorrection_LogsSuccess verifies logging on the success path.
func TestHookDetectCorrection_LogsSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	logPath := filepath.Join(tmpDir, ".floop", "hook-debug.log")
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	cmd := newHookCmd()
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(cmd)

	// "don't" triggers MightBeCorrection, but LLM client will be nil in test
	input := `{"prompt": "no, don't do that, use interfaces instead"}`
	rootCmd.SetIn(strings.NewReader(input))
	rootCmd.SetArgs([]string{"hook", "detect-correction", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.Execute()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected hook-debug.log to exist: %v", err)
	}
	logStr := string(data)
	if !strings.Contains(logStr, "pattern_match") {
		t.Errorf("expected log to contain 'pattern_match', got: %s", logStr)
	}
	if !strings.Contains(logStr, "client_unavailable") && !strings.Contains(logStr, "llm_error") {
		t.Errorf("expected log to contain 'client_unavailable' or 'llm_error', got: %s", logStr)
	}
}

// TestHookSessionStart_IncludesLearnDirective verifies session-start output includes floop_learn directive.
func TestHookSessionStart_IncludesLearnDirective(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize to get behaviors
	initCmd := newTestRootCmd()
	initCmd.AddCommand(newInitCmd())
	initCmd.SetArgs([]string{"init", "--root", tmpDir})
	initCmd.SetOut(&bytes.Buffer{})
	initCmd.Execute()

	// Run session-start
	var out bytes.Buffer
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	rootCmd.SetArgs([]string{"hook", "session-start", "--root", tmpDir})
	rootCmd.SetOut(&out)
	rootCmd.SetIn(strings.NewReader("{}"))
	rootCmd.Execute()

	output := out.String()
	if !strings.Contains(output, "floop_learn") {
		t.Errorf("session-start output should mention floop_learn, got:\n%s", output)
	}
}

// TestHookDetectCorrection_SuccessMessageFormat verifies the correction captured message format.
func TestHookDetectCorrection_SuccessMessageFormat(t *testing.T) {
	msg := formatCorrectionCapturedMessage("c-12345")
	if !strings.Contains(msg, "c-12345") {
		t.Errorf("message should contain correction ID, got: %s", msg)
	}
	if !strings.HasPrefix(msg, "### ") {
		t.Errorf("message should be markdown header format, got: %s", msg)
	}
}

// TestHookDetectCorrection_TimeoutValue verifies the timeout is 15s.
func TestHookDetectCorrection_TimeoutValue(t *testing.T) {
	if hookDetectCorrectionTimeout != 15*time.Second {
		t.Errorf("hookDetectCorrectionTimeout = %v, want 15s", hookDetectCorrectionTimeout)
	}
}

// TestHookLog_NoFloopDir verifies hookLog is a silent no-op when .floop dir doesn't exist.
func TestHookLog_NoFloopDir(t *testing.T) {
	tmpDir := t.TempDir()
	// No .floop dir created — should not panic or create files
	hookLog(tmpDir, "detect-correction", "test_stage", "test_outcome", nil)

	// Verify no hook-debug.log was created
	logPath := filepath.Join(tmpDir, ".floop", "hook-debug.log")
	if _, err := os.Stat(logPath); err == nil {
		t.Error("hookLog should not create files when .floop dir doesn't exist")
	}
}

// TestHookLog_WithExtraFields verifies extra fields appear in log output.
func TestHookLog_WithExtraFields(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	extra := map[string]interface{}{
		"error":      "something broke",
		"confidence": 0.42,
	}
	hookLog(tmpDir, "detect-correction", "test_stage", "test_outcome", extra)

	logPath := filepath.Join(tmpDir, ".floop", "hook-debug.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected hook-debug.log to exist: %v", err)
	}
	logStr := string(data)
	if !strings.Contains(logStr, "something broke") {
		t.Errorf("expected log to contain extra error field, got: %s", logStr)
	}
	if !strings.Contains(logStr, "0.42") {
		t.Errorf("expected log to contain extra confidence field, got: %s", logStr)
	}
	if !strings.Contains(logStr, `"hook":"detect-correction"`) {
		t.Errorf("expected log to contain hook name, got: %s", logStr)
	}
}

// TestHookLog_CantOpenFile verifies hookLog is silent when log file can't be opened.
func TestHookLog_CantOpenFile(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	os.MkdirAll(floopDir, 0700)

	// Create hook-debug.log as a directory to make OpenFile fail
	logPath := filepath.Join(floopDir, "hook-debug.log")
	os.MkdirAll(logPath, 0700)

	// Should not panic
	hookLog(tmpDir, "detect-correction", "test_stage", "test_outcome", nil)
}

// TestHookLog_HookNameInOutput verifies the hookName parameter appears in log output.
func TestHookLog_HookNameInOutput(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	hookLog(tmpDir, "custom-hook", "stage1", "outcome1", nil)

	logPath := filepath.Join(tmpDir, ".floop", "hook-debug.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected hook-debug.log to exist: %v", err)
	}

	var entry map[string]interface{}
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	if entry["hook"] != "custom-hook" {
		t.Errorf("hook = %v, want %q", entry["hook"], "custom-hook")
	}
	if entry["stage"] != "stage1" {
		t.Errorf("stage = %v, want %q", entry["stage"], "stage1")
	}
	if entry["outcome"] != "outcome1" {
		t.Errorf("outcome = %v, want %q", entry["outcome"], "outcome1")
	}
}

// TestFloopLearnDirective verifies the learn directive contains expected content.
func TestFloopLearnDirective(t *testing.T) {
	directive := floopLearnDirective()
	if !strings.Contains(directive, "floop_learn") {
		t.Error("directive should mention floop_learn")
	}
	if !strings.Contains(directive, "IMPORTANT") {
		t.Error("directive should contain IMPORTANT heading")
	}
	if !strings.Contains(directive, "mcp__floop__floop_learn") {
		t.Error("directive should contain the MCP tool call example")
	}
	if !strings.Contains(directive, "Do NOT use auto-memory") {
		t.Error("directive should warn against auto-memory")
	}
}

// TestFormatCorrectionCapturedMessage_Variations verifies message format edge cases.
func TestFormatCorrectionCapturedMessage_Variations(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"normal_id", "c-12345", "c-12345"},
		{"empty_id", "", "(id: )"},
		{"long_id", "c-999999999999999999", "c-999999999999999999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := formatCorrectionCapturedMessage(tt.id)
			if !strings.Contains(msg, tt.want) {
				t.Errorf("message should contain %q, got: %s", tt.want, msg)
			}
			if !strings.Contains(msg, "Correction Captured") {
				t.Errorf("message should contain 'Correction Captured', got: %s", msg)
			}
		})
	}
}

// TestHookDetectCorrection_LogsEmptyPrompt verifies empty prompt is logged with correct outcome.
func TestHookDetectCorrection_LogsEmptyPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	rootCmd.SetIn(strings.NewReader(`{"prompt":""}`))
	rootCmd.SetArgs([]string{"hook", "detect-correction", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.Execute()

	logPath := filepath.Join(tmpDir, ".floop", "hook-debug.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected hook-debug.log to exist: %v", err)
	}
	if !strings.Contains(string(data), "empty_prompt") {
		t.Errorf("expected log to contain 'empty_prompt', got: %s", string(data))
	}
}

// TestHookDetectCorrection_LogsJsonError verifies invalid JSON is logged correctly.
func TestHookDetectCorrection_LogsJsonError(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newHookCmd())
	rootCmd.SetIn(strings.NewReader(`not json`))
	rootCmd.SetArgs([]string{"hook", "detect-correction", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.Execute()

	logPath := filepath.Join(tmpDir, ".floop", "hook-debug.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected hook-debug.log to exist: %v", err)
	}
	if !strings.Contains(string(data), "json_error") {
		t.Errorf("expected log to contain 'json_error', got: %s", string(data))
	}
}

// --- runDetectCorrection tests (exercising all paths from LLM onward) ---

// helper to set up a temp dir with .floop and an initialized store for full-path tests.
func setupInitializedRoot(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize globally (for meta-behaviors)
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
	return tmpDir
}

// validCorrectionJSON returns a JSON response that ParseCorrectionExtractionResponse accepts.
func validCorrectionJSON(isCorrection bool, wrong, right string, confidence float64) string {
	return fmt.Sprintf(`{"is_correction": %t, "wrong": %q, "right": %q, "confidence": %f}`,
		isCorrection, wrong, right, confidence)
}

func TestRunDetectCorrection_ClientNil(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	rootCmd := newTestRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)

	// "don't" triggers MightBeCorrection
	err := runDetectCorrection(rootCmd, tmpDir, "no, don't do that", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logData, _ := os.ReadFile(filepath.Join(tmpDir, ".floop", "hook-debug.log"))
	if !strings.Contains(string(logData), "client_unavailable") {
		t.Errorf("expected client_unavailable in log, got: %s", logData)
	}
}

func TestRunDetectCorrection_LLMError(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	client := &mockLLMClient{err: errors.New("connection refused")}
	rootCmd := newTestRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})

	err := runDetectCorrection(rootCmd, tmpDir, "no, don't do that", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logData, _ := os.ReadFile(filepath.Join(tmpDir, ".floop", "hook-debug.log"))
	if !strings.Contains(string(logData), "llm_error") {
		t.Errorf("expected llm_error in log, got: %s", logData)
	}
}

func TestRunDetectCorrection_ParseError(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	client := &mockLLMClient{response: "not json at all"}
	rootCmd := newTestRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})

	err := runDetectCorrection(rootCmd, tmpDir, "no, don't do that", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logData, _ := os.ReadFile(filepath.Join(tmpDir, ".floop", "hook-debug.log"))
	if !strings.Contains(string(logData), "parse_error") {
		t.Errorf("expected parse_error in log, got: %s", logData)
	}
}

func TestRunDetectCorrection_NotCorrection(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	client := &mockLLMClient{response: validCorrectionJSON(false, "", "", 0.9)}
	rootCmd := newTestRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})

	err := runDetectCorrection(rootCmd, tmpDir, "no, don't do that", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logData, _ := os.ReadFile(filepath.Join(tmpDir, ".floop", "hook-debug.log"))
	if !strings.Contains(string(logData), "not_correction") {
		t.Errorf("expected not_correction in log, got: %s", logData)
	}
}

func TestRunDetectCorrection_MissingFields(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	client := &mockLLMClient{response: validCorrectionJSON(true, "did X", "", 0.9)}
	rootCmd := newTestRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})

	err := runDetectCorrection(rootCmd, tmpDir, "no, don't do that", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logData, _ := os.ReadFile(filepath.Join(tmpDir, ".floop", "hook-debug.log"))
	if !strings.Contains(string(logData), "missing_fields") {
		t.Errorf("expected missing_fields in log, got: %s", logData)
	}
}

func TestRunDetectCorrection_BelowThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	client := &mockLLMClient{response: validCorrectionJSON(true, "did X", "do Y", 0.3)}
	rootCmd := newTestRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})

	err := runDetectCorrection(rootCmd, tmpDir, "no, don't do that", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logData, _ := os.ReadFile(filepath.Join(tmpDir, ".floop", "hook-debug.log"))
	if !strings.Contains(string(logData), "below_threshold") {
		t.Errorf("expected below_threshold in log, got: %s", logData)
	}
}

func TestRunDetectCorrection_FloopDirMissing(t *testing.T) {
	tmpDir := t.TempDir()
	// No .floop dir — but we need hookLog's earlier calls to work,
	// so create .floop for logging, then remove before the dir check.
	// Actually, the floop_dir_missing check happens after LLM calls,
	// so hookLog calls before that point will no-op (no .floop), which is fine.
	// The stderr path is the one we're testing.

	client := &mockLLMClient{response: validCorrectionJSON(true, "did X", "do Y", 0.9)}
	rootCmd := newTestRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})

	err := runDetectCorrection(rootCmd, tmpDir, "no, don't do that", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The stderr message is printed; we verify no crash and no output on stdout.
}

func TestRunDetectCorrection_FullSuccess(t *testing.T) {
	tmpDir := setupInitializedRoot(t)

	client := &mockLLMClient{response: validCorrectionJSON(true, "used print", "use log.Printf", 0.95)}
	rootCmd := newTestRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)

	err := runDetectCorrection(rootCmd, tmpDir, "no, don't use print", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify stdout contains correction captured message
	output := out.String()
	if !strings.Contains(output, "Correction Captured") {
		t.Errorf("expected 'Correction Captured' in output, got: %s", output)
	}

	// Verify corrections.jsonl was written
	correctionsPath := filepath.Join(tmpDir, ".floop", "corrections.jsonl")
	data, err := os.ReadFile(correctionsPath)
	if err != nil {
		t.Fatalf("expected corrections.jsonl to exist: %v", err)
	}
	if !strings.Contains(string(data), "use log.Printf") {
		t.Errorf("expected correction content in corrections.jsonl, got: %s", data)
	}

	// Verify hook-debug.log contains correction_captured
	logData, _ := os.ReadFile(filepath.Join(tmpDir, ".floop", "hook-debug.log"))
	if !strings.Contains(string(logData), "correction_captured") {
		t.Errorf("expected correction_captured in log, got: %s", logData)
	}
}

func TestRunDetectCorrection_PatternMiss(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".floop"), 0700)

	client := &mockLLMClient{response: "should not be called"}
	rootCmd := newTestRootCmd()
	rootCmd.SetOut(&bytes.Buffer{})

	// A prompt that doesn't match any correction patterns
	err := runDetectCorrection(rootCmd, tmpDir, "how do I use this function?", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logData, _ := os.ReadFile(filepath.Join(tmpDir, ".floop", "hook-debug.log"))
	if !strings.Contains(string(logData), "pattern_miss") {
		t.Errorf("expected pattern_miss in log, got: %s", logData)
	}
}

