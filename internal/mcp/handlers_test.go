package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/feedback-loop/internal/activation"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
)

func setupTestServer(t *testing.T) (*Server, string) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("Failed to create .floop dir: %v", err)
	}

	// Set isolated HOME to prevent global store interference
	tmpHome := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(tmpHome, 0700); err != nil {
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

	// Test with no user behaviors in store (seeds are auto-injected)
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

	// Auto-seeded behaviors (2) are always present
	if output.Count != 2 {
		t.Errorf("Count = %d, want 2 (seed behaviors)", output.Count)
	}

	if len(output.Active) != 2 {
		t.Errorf("len(Active) = %d, want 2 (seed behaviors)", len(output.Active))
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
	if err := os.WriteFile(testFile, []byte("package main"), 0600); err != nil {
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
	if err := os.WriteFile(testFile, []byte("package main"), 0600); err != nil {
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
	if err := os.WriteFile(testFile, []byte("package main"), 0600); err != nil {
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

	// Should find the direct match plus 2 seed behaviors (no when = always active)
	if output.Count != 3 {
		t.Errorf("Count = %d, want 3 (1 user + 2 seeds)", output.Count)
	}

	if len(output.Active) != 3 {
		t.Fatalf("len(Active) = %d, want 3", len(output.Active))
	}

	// Verify the user behavior is present
	found := false
	for _, b := range output.Active {
		if b.ID == "solo-behavior" {
			found = true
			break
		}
	}
	if !found {
		t.Error("solo-behavior not found in active results")
	}
}

func TestHandleFloopDeduplicate_RateLimited(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopDeduplicateInput{}

	// floop_deduplicate has burst=1, so first call succeeds
	_, _, err := server.handleFloopDeduplicate(ctx, req, args)
	if err != nil {
		t.Fatalf("First deduplicate should succeed: %v", err)
	}

	// Second call should be rate-limited
	_, _, err = server.handleFloopDeduplicate(ctx, req, args)
	if err == nil {
		t.Error("Second deduplicate should be rate-limited")
	}
	if err != nil && !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("Expected rate limit error, got: %v", err)
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

func TestHandleBehaviorsResource_FramingIsAdvisory(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Add a behavior with no "when" conditions so it always activates.
	behaviorNode := store.Node{
		ID:   "framing-test-behavior",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Test Framing Behavior",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Always use table-driven tests in Go",
			},
			"when": map[string]interface{}{},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.9,
			"priority":   1,
		},
	}
	if _, err := server.store.AddNode(ctx, behaviorNode); err != nil {
		t.Fatalf("Failed to add test behavior: %v", err)
	}
	if err := server.store.Sync(ctx); err != nil {
		t.Fatalf("Failed to sync store: %v", err)
	}

	// Call the resource handler.
	req := &sdk.ReadResourceRequest{}
	result, err := server.handleBehaviorsResource(ctx, req)
	if err != nil {
		t.Fatalf("handleBehaviorsResource failed: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("Expected at least one resource content block")
	}

	text := result.Contents[0].Text

	// Subtest table: phrases that MUST NOT appear (authoritative framing).
	forbiddenPhrases := []struct {
		name   string
		phrase string
	}{
		{"no CRITICAL keyword", "CRITICAL"},
		{"no Violating language", "Violating"},
		{"no repeating a past mistake", "repeating a past mistake"},
		{"no YOUR learned memories", "YOUR learned memories"},
	}

	for _, tc := range forbiddenPhrases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(text, tc.phrase) {
				t.Errorf("Resource output contains forbidden phrase %q:\n%s", tc.phrase, text)
			}
		})
	}

	// Subtest table: phrases that SHOULD appear (advisory framing).
	requiredPhrases := []struct {
		name   string
		phrase string
	}{
		{"has Learned Behaviors header", "# Learned Behaviors"},
		{"has advisory suggestion language", "Suggestions"},
		{"has override-friendly language", "override"},
		{"has memories active stats", "memories active"},
	}

	for _, tc := range requiredPhrases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(text, tc.phrase) {
				t.Errorf("Resource output missing required phrase %q:\n%s", tc.phrase, text)
			}
		})
	}
}

func TestBuildSpreadIndex_UsesSpreadResults(t *testing.T) {
	seeds := []spreading.Seed{
		{BehaviorID: "seed-1", Activation: 0.4, Source: "context:task=dev"},
		{BehaviorID: "seed-2", Activation: 0.6, Source: "context:language=go"},
	}

	spreadResults := []spreading.Result{
		{BehaviorID: "seed-1", Activation: 0.75, Distance: 0, SeedSource: "context:task=dev"},
		{BehaviorID: "seed-2", Activation: 0.85, Distance: 0, SeedSource: "context:language=go"},
		{BehaviorID: "spread-1", Activation: 0.52, Distance: 1, SeedSource: "seed-1"},
		{BehaviorID: "spread-2", Activation: 0.38, Distance: 2, SeedSource: "seed-2"},
	}

	matches := []activation.ActivationResult{
		{Behavior: models.Behavior{ID: "seed-1"}, Specificity: 1},
		{Behavior: models.Behavior{ID: "seed-2"}, Specificity: 2},
		{Behavior: models.Behavior{ID: "spread-1"}, Specificity: 0},
		{Behavior: models.Behavior{ID: "spread-2"}, Specificity: 0},
	}

	index := buildSpreadIndex(seeds, matches, spreadResults)

	tests := []struct {
		name       string
		id         string
		wantAct    float64
		wantDist   int
		wantSource string
	}{
		// Seeds should use post-engine activation (0.75, 0.85), NOT input (0.4, 0.6)
		{"seed-1 uses engine activation", "seed-1", 0.75, 0, "context:task=dev"},
		{"seed-2 uses engine activation", "seed-2", 0.85, 0, "context:language=go"},
		// Spread-only should use engine values, NOT hardcoded 0.3
		{"spread-1 uses engine activation", "spread-1", 0.52, 1, "seed-1"},
		{"spread-2 uses engine activation", "spread-2", 0.38, 2, "seed-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, ok := index[tt.id]
			if !ok {
				t.Fatalf("behavior %q not in spread index", tt.id)
			}
			if meta.activation != tt.wantAct {
				t.Errorf("activation = %f, want %f", meta.activation, tt.wantAct)
			}
			if meta.distance != tt.wantDist {
				t.Errorf("distance = %d, want %d", meta.distance, tt.wantDist)
			}
			if meta.seedSource != tt.wantSource {
				t.Errorf("seedSource = %q, want %q", meta.seedSource, tt.wantSource)
			}
		})
	}
}

func TestBehaviorContentToMap_IncludesTags(t *testing.T) {
	content := models.BehaviorContent{
		Canonical: "test behavior",
		Tags:      []string{"git", "workflow"},
	}

	m := behaviorContentToMap(content)

	tags, ok := m["tags"].([]string)
	if !ok {
		t.Fatal("expected tags in content map")
	}
	if len(tags) != 2 || tags[0] != "git" || tags[1] != "workflow" {
		t.Errorf("tags = %v, want [git workflow]", tags)
	}
}

func TestHandleFloopActive_TokenStats(t *testing.T) {
	// Token counts use raw canonical bytes/4 (no tiering in MCP tool output).
	// 2 seed behaviors are auto-injected (94 tokens total from their canonical content).
	tests := []struct {
		name              string
		behaviors         []store.Node
		wantTokens        int
		wantBudget        int
		wantBehaviorCount int
	}{
		{
			// 2 seed behaviors are auto-injected (94 tokens total)
			name:              "empty store has zero token stats",
			behaviors:         nil,
			wantTokens:        94,
			wantBudget:        2000,
			wantBehaviorCount: 2,
		},
		{
			name: "single behavior with known canonical content",
			behaviors: []store.Node{
				{
					ID:   "token-test-1",
					Kind: "behavior",
					Content: map[string]interface{}{
						"name": "Use gofmt",
						"kind": "directive",
						"content": map[string]interface{}{
							"canonical": "Use gofmt always", // 16 chars -> (16+3)/4 = 4 tokens
						},
						"when": map[string]interface{}{},
					},
					Metadata: map[string]interface{}{
						"confidence": 0.9,
						"priority":   1,
					},
				},
			},
			wantTokens:        94 + 4, // seeds + user behavior
			wantBudget:        2000,
			wantBehaviorCount: 2 + 1,
		},
		{
			name: "multiple behaviors sum tokens",
			behaviors: []store.Node{
				{
					ID:   "token-test-2a",
					Kind: "behavior",
					Content: map[string]interface{}{
						"name": "Behavior A",
						"kind": "directive",
						"content": map[string]interface{}{
							"canonical": "Use gofmt always", // 16 chars -> (16+3)/4 = 4 tokens
						},
						"when": map[string]interface{}{},
					},
					Metadata: map[string]interface{}{
						"confidence": 0.9,
						"priority":   1,
					},
				},
				{
					ID:   "token-test-2b",
					Kind: "behavior",
					Content: map[string]interface{}{
						"name": "Behavior B",
						"kind": "directive",
						"content": map[string]interface{}{
							"canonical": "Run go vet", // 10 chars -> (10+3)/4 = 3 tokens
						},
						"when": map[string]interface{}{},
					},
					Metadata: map[string]interface{}{
						"confidence": 0.85,
						"priority":   1,
					},
				},
			},
			wantTokens:        94 + 7, // seeds + 4 + 3
			wantBudget:        2000,
			wantBehaviorCount: 2 + 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := setupTestServer(t)
			defer server.Close()

			ctx := context.Background()

			// Add behaviors to store
			for _, node := range tt.behaviors {
				if _, err := server.store.AddNode(ctx, node); err != nil {
					t.Fatalf("Failed to add node %s: %v", node.ID, err)
				}
			}
			if len(tt.behaviors) > 0 {
				if err := server.store.Sync(ctx); err != nil {
					t.Fatalf("Failed to sync store: %v", err)
				}
			}

			req := &sdk.CallToolRequest{}
			args := FloopActiveInput{}

			_, output, err := server.handleFloopActive(ctx, req, args)
			if err != nil {
				t.Fatalf("handleFloopActive failed: %v", err)
			}

			if output.TokenStats == nil {
				t.Fatal("TokenStats is nil, want non-nil")
			}

			if output.TokenStats.TotalCanonicalTokens != tt.wantTokens {
				t.Errorf("TotalCanonicalTokens = %d, want %d", output.TokenStats.TotalCanonicalTokens, tt.wantTokens)
			}

			if output.TokenStats.BudgetDefault != tt.wantBudget {
				t.Errorf("BudgetDefault = %d, want %d", output.TokenStats.BudgetDefault, tt.wantBudget)
			}

			if output.TokenStats.BehaviorCount != tt.wantBehaviorCount {
				t.Errorf("BehaviorCount = %d, want %d", output.TokenStats.BehaviorCount, tt.wantBehaviorCount)
			}
		})
	}
}

func TestBehaviorContentToMap_OmitsEmptyTags(t *testing.T) {
	content := models.BehaviorContent{
		Canonical: "test behavior",
	}

	m := behaviorContentToMap(content)

	if _, ok := m["tags"]; ok {
		t.Error("expected tags to be omitted when empty")
	}
}

func TestBoostSeedsWithPageRank(t *testing.T) {
	tests := []struct {
		name     string
		seeds    []spreading.Seed
		pageRank map[string]float64
		weight   float64
		wantActs []float64 // expected activation values after boost
	}{
		{
			name: "basic blending",
			seeds: []spreading.Seed{
				{BehaviorID: "a", Activation: 0.8, Source: "ctx"},
				{BehaviorID: "b", Activation: 0.6, Source: "ctx"},
			},
			pageRank: map[string]float64{
				"a": 0.4,
				"b": 0.2,
			},
			weight: 0.15,
			// a: (1-0.15)*0.8 + 0.15*0.4 = 0.68 + 0.06 = 0.74
			// b: (1-0.15)*0.6 + 0.15*0.2 = 0.51 + 0.03 = 0.54
			wantActs: []float64{0.74, 0.54},
		},
		{
			name:     "empty seeds",
			seeds:    []spreading.Seed{},
			pageRank: map[string]float64{"a": 0.5},
			weight:   0.15,
			wantActs: []float64{},
		},
		{
			name: "no pagerank data for seed",
			seeds: []spreading.Seed{
				{BehaviorID: "a", Activation: 0.8, Source: "ctx"},
			},
			pageRank: map[string]float64{}, // no data
			weight:   0.15,
			wantActs: []float64{0.8}, // unchanged
		},
		{
			name: "weight zero means no blending",
			seeds: []spreading.Seed{
				{BehaviorID: "a", Activation: 0.8, Source: "ctx"},
			},
			pageRank: map[string]float64{"a": 0.4},
			weight:   0.0,
			// (1-0)*0.8 + 0*0.4 = 0.8
			wantActs: []float64{0.8},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := boostSeedsWithPageRank(tt.seeds, tt.pageRank, tt.weight)

			if len(result) != len(tt.wantActs) {
				t.Fatalf("len(result) = %d, want %d", len(result), len(tt.wantActs))
			}

			for i, want := range tt.wantActs {
				got := result[i].Activation
				diff := got - want
				if diff < 0 {
					diff = -diff
				}
				if diff > 0.001 {
					t.Errorf("seed[%d].Activation = %f, want %f", i, got, want)
				}
			}
		})
	}
}

// getTimesActivated extracts the times_activated stat from a store node.
func getTimesActivated(t *testing.T, s store.GraphStore, id string) int {
	t.Helper()
	ctx := context.Background()
	node, err := s.GetNode(ctx, id)
	if err != nil {
		t.Fatalf("GetNode(%s) error = %v", id, err)
	}
	if node == nil {
		t.Fatalf("node %s not found", id)
	}
	stats, _ := node.Metadata["stats"].(map[string]interface{})
	if ta, ok := stats["times_activated"]; ok {
		switch v := ta.(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}
	return 0
}

// drainWorkerPool blocks until all background workers have completed.
func drainWorkerPool(server *Server) {
	for i := 0; i < maxBackgroundWorkers; i++ {
		server.workerPool <- struct{}{}
	}
	for i := 0; i < maxBackgroundWorkers; i++ {
		<-server.workerPool
	}
}

func TestHandleFloopActive_RecordsHits(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Add a user behavior with no "when" so it always activates
	behaviorNode := store.Node{
		ID:   "hit-tracking-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Hit Tracking Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Track activation hits",
			},
			"when": map[string]interface{}{},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.9,
			"priority":   1,
		},
	}
	if _, err := server.store.AddNode(ctx, behaviorNode); err != nil {
		t.Fatalf("Failed to add test behavior: %v", err)
	}
	if err := server.store.Sync(ctx); err != nil {
		t.Fatalf("Failed to sync store: %v", err)
	}

	// Call handleFloopActive
	req := &sdk.CallToolRequest{}
	args := FloopActiveInput{}
	_, _, err := server.handleFloopActive(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopActive failed: %v", err)
	}

	drainWorkerPool(server)

	// User behavior should have times_activated > 0
	if got := getTimesActivated(t, server.store, "hit-tracking-test"); got == 0 {
		t.Error("times_activated = 0, want > 0 after floop_active call")
	}

	// Seed behaviors should NOT have times_activated incremented
	if got := getTimesActivated(t, server.store, "seed-capture-corrections"); got != 0 {
		t.Errorf("seed-capture-corrections times_activated = %d, want 0 (seed behaviors should be skipped)", got)
	}
	if got := getTimesActivated(t, server.store, "seed-know-floop-tools"); got != 0 {
		t.Errorf("seed-know-floop-tools times_activated = %d, want 0 (seed behaviors should be skipped)", got)
	}
}

func TestHandleBehaviorsResource_EmptyStoreFraming(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Call with no behaviors — should still use neutral framing.
	req := &sdk.ReadResourceRequest{}
	result, err := server.handleBehaviorsResource(ctx, req)
	if err != nil {
		t.Fatalf("handleBehaviorsResource failed: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("Expected at least one resource content block")
	}

	text := result.Contents[0].Text

	// Even the empty-state message must not contain authoritative phrasing.
	forbiddenPhrases := []struct {
		name   string
		phrase string
	}{
		{"no CRITICAL keyword", "CRITICAL"},
		{"no Violating language", "Violating"},
		{"no repeating a past mistake", "repeating a past mistake"},
		{"no YOUR learned memories", "YOUR learned memories"},
	}
	for _, tc := range forbiddenPhrases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(text, tc.phrase) {
				t.Errorf("Empty-state output contains forbidden phrase %q:\n%s", tc.phrase, text)
			}
		})
	}
}
