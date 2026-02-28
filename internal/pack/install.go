package pack

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nvandessel/floop/internal/config"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// InstallOptions configures pack installation.
type InstallOptions struct {
	DeriveEdges bool   // Automatically derive edges between pack behaviors and existing behaviors
	Source      string // Canonical source string to record (e.g., "gh:owner/repo@v1.0.0")
}

// InstallResult reports what was installed.
type InstallResult struct {
	PackID       string
	Version      string
	Added        []string // IDs of newly added behaviors
	Updated      []string // IDs of upgraded behaviors
	Skipped      []string // IDs of skipped (up-to-date or forgotten)
	EdgesAdded   int
	EdgesSkipped int
	DerivedEdges int // Edges automatically derived between new and existing behaviors
}

// Install loads a pack file and installs its behaviors into the store.
// Follows the seeder pattern: skip forgotten, version-gate updates, stamp provenance.
func Install(ctx context.Context, s store.GraphStore, filePath string, cfg *config.FloopConfig, opts InstallOptions) (*InstallResult, error) {
	// 1. Read pack file
	data, manifest, err := ReadPackFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading pack file: %w", err)
	}

	result := &InstallResult{
		PackID:  string(manifest.ID),
		Version: manifest.Version,
	}

	// 2. Install nodes
	for _, bn := range data.Nodes {
		node := bn.Node

		// Stamp provenance on each node
		stampProvenance(&node, manifest)

		existing, err := s.GetNode(ctx, node.ID)
		if err != nil {
			return nil, fmt.Errorf("checking node %s: %w", node.ID, err)
		}

		if existing == nil {
			// New node -- add it
			if _, err := s.AddNode(ctx, node); err != nil {
				return nil, fmt.Errorf("adding node %s: %w", node.ID, err)
			}
			result.Added = append(result.Added, node.ID)
			continue
		}

		// Respect user curation: don't re-add forgotten behaviors
		if existing.Kind == string(models.BehaviorKindForgotten) {
			result.Skipped = append(result.Skipped, node.ID)
			continue
		}

		// Check version for upgrade
		existingVersion := models.ExtractPackageVersion(existing.Metadata)
		if existingVersion == manifest.Version {
			// Already up-to-date
			result.Skipped = append(result.Skipped, node.ID)
			continue
		}

		// Version mismatch -- update content
		if err := s.UpdateNode(ctx, node); err != nil {
			return nil, fmt.Errorf("updating node %s: %w", node.ID, err)
		}
		result.Updated = append(result.Updated, node.ID)
	}

	// 3. Install edges
	for _, edge := range data.Edges {
		if err := s.AddEdge(ctx, edge); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to add edge %s -> %s (%s): %v\n",
				edge.Source, edge.Target, edge.Kind, err)
			result.EdgesSkipped++
			continue
		}
		result.EdgesAdded++
	}

	// 4. Sync store
	if err := s.Sync(ctx); err != nil {
		return nil, fmt.Errorf("syncing after install: %w", err)
	}

	// 4b. Derive edges between new/updated pack behaviors and existing behaviors
	if opts.DeriveEdges && (len(result.Added) > 0 || len(result.Updated) > 0) {
		newIDs := make([]string, 0, len(result.Added)+len(result.Updated))
		newIDs = append(newIDs, result.Added...)
		newIDs = append(newIDs, result.Updated...)
		intResult, intErr := IntegratePackBehaviors(ctx, s, newIDs)
		if intErr != nil {
			fmt.Fprintf(os.Stderr, "warning: edge derivation failed: %v\n", intErr)
		} else {
			result.DerivedEdges = intResult.EdgesCreated
		}
	}

	// 5. Record in config
	if cfg != nil {
		recordInstall(cfg, manifest, result, opts.Source)
	}

	return result, nil
}

// stampProvenance sets package and package_version in the node's provenance metadata.
func stampProvenance(node *store.Node, manifest *PackManifest) {
	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}

	prov, ok := node.Metadata["provenance"].(map[string]interface{})
	if !ok {
		prov = make(map[string]interface{})
	}

	prov["package"] = string(manifest.ID)
	prov["package_version"] = manifest.Version
	node.Metadata["provenance"] = prov
}

// recordInstall updates the config's installed packs list.
// source is the canonical source string (e.g., "gh:owner/repo@v1.0.0"); falls back to manifest.Source.
func recordInstall(cfg *config.FloopConfig, manifest *PackManifest, result *InstallResult, source string) {
	// Remove existing entry for this pack if present
	filtered := make([]config.InstalledPack, 0, len(cfg.Packs.Installed))
	for _, p := range cfg.Packs.Installed {
		if p.ID != string(manifest.ID) {
			filtered = append(filtered, p)
		}
	}

	// Resolve source: prefer explicit source, fall back to manifest
	recordedSource := source
	if recordedSource == "" {
		recordedSource = manifest.Source
	}

	// Add new entry
	filtered = append(filtered, config.InstalledPack{
		ID:            string(manifest.ID),
		Version:       manifest.Version,
		InstalledAt:   time.Now(),
		Source:        recordedSource,
		BehaviorCount: len(result.Added) + len(result.Updated) + len(result.Skipped),
		EdgeCount:     result.EdgesAdded,
	})

	cfg.Packs.Installed = filtered
}

// InstallFromSourceOptions configures remote pack installation.
type InstallFromSourceOptions struct {
	DeriveEdges bool
	AllAssets   bool // install all .fpack assets from a multi-asset GitHub release
}

// InstallFromSource resolves a source string, fetches remote packs if needed,
// and installs them. Returns one InstallResult per installed pack file.
//
// Supported source formats:
//   - Local path: ./pack.fpack, /abs/path.fpack
//   - HTTP URL: https://example.com/pack.fpack
//   - GitHub shorthand: gh:owner/repo, gh:owner/repo@v1.2.3
func InstallFromSource(ctx context.Context, s store.GraphStore, source string, cfg *config.FloopConfig, opts InstallFromSourceOptions) ([]*InstallResult, error) {
	resolved, err := ResolveSource(source)
	if err != nil {
		return nil, fmt.Errorf("resolving source: %w", err)
	}

	installOpts := InstallOptions{
		DeriveEdges: opts.DeriveEdges,
		Source:      resolved.Canonical,
	}

	switch resolved.Kind {
	case SourceLocal:
		result, err := Install(ctx, s, resolved.FilePath, cfg, installOpts)
		if err != nil {
			return nil, err
		}
		return []*InstallResult{result}, nil

	case SourceHTTP:
		cacheDir, err := DefaultCacheDir()
		if err != nil {
			return nil, fmt.Errorf("getting cache directory: %w", err)
		}
		cachePath := HTTPCachePath(cacheDir, resolved.URL)

		fetchResult, err := Fetch(ctx, resolved.URL, cachePath, FetchOptions{})
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", resolved.URL, err)
		}

		result, err := Install(ctx, s, fetchResult.LocalPath, cfg, installOpts)
		if err != nil {
			return nil, err
		}
		return []*InstallResult{result}, nil

	case SourceGitHub:
		gh := NewGitHubClient()

		release, err := gh.ResolveRelease(ctx, resolved.Owner, resolved.Repo, resolved.Version)
		if err != nil {
			return nil, err
		}

		packAssets := FindPackAssets(release)
		if len(packAssets) == 0 {
			assetNames := make([]string, len(release.Assets))
			for i, a := range release.Assets {
				assetNames[i] = a.Name
			}
			return nil, fmt.Errorf("no .fpack assets found in release %s; available assets: %s",
				release.TagName, strings.Join(assetNames, ", "))
		}

		if len(packAssets) > 1 && !opts.AllAssets {
			names := make([]string, len(packAssets))
			for i, a := range packAssets {
				names[i] = a.Name
			}
			return nil, fmt.Errorf("release %s contains multiple .fpack assets: %s; use --all-assets to install all",
				release.TagName, strings.Join(names, ", "))
		}

		cacheDir, err := DefaultCacheDir()
		if err != nil {
			return nil, fmt.Errorf("getting cache directory: %w", err)
		}

		version := ReleaseVersion(release)

		var results []*InstallResult
		for _, asset := range packAssets {
			cachePath := GitHubCachePath(cacheDir, resolved.Owner, resolved.Repo, version, asset.Name)
			downloadURL := AssetDownloadURL(asset)

			fetchResult, err := Fetch(ctx, downloadURL, cachePath, FetchOptions{})
			if err != nil {
				return nil, fmt.Errorf("fetching %s: %w", asset.Name, err)
			}

			result, err := Install(ctx, s, fetchResult.LocalPath, cfg, installOpts)
			if err != nil {
				return nil, fmt.Errorf("installing %s: %w", asset.Name, err)
			}
			results = append(results, result)
		}
		return results, nil

	default:
		return nil, fmt.Errorf("unsupported source kind: %s", resolved.Kind)
	}
}
