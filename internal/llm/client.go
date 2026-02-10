// Package llm provides interfaces and types for LLM-based behavior comparison and merging.
// It supports multiple backends including Anthropic, OpenAI, native CLI subagents, and
// a fallback rule-based implementation.
package llm

import (
	"context"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// ComparisonResult contains the result of comparing two behaviors using an LLM.
type ComparisonResult struct {
	// SemanticSimilarity is a score between 0.0 and 1.0 indicating how similar
	// the behaviors are in meaning, not just word overlap.
	SemanticSimilarity float64 `json:"semantic_similarity" yaml:"semantic_similarity"`

	// IntentMatch indicates whether the behaviors express the same underlying intent,
	// even if worded differently.
	IntentMatch bool `json:"intent_match" yaml:"intent_match"`

	// MergeCandidate indicates whether the behaviors are similar enough to be merged.
	MergeCandidate bool `json:"merge_candidate" yaml:"merge_candidate"`

	// Reasoning contains the LLM's explanation for its assessment.
	Reasoning string `json:"reasoning,omitempty" yaml:"reasoning,omitempty"`
}

// MergeResult contains the result of merging multiple behaviors using an LLM.
type MergeResult struct {
	// Merged is the new behavior combining the source behaviors.
	// May be nil if the LLM returns an empty or invalid response.
	Merged *models.Behavior `json:"merged" yaml:"merged"`

	// SourceIDs contains the IDs of the behaviors that were merged.
	SourceIDs []string `json:"source_ids" yaml:"source_ids"`

	// Reasoning contains the LLM's explanation for how it merged the behaviors.
	Reasoning string `json:"reasoning,omitempty" yaml:"reasoning,omitempty"`
}

// ClientConfig configures an LLM client.
type ClientConfig struct {
	// Provider identifies the LLM backend: "anthropic", "openai", "ollama", "subagent", "fallback"
	Provider string `json:"provider" yaml:"provider"`

	// APIKey is the API key for the provider (not used for subagent, fallback, or ollama).
	APIKey string `json:"api_key,omitempty" yaml:"api_key,omitempty"`

	// BaseURL is the API endpoint URL. Used for ollama or custom OpenAI-compatible endpoints.
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`

	// Model is the model identifier to use for requests.
	Model string `json:"model,omitempty" yaml:"model,omitempty"`

	// Timeout is the maximum duration to wait for a response.
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`

	// FallbackToRules indicates whether to fall back to rule-based comparison
	// if the LLM is unavailable or fails.
	FallbackToRules bool `json:"fallback_to_rules,omitempty" yaml:"fallback_to_rules,omitempty"`
}

// DefaultConfig returns a ClientConfig with sensible defaults.
func DefaultConfig() ClientConfig {
	return ClientConfig{
		Provider:        "fallback",
		Model:           "",
		Timeout:         30 * time.Second,
		FallbackToRules: true,
	}
}

// Client defines the interface for LLM-based behavior operations.
type Client interface {
	// CompareBehaviors semantically compares two behaviors and returns a detailed result.
	// This is more accurate than Jaccard similarity as it understands meaning, not just words.
	CompareBehaviors(ctx context.Context, a, b *models.Behavior) (*ComparisonResult, error)

	// MergeBehaviors combines multiple similar behaviors into a single unified behavior.
	// The LLM synthesizes the content, preserving the key information from all sources.
	MergeBehaviors(ctx context.Context, behaviors []*models.Behavior) (*MergeResult, error)

	// Available returns true if the client is configured and ready to handle requests.
	// For API-based clients, this checks that credentials are present.
	// For subagent clients, this checks that the CLI tool is available.
	Available() bool
}

// EmbeddingComparer is an optional interface that Client implementations may support
// for embedding-based similarity comparison. Consumers should type-assert to check
// for support: if ec, ok := client.(EmbeddingComparer); ok { ... }
type EmbeddingComparer interface {
	// Embed returns a dense vector embedding for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// CompareEmbeddings embeds both texts and returns their cosine similarity.
	// Returns a value between -1.0 and 1.0 (typically 0.0 to 1.0 for normalized embeddings).
	CompareEmbeddings(ctx context.Context, a, b string) (float64, error)
}

// Closer is an optional interface for clients that hold resources requiring cleanup.
// Consumers should type-assert and call Close when done: if c, ok := client.(Closer); ok { c.Close() }
type Closer interface {
	Close() error
}
