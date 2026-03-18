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
	openAIDefaultEndpoint = "https://api.openai.com/v1"
	openAIDefaultModel    = "gpt-4o-mini"
	ollamaDefaultModel    = "llama3.2"
)

// OpenAIClient implements the Client interface using the OpenAI API.
// It also works with OpenAI-compatible APIs like Ollama.
type OpenAIClient struct {
	provider string
	apiKey   string
	baseURL  string
	model    string
	timeout  time.Duration
	client   *http.Client
}

// NewOpenAIClient creates a new OpenAIClient with the given configuration.
// If config.APIKey is empty, it falls back to the OPENAI_API_KEY environment variable.
// If config.BaseURL is empty, it defaults to the OpenAI API endpoint.
// If config.Model is empty, it defaults to gpt-4o-mini (or llama3.2 for ollama).
func NewOpenAIClient(config ClientConfig) *OpenAIClient {
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = openAIDefaultEndpoint
	}

	model := config.Model
	if model == "" {
		// Use appropriate default based on provider
		if config.Provider == "ollama" {
			model = ollamaDefaultModel
		} else {
			model = openAIDefaultModel
		}
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &OpenAIClient{
		provider: config.Provider,
		apiKey:   apiKey,
		baseURL:  baseURL,
		model:    model,
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
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

// Complete sends messages to the OpenAI API and returns the response text.
func (c *OpenAIClient) Complete(ctx context.Context, messages []Message) (string, error) {
	if !c.Available() {
		return "", fmt.Errorf("openai client not available: missing API key")
	}

	if len(messages) == 0 {
		return "", fmt.Errorf("at least one message is required")
	}

	var apiMsgs []openAIChatMessage
	for _, m := range messages {
		apiMsgs = append(apiMsgs, openAIChatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	reqBody := openAIChatRequest{
		Model:    c.model,
		Messages: apiMsgs,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	endpoint := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

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

// Available returns true if the client is ready to make requests.
// For OpenAI, this requires an API key. For Ollama, no key is needed.
func (c *OpenAIClient) Available() bool {
	if c.provider == "ollama" {
		return true // Ollama doesn't require API key
	}
	return c.apiKey != ""
}
