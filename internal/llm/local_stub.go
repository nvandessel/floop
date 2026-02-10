//go:build !llamacpp

package llm

import (
	"context"
	"fmt"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// LocalClient is a stub implementation used when the llamacpp build tag is not set.
// It returns Available()=false so callers fall back to other providers.
type LocalClient struct {
	modelPath          string
	embeddingModelPath string
}

// LocalConfig configures the local LLM client.
type LocalConfig struct {
	// ModelPath is the path to the GGUF model file for text generation.
	ModelPath string

	// EmbeddingModelPath is the path to the GGUF model file for embeddings.
	// If empty, ModelPath is used for embeddings as well.
	EmbeddingModelPath string

	// GPULayers is the number of layers to offload to GPU (0 = CPU only).
	GPULayers int

	// ContextSize is the context window size in tokens.
	ContextSize int
}

// NewLocalClient creates a new LocalClient. In the stub build (without llamacpp tag),
// this client is always unavailable.
func NewLocalClient(cfg LocalConfig) *LocalClient {
	return &LocalClient{
		modelPath:          cfg.ModelPath,
		embeddingModelPath: cfg.EmbeddingModelPath,
	}
}

// CompareBehaviors returns an error because the local client is not available
// in stub builds.
func (c *LocalClient) CompareBehaviors(_ context.Context, _, _ *models.Behavior) (*ComparisonResult, error) {
	return nil, fmt.Errorf("local LLM not available: build with -tags llamacpp")
}

// MergeBehaviors returns an error because the local client is not available
// in stub builds.
func (c *LocalClient) MergeBehaviors(_ context.Context, _ []*models.Behavior) (*MergeResult, error) {
	return nil, fmt.Errorf("local LLM not available: build with -tags llamacpp")
}

// Available returns false because the local LLM is not compiled in without
// the llamacpp build tag.
func (c *LocalClient) Available() bool {
	return false
}

// Embed returns an error because the local client is not available in stub builds.
func (c *LocalClient) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("local LLM not available: build with -tags llamacpp")
}

// CompareEmbeddings returns an error because the local client is not available
// in stub builds.
func (c *LocalClient) CompareEmbeddings(_ context.Context, _, _ string) (float64, error) {
	return 0, fmt.Errorf("local LLM not available: build with -tags llamacpp")
}

// Close is a no-op for the stub client.
func (c *LocalClient) Close() error {
	return nil
}
