package events

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// JSONLAdapter parses Claude Code session JSONL format into Events.
type JSONLAdapter struct {
	Source string
}

// Format returns the adapter format name.
func (a *JSONLAdapter) Format() string { return "claude-code-jsonl" }

// Parse reads JSONL input where each line is a JSON object and maps it to Events.
func (a *JSONLAdapter) Parse(reader io.Reader) ([]Event, error) {
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for potentially large JSONL lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	sessionID := generateSessionID()
	now := time.Now()

	var events []Event

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parsing JSONL line: %w", err)
		}

		event := Event{
			ID:        generateEventID(),
			SessionID: sessionID,
			Timestamp: now.Add(time.Duration(len(events)) * time.Millisecond),
			Source:    a.Source,
			Actor:     mapJSONLActor(raw),
			Kind:      mapJSONLKind(raw),
			Content:   extractJSONLContent(raw),
			CreatedAt: now,
		}

		// Preserve the raw record as metadata
		event.Metadata = raw

		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning JSONL input: %w", err)
	}

	return events, nil
}

// mapJSONLActor maps JSONL role/type fields to EventActor.
func mapJSONLActor(raw map[string]any) EventActor {
	// Try "role" field first (Claude Code format)
	if role, ok := raw["role"].(string); ok {
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

	// Try "type" field as fallback
	if typ, ok := raw["type"].(string); ok {
		switch strings.ToLower(typ) {
		case "user", "human":
			return ActorUser
		case "assistant", "ai":
			return ActorAgent
		case "tool", "tool_use", "tool_result":
			return ActorTool
		case "system":
			return ActorSystem
		}
	}

	// Try "actor" field
	if actor, ok := raw["actor"].(string); ok {
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

	return ActorSystem
}

// mapJSONLKind maps JSONL type fields to EventKind.
func mapJSONLKind(raw map[string]any) EventKind {
	if typ, ok := raw["type"].(string); ok {
		switch strings.ToLower(typ) {
		case "message":
			return KindMessage
		case "action", "tool_use":
			return KindAction
		case "result", "tool_result":
			return KindResult
		case "error":
			return KindError
		case "correction":
			return KindCorrection
		}
	}
	return KindMessage
}

// extractJSONLContent extracts the content string from a JSONL record.
func extractJSONLContent(raw map[string]any) string {
	// Try "content" field first
	if content, ok := raw["content"].(string); ok {
		return content
	}

	// Try "message" field
	if msg, ok := raw["message"].(string); ok {
		return msg
	}

	// Try "text" field
	if text, ok := raw["text"].(string); ok {
		return text
	}

	// Fall back to JSON serialization of the whole record
	data, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(data)
}
