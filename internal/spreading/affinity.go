package spreading

import (
	"context"
	"time"

	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/tagging"
)

// AffinityConfig controls virtual edge generation from shared tags.
type AffinityConfig struct {
	// Enabled controls whether feature affinity is active.
	Enabled bool

	// MaxWeight is the maximum weight for a virtual affinity edge.
	// The actual weight is Jaccard similarity * MaxWeight.
	// Default: 0.4 (lower than explicit edges which default to ~0.8).
	MaxWeight float64

	// MinJaccard is the minimum Jaccard similarity to create a virtual edge.
	// Default: 0.3
	MinJaccard float64
}

// DefaultAffinityConfig returns the default feature affinity configuration.
func DefaultAffinityConfig() AffinityConfig {
	return AffinityConfig{
		Enabled:    true,
		MaxWeight:  0.4,
		MinJaccard: 0.3,
	}
}

// TagProvider loads tags for all behaviors in the store.
// This is an interface so the engine doesn't depend on concrete store implementations.
type TagProvider interface {
	// GetAllBehaviorTags returns a map of behavior ID → tags.
	GetAllBehaviorTags(ctx context.Context) map[string][]string
}

// virtualAffinityEdges generates virtual edges from tag overlap between
// nodeID and all other behaviors in the tag map.
// Virtual edges use kind "feature-affinity" and are created fresh each
// activation cycle (CreatedAt set to now), so temporal decay has negligible
// effect on them in practice.
func virtualAffinityEdges(nodeID string, nodeTags []string, allTags map[string][]string, config AffinityConfig) []store.Edge {
	if len(nodeTags) == 0 {
		return nil
	}

	var edges []store.Edge
	for otherID, otherTags := range allTags {
		if otherID == nodeID {
			continue
		}
		if len(otherTags) == 0 {
			continue
		}

		jaccard := tagging.JaccardSimilarity(nodeTags, otherTags)
		if jaccard < config.MinJaccard {
			continue
		}

		weight := jaccard * config.MaxWeight

		edges = append(edges, store.Edge{
			Source:    nodeID,
			Target:    otherID,
			Kind:      "feature-affinity",
			Weight:    weight,
			CreatedAt: time.Now(),
		})
	}

	return edges
}
