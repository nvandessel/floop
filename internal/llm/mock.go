// Package llm provides interfaces and types for LLM-based behavior comparison and merging.
package llm

import (
	"context"
	"sync"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// MockClient implements Client for testing purposes.
// It allows configuring responses for CompareBehaviors and MergeBehaviors,
// simulating errors, and tracking calls for verification.
type MockClient struct {
	mu sync.Mutex

	// Configured responses
	comparisonResult *ComparisonResult
	mergeResult      *MergeResult
	err              error
	available        bool

	// Call tracking
	CompareCalls []CompareCall
	MergeCalls   []MergeCall
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
	m.CompareCalls = make([]CompareCall, 0)
	m.MergeCalls = make([]MergeCall, 0)
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
