// Package sanitize provides content sanitization for behavior data flowing
// through the floop_learn pipeline. It strips control characters, markdown
// hierarchy markers, XML/HTML tags, and excessive backtick sequences to
// prevent stored prompt injection attacks while preserving semantic content.
package sanitize

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxContentLength is the maximum allowed length for behavior content.
const MaxContentLength = 2000

// MaxNameLength is the maximum allowed length for behavior names.
const MaxNameLength = 80

// Pre-compiled regular expressions for performance.
var (
	// reXMLTag matches XML/HTML tags including those with attributes and self-closing tags.
	// It also matches XML processing instructions like <?xml ...?>.
	// It also matches unclosed tags at end-of-string and space-after-slash closing variants.
	reXMLTag = regexp.MustCompile(`<[/?!]?[a-zA-Z][a-zA-Z0-9]*(?:\s+[^>]*)?/?\s*>|<\?[^?]*\?>|</\s+[a-zA-Z][^>]*>|<[/?!]?[a-zA-Z][^>]*$`)

	// reHTMLComment matches HTML comments like <!-- anything -->.
	reHTMLComment = regexp.MustCompile(`<!--[\s\S]*?-->`)

	// reCDATA matches CDATA sections like <![CDATA[anything]]>.
	reCDATA = regexp.MustCompile(`<!\[CDATA\[[\s\S]*?\]\]>`)

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
//  7. Trim leading/trailing whitespace
//  8. Truncate to MaxContentLength
func SanitizeBehaviorContent(input string) string {
	if input == "" {
		return ""
	}

	s := input

	// 1. Strip null bytes and ASCII control characters (0x00-0x1F) except \n (0x0A) and \t (0x09).
	s = stripControlChars(s)

	// 2. Strip HTML comments, CDATA sections, and XML/HTML-like tags.
	s = reHTMLComment.ReplaceAllString(s, "")
	s = reCDATA.ReplaceAllString(s, "")
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

	// 8. Truncate to max length (rune-safe to avoid splitting multi-byte UTF-8 chars).
	if utf8.RuneCountInString(s) > MaxContentLength {
		runes := []rune(s)
		s = string(runes[:MaxContentLength]) + "..."
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

	// Truncate to max length (rune-safe).
	if utf8.RuneCountInString(s) > MaxNameLength {
		runes := []rune(s)
		s = string(runes[:MaxNameLength])
	}

	return s
}

// SanitizeFilePath sanitizes a file path by cleaning path traversal sequences and
// stripping control characters. This is used for the 'file' parameter in floop_learn.
// After filepath.Clean, it strips leading "/" and leading "../" components to prevent
// directory traversal attacks.
func SanitizeFilePath(input string) string {
	if input == "" {
		return ""
	}
	// Strip control characters (including DEL) but preserve path-significant chars.
	s := stripControlChars(input)
	// Clean the path to resolve . and .. and double separators.
	s = filepath.Clean(s)
	// Strip absolute path prefix (Unix, Windows drive-letter, and UNC paths).
	if vol := filepath.VolumeName(s); vol != "" {
		s = strings.TrimPrefix(s, vol)
	}
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimPrefix(s, string(filepath.Separator))
	// Strip leading path traversal components (handle both "/" and OS separator).
	parentPrefix := ".." + string(filepath.Separator)
	for strings.HasPrefix(s, "../") || strings.HasPrefix(s, parentPrefix) {
		if strings.HasPrefix(s, "../") {
			s = strings.TrimPrefix(s, "../")
		} else {
			s = strings.TrimPrefix(s, parentPrefix)
		}
	}
	if s == ".." {
		return ""
	}
	if s == "." {
		return ""
	}
	return s
}

// stripControlChars removes ASCII control characters (0x00-0x1F) and DEL (0x7F) from
// the string, except for newline (0x0A) and tab (0x09) which are preserved.
func stripControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if (r < 0x20 || r == 0x7F) && r != '\n' && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
