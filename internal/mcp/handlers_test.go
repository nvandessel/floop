package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

func setupTestServer(t *testing.T) (*Server, string) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0755); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	// Set isolated HOME to prevent global store interference
	tmpHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(tmpHome, 0755); err != nil {
		t.Fatalf("Failed to create temp home: %v", err)
	}
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
	})

	cfg := &Config{
		Name:    "test-server",
		Version: "v1.0.0",
		Root:    tmpDir,
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	return server, tmpDir
}

func TestHandleFloopActive_Empty(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer server.Close()

	// Test with no behaviors in store
	ctx := context.Background()
	req := &sdk.CallToolRequest{}

	args := FloopActiveInput{}
	result, output, err := server.handleFloopActive(ctx, req, args)

	if err != nil {
		t.Fatalf("handleFloopActive failed: %v", err)
	}

	if result != nil {
		t.Error("Expected nil result (SDK auto-populates)")
	}

	if output.Count != 0 {
		t.Errorf("Count = %d, want 0", output.Count)
	}

	if len(output.Active) != 0 {
		t.Errorf("len(Active) = %d, want 0", len(output.Active))
	}

	// Verify context is populated
	if output.Context["repo"] != tmpDir {
		t.Errorf("Context repo = %v, want %v", output.Context["repo"], tmpDir)
	}
}

func TestHandleFloopActive_WithFile(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer server.Close()

	// Create a test Go file
	testFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()
	req := &sdk.CallToolRequest{}

	args := FloopActiveInput{
		File: "main.go",
		Task: "development",
	}

	result, output, err := server.handleFloopActive(ctx, req, args)

	if err != nil {
		t.Fatalf("handleFloopActive failed: %v", err)
	}

	if result != nil {
		t.Error("Expected nil result")
	}

	// Check context includes file info
	if output.Context["file"] == "" {
		t.Error("Context file is empty")
	}

	if output.Context["language"] != "go" {
		t.Errorf("Context language = %v, want 'go'", output.Context["language"])
	}

	if output.Context["task"] != "development" {
		t.Errorf("Context task = %v, want 'development'", output.Context["task"])
	}
}

func TestHandleFloopLearn_RequiredParams(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}

	// Test missing 'wrong' parameter
	args := FloopLearnInput{
		Right: "Do it this way",
	}

	_, _, err := server.handleFloopLearn(ctx, req, args)
	if err == nil {
		t.Error("Expected error for missing 'wrong' parameter")
	}

	// Test missing 'right' parameter
	args = FloopLearnInput{
		Wrong: "Did it wrong",
	}

	_, _, err = server.handleFloopLearn(ctx, req, args)
	if err == nil {
		t.Error("Expected error for missing 'right' parameter")
	}
}

func TestHandleFloopLearn_Success(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}

	args := FloopLearnInput{
		Wrong: "Used fmt.Println for errors",
		Right: "Use fmt.Fprintln(os.Stderr, err) for error output",
		File:  "main.go",
	}

	result, output, err := server.handleFloopLearn(ctx, req, args)

	if err != nil {
		t.Fatalf("handleFloopLearn failed: %v", err)
	}

	if result != nil {
		t.Error("Expected nil result")
	}

	if output.CorrectionID == "" {
		t.Error("CorrectionID is empty")
	}

	if output.BehaviorID == "" {
		t.Error("BehaviorID is empty")
	}

	if output.Confidence < 0 || output.Confidence > 1 {
		t.Errorf("Confidence = %f, want 0.0-1.0", output.Confidence)
	}

	if output.Message == "" {
		t.Error("Message is empty")
	}

	// Verify behavior was saved to store
	node, err := server.store.GetNode(ctx, output.BehaviorID)
	if err != nil {
		t.Fatalf("Failed to get behavior from store: %v", err)
	}

	if node == nil {
		t.Error("Behavior not found in store")
	}
}

func TestHandleFloopList_Behaviors(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Add a test behavior to store
	behavior := models.Behavior{
		ID:   "test-behavior-1",
		Name: "test-behavior",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "Test behavior",
		},
		Confidence: 0.9,
		Provenance: models.Provenance{
			SourceType: models.SourceTypeLearned,
			CreatedAt:  time.Now(),
		},
	}

	// Convert behavior to node manually
	behaviorNode := store.Node{
		ID:   behavior.ID,
		Kind: "behavior",
		Content: map[string]interface{}{
			"name":       behavior.Name,
			"kind":       string(behavior.Kind),
			"when":       behavior.When,
			"content":    behavior.Content,
			"provenance": behavior.Provenance,
			"requires":   behavior.Requires,
			"overrides":  behavior.Overrides,
			"conflicts":  behavior.Conflicts,
		},
		Metadata: map[string]interface{}{
			"confidence": behavior.Confidence,
			"priority":   behavior.Priority,
			"stats":      behavior.Stats,
		},
	}
	if _, err := server.store.AddNode(ctx, behaviorNode); err != nil {
		t.Fatalf("Failed to add test behavior: %v", err)
	}
	if err := server.store.Sync(ctx); err != nil {
		t.Fatalf("Failed to sync store: %v", err)
	}

	// List behaviors
	req := &sdk.CallToolRequest{}
	args := FloopListInput{Corrections: false}

	result, output, err := server.handleFloopList(ctx, req, args)

	if err != nil {
		t.Fatalf("handleFloopList failed: %v", err)
	}

	if result != nil {
		t.Error("Expected nil result")
	}

	if output.Count < 1 {
		t.Errorf("Count = %d, want at least 1", output.Count)
	}

	if len(output.Behaviors) < 1 {
		t.Fatalf("len(Behaviors) = %d, want at least 1", len(output.Behaviors))
	}

	// Find our test behavior
	var found *BehaviorListItem
	for i := range output.Behaviors {
		if output.Behaviors[i].ID == "test-behavior-1" {
			found = &output.Behaviors[i]
			break
		}
	}

	if found == nil {
		t.Fatal("Test behavior not found in results")
	}

	if found.Kind != "directive" {
		t.Errorf("Behavior kind = %q, want %q", found.Kind, "directive")
	}

	// Note: Source extraction from NodeToBehavior is incomplete in learning package
	// so we just verify the behavior was found
}

func TestHandleFloopList_Corrections(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Write a test correction to corrections.jsonl file
	correctionsPath := filepath.Join(tmpDir, ".floop", "corrections.jsonl")
	correction := models.Correction{
		ID:              "correction-1",
		Timestamp:       time.Now(),
		AgentAction:     "Did something wrong",
		CorrectedAction: "Should do it right",
		Processed:       false,
	}
	f, err := os.Create(correctionsPath)
	if err != nil {
		t.Fatalf("Failed to create corrections file: %v", err)
	}
	// Write as JSON line
	corrJSON := `{"id":"correction-1","timestamp":"` + correction.Timestamp.Format(time.RFC3339) + `","agent_action":"Did something wrong","corrected_action":"Should do it right","processed":false}`
	if _, err := f.WriteString(corrJSON + "\n"); err != nil {
		f.Close()
		t.Fatalf("Failed to write correction: %v", err)
	}
	f.Close()

	// List corrections
	req := &sdk.CallToolRequest{}
	args := FloopListInput{Corrections: true}

	result, output, err := server.handleFloopList(ctx, req, args)

	if err != nil {
		t.Fatalf("handleFloopList failed: %v", err)
	}

	if result != nil {
		t.Error("Expected nil result")
	}

	if output.Count != 1 {
		t.Errorf("Count = %d, want 1", output.Count)
	}

	if len(output.Corrections) != 1 {
		t.Fatalf("len(Corrections) = %d, want 1", len(output.Corrections))
	}

	c := output.Corrections[0]
	if c.ID != "correction-1" {
		t.Errorf("Correction ID = %q, want %q", c.ID, "correction-1")
	}

	if c.AgentAction != "Did something wrong" {
		t.Errorf("AgentAction = %q, want %q", c.AgentAction, "Did something wrong")
	}

	if c.Processed != false {
		t.Errorf("Processed = %v, want false", c.Processed)
	}
}

func TestHandleFloopActive_SpreadingActivation(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Create a test Go file so language detection works.
	testFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Behavior A: matches Go context.
	nodeA := store.Node{
		ID:   "behavior-a",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Go Behavior A",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Use gofmt",
			},
			"when": map[string]interface{}{
				"language": "go",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.9,
			"priority":   1,
		},
	}

	// Behavior B: no matching When — won't activate via context alone.
	nodeB := store.Node{
		ID:   "behavior-b",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Related Behavior B",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Run go vet",
			},
			"when": map[string]interface{}{
				"language": "rust",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.85,
			"priority":   1,
		},
	}

	if _, err := server.store.AddNode(ctx, nodeA); err != nil {
		t.Fatalf("Failed to add node A: %v", err)
	}
	if _, err := server.store.AddNode(ctx, nodeB); err != nil {
		t.Fatalf("Failed to add node B: %v", err)
	}

	// Wire edge A→B (similar-to, weight 0.8).
	edge := store.Edge{
		Source:    "behavior-a",
		Target:    "behavior-b",
		Kind:      "similar-to",
		Weight:    0.8,
		CreatedAt: time.Now(),
	}
	if err := server.store.AddEdge(ctx, edge); err != nil {
		t.Fatalf("Failed to add edge: %v", err)
	}
	if err := server.store.Sync(ctx); err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	// Activate with Go context — should get both A (direct) and B (spread).
	req := &sdk.CallToolRequest{}
	args := FloopActiveInput{
		File: "main.go",
	}

	_, output, err := server.handleFloopActive(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopActive failed: %v", err)
	}

	if output.Count < 2 {
		t.Errorf("Count = %d, want at least 2 (direct + spread)", output.Count)
	}

	// Verify both behaviors are present.
	foundA, foundB := false, false
	for _, s := range output.Active {
		switch s.ID {
		case "behavior-a":
			foundA = true
			if s.Distance != 0 {
				t.Errorf("Behavior A distance = %d, want 0 (direct seed)", s.Distance)
			}
		case "behavior-b":
			foundB = true
			if s.Distance == 0 {
				t.Errorf("Behavior B distance = %d, want > 0 (spread-activated)", s.Distance)
			}
			if s.SeedSource == "" {
				t.Error("Behavior B should have a SeedSource")
			}
		}
	}

	if !foundA {
		t.Error("Behavior A not found in active results")
	}
	if !foundB {
		t.Error("Behavior B not found in active results (should be spread-activated via edge from A)")
	}
}

func TestHandleFloopActive_NoEdgesBackwardCompat(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	testFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Add a single behavior that matches Go context — no edges.
	node := store.Node{
		ID:   "solo-behavior",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Solo Go Behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Always use table-driven tests",
			},
			"when": map[string]interface{}{
				"language": "go",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.9,
			"priority":   1,
		},
	}

	if _, err := server.store.AddNode(ctx, node); err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}
	if err := server.store.Sync(ctx); err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	req := &sdk.CallToolRequest{}
	args := FloopActiveInput{
		File: "main.go",
	}

	_, output, err := server.handleFloopActive(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopActive failed: %v", err)
	}

	// Should still find the direct match.
	if output.Count != 1 {
		t.Errorf("Count = %d, want 1", output.Count)
	}

	if len(output.Active) != 1 {
		t.Fatalf("len(Active) = %d, want 1", len(output.Active))
	}

	if output.Active[0].ID != "solo-behavior" {
		t.Errorf("Active[0].ID = %q, want %q", output.Active[0].ID, "solo-behavior")
	}
}

func TestBehaviorContentToMap(t *testing.T) {
	content := models.BehaviorContent{
		Canonical: "Use X instead of Y",
		Expanded:  "When doing Z, always use X instead of Y because...",
		Structured: map[string]interface{}{
			"prefer": "X",
			"avoid":  "Y",
		},
	}

	m := behaviorContentToMap(content)

	if m["canonical"] != content.Canonical {
		t.Errorf("canonical = %v, want %v", m["canonical"], content.Canonical)
	}

	if m["expanded"] != content.Expanded {
		t.Errorf("expanded = %v, want %v", m["expanded"], content.Expanded)
	}

	structured, ok := m["structured"].(map[string]interface{})
	if !ok {
		t.Fatal("structured is not map[string]interface{}")
	}

	if len(structured) != 2 {
		t.Errorf("len(structured) = %d, want 2", len(structured))
	}
}

func TestHandleFloopValidate_EmptyStore(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopValidateInput{}

	result, output, err := server.handleFloopValidate(ctx, req, args)

	if err != nil {
		t.Fatalf("handleFloopValidate failed: %v", err)
	}

	if result != nil {
		t.Error("Expected nil result")
	}

	if !output.Valid {
		t.Error("Expected Valid = true for empty store")
	}

	if output.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0", output.ErrorCount)
	}

	if len(output.Errors) != 0 {
		t.Errorf("len(Errors) = %d, want 0", len(output.Errors))
	}

	if output.Message == "" {
		t.Error("Message should not be empty")
	}
}

func TestHandleFloopValidate_WithDanglingReference(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Add a behavior with a dangling reference
	behaviorNode := store.Node{
		ID:   "test-behavior-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test Behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test canonical content",
			},
			"requires": []string{"non-existent-behavior"},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.8,
			"priority":   1,
			"scope":      "local",
		},
	}

	if _, err := server.store.AddNode(ctx, behaviorNode); err != nil {
		t.Fatalf("Failed to add test behavior: %v", err)
	}

	// Validate
	req := &sdk.CallToolRequest{}
	args := FloopValidateInput{}

	result, output, err := server.handleFloopValidate(ctx, req, args)

	if err != nil {
		t.Fatalf("handleFloopValidate failed: %v", err)
	}

	if result != nil {
		t.Error("Expected nil result")
	}

	if output.Valid {
		t.Error("Expected Valid = false for store with dangling reference")
	}

	if output.ErrorCount == 0 {
		t.Error("Expected at least 1 error")
	}

	// Check for dangling error
	foundDangling := false
	for _, e := range output.Errors {
		if e.Issue == "dangling" {
			foundDangling = true
			break
		}
	}

	if !foundDangling {
		t.Errorf("Expected dangling error, got: %v", output.Errors)
	}
}
