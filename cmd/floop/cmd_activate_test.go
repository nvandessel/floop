package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/session"
	"github.com/spf13/cobra"
)

func TestActivationToTier(t *testing.T) {
	tests := []struct {
		name       string
		activation float64
		want       models.InjectionTier
	}{
		{"high activation returns full", 0.9, models.TierFull},
		{"threshold activation returns full", 0.7, models.TierFull},
		{"medium activation returns summary", 0.5, models.TierSummary},
		{"low-medium activation returns summary", 0.4, models.TierSummary},
		{"low activation returns name-only", 0.2, models.TierNameOnly},
		{"zero activation returns name-only", 0.0, models.TierNameOnly},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := activationToTier(tt.activation)
			if got != tt.want {
				t.Errorf("activationToTier(%v) = %v, want %v", tt.activation, got, tt.want)
			}
		})
	}
}

func TestEstimateTokenCost(t *testing.T) {
	tests := []struct {
		name string
		tier models.InjectionTier
		want int
	}{
		{"full tier", models.TierFull, 80},
		{"summary tier", models.TierSummary, 30},
		{"name-only tier", models.TierNameOnly, 10},
		{"omitted tier", models.TierOmitted, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokenCost("any-id", tt.tier)
			if got != tt.want {
				t.Errorf("estimateTokenCost(_, %v) = %d, want %d", tt.tier, got, tt.want)
			}
		})
	}
}

func TestApplyTokenBudget(t *testing.T) {
	results := []session.FilteredResult{
		{BehaviorID: "b1", Tier: models.TierFull, Activation: 0.9},     // 80 tokens
		{BehaviorID: "b2", Tier: models.TierSummary, Activation: 0.5},  // 30 tokens
		{BehaviorID: "b3", Tier: models.TierNameOnly, Activation: 0.2}, // 10 tokens
	}

	tests := []struct {
		name   string
		budget int
		want   int
	}{
		{"zero budget returns all", 0, 3},
		{"large budget returns all", 500, 3},
		{"exact budget returns fitting", 110, 2},
		{"tight budget returns first only", 80, 1},
		{"tiny budget returns none", 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyTokenBudget(results, tt.budget)
			if len(got) != tt.want {
				t.Errorf("applyTokenBudget(_, %d) returned %d results, want %d", tt.budget, len(got), tt.want)
			}
		})
	}
}

func TestBuildTriggerReason(t *testing.T) {
	tests := []struct {
		name    string
		signals triggerSignals
		want    string
	}{
		{"file with extension", triggerSignals{File: "main.go"}, "file change to `*.go`"},
		{"file without extension", triggerSignals{File: "Makefile"}, "file `Makefile`"},
		{"task only", triggerSignals{Task: "testing"}, "task: `testing`"},
		{"language only", triggerSignals{Language: "go"}, "language: `go`"},
		{"no signals", triggerSignals{}, "context change"},
		{"file takes priority", triggerSignals{File: "main.py", Task: "testing", Language: "python"}, "file change to `*.py`"},
		{"task over language", triggerSignals{Task: "testing", Language: "go"}, "task: `testing`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTriggerReason(tt.signals)
			if got != tt.want {
				t.Errorf("buildTriggerReason(%+v) = %q, want %q", tt.signals, got, tt.want)
			}
		})
	}
}

func TestBehaviorContent(t *testing.T) {
	b := models.Behavior{
		Name: "test-behavior",
		Content: models.BehaviorContent{
			Canonical: "Use pathlib.Path instead of os.path",
			Summary:   "Prefer pathlib.Path",
		},
	}

	tests := []struct {
		name string
		tier models.InjectionTier
		want string
	}{
		{"full tier returns canonical", models.TierFull, "Use pathlib.Path instead of os.path"},
		{"summary tier returns summary", models.TierSummary, "Prefer pathlib.Path"},
		{"name-only tier returns name", models.TierNameOnly, "test-behavior"},
		{"omitted tier returns name", models.TierOmitted, "test-behavior"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := behaviorContent(b, tt.tier)
			if got != tt.want {
				t.Errorf("behaviorContent(_, %v) = %q, want %q", tt.tier, got, tt.want)
			}
		})
	}
}

func TestBehaviorContentFallbacks(t *testing.T) {
	tests := []struct {
		name string
		b    models.Behavior
		tier models.InjectionTier
		want string
	}{
		{
			"full tier without canonical falls back to name",
			models.Behavior{Name: "fb", Content: models.BehaviorContent{}},
			models.TierFull,
			"fb",
		},
		{
			"summary tier without summary falls back to canonical",
			models.Behavior{Name: "fb", Content: models.BehaviorContent{Canonical: "canonical text"}},
			models.TierSummary,
			"canonical text",
		},
		{
			"summary tier without summary or canonical falls back to name",
			models.Behavior{Name: "fb", Content: models.BehaviorContent{}},
			models.TierSummary,
			"fb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := behaviorContent(tt.b, tt.tier)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionStateDir(t *testing.T) {
	dir := sessionStateDir("test-session-123")
	if !strings.Contains(dir, "floop-session-test-session-123") {
		t.Errorf("unexpected session dir: %s", dir)
	}
	// Session state should be under ~/.floop/sessions/, not os.TempDir()
	homeDir, err := os.UserHomeDir()
	if err == nil {
		if !strings.HasPrefix(dir, filepath.Join(homeDir, ".floop", "sessions")) {
			t.Errorf("session dir should be under ~/.floop/sessions/, got: %s", dir)
		}
	}
}

func TestOutputMarkdown(t *testing.T) {
	behaviorMap := map[string]models.Behavior{
		"b1": {
			Name:    "use-pathlib",
			Kind:    models.BehaviorKindDirective,
			Content: models.BehaviorContent{Canonical: "Use pathlib.Path instead of os.path"},
		},
		"b2": {
			Name:    "no-mutable-defaults",
			Kind:    models.BehaviorKindConstraint,
			Content: models.BehaviorContent{Canonical: "Never use mutable default arguments"},
		},
	}

	results := []session.FilteredResult{
		{BehaviorID: "b1", Tier: models.TierFull, Activation: 0.9},
		{BehaviorID: "b2", Tier: models.TierFull, Activation: 0.8},
	}

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := outputMarkdown(cmd, results, behaviorMap, "file change to `*.py`")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "## Dynamic Context Update") {
		t.Error("expected Dynamic Context Update header")
	}
	if !strings.Contains(output, "file change to `*.py`") {
		t.Error("expected trigger reason in output")
	}
	if !strings.Contains(output, "### Directives") {
		t.Error("expected Directives section")
	}
	if !strings.Contains(output, "### Constraints") {
		t.Error("expected Constraints section")
	}
	if !strings.Contains(output, "Use pathlib.Path") {
		t.Error("expected directive content")
	}
	if !strings.Contains(output, "Never use mutable default arguments") {
		t.Error("expected constraint content")
	}
}

func TestOutputJSON(t *testing.T) {
	behaviorMap := map[string]models.Behavior{
		"b1": {
			Name:    "use-pathlib",
			Kind:    models.BehaviorKindDirective,
			Content: models.BehaviorContent{Canonical: "Use pathlib.Path"},
		},
	}

	results := []session.FilteredResult{
		{BehaviorID: "b1", Tier: models.TierFull, Activation: 0.9},
	}

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := outputJSON(cmd, results, behaviorMap, "file change to `*.py`")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if parsed["trigger"] != "file change to `*.py`" {
		t.Errorf("expected trigger reason, got %v", parsed["trigger"])
	}
	if parsed["count"].(float64) != 1 {
		t.Errorf("expected count=1, got %v", parsed["count"])
	}
}

func TestNewActivateCmd(t *testing.T) {
	cmd := newActivateCmd()

	if cmd.Use != "activate" {
		t.Errorf("expected Use='activate', got '%s'", cmd.Use)
	}

	// Verify flags exist
	flags := []string{"file", "task", "format", "token-budget", "session-id", "language"}
	for _, flag := range flags {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected flag '%s' to exist", flag)
		}
	}
}

func TestRunActivateNoContext(t *testing.T) {
	// With no file or task, activate should silently return nil
	cmd := newActivateCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Set root to a temp dir with .floop
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatal(err)
	}

	cmd.Flags().Set("root", tmpDir)

	err := cmd.RunE(cmd, []string{})
	if err != nil {
		t.Errorf("expected nil error for no context, got: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no output for no context, got: %s", buf.String())
	}
}

func TestRunActivateNoFloop(t *testing.T) {
	// With no .floop dir, activate should silently return nil
	cmd := newActivateCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	tmpDir := t.TempDir()
	cmd.Flags().Set("root", tmpDir)
	cmd.Flags().Set("file", "main.go")

	err := cmd.RunE(cmd, []string{})
	if err != nil {
		t.Errorf("expected nil error for no .floop, got: %v", err)
	}
}
