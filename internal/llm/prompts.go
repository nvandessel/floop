// Package llm provides interfaces and types for LLM-based behavior comparison and merging.
package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// ComparisonPrompt generates a structured prompt for comparing two behaviors semantically.
// The prompt instructs the LLM to analyze similarity, intent match, and merge candidacy.
func ComparisonPrompt(a, b *models.Behavior) string {
	return fmt.Sprintf(`You are analyzing two AI agent behaviors to determine their semantic similarity.

## Behavior A
ID: %s
Name: %s
Kind: %s
Content: %s

## Behavior B
ID: %s
Name: %s
Kind: %s
Content: %s

## Task
Compare these two behaviors and determine:
1. How semantically similar they are (0.0 = completely different, 1.0 = identical meaning)
2. Whether they express the same underlying intent, even if worded differently
3. Whether they are similar enough to be merged into a single behavior

## Response Format
Respond with ONLY a JSON object (no markdown code blocks, no additional text):
{
  "semantic_similarity": <float between 0.0 and 1.0>,
  "intent_match": <boolean>,
  "merge_candidate": <boolean>,
  "reasoning": "<brief explanation of your assessment>"
}`,
		a.ID, a.Name, a.Kind, a.Content.Canonical,
		b.ID, b.Name, b.Kind, b.Content.Canonical)
}

// MergePrompt generates a prompt for merging multiple similar behaviors into one.
// The prompt instructs the LLM to synthesize a unified behavior preserving key information.
func MergePrompt(behaviors []*models.Behavior) string {
	if len(behaviors) == 0 {
		return ""
	}

	var behaviorDescriptions strings.Builder
	var sourceIDs []string

	for i, b := range behaviors {
		sourceIDs = append(sourceIDs, b.ID)
		behaviorDescriptions.WriteString(fmt.Sprintf(`
## Behavior %d
ID: %s
Name: %s
Kind: %s
Content: %s
Priority: %d
Confidence: %.2f
`,
			i+1, b.ID, b.Name, b.Kind, b.Content.Canonical, b.Priority, b.Confidence))
	}

	return fmt.Sprintf(`You are merging multiple similar AI agent behaviors into a single unified behavior.

%s
## Task
Create a single merged behavior that:
1. Preserves the essential guidance from all source behaviors
2. Eliminates redundancy while keeping important nuances
3. Uses clear, concise language optimized for agent consumption
4. Maintains the most specific applicable context conditions

## Response Format
Respond with ONLY a JSON object (no markdown code blocks, no additional text):
{
  "merged": {
    "name": "<descriptive name for the merged behavior>",
    "kind": "<one of: directive, constraint, procedure, preference>",
    "content": {
      "canonical": "<the merged behavior content, concise but complete>"
    },
    "priority": <integer priority, use highest from sources>,
    "confidence": <float confidence, use average from sources>
  },
  "source_ids": %s,
  "reasoning": "<brief explanation of how you merged the behaviors>"
}`,
		behaviorDescriptions.String(),
		toJSONArray(sourceIDs))
}

// ParseComparisonResponse parses an LLM response into a ComparisonResult.
// It handles both raw JSON and JSON wrapped in markdown code blocks.
func ParseComparisonResponse(response string) (*ComparisonResult, error) {
	jsonStr := ExtractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result ComparisonResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parsing comparison result: %w", err)
	}

	// Validate the result
	if result.SemanticSimilarity < 0 || result.SemanticSimilarity > 1 {
		return nil, fmt.Errorf("semantic_similarity must be between 0.0 and 1.0, got %f", result.SemanticSimilarity)
	}

	return &result, nil
}

// ParseMergeResponse parses an LLM response into a MergeResult.
// It handles both raw JSON and JSON wrapped in markdown code blocks.
func ParseMergeResponse(response string) (*MergeResult, error) {
	jsonStr := ExtractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	// Parse into intermediate structure to handle nested behavior
	var raw struct {
		Merged struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Content struct {
				Canonical string `json:"canonical"`
			} `json:"content"`
			Priority   int     `json:"priority"`
			Confidence float64 `json:"confidence"`
		} `json:"merged"`
		SourceIDs []string `json:"source_ids"`
		Reasoning string   `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("parsing merge result: %w", err)
	}

	// Validate required fields
	if raw.Merged.Name == "" {
		return nil, fmt.Errorf("merged behavior must have a name")
	}
	if raw.Merged.Content.Canonical == "" {
		return nil, fmt.Errorf("merged behavior must have canonical content")
	}
	if len(raw.SourceIDs) == 0 {
		return nil, fmt.Errorf("merge result must include source_ids")
	}

	// Convert to MergeResult with proper Behavior struct
	result := &MergeResult{
		Merged: &models.Behavior{
			Name: raw.Merged.Name,
			Kind: models.BehaviorKind(raw.Merged.Kind),
			Content: models.BehaviorContent{
				Canonical: raw.Merged.Content.Canonical,
			},
			Priority:   raw.Merged.Priority,
			Confidence: raw.Merged.Confidence,
		},
		SourceIDs: raw.SourceIDs,
		Reasoning: raw.Reasoning,
	}

	return result, nil
}

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

// toJSONArray converts a string slice to a JSON array string.
func toJSONArray(items []string) string {
	bytes, _ := json.Marshal(items)
	return string(bytes)
}
