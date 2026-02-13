// Package backup provides backup and restore functionality for the floop behavior graph.
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/pathutil"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// MaxRestoreFileSize is the maximum size of a backup file that can be restored (50MB).
const MaxRestoreFileSize = 50 * 1024 * 1024

// BackupFormat is the JSON structure for a full backup file.
type BackupFormat struct {
	Version   int          `json:"version"`
	CreatedAt time.Time    `json:"created_at"`
	Nodes     []BackupNode `json:"nodes"`
	Edges     []store.Edge `json:"edges"`
}

// BackupNode wraps a store.Node for backup serialization.
type BackupNode struct {
	store.Node
}

// DefaultBackupDir returns the default backup directory (~/.floop/backups/).
func DefaultBackupDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".floop", "backups"), nil
}

// collectGraph gathers all nodes and edges from the store into a BackupFormat.
func collectGraph(ctx context.Context, graphStore store.GraphStore) (*BackupFormat, error) {
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("failed to query nodes: %w", err)
	}

	edgeSet := make(map[string]store.Edge)
	for _, node := range nodes {
		edges, err := graphStore.GetEdges(ctx, node.ID, store.DirectionOutbound, "")
		if err != nil {
			return nil, fmt.Errorf("failed to get edges for %s: %w", node.ID, err)
		}
		for _, e := range edges {
			key := fmt.Sprintf("%s:%s:%s", e.Source, e.Target, e.Kind)
			edgeSet[key] = e
		}
	}

	edges := make([]store.Edge, 0, len(edgeSet))
	for _, e := range edgeSet {
		edges = append(edges, e)
	}

	bf := &BackupFormat{
		Version:   FormatV2,
		CreatedAt: time.Now(),
		Nodes:     make([]BackupNode, len(nodes)),
		Edges:     edges,
	}
	for i, n := range nodes {
		bf.Nodes[i] = BackupNode{Node: n}
	}

	return bf, nil
}

// Backup exports all nodes and edges from the store to a V2 compressed backup file.
// If allowedDirs is non-empty, the outputPath is validated against them.
// Pass nil to skip validation (for internal/default paths only).
func Backup(ctx context.Context, graphStore store.GraphStore, outputPath string, allowedDirs ...string) (*BackupFormat, error) {
	return BackupWithOptions(ctx, graphStore, outputPath, true, allowedDirs...)
}

// BackupWithOptions exports all nodes and edges with explicit compression control.
// When compress is true, writes V2 format (gzip + SHA-256). When false, writes V1 (plain JSON).
func BackupWithOptions(ctx context.Context, graphStore store.GraphStore, outputPath string, compress bool, allowedDirs ...string) (*BackupFormat, error) {
	if len(allowedDirs) > 0 {
		if err := pathutil.ValidatePath(outputPath, allowedDirs); err != nil {
			return nil, fmt.Errorf("backup path rejected: %w", err)
		}
	}

	bf, err := collectGraph(ctx, graphStore)
	if err != nil {
		return nil, err
	}

	if compress {
		if err := WriteV2(outputPath, bf); err != nil {
			return nil, fmt.Errorf("failed to write V2 backup: %w", err)
		}
	} else {
		bf.Version = FormatV1
		if err := writeV1(outputPath, bf); err != nil {
			return nil, fmt.Errorf("failed to write V1 backup: %w", err)
		}
	}

	return bf, nil
}

// writeV1 writes a BackupFormat as plain indented JSON (legacy V1 format).
func writeV1(outputPath string, bf *BackupFormat) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bf); err != nil {
		return fmt.Errorf("failed to encode backup: %w", err)
	}

	return nil
}

// RestoreMode controls how restore handles existing data.
type RestoreMode string

const (
	// RestoreMerge skips nodes/edges that already exist (default).
	RestoreMerge RestoreMode = "merge"
	// RestoreReplace clears the store before restoring.
	RestoreReplace RestoreMode = "replace"
)

// RestoreResult contains statistics about the restore operation.
type RestoreResult struct {
	NodesRestored int `json:"nodes_restored"`
	NodesSkipped  int `json:"nodes_skipped"`
	EdgesRestored int `json:"edges_restored"`
	EdgesSkipped  int `json:"edges_skipped"`
}

// Restore imports nodes and edges from a backup file into the store.
// Automatically detects V1 and V2 format.
// If allowedDirs is non-empty, the inputPath is validated against them.
// Pass nil to skip validation (for internal/default paths only).
func Restore(ctx context.Context, graphStore store.GraphStore, inputPath string, mode RestoreMode, allowedDirs ...string) (*RestoreResult, error) {
	if len(allowedDirs) > 0 {
		if err := pathutil.ValidatePath(inputPath, allowedDirs); err != nil {
			return nil, fmt.Errorf("restore path rejected: %w", err)
		}
	}

	backup, err := readBackupAuto(inputPath)
	if err != nil {
		return nil, err
	}

	return restoreFromBackup(ctx, graphStore, backup, mode)
}

// readBackupAuto detects the format and reads the backup file.
func readBackupAuto(inputPath string) (*BackupFormat, error) {
	version, err := DetectFormat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to detect backup format: %w", err)
	}

	switch version {
	case FormatV2:
		return ReadV2(inputPath)
	case FormatV1:
		return readV1(inputPath)
	default:
		return nil, fmt.Errorf("unsupported backup format version: %d", version)
	}
}

// readV1 reads a legacy V1 plain JSON backup file.
func readV1(inputPath string) (*BackupFormat, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open backup file: %w", err)
	}
	defer f.Close()

	limitedReader := io.LimitReader(f, MaxRestoreFileSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup file: %w", err)
	}
	if int64(len(data)) > MaxRestoreFileSize {
		return nil, fmt.Errorf("backup file exceeds maximum size of %d bytes", MaxRestoreFileSize)
	}

	var backup BackupFormat
	if err := json.Unmarshal(data, &backup); err != nil {
		return nil, fmt.Errorf("failed to decode backup (file may be corrupted): %w", err)
	}

	if backup.Version != FormatV1 {
		return nil, fmt.Errorf("expected V1 backup, got version %d", backup.Version)
	}

	return &backup, nil
}

// restoreFromBackup applies a parsed BackupFormat to the store.
func restoreFromBackup(ctx context.Context, graphStore store.GraphStore, backup *BackupFormat, mode RestoreMode) (*RestoreResult, error) {
	result := &RestoreResult{}

	for _, bn := range backup.Nodes {
		if mode == RestoreMerge {
			existing, err := graphStore.GetNode(ctx, bn.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to check existing node %s: %w", bn.ID, err)
			}
			if existing != nil {
				result.NodesSkipped++
				continue
			}
		}

		if _, err := graphStore.AddNode(ctx, bn.Node); err != nil {
			if mode == RestoreMerge {
				result.NodesSkipped++
				continue
			}
			return nil, fmt.Errorf("failed to restore node %s: %w", bn.ID, err)
		}
		result.NodesRestored++
	}

	for _, edge := range backup.Edges {
		if err := graphStore.AddEdge(ctx, edge); err != nil {
			if mode == RestoreMerge {
				result.EdgesSkipped++
				continue
			}
			return nil, fmt.Errorf("failed to restore edge %s->%s: %w", edge.Source, edge.Target, err)
		}
		result.EdgesRestored++
	}

	if err := graphStore.Sync(ctx); err != nil {
		return nil, fmt.Errorf("failed to sync after restore: %w", err)
	}

	return result, nil
}

// GenerateBackupPath creates a timestamped backup filename in the given directory.
// Uses .json.gz extension for V2 compressed backups.
func GenerateBackupPath(dir string) string {
	ts := time.Now().Format("20060102-150405")
	return filepath.Join(dir, fmt.Sprintf("floop-backup-%s.json.gz", ts))
}

// GenerateBackupPathV1 creates a timestamped backup filename with .json extension (V1 format).
func GenerateBackupPathV1(dir string) string {
	ts := time.Now().Format("20060102-150405")
	return filepath.Join(dir, fmt.Sprintf("floop-backup-%s.json", ts))
}

// isBackupFile returns true if the filename matches the floop backup naming pattern.
func isBackupFile(name string) bool {
	return strings.HasPrefix(name, "floop-backup-") &&
		(strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".json.gz"))
}

// RotateBackups keeps only the most recent N backups, deleting older ones.
// Matches both .json (V1) and .json.gz (V2) backup files.
func RotateBackups(dir string, keepN int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && isBackupFile(e.Name()) {
			backups = append(backups, e)
		}
	}

	// Sort by name descending (newest first since timestamp is in the name)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Name() > backups[j].Name()
	})

	if len(backups) > keepN {
		for _, b := range backups[keepN:] {
			path := filepath.Join(dir, b.Name())
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("failed to remove old backup %s: %w", b.Name(), err)
			}
		}
	}

	return nil
}
