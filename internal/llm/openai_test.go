package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIClient_Complete_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify endpoint
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q, want 'Bearer test-key'", r.Header.Get("Authorization"))
		}

		var reqBody openAIChatRequest
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody.Model != "gpt-4o" {
			t.Errorf("model = %q, want gpt-4o", reqBody.Model)
		}
		// System messages should be passed through for OpenAI
		if len(reqBody.Messages) != 2 {
			t.Errorf("messages count = %d, want 2", len(reqBody.Messages))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "Hello from GPT!"}},
			},
		})
	}))
	defer ts.Close()

	c := NewOpenAIClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: ts.URL,
		Model:   "gpt-4o",
	})
	resp, err := c.Complete(context.Background(), []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp != "Hello from GPT!" {
		t.Errorf("Complete() = %q, want 'Hello from GPT!'", resp)
	}
}

func TestOpenAIClient_Complete_EmptyMessages(t *testing.T) {
	c := NewOpenAIClient(ClientConfig{APIKey: "test-key"})
	_, err := c.Complete(context.Background(), []Message{})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
}

func TestOpenAIClient_Complete_MissingAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	c := NewOpenAIClient(ClientConfig{Provider: "openai"})
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestOpenAIClient_Complete_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": {"message": "server error", "type": "server_error"}}`))
	}))
	defer ts.Close()

	c := NewOpenAIClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: ts.URL,
	})
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestOpenAIClient_Complete_APIErrorInBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAIChatResponse{
			Error: &struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			}{
				Message: "invalid model",
				Type:    "invalid_request_error",
			},
		})
	}))
	defer ts.Close()

	c := NewOpenAIClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: ts.URL,
	})
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for API error in body")
	}
}

func TestOpenAIClient_Complete_NoChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{},
		})
	}))
	defer ts.Close()

	c := NewOpenAIClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: ts.URL,
	})
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestOpenAIClient_Complete_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{invalid`))
	}))
	defer ts.Close()

	c := NewOpenAIClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: ts.URL,
	})
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestOpenAIClient_Available(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		apiKey   string
		want     bool
	}{
		{"openai with key", "openai", "sk-test", true},
		{"openai without key", "openai", "", false},
		{"ollama without key", "ollama", "", true},
		{"ollama with key", "ollama", "optional-key", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OPENAI_API_KEY", "")
			c := NewOpenAIClient(ClientConfig{
				Provider: tt.provider,
				APIKey:   tt.apiKey,
			})
			if got := c.Available(); got != tt.want {
				t.Errorf("Available() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewOpenAIClient_Defaults(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	c := NewOpenAIClient(ClientConfig{})
	if c.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want env-key", c.apiKey)
	}
	if c.baseURL != openAIDefaultEndpoint {
		t.Errorf("baseURL = %q, want %s", c.baseURL, openAIDefaultEndpoint)
	}
	if c.model != openAIDefaultModel {
		t.Errorf("model = %q, want %s", c.model, openAIDefaultModel)
	}
	if c.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.timeout)
	}
}

func TestNewOpenAIClient_OllamaDefaults(t *testing.T) {
	c := NewOpenAIClient(ClientConfig{Provider: "ollama"})
	if c.model != ollamaDefaultModel {
		t.Errorf("model = %q, want %s for ollama", c.model, ollamaDefaultModel)
	}
}

func TestNewOpenAIClient_CustomConfig(t *testing.T) {
	c := NewOpenAIClient(ClientConfig{
		Provider: "openai",
		APIKey:   "custom-key",
		BaseURL:  "http://localhost:8080",
		Model:    "gpt-4-turbo",
		Timeout:  45 * time.Second,
	})
	if c.apiKey != "custom-key" {
		t.Errorf("apiKey = %q, want custom-key", c.apiKey)
	}
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want http://localhost:8080", c.baseURL)
	}
	if c.model != "gpt-4-turbo" {
		t.Errorf("model = %q, want gpt-4-turbo", c.model)
	}
	if c.timeout != 45*time.Second {
		t.Errorf("timeout = %v, want 45s", c.timeout)
	}
}

func TestOpenAIClient_Complete_OllamaNoAuthHeader(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ollama with no API key should not send Authorization header
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization header = %q, want empty for ollama without key", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "ollama response"}},
			},
		})
	}))
	defer ts.Close()

	c := NewOpenAIClient(ClientConfig{
		Provider: "ollama",
		BaseURL:  ts.URL,
	})
	resp, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp != "ollama response" {
		t.Errorf("Complete() = %q, want 'ollama response'", resp)
	}
}

func TestOpenAIClient_Complete_ContextCancelled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer ts.Close()

	c := NewOpenAIClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: ts.URL,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Complete(ctx, []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
