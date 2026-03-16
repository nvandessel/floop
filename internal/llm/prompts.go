// Package llm provides interfaces and types for LLM-based text completion.
package llm

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ExtractJSON extracts JSON content from a string, handling markdown code blocks.
// It looks for JSON wrapped in ```json...``` or ```...``` blocks, or returns
// the input if it appears to be raw JSON.
func ExtractJSON(s string) string {
	s = strings.TrimSpace(s)

	// Try to extract from markdown code block with json language tag
	jsonBlockRe := regexp.MustCompile("(?s)```json\\s*\\n?(.*?)\\s*```")
	if matches := jsonBlockRe.FindStringSubmatch(s); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Try to extract from generic markdown code block
	genericBlockRe := regexp.MustCompile("(?s)```\\s*\\n?(.*?)\\s*```")
	if matches := genericBlockRe.FindStringSubmatch(s); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Check if the string itself looks like JSON (starts with { or [)
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		return s
	}

	return ""
}

// ToJSONArray converts a string slice to a JSON array string.
func ToJSONArray(items []string) string {
	bytes, _ := json.Marshal(items)
	return string(bytes)
}
