// Package store provides graph storage implementations.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// SQLiteGraphStore implements GraphStore using SQLite for persistence.
// It stores nodes and edges in a SQLite database and exports to JSONL on Sync().
type SQLiteGraphStore struct {
	mu        sync.RWMutex
	db        *sql.DB
	floopDir  string
	dbPath    string
	nodesFile string
	edgesFile string
}

// NewSQLiteGraphStore creates a new SQLiteGraphStore rooted at projectRoot.
// It creates the database at .floop/floop.db and auto-imports existing JSONL files.
func NewSQLiteGraphStore(projectRoot string) (*SQLiteGraphStore, error) {
	floopDir := filepath.Join(projectRoot, ".floop")

	// Ensure .floop directory exists
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .floop directory: %w", err)
	}

	dbPath := filepath.Join(floopDir, "floop.db")
	nodesFile := filepath.Join(floopDir, "nodes.jsonl")
	edgesFile := filepath.Join(floopDir, "edges.jsonl")

	// Open database
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite works best with single writer

	ctx := context.Background()

	// Initialize schema
	if err := InitSchema(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	s := &SQLiteGraphStore{
		db:        db,
		floopDir:  floopDir,
		dbPath:    dbPath,
		nodesFile: nodesFile,
		edgesFile: edgesFile,
	}

	// Auto-import existing JSONL if database is empty or JSONL is newer
	if err := s.autoImport(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to auto-import JSONL: %w", err)
	}

	return s, nil
}

// autoImport imports existing JSONL files if the database is empty or JSONL is newer.
func (s *SQLiteGraphStore) autoImport(ctx context.Context) error {
	// Check if database has any behaviors
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM behaviors`).Scan(&count); err != nil {
		return fmt.Errorf("failed to count behaviors: %w", err)
	}

	// If database already has data, check if we need to import newer JSONL
	if count > 0 {
		// Get DB modification time
		dbInfo, err := os.Stat(s.dbPath)
		if err != nil {
			return fmt.Errorf("failed to stat database: %w", err)
		}

		// Check if nodes.jsonl is newer
		nodesInfo, err := os.Stat(s.nodesFile)
		if err != nil {
			if os.IsNotExist(err) {
				return nil // No JSONL file, nothing to import
			}
			return fmt.Errorf("failed to stat nodes.jsonl: %w", err)
		}

		// If JSONL is older than DB, no need to import
		if nodesInfo.ModTime().Before(dbInfo.ModTime()) {
			return nil
		}
	}

	// Import nodes.jsonl if it exists
	if _, err := os.Stat(s.nodesFile); err == nil {
		if err := s.ImportNodesFromJSONL(ctx, s.nodesFile); err != nil {
			return fmt.Errorf("failed to import nodes: %w", err)
		}
	}

	// Import edges.jsonl if it exists
	if _, err := os.Stat(s.edgesFile); err == nil {
		if err := s.ImportEdgesFromJSONL(ctx, s.edgesFile); err != nil {
			return fmt.Errorf("failed to import edges: %w", err)
		}
	}

	// Clear dirty flags since we just imported
	if _, err := s.db.ExecContext(ctx, `DELETE FROM dirty_behaviors`); err != nil {
		return fmt.Errorf("failed to clear dirty flags: %w", err)
	}

	return nil
}

// AddNode adds a node to the store.
func (s *SQLiteGraphStore) AddNode(ctx context.Context, node Node) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if node.ID == "" {
		return "", fmt.Errorf("node ID is required")
	}

	// Use addBehavior for all behavior-related kinds
	if isBehaviorKind(node.Kind) {
		return s.addBehavior(ctx, node)
	}

	// For non-behavior nodes, store in a generic way using the behaviors table
	// with a different kind indicator
	return s.addGenericNode(ctx, node)
}

// isBehaviorKind returns true if the kind represents a behavior (active or curated).
func isBehaviorKind(kind string) bool {
	switch kind {
	case "behavior", "forgotten-behavior", "deprecated-behavior", "merged-behavior":
		return true
	default:
		return false
	}
}

// addBehavior adds a behavior node to the behaviors table.
func (s *SQLiteGraphStore) addBehavior(ctx context.Context, node Node) (string, error) {
	// Extract fields from content map
	content := node.Content
	metadata := node.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	// Get content fields - handle both map and struct types
	var behaviorContent map[string]interface{}
	if bc, ok := content["content"].(map[string]interface{}); ok {
		behaviorContent = bc
	} else if content["content"] != nil {
		// Convert struct to map via JSON round-trip
		b, err := json.Marshal(content["content"])
		if err != nil {
			return "", fmt.Errorf("marshal behavior content: %w", err)
		}
		if err := json.Unmarshal(b, &behaviorContent); err != nil {
			return "", fmt.Errorf("unmarshal behavior content: %w", err)
		}
	}
	if behaviorContent == nil {
		behaviorContent = make(map[string]interface{})
	}

	canonical, _ := behaviorContent["canonical"].(string)
	expanded, _ := behaviorContent["expanded"].(string)
	summary, _ := behaviorContent["summary"].(string)
	structuredRaw, _ := behaviorContent["structured"]
	tagsRaw, _ := behaviorContent["tags"]

	var structuredJSON, tagsJSON []byte
	var err error
	if structuredRaw != nil {
		structuredJSON, err = json.Marshal(structuredRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal structured: %w", err)
		}
	}
	if tagsRaw != nil {
		tagsJSON, err = json.Marshal(tagsRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal tags: %w", err)
		}
	}

	// Get other fields
	name, _ := content["name"].(string)
	// behaviorType is the specific type (directive, constraint, etc.)
	// stored in content["kind"], used for reconstruction
	behaviorType, _ := content["kind"].(string)
	// kind is the node kind - can be "behavior", "forgotten-behavior", "merged-behavior", etc.
	// Use the original node.Kind to preserve special states
	kind := node.Kind

	// Provenance - handle both map and struct types
	var provenance map[string]interface{}
	if p, ok := content["provenance"].(map[string]interface{}); ok {
		provenance = p
	} else if content["provenance"] != nil {
		// Convert struct to map via JSON round-trip
		b, err := json.Marshal(content["provenance"])
		if err != nil {
			return "", fmt.Errorf("marshal provenance: %w", err)
		}
		if err := json.Unmarshal(b, &provenance); err != nil {
			return "", fmt.Errorf("unmarshal provenance: %w", err)
		}
	}
	if provenance == nil {
		provenance = make(map[string]interface{})
	}
	sourceType, _ := provenance["source_type"].(string)
	correctionID, _ := provenance["correction_id"].(string)
	createdAtStr, _ := provenance["created_at"].(string)

	// Relationships
	requiresRaw, _ := content["requires"]
	overridesRaw, _ := content["overrides"]
	conflictsRaw, _ := content["conflicts"]

	var requiresJSON, overridesJSON, conflictsJSON []byte
	if requiresRaw != nil {
		var err error
		requiresJSON, err = json.Marshal(requiresRaw)
		if err != nil {
			return "", fmt.Errorf("marshal requires: %w", err)
		}
	}
	if overridesRaw != nil {
		var err error
		overridesJSON, err = json.Marshal(overridesRaw)
		if err != nil {
			return "", fmt.Errorf("marshal overrides: %w", err)
		}
	}
	if conflictsRaw != nil {
		var err error
		conflictsJSON, err = json.Marshal(conflictsRaw)
		if err != nil {
			return "", fmt.Errorf("marshal conflicts: %w", err)
		}
	}

	// Metadata
	confidence, _ := metadata["confidence"].(float64)
	if confidence == 0 {
		confidence = 0.6
	}
	priority, _ := metadata["priority"].(float64)
	scope, _ := metadata["scope"].(string)
	if scope == "" {
		scope = "local"
	}

	// Collect extra metadata fields (not confidence, priority, scope, stats)
	knownMetadataFields := map[string]bool{
		"confidence": true,
		"priority":   true,
		"scope":      true,
		"stats":      true,
	}
	extraMetadata := make(map[string]interface{})
	for k, v := range metadata {
		if !knownMetadataFields[k] {
			extraMetadata[k] = v
		}
	}
	var extraMetadataJSON []byte
	if len(extraMetadata) > 0 {
		var err error
		extraMetadataJSON, err = json.Marshal(extraMetadata)
		if err != nil {
			return "", fmt.Errorf("marshal extra metadata: %w", err)
		}
	}

	// Compute content hash for deduplication
	contentHash := computeContentHash(canonical)

	now := time.Now().Format(time.RFC3339)

	// Insert behavior
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO behaviors (
			id, name, kind, behavior_type,
			content_canonical, content_expanded, content_summary, content_structured, content_tags,
			provenance_source_type, provenance_correction_id, provenance_created_at,
			requires, overrides, conflicts,
			confidence, priority, scope, metadata_extra,
			created_at, updated_at, content_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, node.ID, name, kind, behaviorType,
		canonical, nullString(expanded), nullString(summary), nullBytes(structuredJSON), nullBytes(tagsJSON),
		nullString(sourceType), nullString(correctionID), nullString(createdAtStr),
		nullBytes(requiresJSON), nullBytes(overridesJSON), nullBytes(conflictsJSON),
		confidence, int(priority), scope, nullBytes(extraMetadataJSON),
		now, now, contentHash)
	if err != nil {
		return "", fmt.Errorf("failed to insert behavior: %w", err)
	}

	// Insert when conditions
	when, _ := content["when"].(map[string]interface{})
	for field, value := range when {
		valueStr, valueType, err := serializeWhenValue(value)
		if err != nil {
			return "", fmt.Errorf("serialize when condition %s: %w", field, err)
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO behavior_when (behavior_id, field, value, value_type)
			VALUES (?, ?, ?, ?)
		`, node.ID, field, valueStr, valueType)
		if err != nil {
			return "", fmt.Errorf("failed to insert when condition: %w", err)
		}
	}

	// Insert stats - handle both map and struct types
	var stats map[string]interface{}
	if s, ok := metadata["stats"].(map[string]interface{}); ok {
		stats = s
	} else if metadata["stats"] != nil {
		// Convert struct to map via JSON round-trip
		b, err := json.Marshal(metadata["stats"])
		if err != nil {
			return "", fmt.Errorf("marshal stats: %w", err)
		}
		if err := json.Unmarshal(b, &stats); err != nil {
			return "", fmt.Errorf("unmarshal stats: %w", err)
		}
	}
	if stats == nil {
		stats = make(map[string]interface{})
	}
	timesActivated, _ := stats["times_activated"].(float64)
	timesFollowed, _ := stats["times_followed"].(float64)
	timesOverridden, _ := stats["times_overridden"].(float64)
	timesConfirmed, _ := stats["times_confirmed"].(float64)
	lastActivated, _ := stats["last_activated"].(string)
	lastConfirmed, _ := stats["last_confirmed"].(string)

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO behavior_stats (
			behavior_id, times_activated, times_followed, times_overridden, times_confirmed,
			last_activated, last_confirmed
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, node.ID, int(timesActivated), int(timesFollowed), int(timesOverridden), int(timesConfirmed),
		nullString(lastActivated), nullString(lastConfirmed))
	if err != nil {
		return "", fmt.Errorf("failed to insert stats: %w", err)
	}

	return node.ID, nil
}

// addGenericNode adds a non-behavior node to the behaviors table.
func (s *SQLiteGraphStore) addGenericNode(ctx context.Context, node Node) (string, error) {
	contentJSON, err := json.Marshal(node.Content)
	if err != nil {
		return "", fmt.Errorf("failed to marshal content: %w", err)
	}

	now := time.Now().Format(time.RFC3339)

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO behaviors (
			id, name, kind,
			content_canonical, content_structured,
			confidence, priority, scope,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, node.ID, node.ID, node.Kind,
		"", contentJSON,
		0.6, 0, "local",
		now, now)
	if err != nil {
		return "", fmt.Errorf("failed to insert generic node: %w", err)
	}

	return node.ID, nil
}

// UpdateNode updates an existing node in the store.
func (s *SQLiteGraphStore) UpdateNode(ctx context.Context, node Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if node exists
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM behaviors WHERE id = ?`, node.ID).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("node not found: %s", node.ID)
	}
	if err != nil {
		return fmt.Errorf("failed to check node existence: %w", err)
	}

	// Delete existing when conditions (they'll be re-inserted)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM behavior_when WHERE behavior_id = ?`, node.ID); err != nil {
		return fmt.Errorf("failed to delete when conditions: %w", err)
	}

	// Re-add the node (addBehavior uses INSERT OR REPLACE)
	if isBehaviorKind(node.Kind) {
		_, err = s.addBehavior(ctx, node)
	} else {
		_, err = s.addGenericNode(ctx, node)
	}

	return err
}

// GetNode retrieves a node by ID. Returns nil if not found.
func (s *SQLiteGraphStore) GetNode(ctx context.Context, id string) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getNodeUnlocked(ctx, id)
}

// getNodeUnlocked retrieves a node without locking (caller must hold lock).
func (s *SQLiteGraphStore) getNodeUnlocked(ctx context.Context, id string) (*Node, error) {
	var (
		name, kind                                    string
		behaviorType                                  sql.NullString
		canonical, expanded, summary                  sql.NullString
		structuredJSON, tagsJSON                      sql.NullString
		sourceType, correctionID, provenanceCreatedAt sql.NullString
		requiresJSON, overridesJSON, conflictsJSON    sql.NullString
		confidence                                    float64
		priority                                      int
		scope                                         sql.NullString
		metadataExtraJSON                             sql.NullString
		createdAt, updatedAt                          string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT
			name, kind, behavior_type,
			content_canonical, content_expanded, content_summary, content_structured, content_tags,
			provenance_source_type, provenance_correction_id, provenance_created_at,
			requires, overrides, conflicts,
			confidence, priority, scope, metadata_extra,
			created_at, updated_at
		FROM behaviors WHERE id = ?
	`, id).Scan(
		&name, &kind, &behaviorType,
		&canonical, &expanded, &summary, &structuredJSON, &tagsJSON,
		&sourceType, &correctionID, &provenanceCreatedAt,
		&requiresJSON, &overridesJSON, &conflictsJSON,
		&confidence, &priority, &scope, &metadataExtraJSON,
		&createdAt, &updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Query when conditions
	whenRows, err := s.db.QueryContext(ctx, `
		SELECT field, value, value_type FROM behavior_when WHERE behavior_id = ?
	`, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query when conditions: %w", err)
	}
	defer whenRows.Close()

	when := make(map[string]interface{})
	for whenRows.Next() {
		var field, value, valueType string
		if err := whenRows.Scan(&field, &value, &valueType); err != nil {
			return nil, fmt.Errorf("failed to scan when condition: %w", err)
		}
		deserializedValue, err := deserializeWhenValue(value, valueType)
		if err != nil {
			return nil, fmt.Errorf("deserialize when condition %s for %s: %w", field, id, err)
		}
		when[field] = deserializedValue
	}

	// Query stats
	var timesActivated, timesFollowed, timesOverridden, timesConfirmed int
	var lastActivated, lastConfirmed sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT times_activated, times_followed, times_overridden, times_confirmed,
		       last_activated, last_confirmed
		FROM behavior_stats WHERE behavior_id = ?
	`, id).Scan(&timesActivated, &timesFollowed, &timesOverridden, &timesConfirmed,
		&lastActivated, &lastConfirmed)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	// Build content map
	content := make(map[string]interface{})
	content["name"] = name
	// Use behavior_type for content["kind"] (directive, constraint, etc.)
	if behaviorType.Valid {
		content["kind"] = behaviorType.String
	}

	behaviorContent := make(map[string]interface{})
	behaviorContent["canonical"] = canonical.String
	if expanded.Valid {
		behaviorContent["expanded"] = expanded.String
	}
	if summary.Valid {
		behaviorContent["summary"] = summary.String
	}
	if structuredJSON.Valid {
		var structured interface{}
		if err := json.Unmarshal([]byte(structuredJSON.String), &structured); err != nil {
			return nil, fmt.Errorf("unmarshal structured content for %s: %w", id, err)
		}
		behaviorContent["structured"] = structured
	}
	if tagsJSON.Valid {
		var tags interface{}
		if err := json.Unmarshal([]byte(tagsJSON.String), &tags); err != nil {
			return nil, fmt.Errorf("unmarshal tags for %s: %w", id, err)
		}
		behaviorContent["tags"] = tags
	}
	content["content"] = behaviorContent

	// Provenance
	provenance := make(map[string]interface{})
	if sourceType.Valid {
		provenance["source_type"] = sourceType.String
	}
	if correctionID.Valid {
		provenance["correction_id"] = correctionID.String
	}
	if provenanceCreatedAt.Valid {
		provenance["created_at"] = provenanceCreatedAt.String
	}
	content["provenance"] = provenance

	// Relationships
	if requiresJSON.Valid {
		var requires interface{}
		if err := json.Unmarshal([]byte(requiresJSON.String), &requires); err != nil {
			return nil, fmt.Errorf("unmarshal requires for %s: %w", id, err)
		}
		content["requires"] = requires
	}
	if overridesJSON.Valid {
		var overrides interface{}
		if err := json.Unmarshal([]byte(overridesJSON.String), &overrides); err != nil {
			return nil, fmt.Errorf("unmarshal overrides for %s: %w", id, err)
		}
		content["overrides"] = overrides
	}
	if conflictsJSON.Valid {
		var conflicts interface{}
		if err := json.Unmarshal([]byte(conflictsJSON.String), &conflicts); err != nil {
			return nil, fmt.Errorf("unmarshal conflicts for %s: %w", id, err)
		}
		content["conflicts"] = conflicts
	}

	// When conditions
	if len(when) > 0 {
		content["when"] = when
	}

	// Build metadata map
	metadata := make(map[string]interface{})
	metadata["confidence"] = confidence
	metadata["priority"] = priority
	if scope.Valid {
		metadata["scope"] = scope.String
	}

	// Stats
	stats := map[string]interface{}{
		"times_activated":  timesActivated,
		"times_followed":   timesFollowed,
		"times_overridden": timesOverridden,
		"times_confirmed":  timesConfirmed,
		"created_at":       createdAt,
		"updated_at":       updatedAt,
	}
	if lastActivated.Valid {
		stats["last_activated"] = lastActivated.String
	}
	if lastConfirmed.Valid {
		stats["last_confirmed"] = lastConfirmed.String
	}
	metadata["stats"] = stats

	// Merge extra metadata fields (forget_reason, deprecation_reason, merged_into, etc.)
	if metadataExtraJSON.Valid {
		var extraMetadata map[string]interface{}
		if err := json.Unmarshal([]byte(metadataExtraJSON.String), &extraMetadata); err == nil {
			for k, v := range extraMetadata {
				metadata[k] = v
			}
		}
	}

	// Return the actual node kind from the database
	// (can be "behavior", "forgotten-behavior", "merged-behavior", "correction", etc.)
	return &Node{
		ID:       id,
		Kind:     kind,
		Content:  content,
		Metadata: metadata,
	}, nil
}

// DeleteNode removes a node and its associated edges.
func (s *SQLiteGraphStore) DeleteNode(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete the behavior (cascades to when and stats via foreign keys)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM behaviors WHERE id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete behavior: %w", err)
	}

	// Delete edges involving this node
	if _, err := s.db.ExecContext(ctx, `DELETE FROM edges WHERE source = ? OR target = ?`, id, id); err != nil {
		return fmt.Errorf("failed to delete edges: %w", err)
	}

	return nil
}

// QueryNodes returns nodes matching the predicate.
func (s *SQLiteGraphStore) QueryNodes(ctx context.Context, predicate map[string]interface{}) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build WHERE clause from predicate
	var whereClauses []string
	var args []interface{}

	for key, value := range predicate {
		switch key {
		case "kind":
			whereClauses = append(whereClauses, "kind = ?")
			args = append(args, value)
		case "id":
			whereClauses = append(whereClauses, "id = ?")
			args = append(args, value)
		case "scope":
			whereClauses = append(whereClauses, "scope = ?")
			args = append(args, value)
		}
	}

	query := `SELECT id FROM behaviors`
	if len(whereClauses) > 0 {
		query += " WHERE " + joinStrings(whereClauses, " AND ")
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query nodes: %w", err)
	}

	// Collect IDs first, then close rows before nested queries
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan node ID: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	// Now fetch each node
	var nodes []Node
	for _, id := range ids {
		node, err := s.getNodeUnlocked(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get node %s: %w", id, err)
		}
		if node != nil {
			nodes = append(nodes, *node)
		}
	}

	return nodes, nil
}

// AddEdge adds an edge to the store.
func (s *SQLiteGraphStore) AddEdge(ctx context.Context, edge Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var metadataJSON []byte
	var err error
	if edge.Metadata != nil {
		metadataJSON, err = json.Marshal(edge.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO edges (source, target, kind, metadata)
		VALUES (?, ?, ?, ?)
	`, edge.Source, edge.Target, edge.Kind, nullBytes(metadataJSON))
	if err != nil {
		return fmt.Errorf("failed to add edge: %w", err)
	}

	return nil
}

// RemoveEdge removes an edge matching source, target, and kind.
func (s *SQLiteGraphStore) RemoveEdge(ctx context.Context, source, target, kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM edges WHERE source = ? AND target = ? AND kind = ?
	`, source, target, kind)
	if err != nil {
		return fmt.Errorf("failed to remove edge: %w", err)
	}

	return nil
}

// GetEdges returns edges connected to a node.
func (s *SQLiteGraphStore) GetEdges(ctx context.Context, nodeID string, direction Direction, kind string) ([]Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var query string
	var args []interface{}

	switch direction {
	case DirectionOutbound:
		query = `SELECT source, target, kind, metadata FROM edges WHERE source = ?`
		args = append(args, nodeID)
	case DirectionInbound:
		query = `SELECT source, target, kind, metadata FROM edges WHERE target = ?`
		args = append(args, nodeID)
	case DirectionBoth:
		query = `SELECT source, target, kind, metadata FROM edges WHERE source = ? OR target = ?`
		args = append(args, nodeID, nodeID)
	}

	if kind != "" {
		query += " AND kind = ?"
		args = append(args, kind)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query edges: %w", err)
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var source, target, edgeKind string
		var metadataJSON sql.NullString

		if err := rows.Scan(&source, &target, &edgeKind, &metadataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan edge: %w", err)
		}

		edge := Edge{
			Source: source,
			Target: target,
			Kind:   edgeKind,
		}

		if metadataJSON.Valid {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err == nil {
				edge.Metadata = metadata
			}
		}

		edges = append(edges, edge)
	}

	return edges, nil
}

// Traverse returns all nodes reachable from start by following edges of the given kinds.
func (s *SQLiteGraphStore) Traverse(ctx context.Context, start string, edgeKinds []string, direction Direction, maxDepth int) ([]Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	visited := make(map[string]bool)
	var results []Node

	s.traverseRecursive(ctx, start, edgeKinds, direction, maxDepth, 0, visited, &results)

	return results, nil
}

func (s *SQLiteGraphStore) traverseRecursive(ctx context.Context, current string, edgeKinds []string, direction Direction, maxDepth, depth int, visited map[string]bool, results *[]Node) {
	if depth > maxDepth || visited[current] {
		return
	}
	visited[current] = true

	node, err := s.getNodeUnlocked(ctx, current)
	if err == nil && node != nil {
		*results = append(*results, *node)
	}

	// Get edges
	edges, err := s.GetEdges(ctx, current, direction, "")
	if err != nil {
		return
	}

	for _, e := range edges {
		if !edgeKindMatches(e.Kind, edgeKinds) {
			continue
		}

		var next string
		switch direction {
		case DirectionOutbound:
			if e.Source == current {
				next = e.Target
			}
		case DirectionInbound:
			if e.Target == current {
				next = e.Source
			}
		case DirectionBoth:
			if e.Source == current {
				next = e.Target
			} else if e.Target == current {
				next = e.Source
			}
		}

		if next != "" {
			s.traverseRecursive(ctx, next, edgeKinds, direction, maxDepth, depth+1, visited, results)
		}
	}
}

// Sync exports dirty behaviors to JSONL files.
func (s *SQLiteGraphStore) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Export all behaviors to nodes.jsonl
	if err := s.exportNodesToJSONL(ctx); err != nil {
		return fmt.Errorf("failed to export nodes: %w", err)
	}

	// Export all edges to edges.jsonl
	if err := s.exportEdgesToJSONL(ctx); err != nil {
		return fmt.Errorf("failed to export edges: %w", err)
	}

	// Clear dirty flags
	if _, err := s.db.ExecContext(ctx, `DELETE FROM dirty_behaviors`); err != nil {
		return fmt.Errorf("failed to clear dirty flags: %w", err)
	}

	return nil
}

// exportNodesToJSONL exports all behaviors to the nodes.jsonl file.
func (s *SQLiteGraphStore) exportNodesToJSONL(ctx context.Context) error {
	// Get all behavior IDs first (close rows before nested queries)
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM behaviors`)
	if err != nil {
		return fmt.Errorf("failed to query behaviors: %w", err)
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan ID: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close() // Close before nested queries to avoid connection pool exhaustion

	// Now fetch each node
	var nodes []Node
	for _, id := range ids {
		node, err := s.getNodeUnlocked(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to get node %s: %w", id, err)
		}
		if node != nil {
			nodes = append(nodes, *node)
		}
	}

	// Write to file
	f, err := os.Create(s.nodesFile)
	if err != nil {
		return fmt.Errorf("failed to create nodes file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	for _, node := range nodes {
		if err := encoder.Encode(node); err != nil {
			return fmt.Errorf("failed to encode node: %w", err)
		}
	}

	return nil
}

// exportEdgesToJSONL exports all edges to the edges.jsonl file.
func (s *SQLiteGraphStore) exportEdgesToJSONL(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT source, target, kind, metadata FROM edges`)
	if err != nil {
		return fmt.Errorf("failed to query edges: %w", err)
	}
	defer rows.Close()

	f, err := os.Create(s.edgesFile)
	if err != nil {
		return fmt.Errorf("failed to create edges file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	for rows.Next() {
		var source, target, kind string
		var metadataJSON sql.NullString

		if err := rows.Scan(&source, &target, &kind, &metadataJSON); err != nil {
			return fmt.Errorf("failed to scan edge: %w", err)
		}

		edge := Edge{
			Source: source,
			Target: target,
			Kind:   kind,
		}

		if metadataJSON.Valid {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(metadataJSON.String), &metadata); err == nil {
				edge.Metadata = metadata
			}
		}

		if err := encoder.Encode(edge); err != nil {
			return fmt.Errorf("failed to encode edge: %w", err)
		}
	}

	return nil
}

// Close syncs and closes the store.
func (s *SQLiteGraphStore) Close() error {
	if err := s.Sync(context.Background()); err != nil {
		// Log but don't fail on sync error during close
		fmt.Fprintf(os.Stderr, "warning: failed to sync during close: %v\n", err)
	}
	return s.db.Close()
}

// Helper functions

func computeContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:8]) // First 8 bytes for shorter hash
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullBytes(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}

func serializeWhenValue(value interface{}) (string, string, error) {
	switch v := value.(type) {
	case string:
		return v, "string", nil
	case []interface{}:
		b, err := json.Marshal(v)
		if err != nil {
			return "", "", fmt.Errorf("marshal array when value: %w", err)
		}
		return string(b), "array", nil
	case []string:
		b, err := json.Marshal(v)
		if err != nil {
			return "", "", fmt.Errorf("marshal string array when value: %w", err)
		}
		return string(b), "array", nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", "", fmt.Errorf("marshal when value: %w", err)
		}
		return string(b), "json", nil
	}
}

func deserializeWhenValue(value, valueType string) (interface{}, error) {
	switch valueType {
	case "string":
		return value, nil
	case "array":
		var arr []interface{}
		if err := json.Unmarshal([]byte(value), &arr); err != nil {
			return nil, fmt.Errorf("unmarshal array when value: %w", err)
		}
		return arr, nil
	case "glob":
		return value, nil
	default:
		var v interface{}
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return nil, fmt.Errorf("unmarshal when value: %w", err)
		}
		return v, nil
	}
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
