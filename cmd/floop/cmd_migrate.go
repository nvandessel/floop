package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nvandessel/floop/internal/project"
	"github.com/nvandessel/floop/internal/store"
	"github.com/spf13/cobra"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration utilities",
		RunE:  runMigrate,
	}
	cmd.Flags().Bool("merge-local-to-global", false, "Merge local .floop/floop.db into global store")
	return cmd
}

func runMigrate(cmd *cobra.Command, args []string) error {
	mergeLocal, _ := cmd.Flags().GetBool("merge-local-to-global")
	jsonOut, _ := cmd.Flags().GetBool("json")
	out := cmd.OutOrStdout()

	if !mergeLocal {
		return fmt.Errorf("no migration action specified; use --merge-local-to-global")
	}

	root, _ := cmd.Flags().GetString("root")
	ctx := context.Background()

	// Resolve project ID
	projectID, err := project.ResolveProjectID(root)
	if err != nil {
		return fmt.Errorf("resolving project ID: %w", err)
	}

	// Check local store exists
	localDBPath := filepath.Join(root, ".floop", "floop.db")
	if _, err := os.Stat(localDBPath); os.IsNotExist(err) {
		return fmt.Errorf("no local store found at %s", localDBPath)
	}

	// Open local store
	localStore, err := store.NewSQLiteGraphStore(root)
	if err != nil {
		return fmt.Errorf("opening local store: %w", err)
	}
	defer localStore.Close()

	// Open global store
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	globalRoot := homeDir
	globalFloopDir := filepath.Join(globalRoot, ".floop")
	if err := os.MkdirAll(globalFloopDir, 0700); err != nil {
		return fmt.Errorf("creating global .floop directory: %w", err)
	}

	globalStore, err := store.NewSQLiteGraphStore(globalRoot)
	if err != nil {
		return fmt.Errorf("opening global store: %w", err)
	}
	defer globalStore.Close()

	// Read all behaviors from local store
	nodes, err := localStore.QueryNodes(ctx, map[string]interface{}{"kind": string(store.NodeKindBehavior)})
	if err != nil {
		return fmt.Errorf("querying local behaviors: %w", err)
	}

	migrated := 0
	skipped := 0
	for _, node := range nodes {
		// Stamp scope and project ID
		if node.Metadata == nil {
			node.Metadata = make(map[string]interface{})
		}
		if projectID != "" {
			node.Metadata["project_id"] = projectID
			node.Metadata["scope"] = "project:" + projectID
		} else {
			node.Metadata["scope"] = "local"
		}

		// Try to insert; skip duplicates
		_, err := globalStore.AddNode(ctx, node)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "duplicate") {
				skipped++
				continue
			}
			return fmt.Errorf("migrating behavior %s: %w", node.ID, err)
		}
		migrated++
	}

	if jsonOut {
		json.NewEncoder(out).Encode(map[string]interface{}{
			"status":     "completed",
			"migrated":   migrated,
			"skipped":    skipped,
			"total":      len(nodes),
			"project_id": projectID,
		})
	} else {
		fmt.Fprintf(out, "Migration complete:\n")
		fmt.Fprintf(out, "  Total behaviors: %d\n", len(nodes))
		fmt.Fprintf(out, "  Migrated:        %d\n", migrated)
		fmt.Fprintf(out, "  Skipped:         %d\n", skipped)
		if projectID != "" {
			fmt.Fprintf(out, "  Project ID:      %s\n", projectID)
		}
	}

	return nil
}
