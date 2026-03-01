package pack

import (
	"context"
	"fmt"

	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// RemoveResult reports what was removed.
type RemoveResult struct {
	PackID           string
	BehaviorsRemoved int
}

// Remove marks pack behaviors as forgotten and removes the pack from config.
func Remove(ctx context.Context, s store.GraphStore, packID string, cfg *config.FloopConfig) (*RemoveResult, error) {
	if err := ValidatePackID(packID); err != nil {
		return nil, fmt.Errorf("invalid pack ID: %w", err)
	}

	result := &RemoveResult{
		PackID: packID,
	}

	// 1. Find all behaviors with provenance.package == packID
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("querying nodes: %w", err)
	}

	for _, node := range nodes {
		pkgName := models.ExtractPackageName(node.Metadata)
		if pkgName != packID {
			continue
		}

		// 2. Mark as forgotten-behavior
		node.Kind = store.NodeKindForgotten
		if err := s.UpdateNode(ctx, node); err != nil {
			return nil, fmt.Errorf("marking node %s as forgotten: %w", node.ID, err)
		}
		result.BehaviorsRemoved++
	}

	// 3. Remove from config
	if cfg != nil {
		filtered := make([]config.InstalledPack, 0, len(cfg.Packs.Installed))
		for _, p := range cfg.Packs.Installed {
			if p.ID != packID {
				filtered = append(filtered, p)
			}
		}
		cfg.Packs.Installed = filtered
	}

	// 4. Sync store
	if err := s.Sync(ctx); err != nil {
		return nil, fmt.Errorf("syncing after remove: %w", err)
	}

	return result, nil
}
