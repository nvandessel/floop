package events

import (
	"strings"
	"testing"
)

func TestMarkdownAdapter_Format(t *testing.T) {
	a := &MarkdownAdapter{Source: "test"}
	if got := a.Format(); got != "markdown" {
		t.Errorf("Format() = %q, want %q", got, "markdown")
	}
}

func TestMarkdownAdapter_Parse(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantCount  int
		wantActors []EventActor
		wantTexts  []string
	}{
		{
			name:       "user and assistant",
			input:      "User: Hello\nAssistant: Hi there",
			wantCount:  2,
			wantActors: []EventActor{ActorUser, ActorAgent},
			wantTexts:  []string{"Hello", "Hi there"},
		},
		{
			name:       "human and AI prefixes",
			input:      "Human: What is Go?\nAI: A programming language",
			wantCount:  2,
			wantActors: []EventActor{ActorUser, ActorAgent},
			wantTexts:  []string{"What is Go?", "A programming language"},
		},
		{
			name:       "tool and system prefixes",
			input:      "Tool: file contents here\nSystem: context loaded",
			wantCount:  2,
			wantActors: []EventActor{ActorTool, ActorSystem},
			wantTexts:  []string{"file contents here", "context loaded"},
		},
		{
			name:       "multiline content",
			input:      "User: Line one\nLine two\nLine three\nAssistant: Response",
			wantCount:  2,
			wantActors: []EventActor{ActorUser, ActorAgent},
			wantTexts:  []string{"Line one\nLine two\nLine three", "Response"},
		},
		{
			name:       "empty input",
			input:      "",
			wantCount:  0,
			wantActors: nil,
			wantTexts:  nil,
		},
		{
			name:       "no recognized prefixes",
			input:      "Just some random text\nwith no prefixes",
			wantCount:  0,
			wantActors: nil,
			wantTexts:  nil,
		},
		{
			name:       "leading text before first prefix",
			input:      "Preamble text\nUser: Actual message",
			wantCount:  1,
			wantActors: []EventActor{ActorUser},
			wantTexts:  []string{"Actual message"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &MarkdownAdapter{Source: "test-md"}
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
				if e.Source != "test-md" {
					t.Errorf("event[%d].Source = %q, want %q", i, e.Source, "test-md")
				}
				if e.Kind != KindMessage {
					t.Errorf("event[%d].Kind = %q, want %q", i, e.Kind, KindMessage)
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

func TestMarkdownAdapter_SharedSessionID(t *testing.T) {
	a := &MarkdownAdapter{Source: "test"}
	input := "User: Hello\nAssistant: Hi\nUser: Follow up"
	events, err := a.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}

	// All events from same parse should share a session ID
	sid := events[0].SessionID
	for i, e := range events {
		if e.SessionID != sid {
			t.Errorf("event[%d].SessionID = %q, want %q (shared)", i, e.SessionID, sid)
		}
	}
}
