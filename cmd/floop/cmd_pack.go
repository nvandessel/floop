package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/pack"
	"github.com/nvandessel/floop/internal/store"
	"github.com/spf13/cobra"
)

func newPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Manage skill packs (create, install, list, remove)",
		Long: `Skill packs are portable behavior collections that can be shared and installed.

Examples:
  floop pack create my-pack.fpack --id my-org/my-pack --version 1.0.0
  floop pack install my-pack.fpack
  floop pack list
  floop pack info my-org/my-pack
  floop pack remove my-org/my-pack`,
	}

	cmd.AddCommand(
		newPackCreateCmd(),
		newPackInstallCmd(),
		newPackListCmd(),
		newPackInfoCmd(),
		newPackUpdateCmd(),
		newPackRemoveCmd(),
		newPackAddCmd(),
		newPackRemoveBehaviorCmd(),
	)

	return cmd
}

func newPackCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <output-path>",
		Short: "Create a skill pack from current behaviors",
		Long: `Export filtered behaviors into a portable .fpack file.

Examples:
  floop pack create my-pack.fpack --id my-org/my-pack --version 1.0.0
  floop pack create my-pack.fpack --id my-org/my-pack --version 1.0.0 --filter-tags go,testing
  floop pack create my-pack.fpack --id my-org/my-pack --version 1.0.0 --filter-scope global`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputPath := args[0]
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			id, _ := cmd.Flags().GetString("id")
			ver, _ := cmd.Flags().GetString("version")
			desc, _ := cmd.Flags().GetString("description")
			author, _ := cmd.Flags().GetString("author")
			tags, _ := cmd.Flags().GetString("tags")
			source, _ := cmd.Flags().GetString("source")
			filterTags, _ := cmd.Flags().GetString("filter-tags")
			filterScope, _ := cmd.Flags().GetString("filter-scope")
			filterKinds, _ := cmd.Flags().GetString("filter-kinds")
			fromPack, _ := cmd.Flags().GetString("from-pack")

			manifest := pack.PackManifest{
				ID:          pack.PackID(id),
				Version:     ver,
				Description: desc,
				Author:      author,
				Source:      source,
			}
			if tags != "" {
				manifest.Tags = strings.Split(tags, ",")
			}

			filter := pack.CreateFilter{
				Scope:    filterScope,
				FromPack: fromPack,
			}
			if filterTags != "" {
				filter.Tags = strings.Split(filterTags, ",")
			}
			if filterKinds != "" {
				filter.Kinds = strings.Split(filterKinds, ",")
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			result, err := pack.Create(ctx, graphStore, filter, manifest, outputPath, pack.CreateOptions{
				FloopVersion: version,
			})
			if err != nil {
				return fmt.Errorf("pack create failed: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"path":           result.Path,
					"behavior_count": result.BehaviorCount,
					"edge_count":     result.EdgeCount,
					"pack_id":        id,
					"version":        ver,
					"message":        fmt.Sprintf("Pack created: %d behaviors, %d edges", result.BehaviorCount, result.EdgeCount),
				})
			}

			fmt.Printf("Pack created: %d behaviors, %d edges\n", result.BehaviorCount, result.EdgeCount)
			fmt.Printf("  ID: %s\n", id)
			fmt.Printf("  Version: %s\n", ver)
			fmt.Printf("  Path: %s\n", result.Path)
			return nil
		},
	}

	cmd.Flags().String("id", "", "Pack ID in namespace/name format (required)")
	cmd.Flags().String("version", "", "Pack version (required)")
	cmd.Flags().String("description", "", "Pack description")
	cmd.Flags().String("author", "", "Pack author")
	cmd.Flags().String("tags", "", "Comma-separated pack tags")
	cmd.Flags().String("source", "", "Pack source URL")
	cmd.Flags().String("filter-tags", "", "Filter: only include behaviors with these tags (comma-separated)")
	cmd.Flags().String("filter-scope", "", "Filter: only include behaviors from this scope (global/local)")
	cmd.Flags().String("filter-kinds", "", "Filter: only include behaviors of these kinds (comma-separated)")
	cmd.Flags().String("from-pack", "", "Filter: only include behaviors belonging to this pack (by provenance)")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("version")

	return cmd
}

func newPackInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <source>",
		Short: "Install a skill pack from a file, URL, or GitHub repo",
		Long: `Install behaviors from a skill pack into the store.

Supports local files, HTTP URLs, and GitHub shorthand sources.
Follows the seeder pattern: forgotten behaviors are not re-added,
existing behaviors are version-gated for updates, and provenance
is stamped on each installed behavior.

Examples:
  floop pack install my-pack.fpack
  floop pack install https://example.com/pack.fpack
  floop pack install gh:owner/repo
  floop pack install gh:owner/repo@v1.0.0
  floop pack install gh:owner/repo --all-assets`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			deriveEdges, _ := cmd.Flags().GetBool("derive-edges")
			allAssets, _ := cmd.Flags().GetBool("all-assets")

			cfg, err := config.Load()
			if err != nil {
				cfg = config.Default()
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			results, err := pack.InstallFromSource(ctx, graphStore, source, cfg, pack.InstallFromSourceOptions{
				DeriveEdges: deriveEdges,
				AllAssets:   allAssets,
			})
			if err != nil {
				return fmt.Errorf("pack install failed: %w", err)
			}

			// Save config with updated pack list
			if saveErr := cfg.Save(); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save config: %v\n", saveErr)
			}

			if jsonOut {
				jsonResults := make([]map[string]interface{}, 0, len(results))
				for _, result := range results {
					jsonResults = append(jsonResults, map[string]interface{}{
						"pack_id":       result.PackID,
						"version":       result.Version,
						"added":         result.Added,
						"updated":       result.Updated,
						"skipped":       result.Skipped,
						"edges_added":   result.EdgesAdded,
						"edges_skipped": result.EdgesSkipped,
						"derived_edges": result.DerivedEdges,
						"message":       fmt.Sprintf("Installed %s v%s: %d added, %d updated, %d skipped", result.PackID, result.Version, len(result.Added), len(result.Updated), len(result.Skipped)),
					})
				}
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"results": jsonResults,
				})
			}

			for _, result := range results {
				fmt.Printf("Installed %s v%s\n", result.PackID, result.Version)
				fmt.Printf("  Added: %d behaviors\n", len(result.Added))
				fmt.Printf("  Updated: %d behaviors\n", len(result.Updated))
				fmt.Printf("  Skipped: %d behaviors\n", len(result.Skipped))
				fmt.Printf("  Edges: %d added, %d skipped\n", result.EdgesAdded, result.EdgesSkipped)
				if result.DerivedEdges > 0 {
					fmt.Printf("  Derived edges: %d\n", result.DerivedEdges)
				}
			}
			return nil
		},
	}

	cmd.Flags().Bool("derive-edges", false, "Automatically derive edges between pack behaviors and existing behaviors")
	cmd.Flags().Bool("all-assets", false, "Install all .fpack assets from a multi-asset release")

	return cmd
}

func newPackListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skill packs",
		Long: `Show all currently installed skill packs from config.

Examples:
  floop pack list
  floop pack list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")

			cfg, err := config.Load()
			if err != nil {
				cfg = config.Default()
			}

			installed := pack.ListInstalled(cfg)

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"installed": installed,
					"count":     len(installed),
				})
			}

			if len(installed) == 0 {
				fmt.Println("No skill packs installed.")
				return nil
			}

			fmt.Printf("Installed packs (%d):\n", len(installed))
			for _, p := range installed {
				fmt.Printf("  %s v%s (%d behaviors, %d edges)\n", p.ID, p.Version, p.BehaviorCount, p.EdgeCount)
				if !p.InstalledAt.IsZero() {
					fmt.Printf("    Installed: %s\n", p.InstalledAt.Format("2006-01-02 15:04:05"))
				}
			}
			return nil
		},
	}

	return cmd
}

func newPackInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <pack-id>",
		Short: "Show details of an installed skill pack",
		Long: `Display pack details and behavior count from the store.

Examples:
  floop pack info my-org/my-pack
  floop pack info my-org/my-pack --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			packID := args[0]
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")

			cfg, err := config.Load()
			if err != nil {
				cfg = config.Default()
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			// Find pack in config
			var installed *config.InstalledPack
			for _, p := range cfg.Packs.Installed {
				if p.ID == packID {
					installed = &p
					break
				}
			}

			// Find behaviors in store
			behaviors, err := pack.FindByPack(ctx, graphStore, packID)
			if err != nil {
				return fmt.Errorf("querying pack behaviors: %w", err)
			}

			if jsonOut {
				out := map[string]interface{}{
					"pack_id":        packID,
					"behavior_count": len(behaviors),
				}
				if installed != nil {
					out["version"] = installed.Version
					out["installed_at"] = installed.InstalledAt
					out["edge_count"] = installed.EdgeCount
				}
				return json.NewEncoder(os.Stdout).Encode(out)
			}

			if installed == nil && len(behaviors) == 0 {
				fmt.Printf("Pack %q not found.\n", packID)
				return nil
			}

			fmt.Printf("Pack: %s\n", packID)
			if installed != nil {
				fmt.Printf("  Version: %s\n", installed.Version)
				if !installed.InstalledAt.IsZero() {
					fmt.Printf("  Installed: %s\n", installed.InstalledAt.Format("2006-01-02 15:04:05"))
				}
				fmt.Printf("  Config edges: %d\n", installed.EdgeCount)
			}
			fmt.Printf("  Behaviors in store: %d\n", len(behaviors))
			for _, b := range behaviors {
				name := ""
				if content, ok := b.Content["name"].(string); ok {
					name = content
				}
				fmt.Printf("    - %s (%s)\n", b.ID, name)
			}
			return nil
		},
	}

	return cmd
}

func newPackUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [pack-id|source]",
		Short: "Update installed packs from their remote sources",
		Long: `Update an installed pack by re-fetching from its recorded source, or update
all packs that have remote sources.

When given a pack ID, looks up the installed pack's source and re-fetches it.
When given a source string (file path, URL, or gh: shorthand), installs directly.
When used with --all, updates every installed pack that has a recorded source.

For GitHub sources, the remote release version is checked first; if the
installed version already matches, the download is skipped.

Examples:
  floop pack update my-org/my-pack
  floop pack update gh:owner/repo@v2.0.0
  floop pack update my-pack-v2.fpack
  floop pack update --all`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			deriveEdges, _ := cmd.Flags().GetBool("derive-edges")
			allPacks, _ := cmd.Flags().GetBool("all")

			cfg, err := config.Load()
			if err != nil {
				cfg = config.Default()
			}

			if allPacks && len(args) > 0 {
				return fmt.Errorf("cannot use --all with a specific pack")
			}
			if !allPacks && len(args) == 0 {
				return fmt.Errorf("provide a pack ID or source, or use --all")
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			opts := pack.InstallFromSourceOptions{
				DeriveEdges: deriveEdges,
			}

			// Collect (source, packID) pairs to update
			type updateTarget struct {
				source           string
				packID           string
				installedVersion string
			}
			var targets []updateTarget

			if allPacks {
				for _, p := range cfg.Packs.Installed {
					if p.Source == "" {
						fmt.Fprintf(os.Stderr, "skipping %s: no recorded source\n", p.ID)
						continue
					}
					targets = append(targets, updateTarget{
						source:           p.Source,
						packID:           p.ID,
						installedVersion: p.Version,
					})
				}
				if len(targets) == 0 {
					fmt.Println("No packs with remote sources to update.")
					return nil
				}
			} else {
				arg := args[0]
				source := ""

				// Check if arg is an installed pack ID
				for _, p := range cfg.Packs.Installed {
					if p.ID == arg {
						if p.Source == "" {
							return fmt.Errorf("pack %q has no recorded source; reinstall from a remote source or provide one directly", arg)
						}
						source = p.Source
						targets = append(targets, updateTarget{
							source:           source,
							packID:           p.ID,
							installedVersion: p.Version,
						})
						break
					}
				}

				// Not found as pack ID -- treat as a source string
				if len(targets) == 0 {
					targets = append(targets, updateTarget{
						source: arg,
					})
				}
			}

			var allResults []*pack.InstallResult

			for _, t := range targets {
				// Version check for GitHub sources: skip if already up-to-date
				resolved, err := pack.ResolveSource(t.source)
				if err != nil {
					return fmt.Errorf("resolving source %q: %w", t.source, err)
				}

				if resolved.Kind == pack.SourceGitHub && t.installedVersion != "" {
					gh := pack.NewGitHubClient()
					release, err := gh.ResolveRelease(ctx, resolved.Owner, resolved.Repo, resolved.Version)
					if err != nil {
						return fmt.Errorf("checking release for %s: %w", t.source, err)
					}
					remoteVersion := strings.TrimPrefix(release.TagName, "v")
					installedVersion := strings.TrimPrefix(t.installedVersion, "v")
					if remoteVersion == installedVersion {
						label := t.packID
						if label == "" {
							label = t.source
						}
						fmt.Printf("%s is already up-to-date (v%s)\n", label, remoteVersion)
						continue
					}
				}

				results, err := pack.InstallFromSource(ctx, graphStore, t.source, cfg, opts)
				if err != nil {
					if allPacks {
						fmt.Fprintf(os.Stderr, "warning: failed to update %s: %v\n", t.packID, err)
						continue
					}
					return fmt.Errorf("pack update failed: %w", err)
				}
				allResults = append(allResults, results...)
			}

			if saveErr := cfg.Save(); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save config: %v\n", saveErr)
			}

			if jsonOut {
				jsonResults := make([]map[string]interface{}, 0, len(allResults))
				for _, result := range allResults {
					jsonResults = append(jsonResults, map[string]interface{}{
						"pack_id":       result.PackID,
						"version":       result.Version,
						"added":         result.Added,
						"updated":       result.Updated,
						"skipped":       result.Skipped,
						"edges_added":   result.EdgesAdded,
						"edges_skipped": result.EdgesSkipped,
						"derived_edges": result.DerivedEdges,
						"message":       fmt.Sprintf("Updated %s to v%s: %d added, %d updated, %d skipped", result.PackID, result.Version, len(result.Added), len(result.Updated), len(result.Skipped)),
					})
				}
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"results": jsonResults,
				})
			}

			for _, result := range allResults {
				fmt.Printf("Updated %s to v%s\n", result.PackID, result.Version)
				fmt.Printf("  Added: %d behaviors\n", len(result.Added))
				fmt.Printf("  Updated: %d behaviors\n", len(result.Updated))
				fmt.Printf("  Skipped: %d behaviors\n", len(result.Skipped))
				fmt.Printf("  Edges: %d added, %d skipped\n", result.EdgesAdded, result.EdgesSkipped)
				if result.DerivedEdges > 0 {
					fmt.Printf("  Derived edges: %d\n", result.DerivedEdges)
				}
			}
			return nil
		},
	}

	cmd.Flags().Bool("derive-edges", false, "Automatically derive edges between pack behaviors and existing behaviors")
	cmd.Flags().Bool("all", false, "Update all installed packs that have remote sources")

	return cmd
}

func newPackRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <pack-id>",
		Short: "Remove an installed skill pack",
		Long: `Remove a pack by marking its behaviors as forgotten and removing
the pack from the installed packs list.

Examples:
  floop pack remove my-org/my-pack
  floop pack remove my-org/my-pack --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			packID := args[0]
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")

			cfg, err := config.Load()
			if err != nil {
				cfg = config.Default()
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			result, err := pack.Remove(ctx, graphStore, packID, cfg)
			if err != nil {
				return fmt.Errorf("pack remove failed: %w", err)
			}

			if saveErr := cfg.Save(); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save config: %v\n", saveErr)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"pack_id":           result.PackID,
					"behaviors_removed": result.BehaviorsRemoved,
					"message":           fmt.Sprintf("Removed %s: %d behaviors marked as forgotten", result.PackID, result.BehaviorsRemoved),
				})
			}

			fmt.Printf("Removed %s\n", result.PackID)
			fmt.Printf("  Behaviors marked as forgotten: %d\n", result.BehaviorsRemoved)
			return nil
		},
	}

	return cmd
}

func newPackAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <behavior-id>",
		Short: "Add a behavior to a pack",
		Long: `Promote an existing behavior into a pack by stamping its provenance.

The behavior must exist and not be forgotten. If it already belongs to a
different pack, use --force to reassign it.

Examples:
  floop pack add behavior-abc123 --to my-org/my-pack
  floop pack add behavior-abc123 --to my-org/my-pack --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			behaviorID := args[0]
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			packID, _ := cmd.Flags().GetString("to")
			force, _ := cmd.Flags().GetBool("force")

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			if err := pack.AddToPack(ctx, graphStore, behaviorID, packID, force); err != nil {
				return fmt.Errorf("pack add failed: %w", err)
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"behavior_id": behaviorID,
					"pack_id":     packID,
					"message":     fmt.Sprintf("Added %s to %s", behaviorID, packID),
				})
			}

			fmt.Printf("Added %s to %s\n", behaviorID, packID)
			return nil
		},
	}

	cmd.Flags().String("to", "", "Target pack ID (required)")
	cmd.Flags().Bool("force", false, "Force reassignment if behavior belongs to another pack")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

func newPackRemoveBehaviorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-behavior <behavior-id>",
		Short: "Remove a behavior from its pack",
		Long: `Remove a single behavior from its pack without affecting other pack members.

By default, the behavior is unassigned from the pack but remains active.
Use --forget to mark the behavior as forgotten instead.

Examples:
  floop pack remove-behavior behavior-abc123
  floop pack remove-behavior behavior-abc123 --forget`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			behaviorID := args[0]
			root, _ := cmd.Flags().GetString("root")
			jsonOut, _ := cmd.Flags().GetBool("json")
			forget, _ := cmd.Flags().GetBool("forget")

			mode := pack.RemoveModeUnassign
			if forget {
				mode = pack.RemoveModeForgotten
			}

			ctx := context.Background()
			graphStore, err := store.NewMultiGraphStore(root)
			if err != nil {
				return fmt.Errorf("failed to open store: %w", err)
			}
			defer graphStore.Close()

			if err := pack.RemoveFromPack(ctx, graphStore, behaviorID, mode); err != nil {
				return fmt.Errorf("pack remove-behavior failed: %w", err)
			}

			action := "unassigned from pack"
			if forget {
				action = "forgotten"
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"behavior_id": behaviorID,
					"action":      string(mode),
					"message":     fmt.Sprintf("Behavior %s %s", behaviorID, action),
				})
			}

			fmt.Printf("Behavior %s %s\n", behaviorID, action)
			return nil
		},
	}

	cmd.Flags().Bool("forget", false, "Mark the behavior as forgotten instead of just unassigning")

	return cmd
}
