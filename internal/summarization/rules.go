package summarization

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// Compiled regex patterns (compiled once at package init for performance)
var (
	preferOverRe = regexp.MustCompile(`(?i)prefer\s+(\S+(?:\s+\S+)?)\s+(?:over|instead of|rather than)\s+(\S+(?:\s+\S+)?)`)
	useInsteadRe = regexp.MustCompile(`(?i)use\s+(\S+(?:\s+\S+)?)\s+(?:instead of|rather than)\s+(\S+(?:\s+\S+)?)`)
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// RuleSummarizer implements Summarizer using rule-based compression
type RuleSummarizer struct {
	config SummarizerConfig
}

// Summarize generates a short summary for a behavior using rules
func (s *RuleSummarizer) Summarize(behavior *models.Behavior) (string, error) {
	if behavior == nil {
		return "", nil
	}

	// If summary already exists and is within length, use it
	if behavior.Content.Summary != "" && len(behavior.Content.Summary) <= s.config.MaxLength {
		return behavior.Content.Summary, nil
	}

	// Start with canonical content
	text := behavior.Content.Canonical
	if text == "" {
		text = behavior.Name
	}
	if text == "" {
		return "", nil
	}

	// Apply compression rules
	summary := s.compress(text, behavior.Kind)

	// Truncate if still too long
	summary = s.truncate(summary)

	return summary, nil
}

// SummarizeBatch generates summaries for multiple behaviors
func (s *RuleSummarizer) SummarizeBatch(behaviors []*models.Behavior) (map[string]string, error) {
	results := make(map[string]string, len(behaviors))
	for _, b := range behaviors {
		if b == nil {
			continue
		}
		summary, err := s.Summarize(b)
		if err != nil {
			return nil, err
		}
		results[b.ID] = summary
	}
	return results, nil
}

// compress applies rule-based compression to text
func (s *RuleSummarizer) compress(text string, kind models.BehaviorKind) string {
	result := text

	// Step 1: Remove common filler phrases
	result = s.removeFiller(result)

	// Step 2: Extract key pattern based on kind
	result = s.extractKeyPattern(result, kind)

	// Step 3: Compress common phrases
	result = s.compressCommonPhrases(result)

	// Step 4: Clean up whitespace
	result = s.cleanWhitespace(result)

	return result
}

// removeFiller removes common filler words and phrases
func (s *RuleSummarizer) removeFiller(text string) string {
	fillers := []string{
		"please ",
		"always ",
		"make sure to ",
		"be sure to ",
		"remember to ",
		"don't forget to ",
		"it is important to ",
		"you should ",
		"you must ",
		"we should ",
		"when possible, ",
		"whenever possible, ",
		"if possible, ",
		"in general, ",
		"generally, ",
		"typically, ",
		"usually, ",
		"basically, ",
		"essentially, ",
		"the following ",
		"as follows: ",
	}

	result := text
	lowerResult := strings.ToLower(result)

	for _, filler := range fillers {
		idx := strings.Index(lowerResult, filler)
		if idx != -1 {
			result = result[:idx] + result[idx+len(filler):]
			lowerResult = strings.ToLower(result)
		}
	}

	return result
}

// extractKeyPattern extracts the key action/constraint based on behavior kind
func (s *RuleSummarizer) extractKeyPattern(text string, kind models.BehaviorKind) string {
	switch kind {
	case models.BehaviorKindConstraint:
		return s.extractConstraintPattern(text)
	case models.BehaviorKindPreference:
		return s.extractPreferencePattern(text)
	case models.BehaviorKindDirective:
		return s.extractDirectivePattern(text)
	default:
		return text
	}
}

// extractConstraintPattern extracts "Never X" or "Don't X" patterns
func (s *RuleSummarizer) extractConstraintPattern(text string) string {
	lower := strings.ToLower(text)

	// If it already starts with a constraint word, keep the structure
	constraintPrefixes := []string{"never ", "don't ", "do not ", "avoid ", "no "}
	for _, prefix := range constraintPrefixes {
		if strings.HasPrefix(lower, prefix) {
			// Extract just the action part
			rest := text[len(prefix):]
			// Take first clause (up to comma, semicolon, or period)
			rest = s.firstClause(rest)
			if s.config.PreservePrefixes {
				return "Never " + strings.ToLower(rest)
			}
			return rest
		}
	}

	// Convert to constraint form
	if s.config.PreservePrefixes {
		return "Never " + strings.ToLower(s.firstClause(text))
	}
	return s.firstClause(text)
}

// extractPreferencePattern extracts "Prefer X over Y" patterns
func (s *RuleSummarizer) extractPreferencePattern(text string) string {
	lower := strings.ToLower(text)

	// Look for "prefer X over Y" pattern (using pre-compiled regex)
	if matches := preferOverRe.FindStringSubmatch(text); len(matches) >= 3 {
		return matches[1] + " > " + matches[2]
	}

	// Look for "use X instead of Y" pattern (using pre-compiled regex)
	if matches := useInsteadRe.FindStringSubmatch(text); len(matches) >= 3 {
		return matches[1] + " > " + matches[2]
	}

	// If starts with "prefer", extract the preference
	if strings.HasPrefix(lower, "prefer ") {
		rest := text[7:]
		return "Prefer " + s.firstClause(rest)
	}

	return s.firstClause(text)
}

// extractDirectivePattern extracts directive patterns
func (s *RuleSummarizer) extractDirectivePattern(text string) string {
	// Directives are usually action-oriented, extract first clause
	return s.firstClause(text)
}

// compressCommonPhrases replaces common phrases with shorter versions
func (s *RuleSummarizer) compressCommonPhrases(text string) string {
	replacements := map[string]string{
		"instead of":            ">",
		"rather than":           ">",
		"as opposed to":         ">",
		"for example":           "e.g.",
		"such as":               "e.g.",
		"in order to":           "to",
		"due to the fact":       "because",
		"at this point":         "now",
		"at this time":          "now",
		"in the event of":       "if",
		"in the case of":        "for",
		"with respect to":       "for",
		"with regard to":        "for",
		"on a regular basis":    "regularly",
		"environment variables": "env vars",
		"configuration":         "config",
		"documentation":         "docs",
		"implementation":        "impl",
		"functionality":         "feature",
		"application":           "app",
		"directory":             "dir",
		"repository":            "repo",
	}

	result := text
	for phrase, replacement := range replacements {
		result = strings.ReplaceAll(result, phrase, replacement)
		// Also replace title case version (e.g., "Instead of" -> ">")
		result = strings.ReplaceAll(result, titleCase(phrase), replacement)
	}

	return result
}

// titleCase returns a string with the first letter of each word capitalized
func titleCase(s string) string {
	if s == "" {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(string(w[0])) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

// firstClause extracts the first clause of a sentence
func (s *RuleSummarizer) firstClause(text string) string {
	// Find first delimiter
	delimiters := []string{". ", ", ", "; ", " - ", " — ", " because ", " since ", " when ", " if "}
	minIdx := len(text)

	for _, d := range delimiters {
		idx := strings.Index(strings.ToLower(text), d)
		if idx > 0 && idx < minIdx {
			minIdx = idx
		}
	}

	if minIdx < len(text) {
		return strings.TrimSpace(text[:minIdx])
	}

	return strings.TrimSpace(text)
}

// cleanWhitespace normalizes whitespace in text
func (s *RuleSummarizer) cleanWhitespace(text string) string {
	// Replace multiple spaces with single space (using pre-compiled regex)
	result := whitespaceRe.ReplaceAllString(text, " ")

	// Trim leading/trailing whitespace
	result = strings.TrimSpace(result)

	// Capitalize first letter
	if len(result) > 0 {
		runes := []rune(result)
		runes[0] = unicode.ToUpper(runes[0])
		result = string(runes)
	}

	return result
}

// truncate shortens text to max length with ellipsis
func (s *RuleSummarizer) truncate(text string) string {
	if len(text) <= s.config.MaxLength {
		return text
	}

	// Try to truncate at word boundary
	truncated := text[:s.config.MaxLength-3]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > s.config.MaxLength/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}
