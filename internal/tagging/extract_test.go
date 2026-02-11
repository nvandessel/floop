package tagging

import (
	"reflect"
	"testing"
)

func TestExtractTags(t *testing.T) {
	dict := NewDictionary()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "empty string",
			text: "",
			want: nil,
		},
		{
			name: "single keyword",
			text: "Always run golangci-lint before committing",
			want: []string{"linting"},
		},
		{
			name: "multiple keywords from different categories",
			text: "Use git -C for worktree operations when testing Go code",
			want: []string{"git", "go", "testing", "worktree"},
		},
		{
			name: "deduplicated tags",
			text: "When writing Go code, always use go fmt and go test",
			want: []string{"go", "testing"},
		},
		{
			name: "case insensitive matching",
			text: "Docker containers should use YAML configuration",
			want: []string{"configuration", "docker", "yaml"},
		},
		{
			name: "no matching keywords",
			text: "The quick brown fox jumps over the lazy dog",
			want: nil,
		},
		{
			name: "max tags enforced",
			text: "Use git docker make npm golang testing linting security debugging refactoring ci pr yaml json bash",
			want: []string{"bash", "ci", "debugging", "docker", "git", "go", "json", "linting"},
		},
		{
			name: "project-specific keywords",
			text: "Use floop_learn to capture corrections for beads workflow",
			want: []string{"beads", "correction", "floop", "workflow"},
		},
		{
			name: "compound keywords",
			text: "Always follow the TDD red-green-refactor cycle",
			want: []string{"tdd"},
		},
		{
			name: "error handling phrase",
			text: "Use error-wrapping with fmt.Errorf for error handling",
			want: []string{"error-handling"},
		},
		{
			name: "tool names recognized",
			text: "Configure cobra commands with golangci-lint checks",
			want: []string{"cli", "linting"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTags(tt.text, dict)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTags_MaxTags(t *testing.T) {
	dict := NewDictionary()

	// A string with many keywords should be capped at MaxTags.
	text := "git docker make npm golang testing linting security debugging refactoring ci pr yaml json bash python rust javascript"
	got := ExtractTags(text, dict)
	if len(got) > MaxTags {
		t.Errorf("ExtractTags() returned %d tags, want at most %d", len(got), MaxTags)
	}
}

func TestExtractTags_Sorted(t *testing.T) {
	dict := NewDictionary()

	text := "worktree git testing go"
	got := ExtractTags(text, dict)

	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("tags not sorted: %v", got)
			break
		}
	}
}
