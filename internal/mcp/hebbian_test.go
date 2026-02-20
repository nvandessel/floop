package mcp

import (
	"testing"
	"time"

	"github.com/nvandessel/feedback-loop/internal/spreading"
	"github.com/nvandessel/feedback-loop/internal/store"
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
