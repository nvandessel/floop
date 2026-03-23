package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/events"
	_ "modernc.org/sqlite"
)

func TestNewConsolidateCmd(t *testing.T) {
	cmd := newConsolidateCmd()
	if cmd.Use != "consolidate" {
		t.Errorf("Use = %q, want %q", cmd.Use, "consolidate")
	}

	// Check flags exist
	for _, flag := range []string{"session", "since", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestConsolidateCmdNoEvents(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Ensure global .floop dir and DB exist
	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConsolidateCmd())
	rootCmd.SetArgs([]string{"consolidate", "--json", "--root", tmpDir})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("consolidate failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if result["status"] != "no_events" {
		t.Errorf("status = %v, want %q", result["status"], "no_events")
	}
}

func TestConsolidateCmdDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Ensure global .floop dir
	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	// Insert some test events directly
	dbPath := filepath.Join(globalDir, "floop.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	es := events.NewSQLiteEventStore(db)
	ctx := context.Background()
	if err := es.InitSchema(ctx); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	now := time.Now()
	testEvents := []events.Event{
		{
			ID:        "test-evt-1",
			SessionID: "session-1",
			Timestamp: now,
			Source:    "test",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "No, don't use os.path, use pathlib.Path instead please",
			CreatedAt: now,
		},
	}
	if err := es.AddBatch(ctx, testEvents); err != nil {
		t.Fatalf("failed to add events: %v", err)
	}
	db.Close()

	// Run consolidate with dry-run
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConsolidateCmd())
	rootCmd.SetArgs([]string{"consolidate", "--dry-run", "--json", "--root", tmpDir})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("consolidate dry-run failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if result["status"] != "completed" {
		t.Errorf("status = %v, want %q", result["status"], "completed")
	}
	if result["dry_run"] != true {
		t.Errorf("dry_run = %v, want true", result["dry_run"])
	}
	candidates, ok := result["candidates"].(float64)
	if !ok || candidates < 1 {
		t.Errorf("candidates = %v, want >= 1", result["candidates"])
	}
}

func TestConsolidateCmdNoEventsText(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConsolidateCmd())
	rootCmd.SetArgs([]string{"consolidate", "--root", tmpDir})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("consolidate text failed: %v", err)
	}
	if !strings.Contains(outBuf.String(), "No events to consolidate") {
		t.Errorf("expected 'No events to consolidate', got: %s", outBuf.String())
	}
}

func TestConsolidateCmdWithSession(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConsolidateCmd())
	rootCmd.SetArgs([]string{"consolidate", "--session", "test-session", "--json", "--root", tmpDir})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("consolidate --session failed: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}
	if result["status"] != "no_events" {
		t.Errorf("status = %v, want %q", result["status"], "no_events")
	}
}

func TestConsolidateCmdWithSince(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConsolidateCmd())
	rootCmd.SetArgs([]string{"consolidate", "--since", "24h", "--json", "--root", tmpDir})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("consolidate --since failed: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}
	if result["status"] != "no_events" {
		t.Errorf("status = %v, want %q", result["status"], "no_events")
	}
}

func TestConsolidateCmdDryRunText(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	dbPath := filepath.Join(globalDir, "floop.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	es := events.NewSQLiteEventStore(db)
	ctx := context.Background()
	if err := es.InitSchema(ctx); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}
	now := time.Now()
	if err := es.AddBatch(ctx, []events.Event{{
		ID: "test-evt-text-1", SessionID: "session-text",
		Timestamp: now, Source: "test", Actor: events.ActorUser,
		Kind: events.KindMessage, Content: "No, don't use os.path, use pathlib.Path",
		CreatedAt: now,
	}}); err != nil {
		t.Fatalf("failed to add events: %v", err)
	}
	db.Close()

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newConsolidateCmd())
	rootCmd.SetArgs([]string{"consolidate", "--dry-run", "--root", tmpDir})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("consolidate dry-run text failed: %v", err)
	}
	output := outBuf.String()
	if !strings.Contains(output, "Consolidation complete") {
		t.Errorf("expected 'Consolidation complete', got: %s", output)
	}
	if !strings.Contains(output, "dry-run") {
		t.Errorf("expected 'dry-run' in output, got: %s", output)
	}
}
