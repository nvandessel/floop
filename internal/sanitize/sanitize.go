// Package sanitize provides content sanitization for behavior data flowing
// through the floop_learn pipeline. It strips control characters, markdown
// hierarchy markers, XML/HTML tags, and excessive backtick sequences to
// prevent stored prompt injection attacks while preserving semantic content.
package sanitize

import (
	"regexp"
	"strings"
)

// MaxContentLength is the maximum allowed length for behavior content.
const MaxContentLength = 2000

// MaxNameLength is the maximum allowed length for behavior names.
const MaxNameLength = 80

// Pre-compiled regular expressions for performance.
var (
	// reXMLTag matches XML/HTML tags including those with attributes and self-closing tags.
	// It also matches XML processing instructions like <?xml ...?>.
	reXMLTag = regexp.MustCompile(`<[/?!]?[a-zA-Z][a-zA-Z0-9]*(?:\s+[^>]*)?/?>|<\?[^?]*\?>`)

	// reMarkdownHeading matches markdown headings at the start of a line (# , ## , etc.).
	reMarkdownHeading = regexp.MustCompile(`(?m)^#{1,6}\s+`)

	// reHorizontalRule matches markdown horizontal rules (---, ***, ___) at the start of a line.
	reHorizontalRule = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)

	// reTripleBacktick matches triple (or more) backtick sequences used in code fences.
	reTripleBacktick = regexp.MustCompile("```+")

	// reExcessiveNewlines matches 3 or more consecutive newlines.
	reExcessiveNewlines = regexp.MustCompile(`\n{3,}`)

	// reRepeatedHyphens matches 2 or more consecutive hyphens.
	reRepeatedHyphens = regexp.MustCompile(`-{2,}`)

	// reRepeatedUnderscores matches 2 or more consecutive underscores.
	reRepeatedUnderscores = regexp.MustCompile(`_{2,}`)
)

// SanitizeBehaviorContent sanitizes behavior content text for safe storage
// and later injection into agent system prompts. It strips control characters,
// markdown headings, horizontal rules, XML/HTML tags, and excessive backticks
// while preserving the semantic meaning of the content.
//
// The sanitization pipeline runs in this order:
//  1. Strip null bytes and ASCII control characters (except \n, \t)
//  2. Strip XML/HTML tags
//  3. Replace markdown headings with list markers
//  4. Remove markdown horizontal rules
//  5. Collapse triple backticks to single backtick
//  6. Collapse excessive newlines (3+ -> 2)
//  7. Truncate to MaxContentLength
//  8. Trim leading/trailing whitespace
func SanitizeBehaviorContent(input string) string {
	if input == "" {
		return ""
	}

	s := input

	// 1. Strip null bytes and ASCII control characters (0x00-0x1F) except \n (0x0A) and \t (0x09).
	s = stripControlChars(s)

	// 2. Strip XML/HTML-like tags.
	s = reXMLTag.ReplaceAllString(s, "")

	// 3. Replace markdown headings with list markers to preserve meaning.
	s = reMarkdownHeading.ReplaceAllString(s, "- ")

	// 4. Remove markdown horizontal rules (entire line).
	s = reHorizontalRule.ReplaceAllString(s, "")

	// 5. Collapse triple backticks to single backtick.
	s = reTripleBacktick.ReplaceAllString(s, "`")

	// 6. Collapse excessive newlines (3+ -> 2).
	s = reExcessiveNewlines.ReplaceAllString(s, "\n\n")

	// 7. Trim leading/trailing whitespace.
	s = strings.TrimSpace(s)

	// 8. Truncate to max length.
	if len(s) > MaxContentLength {
		s = s[:MaxContentLength] + "..."
	}

	return s
}

// SanitizeBehaviorName sanitizes a behavior name, keeping only safe characters
// ([a-zA-Z0-9-_/]) and enforcing a maximum length of MaxNameLength characters.
// Repeated hyphens and underscores are collapsed to single instances.
func SanitizeBehaviorName(input string) string {
	if input == "" {
		return ""
	}

	// Keep only allowed characters.
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '/' {
			b.WriteRune(r)
		}
	}
	s := b.String()

	// Collapse repeated hyphens.
	s = reRepeatedHyphens.ReplaceAllString(s, "-")

	// Collapse repeated underscores.
	s = reRepeatedUnderscores.ReplaceAllString(s, "_")

	// Truncate to max length.
	if len(s) > MaxNameLength {
		s = s[:MaxNameLength]
	}

	return s
}

// stripControlChars removes ASCII control characters (0x00-0x1F) from the string,
// except for newline (0x0A) and tab (0x09) which are preserved.
func stripControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
