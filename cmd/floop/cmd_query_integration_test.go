package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

// Helper to initialize a store with a behavior for query tests.
func setupQueryTest(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Initialize
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newInitCmd())
	rootCmd.SetArgs([]string{"init", "--root", tmpDir})
	rootCmd.SetOut(&bytes.Buffer{})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Learn a behavior
	rootCmd2 := newTestRootCmd()
	rootCmd2.AddCommand(newLearnCmd())
	rootCmd2.SetOut(&bytes.Buffer{})
	rootCmd2.SetArgs([]string{
		"learn",
		"--wrong", "used fmt.Println for debugging",
		"--right", "use slog structured logging",
		"--file", "main.go",
		"--task", "coding",
		"--root", tmpDir,
	})
	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("learn failed: %v", err)
	}

	// Get behavior ID
	ctx := context.Background()
	graphStore, err := store.NewMultiGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer graphStore.Close()
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("no behaviors found after learn")
	}
	behaviorID := nodes[0].ID

	return tmpDir, behaviorID
}

func TestShowCmdWithBehavior(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newShowCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"show", behaviorID, "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("show failed: %v", err)
	}
}

func TestShowCmdNotFound(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newShowCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"show", "nonexistent-id", "--root", tmpDir})

	// Should not error (prints "not found" message)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("show nonexistent failed: %v", err)
	}
}

func TestShowCmdJSON(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newShowCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"show", behaviorID, "--json", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("show --json failed: %v", err)
	}
}

func TestShowCmdNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newShowCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"show", "any-id", "--root", tmpDir})

	// Should not error (prints "not initialized" message)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("show not initialized failed: %v", err)
	}
}

func TestWhyCmdWithBehavior(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newWhyCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"why", behaviorID, "--file", "main.go", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("why failed: %v", err)
	}
}

func TestWhyCmdJSON(t *testing.T) {
	tmpDir, behaviorID := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newWhyCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"why", behaviorID, "--json", "--file", "main.go", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("why --json failed: %v", err)
	}
}

func TestWhyCmdNotFound(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newWhyCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"why", "nonexistent-id", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("why nonexistent failed: %v", err)
	}
}

func TestWhyCmdNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newWhyCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"why", "any-id", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("why not initialized failed: %v", err)
	}
}

func TestPromptCmdWithBehaviors(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPromptCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"prompt", "--file", "main.go", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
}

func TestPromptCmdJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPromptCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"prompt", "--json", "--file", "main.go", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("prompt --json failed: %v", err)
	}
}

func TestPromptCmdWithTokenBudget(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPromptCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"prompt", "--file", "main.go", "--token-budget", "500", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("prompt --token-budget failed: %v", err)
	}
}

func TestPromptCmdTiered(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPromptCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"prompt", "--file", "main.go", "--tiered", "--token-budget", "500", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("prompt --tiered failed: %v", err)
	}
}

func TestPromptCmdTieredJSON(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPromptCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"prompt", "--json", "--tiered", "--token-budget", "500", "--file", "main.go", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("prompt --tiered --json failed: %v", err)
	}
}

func TestPromptCmdNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPromptCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"prompt", "--file", "main.go", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("prompt not initialized failed: %v", err)
	}
}

func TestPromptCmdXMLFormat(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newPromptCmd())
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"prompt", "--file", "main.go", "--format", "xml", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("prompt --format xml failed: %v", err)
	}
}

func TestListCmdTagFilter(t *testing.T) {
	tmpDir, _ := setupQueryTest(t)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newListCmd())
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"list", "--local", "--json", "--tag", "nonexistent-tag", "--root", tmpDir})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list --tag failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	countVal, ok := result["count"].(float64)
	if !ok {
		t.Fatal("expected 'count' field to be a number")
	}
	if int(countVal) != 0 {
		t.Errorf("expected 0 behaviors with nonexistent tag, got %d", int(countVal))
	}
}
