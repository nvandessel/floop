package models

import (
	"time"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// NodeToBehavior converts a store.Node to a Behavior.
// This is the canonical conversion used across the codebase for loading behaviors
// from the graph store.
func NodeToBehavior(node store.Node) Behavior {
	b := Behavior{
		ID: node.ID,
	}

	// Extract kind
	if kind, ok := node.Content["kind"].(string); ok {
		b.Kind = BehaviorKind(kind)
	}

	// Extract name
	if name, ok := node.Content["name"].(string); ok {
		b.Name = name
	}

	// Extract when conditions
	if when, ok := node.Content["when"].(map[string]interface{}); ok {
		b.When = when
	}

	// Extract content
	if content, ok := node.Content["content"].(map[string]interface{}); ok {
		if canonical, ok := content["canonical"].(string); ok {
			b.Content.Canonical = canonical
		}
		if expanded, ok := content["expanded"].(string); ok {
			b.Content.Expanded = expanded
		}
		if summary, ok := content["summary"].(string); ok {
			b.Content.Summary = summary
		}
		if structured, ok := content["structured"].(map[string]interface{}); ok {
			b.Content.Structured = structured
		}
		if tags, ok := content["tags"].([]interface{}); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					b.Content.Tags = append(b.Content.Tags, s)
				}
			}
		}
	} else if content, ok := node.Content["content"].(BehaviorContent); ok {
		b.Content = content
	}

	// Extract confidence from metadata
	if confidence, ok := node.Metadata["confidence"].(float64); ok {
		b.Confidence = confidence
	}

	// Extract priority from metadata
	if priority, ok := node.Metadata["priority"].(int); ok {
		b.Priority = priority
	}

	// Extract provenance from metadata
	if provenance, ok := node.Metadata["provenance"].(map[string]interface{}); ok {
		if sourceType, ok := provenance["source_type"].(string); ok {
			b.Provenance.SourceType = SourceType(sourceType)
		}
		if createdAt, ok := provenance["created_at"].(time.Time); ok {
			b.Provenance.CreatedAt = createdAt
		} else if createdAtStr, ok := provenance["created_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
				b.Provenance.CreatedAt = t
			}
		}
		if author, ok := provenance["author"].(string); ok {
			b.Provenance.Author = author
		}
	}

	// Extract stats from metadata
	if stats, ok := node.Metadata["stats"].(map[string]interface{}); ok {
		if activated, ok := stats["times_activated"].(int); ok {
			b.Stats.TimesActivated = activated
		}
		if followed, ok := stats["times_followed"].(int); ok {
			b.Stats.TimesFollowed = followed
		}
		if confirmed, ok := stats["times_confirmed"].(int); ok {
			b.Stats.TimesConfirmed = confirmed
		}
		if overridden, ok := stats["times_overridden"].(int); ok {
			b.Stats.TimesOverridden = overridden
		}

		// Extract time fields (stored as RFC3339 strings by SQLite store)
		if ca, ok := stats["created_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ca); err == nil {
				b.Stats.CreatedAt = t
			}
		}
		if ua, ok := stats["updated_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ua); err == nil {
				b.Stats.UpdatedAt = t
			}
		}
		if la, ok := stats["last_activated"].(string); ok {
			if t, err := time.Parse(time.RFC3339, la); err == nil {
				b.Stats.LastActivated = &t
			}
		}
		if lc, ok := stats["last_confirmed"].(string); ok {
			if t, err := time.Parse(time.RFC3339, lc); err == nil {
				b.Stats.LastConfirmed = &t
			}
		}
	}

	return b
}

// BehaviorToNode converts a Behavior to a store.Node.
func BehaviorToNode(b *Behavior) store.Node {
	return store.Node{
		ID:   b.ID,
		Kind: "behavior",
		Content: map[string]interface{}{
			"name":    b.Name,
			"kind":    string(b.Kind),
			"when":    b.When,
			"content": b.Content,
		},
		Metadata: map[string]interface{}{
			"confidence": b.Confidence,
			"priority":   b.Priority,
			"provenance": b.Provenance,
		},
	}
}
