// Package llm provides interfaces and types for LLM-based text completion.
// It supports multiple backends including Anthropic, OpenAI, native CLI subagents,
// and local GGUF models.
package llm

import (
	"context"
	"time"
)

// Message represents a single message in a chat completion request.
type Message struct {
	Role    string // "system", "user", "assistant"
	Content string
}

// ClientConfig configures an LLM client.
type ClientConfig struct {
	// Provider identifies the LLM backend: "anthropic", "openai", "ollama", "subagent"
	Provider string `json:"provider" yaml:"provider"`

	// APIKey is the API key for the provider (not used for subagent or ollama).
	APIKey string `json:"api_key,omitempty" yaml:"api_key,omitempty"`

	// BaseURL is the API endpoint URL. Used for ollama or custom OpenAI-compatible endpoints.
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`

	// Model is the model identifier to use for requests.
	Model string `json:"model,omitempty" yaml:"model,omitempty"`

	// Timeout is the maximum duration to wait for a response.
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// DefaultConfig returns a ClientConfig with sensible defaults.
func DefaultConfig() ClientConfig {
	return ClientConfig{
		Provider: "",
		Model:    "",
		Timeout:  30 * time.Second,
	}
}

// Client defines the interface for LLM-based text completion.
type Client interface {
	// Complete sends a sequence of messages to the LLM and returns the response text.
	Complete(ctx context.Context, messages []Message) (string, error)

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
