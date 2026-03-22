package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// redirectTransport rewrites all requests to the given target URL,
// allowing httptest servers to intercept calls to the hardcoded
// anthropicAPIURL constant.
type redirectTransport struct {
	target    string
	transport http.RoundTripper
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = rt.target
	t := rt.transport
	if t == nil {
		t = http.DefaultTransport
	}
	return t.RoundTrip(req)
}

// newTestAnthropicClient returns an AnthropicClient whose HTTP requests are
// redirected to the given httptest.Server.
func newTestAnthropicClient(ts *httptest.Server) *AnthropicClient {
	c := NewAnthropicClient(ClientConfig{
		APIKey: "test-key",
		Model:  "test-model",
	})
	c.httpClient = &http.Client{
		Transport: &redirectTransport{target: ts.Listener.Addr().String()},
		Timeout:   5 * time.Second,
	}
	return c
}

func TestAnthropicClient_Complete_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicAPIVersion {
			t.Errorf("anthropic-version = %q, want %s", r.Header.Get("anthropic-version"), anthropicAPIVersion)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		// Verify request body
		var reqBody anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if reqBody.Model != "test-model" {
			t.Errorf("model = %q, want test-model", reqBody.Model)
		}
		if reqBody.System != "You are helpful." {
			t.Errorf("system = %q, want 'You are helpful.'", reqBody.System)
		}
		if len(reqBody.Messages) != 1 || reqBody.Messages[0].Content != "Hello" {
			t.Errorf("messages = %+v, want single user message 'Hello'", reqBody.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:   "msg_123",
			Type: "message",
			Role: "assistant",
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: "Hi there!"},
			},
			StopReason: "end_turn",
		})
	}))
	defer ts.Close()

	c := newTestAnthropicClient(ts)
	resp, err := c.Complete(context.Background(), []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp != "Hi there!" {
		t.Errorf("Complete() = %q, want 'Hi there!'", resp)
	}
}

func TestAnthropicClient_Complete_NoSystemMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if reqBody.System != "" {
			t.Errorf("system = %q, want empty", reqBody.System)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: "response"},
			},
		})
	}))
	defer ts.Close()

	c := newTestAnthropicClient(ts)
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
}

func TestAnthropicClient_Complete_MultipleSystemMessages(t *testing.T) {
	c := NewAnthropicClient(ClientConfig{APIKey: "test-key"})
	_, err := c.Complete(context.Background(), []Message{
		{Role: "system", Content: "first"},
		{Role: "system", Content: "second"},
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for multiple system messages")
	}
	if got := err.Error(); !strings.Contains(got, "system message") {
		t.Errorf("error = %q, want multiple system message error", got)
	}
}

func TestAnthropicClient_Complete_NoUserMessages(t *testing.T) {
	c := NewAnthropicClient(ClientConfig{APIKey: "test-key"})
	_, err := c.Complete(context.Background(), []Message{
		{Role: "system", Content: "system only"},
	})
	if err == nil {
		t.Fatal("expected error for no non-system messages")
	}
}

func TestAnthropicClient_Complete_MissingAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	c := NewAnthropicClient(ClientConfig{})
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestAnthropicClient_Complete_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": {"type": "rate_limit_error", "message": "Too many requests"}}`))
	}))
	defer ts.Close()

	c := newTestAnthropicClient(ts)
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestAnthropicClient_Complete_ResponseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Error: &struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}{
				Type:    "invalid_request_error",
				Message: "bad request",
			},
		})
	}))
	defer ts.Close()

	c := newTestAnthropicClient(ts)
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for API error in response body")
	}
}

func TestAnthropicClient_Complete_EmptyContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{},
		})
	}))
	defer ts.Close()

	c := newTestAnthropicClient(ts)
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestAnthropicClient_Complete_NoTextBlock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "image", Text: ""},
			},
		})
	}))
	defer ts.Close()

	c := newTestAnthropicClient(ts)
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for no text content block")
	}
}

func TestAnthropicClient_Complete_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`))
	}))
	defer ts.Close()

	c := newTestAnthropicClient(ts)
	_, err := c.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestAnthropicClient_Available(t *testing.T) {
	tests := []struct {
		name   string
		apiKey string
		want   bool
	}{
		{"with key", "sk-test", true},
		{"empty key", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", "")
			c := NewAnthropicClient(ClientConfig{APIKey: tt.apiKey})
			if got := c.Available(); got != tt.want {
				t.Errorf("Available() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewAnthropicClient_Defaults(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	c := NewAnthropicClient(ClientConfig{})
	if c.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want env-key from env", c.apiKey)
	}
	if c.model != defaultModel {
		t.Errorf("model = %q, want %s", c.model, defaultModel)
	}
	if c.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.timeout)
	}
}

func TestNewAnthropicClient_CustomConfig(t *testing.T) {
	c := NewAnthropicClient(ClientConfig{
		APIKey:  "custom-key",
		Model:   "claude-3-opus",
		Timeout: 60 * time.Second,
	})
	if c.apiKey != "custom-key" {
		t.Errorf("apiKey = %q, want custom-key", c.apiKey)
	}
	if c.model != "claude-3-opus" {
		t.Errorf("model = %q, want claude-3-opus", c.model)
	}
	if c.timeout != 60*time.Second {
		t.Errorf("timeout = %v, want 60s", c.timeout)
	}
}

func TestAnthropicClient_Complete_ContextCancelled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow handler - context should cancel before this returns
		time.Sleep(5 * time.Second)
	}))
	defer ts.Close()

	c := newTestAnthropicClient(ts)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := c.Complete(ctx, []Message{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
