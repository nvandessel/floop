package backup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/store"
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
		Kind:      store.EdgeKindRequires,
		Weight:    0.9,
		CreatedAt: now,
	})
	s.AddEdge(ctx, store.Edge{
		Source:    "node-b",
		Target:    "node-c",
		Kind:      store.EdgeKindSimilarTo,
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
	backupPath := filepath.Join(t.TempDir(), "test-backup.json.gz")

	// Backup
	backup, err := Backup(ctx, srcStore, backupPath)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	if backup.Version != 2 {
		t.Errorf("Version = %d, want 2", backup.Version)
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
		os.WriteFile(path, []byte("{}"), 0600)
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
	if !strings.HasSuffix(path, ".json.gz") {
		t.Errorf("path = %s, want .json.gz suffix", path)
	}
}

func TestGenerateBackupPathV1(t *testing.T) {
	dir := "/tmp/backups"
	path := GenerateBackupPathV1(dir)

	if filepath.Dir(path) != dir {
		t.Errorf("dir = %s, want %s", filepath.Dir(path), dir)
	}
	if filepath.Ext(path) != ".json" {
		t.Errorf("ext = %s, want .json", filepath.Ext(path))
	}
}

func TestBackup_DefaultsToV2(t *testing.T) {
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	dir := t.TempDir()
	backupPath := filepath.Join(dir, "test-backup.json.gz")

	result, err := Backup(ctx, srcStore, backupPath)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	if result.Version != FormatV2 {
		t.Errorf("Version = %d, want %d (V2)", result.Version, FormatV2)
	}

	// Verify file is V2 format
	version, err := DetectFormat(backupPath)
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if version != FormatV2 {
		t.Errorf("DetectFormat() = %d, want %d", version, FormatV2)
	}
}

func TestRestore_V1BackwardCompat(t *testing.T) {
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	dir := t.TempDir()

	// Create a V1 backup explicitly
	v1Path := filepath.Join(dir, "v1-backup.json")
	_, err := BackupWithOptions(ctx, srcStore, v1Path, BackupOptions{Compress: false})
	if err != nil {
		t.Fatalf("BackupWithOptions(compress=false) error = %v", err)
	}

	// Restore V1 file using the auto-detecting Restore
	dstStore := createTestStore(t)
	defer dstStore.Close()

	result, err := Restore(ctx, dstStore, v1Path, RestoreMerge)
	if err != nil {
		t.Fatalf("Restore(V1) error = %v", err)
	}

	if result.NodesRestored != 3 {
		t.Errorf("NodesRestored = %d, want 3", result.NodesRestored)
	}
	if result.EdgesRestored != 2 {
		t.Errorf("EdgesRestored = %d, want 2", result.EdgesRestored)
	}
}

func TestRotateBackups_MixedFormats(t *testing.T) {
	dir := t.TempDir()

	// Create a mix of V1 and V2 backup files
	files := []string{
		"floop-backup-20260201-120000.json",
		"floop-backup-20260202-120000.json.gz",
		"floop-backup-20260203-120000.json",
		"floop-backup-20260204-120000.json.gz",
		"floop-backup-20260205-120000.json.gz",
	}
	for _, name := range files {
		os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0600)
	}

	if err := RotateBackups(dir, 3); err != nil {
		t.Fatalf("RotateBackups() error = %v", err)
	}

	entries, _ := os.ReadDir(dir)
	count := 0
	for _, e := range entries {
		if isBackupFile(e.Name()) {
			count++
		}
	}

	if count != 3 {
		t.Errorf("after rotation, got %d files, want 3", count)
	}
}

// writeV2WithSchemaVersion creates a V2 backup file with an overridden schema version in the header.
// This is a test helper for schema version validation tests.
func writeV2WithSchemaVersion(t *testing.T, path string, schemaVersion int) {
	t.Helper()

	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "test-node", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}

	// First write a normal V2 backup
	if err := WriteV2(path, bf, nil); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}

	// Now rewrite the header with the desired schema version
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	reader := bufio.NewReader(f)
	headerLine, err := reader.ReadBytes('\n')
	if err != nil {
		f.Close()
		t.Fatalf("ReadBytes() error = %v", err)
	}

	// Read the rest (compressed payload)
	payload, err := io.ReadAll(reader)
	if err != nil {
		f.Close()
		t.Fatalf("ReadAll() error = %v", err)
	}
	f.Close()

	// Parse and modify header
	var header BackupHeader
	if err := json.Unmarshal(bytes.TrimSpace(headerLine), &header); err != nil {
		t.Fatalf("Unmarshal header error = %v", err)
	}
	header.SchemaVersion = schemaVersion

	newHeaderBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("Marshal header error = %v", err)
	}

	// Write modified file
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer out.Close()

	out.Write(newHeaderBytes)
	out.Write([]byte("\n"))
	out.Write(payload)
}

func TestRestoreSchemaVersionTooNew(t *testing.T) {
	ctx := context.Background()
	dstStore := createTestStore(t)
	defer dstStore.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "too-new.json.gz")

	// Write a backup with schema version 999 (much newer than current)
	writeV2WithSchemaVersion(t, path, 999)

	_, err := Restore(ctx, dstStore, path, RestoreMerge)
	if err == nil {
		t.Fatal("Restore() should fail for schema version too new")
	}
	if !strings.Contains(err.Error(), "newer than current schema version") {
		t.Errorf("error = %v, want 'newer than current schema version'", err)
	}
}

func TestRestoreSchemaVersionOlder(t *testing.T) {
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "older.json.gz")

	// Create a normal backup first
	_, err := Backup(ctx, srcStore, path)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}

	// Rewrite with older schema version
	writeV2WithSchemaVersion(t, path, 5)

	// Restore to a new store — should succeed with a warning (on stderr)
	dstStore := createTestStore(t)
	defer dstStore.Close()

	// Note: the checksum won't match because we modified the header,
	// so we write a fresh backup with overridden schema version.
	freshPath := filepath.Join(dir, "older-fresh.json.gz")
	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "test-node", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}
	if err := WriteV2(freshPath, bf, nil); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}
	writeV2WithSchemaVersion(t, freshPath, 5)

	// The restore should succeed (schema version 5 is older but valid)
	// but the checksum will fail because we modified the header after writing.
	// Let me take a different approach: directly test checkSchemaVersion.
	err = checkSchemaVersion(freshPath)
	if err != nil {
		t.Errorf("checkSchemaVersion() should not error for older version, got: %v", err)
	}
}

func TestRestoreSchemaVersionZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zero-schema.json.gz")

	// Write a backup with schema_version=0 (simulating old format without schema version)
	writeV2WithSchemaVersion(t, path, 0)

	// checkSchemaVersion should return nil silently
	err := checkSchemaVersion(path)
	if err != nil {
		t.Errorf("checkSchemaVersion() should be silent for schema_version=0, got: %v", err)
	}
}

func TestRestoreSchemaVersionTooNew_Exact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "too-new.json.gz")

	writeV2WithSchemaVersion(t, path, store.SchemaVersion+1)

	err := checkSchemaVersion(path)
	if err == nil {
		t.Fatal("checkSchemaVersion() should error for schema version > store.SchemaVersion")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("backup schema version %d is newer", store.SchemaVersion+1)) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRestoreSchemaVersionCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "current.json.gz")

	writeV2WithSchemaVersion(t, path, store.SchemaVersion)

	err := checkSchemaVersion(path)
	if err != nil {
		t.Errorf("checkSchemaVersion() should not error for current schema version, got: %v", err)
	}
}

func TestBackupOptions_AllowedDirs(t *testing.T) {
	srcStore := createTestStore(t)
	defer srcStore.Close()
	addTestData(t, srcStore)

	ctx := context.Background()
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()

	tests := []struct {
		name    string
		path    string
		opts    BackupOptions
		wantErr bool
	}{
		{
			name: "valid path inside allowed dir",
			path: filepath.Join(allowedDir, "backup.json.gz"),
			opts: BackupOptions{
				Compress:    true,
				AllowedDirs: []string{allowedDir},
			},
			wantErr: false,
		},
		{
			name: "path outside allowed dir is rejected",
			path: filepath.Join(outsideDir, "backup.json.gz"),
			opts: BackupOptions{
				Compress:    true,
				AllowedDirs: []string{allowedDir},
			},
			wantErr: true,
		},
		{
			name: "nil AllowedDirs skips validation",
			path: filepath.Join(outsideDir, "backup-no-check.json.gz"),
			opts: BackupOptions{
				Compress: true,
			},
			wantErr: false,
		},
		{
			name: "empty AllowedDirs skips validation",
			path: filepath.Join(outsideDir, "backup-empty-check.json.gz"),
			opts: BackupOptions{
				Compress:    true,
				AllowedDirs: []string{},
			},
			wantErr: false,
		},
		{
			name: "FloopVersion is passed through",
			path: filepath.Join(allowedDir, "backup-versioned.json.gz"),
			opts: BackupOptions{
				Compress:     true,
				FloopVersion: "2.0.0-test",
				AllowedDirs:  []string{allowedDir},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BackupWithOptions(ctx, srcStore, tt.path, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupWithOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "path rejected") {
				t.Errorf("BackupWithOptions() error = %v, want 'path rejected' in message", err)
			}

			// Verify FloopVersion was passed through
			if !tt.wantErr && tt.opts.FloopVersion != "" && tt.opts.Compress {
				header, err := ReadV2Header(tt.path)
				if err != nil {
					t.Fatalf("ReadV2Header() error = %v", err)
				}
				if v := header.Metadata["floop_version"]; v != tt.opts.FloopVersion {
					t.Errorf("floop_version = %q, want %q", v, tt.opts.FloopVersion)
				}
			}
		})
	}
}

func TestCheckSchemaVersion_V1File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1-backup.json")

	// Write a V1 backup
	v1 := &BackupFormat{
		Version:   1,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "a", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}
	data, err := json.MarshalIndent(v1, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	// checkSchemaVersion should silently pass for V1 files
	err = checkSchemaVersion(path)
	if err != nil {
		t.Errorf("checkSchemaVersion() should be silent for V1 files, got: %v", err)
	}
}
