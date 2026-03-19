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

// skipTypes are Claude Code event types that carry no conversation content.
var skipTypes = map[string]bool{
	"progress":              true,
	"file-history-snapshot": true,
	"pr-link":               true,
}

// Parse reads Claude Code JSONL input and maps conversation events to Events.
// Non-conversation events (progress, file-history-snapshot, etc.) are skipped.
func (a *JSONLAdapter) Parse(reader io.Reader) ([]Event, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var events []Event
	fallbackSessionID := generateSessionID()
	now := time.Now()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parsing JSONL line: %w", err)
		}

		eventType, _ := raw["type"].(string)
		if skipTypes[eventType] {
			continue
		}

		actor, kind, content := extractClaudeCodeEvent(raw)

		// Determine session ID from the JSONL record or use fallback.
		sessionID := fallbackSessionID
		if sid, ok := raw["sessionId"].(string); ok && sid != "" {
			sessionID = sid
		}

		// Parse timestamp from the record.
		ts := now.Add(time.Duration(len(events)) * time.Millisecond)
		if tsStr, ok := raw["timestamp"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
				ts = parsed
			} else if parsed, err := time.Parse(time.RFC3339, tsStr); err == nil {
				ts = parsed
			}
		}

		event := Event{
			ID:        generateEventID(),
			SessionID: sessionID,
			Timestamp: ts,
			Source:    a.Source,
			Actor:     actor,
			Kind:      kind,
			Content:   content,
			Metadata:  raw,
			CreatedAt: now,
		}

		// Extract provenance from assistant messages.
		if eventType == "assistant" {
			event.Provenance = extractProvenance(raw)
		}

		events = append(events, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning JSONL input: %w", err)
	}

	return events, nil
}

// extractClaudeCodeEvent extracts actor, kind, and text content from a Claude Code JSONL record.
func extractClaudeCodeEvent(raw map[string]any) (EventActor, EventKind, string) {
	eventType, _ := raw["type"].(string)

	switch eventType {
	case "user":
		content, hasText, hasToolResult := extractMessageContent(raw)
		kind := KindMessage
		if hasToolResult && !hasText {
			kind = KindResult
		}
		return ActorUser, kind, content

	case "assistant":
		content, kind := extractAssistantContent(raw)
		return ActorAgent, kind, content

	case "system":
		return ActorSystem, KindMessage, ""

	case "queue-operation":
		// Queue operations are user-initiated follow-up messages.
		content, _ := raw["content"].(string)
		return ActorUser, KindMessage, content

	default:
		// Fallback: try legacy flat format for non-Claude-Code JSONL.
		return extractLegacyEvent(raw)
	}
}

// extractMessageContent extracts text from message.content, which can be a string
// or an array of content blocks. Returns the text, whether text blocks were found,
// and whether any tool_result blocks were found.
func extractMessageContent(raw map[string]any) (string, bool, bool) {
	msg, ok := raw["message"].(map[string]any)
	if !ok {
		// Fallback: try top-level content field.
		if content, ok := raw["content"].(string); ok {
			return content, true, false
		}
		return "", false, false
	}

	content := msg["content"]
	if content == nil {
		return "", false, false
	}

	// String content (simple user message).
	if s, ok := content.(string); ok {
		return s, true, false
	}

	// Array of content blocks.
	blocks, ok := content.([]any)
	if !ok {
		return "", false, false
	}

	var texts []string
	hasText := false
	hasToolResult := false
	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			if text, ok := block["text"].(string); ok && text != "" {
				texts = append(texts, text)
				hasText = true
			}
		case "tool_result":
			hasToolResult = true
			if text, ok := block["content"].(string); ok && text != "" {
				texts = append(texts, text)
			} else if contentBlocks, ok := block["content"].([]any); ok {
				for _, cb := range contentBlocks {
					if cbMap, ok := cb.(map[string]any); ok {
						if cbMap["type"] == "text" {
							if text, ok := cbMap["text"].(string); ok && text != "" {
								texts = append(texts, text)
							}
						}
					}
				}
			}
		}
	}
	return strings.Join(texts, "\n\n"), hasText, hasToolResult
}

// extractAssistantContent extracts text from an assistant message, skipping
// thinking and tool_use blocks. Returns the text and the appropriate EventKind.
func extractAssistantContent(raw map[string]any) (string, EventKind) {
	msg, ok := raw["message"].(map[string]any)
	if !ok {
		return "", KindMessage
	}

	content := msg["content"]
	if content == nil {
		return "", KindMessage
	}

	// String content (unlikely for assistant but handle it).
	if s, ok := content.(string); ok {
		return s, KindMessage
	}

	blocks, ok := content.([]any)
	if !ok {
		return "", KindMessage
	}

	var texts []string
	hasText := false
	hasToolUse := false
	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			if text, ok := block["text"].(string); ok && text != "" {
				texts = append(texts, text)
				hasText = true
			}
		case "tool_use":
			hasToolUse = true
		}
	}

	kind := KindMessage
	if hasToolUse && !hasText {
		kind = KindAction
	}

	return strings.Join(texts, "\n\n"), kind
}

// extractProvenance builds EventProvenance from an assistant event's metadata.
func extractProvenance(raw map[string]any) *EventProvenance {
	p := &EventProvenance{}
	if msg, ok := raw["message"].(map[string]any); ok {
		p.Model, _ = msg["model"].(string)
	}
	p.AgentVersion, _ = raw["version"].(string)
	p.Branch, _ = raw["gitBranch"].(string)
	if p.Model == "" && p.AgentVersion == "" && p.Branch == "" {
		return nil
	}
	return p
}

// extractLegacyEvent handles non-Claude-Code JSONL records with flat role/content fields.
func extractLegacyEvent(raw map[string]any) (EventActor, EventKind, string) {
	actor := mapLegacyActor(raw)
	kind := mapLegacyKind(raw)

	// Extract content from flat fields.
	if content, ok := raw["content"].(string); ok {
		return actor, kind, content
	}
	if msg, ok := raw["message"].(string); ok {
		return actor, kind, msg
	}
	if text, ok := raw["text"].(string); ok {
		return actor, kind, text
	}

	return actor, kind, ""
}

// mapLegacyActor maps flat role/actor fields to EventActor.
func mapLegacyActor(raw map[string]any) EventActor {
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
	// Fall back to type field for legacy records that use type instead of role.
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
	return ActorSystem
}

// mapLegacyKind maps flat type fields to EventKind for non-Claude-Code JSONL.
func mapLegacyKind(raw map[string]any) EventKind {
	if typ, ok := raw["type"].(string); ok {
		switch strings.ToLower(typ) {
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
