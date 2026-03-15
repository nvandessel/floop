package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/utils"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

func newEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Inspect and manage the raw event buffer",
		RunE:  runEvents,
	}
	cmd.Flags().String("session", "", "Filter by session ID")
	cmd.Flags().String("prune", "", "Delete events older than duration (e.g., 90d, 24h)")
	cmd.Flags().Bool("count", false, "Show event count only")
	return cmd
}

func runEvents(cmd *cobra.Command, args []string) error {
	session, _ := cmd.Flags().GetString("session")
	pruneStr, _ := cmd.Flags().GetString("prune")
	countOnly, _ := cmd.Flags().GetBool("count")
	jsonOut, _ := cmd.Flags().GetBool("json")
	out := cmd.OutOrStdout()

	// Open global DB
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
	es := events.NewSQLiteEventStore(db)
	if err := es.InitSchema(ctx); err != nil {
		return fmt.Errorf("initializing events schema: %w", err)
	}

	// Handle prune
	if pruneStr != "" {
		dur, parseErr := utils.ParseDuration(pruneStr)
		if parseErr != nil {
			return fmt.Errorf("parsing --prune duration: %w", parseErr)
		}
		pruned, pruneErr := es.Prune(ctx, dur)
		if pruneErr != nil {
			return fmt.Errorf("pruning events: %w", pruneErr)
		}
		if jsonOut {
			json.NewEncoder(out).Encode(map[string]interface{}{
				"status": "pruned",
				"count":  pruned,
			})
		} else {
			fmt.Fprintf(out, "Pruned %d events older than %s.\n", pruned, pruneStr)
		}
		return nil
	}

	// Handle count
	if countOnly {
		count, countErr := es.Count(ctx)
		if countErr != nil {
			return fmt.Errorf("counting events: %w", countErr)
		}
		if jsonOut {
			json.NewEncoder(out).Encode(map[string]interface{}{
				"status": "count",
				"count":  count,
			})
		} else {
			fmt.Fprintf(out, "Event count: %d\n", count)
		}
		return nil
	}

	// List events
	var evts []events.Event
	if session != "" {
		evts, err = es.GetBySession(ctx, session)
	} else {
		evts, err = es.GetUnconsolidated(ctx)
	}
	if err != nil {
		return fmt.Errorf("querying events: %w", err)
	}

	if jsonOut {
		json.NewEncoder(out).Encode(map[string]interface{}{
			"events": evts,
			"count":  len(evts),
		})
	} else {
		if len(evts) == 0 {
			fmt.Fprintln(out, "No events found.")
			return nil
		}
		fmt.Fprintf(out, "Events (%d):\n\n", len(evts))
		for i, e := range evts {
			content := e.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			fmt.Fprintf(out, "%d. [%s] %s (%s/%s)\n", i+1, e.Timestamp.Format(time.RFC3339), content, e.Actor, e.Kind)
			fmt.Fprintf(out, "   Session: %s\n", e.SessionID)
			fmt.Fprintln(out)
		}
	}

	return nil
}
