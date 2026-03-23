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
			"name": "test-behavior-" + id,
			"kind": string(models.BehaviorKindDirective),
			"content": map[string]interface{}{
				"canonical": "Test behavior " + id,
			},
			"when": map[string]interface{}{},
			"provenance": models.Provenance{
				SourceType: models.SourceTypeLearned,
				CreatedAt:  time.Now(),
			},
			"requires":  []string{},
			"overrides": []string{},
			"conflicts": []string{},
		},
		Metadata: map[string]interface{}{
			"confidence": 0.9,
			"priority":   1,
			"stats":      models.BehaviorStats{},
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

	// Verify state persistence: stats should reflect the confirmed signal
	node, err := server.store.GetNode(ctx, "fb-test-1")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if node == nil {
		t.Fatal("node fb-test-1 not found after feedback")
	}
	rawStats, ok := node.Metadata["stats"]
	if !ok {
		t.Fatal("stats key not found in node.Metadata after confirmed feedback")
	}
	stats, ok := rawStats.(map[string]interface{})
	if !ok {
		t.Fatalf("stats is not map[string]interface{}, got %T", rawStats)
	}
	if tc, ok := stats["times_confirmed"]; ok {
		switch v := tc.(type) {
		case int:
			if v != 1 {
				t.Errorf("times_confirmed = %d, want 1", v)
			}
		case float64:
			if int(v) != 1 {
				t.Errorf("times_confirmed = %v, want 1", v)
			}
		}
	} else {
		t.Error("times_confirmed not found in stats after confirmed feedback")
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
