package events

import (
	"strings"
	"testing"
)

func TestJSONAdapter_Format(t *testing.T) {
	a := &JSONAdapter{Source: "test"}
	if got := a.Format(); got != "generic-json" {
		t.Errorf("Format() = %q, want %q", got, "generic-json")
	}
}

func TestJSONAdapter_Parse(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCount  int
		wantActors []EventActor
		wantKinds  []EventKind
		wantTexts  []string
	}{
		{
			name: "basic array with actor and kind",
			input: `[
				{"actor": "user", "kind": "message", "content": "Hello"},
				{"actor": "agent", "kind": "action", "content": "running tool"},
				{"actor": "tool", "kind": "result", "content": "tool output"}
			]`,
			wantCount:  3,
			wantActors: []EventActor{ActorUser, ActorAgent, ActorTool},
			wantKinds:  []EventKind{KindMessage, KindAction, KindResult},
			wantTexts:  []string{"Hello", "running tool", "tool output"},
		},
		{
			name: "role field fallback",
			input: `[
				{"role": "user", "kind": "message", "content": "via role"}
			]`,
			wantCount:  1,
			wantActors: []EventActor{ActorUser},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"via role"},
		},
		{
			name: "type field fallback for kind",
			input: `[
				{"actor": "system", "type": "error", "content": "something broke"}
			]`,
			wantCount:  1,
			wantActors: []EventActor{ActorSystem},
			wantKinds:  []EventKind{KindError},
			wantTexts:  []string{"something broke"},
		},
		{
			name: "message field fallback for content",
			input: `[
				{"actor": "user", "kind": "message", "message": "via message"}
			]`,
			wantCount:  1,
			wantActors: []EventActor{ActorUser},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"via message"},
		},
		{
			name:       "empty array",
			input:      `[]`,
			wantCount:  0,
			wantActors: nil,
			wantKinds:  nil,
			wantTexts:  nil,
		},
		{
			name: "defaults for missing fields",
			input: `[
				{"content": "no actor or kind"}
			]`,
			wantCount:  1,
			wantActors: []EventActor{ActorSystem},
			wantKinds:  []EventKind{KindMessage},
			wantTexts:  []string{"no actor or kind"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &JSONAdapter{Source: "test-json"}
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
				if e.Kind != tt.wantKinds[i] {
					t.Errorf("event[%d].Kind = %q, want %q", i, e.Kind, tt.wantKinds[i])
				}
				if e.Content != tt.wantTexts[i] {
					t.Errorf("event[%d].Content = %q, want %q", i, e.Content, tt.wantTexts[i])
				}
				if e.Source != "test-json" {
					t.Errorf("event[%d].Source = %q, want %q", i, e.Source, "test-json")
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

func TestJSONAdapter_ParseError(t *testing.T) {
	a := &JSONAdapter{Source: "test"}
	_, err := a.Parse(strings.NewReader("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestJSONAdapter_NotAnArray(t *testing.T) {
	a := &JSONAdapter{Source: "test"}
	_, err := a.Parse(strings.NewReader(`{"actor": "user", "content": "not an array"}`))
	if err == nil {
		t.Error("expected error for non-array JSON, got nil")
	}
}
