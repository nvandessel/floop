package consolidation

import (
	"context"
	"fmt"

	"github.com/nvandessel/floop/internal/llm"
)

// mockLLMClient is a test double for llm.Client used across test files.
// It supports both simple sequential responses (via responses/errors) and
// detailed call tracking (via calls/callIndex).
type mockLLMClient struct {
	// responses maps call index to response string.
	responses []string
	// errors maps call index to error (nil = success).
	errors []error
	// callIndex tracks the current call number.
	callIndex int
	// calls records messages from each call for inspection.
	calls [][]llm.Message
}

func (m *mockLLMClient) Complete(_ context.Context, messages []llm.Message) (string, error) {
	idx := m.callIndex
	m.callIndex++
	m.calls = append(m.calls, messages)

	if idx < len(m.errors) && m.errors[idx] != nil {
		return "", m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return "{}", nil
}

func (m *mockLLMClient) Available() bool { return true }

// newTestLLMConsolidator creates an LLMConsolidator with default config for tests.
func newTestLLMConsolidator(args ...llm.Client) *LLMConsolidator {
	var client llm.Client
	if len(args) > 0 {
		client = args[0]
	}
	return NewLLMConsolidator(client, nil, DefaultLLMConsolidatorConfig())
}

// newTestLLMConsolidatorWithConfig creates an LLMConsolidator with custom config.
func newTestLLMConsolidatorWithConfig(client llm.Client, config LLMConsolidatorConfig) *LLMConsolidator {
	return NewLLMConsolidator(client, nil, config)
}

// makeCandidates creates n test candidates with sequential IDs.
func makeCandidates(n int) []Candidate {
	candidates := make([]Candidate, n)
	for i := range candidates {
		candidates[i] = Candidate{
			SourceEvents:  []string{fmt.Sprintf("evt-%d", i)},
			RawText:       fmt.Sprintf("Test candidate %d raw text content here", i),
			CandidateType: "correction",
			Confidence:    0.7,
			SessionContext: map[string]any{
				"session_id": "sess-1",
				"project_id": "proj-1",
			},
		}
	}
	return candidates
}
