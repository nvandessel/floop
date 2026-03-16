// Package llm provides interfaces and types for LLM-based text completion.
package llm

import (
	"context"
	"sync"
)

// MockClient implements Client and EmbeddingComparer for testing purposes.
// It allows configuring responses for Complete, Embed, and CompareEmbeddings,
// simulating errors, and tracking calls for verification.
type MockClient struct {
	mu sync.Mutex

	// Configured responses
	completeResponse string
	err              error
	available        bool

	// Embedding configured responses
	embedResult      []float32
	embedErr         error
	compareEmbResult float64
	compareEmbErr    error

	// Call tracking
	CompleteCalls []CompleteCall
	EmbedCalls    []string
}

// CompleteCall records a call to Complete.
type CompleteCall struct {
	Messages []Message
}

// NewMockClient creates a new MockClient with default settings.
// By default, it is available and returns empty string responses.
func NewMockClient() *MockClient {
	return &MockClient{
		available:     true,
		CompleteCalls: make([]CompleteCall, 0),
		EmbedCalls:    make([]string, 0),
	}
}

// WithCompleteResponse configures the response returned by Complete.
func (m *MockClient) WithCompleteResponse(response string) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeResponse = response
	return m
}

// WithError configures the error returned by all methods.
func (m *MockClient) WithError(err error) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
	return m
}

// WithAvailable configures whether Available() returns true or false.
func (m *MockClient) WithAvailable(available bool) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.available = available
	return m
}

// WithEmbedResult configures the embedding vector returned by Embed.
func (m *MockClient) WithEmbedResult(v []float32) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.embedResult = v
	return m
}

// WithEmbedError configures the error returned by Embed.
func (m *MockClient) WithEmbedError(err error) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.embedErr = err
	return m
}

// WithCompareEmbeddingsResult configures the similarity returned by CompareEmbeddings.
func (m *MockClient) WithCompareEmbeddingsResult(sim float64) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compareEmbResult = sim
	return m
}

// WithCompareEmbeddingsError configures the error returned by CompareEmbeddings.
func (m *MockClient) WithCompareEmbeddingsError(err error) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compareEmbErr = err
	return m
}

// Embed implements EmbeddingComparer.Embed.
// It records the call and returns the configured result or error.
func (m *MockClient) Embed(_ context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.EmbedCalls = append(m.EmbedCalls, text)

	if m.embedErr != nil {
		return nil, m.embedErr
	}

	if m.embedResult != nil {
		return m.embedResult, nil
	}

	// Default: return a zero vector
	return []float32{0, 0, 0}, nil
}

// CompareEmbeddings implements EmbeddingComparer.CompareEmbeddings.
// It returns the configured similarity or error.
func (m *MockClient) CompareEmbeddings(_ context.Context, a, b string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Track both texts as embed calls for completeness
	m.EmbedCalls = append(m.EmbedCalls, a, b)

	if m.compareEmbErr != nil {
		return 0, m.compareEmbErr
	}

	return m.compareEmbResult, nil
}

// Complete implements Client.Complete.
// It records the call and returns the configured response or error.
func (m *MockClient) Complete(_ context.Context, messages []Message) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CompleteCalls = append(m.CompleteCalls, CompleteCall{Messages: messages})

	if m.err != nil {
		return "", m.err
	}

	return m.completeResponse, nil
}

// Available implements Client.Available.
// Returns the configured availability status.
func (m *MockClient) Available() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.available
}

// Reset clears all call tracking and resets configured responses.
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeResponse = ""
	m.err = nil
	m.available = true
	m.embedResult = nil
	m.embedErr = nil
	m.compareEmbResult = 0
	m.compareEmbErr = nil
	m.CompleteCalls = make([]CompleteCall, 0)
	m.EmbedCalls = make([]string, 0)
}

// CompleteCallCount returns the number of times Complete was called.
func (m *MockClient) CompleteCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.CompleteCalls)
}

// EmbedCallCount returns the number of texts passed to Embed or CompareEmbeddings.
func (m *MockClient) EmbedCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.EmbedCalls)
}

// Compile-time interface assertions.
var (
	_ Client            = (*MockClient)(nil)
	_ EmbeddingComparer = (*MockClient)(nil)
)
