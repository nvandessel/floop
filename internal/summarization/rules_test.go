package summarization

import (
	"strings"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestRuleSummarizer_Summarize_Basic(t *testing.T) {
	s := NewRuleSummarizer(DefaultConfig())

	tests := []struct {
		name     string
		behavior *models.Behavior
		wantLen  int
		contains string
	}{
		{
			name:     "nil behavior",
			behavior: nil,
			wantLen:  0,
		},
		{
			name: "empty content",
			behavior: &models.Behavior{
				ID:      "b1",
				Content: models.BehaviorContent{},
			},
			wantLen: 0,
		},
		{
			name: "short canonical already fits",
			behavior: &models.Behavior{
				ID:      "b2",
				Content: models.BehaviorContent{Canonical: "Use Go modules"},
			},
			contains: "Use Go modules",
		},
		{
			name: "existing summary used",
			behavior: &models.Behavior{
				ID: "b3",
				Content: models.BehaviorContent{
					Canonical: "This is a very long canonical content that exceeds the limit",
					Summary:   "Short summary",
				},
			},
			contains: "Short summary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Summarize(tt.behavior)
			if err != nil {
				t.Errorf("Summarize() error = %v", err)
				return
			}
			if tt.wantLen > 0 && len(got) != tt.wantLen {
				t.Errorf("Summarize() len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.contains != "" && !strings.Contains(got, tt.contains) {
				t.Errorf("Summarize() = %q, want to contain %q", got, tt.contains)
			}
		})
	}
}

func TestRuleSummarizer_Summarize_Constraint(t *testing.T) {
	s := NewRuleSummarizer(DefaultConfig())

	tests := []struct {
		name       string
		canonical  string
		wantPrefix string
	}{
		{
			name:       "never prefix preserved",
			canonical:  "Never commit .env files to the repository",
			wantPrefix: "Never",
		},
		{
			name:       "don't converted to never",
			canonical:  "Don't use global variables",
			wantPrefix: "Never",
		},
		{
			name:       "avoid converted to never",
			canonical:  "Avoid hardcoding credentials in source code",
			wantPrefix: "Never",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &models.Behavior{
				ID:   "test",
				Kind: models.BehaviorKindConstraint,
				Content: models.BehaviorContent{
					Canonical: tt.canonical,
				},
			}
			got, err := s.Summarize(b)
			if err != nil {
				t.Errorf("Summarize() error = %v", err)
				return
			}
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("Summarize() = %q, want prefix %q", got, tt.wantPrefix)
			}
		})
	}
}

func TestRuleSummarizer_Summarize_Preference(t *testing.T) {
	s := NewRuleSummarizer(DefaultConfig())

	tests := []struct {
		name      string
		canonical string
		contains  string
	}{
		{
			name:      "prefer over pattern extracts comparison",
			canonical: "Prefer pathlib over os.path for file operations",
			contains:  " > ",
		},
		{
			name:      "use instead of pattern extracts comparison",
			canonical: "Use context.Context instead of timeouts",
			contains:  " > ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &models.Behavior{
				ID:   "test",
				Kind: models.BehaviorKindPreference,
				Content: models.BehaviorContent{
					Canonical: tt.canonical,
				},
			}
			got, err := s.Summarize(b)
			if err != nil {
				t.Errorf("Summarize() error = %v", err)
				return
			}
			if !strings.Contains(got, tt.contains) {
				t.Errorf("Summarize() = %q, want to contain %q", got, tt.contains)
			}
		})
	}
}

func TestRuleSummarizer_Summarize_Truncation(t *testing.T) {
	config := SummarizerConfig{MaxLength: 30}
	s := NewRuleSummarizer(config)

	b := &models.Behavior{
		ID:   "test",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "This is a very long directive that should be truncated to fit within the maximum length",
		},
	}

	got, err := s.Summarize(b)
	if err != nil {
		t.Errorf("Summarize() error = %v", err)
		return
	}

	if len(got) > 30 {
		t.Errorf("Summarize() len = %d, want <= 30", len(got))
	}

	if !strings.HasSuffix(got, "...") {
		t.Errorf("Summarize() = %q, want to end with '...'", got)
	}
}

func TestRuleSummarizer_Summarize_RemovesFiller(t *testing.T) {
	s := NewRuleSummarizer(DefaultConfig())

	tests := []struct {
		name       string
		canonical  string
		notContain string
	}{
		{
			name:       "removes please",
			canonical:  "Please use consistent naming",
			notContain: "please",
		},
		{
			name:       "removes make sure to",
			canonical:  "Make sure to run tests before committing",
			notContain: "make sure to",
		},
		{
			name:       "removes remember to",
			canonical:  "Remember to update documentation",
			notContain: "remember to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &models.Behavior{
				ID:   "test",
				Kind: models.BehaviorKindDirective,
				Content: models.BehaviorContent{
					Canonical: tt.canonical,
				},
			}
			got, err := s.Summarize(b)
			if err != nil {
				t.Errorf("Summarize() error = %v", err)
				return
			}
			if strings.Contains(strings.ToLower(got), tt.notContain) {
				t.Errorf("Summarize() = %q, should not contain %q", got, tt.notContain)
			}
		})
	}
}

func TestRuleSummarizer_SummarizeBatch(t *testing.T) {
	s := NewRuleSummarizer(DefaultConfig())

	behaviors := []*models.Behavior{
		{ID: "b1", Content: models.BehaviorContent{Canonical: "First behavior"}},
		{ID: "b2", Content: models.BehaviorContent{Canonical: "Second behavior"}},
		nil,
		{ID: "b3", Content: models.BehaviorContent{Canonical: "Third behavior"}},
	}

	results, err := s.SummarizeBatch(behaviors)
	if err != nil {
		t.Errorf("SummarizeBatch() error = %v", err)
		return
	}

	if len(results) != 3 {
		t.Errorf("SummarizeBatch() returned %d results, want 3", len(results))
	}

	for _, id := range []string{"b1", "b2", "b3"} {
		if _, ok := results[id]; !ok {
			t.Errorf("SummarizeBatch() missing result for %s", id)
		}
	}
}

func TestRuleSummarizer_CompressCommonPhrases(t *testing.T) {
	s := &RuleSummarizer{config: DefaultConfig()}

	tests := []struct {
		input string
		want  string
	}{
		{"Use pathlib instead of os.path", "Use pathlib > os.path"},
		{"Check for example the docs", "Check e.g. the docs"},
		{"in order to build", "to build"},
		{"environment variables", "env vars"},
		{"Update the documentation", "Update the docs"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := s.compressCommonPhrases(tt.input)
			if got != tt.want {
				t.Errorf("compressCommonPhrases(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRuleSummarizer_FirstClause(t *testing.T) {
	s := &RuleSummarizer{config: DefaultConfig()}

	tests := []struct {
		input string
		want  string
	}{
		{"Use Go modules. They are better.", "Use Go modules"},
		{"Run tests, then commit", "Run tests"},
		{"Format code; check lint", "Format code"},
		{"Check docs because they matter", "Check docs"},
		{"Single clause only", "Single clause only"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := s.firstClause(tt.input)
			if got != tt.want {
				t.Errorf("firstClause(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewRuleSummarizer_DefaultMaxLength(t *testing.T) {
	// Test that zero MaxLength gets default
	s := NewRuleSummarizer(SummarizerConfig{MaxLength: 0})
	rs := s.(*RuleSummarizer)

	if rs.config.MaxLength != 60 {
		t.Errorf("expected default MaxLength 60, got %d", rs.config.MaxLength)
	}
}
