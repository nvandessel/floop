package pack

import (
	"context"
	"testing"

	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/store"
)

func TestRemove_MarksAsForgotten(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()

	// Add behaviors from a pack
	s.AddNode(ctx, store.Node{
		ID:   "b-rm-1",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "removable-1",
		},
		Metadata: map[string]interface{}{
			"provenance": map[string]interface{}{
				"package":         "test-org/rm-pack",
				"package_version": "1.0.0",
			},
		},
	})
	s.AddNode(ctx, store.Node{
		ID:   "b-rm-2",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "removable-2",
		},
		Metadata: map[string]interface{}{
			"provenance": map[string]interface{}{
				"package":         "test-org/rm-pack",
				"package_version": "1.0.0",
			},
		},
	})

	// Add a behavior from a different pack (should not be affected)
	s.AddNode(ctx, store.Node{
		ID:   "b-other",
		Kind: "behavior",
		Content: map[string]interface{}{
			"name": "other-behavior",
		},
		Metadata: map[string]interface{}{
			"provenance": map[string]interface{}{
				"package":         "other-org/other-pack",
				"package_version": "1.0.0",
			},
		},
	})

	cfg := config.Default()
	cfg.Packs.Installed = []config.InstalledPack{
		{ID: "test-org/rm-pack", Version: "1.0.0", BehaviorCount: 2},
		{ID: "other-org/other-pack", Version: "1.0.0", BehaviorCount: 1},
	}

	result, err := Remove(ctx, s, "test-org/rm-pack", cfg)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if result.BehaviorsRemoved != 2 {
		t.Errorf("BehaviorsRemoved = %d, want 2", result.BehaviorsRemoved)
	}

	// Verify behaviors are marked as forgotten
	n1, _ := s.GetNode(ctx, "b-rm-1")
	if n1.Kind != store.NodeKindForgotten {
		t.Errorf("b-rm-1 Kind = %q, want %q", n1.Kind, store.NodeKindForgotten)
	}
	n2, _ := s.GetNode(ctx, "b-rm-2")
	if n2.Kind != store.NodeKindForgotten {
		t.Errorf("b-rm-2 Kind = %q, want %q", n2.Kind, store.NodeKindForgotten)
	}

	// Verify the other behavior is unaffected
	nOther, _ := s.GetNode(ctx, "b-other")
	if nOther.Kind != "behavior" {
		t.Errorf("b-other Kind = %q, want %q", nOther.Kind, "behavior")
	}

	// Verify config updated
	if len(cfg.Packs.Installed) != 1 {
		t.Fatalf("Installed count = %d, want 1", len(cfg.Packs.Installed))
	}
	if cfg.Packs.Installed[0].ID != "other-org/other-pack" {
		t.Errorf("remaining pack ID = %q, want %q", cfg.Packs.Installed[0].ID, "other-org/other-pack")
	}
}

func TestRemove_UnknownPack(t *testing.T) {
	s := store.NewInMemoryGraphStore()
	ctx := context.Background()
	cfg := config.Default()

	result, err := Remove(ctx, s, "nonexistent/pack", cfg)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if result.BehaviorsRemoved != 0 {
		t.Errorf("BehaviorsRemoved = %d, want 0", result.BehaviorsRemoved)
	}
}
