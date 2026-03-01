package pack

import (
	"context"
	"fmt"

	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// RemoveMode controls how RemoveFromPack handles the behavior.
type RemoveMode string

const (
	// RemoveModeUnassign clears provenance.package but keeps the behavior active.
	RemoveModeUnassign RemoveMode = "unassign"
	// RemoveModeForgotten marks the behavior as forgotten-behavior.
	RemoveModeForgotten RemoveMode = "forget"
)

// AddToPack stamps provenance.package on an existing behavior.
// Returns an error if the behavior doesn't exist, is forgotten, or already
// belongs to a different pack (unless force is true).
func AddToPack(ctx context.Context, s store.GraphStore, behaviorID, packID string, force bool) error {
	if err := ValidatePackID(packID); err != nil {
		return fmt.Errorf("invalid pack ID: %w", err)
	}

	node, err := s.GetNode(ctx, behaviorID)
	if err != nil {
		return fmt.Errorf("getting behavior %s: %w", behaviorID, err)
	}
	if node == nil {
		return fmt.Errorf("behavior %s not found", behaviorID)
	}

	if node.Kind == store.NodeKindForgotten {
		return fmt.Errorf("behavior %s is forgotten; restore it before adding to a pack", behaviorID)
	}

	currentPack := models.ExtractPackageName(node.Metadata)
	if currentPack != "" && currentPack != packID && !force {
		return fmt.Errorf("behavior %s already belongs to pack %q; use --force to reassign", behaviorID, currentPack)
	}

	if currentPack == packID {
		// Already a member — no-op
		return nil
	}

	// Stamp provenance.package
	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}
	prov, ok := node.Metadata["provenance"].(map[string]interface{})
	if !ok {
		prov = make(map[string]interface{})
	}
	prov["package"] = packID
	node.Metadata["provenance"] = prov

	if err := s.UpdateNode(ctx, *node); err != nil {
		return fmt.Errorf("updating behavior %s: %w", behaviorID, err)
	}

	return s.Sync(ctx)
}

// RemoveFromPack removes a behavior from its pack.
// In unassign mode, the provenance.package field is cleared but the behavior stays active.
// In forget mode, the behavior is marked as forgotten-behavior.
func RemoveFromPack(ctx context.Context, s store.GraphStore, behaviorID string, mode RemoveMode) error {
	node, err := s.GetNode(ctx, behaviorID)
	if err != nil {
		return fmt.Errorf("getting behavior %s: %w", behaviorID, err)
	}
	if node == nil {
		return fmt.Errorf("behavior %s not found", behaviorID)
	}

	currentPack := models.ExtractPackageName(node.Metadata)
	if currentPack == "" {
		return fmt.Errorf("behavior %s does not belong to any pack", behaviorID)
	}

	switch mode {
	case RemoveModeForgotten:
		node.Kind = store.NodeKindForgotten
	case RemoveModeUnassign:
		// Clear provenance.package but keep the behavior active
		if prov, ok := node.Metadata["provenance"].(map[string]interface{}); ok {
			delete(prov, "package")
			delete(prov, "package_version")
			node.Metadata["provenance"] = prov
		}
	default:
		return fmt.Errorf("unknown remove mode: %q", mode)
	}

	if err := s.UpdateNode(ctx, *node); err != nil {
		return fmt.Errorf("updating behavior %s: %w", behaviorID, err)
	}

	return s.Sync(ctx)
}
