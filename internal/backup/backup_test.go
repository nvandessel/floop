package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/store"
)

func createTestStore(t *testing.T) *store.SQLiteGraphStore {
	t.Helper()
	tmpDir := t.TempDir()
	s, err := store.NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	return s
}

func addTestData(t *testing.T, s store.GraphStore) {
	t.Helper()
	ctx := context.Background()

	// Add nodes
	for _, id := range []string{"node-a", "node-b", "node-c"} {
		_, err := s.AddNode(ctx, store.Node{
			ID:   id,
			Kind: "behavior",
			Content: map[string]interface{}{
				"name": id,
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "Content for " + id,
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 0.8,
			},
		})
		if err != nil {
			t.Fatalf("AddNode(%s) error = %v", id, err)
		}
	}

	// Add edges
	now := time.Now()
	s.AddEdge(ctx, store.Edge{
		Source:    "node-a",
		Target:    "node-b",
		Kind:      "requires",
		Weight:    0.9,
		CreatedAt: now,
	})
	s.AddEdge(ctx, store.Edge{
		Source:    "node-b",
		Target:    "node-c",
		Kind:      "similar-to",
		Weight:    0.7,
		CreatedAt: now,
	})
}

func TestBackupRestore_RoundTrip(t *testing.T) {
	// Create source store with data
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	backupPath := filepath.Join(t.TempDir(), "test-backup.json")

	// Backup
	backup, err := Backup(ctx, srcStore, backupPath)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	if backup.Version != 1 {
		t.Errorf("Version = %d, want 1", backup.Version)
	}
	if len(backup.Nodes) != 3 {
		t.Errorf("Nodes = %d, want 3", len(backup.Nodes))
	}
	if len(backup.Edges) != 2 {
		t.Errorf("Edges = %d, want 2", len(backup.Edges))
	}

	// Verify file exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatal("backup file was not created")
	}

	// Restore to a new store
	dstStore := createTestStore(t)
	defer dstStore.Close()

	result, err := Restore(ctx, dstStore, backupPath, RestoreMerge)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	if result.NodesRestored != 3 {
		t.Errorf("NodesRestored = %d, want 3", result.NodesRestored)
	}
	if result.EdgesRestored != 2 {
		t.Errorf("EdgesRestored = %d, want 2", result.EdgesRestored)
	}

	// Verify data in destination store
	node, err := dstStore.GetNode(ctx, "node-a")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if node == nil {
		t.Fatal("node-a not found after restore")
	}

	edges, err := dstStore.GetEdges(ctx, "node-a", store.DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("edges from node-a = %d, want 1", len(edges))
	}
}

func TestRestore_MergeMode(t *testing.T) {
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	backupPath := filepath.Join(t.TempDir(), "test-backup.json")

	// Backup
	_, err := Backup(ctx, srcStore, backupPath)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	// Create destination with some existing data
	dstStore := createTestStore(t)
	defer dstStore.Close()

	dstStore.AddNode(ctx, store.Node{
		ID:   "node-a",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "existing-node-a",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Existing content",
			},
		},
	})

	// Restore in merge mode — node-a should be skipped
	result, err := Restore(ctx, dstStore, backupPath, RestoreMerge)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	if result.NodesSkipped != 1 {
		t.Errorf("NodesSkipped = %d, want 1", result.NodesSkipped)
	}
	if result.NodesRestored != 2 {
		t.Errorf("NodesRestored = %d, want 2", result.NodesRestored)
	}

	// Verify existing data was preserved
	node, _ := dstStore.GetNode(ctx, "node-a")
	if node.Content["name"] != "existing-node-a" {
		t.Errorf("existing node was overwritten in merge mode")
	}
}

func TestRotateBackups(t *testing.T) {
	dir := t.TempDir()

	// Create 5 backup files
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, "floop-backup-2026020"+string(rune('1'+i))+"-120000.json")
		os.WriteFile(path, []byte("{}"), 0644)
	}

	// Rotate to keep 3
	if err := RotateBackups(dir, 3); err != nil {
		t.Fatalf("RotateBackups() error = %v", err)
	}

	entries, _ := os.ReadDir(dir)
	jsonCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}

	if jsonCount != 3 {
		t.Errorf("after rotation, got %d files, want 3", jsonCount)
	}
}

func TestBackup_PathValidation(t *testing.T) {
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid path inside allowed dir",
			path:    filepath.Join(allowedDir, "backup.json"),
			wantErr: false,
		},
		{
			name:    "path outside allowed dir is rejected",
			path:    filepath.Join(outsideDir, "backup.json"),
			wantErr: true,
		},
		{
			name:    "path traversal is rejected",
			path:    filepath.Join(allowedDir, "..", "escape.json"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Backup(ctx, srcStore, tt.path, allowedDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("Backup() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "path rejected") {
				t.Errorf("Backup() error = %v, want 'path rejected' in message", err)
			}
		})
	}
}

func TestBackup_NoValidationWithoutAllowedDirs(t *testing.T) {
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	backupPath := filepath.Join(t.TempDir(), "backup.json")

	// No allowedDirs arg = no validation (backward compatible)
	_, err := Backup(ctx, srcStore, backupPath)
	if err != nil {
		t.Fatalf("Backup() without allowedDirs should not fail: %v", err)
	}
}

func TestRestore_PathValidation(t *testing.T) {
	// Create a valid backup file first
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	allowedDir := t.TempDir()
	backupPath := filepath.Join(allowedDir, "backup.json")

	_, err := Backup(ctx, srcStore, backupPath)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	outsideDir := t.TempDir()
	outsideBackup := filepath.Join(outsideDir, "backup.json")
	// Copy the backup to outside dir
	data, _ := os.ReadFile(backupPath)
	os.WriteFile(outsideBackup, data, 0600)

	dstStore := createTestStore(t)
	defer dstStore.Close()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid path inside allowed dir",
			path:    backupPath,
			wantErr: false,
		},
		{
			name:    "path outside allowed dir is rejected",
			path:    outsideBackup,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreStore := createTestStore(t)
			defer restoreStore.Close()
			_, err := Restore(ctx, restoreStore, tt.path, RestoreMerge, allowedDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("Restore() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "path rejected") {
				t.Errorf("Restore() error = %v, want 'path rejected' in message", err)
			}
		})
	}
}

func TestBackup_FilePermissions(t *testing.T) {
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "newdir", "backups")
	backupPath := filepath.Join(backupDir, "backup.json")

	_, err := Backup(ctx, srcStore, backupPath)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	// Verify directory permissions are 0700
	dirInfo, err := os.Stat(backupDir)
	if err != nil {
		t.Fatalf("Stat(backupDir) error = %v", err)
	}
	dirPerm := dirInfo.Mode().Perm()
	if dirPerm != 0700 {
		t.Errorf("backup dir permissions = %o, want 0700", dirPerm)
	}

	// Verify file permissions are 0600
	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("Stat(backupPath) error = %v", err)
	}
	filePerm := fileInfo.Mode().Perm()
	if filePerm != 0600 {
		t.Errorf("backup file permissions = %o, want 0600", filePerm)
	}
}

func TestRestore_OversizedFile(t *testing.T) {
	ctx := context.Background()
	dstStore := createTestStore(t)
	defer dstStore.Close()

	// Create a file that exceeds MaxRestoreFileSize
	oversizedPath := filepath.Join(t.TempDir(), "oversized-backup.json")
	f, err := os.Create(oversizedPath)
	if err != nil {
		t.Fatalf("Failed to create oversized file: %v", err)
	}

	// Write a valid JSON start but pad to exceed the limit
	// We write just over the limit to trigger the bounded read error
	f.WriteString(`{"version":1,"created_at":"2026-01-01T00:00:00Z","nodes":[`)
	// Write enough data to exceed MaxRestoreFileSize
	chunk := make([]byte, 1024*1024) // 1MB chunk
	for i := range chunk {
		chunk[i] = ' '
	}
	for i := 0; i < 55; i++ { // 55MB > 50MB limit
		f.Write(chunk)
	}
	f.WriteString(`],"edges":[]}`)
	f.Close()

	_, err = Restore(ctx, dstStore, oversizedPath, RestoreMerge)
	if err == nil {
		t.Error("expected error for oversized backup file")
	}
}

func TestGenerateBackupPath(t *testing.T) {
	dir := "/tmp/backups"
	path := GenerateBackupPath(dir)

	if filepath.Dir(path) != dir {
		t.Errorf("dir = %s, want %s", filepath.Dir(path), dir)
	}
	if filepath.Ext(path) != ".json" {
		t.Errorf("ext = %s, want .json", filepath.Ext(path))
	}
}
