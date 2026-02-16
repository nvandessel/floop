package dedup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/llm"
	"github.com/nvandessel/feedback-loop/internal/logging"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// createTestStore creates an in-memory store with test behaviors.
func createTestStore(behaviors []models.Behavior) store.GraphStore {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	for _, b := range behaviors {
		node := models.BehaviorToNode(&b)
		s.AddNode(ctx, node)
	}

	return s
}

func TestNewStoreDeduplicator(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	merger := NewBehaviorMerger(MergerConfig{})
	config := DefaultConfig()

	dedup := NewStoreDeduplicator(s, merger, config)

	if dedup.store != s {
		t.Error("expected store to be set")
	}
	if dedup.merger != merger {
		t.Error("expected merger to be set")
	}
	if dedup.config.SimilarityThreshold != config.SimilarityThreshold {
		t.Errorf("expected threshold %v, got %v", config.SimilarityThreshold, dedup.config.SimilarityThreshold)
	}
}

func TestStoreDeduplicator_FindDuplicates(t *testing.T) {
	ctx := context.Background()

	t.Run("finds identical behaviors", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Name:    "Use pathlib",
				Content: models.BehaviorContent{Canonical: "always use pathlib for file operations"},
				When:    map[string]interface{}{"language": "python"},
			},
			{
				ID:      "b2",
				Name:    "Use pathlib module",
				Content: models.BehaviorContent{Canonical: "always use pathlib for file operations"},
				When:    map[string]interface{}{"language": "python"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DeduplicatorConfig{
			SimilarityThreshold: 0.9,
		})

		matches, err := dedup.FindDuplicates(ctx, &behaviors[0])
		if err != nil {
			t.Fatalf("FindDuplicates failed: %v", err)
		}

		if len(matches) != 1 {
			t.Errorf("expected 1 match, got %d", len(matches))
		}

		if len(matches) > 0 && matches[0].Behavior.ID != "b2" {
			t.Errorf("expected match ID b2, got %s", matches[0].Behavior.ID)
		}
	})

	t.Run("returns empty for no duplicates", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Name:    "Use pathlib",
				Content: models.BehaviorContent{Canonical: "always use pathlib for file operations"},
				When:    map[string]interface{}{"language": "python"},
			},
			{
				ID:      "b2",
				Name:    "Run tests",
				Content: models.BehaviorContent{Canonical: "run all tests before committing"},
				When:    map[string]interface{}{"task": "commit"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DeduplicatorConfig{
			SimilarityThreshold: 0.9,
		})

		matches, err := dedup.FindDuplicates(ctx, &behaviors[0])
		if err != nil {
			t.Fatalf("FindDuplicates failed: %v", err)
		}

		if len(matches) != 0 {
			t.Errorf("expected 0 matches, got %d", len(matches))
		}
	})

	t.Run("does not include self", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Name:    "Use pathlib",
				Content: models.BehaviorContent{Canonical: "always use pathlib"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DefaultConfig())

		matches, err := dedup.FindDuplicates(ctx, &behaviors[0])
		if err != nil {
			t.Fatalf("FindDuplicates failed: %v", err)
		}

		for _, m := range matches {
			if m.Behavior.ID == "b1" {
				t.Error("found self in duplicates")
			}
		}
	})

	t.Run("respects similarity threshold", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Content: models.BehaviorContent{Canonical: "use pathlib for files"},
				When:    map[string]interface{}{"language": "python"},
			},
			{
				ID:      "b2",
				Content: models.BehaviorContent{Canonical: "use pathlib for file handling"},
				When:    map[string]interface{}{"language": "python"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})

		// With high threshold, may not match
		dedupHigh := NewStoreDeduplicator(s, merger, DeduplicatorConfig{
			SimilarityThreshold: 0.99,
		})

		matchesHigh, _ := dedupHigh.FindDuplicates(ctx, &behaviors[0])

		// With low threshold, should match
		dedupLow := NewStoreDeduplicator(s, merger, DeduplicatorConfig{
			SimilarityThreshold: 0.5,
		})

		matchesLow, _ := dedupLow.FindDuplicates(ctx, &behaviors[0])

		// Low threshold should find at least as many matches as high threshold
		if len(matchesLow) < len(matchesHigh) {
			t.Errorf("lower threshold should find more matches: low=%d, high=%d",
				len(matchesLow), len(matchesHigh))
		}
	})

	t.Run("sorts by similarity descending", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Content: models.BehaviorContent{Canonical: "use pathlib always"},
				When:    map[string]interface{}{"language": "python"},
			},
			{
				ID:      "b2",
				Content: models.BehaviorContent{Canonical: "use pathlib always for files"},
				When:    map[string]interface{}{"language": "python"},
			},
			{
				ID:      "b3",
				Content: models.BehaviorContent{Canonical: "use pathlib always"},
				When:    map[string]interface{}{"language": "python"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DeduplicatorConfig{
			SimilarityThreshold: 0.5,
		})

		matches, err := dedup.FindDuplicates(ctx, &behaviors[0])
		if err != nil {
			t.Fatalf("FindDuplicates failed: %v", err)
		}

		// Check that matches are sorted by similarity (descending)
		for i := 1; i < len(matches); i++ {
			if matches[i].Similarity > matches[i-1].Similarity {
				t.Errorf("matches not sorted: %f > %f at position %d",
					matches[i].Similarity, matches[i-1].Similarity, i)
			}
		}
	})
}

func TestStoreDeduplicator_MergeDuplicates(t *testing.T) {
	ctx := context.Background()

	t.Run("merges behaviors and removes duplicates", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Name:    "Primary",
				Content: models.BehaviorContent{Canonical: "use pathlib"},
			},
			{
				ID:      "b2",
				Name:    "Duplicate",
				Content: models.BehaviorContent{Canonical: "use pathlib module"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DefaultConfig())

		matches := []DuplicateMatch{
			{Behavior: &behaviors[1], Similarity: 0.95},
		}

		merged, err := dedup.MergeDuplicates(ctx, matches, &behaviors[0])
		if err != nil {
			t.Fatalf("MergeDuplicates failed: %v", err)
		}

		if merged == nil {
			t.Fatal("expected merged behavior, got nil")
		}

		// Check duplicate was deleted
		node, _ := s.GetNode(ctx, "b2")
		if node != nil {
			t.Error("expected b2 to be deleted after merge")
		}
	})

	t.Run("returns primary when no matches", func(t *testing.T) {
		primary := &models.Behavior{ID: "b1", Name: "Primary"}

		s := store.NewInMemoryGraphStore()
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DefaultConfig())

		merged, err := dedup.MergeDuplicates(ctx, []DuplicateMatch{}, primary)
		if err != nil {
			t.Fatalf("MergeDuplicates failed: %v", err)
		}

		if merged != primary {
			t.Error("expected primary to be returned when no matches")
		}
	})
}

func TestStoreDeduplicator_DeduplicateStore(t *testing.T) {
	ctx := context.Background()

	t.Run("reports duplicates without merging when auto-merge disabled", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Content: models.BehaviorContent{Canonical: "use pathlib always"},
				When:    map[string]interface{}{"language": "python"},
			},
			{
				ID:      "b2",
				Content: models.BehaviorContent{Canonical: "use pathlib always"},
				When:    map[string]interface{}{"language": "python"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DeduplicatorConfig{
			SimilarityThreshold: 0.9,
			AutoMerge:           false,
		})

		report, err := dedup.DeduplicateStore(ctx, s)
		if err != nil {
			t.Fatalf("DeduplicateStore failed: %v", err)
		}

		if report.TotalBehaviors != 2 {
			t.Errorf("expected 2 total behaviors, got %d", report.TotalBehaviors)
		}

		if report.DuplicatesFound == 0 {
			t.Error("expected to find duplicates")
		}

		if report.MergesPerformed != 0 {
			t.Errorf("expected 0 merges (auto-merge disabled), got %d", report.MergesPerformed)
		}
	})

	t.Run("merges duplicates when auto-merge enabled", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Content: models.BehaviorContent{Canonical: "use pathlib always"},
				When:    map[string]interface{}{"language": "python"},
			},
			{
				ID:      "b2",
				Content: models.BehaviorContent{Canonical: "use pathlib always"},
				When:    map[string]interface{}{"language": "python"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DeduplicatorConfig{
			SimilarityThreshold: 0.9,
			AutoMerge:           true,
		})

		report, err := dedup.DeduplicateStore(ctx, s)
		if err != nil {
			t.Fatalf("DeduplicateStore failed: %v", err)
		}

		if report.DuplicatesFound == 0 {
			t.Error("expected to find duplicates")
		}

		if report.MergesPerformed == 0 {
			t.Error("expected merges to be performed")
		}

		if len(report.MergedBehaviors) == 0 {
			t.Error("expected merged behaviors in report")
		}
	})

	t.Run("handles empty store", func(t *testing.T) {
		s := store.NewInMemoryGraphStore()
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DefaultConfig())

		report, err := dedup.DeduplicateStore(ctx, s)
		if err != nil {
			t.Fatalf("DeduplicateStore failed: %v", err)
		}

		if report.TotalBehaviors != 0 {
			t.Errorf("expected 0 behaviors, got %d", report.TotalBehaviors)
		}

		if report.DuplicatesFound != 0 {
			t.Errorf("expected 0 duplicates, got %d", report.DuplicatesFound)
		}
	})

	t.Run("handles single behavior", func(t *testing.T) {
		behaviors := []models.Behavior{
			{
				ID:      "b1",
				Content: models.BehaviorContent{Canonical: "unique content"},
			},
		}

		s := createTestStore(behaviors)
		merger := NewBehaviorMerger(MergerConfig{})
		dedup := NewStoreDeduplicator(s, merger, DefaultConfig())

		report, err := dedup.DeduplicateStore(ctx, s)
		if err != nil {
			t.Fatalf("DeduplicateStore failed: %v", err)
		}

		if report.TotalBehaviors != 1 {
			t.Errorf("expected 1 behavior, got %d", report.TotalBehaviors)
		}

		if report.DuplicatesFound != 0 {
			t.Errorf("expected 0 duplicates, got %d", report.DuplicatesFound)
		}
	})
}

func TestStoreDeduplicator_ComputeSimilarity(t *testing.T) {
	dedup := &StoreDeduplicator{}

	t.Run("identical behaviors score 1.0", func(t *testing.T) {
		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}

		sim := dedup.computeSimilarity(a, b)
		if sim.score != 1.0 {
			t.Errorf("expected similarity 1.0, got %f", sim.score)
		}
		if sim.method != "jaccard" {
			t.Errorf("expected method 'jaccard', got %q", sim.method)
		}
	})

	t.Run("completely different behaviors score low", func(t *testing.T) {
		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "run tests"},
			When:    map[string]interface{}{"task": "commit"},
		}

		sim := dedup.computeSimilarity(a, b)
		if sim.score > 0.5 {
			t.Errorf("expected low similarity, got %f", sim.score)
		}
	})

	t.Run("weighs content higher than when conditions", func(t *testing.T) {
		// Same content, different when
		sameContent := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		sameContentDiffWhen := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "go"},
		}

		// Different content, same when
		diffContentSameWhen := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "run tests"},
			When:    map[string]interface{}{"language": "python"},
		}

		simSameContent := dedup.computeSimilarity(sameContent, sameContentDiffWhen)
		simSameWhen := dedup.computeSimilarity(sameContent, diffContentSameWhen)

		// Same content should score higher than same when
		if simSameContent.score <= simSameWhen.score {
			t.Errorf("same content (%f) should score higher than same when (%f)",
				simSameContent.score, simSameWhen.score)
		}
	})
}

func TestBehaviorToNode(t *testing.T) {
	behavior := &models.Behavior{
		ID:         "b1",
		Name:       "Test Behavior",
		Kind:       models.BehaviorKindDirective,
		Confidence: 0.85,
		Priority:   5,
		Content: models.BehaviorContent{
			Canonical: "test content",
		},
		When: map[string]interface{}{
			"language": "python",
		},
	}

	node := models.BehaviorToNode(behavior)

	if node.ID != "b1" {
		t.Errorf("expected ID b1, got %s", node.ID)
	}
	if node.Kind != "behavior" {
		t.Errorf("expected Kind 'behavior', got %s", node.Kind)
	}
	if node.Content["name"] != "Test Behavior" {
		t.Errorf("expected name 'Test Behavior', got %v", node.Content["name"])
	}
	if node.Content["kind"] != "directive" {
		t.Errorf("expected kind 'directive', got %v", node.Content["kind"])
	}
	if node.Metadata["confidence"] != 0.85 {
		t.Errorf("expected confidence 0.85, got %v", node.Metadata["confidence"])
	}
	if node.Metadata["priority"] != 5 {
		t.Errorf("expected priority 5, got %v", node.Metadata["priority"])
	}
}

func TestNewStoreDeduplicatorWithLLM(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	merger := NewBehaviorMerger(MergerConfig{})
	config := DeduplicatorConfig{
		SimilarityThreshold: 0.9,
		UseLLM:              true,
	}
	mockClient := llm.NewMockClient()

	dedup := NewStoreDeduplicatorWithLLM(s, merger, config, mockClient)

	if dedup.store != s {
		t.Error("expected store to be set")
	}
	if dedup.llmClient != mockClient {
		t.Error("expected LLM client to be set")
	}
	if !dedup.config.UseLLM {
		t.Error("expected UseLLM to be true")
	}
}

func TestStoreDeduplicator_ComputeSimilarity_WithLLM(t *testing.T) {
	t.Run("uses LLM when available and configured", func(t *testing.T) {
		mockClient := llm.NewMockClient().
			WithComparisonResult(&llm.ComparisonResult{
				SemanticSimilarity: 0.95,
				IntentMatch:        true,
				MergeCandidate:     true,
			})

		dedup := &StoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: mockClient,
		}

		a := &models.Behavior{Content: models.BehaviorContent{Canonical: "use pathlib"}}
		b := &models.Behavior{Content: models.BehaviorContent{Canonical: "prefer pathlib"}}

		sim := dedup.computeSimilarity(a, b)

		if sim.score != 0.95 {
			t.Errorf("expected LLM similarity 0.95, got %f", sim.score)
		}
		if sim.method != "llm" {
			t.Errorf("expected method 'llm', got %q", sim.method)
		}

		if mockClient.CompareCallCount() != 1 {
			t.Errorf("expected 1 LLM call, got %d", mockClient.CompareCallCount())
		}
	})

	t.Run("falls back to Jaccard when LLM disabled", func(t *testing.T) {
		mockClient := llm.NewMockClient().
			WithComparisonResult(&llm.ComparisonResult{
				SemanticSimilarity: 0.95,
			})

		dedup := &StoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: false}, // Disabled
			llmClient: mockClient,
		}

		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}

		sim := dedup.computeSimilarity(a, b)

		// Should use Jaccard (identical = 1.0)
		if sim.score != 1.0 {
			t.Errorf("expected Jaccard similarity 1.0, got %f", sim.score)
		}
		if sim.method != "jaccard" {
			t.Errorf("expected method 'jaccard', got %q", sim.method)
		}

		// LLM should not have been called
		if mockClient.CompareCallCount() != 0 {
			t.Errorf("expected 0 LLM calls, got %d", mockClient.CompareCallCount())
		}
	})

	t.Run("falls back to Jaccard when LLM unavailable", func(t *testing.T) {
		mockClient := llm.NewMockClient().
			WithAvailable(false).
			WithComparisonResult(&llm.ComparisonResult{
				SemanticSimilarity: 0.95,
			})

		dedup := &StoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: mockClient,
		}

		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}

		sim := dedup.computeSimilarity(a, b)

		// Should use Jaccard
		if sim.score != 1.0 {
			t.Errorf("expected Jaccard similarity 1.0, got %f", sim.score)
		}
		if sim.method != "jaccard" {
			t.Errorf("expected method 'jaccard', got %q", sim.method)
		}

		// LLM should not have been called
		if mockClient.CompareCallCount() != 0 {
			t.Errorf("expected 0 LLM calls, got %d", mockClient.CompareCallCount())
		}
	})

	t.Run("falls back to Jaccard on LLM error", func(t *testing.T) {
		mockClient := llm.NewMockClient().
			WithError(errors.New("LLM error"))

		dedup := &StoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: mockClient,
		}

		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}

		sim := dedup.computeSimilarity(a, b)

		// Should fall back to Jaccard
		if sim.score != 1.0 {
			t.Errorf("expected Jaccard similarity 1.0, got %f", sim.score)
		}
		if sim.method != "jaccard" {
			t.Errorf("expected method 'jaccard', got %q", sim.method)
		}

		// LLM was called but failed
		if mockClient.CompareCallCount() != 1 {
			t.Errorf("expected 1 LLM call, got %d", mockClient.CompareCallCount())
		}
	})

	t.Run("handles nil LLM client", func(t *testing.T) {
		dedup := &StoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: nil, // No client
		}

		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "hello world"},
			When:    map[string]interface{}{},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "hello world"},
			When:    map[string]interface{}{},
		}

		// Should not panic and use Jaccard
		sim := dedup.computeSimilarity(a, b)

		if sim.score != 1.0 {
			t.Errorf("expected Jaccard similarity 1.0, got %f", sim.score)
		}
		if sim.method != "jaccard" {
			t.Errorf("expected method 'jaccard', got %q", sim.method)
		}
	})
}

func TestStoreDeduplicator_ComputeSimilarity_LogsDecision(t *testing.T) {
	dir := t.TempDir()
	dl := logging.NewDecisionLogger(dir, "debug")
	defer dl.Close()

	dedup := &StoreDeduplicator{
		config:    DeduplicatorConfig{SimilarityThreshold: 0.8},
		logger:    logging.NewLogger("debug", os.Stderr),
		decisions: dl,
	}

	a := &models.Behavior{
		ID:      "a1",
		Content: models.BehaviorContent{Canonical: "use pathlib for file paths"},
		When:    map[string]interface{}{"language": "python"},
	}
	b := &models.Behavior{
		ID:      "b1",
		Content: models.BehaviorContent{Canonical: "use pathlib for file paths"},
		When:    map[string]interface{}{"language": "python"},
	}

	sim := dedup.computeSimilarity(a, b)
	if sim.score == 0 {
		t.Error("expected non-zero similarity")
	}

	// Read decisions.jsonl
	data, err := os.ReadFile(filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("failed to read decisions.jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	found := false
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event["event"] == "similarity_computed" {
			found = true
			if event["behavior_a"] != "a1" {
				t.Errorf("expected behavior_a=a1, got %v", event["behavior_a"])
			}
			if event["behavior_b"] != "b1" {
				t.Errorf("expected behavior_b=b1, got %v", event["behavior_b"])
			}
			if event["method"] != "jaccard" {
				t.Errorf("expected method=jaccard, got %v", event["method"])
			}
			if _, ok := event["score"]; !ok {
				t.Error("expected score field")
			}
			if _, ok := event["threshold"]; !ok {
				t.Error("expected threshold field")
			}
			if _, ok := event["is_duplicate"]; !ok {
				t.Error("expected is_duplicate field")
			}
		}
	}
	if !found {
		t.Errorf("expected similarity_computed event, got:\n%s", string(data))
	}
}

func TestStoreDeduplicator_ComputeSimilarity_NilDecisionLogger(t *testing.T) {
	dedup := &StoreDeduplicator{
		config: DeduplicatorConfig{SimilarityThreshold: 0.8},
		// No logger or decisions - should not panic
	}

	a := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "use pathlib"},
		When:    map[string]interface{}{"language": "python"},
	}
	b := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "use pathlib"},
		When:    map[string]interface{}{"language": "python"},
	}

	// Should not panic
	sim := dedup.computeSimilarity(a, b)
	if sim.score != 1.0 {
		t.Errorf("expected 1.0, got %f", sim.score)
	}
}
