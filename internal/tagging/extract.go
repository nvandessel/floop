package tagging

import (
	"regexp"
	"sort"

	"github.com/nvandessel/feedback-loop/internal/sanitize"
)

// MaxTags is the maximum number of tags extracted per behavior.
const MaxTags = 8

// tokenPattern splits text into tokens. Matches words and hyphenated compounds
// like "golangci-lint", "error-handling", "red-green-refactor".
var tokenPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9]*(?:[-_][a-zA-Z0-9]+)*`)

// ExtractTags extracts normalized tags from behavior text using the dictionary.
// Returns a sorted, deduplicated, sanitized slice capped at MaxTags.
// Returns nil if no tags are found.
func ExtractTags(text string, dict *Dictionary) []string {
	if text == "" || dict == nil {
		return nil
	}

	tokens := tokenPattern.FindAllString(text, -1)
	if len(tokens) == 0 {
		return nil
	}

	seen := make(map[string]bool, MaxTags)
	var tags []string

	for _, token := range tokens {
		tag, ok := dict.Lookup(token)
		if !ok {
			continue
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, sanitize.SanitizeBehaviorName(tag))
	}

	if len(tags) == 0 {
		return nil
	}

	sort.Strings(tags)

	if len(tags) > MaxTags {
		tags = tags[:MaxTags]
	}

	return tags
}
