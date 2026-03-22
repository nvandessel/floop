package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

func addTestBehavior(t *testing.T, s *Server, id string) {
	t.Helper()
	ctx := context.Background()
	node := store.Node{
		ID:   id,
		Kind: "behavior",
		Content: map[string]interface{}{
			"name":    "test-behavior-" + id,
			"kind":    string(models.BehaviorKindDirective),
			"content": models.BehaviorContent{Canonical: "Test behavior " + id},
			"provenance": models.Provenance{
				SourceType: models.SourceTypeLearned,
				CreatedAt:  time.Now(),
			},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.9,
		},
	}
	if _, err := s.store.AddNode(ctx, node); err != nil {
		t.Fatalf("Failed to add test behavior %s: %v", id, err)
	}
	if err := s.store.Sync(ctx); err != nil {
		t.Fatalf("Failed to sync store: %v", err)
	}
}

func TestHandleFloopFeedback_Confirmed(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	addTestBehavior(t, server, "fb-test-1")

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopFeedbackInput{
		BehaviorID: "fb-test-1",
		Signal:     "confirmed",
	}

	result, output, err := server.handleFloopFeedback(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopFeedback failed: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result")
	}
	if output.BehaviorID != "fb-test-1" {
		t.Errorf("BehaviorID = %q, want %q", output.BehaviorID, "fb-test-1")
	}
	if output.Signal != "confirmed" {
		t.Errorf("Signal = %q, want %q", output.Signal, "confirmed")
	}
	if output.Message == "" {
		t.Error("Message is empty")
	}
}

func TestHandleFloopFeedback_Overridden(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	addTestBehavior(t, server, "fb-test-2")

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopFeedbackInput{
		BehaviorID: "fb-test-2",
		Signal:     "overridden",
	}

	_, output, err := server.handleFloopFeedback(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopFeedback failed: %v", err)
	}
	if output.Signal != "overridden" {
		t.Errorf("Signal = %q, want %q", output.Signal, "overridden")
	}
}

func TestHandleFloopFeedback_Validation(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	addTestBehavior(t, server, "fb-test-3")

	tests := []struct {
		name    string
		args    FloopFeedbackInput
		wantErr string
	}{
		{
			name:    "missing behavior_id",
			args:    FloopFeedbackInput{Signal: "confirmed"},
			wantErr: "'behavior_id' parameter is required",
		},
		{
			name:    "missing signal",
			args:    FloopFeedbackInput{BehaviorID: "fb-test-3"},
			wantErr: "'signal' parameter is required",
		},
		{
			name:    "invalid signal",
			args:    FloopFeedbackInput{BehaviorID: "fb-test-3", Signal: "invalid"},
			wantErr: "'signal' must be 'confirmed' or 'overridden'",
		},
		{
			name:    "behavior not found",
			args:    FloopFeedbackInput{BehaviorID: "nonexistent", Signal: "confirmed"},
			wantErr: "behavior not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &sdk.CallToolRequest{}
			_, _, err := server.handleFloopFeedback(ctx, req, tt.args)
			if err == nil {
				t.Fatal("Expected error")
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", got, tt.wantErr)
			}
		})
	}
}


