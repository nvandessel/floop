package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nvandessel/floop/internal/events"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

func init() {
	// Register transcript adapters so GetAdapter can find them.
	events.RegisterAdapter(&events.MarkdownAdapter{})
	events.RegisterAdapter(&events.JSONLAdapter{})
	events.RegisterAdapter(&events.JSONAdapter{})
}

func newIngestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest [file]",
		Short: "Import conversation transcript into event buffer",
		Long:  `Parses a transcript file (or stdin) and stores events for consolidation.`,
		RunE:  runIngest,
	}
	cmd.Flags().String("format", "markdown", "Transcript format (markdown, claude-code-jsonl, generic-json)")
	cmd.Flags().String("source", "", "Agent source identifier (e.g., claude-code, gemini)")
	cmd.Flags().String("session", "", "Session ID (auto-generated if empty)")
	return cmd
}

func runIngest(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")
	source, _ := cmd.Flags().GetString("source")
	session, _ := cmd.Flags().GetString("session")
	jsonOut, _ := cmd.Flags().GetBool("json")

	// Get adapter
	adapter, ok := events.GetAdapter(format)
	if !ok {
		available := events.AvailableFormats()
		return fmt.Errorf("unknown format %q, available: %s", format, strings.Join(available, ", "))
	}

	// Open reader: file argument or stdin
	var reader *os.File
	if len(args) > 0 {
		f, err := os.Open(args[0])
		if err != nil {
			return fmt.Errorf("opening file: %w", err)
		}
		defer f.Close()
		reader = f
	} else {
		reader = os.Stdin
	}

	// Parse transcript
	parsed, err := adapter.Parse(reader)
	if err != nil {
		return fmt.Errorf("parsing transcript: %w", err)
	}

	// Stamp source and session on all events
	for i := range parsed {
		if source != "" {
			parsed[i].Source = source
		}
		if session != "" {
			parsed[i].SessionID = session
		}
	}

	// Open global store DB
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	dbDir := filepath.Join(homeDir, ".floop")
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return fmt.Errorf("creating .floop directory: %w", err)
	}
	dbPath := filepath.Join(dbDir, "floop.db")

	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Create event store and init schema
	es := events.NewSQLiteEventStore(db)
	if err := es.InitSchema(ctx); err != nil {
		return fmt.Errorf("initializing events schema: %w", err)
	}

	// Add events in batch
	if err := es.AddBatch(ctx, parsed); err != nil {
		return fmt.Errorf("adding events: %w", err)
	}

	out := cmd.OutOrStdout()
	if jsonOut {
		json.NewEncoder(out).Encode(map[string]interface{}{
			"status": "ingested",
			"count":  len(parsed),
			"format": format,
		})
	} else {
		fmt.Fprintf(out, "Ingested %d events from %s format.\n", len(parsed), format)
	}

	return nil
}
