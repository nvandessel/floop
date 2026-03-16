// Package events provides types and storage for captured conversation events.
package events

import "time"

// EventActor identifies who produced an event.
type EventActor string

const (
	ActorUser   EventActor = "user"
	ActorAgent  EventActor = "agent"
	ActorTool   EventActor = "tool"
	ActorSystem EventActor = "system"
)

// EventKind categorizes what type of event this is.
type EventKind string

const (
	KindMessage    EventKind = "message"
	KindAction     EventKind = "action"
	KindResult     EventKind = "result"
	KindError      EventKind = "error"
	KindCorrection EventKind = "correction"
)

// Event represents a single captured conversation event.
type Event struct {
	ID         string           `json:"id"`
	SessionID  string           `json:"session_id"`
	Timestamp  time.Time        `json:"timestamp"`
	Source     string           `json:"source"`
	Actor      EventActor       `json:"actor"`
	Kind       EventKind        `json:"kind"`
	Content    string           `json:"content"`
	Metadata   map[string]any   `json:"metadata,omitempty"`
	ProjectID  string           `json:"project_id,omitempty"`
	Provenance *EventProvenance `json:"provenance,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
}

// EventProvenance tracks optional context about the event source.
type EventProvenance struct {
	Model        string `json:"model,omitempty"`
	ModelVersion string `json:"model_version,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	Branch       string `json:"branch,omitempty"`
	TaskContext  string `json:"task_context,omitempty"`
}
