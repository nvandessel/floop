package pack

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/store"
)

// makeTestStore creates an in-memory store with sample behaviors for testing.
func makeTestStore(t *testing.T) *store.InMemoryGraphStore {
	t.Helper()
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Behavior 1: go + testing tags, global scope, directive kind, in pack "test-org/go-pack"
	s.AddNode(ctx, store.Node{
		ID:   "b-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "use-go-test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Use go test for testing",
				"tags":      []interface{}{"go", "testing"},
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.9,
			"provenance": map[string]interface{}{
				"scope":   "global",
				"package": "test-org/go-pack",
			},
		},
	})

	// Behavior 2: python tag, local scope, preference kind, no pack
	s.AddNode(ctx, store.Node{
		ID:   "b-2",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "prefer-pathlib",
			"kind": "preference",
			"content": map[string]interface{}{
				"canonical": "Prefer pathlib over os.path",
				"tags":      []interface{}{"python"},
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.8,
			"provenance": map[string]interface{}{
				"scope": "local",
			},
		},
	})

	// Behavior 3: go tag, global scope, constraint kind, in pack "test-org/go-pack"
	s.AddNode(ctx, store.Node{
		ID:   "b-3",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "no-panic",
			"kind": "constraint",
			"content": map[string]interface{}{
				"canonical": "Never use panic in production",
				"tags":      []interface{}{"go"},
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.95,
			"provenance": map[string]interface{}{
				"scope":   "global",
				"package": "test-org/go-pack",
			},
		},
	})

	// Edge between b-1 and b-3 (both go-related)
	s.AddEdge(ctx, store.Edge{
		Source:    "b-1",
		Target:    "b-3",
		Kind:      store.EdgeKindSimilarTo,
		Weight:    0.8,
		CreatedAt: time.Now(),
	})

	// Edge between b-1 and b-2 (cross-language)
	s.AddEdge(ctx, store.Edge{
		Source:    "b-1",
		Target:    "b-2",
		Kind:      store.EdgeKindSimilarTo,
		Weight:    0.5,
		CreatedAt: time.Now(),
	})

	return s
}

func TestCreate_AllBehaviors(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "all.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/all-pack",
		Version: "1.0.0",
	}

	result, err := Create(ctx, s, CreateFilter{}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.BehaviorCount != 3 {
		t.Errorf("BehaviorCount = %d, want 3", result.BehaviorCount)
	}
	if result.EdgeCount != 2 {
		t.Errorf("EdgeCount = %d, want 2", result.EdgeCount)
	}
	if result.Path != outputPath {
		t.Errorf("Path = %q, want %q", result.Path, outputPath)
	}
}

func TestCreate_TagFilter(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "go-only.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/go-pack",
		Version: "1.0.0",
	}

	result, err := Create(ctx, s, CreateFilter{
		Tags: []string{"go"},
	}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// b-1 and b-3 have "go" tag
	if result.BehaviorCount != 2 {
		t.Errorf("BehaviorCount = %d, want 2", result.BehaviorCount)
	}
}

func TestCreate_ScopeFilter(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "global-only.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/global-pack",
		Version: "1.0.0",
	}

	result, err := Create(ctx, s, CreateFilter{
		Scope: "global",
	}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// b-1 and b-3 are global scope
	if result.BehaviorCount != 2 {
		t.Errorf("BehaviorCount = %d, want 2", result.BehaviorCount)
	}
}

func TestCreate_KindFilter(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "directive-only.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/directive-pack",
		Version: "1.0.0",
	}

	result, err := Create(ctx, s, CreateFilter{
		Kinds: []string{"directive"},
	}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Only b-1 is a directive
	if result.BehaviorCount != 1 {
		t.Errorf("BehaviorCount = %d, want 1", result.BehaviorCount)
	}
}

func TestCreate_EdgesFollowNodes(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "edges.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/edges-pack",
		Version: "1.0.0",
	}

	// Filter to only go-tagged behaviors (b-1 and b-3)
	// Edge b-1->b-3 should be included (both pass filter)
	// Edge b-1->b-2 should be excluded (b-2 doesn't pass filter)
	result, err := Create(ctx, s, CreateFilter{
		Tags: []string{"go"},
	}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.BehaviorCount != 2 {
		t.Errorf("BehaviorCount = %d, want 2", result.BehaviorCount)
	}
	if result.EdgeCount != 1 {
		t.Errorf("EdgeCount = %d, want 1 (only edge where both endpoints pass filter)", result.EdgeCount)
	}
}

func TestCreate_EmptyResult(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "empty.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/empty-pack",
		Version: "1.0.0",
	}

	// Filter for a tag that no behavior has
	result, err := Create(ctx, s, CreateFilter{
		Tags: []string{"nonexistent-tag"},
	}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.BehaviorCount != 0 {
		t.Errorf("BehaviorCount = %d, want 0", result.BehaviorCount)
	}
	if result.EdgeCount != 0 {
		t.Errorf("EdgeCount = %d, want 0", result.EdgeCount)
	}

	// Verify the pack file is valid and readable
	_, readManifest, err := ReadPackFile(outputPath)
	if err != nil {
		t.Fatalf("ReadPackFile() error = %v", err)
	}
	if readManifest.ID != manifest.ID {
		t.Errorf("readManifest.ID = %q, want %q", readManifest.ID, manifest.ID)
	}
}

func TestCreate_FromPackFilter(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "from-pack.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/export",
		Version: "1.0.0",
	}

	// Only include behaviors from test-org/go-pack (b-1 and b-3)
	result, err := Create(ctx, s, CreateFilter{
		FromPack: "test-org/go-pack",
	}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.BehaviorCount != 2 {
		t.Errorf("BehaviorCount = %d, want 2", result.BehaviorCount)
	}
	// Edge b-1->b-3 should be included (both in pack), b-1->b-2 excluded
	if result.EdgeCount != 1 {
		t.Errorf("EdgeCount = %d, want 1", result.EdgeCount)
	}
}

func TestCreate_FromPackCombinedWithTags(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "combined.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/export",
		Version: "1.0.0",
	}

	// From pack + testing tag filter: only b-1 has both pack membership AND testing tag
	result, err := Create(ctx, s, CreateFilter{
		FromPack: "test-org/go-pack",
		Tags:     []string{"testing"},
	}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.BehaviorCount != 1 {
		t.Errorf("BehaviorCount = %d, want 1", result.BehaviorCount)
	}
}

func TestCreate_FromPackNoMatch(t *testing.T) {
	s := makeTestStore(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "no-match.fpack")
	ctx := context.Background()

	manifest := PackManifest{
		ID:      "test-org/export",
		Version: "1.0.0",
	}

	// No behaviors belong to this pack
	result, err := Create(ctx, s, CreateFilter{
		FromPack: "nonexistent/pack",
	}, manifest, outputPath, CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if result.BehaviorCount != 0 {
		t.Errorf("BehaviorCount = %d, want 0", result.BehaviorCount)
	}
}
