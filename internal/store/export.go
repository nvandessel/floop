// Package store provides graph storage implementations.
package store

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// ImportNodesFromJSONL imports nodes from a JSONL file into the SQLite database.
func (s *SQLiteGraphStore) ImportNodesFromJSONL(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file is fine
		}
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // 1MB max line length

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var node Node
		if err := json.Unmarshal([]byte(line), &node); err != nil {
			// Log but continue on parse errors
			fmt.Fprintf(os.Stderr, "warning: failed to parse line %d: %v\n", lineNum, err)
			continue
		}

		// Add the node (uses INSERT OR REPLACE)
		if node.Kind == "behavior" || node.Kind == "forgotten-behavior" {
			if _, err := s.addBehavior(ctx, node); err != nil {
				return fmt.Errorf("failed to import node %s: %w", node.ID, err)
			}
		} else {
			if _, err := s.addGenericNode(ctx, node); err != nil {
				return fmt.Errorf("failed to import node %s: %w", node.ID, err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil
}

// ImportEdgesFromJSONL imports edges from a JSONL file into the SQLite database.
func (s *SQLiteGraphStore) ImportEdgesFromJSONL(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file is fine
		}
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var edge Edge
		if err := json.Unmarshal([]byte(line), &edge); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to parse edge at line %d: %v\n", lineNum, err)
			continue
		}

		var metadataJSON []byte
		if edge.Metadata != nil {
			metadataJSON, _ = json.Marshal(edge.Metadata)
		}

		_, err := s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO edges (source, target, kind, metadata)
			VALUES (?, ?, ?, ?)
		`, edge.Source, edge.Target, edge.Kind, nullBytes(metadataJSON))
		if err != nil {
			return fmt.Errorf("failed to import edge: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil
}

// GetDirtyBehaviorIDs returns the IDs of behaviors that have been modified since last export.
func (s *SQLiteGraphStore) GetDirtyBehaviorIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT behavior_id FROM dirty_behaviors`)
	if err != nil {
		return nil, fmt.Errorf("failed to query dirty behaviors: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan ID: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// IsDirty returns true if there are any unsaved changes.
func (s *SQLiteGraphStore) IsDirty(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dirty_behaviors`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to count dirty behaviors: %w", err)
	}
	return count > 0, nil
}
