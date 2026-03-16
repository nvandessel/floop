package events

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// JSONAdapter parses a JSON array of event objects (passthrough format).
type JSONAdapter struct {
	Source string
}

// Format returns the adapter format name.
func (a *JSONAdapter) Format() string { return "generic-json" }

// Parse reads a JSON array of objects with actor, kind, and content fields.
func (a *JSONAdapter) Parse(reader io.Reader) ([]Event, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading JSON input: %w", err)
	}

	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parsing JSON array: %w", err)
	}

	sessionID := generateSessionID()
	now := time.Now()

	events := make([]Event, 0, len(records))
	for i, rec := range records {
		event := Event{
			ID:        generateEventID(),
			SessionID: sessionID,
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			Source:    a.Source,
			Actor:     mapJSONActor(rec),
			Kind:      mapJSONKind(rec),
			Content:   extractJSONContent(rec),
			CreatedAt: now,
		}

		events = append(events, event)
	}

	return events, nil
}

// mapJSONActor maps a JSON record's actor field to EventActor.
func mapJSONActor(rec map[string]any) EventActor {
	if actor, ok := rec["actor"].(string); ok {
		switch strings.ToLower(actor) {
		case "user", "human":
			return ActorUser
		case "agent", "assistant", "ai":
			return ActorAgent
		case "tool":
			return ActorTool
		case "system":
			return ActorSystem
		}
	}

	// Fallback to role field
	if role, ok := rec["role"].(string); ok {
		switch strings.ToLower(role) {
		case "user", "human":
			return ActorUser
		case "assistant", "ai":
			return ActorAgent
		case "tool":
			return ActorTool
		case "system":
			return ActorSystem
		}
	}

	return ActorSystem
}

// mapJSONKind maps a JSON record's kind field to EventKind.
func mapJSONKind(rec map[string]any) EventKind {
	if kind, ok := rec["kind"].(string); ok {
		switch strings.ToLower(kind) {
		case "message":
			return KindMessage
		case "action":
			return KindAction
		case "result":
			return KindResult
		case "error":
			return KindError
		case "correction":
			return KindCorrection
		}
	}

	// Fallback to type field
	if typ, ok := rec["type"].(string); ok {
		switch strings.ToLower(typ) {
		case "message":
			return KindMessage
		case "action":
			return KindAction
		case "result":
			return KindResult
		case "error":
			return KindError
		case "correction":
			return KindCorrection
		}
	}

	return KindMessage
}

// extractJSONContent extracts the content string from a JSON record.
func extractJSONContent(rec map[string]any) string {
	if content, ok := rec["content"].(string); ok {
		return content
	}
	if msg, ok := rec["message"].(string); ok {
		return msg
	}
	if text, ok := rec["text"].(string); ok {
		return text
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return ""
	}
	return string(data)
}
