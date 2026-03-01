package seed

import (
	"context"
	"testing"

	"github.com/nvandessel/floop/internal/store"
)

func TestSeedGlobalStore_EmptyStore(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	seeder := NewSeeder(s)

	result, err := seeder.SeedGlobalStore(context.Background())
	if err != nil {
		t.Fatalf("SeedGlobalStore() error = %v", err)
	}

	if result.Total != 9 {
		t.Errorf("Total = %d, want 9", result.Total)
	}
	if len(result.Added) != 9 {
		t.Errorf("Added = %d, want 9", len(result.Added))
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
	if len(result.Skipped) != 9 {
		t.Errorf("Skipped = %d, want 9 (idempotent)", len(result.Skipped))
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

	// First seed should be updated (version mismatch), rest added (new)
	if len(result.Updated) != 1 {
		t.Errorf("Updated = %d, want 1", len(result.Updated))
	}
	if len(result.Added) != 8 {
		t.Errorf("Added = %d, want 8", len(result.Added))
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
	forgotten.Kind = store.NodeKindForgotten
	if _, err := s.AddNode(ctx, forgotten); err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}

	seeder := NewSeeder(s)
	result, err := seeder.SeedGlobalStore(ctx)
	if err != nil {
		t.Fatalf("SeedGlobalStore() error = %v", err)
	}

	// Forgotten seed should be skipped, others should be added
	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1 (forgotten respected)", len(result.Skipped))
	}
	if len(result.Added) != 8 {
		t.Errorf("Added = %d, want 8", len(result.Added))
	}

	// Verify forgotten node is still forgotten-behavior kind
	node, err := s.GetNode(ctx, forgotten.ID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if node.Kind != store.NodeKindForgotten {
		t.Errorf("forgotten node kind = %q, want %q", node.Kind, store.NodeKindForgotten)
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

	// First seed should be skipped (exists at current version), rest added
	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1", len(result.Skipped))
	}
	if len(result.Added) != 8 {
		t.Errorf("Added = %d, want 8", len(result.Added))
	}
	if result.Total != 9 {
		t.Errorf("Total = %d, want 9", result.Total)
	}
}

func TestCoreBehaviors_Structure(t *testing.T) {
	seeds := coreBehaviors()

	if len(seeds) != 9 {
		t.Fatalf("coreBehaviors() returned %d, want 9", len(seeds))
	}

	for _, seed := range seeds {
		t.Run(seed.ID, func(t *testing.T) {
			// Verify ID prefix
			if len(seed.ID) < 5 || seed.ID[:5] != "seed-" {
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

			// Verify priority is either 100 (original) or 90 (new)
			priority := seed.Metadata["priority"]
			if priority != 100 && priority != 90 {
				t.Errorf("priority = %v, want 100 or 90", priority)
			}

			// Verify provenance
			prov, ok := seed.Metadata["provenance"].(map[string]interface{})
			if !ok {
				t.Fatal("metadata.provenance is not a map")
			}
			if prov["source_type"] != "imported" {
				t.Errorf("source_type = %v, want imported", prov["source_type"])
			}
			if prov["package"] != "floop/core" {
				t.Errorf("package = %v, want floop/core", prov["package"])
			}
			if prov["package_version"] != SeedVersion {
				t.Errorf("package_version = %v, want %s", prov["package_version"], SeedVersion)
			}
		})
	}
}

func TestSeedVersion(t *testing.T) {
	if SeedVersion != "0.4.0" {
		t.Errorf("SeedVersion = %q, want %q", SeedVersion, "0.4.0")
	}
}

func TestCoreBehaviors_SeedPrefix(t *testing.T) {
	seeds := coreBehaviors()
	for _, seed := range seeds {
		if len(seed.ID) < 5 || seed.ID[:5] != "seed-" {
			t.Errorf("seed ID %q does not have required 'seed-' prefix", seed.ID)
		}
	}
}

func TestCoreBehaviors_NoWhenConditions(t *testing.T) {
	seeds := coreBehaviors()
	for _, seed := range seeds {
		t.Run(seed.ID, func(t *testing.T) {
			if seed.Content["when"] != nil {
				t.Errorf("seed %s has 'when' conditions, but seeds should have none", seed.ID)
			}
		})
	}
}

func TestCoreBehaviors_Provenance(t *testing.T) {
	seeds := coreBehaviors()
	for _, seed := range seeds {
		t.Run(seed.ID, func(t *testing.T) {
			prov, ok := seed.Metadata["provenance"].(map[string]interface{})
			if !ok {
				t.Fatalf("seed %s: metadata.provenance is not a map", seed.ID)
			}
			if prov["package"] != "floop/core" {
				t.Errorf("package = %v, want floop/core", prov["package"])
			}
			if prov["package_version"] != SeedVersion {
				t.Errorf("package_version = %v, want %s", prov["package_version"], SeedVersion)
			}
			if prov["source_type"] != "imported" {
				t.Errorf("source_type = %v, want imported", prov["source_type"])
			}
		})
	}
}

func TestCoreBehaviors_PriorityValues(t *testing.T) {
	seeds := coreBehaviors()

	// Original two seeds should have priority 100
	originalIDs := map[string]bool{
		"seed-capture-corrections": true,
		"seed-know-floop-tools":    true,
	}

	for _, seed := range seeds {
		t.Run(seed.ID, func(t *testing.T) {
			priority := seed.Metadata["priority"]
			if originalIDs[seed.ID] {
				if priority != 100 {
					t.Errorf("original seed %s: priority = %v, want 100", seed.ID, priority)
				}
			} else {
				if priority != 90 {
					t.Errorf("new seed %s: priority = %v, want 90", seed.ID, priority)
				}
			}
		})
	}
}

func TestSeedGlobalStore_VersionUpgradeMultiple(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Pre-populate all seeds with old version
	seeds := coreBehaviors()
	for i := range seeds {
		oldSeed := seeds[i]
		oldSeed.Metadata = map[string]interface{}{
			"confidence": 1.0,
			"priority":   90,
			"provenance": map[string]interface{}{
				"source_type":     "imported",
				"package":         "floop/core",
				"package_version": "0.1.0",
			},
		}
		if _, err := s.AddNode(ctx, oldSeed); err != nil {
			t.Fatalf("AddNode(%s) error = %v", oldSeed.ID, err)
		}
	}

	seeder := NewSeeder(s)
	result, err := seeder.SeedGlobalStore(ctx)
	if err != nil {
		t.Fatalf("SeedGlobalStore() error = %v", err)
	}

	// All seeds should be updated (version mismatch)
	if len(result.Updated) != 9 {
		t.Errorf("Updated = %d, want 9", len(result.Updated))
	}
	if len(result.Added) != 0 {
		t.Errorf("Added = %d, want 0", len(result.Added))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("Skipped = %d, want 0", len(result.Skipped))
	}

	// Verify all nodes have new version
	for _, id := range result.Updated {
		node, err := s.GetNode(ctx, id)
		if err != nil {
			t.Fatalf("GetNode(%s) error = %v", id, err)
		}
		nodeProv, _ := node.Metadata["provenance"].(map[string]interface{})
		if nodeProv["package_version"] != SeedVersion {
			t.Errorf("seed %s: package_version = %v, want %s", id, nodeProv["package_version"], SeedVersion)
		}
	}
}
