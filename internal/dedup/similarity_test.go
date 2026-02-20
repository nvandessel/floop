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
)

func TestComputeSimilarity_EmbeddingPath(t *testing.T) {
	mock := llm.NewMockClient().
		WithCompareEmbeddingsResult(0.85)

	a := &models.Behavior{
		ID:      "a1",
		Content: models.BehaviorContent{Canonical: "use pathlib for file paths"},
	}
	b := &models.Behavior{
		ID:      "b1",
		Content: models.BehaviorContent{Canonical: "prefer pathlib module for paths"},
	}

	result := ComputeSimilarity(a, b, SimilarityConfig{
		UseLLM:              true,
		LLMClient:           mock,
		SimilarityThreshold: 0.9,
	})

	if result.Method != "embedding" {
		t.Errorf("expected method 'embedding', got %q", result.Method)
	}
	if result.Score != 0.85 {
		t.Errorf("expected score 0.85, got %f", result.Score)
	}
}

func TestComputeSimilarity_LLMFallbackWhenEmbeddingErrors(t *testing.T) {
	mock := llm.NewMockClient().
		WithCompareEmbeddingsError(errors.New("embedding error")).
		WithComparisonResult(&llm.ComparisonResult{
			SemanticSimilarity: 0.92,
		})

	a := &models.Behavior{Content: models.BehaviorContent{Canonical: "use pathlib"}}
	b := &models.Behavior{Content: models.BehaviorContent{Canonical: "prefer pathlib"}}

	result := ComputeSimilarity(a, b, SimilarityConfig{
		UseLLM:              true,
		LLMClient:           mock,
		SimilarityThreshold: 0.9,
	})

	if result.Method != "llm" {
		t.Errorf("expected method 'llm', got %q", result.Method)
	}
	if result.Score != 0.92 {
		t.Errorf("expected score 0.92, got %f", result.Score)
	}
}

func TestComputeSimilarity_JaccardFallbackWhenLLMDisabled(t *testing.T) {
	a := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "use pathlib"},
		When:    map[string]interface{}{"language": "python"},
	}
	b := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "use pathlib"},
		When:    map[string]interface{}{"language": "python"},
	}

	result := ComputeSimilarity(a, b, SimilarityConfig{
		UseLLM:              false,
		SimilarityThreshold: 0.9,
	})

	if result.Method != "jaccard" {
		t.Errorf("expected method 'jaccard', got %q", result.Method)
	}
	if result.Score != 1.0 {
		t.Errorf("expected score 1.0, got %f", result.Score)
	}
}

func TestComputeSimilarity_JaccardFallbackWhenNilClient(t *testing.T) {
	a := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "hello world"},
		When:    map[string]interface{}{},
	}
	b := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "hello world"},
		When:    map[string]interface{}{},
	}

	result := ComputeSimilarity(a, b, SimilarityConfig{
		UseLLM:              true,
		LLMClient:           nil,
		SimilarityThreshold: 0.9,
	})

	if result.Method != "jaccard" {
		t.Errorf("expected method 'jaccard', got %q", result.Method)
	}
}

func TestComputeSimilarity_JaccardFallbackWhenUnavailable(t *testing.T) {
	mock := llm.NewMockClient().WithAvailable(false)

	a := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "hello"},
		When:    map[string]interface{}{},
	}
	b := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "hello"},
		When:    map[string]interface{}{},
	}

	result := ComputeSimilarity(a, b, SimilarityConfig{
		UseLLM:              true,
		LLMClient:           mock,
		SimilarityThreshold: 0.9,
	})

	if result.Method != "jaccard" {
		t.Errorf("expected method 'jaccard', got %q", result.Method)
	}
}

func TestComputeSimilarity_JaccardFallbackWhenLLMErrors(t *testing.T) {
	mock := llm.NewMockClient().
		WithCompareEmbeddingsError(errors.New("embed fail")).
		WithError(errors.New("LLM error"))

	a := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "use pathlib"},
		When:    map[string]interface{}{"language": "python"},
	}
	b := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "use pathlib"},
		When:    map[string]interface{}{"language": "python"},
	}

	result := ComputeSimilarity(a, b, SimilarityConfig{
		UseLLM:              true,
		LLMClient:           mock,
		SimilarityThreshold: 0.9,
	})

	if result.Method != "jaccard" {
		t.Errorf("expected method 'jaccard', got %q", result.Method)
	}
}

func TestComputeSimilarity_NilLoggerDoesNotPanic(t *testing.T) {
	a := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "test"},
	}
	b := &models.Behavior{
		Content: models.BehaviorContent{Canonical: "test"},
	}

	// Should not panic with nil logger and decisions
	result := ComputeSimilarity(a, b, SimilarityConfig{
		SimilarityThreshold: 0.9,
	})

	if result.Score == 0 {
		t.Error("expected non-zero similarity for identical text")
	}
}

func TestComputeSimilarity_DecisionLogging(t *testing.T) {
	dir := t.TempDir()
	dl := logging.NewDecisionLogger(dir, "debug")
	defer dl.Close()

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

	result := ComputeSimilarity(a, b, SimilarityConfig{
		SimilarityThreshold: 0.8,
		Logger:              logging.NewLogger("debug", os.Stderr),
		Decisions:           dl,
	})

	if result.Score == 0 {
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
			if event["method"] != "jaccard" {
				t.Errorf("expected method=jaccard, got %v", event["method"])
			}
		}
	}
	if !found {
		t.Errorf("expected similarity_computed event in decisions log")
	}
}

func TestComputeSimilarity_EmbeddingCache_EmbedOnce(t *testing.T) {
	mock := llm.NewMockClient().
		WithEmbedResult([]float32{1, 0, 0})

	cache := NewEmbeddingCache()

	behaviors := []models.Behavior{
		{ID: "b1", Content: models.BehaviorContent{Canonical: "use pathlib for files"}},
		{ID: "b2", Content: models.BehaviorContent{Canonical: "prefer pathlib module"}},
		{ID: "b3", Content: models.BehaviorContent{Canonical: "always use pathlib"}},
	}

	cfg := SimilarityConfig{
		UseLLM:              true,
		LLMClient:           mock,
		SimilarityThreshold: 0.5,
		EmbeddingCache:      cache,
	}

	// Compare all pairs: (b1,b2), (b1,b3), (b2,b3)
	for i := 0; i < len(behaviors); i++ {
		for j := i + 1; j < len(behaviors); j++ {
			ComputeSimilarity(&behaviors[i], &behaviors[j], cfg)
		}
	}

	// Each canonical text should be embedded exactly once via Embed (not CompareEmbeddings).
	// 3 unique texts = 3 Embed calls.
	if mock.EmbedCallCount() != 3 {
		t.Errorf("expected 3 embed calls (one per unique text), got %d", mock.EmbedCallCount())
	}
}

func TestNewEmbeddingCache(t *testing.T) {
	cache := NewEmbeddingCache()
	if cache == nil {
		t.Fatal("NewEmbeddingCache() returned nil")
	}
}

func TestEmbeddingCache_GetOrCompute(t *testing.T) {
	mock := llm.NewMockClient().
		WithEmbedResult([]float32{0.5, 0.5, 0.5})

	cache := NewEmbeddingCache()
	ctx := context.Background()

	// First call should embed
	vec1, err := cache.GetOrCompute(ctx, mock, "hello world")
	if err != nil {
		t.Fatalf("GetOrCompute failed: %v", err)
	}
	if len(vec1) != 3 {
		t.Errorf("expected 3-dim vector, got %d", len(vec1))
	}

	// Second call with same text should return cached
	vec2, err := cache.GetOrCompute(ctx, mock, "hello world")
	if err != nil {
		t.Fatalf("GetOrCompute (cached) failed: %v", err)
	}

	// Should be same vector
	if vec1[0] != vec2[0] {
		t.Error("expected cached vector to match")
	}

	// Only 1 actual embed call (second was cached)
	if mock.EmbedCallCount() != 1 {
		t.Errorf("expected 1 embed call, got %d", mock.EmbedCallCount())
	}
}

func TestEmbeddingCache_GetOrCompute_Error(t *testing.T) {
	mock := llm.NewMockClient().
		WithEmbedError(errors.New("embed failed"))

	cache := NewEmbeddingCache()
	ctx := context.Background()

	_, err := cache.GetOrCompute(ctx, mock, "hello")
	if err == nil {
		t.Error("expected error from GetOrCompute")
	}
}
