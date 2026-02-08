package store

import (
	"context"
	"testing"
)

// setupTestSQLiteStore creates a new SQLite store in a temp directory for testing.
func setupTestSQLiteStore(t *testing.T) (*SQLiteGraphStore, func()) {
	t.Helper()
	tmpDir := t.TempDir()

	store, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}

	cleanup := func() {
		store.Close()
	}

	return store, cleanup
}

func TestValidateBehaviorGraph_ValidGraph(t *testing.T) {
	// Create a store with valid relationships
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add behaviors with valid relationships
	// A requires B, B has no requirements
	behaviorA := createTestBehavior("behavior-a", "Behavior A")
	behaviorA.Content["requires"] = []string{"behavior-b"}
	behaviorB := createTestBehavior("behavior-b", "Behavior B")

	if _, err := store.AddNode(ctx, behaviorA); err != nil {
		t.Fatalf("failed to add behavior A: %v", err)
	}
	if _, err := store.AddNode(ctx, behaviorB); err != nil {
		t.Fatalf("failed to add behavior B: %v", err)
	}

	// Validate
	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("expected no validation errors, got %d: %v", len(errors), errors)
	}
}

func TestValidateBehaviorGraph_DanglingReference(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add behavior that references a non-existent behavior
	behavior := createTestBehavior("behavior-a", "Behavior A")
	behavior.Content["requires"] = []string{"non-existent"}

	if _, err := store.AddNode(ctx, behavior); err != nil {
		t.Fatalf("failed to add behavior: %v", err)
	}

	// Validate
	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if len(errors) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errors), errors)
	}

	ve := errors[0]
	if ve.BehaviorID != "behavior-a" {
		t.Errorf("expected BehaviorID 'behavior-a', got %q", ve.BehaviorID)
	}
	if ve.Field != "requires" {
		t.Errorf("expected Field 'requires', got %q", ve.Field)
	}
	if ve.RefID != "non-existent" {
		t.Errorf("expected RefID 'non-existent', got %q", ve.RefID)
	}
	if ve.Issue != "dangling" {
		t.Errorf("expected Issue 'dangling', got %q", ve.Issue)
	}
}

func TestValidateBehaviorGraph_DanglingInOverrides(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add behavior that overrides a non-existent behavior
	behavior := createTestBehavior("behavior-a", "Behavior A")
	behavior.Content["overrides"] = []string{"non-existent"}

	if _, err := store.AddNode(ctx, behavior); err != nil {
		t.Fatalf("failed to add behavior: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if len(errors) != 1 {
		t.Fatalf("expected 1 validation error, got %d", len(errors))
	}

	if errors[0].Field != "overrides" {
		t.Errorf("expected Field 'overrides', got %q", errors[0].Field)
	}
}

func TestValidateBehaviorGraph_DanglingInConflicts(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add behavior that conflicts with a non-existent behavior
	behavior := createTestBehavior("behavior-a", "Behavior A")
	behavior.Content["conflicts"] = []string{"non-existent"}

	if _, err := store.AddNode(ctx, behavior); err != nil {
		t.Fatalf("failed to add behavior: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if len(errors) != 1 {
		t.Fatalf("expected 1 validation error, got %d", len(errors))
	}

	if errors[0].Field != "conflicts" {
		t.Errorf("expected Field 'conflicts', got %q", errors[0].Field)
	}
}

func TestValidateBehaviorGraph_SelfReference(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add behavior that references itself
	behavior := createTestBehavior("behavior-a", "Behavior A")
	behavior.Content["requires"] = []string{"behavior-a"}

	if _, err := store.AddNode(ctx, behavior); err != nil {
		t.Fatalf("failed to add behavior: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if len(errors) != 1 {
		t.Fatalf("expected 1 validation error, got %d: %v", len(errors), errors)
	}

	ve := errors[0]
	if ve.Issue != "self-reference" {
		t.Errorf("expected Issue 'self-reference', got %q", ve.Issue)
	}
}

func TestValidateBehaviorGraph_SimpleCycle(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create A -> B -> A cycle in requires
	behaviorA := createTestBehavior("behavior-a", "Behavior A")
	behaviorA.Content["requires"] = []string{"behavior-b"}
	behaviorB := createTestBehavior("behavior-b", "Behavior B")
	behaviorB.Content["requires"] = []string{"behavior-a"}

	if _, err := store.AddNode(ctx, behaviorA); err != nil {
		t.Fatalf("failed to add behavior A: %v", err)
	}
	if _, err := store.AddNode(ctx, behaviorB); err != nil {
		t.Fatalf("failed to add behavior B: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	// Should find cycle errors
	cycleErrors := filterByIssue(errors, "cycle")
	if len(cycleErrors) == 0 {
		t.Errorf("expected cycle errors, got none. All errors: %v", errors)
	}
}

func TestValidateBehaviorGraph_ComplexCycle(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create A -> B -> C -> A cycle in requires
	behaviorA := createTestBehavior("behavior-a", "Behavior A")
	behaviorA.Content["requires"] = []string{"behavior-b"}
	behaviorB := createTestBehavior("behavior-b", "Behavior B")
	behaviorB.Content["requires"] = []string{"behavior-c"}
	behaviorC := createTestBehavior("behavior-c", "Behavior C")
	behaviorC.Content["requires"] = []string{"behavior-a"}

	if _, err := store.AddNode(ctx, behaviorA); err != nil {
		t.Fatalf("failed to add behavior A: %v", err)
	}
	if _, err := store.AddNode(ctx, behaviorB); err != nil {
		t.Fatalf("failed to add behavior B: %v", err)
	}
	if _, err := store.AddNode(ctx, behaviorC); err != nil {
		t.Fatalf("failed to add behavior C: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	// Should find cycle errors
	cycleErrors := filterByIssue(errors, "cycle")
	if len(cycleErrors) == 0 {
		t.Errorf("expected cycle errors, got none. All errors: %v", errors)
	}
}

func TestValidateBehaviorGraph_MixedIssues(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Behavior A: self-reference and dangling reference
	behaviorA := createTestBehavior("behavior-a", "Behavior A")
	behaviorA.Content["requires"] = []string{"behavior-a", "non-existent"}

	// Behavior B and C form a cycle
	behaviorB := createTestBehavior("behavior-b", "Behavior B")
	behaviorB.Content["requires"] = []string{"behavior-c"}
	behaviorC := createTestBehavior("behavior-c", "Behavior C")
	behaviorC.Content["requires"] = []string{"behavior-b"}

	if _, err := store.AddNode(ctx, behaviorA); err != nil {
		t.Fatalf("failed to add behavior A: %v", err)
	}
	if _, err := store.AddNode(ctx, behaviorB); err != nil {
		t.Fatalf("failed to add behavior B: %v", err)
	}
	if _, err := store.AddNode(ctx, behaviorC); err != nil {
		t.Fatalf("failed to add behavior C: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	// Should have multiple error types
	selfRefErrors := filterByIssue(errors, "self-reference")
	danglingErrors := filterByIssue(errors, "dangling")
	cycleErrors := filterByIssue(errors, "cycle")

	if len(selfRefErrors) == 0 {
		t.Error("expected self-reference error")
	}
	if len(danglingErrors) == 0 {
		t.Error("expected dangling reference error")
	}
	if len(cycleErrors) == 0 {
		t.Error("expected cycle error")
	}
}

func TestValidateBehaviorGraph_DanglingEdgeTarget(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add a behavior and an edge pointing to a non-existent target
	behavior := createTestBehavior("behavior-a", "Behavior A")
	if _, err := store.AddNode(ctx, behavior); err != nil {
		t.Fatalf("failed to add behavior: %v", err)
	}

	edge := Edge{
		Source: "behavior-a",
		Target: "nonexistent-target",
		Kind:   "similar-to",
		Weight: 0.8,
	}
	if err := store.AddEdge(ctx, edge); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	// Should report a dangling edge-target
	edgeErrors := filterByField(errors, "edge-target")
	if len(edgeErrors) != 1 {
		t.Fatalf("expected 1 edge-target error, got %d: %v", len(edgeErrors), errors)
	}
	if edgeErrors[0].RefID != "nonexistent-target" {
		t.Errorf("expected RefID 'nonexistent-target', got %q", edgeErrors[0].RefID)
	}
	if edgeErrors[0].Issue != "dangling" {
		t.Errorf("expected Issue 'dangling', got %q", edgeErrors[0].Issue)
	}
}

func TestValidateBehaviorGraph_DanglingEdgeSource(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add a behavior, then insert an edge with a non-existent source directly
	behavior := createTestBehavior("behavior-a", "Behavior A")
	if _, err := store.AddNode(ctx, behavior); err != nil {
		t.Fatalf("failed to add behavior: %v", err)
	}

	// Insert edge directly into DB to bypass any source validation
	_, err := store.db.ExecContext(ctx,
		`INSERT INTO edges (source, target, kind, weight) VALUES (?, ?, ?, ?)`,
		"nonexistent-source", "behavior-a", "requires", 1.0)
	if err != nil {
		t.Fatalf("failed to insert edge: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	edgeErrors := filterByField(errors, "edge-source")
	if len(edgeErrors) != 1 {
		t.Fatalf("expected 1 edge-source error, got %d: %v", len(edgeErrors), errors)
	}
	if edgeErrors[0].RefID != "nonexistent-source" {
		t.Errorf("expected RefID 'nonexistent-source', got %q", edgeErrors[0].RefID)
	}
}

func TestValidateBehaviorGraph_ValidEdges(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	behaviorA := createTestBehavior("behavior-a", "Behavior A")
	behaviorB := createTestBehavior("behavior-b", "Behavior B")
	if _, err := store.AddNode(ctx, behaviorA); err != nil {
		t.Fatalf("failed to add A: %v", err)
	}
	if _, err := store.AddNode(ctx, behaviorB); err != nil {
		t.Fatalf("failed to add B: %v", err)
	}

	edge := Edge{Source: "behavior-a", Target: "behavior-b", Kind: "similar-to", Weight: 0.8}
	if err := store.AddEdge(ctx, edge); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}
	if len(errors) != 0 {
		t.Errorf("expected no errors for valid edges, got %d: %v", len(errors), errors)
	}
}

func TestValidateBehaviorGraph_EmptyStore(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if len(errors) != 0 {
		t.Errorf("expected no errors for empty store, got %d", len(errors))
	}
}

func TestValidateBehaviorGraph_IgnoresNonBehaviorKinds(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add a behavior with valid reference to forgotten-behavior
	forgotten := createTestBehavior("forgotten-a", "Forgotten A")
	forgotten.Kind = "forgotten-behavior"

	active := createTestBehavior("behavior-a", "Behavior A")
	active.Content["overrides"] = []string{"forgotten-a"}

	if _, err := store.AddNode(ctx, forgotten); err != nil {
		t.Fatalf("failed to add forgotten behavior: %v", err)
	}
	if _, err := store.AddNode(ctx, active); err != nil {
		t.Fatalf("failed to add active behavior: %v", err)
	}

	errors, err := store.ValidateBehaviorGraph(ctx)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	// Should have no errors since forgotten-a exists (even though it's forgotten)
	if len(errors) != 0 {
		t.Errorf("expected no errors, got %d: %v", len(errors), errors)
	}
}

// Test helper functions

func TestDetectCycles_NoCycles(t *testing.T) {
	// A -> B -> C (no cycles)
	graph := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {},
	}

	cycles := detectCycles(graph)
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %v", cycles)
	}
}

func TestDetectCycles_SimpleCycle(t *testing.T) {
	// A -> B -> A
	graph := map[string][]string{
		"A": {"B"},
		"B": {"A"},
	}

	cycles := detectCycles(graph)
	if len(cycles) == 0 {
		t.Error("expected to find a cycle")
	}
}

func TestDetectCycles_ComplexCycle(t *testing.T) {
	// A -> B -> C -> A
	graph := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"A"},
	}

	cycles := detectCycles(graph)
	if len(cycles) == 0 {
		t.Error("expected to find a cycle")
	}
}

func TestDetectCycles_MultipleCycles(t *testing.T) {
	// A -> B -> A and C -> D -> C
	graph := map[string][]string{
		"A": {"B"},
		"B": {"A"},
		"C": {"D"},
		"D": {"C"},
	}

	cycles := detectCycles(graph)
	// Should detect both cycles
	if len(cycles) < 2 {
		t.Errorf("expected at least 2 cycles, got %d: %v", len(cycles), cycles)
	}
}

func TestDetectCycles_SelfLoop(t *testing.T) {
	// A -> A (self-loop)
	graph := map[string][]string{
		"A": {"A"},
	}

	cycles := detectCycles(graph)
	if len(cycles) == 0 {
		t.Error("expected to find a self-loop cycle")
	}
}

func TestFindDanglingRefs(t *testing.T) {
	allIDs := map[string]bool{
		"A": true,
		"B": true,
		"C": true,
	}

	// No dangling refs
	refs := []string{"A", "B"}
	dangling := findDanglingRefs("test", refs, allIDs)
	if len(dangling) != 0 {
		t.Errorf("expected no dangling refs, got %v", dangling)
	}

	// One dangling ref
	refs = []string{"A", "D"}
	dangling = findDanglingRefs("test", refs, allIDs)
	if len(dangling) != 1 || dangling[0] != "D" {
		t.Errorf("expected [D], got %v", dangling)
	}

	// Multiple dangling refs
	refs = []string{"D", "E"}
	dangling = findDanglingRefs("test", refs, allIDs)
	if len(dangling) != 2 {
		t.Errorf("expected 2 dangling refs, got %v", dangling)
	}
}

// Helper functions for tests

func createTestBehavior(id, name string) Node {
	return Node{
		ID:   id,
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": name,
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test behavior " + name,
			},
			"provenance": map[string]interface{}{
				"source_type": "manual",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.8,
			"priority":   1,
			"scope":      "local",
		},
	}
}

func filterByField(errors []ValidationError, field string) []ValidationError {
	var result []ValidationError
	for _, e := range errors {
		if e.Field == field {
			result = append(result, e)
		}
	}
	return result
}

func filterByIssue(errors []ValidationError, issue string) []ValidationError {
	var result []ValidationError
	for _, e := range errors {
		if e.Issue == issue {
			result = append(result, e)
		}
	}
	return result
}
