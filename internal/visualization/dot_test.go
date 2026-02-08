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
	if err := os.MkdirAll(floopDir, 0755); err != nil {
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
