package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nvandessel/feedback-loop/internal/backup"
	"github.com/spf13/cobra"
)

func newBackupVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <file>",
		Short: "Verify backup file integrity",
		Long: `Verify the integrity of a backup file by checking its SHA-256 checksum.
Only applicable to V2 (compressed) backup files.

Examples:
  floop backup verify ~/.floop/backups/floop-backup-20260206-120000.json.gz`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			jsonOut, _ := cmd.Flags().GetBool("json")

			version, err := backup.DetectFormat(filePath)
			if err != nil {
				if jsonOut {
					return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"file":    filePath,
						"valid":   false,
						"error":   err.Error(),
						"message": fmt.Sprintf("Failed to detect format: %v", err),
					})
				}
				return fmt.Errorf("failed to detect format: %w", err)
			}

			if version == backup.FormatV1 {
				if jsonOut {
					return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"file":    filePath,
						"version": 1,
						"valid":   true,
						"message": "V1 format: no checksum to verify (integrity check N/A)",
					})
				}
				fmt.Printf("V1 format: no checksum to verify (integrity check N/A)\n")
				fmt.Printf("  File: %s\n", filePath)
				return nil
			}

			err = backup.VerifyChecksum(filePath)
			if err != nil {
				if jsonOut {
					return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"file":    filePath,
						"version": 2,
						"valid":   false,
						"error":   err.Error(),
						"message": "Checksum verification FAILED",
					})
				}
				fmt.Printf("FAILED: %v\n", err)
				fmt.Printf("  File: %s\n", filePath)
				return fmt.Errorf("checksum verification failed")
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"file":    filePath,
					"version": 2,
					"valid":   true,
					"message": "Checksum OK",
				})
			}

			fmt.Printf("OK: checksum verified\n")
			fmt.Printf("  File: %s\n", filePath)
			return nil
		},
	}

	return cmd
}
