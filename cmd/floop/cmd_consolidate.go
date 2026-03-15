package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvandessel/floop/internal/consolidation"
	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/utils"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

func newConsolidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consolidate",
		Short: "Run consolidation pipeline on raw events",
		Long:  `Extracts, classifies, and promotes memories from the event buffer.`,
		RunE:  runConsolidate,
	}
	cmd.Flags().String("session", "", "Consolidate specific session only")
	cmd.Flags().String("since", "", "Consolidate events since duration (e.g., 24h)")
	cmd.Flags().Bool("dry-run", false, "Show what would be extracted without promoting")
	return cmd
}

func runConsolidate(cmd *cobra.Command, args []string) error {
	session, _ := cmd.Flags().GetString("session")
	since, _ := cmd.Flags().GetString("since")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
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

	// Query events based on flags
	var evts []events.Event
	switch {
	case session != "":
		evts, err = es.GetBySession(ctx, session)
	case since != "":
		dur, parseErr := utils.ParseDuration(since)
		if parseErr != nil {
			return fmt.Errorf("parsing --since duration: %w", parseErr)
		}
		evts, err = es.GetSince(ctx, time.Now().Add(-dur))
	default:
		evts, err = es.GetUnconsolidated(ctx)
	}
	if err != nil {
		return fmt.Errorf("querying events: %w", err)
	}

	if len(evts) == 0 {
		if jsonOut {
			json.NewEncoder(out).Encode(map[string]interface{}{
				"status":     "no_events",
				"candidates": 0,
				"classified": 0,
				"promoted":   0,
			})
		} else {
			fmt.Fprintln(out, "No events to consolidate.")
		}
		return nil
	}

	// Open graph store for promotion (unless dry-run)
	var graphStore store.GraphStore
	if !dryRun {
		root, _ := cmd.Flags().GetString("root")
		graphStore, err = store.NewSQLiteGraphStore(root)
		if err != nil {
			return fmt.Errorf("opening graph store: %w", err)
		}
		defer graphStore.Close()
	}

	// Run consolidation pipeline
	consolidator := consolidation.NewHeuristicConsolidator()
	runner := consolidation.NewRunner(consolidator)

	result, err := runner.Run(ctx, evts, graphStore, consolidation.RunOptions{
		DryRun: dryRun,
	})
	if err != nil {
		return fmt.Errorf("consolidation pipeline: %w", err)
	}

	if jsonOut {
		json.NewEncoder(out).Encode(map[string]interface{}{
			"status":     "completed",
			"events":     len(evts),
			"candidates": len(result.Candidates),
			"classified": len(result.Classified),
			"promoted":   result.Promoted,
			"duration":   result.Duration.String(),
			"dry_run":    dryRun,
		})
	} else {
		fmt.Fprintf(out, "Consolidation complete:\n")
		fmt.Fprintf(out, "  Events processed: %d\n", len(evts))
		fmt.Fprintf(out, "  Candidates found: %d\n", len(result.Candidates))
		fmt.Fprintf(out, "  Classified:       %d\n", len(result.Classified))
		if !dryRun {
			fmt.Fprintf(out, "  Promoted:         %d\n", result.Promoted)
		}
		fmt.Fprintf(out, "  Duration:         %s\n", result.Duration)
		if dryRun {
			fmt.Fprintln(out, "\n  (dry-run: no memories were promoted)")
		}
	}

	return nil
}
