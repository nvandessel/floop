package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/utils"
	_ "modernc.org/sqlite"
)

func TestNewEventsCmd(t *testing.T) {
	cmd := newEventsCmd()
	if cmd.Use != "events" {
		t.Errorf("Use = %q, want %q", cmd.Use, "events")
	}

	// Check flags exist
	for _, flag := range []string{"session", "prune", "count"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("missing --%s flag", flag)
		}
	}
}

func TestEventsCmdCountEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newEventsCmd())
	rootCmd.SetArgs([]string{"events", "--count", "--json"})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("events --count failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	count, ok := result["count"].(float64)
	if !ok || count != 0 {
		t.Errorf("count = %v, want 0", result["count"])
	}
}

func TestEventsCmdCountWithEvents(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	// Insert events
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
	for i := 0; i < 3; i++ {
		if err := es.Add(ctx, events.Event{
			ID:        "evt-" + time.Now().Format("20060102150405.000000000") + "-" + string(rune('a'+i)),
			SessionID: "session-1",
			Timestamp: now,
			Source:    "test",
			Actor:     events.ActorUser,
			Kind:      events.KindMessage,
			Content:   "test event content that is long enough",
			CreatedAt: now,
		}); err != nil {
			t.Fatalf("failed to add event: %v", err)
		}
	}
	db.Close()

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newEventsCmd())
	rootCmd.SetArgs([]string{"events", "--count", "--json"})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("events --count failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	count, ok := result["count"].(float64)
	if !ok || count != 3 {
		t.Errorf("count = %v, want 3", result["count"])
	}
}

func TestEventsCmdPrune(t *testing.T) {
	tmpDir := t.TempDir()
	isolateHome(t, tmpDir)

	globalDir := filepath.Join(tmpDir, "home", ".floop")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatalf("failed to create global dir: %v", err)
	}

	// Insert an old event
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

	oldTime := time.Now().Add(-100 * 24 * time.Hour)
	if err := es.Add(ctx, events.Event{
		ID:        "old-evt-1",
		SessionID: "old-session",
		Timestamp: oldTime,
		Source:    "test",
		Actor:     events.ActorUser,
		Kind:      events.KindMessage,
		Content:   "this is an old event that should be pruned",
		CreatedAt: oldTime,
	}); err != nil {
		t.Fatalf("failed to add event: %v", err)
	}

	db.Close()

	rootCmd := newTestRootCmd()
	rootCmd.AddCommand(newEventsCmd())
	rootCmd.SetArgs([]string{"events", "--prune", "90d", "--json"})
	var outBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("events --prune failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if result["status"] != "pruned" {
		t.Errorf("status = %v, want %q", result["status"], "pruned")
	}
	count, ok := result["count"].(float64)
	if !ok || count != 1 {
		t.Errorf("pruned count = %v, want 1", result["count"])
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"24h", 24 * time.Hour, false},
		{"1h30m", 90 * time.Minute, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"90d", 90 * 24 * time.Hour, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := utils.ParseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("utils.ParseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("utils.ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
