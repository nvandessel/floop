// Package backup provides backup and restore functionality for the floop behavior graph.
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nvandessel/feedback-loop/internal/store"
)

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

// Backup exports all nodes and edges from the store to a JSON file.
func Backup(ctx context.Context, graphStore store.GraphStore, outputPath string) (*BackupFormat, error) {
	// Get all nodes (empty predicate = all)
	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("failed to query nodes: %w", err)
	}

	// Collect all edges by iterating each node's outbound edges
	edgeSet := make(map[string]store.Edge) // key = source:target:kind
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

	// Convert edge map to slice
	edges := make([]store.Edge, 0, len(edgeSet))
	for _, e := range edgeSet {
		edges = append(edges, e)
	}

	// Build backup
	backup := &BackupFormat{
		Version:   1,
		CreatedAt: time.Now(),
		Nodes:     make([]BackupNode, len(nodes)),
		Edges:     edges,
	}
	for i, n := range nodes {
		backup.Nodes[i] = BackupNode{Node: n}
	}

	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Write to file
	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(backup); err != nil {
		return nil, fmt.Errorf("failed to encode backup: %w", err)
	}

	return backup, nil
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
func Restore(ctx context.Context, graphStore store.GraphStore, inputPath string, mode RestoreMode) (*RestoreResult, error) {
	// Read backup file
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open backup file: %w", err)
	}
	defer f.Close()

	var backup BackupFormat
	if err := json.NewDecoder(f).Decode(&backup); err != nil {
		return nil, fmt.Errorf("failed to decode backup: %w", err)
	}

	if backup.Version != 1 {
		return nil, fmt.Errorf("unsupported backup version: %d", backup.Version)
	}

	result := &RestoreResult{}

	// Restore nodes
	for _, bn := range backup.Nodes {
		if mode == RestoreMerge {
			// Check if node already exists
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
			// Skip duplicates silently in merge mode
			if mode == RestoreMerge {
				result.NodesSkipped++
				continue
			}
			return nil, fmt.Errorf("failed to restore node %s: %w", bn.ID, err)
		}
		result.NodesRestored++
	}

	// Restore edges
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

	// Sync
	if err := graphStore.Sync(ctx); err != nil {
		return nil, fmt.Errorf("failed to sync after restore: %w", err)
	}

	return result, nil
}

// GenerateBackupPath creates a timestamped backup filename in the given directory.
func GenerateBackupPath(dir string) string {
	ts := time.Now().Format("20060102-150405")
	return filepath.Join(dir, fmt.Sprintf("floop-backup-%s.json", ts))
}

// RotateBackups keeps only the most recent N backups, deleting older ones.
func RotateBackups(dir string, keepN int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Filter for backup files and sort by name (which includes timestamp)
	var backups []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			backups = append(backups, e)
		}
	}

	// Sort by name descending (newest first since timestamp is in the name)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Name() > backups[j].Name()
	})

	// Delete older backups
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
