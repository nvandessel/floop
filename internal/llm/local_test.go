package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestNewLocalClient(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	if client == nil {
		t.Fatal("NewLocalClient() returned nil")
	}
}

func TestLocalClient_Available_EmptyConfig(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	if client.Available() {
		t.Error("Available() should return false with empty config")
	}
}

func TestLocalClient_Available_MissingLibPath(t *testing.T) {
	t.Setenv("YZMA_LIB", "")
	client := NewLocalClient(LocalConfig{
		EmbeddingModelPath: "/some/model.gguf",
	})
	if client.Available() {
		t.Error("Available() should return false when lib path is missing")
	}
}

func TestLocalClient_Available_MissingModelPath(t *testing.T) {
	client := NewLocalClient(LocalConfig{
		LibPath: "/some/lib/dir",
	})
	if client.Available() {
		t.Error("Available() should return false when model path is missing")
	}
}

func TestLocalClient_Available_LibPathNotDir(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	modelFile := filepath.Join(tmpDir, "model.gguf")
	if err := os.WriteFile(modelFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	client := NewLocalClient(LocalConfig{
		LibPath:            tmpFile, // file, not dir
		EmbeddingModelPath: modelFile,
	})
	if client.Available() {
		t.Error("Available() should return false when lib path is a file, not a directory")
	}
}

func TestLocalClient_Available_BothExist(t *testing.T) {
	tmpDir := t.TempDir()
	libDir := filepath.Join(tmpDir, "lib")
	if err := os.Mkdir(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	modelFile := filepath.Join(tmpDir, "model.gguf")
	if err := os.WriteFile(modelFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	client := NewLocalClient(LocalConfig{
		LibPath:            libDir,
		EmbeddingModelPath: modelFile,
	})
	if !client.Available() {
		t.Error("Available() should return true when both lib dir and model file exist")
	}
}

func TestLocalClient_Available_FallbackToYZMALIB(t *testing.T) {
	tmpDir := t.TempDir()
	libDir := filepath.Join(tmpDir, "lib")
	if err := os.Mkdir(libDir, 0755); err != nil {
		t.Fatal(err)
	}
	modelFile := filepath.Join(tmpDir, "model.gguf")
	if err := os.WriteFile(modelFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("YZMA_LIB", libDir)
	client := NewLocalClient(LocalConfig{
		EmbeddingModelPath: modelFile,
		// LibPath empty — should fall back to YZMA_LIB
	})
	if !client.Available() {
		t.Error("Available() should return true when YZMA_LIB is set and model exists")
	}
}

func TestLocalClient_Embed_NoModelPath(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	ctx := context.Background()

	_, err := client.Embed(ctx, "test text")
	if err == nil {
		t.Error("Embed should return error with no model path")
	}
}

func TestLocalClient_Embed_NoLibPath(t *testing.T) {
	t.Setenv("YZMA_LIB", "")
	client := NewLocalClient(LocalConfig{
		EmbeddingModelPath: "/some/model.gguf",
	})
	ctx := context.Background()

	_, err := client.Embed(ctx, "test text")
	if err == nil {
		t.Error("Embed should return error with no lib path")
	}
}

func TestLocalClient_CompareEmbeddings_NoConfig(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	ctx := context.Background()

	_, err := client.CompareEmbeddings(ctx, "text a", "text b")
	if err == nil {
		t.Error("CompareEmbeddings should return error with no config")
	}
}

func TestLocalClient_CompareBehaviors_NoConfig(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	ctx := context.Background()

	a := &models.Behavior{ID: "a", Content: models.BehaviorContent{Canonical: "test"}}
	b := &models.Behavior{ID: "b", Content: models.BehaviorContent{Canonical: "test"}}

	_, err := client.CompareBehaviors(ctx, a, b)
	if err == nil {
		t.Error("CompareBehaviors should return error with no config")
	}
}

func TestLocalClient_MergeBehaviors(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	ctx := context.Background()

	behaviors := []*models.Behavior{
		{ID: "a", Content: models.BehaviorContent{Canonical: "test a"}},
		{ID: "b", Content: models.BehaviorContent{Canonical: "test b"}},
	}

	result, err := client.MergeBehaviors(ctx, behaviors)
	if err != nil {
		t.Errorf("MergeBehaviors should delegate to fallback, got error: %v", err)
	}
	if result == nil {
		t.Error("MergeBehaviors should return a result from fallback")
	}
}

func TestLocalClient_Close(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	if err := client.Close(); err != nil {
		t.Errorf("Close() should not return error, got: %v", err)
	}
	// Double close should be safe
	if err := client.Close(); err != nil {
		t.Errorf("second Close() should not return error, got: %v", err)
	}
}

func TestLocalClient_ImplementsInterfaces(t *testing.T) {
	client := NewLocalClient(LocalConfig{})

	var _ Client = client
	var _ EmbeddingComparer = client
	var _ Closer = client
}

func TestLocalConfig_Fields(t *testing.T) {
	cfg := LocalConfig{
		LibPath:            "/usr/local/lib/yzma",
		ModelPath:          "/path/to/model.gguf",
		EmbeddingModelPath: "/path/to/embed.gguf",
		GPULayers:          32,
		ContextSize:        2048,
	}

	client := NewLocalClient(cfg)
	if client.embeddingModelPath != cfg.EmbeddingModelPath {
		t.Errorf("embeddingModelPath = %q, want %q", client.embeddingModelPath, cfg.EmbeddingModelPath)
	}
	if client.libPath != cfg.LibPath {
		t.Errorf("libPath = %q, want %q", client.libPath, cfg.LibPath)
	}
}

func TestLocalConfig_FallbackModelPath(t *testing.T) {
	cfg := LocalConfig{
		ModelPath: "/path/to/model.gguf",
	}

	client := NewLocalClient(cfg)
	if client.embeddingModelPath != cfg.ModelPath {
		t.Errorf("embeddingModelPath = %q, want %q (fallback to ModelPath)", client.embeddingModelPath, cfg.ModelPath)
	}
}

func TestLocalConfig_DefaultContextSize(t *testing.T) {
	client := NewLocalClient(LocalConfig{})
	if client.contextSize != 512 {
		t.Errorf("contextSize = %d, want 512 (default)", client.contextSize)
	}
}

func TestLocalConfig_FallbackLibPathFromEnv(t *testing.T) {
	t.Setenv("YZMA_LIB", "/env/lib/yzma")
	client := NewLocalClient(LocalConfig{})
	if client.libPath != "/env/lib/yzma" {
		t.Errorf("libPath = %q, want %q (from YZMA_LIB)", client.libPath, "/env/lib/yzma")
	}
}
