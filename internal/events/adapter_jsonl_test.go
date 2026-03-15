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

func TestJSONLAdapter_Parse(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCount  int
		wantActors []EventActor
		wantTexts  []string
	}{
		{
			name: "claude code role format",
			input: `{"role": "user", "type": "message", "content": "Hello"}
{"role": "assistant", "type": "message", "content": "Hi there"}`,
			wantCount:  2,
			wantActors: []EventActor{ActorUser, ActorAgent},
			wantTexts:  []string{"Hello", "Hi there"},
		},
		{
			name: "tool use and result",
			input: `{"role": "assistant", "type": "tool_use", "content": "running command"}
{"role": "tool", "type": "tool_result", "content": "output here"}`,
			wantCount:  2,
			wantActors: []EventActor{ActorAgent, ActorTool},
			wantTexts:  []string{"running command", "output here"},
		},
		{
			name:       "message field fallback",
			input:      `{"role": "user", "message": "via message field"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorUser},
			wantTexts:  []string{"via message field"},
		},
		{
			name:       "text field fallback",
			input:      `{"role": "assistant", "text": "via text field"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorAgent},
			wantTexts:  []string{"via text field"},
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
			input: `{"role": "user", "content": "msg"}

{"role": "assistant", "content": "reply"}`,
			wantCount:  2,
			wantActors: []EventActor{ActorUser, ActorAgent},
			wantTexts:  []string{"msg", "reply"},
		},
		{
			name:       "actor field fallback",
			input:      `{"actor": "agent", "content": "from actor field"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorAgent},
			wantTexts:  []string{"from actor field"},
		},
		{
			name:       "unknown role defaults to system",
			input:      `{"content": "no role specified"}`,
			wantCount:  1,
			wantActors: []EventActor{ActorSystem},
			wantTexts:  []string{"no role specified"},
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
	input := `{"role": "user", "content": "hello", "extra_field": "preserved"}`
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
	if events[0].Metadata["extra_field"] != "preserved" {
		t.Errorf("Metadata[extra_field] = %v, want %q", events[0].Metadata["extra_field"], "preserved")
	}
}
