//go:build integration

package llm

import (
	"context"
	"os"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// These tests require yzma shared libraries and a GGUF embedding model.
// Run with:
//   FLOOP_TEST_LIB_PATH=/path/to/yzma/libs \
//   FLOOP_TEST_MODEL_PATH=/path/to/model.gguf \
//   go test -tags integration ./internal/llm/ -v
//
// If YZMA_LIB is set, FLOOP_TEST_LIB_PATH can be omitted.
//
// Recommended models:
//   - all-MiniLM-L6-v2-Q8_0.gguf (~23MB)
//   - nomic-embed-text-v1.5.Q8_0.gguf (~137MB)

func integrationLibPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("FLOOP_TEST_LIB_PATH")
	if path == "" {
		path = os.Getenv("YZMA_LIB")
	}
	if path == "" {
		t.Skip("FLOOP_TEST_LIB_PATH (or YZMA_LIB) not set, skipping integration test")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Skipf("lib path not found or not a directory at %s", path)
	}
	return path
}

func integrationModelPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("FLOOP_TEST_MODEL_PATH")
	if path == "" {
		t.Skip("FLOOP_TEST_MODEL_PATH not set, skipping integration test")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("model file not found at %s: %v", path, err)
	}
	return path
}

func TestLocalClient_Integration_Available(t *testing.T) {
	libPath := integrationLibPath(t)
	modelPath := integrationModelPath(t)

	client := NewLocalClient(LocalConfig{
		LibPath:            libPath,
		EmbeddingModelPath: modelPath,
		ContextSize:        512,
	})
	defer client.Close()

	if !client.Available() {
		t.Error("Available() should return true when lib dir and model file exist")
	}
}

func TestLocalClient_Integration_Embed(t *testing.T) {
	libPath := integrationLibPath(t)
	modelPath := integrationModelPath(t)

	client := NewLocalClient(LocalConfig{
		LibPath:            libPath,
		EmbeddingModelPath: modelPath,
		GPULayers:          0,
		ContextSize:        512,
	})
	defer client.Close()

	ctx := context.Background()
	emb, err := client.Embed(ctx, "The quick brown fox jumps over the lazy dog")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if len(emb) == 0 {
		t.Fatal("Embed() returned empty vector")
	}
	t.Logf("Embedding dimension: %d", len(emb))
}

func TestLocalClient_Integration_CompareEmbeddings(t *testing.T) {
	libPath := integrationLibPath(t)
	modelPath := integrationModelPath(t)

	client := NewLocalClient(LocalConfig{
		LibPath:            libPath,
		EmbeddingModelPath: modelPath,
		GPULayers:          0,
		ContextSize:        512,
	})
	defer client.Close()

	ctx := context.Background()

	tests := []struct {
		name    string
		a       string
		b       string
		wantMin float64
		wantMax float64
	}{
		{
			name:    "identical texts",
			a:       "use pathlib for file paths",
			b:       "use pathlib for file paths",
			wantMin: 0.99,
			wantMax: 1.0,
		},
		{
			name:    "semantically similar",
			a:       "always run tests before committing code",
			b:       "execute the test suite prior to making a commit",
			wantMin: 0.5,
			wantMax: 1.0,
		},
		{
			name:    "semantically different",
			a:       "use pathlib for file paths in Python",
			b:       "the weather is sunny today",
			wantMin: -1.0,
			wantMax: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim, err := client.CompareEmbeddings(ctx, tt.a, tt.b)
			if err != nil {
				t.Fatalf("CompareEmbeddings() error: %v", err)
			}
			t.Logf("similarity = %.4f", sim)
			if sim < tt.wantMin || sim > tt.wantMax {
				t.Errorf("similarity = %.4f, want [%.2f, %.2f]", sim, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestLocalClient_Integration_CompareBehaviors(t *testing.T) {
	libPath := integrationLibPath(t)
	modelPath := integrationModelPath(t)

	client := NewLocalClient(LocalConfig{
		LibPath:            libPath,
		EmbeddingModelPath: modelPath,
		GPULayers:          0,
		ContextSize:        512,
	})
	defer client.Close()

	ctx := context.Background()

	tests := []struct {
		name               string
		aCanonical         string
		bCanonical         string
		wantMergeCandidate bool
	}{
		{
			name:               "near-duplicate behaviors",
			aCanonical:         "Always run go test before committing changes",
			bCanonical:         "Run go test prior to each commit to catch regressions",
			wantMergeCandidate: true,
		},
		{
			name:               "unrelated behaviors",
			aCanonical:         "Use table-driven tests with t.Run",
			bCanonical:         "Never commit secrets or API keys to the repository",
			wantMergeCandidate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := testBehavior("a", tt.aCanonical)
			b := testBehavior("b", tt.bCanonical)

			result, err := client.CompareBehaviors(ctx, a, b)
			if err != nil {
				t.Fatalf("CompareBehaviors() error: %v", err)
			}
			t.Logf("similarity=%.4f intent=%v merge=%v",
				result.SemanticSimilarity, result.IntentMatch, result.MergeCandidate)

			if result.MergeCandidate != tt.wantMergeCandidate {
				t.Errorf("MergeCandidate = %v, want %v (similarity=%.4f)",
					result.MergeCandidate, tt.wantMergeCandidate, result.SemanticSimilarity)
			}
		})
	}
}

func TestLocalClient_Integration_Close(t *testing.T) {
	libPath := integrationLibPath(t)
	modelPath := integrationModelPath(t)

	client := NewLocalClient(LocalConfig{
		LibPath:            libPath,
		EmbeddingModelPath: modelPath,
		GPULayers:          0,
		ContextSize:        512,
	})

	// Load model by using it
	ctx := context.Background()
	_, err := client.Embed(ctx, "test")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}

	// Close should free resources
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	// Double close should be safe
	if err := client.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func testBehavior(id, canonical string) *models.Behavior {
	return &models.Behavior{
		ID:      id,
		Name:    id,
		Kind:    models.BehaviorKindDirective,
		Content: models.BehaviorContent{Canonical: canonical},
	}
}
