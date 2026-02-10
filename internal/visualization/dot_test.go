package visualization

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/store"
)

func setupTestStore(t *testing.T) store.GraphStore {
	t.Helper()
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("create floop dir: %v", err)
	}

	gs, err := store.NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { gs.Close() })
	return gs
}

func addBehavior(t *testing.T, gs store.GraphStore, id, name, kind string, confidence float64) {
	t.Helper()
	ctx := context.Background()
	node := store.Node{
		ID:   id,
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": name,
			"kind": kind,
			"content": map[string]interface{}{
				"canonical": "Test: " + name,
			},
			"provenance": map[string]interface{}{
				"source_type": "manual",
			},
		},
		Metadata: map[string]interface{}{
			"confidence": confidence,
			"priority":   0,
			"scope":      "local",
		},
	}
	if _, err := gs.AddNode(ctx, node); err != nil {
		t.Fatalf("add node %s: %v", id, err)
	}
}

func TestRenderDOT_EmptyStore(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	dot, err := RenderDOT(ctx, gs)
	if err != nil {
		t.Fatalf("RenderDOT: %v", err)
	}

	if !strings.Contains(dot, "digraph floop") {
		t.Error("expected digraph header")
	}
	if !strings.HasSuffix(strings.TrimSpace(dot), "}") {
		t.Error("expected closing brace")
	}
}

func TestRenderDOT_WithNodes(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)
	addBehavior(t, gs, "b2", "tdd-workflow", "constraint", 0.9)

	dot, err := RenderDOT(ctx, gs)
	if err != nil {
		t.Fatalf("RenderDOT: %v", err)
	}

	if !strings.Contains(dot, `"b1"`) {
		t.Error("expected node b1")
	}
	if !strings.Contains(dot, `"b2"`) {
		t.Error("expected node b2")
	}
	if !strings.Contains(dot, "steelblue") {
		t.Error("expected directive color steelblue")
	}
	if !strings.Contains(dot, "tomato") {
		t.Error("expected constraint color tomato")
	}
}

func TestRenderDOT_WithEdges(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)
	addBehavior(t, gs, "b2", "tdd-workflow", "constraint", 0.9)
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "requires", Weight: 0.8}); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	dot, err := RenderDOT(ctx, gs)
	if err != nil {
		t.Fatalf("RenderDOT: %v", err)
	}

	if !strings.Contains(dot, `"b1" -> "b2"`) {
		t.Error("expected edge b1 -> b2")
	}
	if !strings.Contains(dot, "requires") {
		t.Error("expected edge label 'requires'")
	}
}

func TestRenderJSON_WithNodesAndEdges(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)
	addBehavior(t, gs, "b2", "tdd-workflow", "constraint", 0.9)
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "similar-to", Weight: 0.7}); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	result, err := RenderJSON(ctx, gs)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	nodeCount, ok := result["node_count"].(int)
	if !ok || nodeCount != 2 {
		t.Errorf("node_count = %v, want 2", result["node_count"])
	}
	edgeCount, ok := result["edge_count"].(int)
	if !ok || edgeCount != 1 {
		t.Errorf("edge_count = %v, want 1", result["edge_count"])
	}
}

func TestRenderEnrichedJSON_WithPageRank(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)
	addBehavior(t, gs, "b2", "tdd-workflow", "constraint", 0.9)

	enrichment := &EnrichmentData{
		PageRank: map[string]float64{
			"b1": 0.75,
			"b2": 1.0,
		},
	}

	result, err := RenderEnrichedJSON(ctx, gs, enrichment)
	if err != nil {
		t.Fatalf("RenderEnrichedJSON: %v", err)
	}

	nodes, ok := result["nodes"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected nodes to be []map[string]interface{}")
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	// Check that PageRank scores are present
	foundPR := false
	for _, node := range nodes {
		if id, ok := node["id"].(string); ok && id == "b1" {
			pr, ok := node["pagerank"].(float64)
			if !ok {
				t.Error("expected pagerank field on b1")
			} else if pr != 0.75 {
				t.Errorf("b1 pagerank = %v, want 0.75", pr)
			}
			foundPR = true
		}
	}
	if !foundPR {
		t.Error("did not find node b1 in result")
	}
}

func TestRenderEnrichedJSON_NilEnrichment(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	result, err := RenderEnrichedJSON(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderEnrichedJSON: %v", err)
	}

	// Should still return nodes and edges
	nodeCount, ok := result["node_count"].(int)
	if !ok || nodeCount != 1 {
		t.Errorf("node_count = %v, want 1", result["node_count"])
	}

	// Should not have pagerank field
	nodes, ok := result["nodes"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected nodes to be []map[string]interface{}")
	}
	for _, node := range nodes {
		if _, hasPR := node["pagerank"]; hasPR {
			t.Error("expected no pagerank field when enrichment is nil")
		}
	}
}

func TestRenderEnrichedJSON_IncludesCanonical(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	result, err := RenderEnrichedJSON(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderEnrichedJSON: %v", err)
	}

	nodes, ok := result["nodes"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected nodes to be []map[string]interface{}")
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	canonical, ok := nodes[0]["canonical"].(string)
	if !ok {
		t.Fatal("expected canonical field on node")
	}
	if canonical != "Test: use-worktrees" {
		t.Errorf("canonical = %q, want %q", canonical, "Test: use-worktrees")
	}
}

func TestRenderHTML_ProducesValidHTML(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)
	addBehavior(t, gs, "b2", "tdd-workflow", "constraint", 0.9)
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "requires", Weight: 0.8}); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	enrichment := &EnrichmentData{
		PageRank: map[string]float64{"b1": 0.5, "b2": 1.0},
	}

	html, err := RenderHTML(ctx, gs, enrichment)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}

	htmlStr := string(html)

	// Check basic HTML structure
	if !strings.Contains(htmlStr, "<!DOCTYPE html>") {
		t.Error("expected DOCTYPE declaration")
	}
	if !strings.Contains(htmlStr, "<title>floop") {
		t.Error("expected floop title")
	}
	if !strings.Contains(htmlStr, "ForceGraph") {
		t.Error("expected ForceGraph library to be embedded")
	}

	// Check that graph data is embedded
	if !strings.Contains(htmlStr, "use-worktrees") {
		t.Error("expected node name 'use-worktrees' in HTML")
	}
	if !strings.Contains(htmlStr, "tdd-workflow") {
		t.Error("expected node name 'tdd-workflow' in HTML")
	}

	// Reasonable size (should be > 100KB because of embedded JS)
	if len(html) < 100000 {
		t.Errorf("HTML too small (%d bytes), expected > 100KB with embedded JS", len(html))
	}
}

func TestRenderHTML_EmptyStore(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	html, err := RenderHTML(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}

	htmlStr := string(html)

	if !strings.Contains(htmlStr, "<!DOCTYPE html>") {
		t.Error("expected DOCTYPE declaration")
	}
	if !strings.Contains(htmlStr, "ForceGraph") {
		t.Error("expected ForceGraph library even with empty store")
	}
}

func TestCollectEdges(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)
	addBehavior(t, gs, "b2", "tdd-workflow", "constraint", 0.9)
	addBehavior(t, gs, "b3", "parallel-work", "procedure", 0.7)
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "requires", Weight: 0.8}); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	if err := gs.AddEdge(ctx, store.Edge{Source: "b2", Target: "b3", Kind: "similar-to", Weight: 0.5}); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	nodes, err := gs.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("query nodes: %v", err)
	}

	edges, err := collectEdges(ctx, gs, nodes)
	if err != nil {
		t.Fatalf("collectEdges: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world", 8, "hello..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
