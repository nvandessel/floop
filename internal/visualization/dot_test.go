package visualization

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "requires", Weight: 0.8, CreatedAt: time.Now()}); err != nil {
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
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "similar-to", Weight: 0.7, CreatedAt: time.Now()}); err != nil {
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
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "requires", Weight: 0.8, CreatedAt: time.Now()}); err != nil {
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
	if !strings.Contains(htmlStr, "data:text/javascript;base64,") {
		t.Error("expected force-graph library loaded via data URI")
	}

	// Check that graph data is embedded (HTML-escaped JSON)
	if !strings.Contains(htmlStr, "use-worktrees") {
		t.Error("expected node name 'use-worktrees' in HTML")
	}
	if !strings.Contains(htmlStr, "tdd-workflow") {
		t.Error("expected node name 'tdd-workflow' in HTML")
	}

	// HTML should be > 5KB (template + data URI + graph JSON)
	if len(html) < 5000 {
		t.Errorf("HTML too small (%d bytes), expected > 5KB", len(html))
	}
}

func TestRenderHTML_GraphDataIsJSObject(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	html, err := RenderHTML(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}

	htmlStr := string(html)

	// Graph data must be embedded as a JS object literal, not a quoted string.
	// If template.HTML is used instead of template.JS, html/template double-encodes
	// the JSON inside <script> context, producing `var graphData = "{...}"` (string)
	// instead of `var graphData = {...}` (object).
	if strings.Contains(htmlStr, `var graphData = "`) {
		t.Error("graphData is a quoted string — should be an object literal (use template.JS, not template.HTML)")
	}
	if !strings.Contains(htmlStr, `var graphData = {`) {
		t.Error("expected graphData to be an object literal starting with '{'")
	}
}

func TestRenderHTML_ScriptBreakoutEscaped(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	// Add a behavior with </script> in its content to test XSS prevention
	node := store.Node{
		ID:   "b-xss",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "xss-test",
			"kind": "constraint",
			"content": map[string]interface{}{
				"canonical": `Try to break out: </script><script>alert(1)</script>`,
			},
			"provenance": map[string]interface{}{"source_type": "manual"},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.5,
			"priority":   0,
			"scope":      "local",
		},
	}
	if _, err := gs.AddNode(ctx, node); err != nil {
		t.Fatalf("add xss node: %v", err)
	}

	html, err := RenderHTML(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}

	htmlStr := string(html)

	// The literal </script> must NOT appear in the JSON data section.
	// json.HTMLEscape converts < to \u003c, preventing script breakout.
	dataIdx := strings.Index(htmlStr, "var graphData = ")
	if dataIdx == -1 {
		t.Fatal("could not find graphData in HTML")
	}
	// Check the section after graphData injection up to the closing </script>
	dataSection := htmlStr[dataIdx : dataIdx+len(htmlStr[dataIdx:])]
	endIdx := strings.Index(dataSection, ";\n")
	if endIdx > 0 {
		dataSection = dataSection[:endIdx]
	}

	if strings.Contains(dataSection, "</script>") {
		t.Error("raw </script> found in graphData — XSS vulnerability")
	}
	if !strings.Contains(dataSection, `\u003c/script\u003e`) {
		t.Error("expected </script> to be escaped as \\u003c/script\\u003e")
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
	if !strings.Contains(htmlStr, "data:text/javascript;base64,") {
		t.Error("expected force-graph library even with empty store")
	}
}

func TestCollectEdges(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)
	addBehavior(t, gs, "b2", "tdd-workflow", "constraint", 0.9)
	addBehavior(t, gs, "b3", "parallel-work", "procedure", 0.7)
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "requires", Weight: 0.8, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	if err := gs.AddEdge(ctx, store.Edge{Source: "b2", Target: "b3", Kind: "similar-to", Weight: 0.5, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	nodes, err := gs.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		t.Fatalf("query nodes: %v", err)
	}

	edges, err := CollectEdges(ctx, gs, nodes)
	if err != nil {
		t.Fatalf("CollectEdges: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
}

func addBehaviorWithScope(t *testing.T, gs store.GraphStore, id, name, kind string, confidence float64, scope string) {
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
			"scope":      scope,
		},
	}
	if _, err := gs.AddNode(ctx, node); err != nil {
		t.Fatalf("add node %s: %v", id, err)
	}
}

func TestRenderEnrichedJSON_IncludesNodeScope(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehaviorWithScope(t, gs, "b1", "local-behavior", "directive", 0.8, "local")
	addBehaviorWithScope(t, gs, "b2", "global-behavior", "constraint", 0.9, "global")

	result, err := RenderEnrichedJSON(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderEnrichedJSON: %v", err)
	}

	nodes, ok := result["nodes"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected nodes to be []map[string]interface{}")
	}

	scopeByID := map[string]string{}
	for _, node := range nodes {
		id := node["id"].(string)
		scope, ok := node["scope"].(string)
		if !ok {
			t.Errorf("node %s missing scope field", id)
			continue
		}
		scopeByID[id] = scope
	}

	if scopeByID["b1"] != "local" {
		t.Errorf("b1 scope = %q, want %q", scopeByID["b1"], "local")
	}
	if scopeByID["b2"] != "global" {
		t.Errorf("b2 scope = %q, want %q", scopeByID["b2"], "global")
	}
}

func TestRenderEnrichedJSON_NodeScopeDefaultsToLocal(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	// Add a node without explicit scope metadata — store defaults to "local"
	node := store.Node{
		ID:   "b-noscope",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "no-scope",
			"kind": "directive",
		},
		Metadata: map[string]interface{}{
			"confidence": 0.5,
		},
	}
	if _, err := gs.AddNode(ctx, node); err != nil {
		t.Fatalf("add node: %v", err)
	}

	result, err := RenderEnrichedJSON(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderEnrichedJSON: %v", err)
	}

	nodes := result["nodes"].([]map[string]interface{})
	scope, ok := nodes[0]["scope"].(string)
	if !ok {
		t.Fatal("expected scope field on node")
	}
	// SQLite schema defaults scope to "local" when not explicitly set
	if scope != "local" {
		t.Errorf("scope = %q, want %q", scope, "local")
	}
}

func TestRenderEnrichedJSON_IncludesEdgeScope(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehaviorWithScope(t, gs, "b1", "local-a", "directive", 0.8, "local")
	addBehaviorWithScope(t, gs, "b2", "local-b", "constraint", 0.9, "local")
	addBehaviorWithScope(t, gs, "b3", "global-a", "procedure", 0.7, "global")

	// local -> local = "local"
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b2", Kind: "requires", Weight: 0.8, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	// local -> global = "both"
	if err := gs.AddEdge(ctx, store.Edge{Source: "b1", Target: "b3", Kind: "similar-to", Weight: 0.5, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	result, err := RenderEnrichedJSON(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderEnrichedJSON: %v", err)
	}

	edges := result["edges"].([]map[string]interface{})
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(edges))
	}

	edgeScopeByTargets := map[string]string{}
	for _, e := range edges {
		target := e["target"].(string)
		scope, ok := e["scope"].(string)
		if !ok {
			t.Errorf("edge to %s missing scope field", target)
			continue
		}
		edgeScopeByTargets[target] = scope
	}

	if edgeScopeByTargets["b2"] != "local" {
		t.Errorf("edge to b2 scope = %q, want %q", edgeScopeByTargets["b2"], "local")
	}
	if edgeScopeByTargets["b3"] != "both" {
		t.Errorf("edge to b3 scope = %q, want %q", edgeScopeByTargets["b3"], "both")
	}
}

func TestRenderHTML_ElectricModeMarkers(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	enrichment := &EnrichmentData{
		PageRank: map[string]float64{"b1": 0.5},
	}

	html, err := RenderHTML(ctx, gs, enrichment)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	htmlStr := string(html)

	// Static mode (no API base URL) should have electric toolbar hidden by default
	if !strings.Contains(htmlStr, "electric-toolbar") {
		t.Error("expected electric-toolbar element in HTML")
	}
	if !strings.Contains(htmlStr, "electricActivate") {
		t.Error("expected electricActivate function in HTML")
	}
	if !strings.Contains(htmlStr, "__electricSim") {
		t.Error("expected __electricSim test helper in HTML")
	}
}

func TestRenderHTMLForServer_HasAPIBaseURL(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	enrichment := &EnrichmentData{
		PageRank: map[string]float64{"b1": 0.5},
	}

	html, err := RenderHTMLForServer(ctx, gs, enrichment, "http://localhost:9999")
	if err != nil {
		t.Fatalf("RenderHTMLForServer: %v", err)
	}
	htmlStr := string(html)

	// html/template JS-escapes the URL; check for key parts
	if !strings.Contains(htmlStr, "localhost") || !strings.Contains(htmlStr, "9999") {
		t.Error("expected API base URL components (localhost, 9999) in server-mode HTML")
	}
	// Electric mode should be enabled (toolbar not hidden)
	if !strings.Contains(htmlStr, "electric-toolbar") {
		t.Error("expected electric-toolbar in server-mode HTML")
	}
}

func TestRenderHTML_FPSDiagnostic(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	html, err := RenderHTML(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	htmlStr := string(html)

	if !strings.Contains(htmlStr, "__getElectricFPS") {
		t.Error("expected __getElectricFPS test helper in HTML")
	}
}

func TestRenderHTML_ProgressMemoization(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	html, err := RenderHTML(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	htmlStr := string(html)

	if !strings.Contains(htmlStr, "__testProgressMemoization") {
		t.Error("expected __testProgressMemoization test helper in HTML")
	}
}

func TestRenderHTML_NoShadowBlurInElectricMode(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	html, err := RenderHTML(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	htmlStr := string(html)

	// shadowBlur should not appear anywhere in the electric rendering sections.
	// It's the heaviest Canvas 2D compositing operation and was replaced with
	// gradient-only equivalents.
	if strings.Contains(htmlStr, "shadowBlur") {
		t.Error("shadowBlur found in HTML — should be eliminated from electric mode rendering")
	}
}

func TestRenderHTML_EdgeEnergyCacheHelper(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	html, err := RenderHTML(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	htmlStr := string(html)

	if !strings.Contains(htmlStr, "__testEdgeEnergyCache") {
		t.Error("expected __testEdgeEnergyCache test helper in HTML")
	}
}

func TestRenderHTML_AutoPauseRedrawDefault(t *testing.T) {
	gs := setupTestStore(t)
	ctx := context.Background()

	addBehavior(t, gs, "b1", "use-worktrees", "directive", 0.8)

	html, err := RenderHTML(ctx, gs, nil)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	htmlStr := string(html)

	// The default should be autoPauseRedraw(true) for idle CPU savings
	if !strings.Contains(htmlStr, ".autoPauseRedraw(true)") {
		t.Error("expected .autoPauseRedraw(true) as default in HTML")
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
