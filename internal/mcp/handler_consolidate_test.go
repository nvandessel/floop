package mcp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandleFloopConsolidate_NoEvents(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopConsolidateInput{}

	_, output, err := server.handleFloopConsolidate(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopConsolidate failed: %v", err)
	}
	if output.EventsProcessed != 0 {
		t.Errorf("EventsProcessed = %d, want 0", output.EventsProcessed)
	}
	if output.Message != "No events found to consolidate" {
		t.Errorf("Message = %q, want 'No events found to consolidate'", output.Message)
	}
}

func TestHandleFloopConsolidate_WithEvents(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	ctx := context.Background()

	// First, observe some events
	observeReq := &sdk.CallToolRequest{}
	for _, content := range []string{
		"User corrected formatting to use gofmt",
		"Agent learned to use table-driven tests",
		"User said to always wrap errors with context",
	} {
		args := FloopObserveInput{
			Source:    "test-agent",
			Content:   content,
			SessionID: "consolidate-test-session",
		}
		_, _, err := server.handleFloopObserve(ctx, observeReq, args)
		if err != nil {
			t.Fatalf("handleFloopObserve failed: %v", err)
		}
	}

	// Run consolidation (default: all unconsolidated events)
	req := &sdk.CallToolRequest{}
	args := FloopConsolidateInput{DryRun: true}

	_, output, err := server.handleFloopConsolidate(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopConsolidate failed: %v", err)
	}
	if output.EventsProcessed != 3 {
		t.Errorf("EventsProcessed = %d, want 3", output.EventsProcessed)
	}
	if !output.DryRun {
		t.Error("DryRun should be true")
	}
	if output.Duration == "" {
		t.Error("Duration is empty")
	}
	if !strings.Contains(output.Message, "dry run") {
		t.Errorf("Message = %q, want to contain 'dry run'", output.Message)
	}
}

func TestHandleFloopConsolidate_WithSessionFilter(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	ctx := context.Background()
	observeReq := &sdk.CallToolRequest{}

	// Add events to two sessions
	for _, sid := range []string{"session-A", "session-B"} {
		args := FloopObserveInput{
			Source:    "test-agent",
			Content:   "Event for " + sid,
			SessionID: sid,
		}
		_, _, err := server.handleFloopObserve(ctx, observeReq, args)
		if err != nil {
			t.Fatalf("handleFloopObserve failed: %v", err)
		}
	}

	// Consolidate only session-A
	req := &sdk.CallToolRequest{}
	args := FloopConsolidateInput{Session: "session-A", DryRun: true}

	_, output, err := server.handleFloopConsolidate(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopConsolidate failed: %v", err)
	}
	if output.EventsProcessed != 1 {
		t.Errorf("EventsProcessed = %d, want 1 (only session-A)", output.EventsProcessed)
	}
}

func TestHandleFloopConsolidate_WithSinceFilter(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	ctx := context.Background()
	observeReq := &sdk.CallToolRequest{}

	// Add an event
	args := FloopObserveInput{
		Source:  "test-agent",
		Content: "Recent event",
	}
	_, _, err := server.handleFloopObserve(ctx, observeReq, args)
	if err != nil {
		t.Fatalf("handleFloopObserve failed: %v", err)
	}

	// Consolidate with since=1h (should pick up our recent event)
	req := &sdk.CallToolRequest{}
	consArgs := FloopConsolidateInput{Since: "1h", DryRun: true}

	_, output, err := server.handleFloopConsolidate(ctx, req, consArgs)
	if err != nil {
		t.Fatalf("handleFloopConsolidate failed: %v", err)
	}
	if output.EventsProcessed != 1 {
		t.Errorf("EventsProcessed = %d, want 1", output.EventsProcessed)
	}
}

func TestHandleFloopConsolidate_InvalidSince(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopConsolidateInput{Since: "invalid"}

	_, _, err := server.handleFloopConsolidate(ctx, req, args)
	if err == nil {
		t.Fatal("Expected error for invalid since duration")
	}
	if !strings.Contains(err.Error(), "invalid 'since' duration") {
		t.Errorf("error = %q, want to contain 'invalid since duration'", err.Error())
	}
}

func TestHandleFloopConsolidate_NilEventStore(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	origStore := server.eventStore
	server.eventStore = nil
	defer func() { server.eventStore = origStore }()

	ctx := context.Background()
	req := &sdk.CallToolRequest{}
	args := FloopConsolidateInput{}

	_, _, err := server.handleFloopConsolidate(ctx, req, args)
	if err == nil {
		t.Fatal("Expected error for nil event store")
	}
	if !strings.Contains(err.Error(), "event store not available") {
		t.Errorf("error = %q, want to contain 'event store not available'", err.Error())
	}
}

func TestHandleFloopConsolidate_NonDryRun(t *testing.T) {
	server, _ := setupTestServer(t)
	defer server.Close()

	if server.eventStore == nil {
		t.Skip("event store not available in test environment")
	}

	ctx := context.Background()
	observeReq := &sdk.CallToolRequest{}

	// Add events
	for i := 0; i < 2; i++ {
		args := FloopObserveInput{
			Source:    "test-agent",
			Content:   "Event to consolidate for real",
			SessionID: "non-dry-run-session",
		}
		_, _, err := server.handleFloopObserve(ctx, observeReq, args)
		if err != nil {
			t.Fatalf("handleFloopObserve failed: %v", err)
		}
	}

	// Run consolidation without dry_run
	req := &sdk.CallToolRequest{}
	args := FloopConsolidateInput{DryRun: false}

	_, output, err := server.handleFloopConsolidate(ctx, req, args)
	if err != nil {
		t.Fatalf("handleFloopConsolidate failed: %v", err)
	}
	if output.EventsProcessed != 2 {
		t.Errorf("EventsProcessed = %d, want 2", output.EventsProcessed)
	}
	if output.DryRun {
		t.Error("DryRun should be false")
	}

	// Running again should find no unconsolidated events (they were marked)
	_, output2, err := server.handleFloopConsolidate(ctx, req, args)
	if err != nil {
		t.Fatalf("second handleFloopConsolidate failed: %v", err)
	}
	if output2.EventsProcessed != 0 {
		t.Errorf("second run EventsProcessed = %d, want 0 (already consolidated)", output2.EventsProcessed)
	}
}
