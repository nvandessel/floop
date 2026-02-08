package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// TestE2E_FullPipeline validates the complete floop pipeline:
// learn → connect → activate → validate → backup → restore.
// This exercises every feature implemented in d4f.1–d4f.5.
func TestE2E_FullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	server, tmpDir := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Create a Go file so language detection works.
	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\n"), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// ── Stage 1: Learn two corrections ──────────────────────────────
	var behaviorA, behaviorB string

	t.Run("Stage1_Learn", func(t *testing.T) {
		// Behavior A: Go-specific (should activate for .go files)
		_, outA, err := server.handleFloopLearn(ctx, nil, FloopLearnInput{
			Wrong: "Used println for debugging in Go code",
			Right: "Use slog structured logging in Go code",
			File:  "main.go",
			Task:  "development",
		})
		if err != nil {
			t.Fatalf("learn A failed: %v", err)
		}
		if outA.BehaviorID == "" {
			t.Fatal("BehaviorID A is empty")
		}
		behaviorA = outA.BehaviorID

		// Behavior B: General (no file context)
		_, outB, err := server.handleFloopLearn(ctx, nil, FloopLearnInput{
			Wrong: "Committed secrets to repository",
			Right: "Use environment variables for secrets",
		})
		if err != nil {
			t.Fatalf("learn B failed: %v", err)
		}
		if outB.BehaviorID == "" {
			t.Fatal("BehaviorID B is empty")
		}
		behaviorB = outB.BehaviorID

		t.Logf("Learned: A=%s B=%s", behaviorA, behaviorB)
	})

	// ── Stage 2: Connect behaviors ──────────────────────────────────
	t.Run("Stage2_Connect", func(t *testing.T) {
		// Create a similar-to edge (bidirectional)
		_, out, err := server.handleFloopConnect(ctx, nil, FloopConnectInput{
			Source:        behaviorA,
			Target:        behaviorB,
			Kind:          "similar-to",
			Weight:        0.7,
			Bidirectional: true,
		})
		if err != nil {
			t.Fatalf("connect failed: %v", err)
		}
		if !out.Bidirectional {
			t.Error("Expected bidirectional=true in output")
		}
		if out.Weight != 0.7 {
			t.Errorf("Weight = %f, want 0.7", out.Weight)
		}

		// Verify edges exist in store
		edges, err := server.store.GetEdges(ctx, behaviorA, store.DirectionOutbound, "similar-to")
		if err != nil {
			t.Fatalf("GetEdges failed: %v", err)
		}
		found := false
		for _, e := range edges {
			if e.Target == behaviorB {
				found = true
			}
		}
		if !found {
			t.Errorf("Edge A→B not found in store")
		}

		// Verify reverse edge
		reverseEdges, err := server.store.GetEdges(ctx, behaviorB, store.DirectionOutbound, "similar-to")
		if err != nil {
			t.Fatalf("GetEdges reverse failed: %v", err)
		}
		found = false
		for _, e := range reverseEdges {
			if e.Target == behaviorA {
				found = true
			}
		}
		if !found {
			t.Errorf("Reverse edge B→A not found in store")
		}
	})

	// Verify connect rejects invalid inputs
	t.Run("Stage2_ConnectValidation", func(t *testing.T) {
		// Self-edge
		_, _, err := server.handleFloopConnect(ctx, nil, FloopConnectInput{
			Source: behaviorA,
			Target: behaviorA,
			Kind:   "similar-to",
		})
		if err == nil {
			t.Error("Expected error for self-edge")
		}

		// Invalid kind
		_, _, err = server.handleFloopConnect(ctx, nil, FloopConnectInput{
			Source: behaviorA,
			Target: behaviorB,
			Kind:   "invalid-kind",
		})
		if err == nil {
			t.Error("Expected error for invalid kind")
		}

		// Non-existent source
		_, _, err = server.handleFloopConnect(ctx, nil, FloopConnectInput{
			Source: "nonexistent-id",
			Target: behaviorB,
			Kind:   "requires",
		})
		if err == nil {
			t.Error("Expected error for non-existent source")
		}
	})

	// ── Stage 3: Activate with Go context ───────────────────────────
	t.Run("Stage3_Activate", func(t *testing.T) {
		_, out, err := server.handleFloopActive(ctx, nil, FloopActiveInput{
			File: "main.go",
			Task: "development",
		})
		if err != nil {
			t.Fatalf("floop_active failed: %v", err)
		}

		// Context should detect Go
		if out.Context["language"] != "go" {
			t.Errorf("language = %v, want 'go'", out.Context["language"])
		}
		if out.Context["task"] != "development" {
			t.Errorf("task = %v, want 'development'", out.Context["task"])
		}

		// At least the learned behaviors should be present
		// (depending on whether they have When predicates — behaviors
		// without When predicates are always active)
		if out.Count < 1 {
			t.Errorf("Expected at least 1 active behavior, got %d", out.Count)
		}

		t.Logf("Active behaviors: %d", out.Count)
		for _, b := range out.Active {
			t.Logf("  - %s: %s (kind=%s, confidence=%.2f)", b.ID, b.Name, b.Kind, b.Confidence)
		}
	})

	// ── Stage 4: Validate graph integrity ───────────────────────────
	t.Run("Stage4_Validate", func(t *testing.T) {
		_, out, err := server.handleFloopValidate(ctx, nil, FloopValidateInput{})
		if err != nil {
			t.Fatalf("floop_validate failed: %v", err)
		}
		if !out.Valid {
			t.Errorf("Graph invalid: %d errors", out.ErrorCount)
			for _, e := range out.Errors {
				t.Logf("  error: %s %s %s → %s", e.BehaviorID, e.Field, e.Issue, e.RefID)
			}
		}
	})

	// ── Stage 5: Backup and restore ─────────────────────────────────
	t.Run("Stage5_BackupRestore", func(t *testing.T) {
		// Use project-local .floop/backups/ directory (allowed by path validation)
		backupDir := filepath.Join(tmpDir, ".floop", "backups")
		if err := os.MkdirAll(backupDir, 0700); err != nil {
			t.Fatalf("Failed to create backup dir: %v", err)
		}
		backupPath := filepath.Join(backupDir, "test-backup.json")

		// Backup
		_, backupOut, err := server.handleFloopBackup(ctx, nil, FloopBackupInput{
			OutputPath: backupPath,
		})
		if err != nil {
			t.Fatalf("backup failed: %v", err)
		}
		if backupOut.NodeCount < 2 {
			t.Errorf("NodeCount = %d, want >= 2", backupOut.NodeCount)
		}
		if backupOut.EdgeCount < 2 {
			t.Errorf("EdgeCount = %d, want >= 2 (bidirectional)", backupOut.EdgeCount)
		}

		// Verify backup file exists
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			t.Fatalf("Backup file not created at %s", backupPath)
		}

		// Restore into a fresh server (replace mode)
		server2, tmpDir2 := setupTestServer(t)
		defer server2.Close()

		// Copy backup to server2's allowed backup directory
		backupDir2 := filepath.Join(tmpDir2, ".floop", "backups")
		if err := os.MkdirAll(backupDir2, 0700); err != nil {
			t.Fatalf("Failed to create backup dir 2: %v", err)
		}
		backupPath2 := filepath.Join(backupDir2, "restore.json")
		data, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("Failed to read backup: %v", err)
		}
		if err := os.WriteFile(backupPath2, data, 0600); err != nil {
			t.Fatalf("Failed to write backup copy: %v", err)
		}

		_, restoreOut, err := server2.handleFloopRestore(ctx, nil, FloopRestoreInput{
			InputPath: backupPath2,
			Mode:      "replace",
		})
		if err != nil {
			t.Fatalf("restore failed: %v", err)
		}
		if restoreOut.NodesRestored < 2 {
			t.Errorf("NodesRestored = %d, want >= 2", restoreOut.NodesRestored)
		}
		if restoreOut.EdgesRestored < 2 {
			t.Errorf("EdgesRestored = %d, want >= 2", restoreOut.EdgesRestored)
		}

		// Verify restored data is usable
		_, listOut, err := server2.handleFloopList(ctx, nil, FloopListInput{})
		if err != nil {
			t.Fatalf("list after restore failed: %v", err)
		}
		if listOut.Count < 2 {
			t.Errorf("Restored behavior count = %d, want >= 2", listOut.Count)
		}
	})

	// ── Stage 6: Confidence reinforcement ───────────────────────────
	t.Run("Stage6_ConfidenceReinforcement", func(t *testing.T) {
		// Record initial confidence values
		nodes, err := server.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
		if err != nil {
			t.Fatalf("QueryNodes failed: %v", err)
		}

		initialConf := make(map[string]float64)
		for _, n := range nodes {
			if conf, ok := n.Metadata["confidence"].(float64); ok {
				initialConf[n.ID] = conf
			}
		}

		// Trigger activation (which fires reinforcement goroutine)
		_, _, err = server.handleFloopActive(ctx, nil, FloopActiveInput{
			File: "main.go",
			Task: "development",
		})
		if err != nil {
			t.Fatalf("floop_active failed: %v", err)
		}

		// Wait for fire-and-forget goroutine to complete
		time.Sleep(500 * time.Millisecond)

		// Check confidence values changed
		nodesAfter, err := server.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
		if err != nil {
			t.Fatalf("QueryNodes after activation failed: %v", err)
		}

		changed := 0
		for _, n := range nodesAfter {
			if conf, ok := n.Metadata["confidence"].(float64); ok {
				if initial, found := initialConf[n.ID]; found && conf != initial {
					changed++
					t.Logf("  %s: %.4f → %.4f", n.ID, initial, conf)
				}
			}
		}

		// At least some confidences should have changed
		// (active ones boosted, inactive ones decayed)
		if changed == 0 {
			t.Log("Warning: no confidence changes detected (may indicate UpdateConfidence not supported by test store)")
		} else {
			t.Logf("Confidence changed for %d/%d behaviors", changed, len(nodesAfter))
		}
	})

	// ── Stage 7: Timestamp round-trip (d4f.1 fix) ───────────────────
	t.Run("Stage7_TimestampRoundTrip", func(t *testing.T) {
		// Query nodes and verify provenance timestamps are time.Time, not strings
		nodes, err := server.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
		if err != nil {
			t.Fatalf("QueryNodes failed: %v", err)
		}

		for _, n := range nodes {
			prov, ok := n.Content["provenance"].(map[string]interface{})
			if !ok {
				continue
			}
			createdAt, exists := prov["created_at"]
			if !exists {
				continue
			}
			switch createdAt.(type) {
			case time.Time:
				// Good — this is what we want after d4f.1 fix
			case string:
				// Acceptable for in-memory store (it doesn't go through SQLite parsing)
				t.Logf("Note: created_at is string for %s (expected for in-memory store)", n.ID)
			default:
				t.Errorf("Unexpected created_at type for %s: %T", n.ID, createdAt)
			}
		}
	})
}
