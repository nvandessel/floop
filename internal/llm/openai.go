package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
)

const (
	openAIEndpoint     = "https://api.openai.com/v1/chat/completions"
	openAIDefaultModel = "gpt-4o-mini"
)

// OpenAIClient implements the Client interface using the OpenAI API.
type OpenAIClient struct {
	apiKey  string
	model   string
	timeout time.Duration
	client  *http.Client
}

// NewOpenAIClient creates a new OpenAIClient with the given configuration.
// If config.APIKey is empty, it falls back to the OPENAI_API_KEY environment variable.
// If config.Model is empty, it defaults to gpt-4o-mini.
func NewOpenAIClient(config ClientConfig) *OpenAIClient {
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	model := config.Model
	if model == "" {
		model = openAIDefaultModel
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &OpenAIClient{
		apiKey:  apiKey,
		model:   model,
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
	}
}

// openAIChatRequest represents a request to the OpenAI chat completions API.
type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []openAIChatMessage `json:"messages"`
}

// openAIChatMessage represents a message in the OpenAI chat format.
type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIChatResponse represents a response from the OpenAI chat completions API.
type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// CompareBehaviors compares two behaviors using the OpenAI API.
// It sends a structured prompt and parses the JSON response into a ComparisonResult.
func (c *OpenAIClient) CompareBehaviors(ctx context.Context, a, b *models.Behavior) (*ComparisonResult, error) {
	prompt := ComparisonPrompt(a, b)

	response, err := c.callAPI(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("calling OpenAI API: %w", err)
	}

	result, err := ParseComparisonResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parsing comparison response: %w", err)
	}

	return result, nil
}

// MergeBehaviors merges multiple behaviors using the OpenAI API.
// It sends a structured prompt and parses the JSON response into a MergeResult.
func (c *OpenAIClient) MergeBehaviors(ctx context.Context, behaviors []*models.Behavior) (*MergeResult, error) {
	if len(behaviors) == 0 {
		return &MergeResult{Merged: nil, SourceIDs: []string{}, Reasoning: "No behaviors to merge"}, nil
	}
	if len(behaviors) == 1 {
		return &MergeResult{Merged: behaviors[0], SourceIDs: []string{behaviors[0].ID}, Reasoning: "Single behavior, no merge needed"}, nil
	}

	prompt := MergePrompt(behaviors)

	response, err := c.callAPI(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("calling OpenAI API: %w", err)
	}

	result, err := ParseMergeResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parsing merge response: %w", err)
	}

	return result, nil
}

// Available returns true if the OpenAI API key is present.
func (c *OpenAIClient) Available() bool {
	return c.apiKey != ""
}

// callAPI makes a request to the OpenAI chat completions API.
func (c *OpenAIClient) callAPI(ctx context.Context, prompt string) (string, error) {
	reqBody := openAIChatRequest{
		Model: c.model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openAIEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("parsing API response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in API response")
	}

	return chatResp.Choices[0].Message.Content, nil
}
