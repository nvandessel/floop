package llm

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/nvandessel/feedback-loop/internal/models"
)

// Package-level library initialization. llama.Load() and llama.Init() are
// process-global operations that must only happen once.
var (
	libOnce    sync.Once
	libLoadErr error
)

func loadLib(libPath string) error {
	libOnce.Do(func() {
		if err := llama.Load(libPath); err != nil {
			libLoadErr = fmt.Errorf("loading yzma shared library from %q: %w", libPath, err)
			return
		}
		llama.LogSet(llama.LogSilent())
		llama.Init()
	})
	return libLoadErr
}

// LocalClient implements Client and EmbeddingComparer using a local GGUF model
// via hybridgroup/yzma (purego). It provides embedding-based similarity comparison
// without external API dependencies. Thread-safe: all model access is serialized
// via mutex. Contexts are created per Embed() call and freed immediately.
type LocalClient struct {
	libPath            string
	embeddingModelPath string
	gpuLayers          int
	contextSize        int

	mu      sync.Mutex
	model   llama.Model
	vocab   llama.Vocab
	nEmbd   int32
	loaded  bool
	loadErr error
	once    sync.Once

	// fallback handles MergeBehaviors until local generation is implemented.
	fallback *FallbackClient
}

// LocalConfig configures the local LLM client.
type LocalConfig struct {
	// LibPath is the directory containing yzma shared libraries (.so/.dylib).
	// Falls back to YZMA_LIB env var at runtime.
	LibPath string

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

// NewLocalClient creates a new LocalClient. The model is not loaded until first use.
func NewLocalClient(cfg LocalConfig) *LocalClient {
	embPath := cfg.EmbeddingModelPath
	if embPath == "" {
		embPath = cfg.ModelPath
	}
	ctxSize := cfg.ContextSize
	if ctxSize <= 0 {
		ctxSize = 512
	}
	libPath := cfg.LibPath
	if libPath == "" {
		libPath = os.Getenv("YZMA_LIB")
	}
	return &LocalClient{
		libPath:            libPath,
		embeddingModelPath: embPath,
		gpuLayers:          cfg.GPULayers,
		contextSize:        ctxSize,
		fallback:           NewFallbackClient(),
	}
}

// resolveLibPath returns the effective library path, falling back to YZMA_LIB.
func (c *LocalClient) resolveLibPath() string {
	if c.libPath != "" {
		return c.libPath
	}
	return os.Getenv("YZMA_LIB")
}

// loadModel lazy-loads the embedding model on first use.
func (c *LocalClient) loadModel() error {
	c.once.Do(func() {
		path := c.embeddingModelPath
		if path == "" {
			c.loadErr = fmt.Errorf("no model path configured")
			return
		}

		libPath := c.resolveLibPath()
		if libPath == "" {
			c.loadErr = fmt.Errorf("no library path configured (set LocalLibPath or YZMA_LIB)")
			return
		}

		if err := loadLib(libPath); err != nil {
			c.loadErr = err
			return
		}

		modelParams := llama.ModelDefaultParams()
		gpuLayers := c.gpuLayers
		if gpuLayers > math.MaxInt32 {
			gpuLayers = math.MaxInt32
		}
		modelParams.NGpuLayers = int32(gpuLayers)

		model, err := llama.ModelLoadFromFile(path, modelParams)
		if err != nil {
			c.loadErr = fmt.Errorf("loading model %s: %w", path, err)
			return
		}
		if model == 0 {
			c.loadErr = fmt.Errorf("loading model %s: returned null handle", path)
			return
		}

		c.model = model
		c.vocab = llama.ModelGetVocab(model)
		c.nEmbd = int32(llama.ModelNEmbd(model))
		c.loaded = true
	})
	return c.loadErr
}

// Available returns true if both the library directory and model file exist on disk.
// This is a cheap check that does not load the model or library.
func (c *LocalClient) Available() bool {
	libPath := c.resolveLibPath()
	if libPath == "" || c.embeddingModelPath == "" {
		return false
	}
	if info, err := os.Stat(libPath); err != nil || !info.IsDir() {
		return false
	}
	_, err := os.Stat(c.embeddingModelPath)
	return err == nil
}

// Embed returns a dense vector embedding for the given text.
// Creates a fresh llama context per call and frees it immediately.
func (c *LocalClient) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := c.loadModel(); err != nil {
		return nil, fmt.Errorf("local embed: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	tokens := llama.Tokenize(c.vocab, text, true, true)

	ctxParams := llama.ContextDefaultParams()
	nTokens := len(tokens) + 64
	if nTokens > math.MaxUint32 {
		nTokens = math.MaxUint32
	}
	ctxParams.NCtx = uint32(nTokens)

	lctx, err := llama.InitFromModel(c.model, ctxParams)
	if err != nil {
		return nil, fmt.Errorf("creating embedding context: %w", err)
	}
	defer func() { _ = llama.Free(lctx) }()

	llama.SetEmbeddings(lctx, true)

	batch := llama.BatchGetOne(tokens)
	if _, err := llama.Decode(lctx, batch); err != nil {
		return nil, fmt.Errorf("decoding tokens: %w", err)
	}

	rawVec, err := llama.GetEmbeddingsSeq(lctx, 0, c.nEmbd)
	if err != nil {
		return nil, fmt.Errorf("getting embeddings: %w", err)
	}

	// Copy + L2 normalize (rawVec points to memory owned by lctx)
	vec := make([]float32, len(rawVec))
	copy(vec, rawVec)
	normalize(vec)

	return vec, nil
}

// CompareEmbeddings embeds both texts and returns their cosine similarity.
func (c *LocalClient) CompareEmbeddings(ctx context.Context, a, b string) (float64, error) {
	embA, err := c.Embed(ctx, a)
	if err != nil {
		return 0, fmt.Errorf("embedding text a: %w", err)
	}
	embB, err := c.Embed(ctx, b)
	if err != nil {
		return 0, fmt.Errorf("embedding text b: %w", err)
	}
	return CosineSimilarity(embA, embB), nil
}

// CompareBehaviors compares two behaviors using embedding-based cosine similarity.
func (c *LocalClient) CompareBehaviors(ctx context.Context, a, b *models.Behavior) (*ComparisonResult, error) {
	similarity, err := c.CompareEmbeddings(ctx, a.Content.Canonical, b.Content.Canonical)
	if err != nil {
		return nil, fmt.Errorf("comparing behaviors: %w", err)
	}

	return &ComparisonResult{
		SemanticSimilarity: similarity,
		IntentMatch:        similarity > 0.8,
		MergeCandidate:     similarity > 0.7,
		Reasoning:          "Local embedding-based cosine similarity comparison",
	}, nil
}

// MergeBehaviors delegates to the rule-based FallbackClient.
// Phase 2 will add generation-based merging with a local text model.
func (c *LocalClient) MergeBehaviors(ctx context.Context, behaviors []*models.Behavior) (*MergeResult, error) {
	return c.fallback.MergeBehaviors(ctx, behaviors)
}

// Close releases the model resources. Safe to call multiple times.
// Does NOT call llama.Close() — that's process-global.
func (c *LocalClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loaded {
		_ = llama.ModelFree(c.model)
		c.model = 0
		c.vocab = 0
		c.nEmbd = 0
		c.loaded = false
		c.once = sync.Once{} // allow reloading after close
	}
	return nil
}
