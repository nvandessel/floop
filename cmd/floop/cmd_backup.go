package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvandessel/feedback-loop/internal/backup"
	"github.com/nvandessel/feedback-loop/internal/config"
	"github.com/nvandessel/feedback-loop/internal/pathutil"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Export full graph state to a backup file",
		Long: `Backup the complete behavior graph (nodes + edges) to a compressed file.

Default location: ~/.floop/backups/floop-backup-YYYYMMDD-HHMMSS.json.gz
Keeps backups according to retention policy (default: last 10).

Examples:
  floop backup                              # Backup to default location (V2 compressed)
  floop backup --output my-backup.json.gz   # Backup to specific file
  floop backup --no-compress                # Create V1 uncompressed backup
  floop backup list                         # List all backups
  floop backup verify <file>                # Verify backup integrity`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			outputPath, _ := cmd.Flags().GetString("output")
			noCompress, _ := cmd.Flags().GetBool("no-compress")

			cfg, err := config.Load()
			if err != nil {
				cfg = config.Default()
			}

			compress := cfg.Backup.Compression && !noCompress

			if outputPath == "" {
				dir, err := backup.DefaultBackupDir()
				if err != nil {
					return fmt.Errorf("failed to get backup directory: %w", err)
				}
				if compress {
					outputPath = backup.GenerateBackupPath(dir)
				} else {
					outputPath = backup.GenerateBackupPathV1(dir)
				}
			} else {
				allowedDirs, err := pathutil.DefaultAllowedBackupDirsWithProjectRoot(root)
				if err != nil {
					return fmt.Errorf("failed to determine allowed backup dirs: %w", err)
				}
				if err := pathutil.ValidatePath(outputPath, allowedDirs); err != nil {
					return fmt.Errorf("backup path rejected: %w", err)
				}
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			result, err := backup.BackupWithOptions(ctx, graphStore, outputPath, compress)
			if err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}

			// Apply retention policy
			policy := buildRetentionPolicy(&cfg.Backup)
			dir := filepath.Dir(outputPath)
			if _, err := backup.ApplyRetention(dir, policy); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to apply retention: %v\n", err)
			}

			if jsonOut {
				info, _ := os.Stat(outputPath)
				var sizeBytes int64
				if info != nil {
					sizeBytes = info.Size()
				}
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"path":       outputPath,
					"node_count": len(result.Nodes),
					"edge_count": len(result.Edges),
					"version":    result.Version,
					"compressed": compress,
					"size_bytes": sizeBytes,
					"message":    fmt.Sprintf("Backup created: %d nodes, %d edges", len(result.Nodes), len(result.Edges)),
				})
			}

			versionLabel := "v2/gzip"
			if !compress {
				versionLabel = "v1/json"
			}
			fmt.Printf("Backup created: %d nodes, %d edges (%s)\n", len(result.Nodes), len(result.Edges), versionLabel)
			fmt.Printf("  Path: %s\n", outputPath)
			return nil
		},
	}

	cmd.Flags().String("output", "", "Output file path (default: auto-generated in ~/.floop/backups/)")
	cmd.Flags().Bool("no-compress", false, "Create V1 uncompressed backup instead of V2 compressed")

	// Add subcommands
	cmd.AddCommand(
		newBackupListCmd(),
		newBackupVerifyCmd(),
	)

	return cmd
}

// buildRetentionPolicy constructs a retention policy from config.
func buildRetentionPolicy(cfg *config.BackupConfig) backup.RetentionPolicy {
	var policies []backup.RetentionPolicy

	if cfg.Retention.MaxCount > 0 {
		policies = append(policies, &backup.CountPolicy{MaxCount: cfg.Retention.MaxCount})
	}

	if cfg.Retention.MaxAge != "" {
		if d, err := backup.ParseDuration(cfg.Retention.MaxAge); err == nil {
			policies = append(policies, &backup.AgePolicy{MaxAge: d})
		}
	}

	if cfg.Retention.MaxTotalSize != "" {
		if s, err := backup.ParseSize(cfg.Retention.MaxTotalSize); err == nil {
			policies = append(policies, &backup.SizePolicy{MaxTotalBytes: s})
		}
	}

	if len(policies) == 0 {
		return &backup.CountPolicy{MaxCount: 10}
	}

	if len(policies) == 1 {
		return policies[0]
	}

	return &backup.CompositePolicy{Policies: policies}
}

func newRestoreFromBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore-backup <file>",
		Short: "Restore graph state from a backup file",
		Long: `Restore behavior graph from a backup file (V1 or V2 format).
Format is auto-detected.

Modes:
  merge   - Skip existing nodes/edges (default)
  replace - Clear store first, then restore

Examples:
  floop restore-backup ~/.floop/backups/floop-backup-20260206-120000.json.gz
  floop restore-backup backup.json --mode replace`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputPath := args[0]
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			mode, _ := cmd.Flags().GetString("mode")

			allowedDirs, err := pathutil.DefaultAllowedBackupDirsWithProjectRoot(root)
			if err != nil {
				return fmt.Errorf("failed to determine allowed backup dirs: %w", err)
			}
			if err := pathutil.ValidatePath(inputPath, allowedDirs); err != nil {
				return fmt.Errorf("restore path rejected: %w", err)
			}

			restoreMode := backup.RestoreMerge
			if mode == "replace" {
				restoreMode = backup.RestoreReplace
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			result, err := backup.Restore(ctx, graphStore, inputPath, restoreMode)
			if err != nil {
				return fmt.Errorf("restore failed: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"nodes_restored": result.NodesRestored,
					"nodes_skipped":  result.NodesSkipped,
					"edges_restored": result.EdgesRestored,
					"edges_skipped":  result.EdgesSkipped,
					"message":        fmt.Sprintf("Restore complete: %d nodes, %d edges", result.NodesRestored, result.EdgesRestored),
				})
			}

			fmt.Printf("Restore complete (mode: %s)\n", mode)
			fmt.Printf("  Nodes: %d restored, %d skipped\n", result.NodesRestored, result.NodesSkipped)
			fmt.Printf("  Edges: %d restored, %d skipped\n", result.EdgesRestored, result.EdgesSkipped)
			return nil
		},
	}

	cmd.Flags().String("mode", "merge", "Restore mode: merge or replace")

	return cmd
}
