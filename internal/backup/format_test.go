package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/store"
)

func TestDetectFormat_V1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1-backup.json")

	// Write a plain V1 JSON backup
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

	version, err := DetectFormat(path)
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if version != FormatV1 {
		t.Errorf("DetectFormat() = %d, want %d", version, FormatV1)
	}
}

func TestDetectFormat_V2(t *testing.T) {
	dir := t.TempDir()

	// Create a V2 backup via WriteV2
	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "a", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}
	path := filepath.Join(dir, "v2-backup.json.gz")
	if err := WriteV2(path, bf, nil); err != nil {
		t.Fatal(err)
	}

	version, err := DetectFormat(path)
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if version != FormatV2 {
		t.Errorf("DetectFormat() = %d, want %d", version, FormatV2)
	}
}

func TestWriteV2_ReadV2_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roundtrip.json.gz")

	now := time.Now().Truncate(time.Millisecond) // JSON truncates sub-ms
	original := &BackupFormat{
		Version:   2,
		CreatedAt: now,
		Nodes: []BackupNode{
			{Node: store.Node{ID: "node-1", Kind: "behavior", Content: map[string]interface{}{"name": "test"}}},
			{Node: store.Node{ID: "node-2", Kind: "behavior"}},
		},
		Edges: []store.Edge{
			{Source: "node-1", Target: "node-2", Kind: store.EdgeKindRequires, Weight: 0.9},
		},
	}

	if err := WriteV2(path, original, nil); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}

	// Verify file exists
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("backup file is empty")
	}

	restored, err := ReadV2(path)
	if err != nil {
		t.Fatalf("ReadV2() error = %v", err)
	}

	if len(restored.Nodes) != len(original.Nodes) {
		t.Errorf("Nodes count = %d, want %d", len(restored.Nodes), len(original.Nodes))
	}
	if len(restored.Edges) != len(original.Edges) {
		t.Errorf("Edges count = %d, want %d", len(restored.Edges), len(original.Edges))
	}
	if restored.Nodes[0].ID != "node-1" {
		t.Errorf("first node ID = %s, want node-1", restored.Nodes[0].ID)
	}
	if restored.Edges[0].Source != "node-1" || restored.Edges[0].Target != "node-2" {
		t.Errorf("edge = %s->%s, want node-1->node-2", restored.Edges[0].Source, restored.Edges[0].Target)
	}
}

func TestReadV2_CorruptedChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted.json.gz")

	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "a", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}
	if err := WriteV2(path, bf, nil); err != nil {
		t.Fatal(err)
	}

	// Tamper with the compressed payload (append garbage bytes)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("CORRUPTED"))
	f.Close()

	_, err = ReadV2(path)
	if err == nil {
		t.Error("ReadV2() should fail with corrupted checksum")
	}
}

func TestVerifyChecksum_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "valid.json.gz")

	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "a", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}
	if err := WriteV2(path, bf, nil); err != nil {
		t.Fatal(err)
	}

	if err := VerifyChecksum(path); err != nil {
		t.Errorf("VerifyChecksum() error = %v", err)
	}
}

func TestVerifyChecksum_Tampered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tampered.json.gz")

	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "a", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}
	if err := WriteV2(path, bf, nil); err != nil {
		t.Fatal(err)
	}

	// Tamper with the file
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("tampered"))
	f.Close()

	if err := VerifyChecksum(path); err == nil {
		t.Error("VerifyChecksum() should fail with tampered file")
	}
}

func TestReadV2Header(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "header-test.json.gz")

	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes: []BackupNode{
			{Node: store.Node{ID: "a", Kind: "behavior"}},
			{Node: store.Node{ID: "b", Kind: "behavior"}},
		},
		Edges: []store.Edge{
			{Source: "a", Target: "b", Kind: store.EdgeKindRequires},
		},
	}
	if err := WriteV2(path, bf, nil); err != nil {
		t.Fatal(err)
	}

	header, err := ReadV2Header(path)
	if err != nil {
		t.Fatalf("ReadV2Header() error = %v", err)
	}

	if header.Version != FormatV2 {
		t.Errorf("Version = %d, want %d", header.Version, FormatV2)
	}
	if header.NodeCount != 2 {
		t.Errorf("NodeCount = %d, want 2", header.NodeCount)
	}
	if header.EdgeCount != 1 {
		t.Errorf("EdgeCount = %d, want 1", header.EdgeCount)
	}
	if !header.Compressed {
		t.Error("Compressed = false, want true")
	}
	if header.Checksum == "" {
		t.Error("Checksum is empty")
	}
}

func TestWriteV2_MetadataPopulated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metadata-test.json.gz")

	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "a", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}

	opts := &WriteOptions{
		FloopVersion: "1.2.3",
		Metadata: map[string]string{
			"custom_key": "custom_value",
		},
	}

	if err := WriteV2(path, bf, opts); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}

	header, err := ReadV2Header(path)
	if err != nil {
		t.Fatalf("ReadV2Header() error = %v", err)
	}

	// Verify auto-populated metadata
	if header.Metadata == nil {
		t.Fatal("Metadata is nil")
	}

	if v := header.Metadata["floop_version"]; v != "1.2.3" {
		t.Errorf("floop_version = %q, want %q", v, "1.2.3")
	}

	if v := header.Metadata["hostname"]; v == "" {
		t.Error("hostname is empty")
	}

	expectedPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if v := header.Metadata["platform"]; v != expectedPlatform {
		t.Errorf("platform = %q, want %q", v, expectedPlatform)
	}

	if v := header.Metadata["schema"]; v == "" {
		t.Error("schema is empty")
	}

	// Verify user-supplied metadata is merged
	if v := header.Metadata["custom_key"]; v != "custom_value" {
		t.Errorf("custom_key = %q, want %q", v, "custom_value")
	}
}

func TestWriteV2_MetadataPopulated_NilOpts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nil-opts-test.json.gz")

	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "a", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}

	if err := WriteV2(path, bf, nil); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}

	header, err := ReadV2Header(path)
	if err != nil {
		t.Fatalf("ReadV2Header() error = %v", err)
	}

	// Even with nil opts, auto-populated metadata should be present
	if header.Metadata == nil {
		t.Fatal("Metadata is nil with nil opts")
	}

	if v := header.Metadata["platform"]; v == "" {
		t.Error("platform should be populated even with nil opts")
	}

	if v := header.Metadata["schema"]; v == "" {
		t.Error("schema should be populated even with nil opts")
	}

	// floop_version should NOT be present without opts
	if v, ok := header.Metadata["floop_version"]; ok {
		t.Errorf("floop_version should not be present with nil opts, got %q", v)
	}
}

func TestWriteV2_SchemaVersionSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema-version-test.json.gz")

	bf := &BackupFormat{
		Version:   2,
		CreatedAt: time.Now(),
		Nodes:     []BackupNode{{Node: store.Node{ID: "a", Kind: "behavior"}}},
		Edges:     []store.Edge{},
	}

	if err := WriteV2(path, bf, nil); err != nil {
		t.Fatalf("WriteV2() error = %v", err)
	}

	header, err := ReadV2Header(path)
	if err != nil {
		t.Fatalf("ReadV2Header() error = %v", err)
	}

	if header.SchemaVersion != store.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", header.SchemaVersion, store.SchemaVersion)
	}
}
