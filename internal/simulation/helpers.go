package simulation

import (
	"time"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// BehaviorSpec is a flat builder for constructing behavior nodes in tests.
// It converts to store.Node via ToNode(), producing the nested Content map
// that learning.NodeToBehavior expects.
type BehaviorSpec struct {
	ID         string
	Name       string
	Kind       models.BehaviorKind
	When       map[string]interface{}
	Canonical  string
	Summary    string
	Tags       []string
	Confidence float64
	Priority   int
	Stats      models.BehaviorStats
}

// ToNode converts a BehaviorSpec into a store.Node with the nested content
// structure that learning.NodeToBehavior reads.
func (s BehaviorSpec) ToNode() store.Node {
	// Build nested content map matching learning.NodeToBehavior expectations.
	contentMap := map[string]interface{}{
		"canonical": s.Canonical,
	}
	if s.Summary != "" {
		contentMap["summary"] = s.Summary
	}
	if len(s.Tags) > 0 {
		tags := make([]interface{}, len(s.Tags))
		for i, t := range s.Tags {
			tags[i] = t
		}
		contentMap["tags"] = tags
	}

	// Build stats metadata matching learning.NodeToBehavior expectations.
	statsMap := map[string]interface{}{
		"times_activated":  s.Stats.TimesActivated,
		"times_followed":   s.Stats.TimesFollowed,
		"times_confirmed":  s.Stats.TimesConfirmed,
		"times_overridden": s.Stats.TimesOverridden,
	}
	createdAt := s.Stats.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().Add(-7 * 24 * time.Hour)
	}
	statsMap["created_at"] = createdAt.Format(time.RFC3339)
	updatedAt := s.Stats.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	statsMap["updated_at"] = updatedAt.Format(time.RFC3339)
	if s.Stats.LastActivated != nil {
		statsMap["last_activated"] = s.Stats.LastActivated.Format(time.RFC3339)
	}
	if s.Stats.LastConfirmed != nil {
		statsMap["last_confirmed"] = s.Stats.LastConfirmed.Format(time.RFC3339)
	}

	nodeContent := map[string]interface{}{
		"kind":    string(s.Kind),
		"name":    s.Name,
		"content": contentMap,
	}
	if len(s.When) > 0 {
		nodeContent["when"] = s.When
	}

	confidence := s.Confidence
	if confidence == 0 {
		confidence = 0.8
	}

	return store.Node{
		ID:      s.ID,
		Kind:    "behavior",
		Content: nodeContent,
		Metadata: map[string]interface{}{
			"confidence": confidence,
			"priority":   s.Priority,
			"stats":      statsMap,
		},
	}
}

// NewSessionContexts creates a slice of SessionContext from ContextSnapshots,
// repeating the slice to fill the requested count.
func NewSessionContexts(count int, contexts ...models.ContextSnapshot) []SessionContext {
	sessions := make([]SessionContext, count)
	for i := range sessions {
		sessions[i] = SessionContext{
			ContextSnapshot: contexts[i%len(contexts)],
		}
	}
	return sessions
}

// TimeAgo returns a time.Time that is the given duration in the past.
func TimeAgo(d time.Duration) time.Time {
	return time.Now().Add(-d)
}

// TimeAgoPtr returns a pointer to a time.Time in the past.
func TimeAgoPtr(d time.Duration) *time.Time {
	t := TimeAgo(d)
	return &t
}
