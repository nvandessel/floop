package learning

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nvandessel/floop/internal/llm"
)

// CorrectionExtractionResult contains the result of extracting a correction from user text.
type CorrectionExtractionResult struct {
	// IsCorrection indicates whether the text appears to be a correction
	IsCorrection bool `json:"is_correction"`

	// Wrong describes what the agent did incorrectly
	Wrong string `json:"wrong,omitempty"`

	// Right describes what should have been done instead
	Right string `json:"right,omitempty"`

	// Confidence is how confident we are this is a correction (0.0-1.0)
	Confidence float64 `json:"confidence"`

	// Reasoning explains why this is or isn't a correction
	Reasoning string `json:"reasoning,omitempty"`
}

// CorrectionExtractionPrompt generates a prompt for extracting correction details from user text.
//
// User-provided text is concatenated via strings.Builder rather than
// interpolated through fmt.Sprintf alongside JSON template text, to prevent
// quote-breaking if the user text contains special characters (CWE-94).
func CorrectionExtractionPrompt(userText string) string {
	var prompt strings.Builder

	prompt.WriteString("You are analyzing a user message to determine if it contains a correction to an AI agent's behavior.\n\n## User Message\n")
	prompt.WriteString(userText)
	prompt.WriteString(`

## Task
Analyze whether this message is correcting something the AI agent did wrong, and if so, extract:
1. What the agent did wrong (the incorrect action/approach)
2. What should have been done instead (the correct action/approach)

A correction typically:
- Points out a mistake ("no, don't do X", "that's wrong", "actually...")
- Provides guidance on the right approach ("instead, use Y", "you should Z")
- Expresses preference ("I prefer X over Y", "better to use Z")

NOT a correction:
- Simple questions or requests
- General conversation
- Acknowledgments or thanks

## Response Format
Respond with ONLY a JSON object (no markdown code blocks, no additional text):
{
  "is_correction": <boolean>,
  "wrong": "<what the agent did wrong, if this is a correction>",
  "right": "<what should be done instead, if this is a correction>",
  "confidence": <float between 0.0 and 1.0>,
  "reasoning": "<brief explanation>"
}`)
	return prompt.String()
}

// ParseCorrectionExtractionResponse parses an LLM response into a CorrectionExtractionResult.
func ParseCorrectionExtractionResponse(response string) (*CorrectionExtractionResult, error) {
	jsonStr := llm.ExtractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result CorrectionExtractionResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parsing correction extraction result: %w", err)
	}

	// Validate confidence range
	if result.Confidence < 0 || result.Confidence > 1 {
		result.Confidence = 0.5 // Default to medium confidence if invalid
	}

	return &result, nil
}
