package seed

import (
	"context"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/store"
)

func TestSeedGlobalStore_EmptyStore(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	seeder := NewSeeder(s)

	result, err := seeder.SeedGlobalStore(context.Background())
	if err != nil {
		t.Fatalf("SeedGlobalStore() error = %v", err)
	}

	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if len(result.Added) != 2 {
		t.Errorf("Added = %d, want 2", len(result.Added))
	}
	if len(result.Updated) != 0 {
		t.Errorf("Updated = %d, want 0", len(result.Updated))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("Skipped = %d, want 0", len(result.Skipped))
	}

	// Verify nodes actually exist in store
	ctx := context.Background()
	for _, id := range result.Added {
		node, err := s.GetNode(ctx, id)
		if err != nil {
			t.Fatalf("GetNode(%s) error = %v", id, err)
		}
		if node == nil {
			t.Fatalf("node %s not found in store after seeding", id)
		}
		if node.Kind != "behavior" {
			t.Errorf("node %s kind = %q, want %q", id, node.Kind, "behavior")
		}
	}
}

func TestSeedGlobalStore_Idempotent(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	seeder := NewSeeder(s)
	ctx := context.Background()

	// First seed
	_, err := seeder.SeedGlobalStore(ctx)
	if err != nil {
		t.Fatalf("first SeedGlobalStore() error = %v", err)
	}

	// Second seed should skip all
	result, err := seeder.SeedGlobalStore(ctx)
	if err != nil {
		t.Fatalf("second SeedGlobalStore() error = %v", err)
	}

	if len(result.Added) != 0 {
		t.Errorf("Added = %d, want 0 (idempotent)", len(result.Added))
	}
	if len(result.Skipped) != 2 {
		t.Errorf("Skipped = %d, want 2 (idempotent)", len(result.Skipped))
	}
}

func TestSeedGlobalStore_VersionUpgrade(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Pre-populate with old version seed
	oldSeed := coreBehaviors()[0]
	prov, _ := oldSeed.Metadata["provenance"].(map[string]interface{})
	prov["package_version"] = "0.0.1" // older version
	oldSeed.Metadata["provenance"] = prov
	if _, err := s.AddNode(ctx, oldSeed); err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	seeder := NewSeeder(s)
	result, err := seeder.SeedGlobalStore(ctx)
	if err != nil {
		t.Fatalf("SeedGlobalStore() error = %v", err)
	}

	// First seed should be updated (version mismatch), second added (new)
	if len(result.Updated) != 1 {
		t.Errorf("Updated = %d, want 1", len(result.Updated))
	}
	if len(result.Added) != 1 {
		t.Errorf("Added = %d, want 1", len(result.Added))
	}

	// Verify updated node has new version
	node, err := s.GetNode(ctx, oldSeed.ID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	nodeProv, _ := node.Metadata["provenance"].(map[string]interface{})
	if nodeProv["package_version"] != SeedVersion {
		t.Errorf("package_version = %v, want %s", nodeProv["package_version"], SeedVersion)
	}
}

func TestSeedGlobalStore_RespectsForgotten(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Pre-populate with a seed marked as forgotten-behavior
	forgotten := coreBehaviors()[0]
	forgotten.Kind = "forgotten-behavior"
	if _, err := s.AddNode(ctx, forgotten); err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	seeder := NewSeeder(s)
	result, err := seeder.SeedGlobalStore(ctx)
	if err != nil {
		t.Fatalf("SeedGlobalStore() error = %v", err)
	}

	// Forgotten seed should be skipped, other should be added
	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1 (forgotten respected)", len(result.Skipped))
	}
	if len(result.Added) != 1 {
		t.Errorf("Added = %d, want 1", len(result.Added))
	}

	// Verify forgotten node is still forgotten-behavior kind
	node, err := s.GetNode(ctx, forgotten.ID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if node.Kind != "forgotten-behavior" {
		t.Errorf("forgotten node kind = %q, want %q", node.Kind, "forgotten-behavior")
	}
}

func TestSeedGlobalStore_PartialSeeding(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Pre-populate with only one seed (current version)
	seeds := coreBehaviors()
	if _, err := s.AddNode(ctx, seeds[0]); err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	seeder := NewSeeder(s)
	result, err := seeder.SeedGlobalStore(ctx)
	if err != nil {
		t.Fatalf("SeedGlobalStore() error = %v", err)
	}

	// First seed should be skipped (exists at current version), second added
	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1", len(result.Skipped))
	}
	if len(result.Added) != 1 {
		t.Errorf("Added = %d, want 1", len(result.Added))
	}
	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
}

func TestCoreBehaviors_Structure(t *testing.T) {
	seeds := coreBehaviors()

	if len(seeds) != 2 {
		t.Fatalf("coreBehaviors() returned %d, want 2", len(seeds))
	}

	for _, seed := range seeds {
		t.Run(seed.ID, func(t *testing.T) {
			// Verify ID prefix
			if seed.ID[:5] != "seed-" {
				t.Errorf("ID %q doesn't start with 'seed-'", seed.ID)
			}

			// Verify kind
			if seed.Kind != "behavior" {
				t.Errorf("Kind = %q, want %q", seed.Kind, "behavior")
			}

			// Verify required content fields
			if seed.Content["name"] == nil {
				t.Error("missing content.name")
			}
			if seed.Content["kind"] == nil {
				t.Error("missing content.kind")
			}
			contentMap, ok := seed.Content["content"].(map[string]interface{})
			if !ok {
				t.Fatal("content.content is not a map")
			}
			if contentMap["canonical"] == nil {
				t.Error("missing content.content.canonical")
			}

			// Verify metadata
			if seed.Metadata["confidence"] != 1.0 {
				t.Errorf("confidence = %v, want 1.0", seed.Metadata["confidence"])
			}
			if seed.Metadata["priority"] != 100 {
				t.Errorf("priority = %v, want 100", seed.Metadata["priority"])
			}

			// Verify provenance
			prov, ok := seed.Metadata["provenance"].(map[string]interface{})
			if !ok {
				t.Fatal("metadata.provenance is not a map")
			}
			if prov["source_type"] != "imported" {
				t.Errorf("source_type = %v, want imported", prov["source_type"])
			}
			if prov["package"] != "floop-core" {
				t.Errorf("package = %v, want floop-core", prov["package"])
			}
			if prov["package_version"] != SeedVersion {
				t.Errorf("package_version = %v, want %s", prov["package_version"], SeedVersion)
			}
		})
	}
}
