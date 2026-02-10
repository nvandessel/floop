package llm

import (
	"context"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestNewLocalClient(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	if client == nil {
		t.Fatal("NewLocalClient() returned nil")
	}
}

func TestLocalClient_Available(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	if client.Available() {
		t.Error("stub LocalClient.Available() should return false")
	}
}

func TestLocalClient_CompareBehaviors(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	ctx := context.Background()

	a := &models.Behavior{ID: "a", Content: models.BehaviorContent{Canonical: "test"}}
	b := &models.Behavior{ID: "b", Content: models.BehaviorContent{Canonical: "test"}}

	_, err := client.CompareBehaviors(ctx, a, b)
	if err == nil {
		t.Error("stub CompareBehaviors should return error")
	}
}

func TestLocalClient_MergeBehaviors(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	ctx := context.Background()

	behaviors := []*models.Behavior{
		{ID: "a", Content: models.BehaviorContent{Canonical: "test"}},
	}

	_, err := client.MergeBehaviors(ctx, behaviors)
	if err == nil {
		t.Error("stub MergeBehaviors should return error")
	}
}

func TestLocalClient_Embed(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	ctx := context.Background()

	_, err := client.Embed(ctx, "test text")
	if err == nil {
		t.Error("stub Embed should return error")
	}
}

func TestLocalClient_CompareEmbeddings(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	ctx := context.Background()

	_, err := client.CompareEmbeddings(ctx, "text a", "text b")
	if err == nil {
		t.Error("stub CompareEmbeddings should return error")
	}
}

func TestLocalClient_Close(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	if err := client.Close(); err != nil {
		t.Errorf("stub Close() should not return error, got: %v", err)
	}
}

func TestLocalClient_ImplementsInterfaces(t *testing.T) {
	client := NewLocalClient(LocalConfig{})

	// Verify Client interface
	var _ Client = client

	// Verify EmbeddingComparer interface
	var _ EmbeddingComparer = client

	// Verify Closer interface
	var _ Closer = client
}

func TestLocalConfig_Fields(t *testing.T) {
	cfg := LocalConfig{
		ModelPath:          "/path/to/model.gguf",
		EmbeddingModelPath: "/path/to/embed.gguf",
		GPULayers:          32,
		ContextSize:        2048,
	}

	client := NewLocalClient(cfg)
	if client.modelPath != cfg.ModelPath {
		t.Errorf("modelPath = %q, want %q", client.modelPath, cfg.ModelPath)
	}
	if client.embeddingModelPath != cfg.EmbeddingModelPath {
		t.Errorf("embeddingModelPath = %q, want %q", client.embeddingModelPath, cfg.EmbeddingModelPath)
	}
}
