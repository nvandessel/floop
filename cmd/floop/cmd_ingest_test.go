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

	"github.com/nvandessel/floop/internal/events"
	_ "modernc.org/sqlite"
)

func TestNewIngestCmd(t *testing.T) {
	cmd := newIngestCmd()
	if cmd.Use != "ingest [file]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "ingest [file]")
	}

	// Check flags exist
	for _, flag := range []string{"format", "source", "session"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestIngestCmdFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	// Create a markdown transcript file
	transcriptPath := filepath.Join(tmpDir, "transcript.md")
	content := "User: No, don't use fmt.Println for logging, use slog instead\n\nAssistant: I'll switch to slog.\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	// Ensure global .floop dir exists
	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	// Run ingest command
	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newIngestCmd())
	rootCmd.SetArgs([]string{"ingest", transcriptPath, "--format", "markdown", "--source", "test-agent", "--session", "test-session", "--json"})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	// Parse JSON output
	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if result["status"] != "ingested" {
		t.Errorf("status = %v, want %q", result["status"], "ingested")
	}
	count, ok := result["count"].(float64)
	if !ok || count < 1 {
		t.Errorf("count = %v, want >= 1", result["count"])
	}

	// Verify events were stored in the database
	dbPath := filepath.Join(globalDir, "floop.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	es := events.NewSQLiteEventStore(db)
	ctx := context.Background()
	stored, err := es.GetBySession(ctx, "test-session")
	if err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	if len(stored) == 0 {
		t.Error("no events stored in database")
	}
	for _, e := range stored {
		if e.Source != "test-agent" {
			t.Errorf("event source = %q, want %q", e.Source, "test-agent")
		}
		if e.SessionID != "test-session" {
			t.Errorf("event session_id = %q, want %q", e.SessionID, "test-session")
		}
	}
}

func TestIngestCmdUnknownFormat(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newIngestCmd())
	rootCmd.SetArgs([]string{"ingest", "/dev/null", "--format", "unknown-format"})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error = %q, want it to contain 'unknown format'", err.Error())
	}
}

func TestIngestCmdNonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newIngestCmd())
	rootCmd.SetArgs([]string{"ingest", "/nonexistent/file.md", "--format", "markdown"})
	rootCmd.SetOut(&bytes.Buffer{})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "opening file") {
		t.Errorf("error = %q, want it to contain 'opening file'", err.Error())
	}
}
