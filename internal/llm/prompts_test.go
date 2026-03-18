package llm

import (
	"testing"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "raw JSON object",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "raw JSON array",
			input: `[1, 2, 3]`,
			want:  `[1, 2, 3]`,
		},
		{
			name:  "JSON in markdown code block with language",
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "JSON in generic markdown code block",
			input: "```\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "text without JSON",
			input: "This is just some text",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "JSON with surrounding whitespace",
			input: "  \n  {\"key\": \"value\"}  \n  ",
			want:  `{"key": "value"}`,
		},
		{
			name:  "markdown block with extra whitespace",
			input: "```json\n\n  {\"key\": \"value\"}  \n\n```",
			want:  `{"key": "value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractJSON(tt.input)
			if got != tt.want {
				t.Errorf("ExtractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToJSONArray(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{
			name:  "empty slice",
			input: []string{},
			want:  "[]",
		},
		{
			name:  "single item",
			input: []string{"a"},
			want:  `["a"]`,
		},
		{
			name:  "multiple items",
			input: []string{"a", "b", "c"},
			want:  `["a","b","c"]`,
		},
		{
			name:  "items with special characters",
			input: []string{"hello world", "foo\"bar"},
			want:  `["hello world","foo\"bar"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToJSONArray(tt.input)
			if got != tt.want {
				t.Errorf("ToJSONArray() = %q, want %q", got, tt.want)
			}
		})
	}
}
