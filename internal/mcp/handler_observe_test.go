package mcp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandleFloopObserve_Success(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopObserveInput{
		Source:  "test-agent",
		Content: "User corrected formatting approach",
	}

	result, output, err := server.handleFloopObserve(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopObserve failed: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result")
	}
	if output.EventID == "" {
		t.Error("EventID is empty")
	}
	if output.SessionID == "" {
		t.Error("SessionID is empty")
	}
	if !strings.Contains(output.Message, "test-agent") {
		t.Errorf("Message = %q, want to contain source", output.Message)
	}
}

func TestHandleFloopObserve_WithAllFields(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopObserveInput{
		Source:    "test-agent",
		Content:   "Test event with all fields",
		Actor:     "user",
		Kind:      "correction",
		SessionID: "test-session-42",
		Metadata:  map[string]any{"key": "value"},
	}

	_, output, err := server.handleFloopObserve(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopObserve failed: %v", err)
	}
	if output.SessionID != "test-session-42" {
		t.Errorf("SessionID = %q, want %q", output.SessionID, "test-session-42")
	}
}

func TestHandleFloopObserve_Defaults(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	// When actor and kind are omitted, defaults should be applied (agent, message)
	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopObserveInput{
		Source:  "test-agent",
		Content: "Default actor and kind test",
	}

	_, output, err := server.handleFloopObserve(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopObserve failed: %v", err)
	}
	// Auto-generated session ID should start with "mcp-"
	if !strings.HasPrefix(output.SessionID, "mcp-") {
		t.Errorf("SessionID = %q, want prefix 'mcp-'", output.SessionID)
	}
}

func TestHandleFloopObserve_Validation(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	tests := []struct {
		name    string
		args    FloopObserveInput
		wantErr string
	}{
		{
			name:    "missing source",
			args:    FloopObserveInput{Content: "test"},
			wantErr: "'source' parameter is required",
		},
		{
			name:    "missing content",
			args:    FloopObserveInput{Source: "test"},
			wantErr: "'content' parameter is required",
		},
		{
			name:    "invalid actor",
			args:    FloopObserveInput{Source: "test", Content: "test", Actor: "invalid"},
			wantErr: "invalid actor",
		},
		{
			name:    "invalid kind",
			args:    FloopObserveInput{Source: "test", Content: "test", Kind: "invalid"},
			wantErr: "invalid kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &sdk.CallToolRequest{}
			_, _, err := server.handleFloopObserve(ctx, req, tt.args)
			if err == nil {
				t.Fatal("Expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestHandleFloopObserve_NilEventStore(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	// Force nil event store
	origStore := server.eventStore
	server.eventStore = nil
	defer func() { server.eventStore = origStore }()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopObserveInput{
		Source:  "test-agent",
		Content: "Should fail",
	}

	_, _, err := server.handleFloopObserve(ctx, req, args)
	if err == nil {
		t.Fatal("Expected error for nil event store")
	}
	if !strings.Contains(err.Error(), "event store not available") {
		t.Errorf("error = %q, want to contain 'event store not available'", err.Error())
	}
}
