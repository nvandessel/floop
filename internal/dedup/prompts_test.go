package dedup

import (
	"strings"
	"testing"

	"github.com/nvandessel/floop/internal/models"
)

func TestComparisonPrompt(t *testing.T) {
	a := &models.Behavior{
		ID:      "b1",
		Name:    "Use pathlib",
		Kind:    models.BehaviorKindDirective,
		Content: models.BehaviorContent{Canonical: "Use pathlib.Path instead of os.path"},
	}
	b := &models.Behavior{
		ID:      "b2",
		Name:    "Prefer pathlib",
		Kind:    models.BehaviorKindPreference,
		Content: models.BehaviorContent{Canonical: "Prefer pathlib over os.path for file paths"},
	}

	prompt := ComparisonPrompt(a, b)

	if !strings.Contains(prompt, "b1") {
		t.Error("prompt should contain behavior A ID")
	}
	if !strings.Contains(prompt, "b2") {
		t.Error("prompt should contain behavior B ID")
	}
	if !strings.Contains(prompt, "Use pathlib") {
		t.Error("prompt should contain behavior A name")
	}
	if !strings.Contains(prompt, "semantic_similarity") {
		t.Error("prompt should mention semantic_similarity in expected format")
	}
}

func TestComparisonPromptWithQuotes(t *testing.T) {
	a := &models.Behavior{
		ID:      "b1",
		Name:    `Use "pathlib"`,
		Kind:    models.BehaviorKindDirective,
		Content: models.BehaviorContent{Canonical: `Content with "quotes" and backslashes \ and markdown # headers`},
	}
	b := &models.Behavior{
		ID:      "b2",
		Name:    "Normal behavior",
		Kind:    models.BehaviorKindPreference,
		Content: models.BehaviorContent{Canonical: "Normal content"},
	}

	prompt := ComparisonPrompt(a, b)

	// The double-quoted content must appear verbatim in the prompt
	if !strings.Contains(prompt, `Use "pathlib"`) {
		t.Error("prompt should preserve double quotes in behavior name")
	}
	if !strings.Contains(prompt, `Content with "quotes"`) {
		t.Error("prompt should preserve double quotes in behavior content")
	}
	if !strings.Contains(prompt, `# headers`) {
		t.Error("prompt should preserve markdown headers in content")
	}
}

func TestMergePromptWithQuotes(t *testing.T) {
	behaviors := []*models.Behavior{
		{
			ID:      "b1",
			Name:    `Use "double quotes"`,
			Kind:    models.BehaviorKindDirective,
			Content: models.BehaviorContent{Canonical: `Content with "quotes" and special chars`},
		},
		{
			ID:      "b2",
			Name:    "Normal",
			Kind:    models.BehaviorKindPreference,
			Content: models.BehaviorContent{Canonical: "Normal content"},
		},
	}

	prompt := MergePrompt(behaviors)

	if !strings.Contains(prompt, `Use "double quotes"`) {
		t.Error("prompt should preserve double quotes in behavior name")
	}
	if !strings.Contains(prompt, `Content with "quotes"`) {
		t.Error("prompt should preserve double quotes in behavior content")
	}
}

func TestMergePrompt(t *testing.T) {
	t.Run("empty input returns empty string", func(t *testing.T) {
		prompt := MergePrompt([]*models.Behavior{})
		if prompt != "" {
			t.Error("expected empty prompt for empty input")
		}
	})

	t.Run("multiple behaviors", func(t *testing.T) {
		behaviors := []*models.Behavior{
			{ID: "b1", Name: "First", Content: models.BehaviorContent{Canonical: "first content"}},
			{ID: "b2", Name: "Second", Content: models.BehaviorContent{Canonical: "second content"}},
		}

		prompt := MergePrompt(behaviors)

		if !strings.Contains(prompt, "b1") || !strings.Contains(prompt, "b2") {
			t.Error("prompt should contain all behavior IDs")
		}
		if !strings.Contains(prompt, `["b1","b2"]`) {
			t.Error("prompt should contain source_ids array")
		}
	})
}

func TestParseComparisonResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		response := `{
			"semantic_similarity": 0.85,
			"intent_match": true,
			"merge_candidate": true,
			"reasoning": "Both behaviors address file path handling"
		}`

		result, err := ParseComparisonResponse(response)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SemanticSimilarity != 0.85 {
			t.Errorf("SemanticSimilarity = %v, want 0.85", result.SemanticSimilarity)
		}
		if !result.IntentMatch {
			t.Error("IntentMatch should be true")
		}
	})

	t.Run("similarity above range", func(t *testing.T) {
		response := `{"semantic_similarity": 1.5, "intent_match": true, "merge_candidate": true}`
		_, err := ParseComparisonResponse(response)
		if err == nil {
			t.Error("expected error for similarity > 1.0")
		}
	})

	t.Run("similarity below range", func(t *testing.T) {
		response := `{"semantic_similarity": -0.1, "intent_match": true, "merge_candidate": true}`
		_, err := ParseComparisonResponse(response)
		if err == nil {
			t.Error("expected error for similarity < 0.0")
		}
	})

	t.Run("no JSON in response", func(t *testing.T) {
		_, err := ParseComparisonResponse("This is just text")
		if err == nil {
			t.Error("expected error for response without JSON")
		}
	})
}

func TestParseMergeResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		response := `{
			"merged": {
				"name": "Use pathlib for paths",
				"kind": "directive",
				"content": {"canonical": "Use pathlib.Path for all file path operations"},
				"priority": 5,
				"confidence": 0.85
			},
			"source_ids": ["b1", "b2"],
			"reasoning": "Merged two similar pathlib behaviors"
		}`

		result, err := ParseMergeResponse(response)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Merged == nil {
			t.Fatal("Merged should not be nil")
		}
		if result.Merged.Name != "Use pathlib for paths" {
			t.Errorf("Name = %q, want %q", result.Merged.Name, "Use pathlib for paths")
		}
	})

	t.Run("missing name", func(t *testing.T) {
		response := `{"merged": {"kind": "directive", "content": {"canonical": "test"}}, "source_ids": ["b1"]}`
		_, err := ParseMergeResponse(response)
		if err == nil {
			t.Error("expected error for missing name")
		}
	})

	t.Run("missing source_ids", func(t *testing.T) {
		response := `{"merged": {"name": "Test", "kind": "directive", "content": {"canonical": "test"}}}`
		_, err := ParseMergeResponse(response)
		if err == nil {
			t.Error("expected error for missing source_ids")
		}
	})

	t.Run("invalid kind", func(t *testing.T) {
		response := `{"merged": {"name": "Test", "kind": "invalid_kind", "content": {"canonical": "test"}, "confidence": 0.5}, "source_ids": ["b1"]}`
		_, err := ParseMergeResponse(response)
		if err == nil {
			t.Error("expected error for invalid behavior kind")
		}
	})

	t.Run("confidence out of range", func(t *testing.T) {
		response := `{"merged": {"name": "Test", "kind": "directive", "content": {"canonical": "test"}, "confidence": 2.5}, "source_ids": ["b1"]}`
		_, err := ParseMergeResponse(response)
		if err == nil {
			t.Error("expected error for confidence > 1.0")
		}
	})
}
