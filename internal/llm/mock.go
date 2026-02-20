// Package llm provides interfaces and types for LLM-based behavior comparison and merging.
package llm

import (
	"context"
	"sync"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// MockClient implements Client and EmbeddingComparer for testing purposes.
// It allows configuring responses for CompareBehaviors, MergeBehaviors, Embed,
// and CompareEmbeddings, simulating errors, and tracking calls for verification.
type MockClient struct {
	mu sync.Mutex

	// Configured responses
	comparisonResult *ComparisonResult
	mergeResult      *MergeResult
	err              error
	available        bool

	// Embedding configured responses
	embedResult      []float32
	embedErr         error
	compareEmbResult float64
	compareEmbErr    error

	// Call tracking
	CompareCalls []CompareCall
	MergeCalls   []MergeCall
	EmbedCalls   []string
}

// CompareCall records a call to CompareBehaviors.
type CompareCall struct {
	A *models.Behavior
	B *models.Behavior
}

// MergeCall records a call to MergeBehaviors.
type MergeCall struct {
	Behaviors []*models.Behavior
}

// NewMockClient creates a new MockClient with default settings.
// By default, it is available and returns zero-value results.
func NewMockClient() *MockClient {
	return &MockClient{
		available:    true,
		CompareCalls: make([]CompareCall, 0),
		MergeCalls:   make([]MergeCall, 0),
		EmbedCalls:   make([]string, 0),
	}
}

// WithComparisonResult configures the result returned by CompareBehaviors.
func (m *MockClient) WithComparisonResult(result *ComparisonResult) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comparisonResult = result
	return m
}

// WithMergeResult configures the result returned by MergeBehaviors.
func (m *MockClient) WithMergeResult(result *MergeResult) *MockClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mergeResult = result
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
func (m *MockClient) Embed(ctx context.Context, text string) ([]float32, error) {
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
func (m *MockClient) CompareEmbeddings(ctx context.Context, a, b string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Track both texts as embed calls for completeness
	m.EmbedCalls = append(m.EmbedCalls, a, b)

	if m.compareEmbErr != nil {
		return 0, m.compareEmbErr
	}

	return m.compareEmbResult, nil
}

// CompareBehaviors implements Client.CompareBehaviors.
// It records the call and returns the configured result or error.
func (m *MockClient) CompareBehaviors(ctx context.Context, a, b *models.Behavior) (*ComparisonResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CompareCalls = append(m.CompareCalls, CompareCall{A: a, B: b})

	if m.err != nil {
		return nil, m.err
	}

	if m.comparisonResult != nil {
		return m.comparisonResult, nil
	}

	// Default response
	return &ComparisonResult{
		SemanticSimilarity: 0.0,
		IntentMatch:        false,
		MergeCandidate:     false,
	}, nil
}

// MergeBehaviors implements Client.MergeBehaviors.
// It records the call and returns the configured result or error.
func (m *MockClient) MergeBehaviors(ctx context.Context, behaviors []*models.Behavior) (*MergeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.MergeCalls = append(m.MergeCalls, MergeCall{Behaviors: behaviors})

	if m.err != nil {
		return nil, m.err
	}

	if m.mergeResult != nil {
		return m.mergeResult, nil
	}

	// Default response with empty merge
	return &MergeResult{
		Merged:    nil,
		SourceIDs: nil,
	}, nil
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
	m.comparisonResult = nil
	m.mergeResult = nil
	m.err = nil
	m.available = true
	m.embedResult = nil
	m.embedErr = nil
	m.compareEmbResult = 0
	m.compareEmbErr = nil
	m.CompareCalls = make([]CompareCall, 0)
	m.MergeCalls = make([]MergeCall, 0)
	m.EmbedCalls = make([]string, 0)
}

// CompareCallCount returns the number of times CompareBehaviors was called.
func (m *MockClient) CompareCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.CompareCalls)
}

// MergeCallCount returns the number of times MergeBehaviors was called.
func (m *MockClient) MergeCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.MergeCalls)
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
