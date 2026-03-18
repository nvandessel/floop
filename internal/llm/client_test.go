package llm

import (
	"testing"
	"time"
)

func TestClientConfig_Defaults(t *testing.T) {
	config := DefaultConfig()

	if config.Provider != "" {
		t.Errorf("expected empty Provider, got '%s'", config.Provider)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("expected Timeout 30s, got %v", config.Timeout)
	}
}

func TestClientConfig_Instantiation(t *testing.T) {
	config := ClientConfig{
		Provider: "anthropic",
		APIKey:   "sk-test-key",
		Model:    "claude-3-haiku-20240307",
		Timeout:  5 * time.Second,
	}

	if config.Provider != "anthropic" {
		t.Errorf("expected Provider 'anthropic', got '%s'", config.Provider)
	}
	if config.APIKey != "sk-test-key" {
		t.Error("expected APIKey to be set")
	}
	if config.Model != "claude-3-haiku-20240307" {
		t.Errorf("expected Model 'claude-3-haiku-20240307', got '%s'", config.Model)
	}
}

func TestMessage_Instantiation(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	if msg.Role != "user" {
		t.Errorf("expected Role 'user', got '%s'", msg.Role)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("expected Content 'Hello, world!', got '%s'", msg.Content)
	}
}
