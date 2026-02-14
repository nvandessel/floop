package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvandessel/feedback-loop/internal/backup"
	"github.com/spf13/cobra"
)

func newBackupListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all backups with metadata",
		Long: `List all backup files in the default backup directory with version,
format, size, and node/edge counts.

Examples:
  floop backup list
  floop backup list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")

			dir, err := backup.DefaultBackupDir()
			if err != nil {
				return fmt.Errorf("failed to get backup directory: %w", err)
			}

			backups, err := backup.ListBackups(dir)
			if err != nil {
				return fmt.Errorf("failed to list backups: %w", err)
			}

			if jsonOut {
				type jsonEntry struct {
					Path      string `json:"path"`
					Version   int    `json:"version"`
					Size      int64  `json:"size_bytes"`
					CreatedAt string `json:"created_at"`
					NodeCount int    `json:"node_count,omitempty"`
					EdgeCount int    `json:"edge_count,omitempty"`
					Checksum  string `json:"checksum,omitempty"`
				}
				entries := make([]jsonEntry, 0, len(backups))
				for _, b := range backups {
					entry := jsonEntry{
						Path:      b.Path,
						Version:   b.Version,
						Size:      b.Size,
						CreatedAt: b.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
					}
					if b.Version == backup.FormatV2 {
						if header, err := backup.ReadV2Header(b.Path); err == nil {
							entry.NodeCount = header.NodeCount
							entry.EdgeCount = header.EdgeCount
							entry.Checksum = header.Checksum
						}
					}
					entries = append(entries, entry)
				}
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"backups":     entries,
					"total_count": len(entries),
					"directory":   dir,
				})
			}

			if len(backups) == 0 {
				fmt.Printf("No backups found in %s\n", dir)
				return nil
			}

			fmt.Printf("Backups in %s:\n", dir)
			var totalSize int64
			for _, b := range backups {
				totalSize += b.Size

				versionStr := "v1"
				formatStr := "json"
				nodeCount := 0
				edgeCount := 0

				if b.Version == backup.FormatV2 {
					versionStr = "v2"
					formatStr = "gzip"
					if header, err := backup.ReadV2Header(b.Path); err == nil {
						nodeCount = header.NodeCount
						edgeCount = header.EdgeCount
					}
				}

				fmt.Printf("  %s  %s  %s  %s  %d nodes  %d edges  %s\n",
					b.CreatedAt.Format("2006-01-02 15:04"),
					versionStr,
					formatStr,
					formatBytes(b.Size),
					nodeCount,
					edgeCount,
					filepath.Base(b.Path),
				)
			}
			fmt.Printf("Total: %d backups, %s\n", len(backups), formatBytes(totalSize))
			return nil
		},
	}

	return cmd
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1fKB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
