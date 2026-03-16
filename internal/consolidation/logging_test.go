package consolidation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/logging"
)

func TestDecisionLogging_AllStages(t *testing.T) {
	dir := t.TempDir()
	dl := logging.NewDecisionLogger(dir, "debug")
	defer dl.Close()

	cl := NewConsolidationLogger(dl, "run-test-123", "test-model")

	// Log each stage
	cl.LogExtract("extract.summarize", 100, 500, "sha256:abc", map[string]any{
		"chunk_index": 0,
		"event_count": 20,
	})

	cl.LogExtract("extract.arc", 200, 800, "sha256:def", map[string]any{
		"chunk_count": 5,
	})

	cl.LogExtract("extract.candidates", 150, 600, "sha256:ghi", map[string]any{
		"chunk_index":      0,
		"candidates_found": 3,
	})

	cl.LogClassify(120, 400, "sha256:jkl", map[string]any{
		"batch_index":  0,
		"input_count":  8,
		"output_count": 8,
	})

	cl.LogRelate(300, 1000, map[string]any{
		"memories_count":  8,
		"neighbors_found": 12,
		"edges_proposed":  5,
	})

	cl.LogPromote("promote", 50, map[string]any{
		"memory_kind": "directive",
		"confidence":  0.92,
	})

	cl.LogPromote("merge", 80, map[string]any{
		"into":     "bhv-789",
		"strategy": "absorb",
	})

	cl.LogPromote("skip", 0, map[string]any{
		"reason": "already_captured",
	})

	// Close and read the file
	dl.Close()

	data, err := os.ReadFile(filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("reading decisions.jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 8 {
		t.Fatalf("expected 8 log entries, got %d", len(lines))
	}

	// Verify each line is valid JSON with required fields
	expectedStages := []string{
		"extract.summarize", "extract.arc", "extract.candidates",
		"classify", "relate",
		"promote", "promote", "promote",
	}
	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}

		if entry["run_id"] != "run-test-123" {
			t.Errorf("line %d: expected run_id='run-test-123', got %v", i, entry["run_id"])
		}
		if entry["model"] != "test-model" {
			t.Errorf("line %d: expected model='test-model', got %v", i, entry["model"])
		}

		stage, _ := entry["stage"].(string)
		if stage != expectedStages[i] {
			t.Errorf("line %d: expected stage=%q, got %q", i, expectedStages[i], stage)
		}

		if _, ok := entry["timestamp"]; !ok {
			t.Errorf("line %d: missing timestamp", i)
		}
		if _, ok := entry["time"]; !ok {
			t.Errorf("line %d: missing time (from DecisionLogger)", i)
		}
	}
}

func TestDecisionLogging_Fallback(t *testing.T) {
	dir := t.TempDir()
	dl := logging.NewDecisionLogger(dir, "debug")
	defer dl.Close()

	cl := NewConsolidationLogger(dl, "run-fallback", "test-model")

	cl.LogFallback("extract.candidates", "llm_error", errors.New("timeout after 30s"))

	dl.Close()

	data, err := os.ReadFile(filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("reading decisions.jsonl: %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dataMap, _ := entry["data"].(map[string]any)
	if dataMap["fallback_used"] != true {
		t.Error("expected fallback_used=true")
	}
	if dataMap["fallback_reason"] != "llm_error" {
		t.Errorf("expected fallback_reason='llm_error', got %v", dataMap["fallback_reason"])
	}
	if dataMap["error"] != "timeout after 30s" {
		t.Errorf("expected error message, got %v", dataMap["error"])
	}
}

func TestDecisionLogging_InputHash(t *testing.T) {
	// Same input should produce same hash
	input := map[string]any{"key": "value", "count": 42}
	hash1 := InputHash(input)
	hash2 := InputHash(input)

	if hash1 != hash2 {
		t.Errorf("same input produced different hashes: %q vs %q", hash1, hash2)
	}
	if hash1 == "" {
		t.Error("expected non-empty hash")
	}
	if !strings.HasPrefix(hash1, "sha256:") {
		t.Errorf("expected sha256: prefix, got %q", hash1)
	}

	// Different input should produce different hash
	different := map[string]any{"key": "other"}
	hash3 := InputHash(different)
	if hash1 == hash3 {
		t.Error("different inputs produced same hash")
	}
}

func TestDecisionLogging_NilLogger(t *testing.T) {
	// Nil ConsolidationLogger should not panic
	var cl *ConsolidationLogger

	cl.LogExtract("extract.summarize", 100, 500, "", nil)
	cl.LogClassify(100, 500, "", nil)
	cl.LogRelate(100, 500, nil)
	cl.LogPromote("promote", 0, nil)
	cl.LogFallback("extract", "test", nil)
}
