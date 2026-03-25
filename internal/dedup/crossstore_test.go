package dedup

import (
	"context"
	"errors"
	"testing"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

func addBehaviorNode(t *testing.T, s store.GraphStore, id, name, canonical string, tags []string) {
	t.Helper()
	node := store.Node{
		ID:   id,
		Kind: store.NodeKindBehavior,
		Content: map[string]interface{}{
			"kind": "preference",
			"name": name,
			"content": map[string]interface{}{
				"canonical": canonical,
				"tags":      tags,
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.6,
			"scope":      "global",
		},
	}
	if _, err := s.AddNode(context.Background(), node); err != nil {
		t.Fatalf("failed to add node %s: %v", id, err)
	}
}

func TestCrossStoreDeduplicator_IntraStoreDuplicates(t *testing.T) {
	// Regression test: scope=both must detect duplicates within the same store,
	// not just across stores. These 5 "use logging" behaviors are all in the
	// global store with identical content and tags — they must be flagged.
	localStore := store.NewInMemoryGraphStore()
	globalStore := store.NewInMemoryGraphStore()

	// Add near-identical logging behaviors to the global store (mirrors real data)
	addBehaviorNode(t, globalStore, "b-log-1", "learned/use-logging-instead-of-print-statements",
		"Use logging instead of print statements", []string{"logging"})
	addBehaviorNode(t, globalStore, "b-log-2", "learned/use-logging-instead-of-print",
		"Use logging instead of print", []string{"logging"})
	addBehaviorNode(t, globalStore, "b-log-3", "learned/use-logging-library-instead-of-print",
		"Use logging library instead of print", []string{"logging"})
	addBehaviorNode(t, globalStore, "b-log-4", "learned/using-logging-instead-of-print",
		"Using logging instead of print", []string{"logging"})
	addBehaviorNode(t, globalStore, "b-log-5", "learned/use-logging-not-print",
		"Use logging instead of print statements", []string{"logging"})

	// Add one unrelated local behavior so the local store is non-empty
	addBehaviorNode(t, localStore, "b-local-1", "learned/use-gofmt",
		"Always run gofmt before committing", []string{"go", "formatting"})

	merger := NewBehaviorMerger(MergerConfig{})
	config := DeduplicatorConfig{
		SimilarityThreshold: 0.9,
		AutoMerge:           false, // dry-run equivalent
	}

	deduplicator := NewCrossStoreDeduplicatorWithConfig(localStore, globalStore, merger, config)
	results, err := deduplicator.DeduplicateAcrossStores(context.Background())
	if err != nil {
		t.Fatalf("DeduplicateAcrossStores failed: %v", err)
	}

	// Count intra-store duplicates found (global-vs-global)
	intraStoreDups := 0
	for _, r := range results {
		if r.Action == "merge" {
			intraStoreDups++
		}
	}

	// At minimum, b-log-1 and b-log-5 have identical content — must be found
	if intraStoreDups == 0 {
		t.Errorf("expected intra-store duplicates to be detected in global store, got 0")
		t.Log("Results:")
		for _, r := range results {
			t.Logf("  %s: action=%s", r.LocalBehavior.ID, r.Action)
		}
	}
}

func TestDeduplicationResult_Instantiation(t *testing.T) {
	result := DeduplicationResult{
		LocalBehavior: &models.Behavior{ID: "local-1"},
		Action:        "merge",
		GlobalMatch:   &models.Behavior{ID: "global-1"},
		Similarity:    0.95,
	}

	if result.LocalBehavior.ID != "local-1" {
		t.Errorf("LocalBehavior.ID = %q, want local-1", result.LocalBehavior.ID)
	}
	if result.Action != "merge" {
		t.Errorf("Action = %q, want merge", result.Action)
	}
	if result.Similarity != 0.95 {
		t.Errorf("Similarity = %v, want 0.95", result.Similarity)
	}
}

func TestNewCrossStoreDeduplicator(t *testing.T) {
	merger := NewBehaviorMerger(MergerConfig{})
	dedup := NewCrossStoreDeduplicator(nil, nil, merger)

	if dedup.merger != merger {
		t.Error("expected merger to be set")
	}
	if dedup.config.SimilarityThreshold != DefaultConfig().SimilarityThreshold {
		t.Error("expected default config")
	}
}

func TestNewCrossStoreDeduplicatorWithConfig(t *testing.T) {
	merger := NewBehaviorMerger(MergerConfig{})
	config := DeduplicatorConfig{
		SimilarityThreshold: 0.8,
		AutoMerge:           true,
	}
	dedup := NewCrossStoreDeduplicatorWithConfig(nil, nil, merger, config)

	if dedup.config.SimilarityThreshold != 0.8 {
		t.Errorf("SimilarityThreshold = %v, want 0.8", dedup.config.SimilarityThreshold)
	}
	if !dedup.config.AutoMerge {
		t.Error("expected AutoMerge to be true")
	}
}

func TestNodeToBehavior(t *testing.T) {
	t.Run("basic conversion", func(t *testing.T) {
		node := store.Node{
			ID: "b1",
			Content: map[string]interface{}{
				"kind": "directive",
				"name": "Test Behavior",
				"when": map[string]interface{}{
					"language": "python",
				},
				"content": map[string]interface{}{
					"canonical": "use pathlib",
					"summary":   "pathlib usage",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 0.85,
				"priority":   5,
			},
		}

		b := models.NodeToBehavior(node)

		if b.ID != "b1" {
			t.Errorf("ID = %q, want b1", b.ID)
		}
		if b.Kind != models.BehaviorKindDirective {
			t.Errorf("Kind = %q, want directive", b.Kind)
		}
		if b.Name != "Test Behavior" {
			t.Errorf("Name = %q, want Test Behavior", b.Name)
		}
		if b.Content.Canonical != "use pathlib" {
			t.Errorf("Content.Canonical = %q, want use pathlib", b.Content.Canonical)
		}
		if b.Content.Summary != "pathlib usage" {
			t.Errorf("Content.Summary = %q", b.Content.Summary)
		}
		if b.Confidence != 0.85 {
			t.Errorf("Confidence = %v, want 0.85", b.Confidence)
		}
		if b.When["language"] != "python" {
			t.Errorf("When[language] = %v, want python", b.When["language"])
		}
	})

	t.Run("empty node", func(t *testing.T) {
		node := store.Node{
			ID:       "b1",
			Content:  map[string]interface{}{},
			Metadata: map[string]interface{}{},
		}

		b := models.NodeToBehavior(node)

		if b.ID != "b1" {
			t.Errorf("ID = %q, want b1", b.ID)
		}
		// Other fields should be zero values
		if b.Kind != "" {
			t.Errorf("Kind should be empty, got %q", b.Kind)
		}
		if b.Confidence != 0 {
			t.Errorf("Confidence should be 0, got %v", b.Confidence)
		}
	})

	t.Run("with stats", func(t *testing.T) {
		node := store.Node{
			ID:      "b1",
			Content: map[string]interface{}{},
			Metadata: map[string]interface{}{
				"stats": map[string]interface{}{
					"times_activated":  10,
					"times_followed":   8,
					"times_confirmed":  5,
					"times_overridden": 2,
				},
			},
		}

		b := models.NodeToBehavior(node)

		if b.Stats.TimesActivated != 10 {
			t.Errorf("Stats.TimesActivated = %d, want 10", b.Stats.TimesActivated)
		}
		if b.Stats.TimesFollowed != 8 {
			t.Errorf("Stats.TimesFollowed = %d, want 8", b.Stats.TimesFollowed)
		}
		if b.Stats.TimesConfirmed != 5 {
			t.Errorf("Stats.TimesConfirmed = %d, want 5", b.Stats.TimesConfirmed)
		}
		if b.Stats.TimesOverridden != 2 {
			t.Errorf("Stats.TimesOverridden = %d, want 2", b.Stats.TimesOverridden)
		}
	})
}

func TestCrossStoreDeduplicator_ComputeSimilarity(t *testing.T) {
	dedup := &CrossStoreDeduplicator{}

	t.Run("identical behaviors", func(t *testing.T) {
		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}

		sim := dedup.computeSimilarity(a, b)
		if sim != 1.0 {
			t.Errorf("similarity = %v, want 1.0 for identical behaviors", sim)
		}
	})

	t.Run("different behaviors", func(t *testing.T) {
		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "use pathlib"},
			When:    map[string]interface{}{"language": "python"},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "run tests"},
			When:    map[string]interface{}{"task": "commit"},
		}

		sim := dedup.computeSimilarity(a, b)
		if sim > 0.5 {
			t.Errorf("similarity = %v, expected < 0.5 for different behaviors", sim)
		}
	})
}

func TestNewCrossStoreDeduplicatorWithLLM(t *testing.T) {
	localStore := store.NewInMemoryGraphStore()
	globalStore := store.NewInMemoryGraphStore()
	merger := NewBehaviorMerger(MergerConfig{})
	config := DeduplicatorConfig{
		SimilarityThreshold: 0.9,
		UseLLM:              true,
	}
	mockClient := llm.NewMockClient()

	dedup := NewCrossStoreDeduplicatorWithLLM(localStore, globalStore, merger, config, mockClient)

	if dedup.localStore != localStore {
		t.Error("expected local store to be set")
	}
	if dedup.globalStore != globalStore {
		t.Error("expected global store to be set")
	}
	if dedup.llmClient != mockClient {
		t.Error("expected LLM client to be set")
	}
	if !dedup.config.UseLLM {
		t.Error("expected UseLLM to be true")
	}
}

func TestCrossStoreDeduplicator_ComputeSimilarity_WithLLM(t *testing.T) {
	t.Run("uses embedding when client supports EmbeddingComparer", func(t *testing.T) {
		mockClient := llm.NewMockClient().
			WithCompareEmbeddingsResult(0.92)

		dedup := &CrossStoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: mockClient,
		}

		a := &models.Behavior{Content: models.BehaviorContent{Canonical: "use pathlib"}}
		b := &models.Behavior{Content: models.BehaviorContent{Canonical: "prefer pathlib"}}

		sim := dedup.computeSimilarity(a, b)

		if sim != 0.92 {
			t.Errorf("expected embedding similarity 0.92, got %f", sim)
		}
	})

	t.Run("falls back to LLM when embedding errors", func(t *testing.T) {
		mockClient := llm.NewMockClient().
			WithCompareEmbeddingsError(errors.New("embedding error")).
			WithCompleteResponse(`{"semantic_similarity": 0.92, "intent_match": true, "merge_candidate": true}`)

		dedup := &CrossStoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: mockClient,
		}

		a := &models.Behavior{Content: models.BehaviorContent{Canonical: "use pathlib"}}
		b := &models.Behavior{Content: models.BehaviorContent{Canonical: "prefer pathlib"}}

		sim := dedup.computeSimilarity(a, b)

		if sim != 0.92 {
			t.Errorf("expected LLM similarity 0.92, got %f", sim)
		}

		if mockClient.CompleteCallCount() != 1 {
			t.Errorf("expected 1 LLM call, got %d", mockClient.CompleteCallCount())
		}
	})

	t.Run("falls back to Jaccard when LLM disabled", func(t *testing.T) {
		mockClient := llm.NewMockClient()

		dedup := &CrossStoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: false},
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

		if sim != 1.0 {
			t.Errorf("expected Jaccard similarity 1.0, got %f", sim)
		}

		if mockClient.CompleteCallCount() != 0 {
			t.Errorf("expected 0 LLM calls, got %d", mockClient.CompleteCallCount())
		}
	})

	t.Run("handles nil LLM client gracefully", func(t *testing.T) {
		dedup := &CrossStoreDeduplicator{
			config:    DeduplicatorConfig{UseLLM: true},
			llmClient: nil,
		}

		a := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "hello"},
			When:    map[string]interface{}{},
		}
		b := &models.Behavior{
			Content: models.BehaviorContent{Canonical: "hello"},
			When:    map[string]interface{}{},
		}

		// Should not panic
		sim := dedup.computeSimilarity(a, b)

		if sim != 1.0 {
			t.Errorf("expected Jaccard similarity 1.0, got %f", sim)
		}
	})
}
