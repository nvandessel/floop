package pack

import (
	"context"
	"fmt"
	"time"

	"github.com/nvandessel/floop/internal/backup"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// CreateFilter controls which behaviors are included in a pack.
type CreateFilter struct {
	Tags     []string // include behaviors matching any tag (empty = all)
	Scope    string   // "global", "local", or "" (all)
	Kinds    []string // behavior kinds to include (empty = all)
	FromPack string   // only include behaviors where provenance.package matches (empty = all)
}

// CreateOptions configures pack creation.
type CreateOptions struct {
	FloopVersion string
}

// CreateResult reports what was created.
type CreateResult struct {
	Path          string
	BehaviorCount int
	EdgeCount     int
}

// Create exports filtered behaviors and their connecting edges into a pack file.
func Create(ctx context.Context, s store.GraphStore, filter CreateFilter, manifest PackManifest, outputPath string, opts CreateOptions) (*CreateResult, error) {
	if err := ValidatePackID(string(manifest.ID)); err != nil {
		return nil, err
	}

	// 1. Query all nodes
	nodes, err := s.QueryNodes(ctx, map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("querying nodes: %w", err)
	}

	// 2. Convert and filter
	filteredIDs := make(map[string]bool)
	var filteredNodes []backup.BackupNode
	for _, node := range nodes {
		if !matchesFilter(node, filter) {
			continue
		}
		filteredIDs[node.ID] = true
		filteredNodes = append(filteredNodes, backup.BackupNode{Node: node})
	}

	// 3. Collect only edges where BOTH endpoints are in the filtered set
	edgeSet := make(map[string]store.Edge)
	for _, node := range nodes {
		if !filteredIDs[node.ID] {
			continue
		}
		edges, err := s.GetEdges(ctx, node.ID, store.DirectionOutbound, "")
		if err != nil {
			return nil, fmt.Errorf("getting edges for %s: %w", node.ID, err)
		}
		for _, e := range edges {
			if filteredIDs[e.Source] && filteredIDs[e.Target] {
				key := fmt.Sprintf("%s:%s:%s", e.Source, e.Target, e.Kind)
				edgeSet[key] = e
			}
		}
	}

	edges := make([]store.Edge, 0, len(edgeSet))
	for _, e := range edgeSet {
		edges = append(edges, e)
	}

	// 4. Build BackupFormat
	bf := &backup.BackupFormat{
		Version:   backup.FormatV2,
		CreatedAt: time.Now(),
		Nodes:     filteredNodes,
		Edges:     edges,
	}

	// 5. Write pack file
	writeOpts := &backup.WriteOptions{
		FloopVersion: opts.FloopVersion,
	}
	if err := WritePackFile(outputPath, bf, manifest, writeOpts); err != nil {
		return nil, fmt.Errorf("writing pack file: %w", err)
	}

	return &CreateResult{
		Path:          outputPath,
		BehaviorCount: len(filteredNodes),
		EdgeCount:     len(edges),
	}, nil
}

// matchesFilter checks if a node passes the given filter criteria.
func matchesFilter(node store.Node, filter CreateFilter) bool {
	b := models.NodeToBehavior(node)

	// Filter by pack membership
	if filter.FromPack != "" {
		if models.ExtractPackageName(node.Metadata) != filter.FromPack {
			return false
		}
	}

	// Filter by scope
	if filter.Scope != "" {
		nodeScope := extractScope(node)
		if nodeScope != filter.Scope {
			return false
		}
	}

	// Filter by kind
	if len(filter.Kinds) > 0 {
		kindStr := string(b.Kind)
		if !containsString(filter.Kinds, kindStr) {
			return false
		}
	}

	// Filter by tags
	if len(filter.Tags) > 0 {
		if !hasAnyTag(b.Content.Tags, filter.Tags) {
			return false
		}
	}

	return true
}

// extractScope determines the scope of a node from its metadata.
func extractScope(node store.Node) string {
	if node.Metadata == nil {
		return ""
	}
	prov, ok := node.Metadata["provenance"].(map[string]interface{})
	if !ok {
		return ""
	}
	scope, _ := prov["scope"].(string)
	return scope
}

// hasAnyTag returns true if any of the wanted tags appear in the node's tags.
func hasAnyTag(nodeTags, wantedTags []string) bool {
	tagSet := make(map[string]bool, len(nodeTags))
	for _, t := range nodeTags {
		tagSet[t] = true
	}
	for _, w := range wantedTags {
		if tagSet[w] {
			return true
		}
	}
	return false
}

// containsString returns true if slice contains the target string.
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
