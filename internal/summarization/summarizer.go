package summarization

import (
	"github.com/nvandessel/feedback-loop/internal/models"
)

// Summarizer generates compressed summaries of behaviors
type Summarizer interface {
	// Summarize generates a short summary for a behavior
	// The summary should be ~60 characters, suitable for quick reference
	Summarize(behavior *models.Behavior) (string, error)

	// SummarizeBatch generates summaries for multiple behaviors
	SummarizeBatch(behaviors []*models.Behavior) (map[string]string, error)
}

// SummarizerConfig holds configuration for summarizers
type SummarizerConfig struct {
	// MaxLength is the maximum length for summaries (default: 60)
	MaxLength int

	// PreservePrefixes keeps kind-specific prefixes like "Never" for constraints
	PreservePrefixes bool
}

// DefaultConfig returns the default summarizer configuration
func DefaultConfig() SummarizerConfig {
	return SummarizerConfig{
		MaxLength:        60,
		PreservePrefixes: true,
	}
}

// NewRuleSummarizer creates a new rule-based summarizer
func NewRuleSummarizer(config SummarizerConfig) Summarizer {
	if config.MaxLength <= 0 {
		config.MaxLength = 60
	}
	return &RuleSummarizer{config: config}
}
