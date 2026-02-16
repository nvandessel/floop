// Package similarity provides shared similarity computation functions
// for behavior deduplication and graph placement.
package similarity

import "strings"

// Tokenize splits a string into word tokens.
// Word characters are letters, digits, and underscores.
func Tokenize(s string) []string {
	words := make([]string, 0)
	var current strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			current.WriteRune(r)
		} else if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}
