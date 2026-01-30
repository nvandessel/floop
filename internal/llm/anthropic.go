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
	anthropicAPIURL     = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion = "2023-06-01"
	defaultModel        = "claude-3-haiku-20240307"
)

// AnthropicClient implements the Client interface using the Anthropic Messages API.
type AnthropicClient struct {
	apiKey     string
	model      string
	timeout    time.Duration
	httpClient *http.Client
}

// NewAnthropicClient creates a new AnthropicClient with the given configuration.
// If config.APIKey is empty, it falls back to the ANTHROPIC_API_KEY environment variable.
// If config.Model is empty, it defaults to claude-3-haiku-20240307.
// If config.Timeout is zero, it defaults to 30 seconds.
func NewAnthropicClient(config ClientConfig) *AnthropicClient {
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	model := config.Model
	if model == "" {
		model = defaultModel
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &AnthropicClient{
		apiKey:  apiKey,
		model:   model,
		timeout: timeout,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// anthropicRequest represents a request to the Anthropic Messages API.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage represents a message in the Anthropic API format.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse represents a response from the Anthropic Messages API.
type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// CompareBehaviors compares two behaviors using the Anthropic API.
// It sends a structured prompt and parses the JSON response.
func (c *AnthropicClient) CompareBehaviors(ctx context.Context, a, b *models.Behavior) (*ComparisonResult, error) {
	if !c.Available() {
		return nil, fmt.Errorf("anthropic client not available: missing API key")
	}

	prompt := ComparisonPrompt(a, b)
	response, err := c.sendRequest(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("comparing behaviors: %w", err)
	}

	result, err := ParseComparisonResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parsing comparison response: %w", err)
	}

	return result, nil
}

// MergeBehaviors merges multiple behaviors using the Anthropic API.
// It sends a structured prompt and parses the JSON response.
func (c *AnthropicClient) MergeBehaviors(ctx context.Context, behaviors []*models.Behavior) (*MergeResult, error) {
	if !c.Available() {
		return nil, fmt.Errorf("anthropic client not available: missing API key")
	}

	if len(behaviors) == 0 {
		return &MergeResult{Merged: nil, SourceIDs: []string{}, Reasoning: "No behaviors to merge"}, nil
	}
	if len(behaviors) == 1 {
		return &MergeResult{
			Merged:    behaviors[0],
			SourceIDs: []string{behaviors[0].ID},
			Reasoning: "Single behavior, no merge needed",
		}, nil
	}

	prompt := MergePrompt(behaviors)
	response, err := c.sendRequest(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("merging behaviors: %w", err)
	}

	result, err := ParseMergeResponse(response)
	if err != nil {
		return nil, fmt.Errorf("parsing merge response: %w", err)
	}

	return result, nil
}

// Available returns true if the API key is present.
func (c *AnthropicClient) Available() bool {
	return c.apiKey != ""
}

// sendRequest sends a prompt to the Anthropic API and returns the response text.
func (c *AnthropicClient) sendRequest(ctx context.Context, prompt string) (string, error) {
	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: 1024,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("parsing API response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error: %s - %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	// Extract text from the first content block
	for _, content := range apiResp.Content {
		if content.Type == "text" {
			return content.Text, nil
		}
	}

	return "", fmt.Errorf("no text content in API response")
}
