package events

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"
)

var eventCounter atomic.Int64
var sessionCounter atomic.Int64

// generateEventID produces a unique event ID using timestamp and counter.
func generateEventID() string {
	n := eventCounter.Add(1)
	return fmt.Sprintf("evt-%d-%d", time.Now().UnixNano(), n)
}

// generateSessionID produces a unique session ID using timestamp and counter.
func generateSessionID() string {
	n := sessionCounter.Add(1)
	return fmt.Sprintf("session-%d-%d", time.Now().UnixNano(), n)
}

// MarkdownAdapter parses markdown-style transcripts with actor prefixes.
type MarkdownAdapter struct {
	Source string // agent source identifier
}

// Format returns the adapter format name.
func (a *MarkdownAdapter) Format() string { return "markdown" }

// Parse reads a markdown transcript and produces Events.
// It detects "User:", "Human:", "Assistant:", "AI:", "Tool:", "System:" prefixes
// and groups consecutive lines into events.
func (a *MarkdownAdapter) Parse(reader io.Reader) ([]Event, error) {
	scanner := bufio.NewScanner(reader)
	sessionID := generateSessionID()
	now := time.Now()

	var events []Event
	var currentActor EventActor
	var currentContent strings.Builder
	inBlock := false

	flush := func() {
		if !inBlock {
			return
		}
		content := strings.TrimSpace(currentContent.String())
		if content == "" {
			return
		}
		events = append(events, Event{
			ID:        generateEventID(),
			SessionID: sessionID,
			Timestamp: now.Add(time.Duration(len(events)) * time.Millisecond),
			Source:    a.Source,
			Actor:     currentActor,
			Kind:      KindMessage,
			Content:   content,
			CreatedAt: now,
		})
		currentContent.Reset()
		inBlock = false
	}

	for scanner.Scan() {
		line := scanner.Text()

		actor, rest, matched := detectActorPrefix(line)
		if matched {
			flush()
			currentActor = actor
			inBlock = true
			currentContent.WriteString(strings.TrimSpace(rest))
		} else if inBlock {
			// Continuation line for current actor block
			if currentContent.Len() > 0 {
				currentContent.WriteString("\n")
			}
			currentContent.WriteString(line)
		}
		// Lines before any actor prefix are ignored
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning markdown transcript: %w", err)
	}

	flush()

	return events, nil
}

// detectActorPrefix checks if a line starts with a known actor prefix.
// Returns the actor, the remainder of the line, and whether a match was found.
func detectActorPrefix(line string) (EventActor, string, bool) {
	prefixes := []struct {
		prefix string
		actor  EventActor
	}{
		{"User:", ActorUser},
		{"Human:", ActorUser},
		{"Assistant:", ActorAgent},
		{"AI:", ActorAgent},
		{"Tool:", ActorTool},
		{"System:", ActorSystem},
	}

	for _, p := range prefixes {
		if strings.HasPrefix(line, p.prefix) {
			return p.actor, line[len(p.prefix):], true
		}
	}
	return "", "", false
}
