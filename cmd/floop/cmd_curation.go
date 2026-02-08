package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/spf13/cobra"
)

func newForgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forget <behavior-id>",
		Short: "Soft-delete a behavior from active use",
		Long: `Mark a behavior as forgotten, removing it from active use.

The behavior is not deleted, just marked with kind "forgotten-behavior".
Use 'floop restore' to undo this action.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			force, _ := cmd.Flags().GetBool("force")
			reason, _ := cmd.Flags().GetString("reason")
			id := args[0]

			// JSON mode implies force (no interactive prompts)
			if jsonOut {
				force = true
			}

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, store.ScopeLocal)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Find the behavior by ID
			node, err := graphStore.GetNode(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to get behavior: %w", err)
			}
			if node == nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "behavior not found",
						"id":    id,
					})
					return nil
				}
				return fmt.Errorf("behavior not found: %s", id)
			}

			// Verify it's an active behavior
			if node.Kind != "behavior" {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error":        "not an active behavior",
						"id":           id,
						"current_kind": node.Kind,
					})
					return nil
				}
				return fmt.Errorf("not an active behavior (current kind: %s)", node.Kind)
			}

			// Get behavior name for display
			name := id
			if n, ok := node.Content["name"].(string); ok {
				name = n
			}

			// Confirm unless --force
			if !force {
				fmt.Printf("Forget behavior: %s\n", name)
				if reason != "" {
					fmt.Printf("Reason: %s\n", reason)
				}
				fmt.Print("\nConfirm? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				response, _ := reader.ReadString('\n')
				response = strings.TrimSpace(strings.ToLower(response))
				if response != "y" && response != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			// Update node to forgotten state
			now := time.Now()
			if node.Metadata == nil {
				node.Metadata = make(map[string]interface{})
			}
			node.Metadata["original_kind"] = node.Kind
			node.Metadata["forgotten_at"] = now.Format(time.RFC3339)
			node.Metadata["forgotten_by"] = os.Getenv("USER")
			if reason != "" {
				node.Metadata["forget_reason"] = reason
			}
			node.Kind = "forgotten-behavior"

			if err := graphStore.UpdateNode(ctx, *node); err != nil {
				return fmt.Errorf("failed to update behavior: %w", err)
			}

			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync changes: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":     "forgotten",
					"id":         id,
					"name":       name,
					"reason":     reason,
					"restorable": true,
				})
			} else {
				fmt.Printf("Behavior '%s' has been forgotten.\n", name)
				fmt.Println("Use 'floop restore' to undo this action.")
			}

			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Skip confirmation prompt")
	cmd.Flags().String("reason", "", "Reason for forgetting")

	return cmd
}

func newDeprecateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deprecate <behavior-id>",
		Short: "Mark a behavior as deprecated",
		Long: `Mark a behavior as deprecated but keep it visible.

Deprecated behaviors are not active but can be restored.
Use --replacement to link to a newer behavior.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			reason, _ := cmd.Flags().GetString("reason")
			replacement, _ := cmd.Flags().GetString("replacement")
			id := args[0]

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Reason is required
			if reason == "" {
				return fmt.Errorf("--reason is required for deprecation")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, store.ScopeLocal)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Find the behavior by ID
			node, err := graphStore.GetNode(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to get behavior: %w", err)
			}
			if node == nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "behavior not found",
						"id":    id,
					})
					return nil
				}
				return fmt.Errorf("behavior not found: %s", id)
			}

			// Verify it's an active behavior
			if node.Kind != "behavior" {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error":        "not an active behavior",
						"id":           id,
						"current_kind": node.Kind,
					})
					return nil
				}
				return fmt.Errorf("not an active behavior (current kind: %s)", node.Kind)
			}

			// Verify replacement exists if specified
			if replacement != "" {
				replNode, err := graphStore.GetNode(ctx, replacement)
				if err != nil {
					return fmt.Errorf("failed to get replacement behavior: %w", err)
				}
				if replNode == nil {
					return fmt.Errorf("replacement behavior not found: %s", replacement)
				}
			}

			// Get behavior name for display
			name := id
			if n, ok := node.Content["name"].(string); ok {
				name = n
			}

			// Update node to deprecated state
			now := time.Now()
			if node.Metadata == nil {
				node.Metadata = make(map[string]interface{})
			}
			node.Metadata["original_kind"] = node.Kind
			node.Metadata["deprecated_at"] = now.Format(time.RFC3339)
			node.Metadata["deprecated_by"] = os.Getenv("USER")
			node.Metadata["deprecation_reason"] = reason
			if replacement != "" {
				node.Metadata["replacement_id"] = replacement
			}
			node.Kind = "deprecated-behavior"

			if err := graphStore.UpdateNode(ctx, *node); err != nil {
				return fmt.Errorf("failed to update behavior: %w", err)
			}

			// Add deprecated-to edge if replacement specified
			if replacement != "" {
				edge := store.Edge{
					Source: id,
					Target: replacement,
					Kind:   "deprecated-to",
					Metadata: map[string]interface{}{
						"created_at": now.Format(time.RFC3339),
					},
				}
				if err := graphStore.AddEdge(ctx, edge); err != nil {
					return fmt.Errorf("failed to add deprecation edge: %w", err)
				}
			}

			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync changes: %w", err)
			}

			if jsonOut {
				result := map[string]interface{}{
					"status":     "deprecated",
					"id":         id,
					"name":       name,
					"reason":     reason,
					"restorable": true,
				}
				if replacement != "" {
					result["replacement"] = replacement
				}
				json.NewEncoder(os.Stdout).Encode(result)
			} else {
				fmt.Printf("Behavior '%s' has been deprecated.\n", name)
				fmt.Printf("Reason: %s\n", reason)
				if replacement != "" {
					fmt.Printf("Replacement: %s\n", replacement)
				}
				fmt.Println("Use 'floop restore' to undo this action.")
			}

			return nil
		},
	}

	cmd.Flags().String("reason", "", "Reason for deprecation (required)")
	cmd.Flags().String("replacement", "", "ID of behavior that replaces this one")

	return cmd
}

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <behavior-id>",
		Short: "Restore a deprecated or forgotten behavior",
		Long: `Restore a behavior that was previously deprecated or forgotten.

This undoes 'floop forget' or 'floop deprecate'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			id := args[0]

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, store.ScopeLocal)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Find the behavior by ID
			node, err := graphStore.GetNode(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to get behavior: %w", err)
			}
			if node == nil {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error": "behavior not found",
						"id":    id,
					})
					return nil
				}
				return fmt.Errorf("behavior not found: %s", id)
			}

			// Verify it's restorable (deprecated or forgotten)
			if node.Kind != "deprecated-behavior" && node.Kind != "forgotten-behavior" {
				if jsonOut {
					json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
						"error":        "behavior is not deprecated or forgotten",
						"id":           id,
						"current_kind": node.Kind,
					})
					return nil
				}
				return fmt.Errorf("behavior is not deprecated or forgotten (current kind: %s)", node.Kind)
			}

			// Get behavior name for display
			name := id
			if n, ok := node.Content["name"].(string); ok {
				name = n
			}

			previousKind := node.Kind

			// Restore original kind
			originalKind := "behavior"
			if origKind, ok := node.Metadata["original_kind"].(string); ok {
				originalKind = origKind
			}
			node.Kind = originalKind

			// Record restoration
			now := time.Now()
			node.Metadata["restored_at"] = now.Format(time.RFC3339)
			node.Metadata["restored_by"] = os.Getenv("USER")

			// Clean up curation metadata
			delete(node.Metadata, "original_kind")
			delete(node.Metadata, "forgotten_at")
			delete(node.Metadata, "forgotten_by")
			delete(node.Metadata, "forget_reason")
			delete(node.Metadata, "deprecated_at")
			delete(node.Metadata, "deprecated_by")
			delete(node.Metadata, "deprecation_reason")
			delete(node.Metadata, "replacement_id")

			if err := graphStore.UpdateNode(ctx, *node); err != nil {
				return fmt.Errorf("failed to update behavior: %w", err)
			}

			// Remove deprecated-to edges if this was deprecated
			if previousKind == "deprecated-behavior" {
				edges, err := graphStore.GetEdges(ctx, id, store.DirectionOutbound, "deprecated-to")
				if err == nil {
					for _, e := range edges {
						_ = graphStore.RemoveEdge(ctx, e.Source, e.Target, e.Kind)
					}
				}
			}

			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync changes: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":        "restored",
					"id":            id,
					"name":          name,
					"previous_kind": previousKind,
					"current_kind":  originalKind,
				})
			} else {
				fmt.Printf("Behavior '%s' has been restored.\n", name)
			}

			return nil
		},
	}

	return cmd
}

func newMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge <source-id> <target-id>",
		Short: "Merge two behaviors into one",
		Long: `Combine two similar behaviors into one.

The source behavior is marked as merged and linked to the target.
Use --into to specify which behavior survives (default: target).

This action cannot be undone with restore.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			force, _ := cmd.Flags().GetBool("force")
			into, _ := cmd.Flags().GetString("into")
			sourceID := args[0]
			targetID := args[1]

			// JSON mode implies force (no interactive prompts)
			if jsonOut {
				force = true
			}

			// Handle --into flag to swap source/target
			if into == sourceID {
				sourceID, targetID = targetID, sourceID
			} else if into != "" && into != targetID {
				return fmt.Errorf("--into must be one of the provided behavior IDs")
			}

			floopDir := filepath.Join(root, ".floop")
			if _, err := os.Stat(floopDir); os.IsNotExist(err) {
				return fmt.Errorf(".floop not initialized. Run 'floop init' first")
			}

			// Open graph store
			graphStore, err := store.NewMultiGraphStore(root, store.ScopeLocal)
			if err != nil {
				return fmt.Errorf("failed to open graph store: %w", err)
			}
			defer graphStore.Close()

			ctx := context.Background()

			// Load both behaviors
			sourceNode, err := graphStore.GetNode(ctx, sourceID)
			if err != nil {
				return fmt.Errorf("failed to get source behavior: %w", err)
			}
			if sourceNode == nil {
				return fmt.Errorf("source behavior not found: %s", sourceID)
			}

			targetNode, err := graphStore.GetNode(ctx, targetID)
			if err != nil {
				return fmt.Errorf("failed to get target behavior: %w", err)
			}
			if targetNode == nil {
				return fmt.Errorf("target behavior not found: %s", targetID)
			}

			// Verify both are active behaviors
			if sourceNode.Kind != "behavior" {
				return fmt.Errorf("source is not an active behavior (kind: %s)", sourceNode.Kind)
			}
			if targetNode.Kind != "behavior" {
				return fmt.Errorf("target is not an active behavior (kind: %s)", targetNode.Kind)
			}

			// Get names for display
			sourceName := sourceID
			if n, ok := sourceNode.Content["name"].(string); ok {
				sourceName = n
			}
			targetName := targetID
			if n, ok := targetNode.Content["name"].(string); ok {
				targetName = n
			}

			// Confirm unless --force
			if !force {
				fmt.Printf("Merge behaviors:\n")
				fmt.Printf("  Source (will be merged): %s\n", sourceName)
				fmt.Printf("  Target (will survive):   %s\n", targetName)
				fmt.Println("\nThis action cannot be undone.")
				fmt.Print("\nConfirm? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				response, _ := reader.ReadString('\n')
				response = strings.TrimSpace(strings.ToLower(response))
				if response != "y" && response != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			now := time.Now()

			// Merge when conditions (union)
			sourceWhen, _ := sourceNode.Content["when"].(map[string]interface{})
			targetWhen, _ := targetNode.Content["when"].(map[string]interface{})
			if targetWhen == nil {
				targetWhen = make(map[string]interface{})
			}
			for k, v := range sourceWhen {
				if _, exists := targetWhen[k]; !exists {
					targetWhen[k] = v
				}
			}
			targetNode.Content["when"] = targetWhen

			// Keep higher confidence
			sourceConf, _ := sourceNode.Metadata["confidence"].(float64)
			targetConf, _ := targetNode.Metadata["confidence"].(float64)
			if sourceConf > targetConf {
				targetNode.Metadata["confidence"] = sourceConf
			}

			// Keep higher priority
			sourcePrio, _ := sourceNode.Metadata["priority"].(int)
			targetPrio, _ := targetNode.Metadata["priority"].(int)
			if sourcePrio > targetPrio {
				targetNode.Metadata["priority"] = sourcePrio
			}

			// Track merge in target metadata
			mergedFrom, _ := targetNode.Metadata["merged_from"].([]interface{})
			mergedFrom = append(mergedFrom, sourceID)
			targetNode.Metadata["merged_from"] = mergedFrom
			targetNode.Metadata["last_merge_at"] = now.Format(time.RFC3339)

			// Update target
			if err := graphStore.UpdateNode(ctx, *targetNode); err != nil {
				return fmt.Errorf("failed to update target behavior: %w", err)
			}

			// Mark source as merged
			if sourceNode.Metadata == nil {
				sourceNode.Metadata = make(map[string]interface{})
			}
			sourceNode.Metadata["original_kind"] = sourceNode.Kind
			sourceNode.Metadata["merged_into"] = targetID
			sourceNode.Metadata["merged_at"] = now.Format(time.RFC3339)
			sourceNode.Metadata["merged_by"] = os.Getenv("USER")
			sourceNode.Kind = "merged-behavior"

			if err := graphStore.UpdateNode(ctx, *sourceNode); err != nil {
				return fmt.Errorf("failed to update source behavior: %w", err)
			}

			// Add merged-into edge
			edge := store.Edge{
				Source: sourceID,
				Target: targetID,
				Kind:   "merged-into",
				Metadata: map[string]interface{}{
					"merged_at": now.Format(time.RFC3339),
				},
			}
			if err := graphStore.AddEdge(ctx, edge); err != nil {
				return fmt.Errorf("failed to add merge edge: %w", err)
			}

			// Redirect edges that pointed to source to point to target
			inboundEdges, err := graphStore.GetEdges(ctx, sourceID, store.DirectionInbound, "")
			if err == nil {
				for _, e := range inboundEdges {
					if e.Kind != "merged-into" { // Don't redirect the edge we just added
						// Remove old edge
						_ = graphStore.RemoveEdge(ctx, e.Source, e.Target, e.Kind)
						// Add redirected edge
						e.Target = targetID
						_ = graphStore.AddEdge(ctx, e)
					}
				}
			}

			if err := graphStore.Sync(ctx); err != nil {
				return fmt.Errorf("failed to sync changes: %w", err)
			}

			if jsonOut {
				json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"status":       "merged",
					"source_id":    sourceID,
					"source_name":  sourceName,
					"target_id":    targetID,
					"target_name":  targetName,
					"surviving_id": targetID,
				})
			} else {
				fmt.Printf("Behaviors merged successfully.\n")
				fmt.Printf("  '%s' has been merged into '%s'\n", sourceName, targetName)
			}

			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Skip confirmation prompt")
	cmd.Flags().String("into", "", "ID of behavior that should survive (default: second argument)")

	return cmd
}
