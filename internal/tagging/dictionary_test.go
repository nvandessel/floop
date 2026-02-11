package tagging

import (
	"testing"
)

func TestNewDictionary(t *testing.T) {
	dict := NewDictionary()

	if dict == nil {
		t.Fatal("NewDictionary() returned nil")
	}

	// Should have entries.
	if len(dict.entries) == 0 {
		t.Error("dictionary has no entries")
	}
}

func TestDictionary_Lookup(t *testing.T) {
	dict := NewDictionary()

	tests := []struct {
		name    string
		token   string
		wantTag string
		wantOK  bool
	}{
		{"exact match", "git", "git", true},
		{"case insensitive", "Git", "git", true},
		{"compound keyword", "golangci-lint", "linting", true},
		{"unknown token", "xyzzy", "", false},
		{"language name", "python", "python", true},
		{"tool maps to concept", "cobra", "cli", true},
		{"tdd keyword", "tdd", "tdd", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTag, gotOK := dict.Lookup(tt.token)
			if gotOK != tt.wantOK {
				t.Errorf("Lookup(%q) ok = %v, want %v", tt.token, gotOK, tt.wantOK)
			}
			if gotTag != tt.wantTag {
				t.Errorf("Lookup(%q) = %q, want %q", tt.token, gotTag, tt.wantTag)
			}
		})
	}
}

func TestDictionary_AllTags(t *testing.T) {
	dict := NewDictionary()
	tags := dict.AllTags()

	if len(tags) == 0 {
		t.Error("AllTags() returned empty")
	}

	// Should be sorted.
	for i := 1; i < len(tags); i++ {
		if tags[i] < tags[i-1] {
			t.Errorf("AllTags() not sorted: %v", tags)
			break
		}
	}

	// Should contain common tags.
	tagSet := make(map[string]bool, len(tags))
	for _, tag := range tags {
		tagSet[tag] = true
	}
	for _, want := range []string{"git", "go", "testing", "linting"} {
		if !tagSet[want] {
			t.Errorf("AllTags() missing expected tag %q", want)
		}
	}
}
