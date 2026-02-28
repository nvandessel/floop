package pack

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/backup"
	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// writeTestPack writes a test pack file with the given nodes and edges.
func writeTestPack(t *testing.T, dir string, nodes []store.Node, edges []store.Edge, manifest PackManifest) string {
	t.Helper()
	packPath := filepath.Join(dir, "test.fpack")

	backupNodes := make([]backup.BackupNode, len(nodes))
	for i, n := range nodes {
		backupNodes[i] = backup.BackupNode{Node: n}
	}

	data := &backup.BackupFormat{
		Version:   backup.FormatV2,
		CreatedAt: time.Now(),
		Nodes:     backupNodes,
		Edges:     edges,
	}

	if err := WritePackFile(packPath, data, manifest, nil); err != nil {
		t.Fatalf("WritePackFile() error = %v", err)
	}
	return packPath
}

func TestInstall_NewBehaviors(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()
	cfg := config.Default()
	tmpDir := t.TempDir()

	nodes := []store.Node{
		{
			ID:   "b-new-1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": "test-behavior-1",
				"kind": "directive",
			},
			Metadata: map[string]interface{}{},
		},
		{
			ID:   "b-new-2",
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": "test-behavior-2",
				"kind": "preference",
			},
			Metadata: map[string]interface{}{},
		},
	}

	manifest := PackManifest{
		ID:      "test-org/new-pack",
		Version: "1.0.0",
	}

	packPath := writeTestPack(t, tmpDir, nodes, nil, manifest)

	result, err := Install(ctx, s, packPath, cfg, InstallOptions{})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if len(result.Added) != 2 {
		t.Errorf("Added = %d, want 2", len(result.Added))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("Skipped = %d, want 0", len(result.Skipped))
	}
	if len(result.Updated) != 0 {
		t.Errorf("Updated = %d, want 0", len(result.Updated))
	}
	if result.PackID != "test-org/new-pack" {
		t.Errorf("PackID = %q, want %q", result.PackID, "test-org/new-pack")
	}

	// Verify nodes are in store
	n, _ := s.GetNode(ctx, "b-new-1")
	if n == nil {
		t.Error("expected b-new-1 to be in store")
	}
}

func TestInstall_Upgrade(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()
	cfg := config.Default()
	tmpDir := t.TempDir()

	// Pre-install a behavior with older version
	s.AddNode(ctx, store.Node{
		ID:   "b-upgrade-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "old-behavior",
			"kind": "directive",
		},
		Metadata: map[string]interface{}{
			"provenance": map[string]interface{}{
				"package":         "test-org/upgrade-pack",
				"package_version": "0.9.0",
			},
		},
	})

	// Install a newer version
	nodes := []store.Node{
		{
			ID:   "b-upgrade-1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": "updated-behavior",
				"kind": "directive",
			},
			Metadata: map[string]interface{}{},
		},
	}

	manifest := PackManifest{
		ID:      "test-org/upgrade-pack",
		Version: "1.0.0",
	}

	packPath := writeTestPack(t, tmpDir, nodes, nil, manifest)

	result, err := Install(ctx, s, packPath, cfg, InstallOptions{})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if len(result.Updated) != 1 {
		t.Errorf("Updated = %d, want 1", len(result.Updated))
	}
	if len(result.Added) != 0 {
		t.Errorf("Added = %d, want 0", len(result.Added))
	}

	// Verify the content was updated
	n, _ := s.GetNode(ctx, "b-upgrade-1")
	if n == nil {
		t.Fatal("expected b-upgrade-1 to be in store")
	}
	name, _ := n.Content["name"].(string)
	if name != "updated-behavior" {
		t.Errorf("name = %q, want %q", name, "updated-behavior")
	}
}

func TestInstall_RespectForgotten(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()
	cfg := config.Default()
	tmpDir := t.TempDir()

	// Pre-install a forgotten behavior
	s.AddNode(ctx, store.Node{
		ID:   "b-forgotten-1",
		Kind: string(models.BehaviorKindForgotten),
		Content: map[string]interface{}{
			"name": "forgotten-behavior",
			"kind": "directive",
		},
		Metadata: map[string]interface{}{},
	})

	nodes := []store.Node{
		{
			ID:   "b-forgotten-1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": "revived-behavior",
				"kind": "directive",
			},
			Metadata: map[string]interface{}{},
		},
	}

	manifest := PackManifest{
		ID:      "test-org/forgotten-pack",
		Version: "1.0.0",
	}

	packPath := writeTestPack(t, tmpDir, nodes, nil, manifest)

	result, err := Install(ctx, s, packPath, cfg, InstallOptions{})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1", len(result.Skipped))
	}

	// Verify the behavior is still forgotten
	n, _ := s.GetNode(ctx, "b-forgotten-1")
	if n == nil {
		t.Fatal("expected b-forgotten-1 to be in store")
	}
	if n.Kind != string(models.BehaviorKindForgotten) {
		t.Errorf("Kind = %q, want %q", n.Kind, string(models.BehaviorKindForgotten))
	}
}

func TestInstall_Idempotent(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()
	cfg := config.Default()
	tmpDir := t.TempDir()

	// Pre-install a behavior with same version
	s.AddNode(ctx, store.Node{
		ID:   "b-idem-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "existing-behavior",
			"kind": "directive",
		},
		Metadata: map[string]interface{}{
			"provenance": map[string]interface{}{
				"package":         "test-org/idem-pack",
				"package_version": "1.0.0",
			},
		},
	})

	nodes := []store.Node{
		{
			ID:   "b-idem-1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": "existing-behavior",
				"kind": "directive",
			},
			Metadata: map[string]interface{}{},
		},
	}

	manifest := PackManifest{
		ID:      "test-org/idem-pack",
		Version: "1.0.0",
	}

	packPath := writeTestPack(t, tmpDir, nodes, nil, manifest)

	result, err := Install(ctx, s, packPath, cfg, InstallOptions{})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1", len(result.Skipped))
	}
	if len(result.Added) != 0 {
		t.Errorf("Added = %d, want 0", len(result.Added))
	}
	if len(result.Updated) != 0 {
		t.Errorf("Updated = %d, want 0", len(result.Updated))
	}
}

func TestInstall_StampsProvenance(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()
	cfg := config.Default()
	tmpDir := t.TempDir()

	nodes := []store.Node{
		{
			ID:   "b-prov-1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": "provenance-test",
				"kind": "directive",
			},
			Metadata: map[string]interface{}{},
		},
	}

	manifest := PackManifest{
		ID:      "test-org/prov-pack",
		Version: "2.0.0",
	}

	packPath := writeTestPack(t, tmpDir, nodes, nil, manifest)

	_, err := Install(ctx, s, packPath, cfg, InstallOptions{})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	// Verify provenance is stamped
	n, _ := s.GetNode(ctx, "b-prov-1")
	if n == nil {
		t.Fatal("expected b-prov-1 to be in store")
	}

	pkgName := models.ExtractPackageName(n.Metadata)
	if pkgName != "test-org/prov-pack" {
		t.Errorf("package = %q, want %q", pkgName, "test-org/prov-pack")
	}
	pkgVer := models.ExtractPackageVersion(n.Metadata)
	if pkgVer != "2.0.0" {
		t.Errorf("package_version = %q, want %q", pkgVer, "2.0.0")
	}
}

func TestInstall_EdgesInstalled(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()
	cfg := config.Default()
	tmpDir := t.TempDir()

	nodes := []store.Node{
		{
			ID:   "b-edge-1",
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": "edge-source",
				"kind": "directive",
			},
			Metadata: map[string]interface{}{},
		},
		{
			ID:   "b-edge-2",
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": "edge-target",
				"kind": "directive",
			},
			Metadata: map[string]interface{}{},
		},
	}

	edges := []store.Edge{
		{
			Source:    "b-edge-1",
			Target:    "b-edge-2",
			Kind:      store.EdgeKindSimilarTo,
			Weight:    0.8,
			CreatedAt: time.Now(),
		},
	}

	manifest := PackManifest{
		ID:      "test-org/edge-pack",
		Version: "1.0.0",
	}

	packPath := writeTestPack(t, tmpDir, nodes, edges, manifest)

	result, err := Install(ctx, s, packPath, cfg, InstallOptions{})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if result.EdgesAdded != 1 {
		t.Errorf("EdgesAdded = %d, want 1", result.EdgesAdded)
	}

	// Verify edge is in store
	foundEdges, _ := s.GetEdges(ctx, "b-edge-1", store.DirectionOutbound, "similar-to")
	if len(foundEdges) != 1 {
		t.Errorf("found %d edges, want 1", len(foundEdges))
	}
}
