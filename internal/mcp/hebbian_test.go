package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nvandessel/floop/internal/spreading"
	"github.com/nvandessel/floop/internal/store"
)

func TestCoActivationTracker_Record(t *testing.T) {
	tracker := newCoActivationTracker()
	cfg := spreading.DefaultHebbianConfig()
	cfg.CreationGate = 3
	cfg.CreationWindow = 1 * time.Hour

	pair := spreading.CoActivationPair{
		BehaviorA:   "a",
		BehaviorB:   "b",
		ActivationA: 0.8,
		ActivationB: 0.7,
	}

	// First two recordings: gate not met
	if tracker.record(pair, cfg) {
		t.Error("gate should not be met after 1 recording")
	}
	if tracker.record(pair, cfg) {
		t.Error("gate should not be met after 2 recordings")
	}

	// Third recording: gate met
	if !tracker.record(pair, cfg) {
		t.Error("gate should be met after 3 recordings")
	}
}

func TestCoActivationTracker_WindowExpiry(t *testing.T) {
	tracker := newCoActivationTracker()
	cfg := spreading.DefaultHebbianConfig()
	cfg.CreationGate = 3
	cfg.CreationWindow = 1 * time.Hour

	pair := spreading.CoActivationPair{
		BehaviorA:   "a",
		BehaviorB:   "b",
		ActivationA: 0.8,
		ActivationB: 0.7,
	}

	key := pairKey(pair.BehaviorA, pair.BehaviorB)

	// Simulate 2 old recordings (outside window)
	tracker.mu.Lock()
	tracker.entries[key] = []time.Time{
		time.Now().Add(-2 * time.Hour),
		time.Now().Add(-90 * time.Minute),
	}
	tracker.mu.Unlock()

	// These should be expired, so recording should start fresh
	if tracker.record(pair, cfg) {
		t.Error("expired entries should not count toward gate")
	}
	if tracker.record(pair, cfg) {
		t.Error("gate should not be met after 2 fresh recordings")
	}
	if !tracker.record(pair, cfg) {
		t.Error("gate should be met after 3 fresh recordings")
	}
}

func TestCoActivationTracker_DifferentPairs(t *testing.T) {
	tracker := newCoActivationTracker()
	cfg := spreading.DefaultHebbianConfig()
	cfg.CreationGate = 3

	pairAB := spreading.CoActivationPair{BehaviorA: "a", BehaviorB: "b"}
	pairAC := spreading.CoActivationPair{BehaviorA: "a", BehaviorB: "c"}

	// Record AB twice, AC once
	tracker.record(pairAB, cfg)
	tracker.record(pairAC, cfg)
	tracker.record(pairAB, cfg)

	// Third AB should trigger gate; AC has only 1 recording
	if !tracker.record(pairAB, cfg) {
		t.Error("AB gate should be met after 3 recordings")
	}
	if tracker.record(pairAC, cfg) {
		t.Error("AC gate should not be met after only 2 recordings")
	}
}

func TestCoActivationTracker_PersistentCrossSession(t *testing.T) {
	// The whole point of persistence: co-activation counts survive across
	// tracker instances (simulating MCP server restarts).
	tmpDir := t.TempDir()
	s, err := store.NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore: %v", err)
	}

	cfg := spreading.DefaultHebbianConfig()
	cfg.CreationGate = 3
	cfg.CreationWindow = 1 * time.Hour

	pair := spreading.CoActivationPair{
		BehaviorA:   "a",
		BehaviorB:   "b",
		ActivationA: 0.8,
		ActivationB: 0.7,
	}

	// Session 1: record twice
	tracker1 := newPersistentCoActivationTracker(s)
	if tracker1.record(pair, cfg) {
		t.Error("gate should not be met after 1 recording")
	}
	if tracker1.record(pair, cfg) {
		t.Error("gate should not be met after 2 recordings")
	}

	// Simulate server restart: close store, reopen, create new tracker
	s.Close()
	s2, err := store.NewSQLiteGraphStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteGraphStore (reopen): %v", err)
	}
	defer s2.Close()

	// Session 2: one more recording should meet the gate
	tracker2 := newPersistentCoActivationTracker(s2)
	if !tracker2.record(pair, cfg) {
		t.Error("gate should be met after 3 recordings across sessions")
	}
}

func TestCoActivationTracker_PersistentFallback(t *testing.T) {
	// When store is nil, the tracker should fall back to in-memory behavior
	tracker := newCoActivationTracker()
	if tracker.store != nil {
		t.Error("in-memory tracker should have nil store")
	}

	cfg := spreading.DefaultHebbianConfig()
	cfg.CreationGate = 2

	pair := spreading.CoActivationPair{
		BehaviorA:   "x",
		BehaviorB:   "y",
		ActivationA: 0.5,
		ActivationB: 0.5,
	}

	if tracker.record(pair, cfg) {
		t.Error("gate should not be met after 1 recording")
	}
	if !tracker.record(pair, cfg) {
		t.Error("gate should be met after 2 recordings (in-memory)")
	}
}

func TestPairKey_Canonical(t *testing.T) {
	k1 := pairKey("alpha", "beta")
	k2 := pairKey("alpha", "beta")
	if k1 != k2 {
		t.Errorf("same inputs should produce same key: %s != %s", k1, k2)
	}

	k3 := pairKey("beta", "alpha")
	if k1 == k3 {
		t.Log("note: pairKey is NOT symmetric — caller must ensure canonical order")
	}
}

func TestApplyHebbianUpdates_ReturnsChanged(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Add two behavior nodes so edges can reference them
	nodeA := store.Node{
		ID:   "behav-a",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "behav-a",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Use error wrapping",
			},
		},
	}
	nodeB := store.Node{
		ID:   "behav-b",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "behav-b",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Return errors from functions",
			},
		},
	}
	if _, err := server.store.AddNode(ctx, nodeA); err != nil {
		t.Fatalf("AddNode(a): %v", err)
	}
	if _, err := server.store.AddNode(ctx, nodeB); err != nil {
		t.Fatalf("AddNode(b): %v", err)
	}

	pair := spreading.CoActivationPair{
		BehaviorA:   "behav-a",
		BehaviorB:   "behav-b",
		ActivationA: 0.8,
		ActivationB: 0.7,
	}
	cfg := spreading.DefaultHebbianConfig()
	cfg.CreationGate = 3
	cfg.CreationWindow = 1 * time.Hour

	// Empty pairs → false
	if server.applyHebbianUpdates(ctx, nil, cfg) {
		t.Error("empty pairs should return false")
	}

	// Gate not met → false (only records co-activation, no edge mutation)
	if server.applyHebbianUpdates(ctx, []spreading.CoActivationPair{pair}, cfg) {
		t.Error("should return false when gate not met (1/3)")
	}
	if server.applyHebbianUpdates(ctx, []spreading.CoActivationPair{pair}, cfg) {
		t.Error("should return false when gate not met (2/3)")
	}

	// Gate met on 3rd → true (edge created)
	if !server.applyHebbianUpdates(ctx, []spreading.CoActivationPair{pair}, cfg) {
		t.Error("should return true when gate met and edge created")
	}

	// Verify edge actually exists
	edges, err := server.store.GetEdges(ctx, "behav-a", store.DirectionOutbound, store.EdgeKindCoActivated)
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Target == "behav-b" {
			found = true
			break
		}
	}
	if !found {
		t.Error("co-activated edge should exist after gate met")
	}

	// Edge already exists → weight update → true
	if !server.applyHebbianUpdates(ctx, []spreading.CoActivationPair{pair}, cfg) {
		t.Error("should return true when existing edge weight is updated")
	}
}

func TestApplyHebbianUpdates_SyncsEdgesToJSONL(t *testing.T) {
	server, tmpDir := setupTestServer(t)
	defer server.Close()

	ctx := context.Background()

	// Add two behavior nodes
	nodeA := store.Node{
		ID:   "behav-x",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "behav-x",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Always use context",
			},
		},
	}
	nodeB := store.Node{
		ID:   "behav-y",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "behav-y",
			"kind": "directive",
			"content": map[string]interface{}{
				"canonical": "Pass context to functions",
			},
		},
	}
	if _, err := server.store.AddNode(ctx, nodeA); err != nil {
		t.Fatalf("AddNode(x): %v", err)
	}
	if _, err := server.store.AddNode(ctx, nodeB); err != nil {
		t.Fatalf("AddNode(y): %v", err)
	}

	// Set creation gate to 1 so edge is created immediately
	cfg := spreading.DefaultHebbianConfig()
	cfg.CreationGate = 1
	cfg.CreationWindow = 1 * time.Hour

	pair := spreading.CoActivationPair{
		BehaviorA:   "behav-x",
		BehaviorB:   "behav-y",
		ActivationA: 0.8,
		ActivationB: 0.7,
	}

	// Apply hebbian update — should create edge and return true
	changed := server.applyHebbianUpdates(ctx, []spreading.CoActivationPair{pair}, cfg)
	if !changed {
		t.Fatal("applyHebbianUpdates should return true when edge created")
	}

	// Now sync (simulating what the fixed background goroutine does)
	if err := server.store.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify edges.jsonl contains the co-activated edge.
	// AddNode defaults to global store, so edges route there too.
	// Global store root = $HOME/.floop/ and HOME = tmpDir/home in tests.
	edgesFile := filepath.Join(tmpDir, "home", ".floop", "edges.jsonl")
	data, err := os.ReadFile(edgesFile)
	if err != nil {
		t.Fatalf("ReadFile(edges.jsonl): %v", err)
	}

	type edgeRecord struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Kind   string `json:"kind"`
	}

	foundEdge := false
	for _, line := range splitNonEmpty(data) {
		var rec edgeRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Source == "behav-x" && rec.Target == "behav-y" && rec.Kind == string(store.EdgeKindCoActivated) {
			foundEdge = true
			break
		}
	}
	if !foundEdge {
		t.Errorf("edges.jsonl should contain co-activated edge behav-x→behav-y, got: %s", string(data))
	}
}

// splitNonEmpty splits bytes by newline and returns non-empty lines.
func splitNonEmpty(data []byte) [][]byte {
	var result [][]byte
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) > 0 {
			result = append(result, line)
		}
	}
	return result
}
