package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/backup"
	"github.com/nvandessel/floop/internal/pathutil"
	"github.com/nvandessel/floop/internal/ratelimit"
)

// handleFloopBackup implements the floop_backup tool.
func (s *Server) handleFloopBackup(ctx context.Context, req *sdk.CallToolRequest, args FloopBackupInput) (_ *sdk.CallToolResult, _ FloopBackupOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_backup", start, retErr, sanitizeToolParams("floop_backup", map[string]interface{}{
			"output_path": args.OutputPath,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_backup"); err != nil {
		return nil, FloopBackupOutput{}, err
	}

	outputPath := args.OutputPath
	if outputPath == "" {
		// Default path -- controlled by us, no validation needed
		backupDir, err := backup.DefaultBackupDir()
		if err != nil {
			return nil, FloopBackupOutput{}, fmt.Errorf("failed to get backup directory: %w", err)
		}
		outputPath = backup.GenerateBackupPath(backupDir)
	} else {
		// User-specified path -- validate against allowed directories
		allowedDirs, err := pathutil.DefaultAllowedBackupDirsWithProjectRoot(s.root)
		if err != nil {
			return nil, FloopBackupOutput{}, fmt.Errorf("failed to determine allowed backup dirs: %w", err)
		}
		if err := pathutil.ValidatePath(outputPath, allowedDirs); err != nil {
			return nil, FloopBackupOutput{}, fmt.Errorf("backup path rejected: %w", err)
		}
	}

	result, err := backup.BackupWithOptions(ctx, s.store, outputPath, backup.BackupOptions{
		Compress:     true,
		FloopVersion: s.floopVersion,
	})
	if err != nil {
		return nil, FloopBackupOutput{}, fmt.Errorf("backup failed: %w", err)
	}

	// Apply retention policy
	backupDir := filepath.Dir(outputPath)
	if _, err := backup.ApplyRetention(backupDir, s.retentionPolicy); err != nil {
		s.logger.Warn("failed to apply retention", "error", err)
	}

	// Get file size for output
	var sizeBytes int64
	if info, err := os.Stat(outputPath); err == nil {
		sizeBytes = info.Size()
	}

	// Read header metadata for output
	var schemaVersion int
	var metadata map[string]string
	if result.Version == backup.FormatV2 {
		if header, err := backup.ReadV2Header(outputPath); err == nil {
			schemaVersion = header.SchemaVersion
			metadata = header.Metadata
		}
	}

	return nil, FloopBackupOutput{
		Path:          outputPath,
		NodeCount:     len(result.Nodes),
		EdgeCount:     len(result.Edges),
		Version:       result.Version,
		SchemaVersion: schemaVersion,
		Compressed:    result.Version == backup.FormatV2,
		SizeBytes:     sizeBytes,
		Metadata:      metadata,
		Message:       fmt.Sprintf("Backup created: %d nodes, %d edges → %s", len(result.Nodes), len(result.Edges), outputPath),
	}, nil
}

// handleFloopRestore implements the floop_restore tool.
func (s *Server) handleFloopRestore(ctx context.Context, req *sdk.CallToolRequest, args FloopRestoreInput) (_ *sdk.CallToolResult, _ FloopRestoreOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_restore", start, retErr, sanitizeToolParams("floop_restore", map[string]interface{}{
			"input_path": args.InputPath, "mode": args.Mode,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_restore"); err != nil {
		return nil, FloopRestoreOutput{}, err
	}

	if args.InputPath == "" {
		return nil, FloopRestoreOutput{}, fmt.Errorf("'input_path' parameter is required")
	}

	// Validate user-supplied path against allowed directories
	allowedDirs, err := pathutil.DefaultAllowedBackupDirsWithProjectRoot(s.root)
	if err != nil {
		return nil, FloopRestoreOutput{}, fmt.Errorf("failed to determine allowed backup dirs: %w", err)
	}
	if err := pathutil.ValidatePath(args.InputPath, allowedDirs); err != nil {
		return nil, FloopRestoreOutput{}, fmt.Errorf("restore path rejected: %w", err)
	}

	mode := backup.RestoreMerge
	if args.Mode == "replace" {
		mode = backup.RestoreReplace
	}

	result, err := backup.Restore(ctx, s.store, args.InputPath, mode)
	if err != nil {
		return nil, FloopRestoreOutput{}, fmt.Errorf("restore failed: %w", err)
	}

	// Debounced PageRank refresh after restore
	s.debouncedRefreshPageRank()

	return nil, FloopRestoreOutput{
		NodesRestored: result.NodesRestored,
		NodesSkipped:  result.NodesSkipped,
		EdgesRestored: result.EdgesRestored,
		EdgesSkipped:  result.EdgesSkipped,
		Message:       fmt.Sprintf("Restore complete: %d nodes restored, %d skipped; %d edges restored, %d skipped", result.NodesRestored, result.NodesSkipped, result.EdgesRestored, result.EdgesSkipped),
	}, nil
}
