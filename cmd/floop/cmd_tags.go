package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
	"github.com/nvandessel/feedback-loop/internal/tagging"
	"github.com/spf13/cobra"
)

func newTagsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tags",
		Short: "Manage behavior tags",
		Long:  `Commands for managing semantic tags on behaviors.`,
	}

	cmd.AddCommand(newTagsBackfillCmd())
	return cmd
}

func newTagsBackfillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Add tags to behaviors that have none",
		Long: `Extracts semantic tags from behavior content and adds them to
behaviors that currently have no tags. Uses the same dictionary-based
extraction that new behaviors get automatically.

Use --dry-run to preview changes without modifying the store.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			jsonOut, _ := cmd.Flags().GetBool("json")
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("opening graph store: %w", err)
			}
			defer graphStore.Close()

			return runTagsBackfill(graphStore, dryRun, jsonOut)
		},
	}

	cmd.Flags().Bool("dry-run", false, "Preview changes without modifying the store")
	cmd.Flags().String("scope", "both", "Scope: local, global, or both")
	return cmd
}

type backfillResult struct {
	BehaviorID string   `json:"behavior_id"`
	Name       string   `json:"name"`
	Tags       []string `json:"tags"`
}

type backfillOutput struct {
	Updated []backfillResult `json:"updated"`
	Skipped int              `json:"skipped"`
	Total   int              `json:"total"`
	DryRun  bool             `json:"dry_run"`
}

func runTagsBackfill(graphStore store.GraphStore, dryRun, jsonOut bool) error {
	ctx := context.Background()
	dict := tagging.NewDictionary()

	var output backfillOutput
	output.DryRun = dryRun

	nodes, err := graphStore.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return fmt.Errorf("querying behaviors: %w", err)
	}

	for _, node := range nodes {
		output.Total++
		b := models.NodeToBehavior(node)

		if len(b.Content.Tags) > 0 {
			output.Skipped++
			continue
		}

		tags := tagging.ExtractTags(b.Content.Canonical, dict)
		if len(tags) == 0 {
			output.Skipped++
			continue
		}

		if !dryRun {
			contentMap, ok := node.Content["content"].(map[string]interface{})
			if !ok {
				contentMap = make(map[string]interface{})
				node.Content["content"] = contentMap
			}
			contentMap["tags"] = tags

			if _, err := graphStore.AddNode(ctx, node); err != nil {
				return fmt.Errorf("updating node %s: %w", node.ID, err)
			}
		}

		output.Updated = append(output.Updated, backfillResult{
			BehaviorID: b.ID,
			Name:       b.Name,
			Tags:       tags,
		})
	}

	if jsonOut {
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	if dryRun {
		fmt.Println("DRY RUN — no changes made")
		fmt.Println()
	}
	fmt.Printf("Total: %d behaviors, %d would be tagged, %d skipped\n",
		output.Total, len(output.Updated), output.Skipped)

	for _, r := range output.Updated {
		fmt.Printf("  %s -> %v\n", r.Name, r.Tags)
	}

	return nil
}
