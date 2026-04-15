package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

// mockS3 is a simple in-memory S3 mock for testing.
type mockS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMockS3() *mockS3 {
	return &mockS3{objects: make(map[string][]byte)}
}

func (m *mockS3) Upload(_ context.Context, key string, reader io.Reader, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.objects[key] = data
	return nil
}

func (m *mockS3) Download(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockS3) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok, nil
}

func (m *mockS3) PutJSON(_ context.Context, key string, v interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.objects[key] = data
	return nil
}

func (m *mockS3) GetJSON(_ context.Context, key string, v interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return fmt.Errorf("key %q not found", key)
	}
	return json.Unmarshal(data, v)
}

func TestGraphSyncer_Push_UploadsBackup(t *testing.T) {
	s3 := newMockS3()
	syncer := NewGraphSyncer(s3, "workstation", nil)

	ctx := context.Background()
	graphStore := newTestGraphStore(t)

	result, err := syncer.Push(ctx, graphStore, "", "1.0.0")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	// Verify backup was uploaded
	backupKey := "machines/workstation/graph/floop-backup.json.gz"
	exists, _ := s3.Exists(ctx, backupKey)
	if !exists {
		t.Error("backup was not uploaded to expected key")
	}

	_ = result
}

func TestGraphSyncer_Push_UploadsCorrections(t *testing.T) {
	s3 := newMockS3()
	syncer := NewGraphSyncer(s3, "workstation", nil)

	ctx := context.Background()
	graphStore := newTestGraphStore(t)

	// Create a corrections file
	corrDir := t.TempDir()
	corrPath := filepath.Join(corrDir, "corrections.jsonl")
	os.WriteFile(corrPath, []byte("line1\nline2\nline3\n"), 0600)

	result, err := syncer.Push(ctx, graphStore, corrPath, "1.0.0")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	corrKey := "machines/workstation/graph/corrections.jsonl"
	exists, _ := s3.Exists(ctx, corrKey)
	if !exists {
		t.Error("corrections were not uploaded")
	}

	if result.CorrectionsSize == 0 {
		t.Error("CorrectionsSize should be > 0")
	}
}

func TestGraphSyncer_Pull_RestoresBackup(t *testing.T) {
	s3 := newMockS3()
	ctx := context.Background()

	// Push from "machine A"
	graphStoreA := newTestGraphStore(t)
	pushSyncer := NewGraphSyncer(s3, "machineA", nil)
	_, err := pushSyncer.Push(ctx, graphStoreA, "", "1.0.0")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	// Pull to "machine B"
	graphStoreB := newTestGraphStore(t)
	pullSyncer := NewGraphSyncer(s3, "machineB", nil)
	_, err = pullSyncer.Pull(ctx, graphStoreB, "machineA", "")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
}

func TestMergeCorrections(t *testing.T) {
	dir := t.TempDir()

	// Local has 5 lines
	localPath := filepath.Join(dir, "local-corrections.jsonl")
	var localLines []string
	for i := 1; i <= 5; i++ {
		localLines = append(localLines, fmt.Sprintf(`{"line":%d}`, i))
	}
	os.WriteFile(localPath, []byte(strings.Join(localLines, "\n")+"\n"), 0600)

	// Remote has 8 lines (same first 5 + 3 new)
	remotePath := filepath.Join(dir, "remote-corrections.jsonl")
	var remoteLines []string
	for i := 1; i <= 8; i++ {
		remoteLines = append(remoteLines, fmt.Sprintf(`{"line":%d}`, i))
	}
	os.WriteFile(remotePath, []byte(strings.Join(remoteLines, "\n")+"\n"), 0600)

	_, err := mergeCorrections(remotePath, localPath)
	if err != nil {
		t.Fatalf("mergeCorrections: %v", err)
	}

	// Read local and count lines
	data, _ := os.ReadFile(localPath)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 8 {
		t.Errorf("local line count = %d, want 8", len(lines))
	}
}

func TestMergeCorrections_NoNewLines(t *testing.T) {
	dir := t.TempDir()

	content := "line1\nline2\nline3\n"
	localPath := filepath.Join(dir, "local.jsonl")
	remotePath := filepath.Join(dir, "remote.jsonl")
	os.WriteFile(localPath, []byte(content), 0600)
	os.WriteFile(remotePath, []byte(content), 0600)

	beforeData, _ := os.ReadFile(localPath)

	_, err := mergeCorrections(remotePath, localPath)
	if err != nil {
		t.Fatalf("mergeCorrections: %v", err)
	}

	afterData, _ := os.ReadFile(localPath)
	if string(afterData) != string(beforeData) {
		t.Errorf("file was modified when no new lines should be added")
	}
}

// newTestGraphStore creates a minimal graph store for testing.
func newTestGraphStore(t *testing.T) *testGraphStore {
	t.Helper()
	return &testGraphStore{}
}

// testGraphStore is a minimal in-memory graph store for graph sync tests.
type testGraphStore struct {
	nodes []store.Node
	edges []store.Edge
}

func (s *testGraphStore) AddNode(_ context.Context, node store.Node) (string, error) {
	s.nodes = append(s.nodes, node)
	return node.ID, nil
}

func (s *testGraphStore) UpdateNode(_ context.Context, _ store.Node) error { return nil }

func (s *testGraphStore) GetNode(_ context.Context, id string) (*store.Node, error) {
	for _, n := range s.nodes {
		if n.ID == id {
			return &n, nil
		}
	}
	return nil, nil
}

func (s *testGraphStore) DeleteNode(_ context.Context, _ string) error { return nil }

func (s *testGraphStore) QueryNodes(_ context.Context, _ map[string]interface{}) ([]store.Node, error) {
	return s.nodes, nil
}

func (s *testGraphStore) AddEdge(_ context.Context, edge store.Edge) error {
	s.edges = append(s.edges, edge)
	return nil
}

func (s *testGraphStore) RemoveEdge(_ context.Context, _, _ string, _ store.EdgeKind) error {
	return nil
}

func (s *testGraphStore) GetEdges(_ context.Context, _ string, _ store.Direction, _ store.EdgeKind) ([]store.Edge, error) {
	return nil, nil
}

func (s *testGraphStore) Traverse(_ context.Context, _ string, _ []store.EdgeKind, _ store.Direction, _ int) ([]store.Node, error) {
	return nil, nil
}

func (s *testGraphStore) Sync(_ context.Context) error { return nil }
func (s *testGraphStore) Close() error                 { return nil }

// Verify testGraphStore implements GraphStore.
var _ store.GraphStore = (*testGraphStore)(nil)
