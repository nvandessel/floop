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
	System    string             `json:"system,omitempty"`
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

// Complete sends messages to the Anthropic API and returns the response text.
func (c *AnthropicClient) Complete(ctx context.Context, messages []Message) (string, error) {
	if !c.Available() {
		return "", fmt.Errorf("anthropic client not available: missing API key")
	}

	// Separate system messages from user/assistant messages.
	// The Anthropic API accepts a single top-level system prompt; passing
	// multiple system messages is an error rather than silent data loss.
	var system string
	var systemCount int
	var apiMsgs []anthropicMessage
	for _, m := range messages {
		if m.Role == "system" {
			systemCount++
			if systemCount > 1 {
				return "", fmt.Errorf("anthropic API supports a single system message, got %d", systemCount)
			}
			system = m.Content
		} else {
			apiMsgs = append(apiMsgs, anthropicMessage{
				Role:    m.Role,
				Content: m.Content,
			})
		}
	}

	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: 1024,
		System:    system,
		Messages:  apiMsgs,
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

// Available returns true if the API key is present.
func (c *AnthropicClient) Available() bool {
	return c.apiKey != ""
}
