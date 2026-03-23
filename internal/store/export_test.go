package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportImportPreservesEmbeddings(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}

	ctx := context.Background()

	// Add a behavior and store an embedding
	_, err = s.AddNode(ctx, Node{
		ID:   "emb-round-trip",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Embedding Round Trip",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test embedding round-trip through JSONL",
			},
		},
	})
	if err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	originalVec := []float32{0.1, -0.2, 0.3, 0.0, 0.5}
	modelName := "text-embedding-3-small"
	err = s.StoreEmbedding(ctx, "emb-round-trip", originalVec, modelName)
	if err != nil {
		t.Fatalf("StoreEmbedding() error = %v", err)
	}

	// Sync to export to JSONL
	if err := s.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	s.Close()

	// Create a new store in a different directory and import the JSONL
	tmpDir2 := t.TempDir()
	floopDir2 := filepath.Join(tmpDir2, ".floop")
	if err := os.MkdirAll(floopDir2, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Copy the nodes.jsonl from the first store
	nodesFile := filepath.Join(tmpDir, ".floop", "nodes.jsonl")
	nodesData, err := os.ReadFile(nodesFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	destNodesFile := filepath.Join(floopDir2, "nodes.jsonl")
	if err := os.WriteFile(destNodesFile, nodesData, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Open the new store which auto-imports
	s2, err := NewSQLiteGraphStore(tmpDir2)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() for import error = %v", err)
	}
	defer s2.Close()

	// Verify the embedding was restored
	embeddings, err := s2.GetAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetAllEmbeddings() error = %v", err)
	}

	if len(embeddings) != 1 {
		t.Fatalf("GetAllEmbeddings() returned %d, want 1", len(embeddings))
	}

	if embeddings[0].BehaviorID != "emb-round-trip" {
		t.Errorf("BehaviorID = %s, want emb-round-trip", embeddings[0].BehaviorID)
	}

	if len(embeddings[0].Embedding) != len(originalVec) {
		t.Fatalf("embedding length = %d, want %d", len(embeddings[0].Embedding), len(originalVec))
	}

	for i, v := range embeddings[0].Embedding {
		if v != originalVec[i] {
			t.Errorf("embedding[%d] = %v, want %v", i, v, originalVec[i])
		}
	}

	// Verify embedding_model was stored
	var storedModel string
	err = s2.db.QueryRowContext(ctx,
		`SELECT embedding_model FROM behaviors WHERE id = ?`,
		"emb-round-trip").Scan(&storedModel)
	if err != nil {
		t.Fatalf("query embedding_model error = %v", err)
	}
	if storedModel != modelName {
		t.Errorf("embedding_model = %s, want %s", storedModel, modelName)
	}
}

func TestImportJSONL_NoEmbedding(t *testing.T) {
	// Test that old JSONL files without embedding fields import cleanly
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Write a JSONL file without any embedding fields (old format)
	nodesFile := filepath.Join(floopDir, "nodes.jsonl")
	f, err := os.Create(nodesFile)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	f.WriteString(`{"id":"old-node","kind":"behavior","content":{"name":"Old Node","kind":"directive","content":{"canonical":"A behavior from old JSONL"}},"metadata":{"confidence":0.7}}`)
	f.WriteString("\n")
	f.Close()

	// Import should succeed without errors
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Verify node was imported
	got, err := s.GetNode(ctx, "old-node")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("imported node not found")
	}
	if got.Content["name"] != "Old Node" {
		t.Errorf("node name = %v, want Old Node", got.Content["name"])
	}

	// Verify no embedding was stored
	embeddings, err := s.GetAllEmbeddings(ctx)
	if err != nil {
		t.Fatalf("GetAllEmbeddings() error = %v", err)
	}
	if len(embeddings) != 0 {
		t.Errorf("GetAllEmbeddings() returned %d, want 0 (old JSONL has no embeddings)", len(embeddings))
	}
}

func TestExportEmbedding_Base64Format(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}

	ctx := context.Background()

	// Add a behavior and store an embedding
	_, err = s.AddNode(ctx, Node{
		ID:   "base64-test",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "Base64 Test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Test base64 encoding correctness",
			},
		},
	})
	if err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	vec := []float32{1.0, 2.0, 3.0}
	modelName := "test-model-v1"
	err = s.StoreEmbedding(ctx, "base64-test", vec, modelName)
	if err != nil {
		t.Fatalf("StoreEmbedding() error = %v", err)
	}

	// Sync to export to JSONL
	if err := s.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Read the raw JSONL and verify the base64 encoding
	nodesFile := filepath.Join(tmpDir, ".floop", "nodes.jsonl")
	data, err := os.ReadFile(nodesFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var node Node
	if err := json.Unmarshal(data, &node); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Verify embedding field is present and is base64
	embStr, ok := node.Metadata["embedding"].(string)
	if !ok || embStr == "" {
		t.Fatal("embedding not found in metadata or not a string")
	}

	// Decode the base64 and verify it matches the original binary encoding
	embBytes, err := base64.StdEncoding.DecodeString(embStr)
	if err != nil {
		t.Fatalf("base64.DecodeString() error = %v", err)
	}

	expectedBlob := encodeEmbedding(vec)
	if len(embBytes) != len(expectedBlob) {
		t.Fatalf("decoded blob length = %d, want %d", len(embBytes), len(expectedBlob))
	}
	for i := range embBytes {
		if embBytes[i] != expectedBlob[i] {
			t.Errorf("blob[%d] = %d, want %d", i, embBytes[i], expectedBlob[i])
		}
	}

	// Verify embedding_model field
	embModelStr, ok := node.Metadata["embedding_model"].(string)
	if !ok || embModelStr != modelName {
		t.Errorf("embedding_model = %v, want %s", node.Metadata["embedding_model"], modelName)
	}

	// Also verify round-trip: decode the blob to float32 and compare
	decoded := decodeEmbedding(embBytes)
	if len(decoded) != len(vec) {
		t.Fatalf("decoded vector length = %d, want %d", len(decoded), len(vec))
	}
	for i, v := range decoded {
		if v != vec[i] {
			t.Errorf("decoded[%d] = %v, want %v", i, v, vec[i])
		}
	}

	s.Close()
}

func TestImportEdgesFromJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	ctx := context.Background()

	// Create store first to get schema
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}

	// Write an edges JSONL file with various formats
	edgesFile := filepath.Join(floopDir, "edges.jsonl")
	now := time.Now()

	edges := []Edge{
		{Source: "a", Target: "b", Kind: EdgeKindRequires, Weight: 0.8, CreatedAt: now},
		{Source: "b", Target: "c", Kind: EdgeKindOverrides, Weight: 0, CreatedAt: time.Time{}}, // old format: zero weight and zero time
		{Source: "c", Target: "d", Kind: EdgeKindConflicts, Weight: 0.5, CreatedAt: now, LastActivated: &now, Metadata: map[string]interface{}{"note": "test"}},
	}

	f, err := os.Create(edgesFile)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	enc := json.NewEncoder(f)
	for _, e := range edges {
		enc.Encode(e)
	}
	f.Close()

	// Import edges
	err = s.ImportEdgesFromJSONL(ctx, edgesFile)
	if err != nil {
		t.Fatalf("ImportEdgesFromJSONL() error = %v", err)
	}

	// Verify edges were imported
	got, err := s.GetEdges(ctx, "a", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("GetEdges(a) got %d, want 1", len(got))
	}

	// Verify old-format edge got default weight of 1.0
	got2, err := s.GetEdges(ctx, "b", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges(b) error = %v", err)
	}
	if len(got2) == 1 && got2[0].Weight != 1.0 {
		t.Errorf("backfilled weight = %v, want 1.0", got2[0].Weight)
	}

	// Verify edge with metadata
	got3, err := s.GetEdges(ctx, "c", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges(c) error = %v", err)
	}
	if len(got3) == 1 {
		if got3[0].Metadata == nil || got3[0].Metadata["note"] != "test" {
			t.Errorf("edge metadata not preserved: %v", got3[0].Metadata)
		}
	}

	s.Close()
}

func TestImportNodesFromJSONL_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Import from non-existent file should be a no-op
	err = s.ImportNodesFromJSONL(ctx, "/nonexistent/path/nodes.jsonl")
	if err != nil {
		t.Errorf("ImportNodesFromJSONL() for missing file should return nil, got %v", err)
	}
}

func TestImportEdgesFromJSONL_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Import from non-existent file should be a no-op
	err = s.ImportEdgesFromJSONL(ctx, "/nonexistent/path/edges.jsonl")
	if err != nil {
		t.Errorf("ImportEdgesFromJSONL() for missing file should return nil, got %v", err)
	}
}

func TestImportNodesFromJSONL_GenericNode(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Write JSONL with a non-behavior node (correction type)
	nodesFile := filepath.Join(floopDir, "nodes.jsonl")
	node := Node{
		ID:   "corr-1",
		Kind: NodeKindCorrection,
		Content: map[string]interface{}{
			"name":        "some correction",
			"description": "a correction node",
		},
	}
	f, err := os.Create(nodesFile)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	json.NewEncoder(f).Encode(node)
	f.Close()

	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	got, err := s.GetNode(ctx, "corr-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("generic node not imported")
	}
	if got.Kind != NodeKindCorrection {
		t.Errorf("node kind = %v, want correction", got.Kind)
	}
}

func TestImportNodesFromJSONL_WithMalformedAndEmptyLines(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Write JSONL with empty lines and malformed JSON
	nodesFile := filepath.Join(floopDir, "nodes.jsonl")
	f, err := os.Create(nodesFile)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	f.WriteString("\n") // empty line
	f.WriteString(`{"id":"good-node","kind":"behavior","content":{"name":"Good","kind":"directive","content":{"canonical":"a good behavior"}}}` + "\n")
	f.WriteString("this is not valid json\n") // malformed
	f.WriteString("\n")                       // another empty line
	f.Close()

	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	got, err := s.GetNode(ctx, "good-node")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if got == nil {
		t.Fatal("valid node should be imported despite malformed lines")
	}
}

func TestGetDirtyBehaviorIDs(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Initially should be empty
	ids, err := s.GetDirtyBehaviorIDs(ctx)
	if err != nil {
		t.Fatalf("GetDirtyBehaviorIDs() error = %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("initial dirty IDs = %d, want 0", len(ids))
	}

	// Add a behavior — should trigger dirty flag
	s.AddNode(ctx, Node{
		ID:   "dirty-1",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"name": "dirty test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "a dirty behavior",
			},
		},
	})

	ids, err = s.GetDirtyBehaviorIDs(ctx)
	if err != nil {
		t.Fatalf("GetDirtyBehaviorIDs() after add error = %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("dirty IDs after add = %d, want 1", len(ids))
	}

	// IsDirty should return true
	dirty, err := s.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty() error = %v", err)
	}
	if !dirty {
		t.Error("IsDirty() = false, want true")
	}
}

func TestSQLiteGraphStore_AutoImport_NewerJSONL(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store, add data, sync, close
	s1, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	ctx := context.Background()
	s1.AddNode(ctx, Node{
		ID:   "auto-import-1",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"name": "auto import test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "test auto import with newer jsonl",
			},
		},
	})
	if err := s1.Sync(ctx); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	s1.Close()

	// Touch the JSONL file to make it newer than the DB
	nodesFile := filepath.Join(tmpDir, ".floop", "nodes.jsonl")
	futureTime := time.Now().Add(2 * time.Second)
	os.Chtimes(nodesFile, futureTime, futureTime)

	// Also add a new node to the JSONL
	f, err := os.OpenFile(nodesFile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	newNode := Node{
		ID:   "auto-import-2",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"name": "second node",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "added via jsonl",
			},
		},
	}
	json.NewEncoder(f).Encode(newNode)
	f.Close()
	os.Chtimes(nodesFile, futureTime, futureTime)

	// Reopen store — should reimport from JSONL since it's newer
	s2, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() reopen error = %v", err)
	}
	defer s2.Close()

	// Verify both nodes exist
	got1, _ := s2.GetNode(ctx, "auto-import-1")
	if got1 == nil {
		t.Error("original node should exist after reimport")
	}
	got2, _ := s2.GetNode(ctx, "auto-import-2")
	if got2 == nil {
		t.Error("new node from JSONL should exist after reimport")
	}
}

func TestSQLiteGraphStore_AutoImport_WithEdgesJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	floopDir := filepath.Join(tmpDir, ".floop")
	if err := os.MkdirAll(floopDir, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	ctx := context.Background()

	// Write nodes and edges JSONL files
	nodesFile := filepath.Join(floopDir, "nodes.jsonl")
	edgesFile := filepath.Join(floopDir, "edges.jsonl")

	nf, err := os.Create(nodesFile)
	if err != nil {
		t.Fatalf("Create(nodesFile) error = %v", err)
	}
	json.NewEncoder(nf).Encode(Node{ID: "n1", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "n1", "kind": "directive", "content": map[string]interface{}{"canonical": "node n1"}}})
	json.NewEncoder(nf).Encode(Node{ID: "n2", Kind: NodeKindBehavior, Content: map[string]interface{}{"name": "n2", "kind": "directive", "content": map[string]interface{}{"canonical": "node n2"}}})
	nf.Close()

	ef, err := os.Create(edgesFile)
	if err != nil {
		t.Fatalf("Create(edgesFile) error = %v", err)
	}
	json.NewEncoder(ef).Encode(Edge{Source: "n1", Target: "n2", Kind: EdgeKindRequires, Weight: 0.9, CreatedAt: time.Now()})
	ef.Close()

	// Open store — should auto-import both
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	edges, err := s.GetEdges(ctx, "n1", DirectionOutbound, "")
	if err != nil {
		t.Fatalf("GetEdges() error = %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("GetEdges() got %d, want 1", len(edges))
	}
}

func TestSQLiteGraphStore_SyncCreatesJSONLWhenMissing(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore() error = %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Add and sync to create JSONL files
	s.AddNode(ctx, Node{
		ID:   "sync-test",
		Kind: NodeKindBehavior,
		Content: map[string]interface{}{
			"name": "sync test",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "test sync creates jsonl",
			},
		},
	})
	if err := s.Sync(ctx); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}

	// Remove the JSONL files
	nodesFile := filepath.Join(tmpDir, ".floop", "nodes.jsonl")
	os.Remove(nodesFile)

	// Sync again — should recreate the file even with no dirty behaviors
	if err := s.Sync(ctx); err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}

	// Verify file was recreated
	if _, err := os.Stat(nodesFile); os.IsNotExist(err) {
		t.Error("Sync() should recreate missing nodes.jsonl")
	}
}
