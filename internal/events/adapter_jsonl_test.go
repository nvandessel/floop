package events

import (
	"strings"
	"testing"
)

func TestJSONLAdapter_Format(t *testing.T) {
	a := &JSONLAdapter{Source: "test"}
	if got := a.Format(); got != "claude-code-jsonl" {
		t.Errorf("Format() = %q, want %q", got, "claude-code-jsonl")
	}
}

func TestJSONLAdapter_Parse_ClaudeCodeFormat(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCount  int
		wantActors []EventActor
		wantKinds  []EventKind
		wantTexts  []string
	}{
		{
			name:       "user message with string content",
			input:      `{"type":"user","message":{"role":"user","content":"Hello world"},"timestamp":"2026-03-15T06:49:27.907Z","uuid":"abc123","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorUser},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"Hello world"},
		},
		{
			name:       "assistant message with text content blocks",
			input:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me read the file."}]},"timestamp":"2026-03-15T06:49:32.177Z","uuid":"def456","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorAgent},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"Let me read the file."},
		},
		{
			name:       "assistant message with thinking and text blocks extracts only text",
			input:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"internal reasoning"},{"type":"text","text":"Here is the answer."}]},"timestamp":"2026-03-15T06:50:00.000Z","uuid":"ghi789","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorAgent},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"Here is the answer."},
		},
		{
			name:       "assistant message with text and tool_use skips tool_use",
			input:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll check the file."},{"type":"tool_use","id":"toolu_123","name":"Read","input":{"path":"/tmp/foo"}}]},"timestamp":"2026-03-15T06:50:01.000Z","uuid":"jkl012","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorAgent},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"I'll check the file."},
		},
		{
			name:       "assistant message with only tool_use has empty content",
			input:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_456","name":"Bash","input":{"command":"ls"}}]},"timestamp":"2026-03-15T06:50:02.000Z","uuid":"mno345","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorAgent},
			wantKinds:  []EventKind{KindAction},
			wantTexts:  []string{""},
		},
		{
			name:       "user message with tool_result content blocks",
			input:      `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_123","type":"tool_result","content":"file contents here"}]},"timestamp":"2026-03-15T06:50:03.000Z","uuid":"pqr678","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorUser},
			wantKinds:  []EventKind{KindResult},
			wantTexts:  []string{"file contents here"},
		},
		{
			name:       "progress events are skipped",
			input:      `{"type":"progress","data":{"type":"hook_progress","hookEvent":"SessionStart"},"timestamp":"2026-03-15T06:48:52.010Z","uuid":"stu901"}`,
			wantCount:  0,
			wantActors: nil,
			wantTexts:  nil,
		},
		{
			name:       "file-history-snapshot events are skipped",
			input:      `{"type":"file-history-snapshot","messageId":"msg1","snapshot":{"trackedFileBackups":{}}}`,
			wantCount:  0,
			wantActors: nil,
			wantTexts:  nil,
		},
		{
			name:       "system events are kept",
			input:      `{"type":"system","subtype":"stop_hook_summary","hookCount":2,"timestamp":"2026-03-15T06:50:36.927Z","uuid":"vwx234","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorSystem},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{""},
		},
		{
			name: "multiple conversation events mixed with noise",
			input: `{"type":"progress","data":{"type":"hook_progress"},"timestamp":"2026-03-15T06:48:52.010Z","uuid":"a1"}
{"type":"file-history-snapshot","messageId":"msg1","snapshot":{}}
{"type":"user","message":{"role":"user","content":"Fix the bug"},"timestamp":"2026-03-15T06:49:27.907Z","uuid":"a2","sessionId":"sess1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I found the issue."}]},"timestamp":"2026-03-15T06:49:32.177Z","uuid":"a3","sessionId":"sess1"}
{"type":"progress","data":{"type":"tool_progress"},"timestamp":"2026-03-15T06:49:33.000Z","uuid":"a4"}`,
			wantCount:  2,
			wantActors: []EventActor{ActorUser, ActorAgent},
			wantKinds:  []EventKind{KindMessage, KindMessage},
			wantTexts:  []string{"Fix the bug", "I found the issue."},
		},
		{
			name:       "assistant message with multiple text blocks joins them",
			input:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"First part."},{"type":"text","text":"Second part."}]},"timestamp":"2026-03-15T06:50:04.000Z","uuid":"abc","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorAgent},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"First part.\n\nSecond part."},
		},
		{
			name:       "empty input",
			input:      "",
			wantCount:  0,
			wantActors: nil,
			wantTexts:  nil,
		},
		{
			name: "blank lines ignored",
			input: `{"type":"user","message":{"role":"user","content":"msg"},"uuid":"b1","sessionId":"sess1"}

{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"reply"}]},"uuid":"b2","sessionId":"sess1"}`,
			wantCount:  2,
			wantActors: []EventActor{ActorUser, ActorAgent},
			wantKinds:  []EventKind{KindMessage, KindMessage},
			wantTexts:  []string{"msg", "reply"},
		},
		{
			name:       "queue-operation with content",
			input:      `{"type":"queue-operation","operation":"enqueue","content":"they are in an open pr","timestamp":"2026-03-15T06:50:02.370Z","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorUser},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"they are in an open pr"},
		},
		{
			name:       "mixed text and tool_result user message is KindMessage",
			input:      `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Here is context"},{"tool_use_id":"toolu_1","type":"tool_result","content":"tool output"}]},"uuid":"mix1","sessionId":"sess1"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorUser},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"Here is context\n\ntool output"},
		},
		{
			name:       "legacy JSONL kind detection preserved",
			input:      `{"role":"assistant","type":"tool_use","content":"running command"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorAgent},
			wantKinds:  []EventKind{KindAction},
			wantTexts:  []string{"running command"},
		},
		{
			name:       "legacy tool_result without role maps to ActorTool",
			input:      `{"type":"tool_result","content":"output here"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorTool},
			wantKinds:  []EventKind{KindResult},
			wantTexts:  []string{"output here"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &JSONLAdapter{Source: "test-jsonl"}
			events, err := a.Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if len(events) != tt.wantCount {
				t.Fatalf("got %d events, want %d", len(events), tt.wantCount)
			}
			for i, e := range events {
				if e.Actor != tt.wantActors[i] {
					t.Errorf("event[%d].Actor = %q, want %q", i, e.Actor, tt.wantActors[i])
				}
				if tt.wantKinds != nil && e.Kind != tt.wantKinds[i] {
					t.Errorf("event[%d].Kind = %q, want %q", i, e.Kind, tt.wantKinds[i])
				}
				if e.Content != tt.wantTexts[i] {
					t.Errorf("event[%d].Content = %q, want %q", i, e.Content, tt.wantTexts[i])
				}
				if e.Source != "test-jsonl" {
					t.Errorf("event[%d].Source = %q, want %q", i, e.Source, "test-jsonl")
				}
				if e.ID == "" {
					t.Errorf("event[%d].ID is empty", i)
				}
				if e.SessionID == "" {
					t.Errorf("event[%d].SessionID is empty", i)
				}
			}
		})
	}
}

func TestJSONLAdapter_ParseError(t *testing.T) {
	a := &JSONLAdapter{Source: "test"}
	_, err := a.Parse(strings.NewReader("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestJSONLAdapter_MetadataPreserved(t *testing.T) {
	a := &JSONLAdapter{Source: "test"}
	input := `{"type":"user","message":{"role":"user","content":"hello"},"uuid":"abc","sessionId":"sess1","gitBranch":"main"}`
	events, err := a.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Metadata == nil {
		t.Fatal("Metadata is nil, expected raw record")
	}
	if events[0].Metadata["gitBranch"] != "main" {
		t.Errorf("Metadata[gitBranch] = %v, want %q", events[0].Metadata["gitBranch"], "main")
	}
}

func TestJSONLAdapter_SessionIDFromJSONL(t *testing.T) {
	a := &JSONLAdapter{Source: "test"}
	input := `{"type":"user","message":{"role":"user","content":"hello"},"uuid":"abc","sessionId":"real-session-id"}`
	events, err := a.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].SessionID != "real-session-id" {
		t.Errorf("SessionID = %q, want %q", events[0].SessionID, "real-session-id")
	}
}

func TestJSONLAdapter_TimestampFromJSONL(t *testing.T) {
	a := &JSONLAdapter{Source: "test"}
	input := `{"type":"user","message":{"role":"user","content":"hello"},"uuid":"abc","sessionId":"s1","timestamp":"2026-03-15T06:49:27.907Z"}`
	events, err := a.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Timestamp.Year() != 2026 || events[0].Timestamp.Month() != 3 || events[0].Timestamp.Day() != 15 {
		t.Errorf("Timestamp = %v, want 2026-03-15", events[0].Timestamp)
	}
}

func TestJSONLAdapter_ProvenanceFromAssistant(t *testing.T) {
	a := &JSONLAdapter{Source: "test"}
	input := `{"type":"assistant","message":{"model":"claude-opus-4-6","role":"assistant","content":[{"type":"text","text":"hi"}]},"uuid":"abc","sessionId":"s1","version":"2.1.76","gitBranch":"feat/foo"}`
	events, err := a.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	p := events[0].Provenance
	if p == nil {
		t.Fatal("Provenance is nil")
	}
	if p.Model != "claude-opus-4-6" {
		t.Errorf("Provenance.Model = %q, want %q", p.Model, "claude-opus-4-6")
	}
	if p.AgentVersion != "2.1.76" {
		t.Errorf("Provenance.AgentVersion = %q, want %q", p.AgentVersion, "2.1.76")
	}
	if p.Branch != "feat/foo" {
		t.Errorf("Provenance.Branch = %q, want %q", p.Branch, "feat/foo")
	}
}

func TestJSONLAdapter_TimestampWholeSecond(t *testing.T) {
	a := &JSONLAdapter{Source: "test"}
	input := `{"type":"user","message":{"role":"user","content":"hello"},"uuid":"abc","sessionId":"s1","timestamp":"2026-03-15T06:49:27Z"}`
	events, err := a.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Timestamp.Year() != 2026 || events[0].Timestamp.Month() != 3 || events[0].Timestamp.Day() != 15 {
		t.Errorf("Timestamp = %v, want 2026-03-15 (whole-second RFC3339)", events[0].Timestamp)
	}
}

func TestJSONLAdapter_ToolResultArrayContent(t *testing.T) {
	a := &JSONLAdapter{Source: "test"}
	input := `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"line 1"},{"type":"text","text":"line 2"}]}]},"uuid":"abc","sessionId":"s1"}`
	events, err := a.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Content != "line 1\n\nline 2" {
		t.Errorf("Content = %q, want %q", events[0].Content, "line 1\n\nline 2")
	}
	if events[0].Kind != KindResult {
		t.Errorf("Kind = %q, want %q", events[0].Kind, KindResult)
	}
}
