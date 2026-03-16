package dedup

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
)

// ComparisonResult contains the result of comparing two behaviors using an LLM.
type ComparisonResult struct {
	// SemanticSimilarity is a score between 0.0 and 1.0 indicating how similar
	// the behaviors are in meaning, not just word overlap.
	SemanticSimilarity float64 `json:"semantic_similarity" yaml:"semantic_similarity"`

	// IntentMatch indicates whether the behaviors express the same underlying intent,
	// even if worded differently.
	IntentMatch bool `json:"intent_match" yaml:"intent_match"`

	// MergeCandidate indicates whether the behaviors are similar enough to be merged.
	MergeCandidate bool `json:"merge_candidate" yaml:"merge_candidate"`

	// Reasoning contains the LLM's explanation for its assessment.
	Reasoning string `json:"reasoning,omitempty" yaml:"reasoning,omitempty"`
}

// MergeResult contains the result of merging multiple behaviors using an LLM.
type MergeResult struct {
	// Merged is the new behavior combining the source behaviors.
	// May be nil if the LLM returns an empty or invalid response.
	Merged *models.Behavior `json:"merged" yaml:"merged"`

	// SourceIDs contains the IDs of the behaviors that were merged.
	SourceIDs []string `json:"source_ids" yaml:"source_ids"`

	// Reasoning contains the LLM's explanation for how it merged the behaviors.
	Reasoning string `json:"reasoning,omitempty" yaml:"reasoning,omitempty"`
}

// ComparisonPrompt generates a structured prompt for comparing two behaviors semantically.
// The prompt instructs the LLM to analyze similarity, intent match, and merge candidacy.
//
// User-provided behavior data is concatenated via strings.Builder rather than
// interpolated through fmt.Sprintf alongside JSON template text, to prevent
// quote-breaking if behavior content contains double quotes or markdown headers (CWE-94).
func ComparisonPrompt(a, b *models.Behavior) string {
	var p strings.Builder
	p.WriteString("You are analyzing two AI agent behaviors to determine their semantic similarity.\n\n")
	fmt.Fprintf(&p, "## Behavior A\nID: %s\nName: %s\nKind: %s\nContent: ", a.ID, a.Name, a.Kind)
	p.WriteString(a.Content.Canonical)
	fmt.Fprintf(&p, "\n\n## Behavior B\nID: %s\nName: %s\nKind: %s\nContent: ", b.ID, b.Name, b.Kind)
	p.WriteString(b.Content.Canonical)
	p.WriteString(`

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
}`)
	return p.String()
}

// MergePrompt generates a prompt for merging multiple similar behaviors into one.
// The prompt instructs the LLM to synthesize a unified behavior preserving key information.
//
// User-provided behavior data is concatenated via strings.Builder rather than
// interpolated through fmt.Sprintf alongside JSON template text, to prevent
// quote-breaking if behavior content contains double quotes (CWE-94).
func MergePrompt(behaviors []*models.Behavior) string {
	if len(behaviors) == 0 {
		return ""
	}

	var sourceIDs []string
	var prompt strings.Builder

	prompt.WriteString("You are merging multiple similar AI agent behaviors into a single unified behavior.\n\n")

	for i, b := range behaviors {
		sourceIDs = append(sourceIDs, b.ID)
		fmt.Fprintf(&prompt, "\n## Behavior %d\nID: %s\nName: %s\nKind: %s\nContent: %s\nPriority: %d\nConfidence: %.2f\n",
			i+1, b.ID, b.Name, b.Kind, b.Content.Canonical, b.Priority, b.Confidence)
	}

	prompt.WriteString(`
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
  "source_ids": `)
	prompt.WriteString(llm.ToJSONArray(sourceIDs))
	prompt.WriteString(`,
  "reasoning": "<brief explanation of how you merged the behaviors>"
}`)

	return prompt.String()
}

// ParseComparisonResponse parses an LLM response into a ComparisonResult.
// It handles both raw JSON and JSON wrapped in markdown code blocks.
func ParseComparisonResponse(response string) (*ComparisonResult, error) {
	jsonStr := llm.ExtractJSON(response)
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
	jsonStr := llm.ExtractJSON(response)
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
