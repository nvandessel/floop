package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// ValidationError describes a graph validation issue.
type ValidationError struct {
	BehaviorID string `json:"behavior_id"`
	Field      string `json:"field"`  // "requires", "overrides", "conflicts"
	RefID      string `json:"ref_id"` // The problematic reference
	Issue      string `json:"issue"`  // "dangling", "cycle", "self-reference"
}

// String returns a human-readable description of the validation error.
func (e ValidationError) String() string {
	return fmt.Sprintf("%s: %s in %s references %s", e.Issue, e.BehaviorID, e.Field, e.RefID)
}

// ValidateBehaviorGraph validates the relationship graph for consistency.
// Returns validation errors for:
// - Dangling references (references to non-existent behavior IDs)
// - Cycles in requires graph (A requires B, B requires A)
// - Self-references (behavior references itself)
func (s *SQLiteGraphStore) ValidateBehaviorGraph(ctx context.Context) ([]ValidationError, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var errors []ValidationError

	// Get all behavior nodes
	behaviors, err := s.getAllBehaviorsForValidation(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get behaviors: %w", err)
	}

	// Build set of all behavior IDs for dangling reference detection
	allIDs := make(map[string]bool)
	for _, b := range behaviors {
		allIDs[b.id] = true
	}

	// Build requires graph for cycle detection
	requiresGraph := make(map[string][]string)

	// Check each behavior
	for _, b := range behaviors {
		// Check self-references and dangling references in each relationship field
		selfRefs, danglingRefs := checkRelationshipField(b.id, b.requires, allIDs)
		for _, ref := range selfRefs {
			errors = append(errors, ValidationError{
				BehaviorID: b.id,
				Field:      "requires",
				RefID:      ref,
				Issue:      "self-reference",
			})
		}
		for _, ref := range danglingRefs {
			errors = append(errors, ValidationError{
				BehaviorID: b.id,
				Field:      "requires",
				RefID:      ref,
				Issue:      "dangling",
			})
		}

		selfRefs, danglingRefs = checkRelationshipField(b.id, b.overrides, allIDs)
		for _, ref := range selfRefs {
			errors = append(errors, ValidationError{
				BehaviorID: b.id,
				Field:      "overrides",
				RefID:      ref,
				Issue:      "self-reference",
			})
		}
		for _, ref := range danglingRefs {
			errors = append(errors, ValidationError{
				BehaviorID: b.id,
				Field:      "overrides",
				RefID:      ref,
				Issue:      "dangling",
			})
		}

		selfRefs, danglingRefs = checkRelationshipField(b.id, b.conflicts, allIDs)
		for _, ref := range selfRefs {
			errors = append(errors, ValidationError{
				BehaviorID: b.id,
				Field:      "conflicts",
				RefID:      ref,
				Issue:      "self-reference",
			})
		}
		for _, ref := range danglingRefs {
			errors = append(errors, ValidationError{
				BehaviorID: b.id,
				Field:      "conflicts",
				RefID:      ref,
				Issue:      "dangling",
			})
		}

		// Build requires graph (only for existing references)
		validRequires := make([]string, 0, len(b.requires))
		for _, ref := range b.requires {
			if allIDs[ref] && ref != b.id {
				validRequires = append(validRequires, ref)
			}
		}
		requiresGraph[b.id] = validRequires
	}

	// Check edges table for dangling source/target references
	edgeErrors, err := s.validateEdges(ctx, allIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to validate edges: %w", err)
	}
	errors = append(errors, edgeErrors...)

	// Detect cycles in requires graph
	cycles := detectCycles(requiresGraph)
	for _, cycle := range cycles {
		// Report cycle error for the first node in the cycle
		// The cycle path shows which nodes are involved
		if len(cycle) >= 2 {
			errors = append(errors, ValidationError{
				BehaviorID: cycle[0],
				Field:      "requires",
				RefID:      cycle[1], // The immediate dependency that leads to cycle
				Issue:      "cycle",
			})
		}
	}

	return errors, nil
}

// validateEdges checks all edges in the edges table for dangling references.
func (s *SQLiteGraphStore) validateEdges(ctx context.Context, allIDs map[string]bool) ([]ValidationError, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source, target, kind FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("failed to query edges: %w", err)
	}
	defer rows.Close()

	var errors []ValidationError
	for rows.Next() {
		var source, target, kind string
		if err := rows.Scan(&source, &target, &kind); err != nil {
			return nil, fmt.Errorf("failed to scan edge: %w", err)
		}
		if !allIDs[source] {
			errors = append(errors, ValidationError{
				BehaviorID: source,
				Field:      "edge-source",
				RefID:      source,
				Issue:      "dangling",
			})
		}
		if !allIDs[target] {
			errors = append(errors, ValidationError{
				BehaviorID: source,
				Field:      "edge-target",
				RefID:      target,
				Issue:      "dangling",
			})
		}
	}
	return errors, rows.Err()
}

// behaviorRelationships holds the relationship arrays for a behavior.
type behaviorRelationships struct {
	id        string
	requires  []string
	overrides []string
	conflicts []string
}

// getAllBehaviorsForValidation retrieves all behaviors with their relationship arrays.
func (s *SQLiteGraphStore) getAllBehaviorsForValidation(ctx context.Context) ([]behaviorRelationships, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, requires, overrides, conflicts FROM behaviors
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query behaviors: %w", err)
	}
	defer rows.Close()

	var behaviors []behaviorRelationships
	for rows.Next() {
		var id string
		var requiresJSON, overridesJSON, conflictsJSON *string

		if err := rows.Scan(&id, &requiresJSON, &overridesJSON, &conflictsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan behavior: %w", err)
		}

		b := behaviorRelationships{id: id}
		b.requires = parseStringArray(requiresJSON)
		b.overrides = parseStringArray(overridesJSON)
		b.conflicts = parseStringArray(conflictsJSON)

		behaviors = append(behaviors, b)
	}

	return behaviors, rows.Err()
}

// parseStringArray parses a JSON string array from a nullable string.
func parseStringArray(jsonStr *string) []string {
	if jsonStr == nil || *jsonStr == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(*jsonStr), &arr); err != nil {
		log.Printf("warning: corrupt JSON in string array field: %v", err)
		return nil
	}
	return arr
}

// checkRelationshipField checks for self-references and dangling references in a relationship field.
// Returns (selfRefs, danglingRefs).
func checkRelationshipField(behaviorID string, refs []string, allIDs map[string]bool) ([]string, []string) {
	var selfRefs, danglingRefs []string
	for _, ref := range refs {
		if ref == behaviorID {
			selfRefs = append(selfRefs, ref)
		} else if !allIDs[ref] {
			danglingRefs = append(danglingRefs, ref)
		}
	}
	return selfRefs, danglingRefs
}

// findDanglingRefs finds references that don't exist in the allIDs set.
func findDanglingRefs(behaviorID string, refs []string, allIDs map[string]bool) []string {
	var dangling []string
	for _, ref := range refs {
		if !allIDs[ref] {
			dangling = append(dangling, ref)
		}
	}
	return dangling
}

// detectCycles detects cycles in a directed graph using DFS with color marking.
// Returns a list of cycles found, where each cycle is a list of node IDs.
func detectCycles(graph map[string][]string) [][]string {
	// Color states: 0 = white (unvisited), 1 = gray (in progress), 2 = black (done)
	color := make(map[string]int)
	parent := make(map[string]string)
	var cycles [][]string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = 1 // Mark as in-progress (gray)

		for _, neighbor := range graph[node] {
			if color[neighbor] == 1 {
				// Found a back edge - this is a cycle
				// Reconstruct the cycle path
				cycle := []string{neighbor, node}
				current := node
				for current != neighbor {
					if p, ok := parent[current]; ok && p != neighbor {
						cycle = append(cycle, p)
						current = p
					} else {
						break
					}
				}
				cycles = append(cycles, cycle)
				return true
			}

			if color[neighbor] == 0 {
				parent[neighbor] = node
				dfs(neighbor)
			}
		}

		color[node] = 2 // Mark as done (black)
		return false
	}

	// Run DFS from all unvisited nodes
	for node := range graph {
		if color[node] == 0 {
			dfs(node)
		}
	}

	return cycles
}
