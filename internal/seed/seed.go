package seed

import (
	"context"
	"fmt"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// Seeder handles injecting seed behaviors into a store.
type Seeder struct {
	store store.GraphStore
}

// NewSeeder creates a new Seeder for the given store.
func NewSeeder(s store.GraphStore) *Seeder {
	return &Seeder{store: s}
}

// SeedResult reports what the seeder did.
type SeedResult struct {
	Added   []string // IDs of newly added seeds
	Updated []string // IDs of seeds updated (version upgrade)
	Skipped []string // IDs of seeds skipped (up-to-date or forgotten)
	Total   int      // Total number of seed definitions
}

// SeedGlobalStore ensures all seed behaviors exist in the store.
// It is idempotent: seeds at the current version are skipped,
// outdated seeds are updated, and forgotten seeds are respected.
func (s *Seeder) SeedGlobalStore(ctx context.Context) (*SeedResult, error) {
	seeds := coreBehaviors()
	result := &SeedResult{Total: len(seeds)}

	for _, seed := range seeds {
		existing, err := s.store.GetNode(ctx, seed.ID)
		if err != nil {
			return nil, fmt.Errorf("checking seed %s: %w", seed.ID, err)
		}

		if existing == nil {
			// New seed — add it
			if _, err := s.store.AddNode(ctx, seed); err != nil {
				return nil, fmt.Errorf("adding seed %s: %w", seed.ID, err)
			}
			result.Added = append(result.Added, seed.ID)
			continue
		}

		// Respect user curation: don't re-add forgotten behaviors
		if existing.Kind == store.NodeKindForgotten {
			result.Skipped = append(result.Skipped, seed.ID)
			continue
		}

		// Check version for upgrade
		existingVersion := models.ExtractPackageVersion(existing.Metadata)
		if existingVersion != SeedVersion {
			// Version mismatch — update content
			if err := s.store.UpdateNode(ctx, seed); err != nil {
				return nil, fmt.Errorf("updating seed %s: %w", seed.ID, err)
			}
			result.Updated = append(result.Updated, seed.ID)
			continue
		}

		// Already up-to-date
		result.Skipped = append(result.Skipped, seed.ID)
	}

	if err := s.store.Sync(ctx); err != nil {
		return nil, fmt.Errorf("syncing after seeding: %w", err)
	}

	return result, nil
}
