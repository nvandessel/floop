package spreading

import (
	"context"

	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// StoreTagProvider implements TagProvider by loading tags from a GraphStore.
type StoreTagProvider struct {
	store store.GraphStore
}

// NewStoreTagProvider creates a new StoreTagProvider.
func NewStoreTagProvider(s store.GraphStore) *StoreTagProvider {
	return &StoreTagProvider{store: s}
}

// GetAllBehaviorTags loads all behaviors and returns their tags.
func (p *StoreTagProvider) GetAllBehaviorTags() map[string][]string {
	nodes, err := p.store.QueryNodes(context.Background(), map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil
	}

	tags := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		b := learning.NodeToBehavior(node)
		if len(b.Content.Tags) > 0 {
			tags[b.ID] = b.Content.Tags
		}
	}
	return tags
}
