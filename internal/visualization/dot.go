// Package visualization renders behavior graphs in various output formats.
package visualization

import (
	"context"
	"fmt"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// Format specifies the output format for graph rendering.
type Format string

const (
	FormatDOT  Format = "dot"
	FormatJSON Format = "json"
)

// nodeColors maps behavior kinds to DOT colors.
var nodeColors = map[string]string{
	"directive":  "steelblue",
	"constraint": "tomato",
	"procedure":  "mediumseagreen",
	"preference": "goldenrod",
}

// edgeStyles maps edge kinds to DOT styles.
var edgeStyles = map[string]string{
	"requires":     "solid",
	"overrides":    "bold",
	"conflicts":    "dashed",
	"similar-to":   "dotted",
	"learned-from": "tapered",
}

// RenderDOT produces a Graphviz DOT representation of the behavior graph.
func RenderDOT(ctx context.Context, gs store.GraphStore) (string, error) {
	// Get all behavior nodes
	nodes, err := gs.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return "", fmt.Errorf("query nodes: %w", err)
	}

	var b strings.Builder
	b.WriteString("digraph floop {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=filled, fontname=\"Helvetica\"];\n")
	b.WriteString("  edge [fontname=\"Helvetica\", fontsize=10];\n\n")

	// Render nodes
	for _, node := range nodes {
		name := ""
		if n, ok := node.Content["name"].(string); ok {
			name = n
		}
		kind := ""
		if k, ok := node.Content["kind"].(string); ok {
			kind = k
		}
		confidence := 0.6
		if meta, ok := node.Metadata["confidence"].(float64); ok {
			confidence = meta
		}

		color := nodeColors[kind]
		if color == "" {
			color = "lightgray"
		}

		label := truncate(name, 40)
		b.WriteString(fmt.Sprintf("  %q [label=%q, fillcolor=%q, tooltip=\"confidence=%.2f\"];\n",
			node.ID, label, color, confidence))
	}
	b.WriteString("\n")

	// Render edges — collect from all nodes
	seen := make(map[string]bool) // dedup "src|tgt|kind"
	for _, node := range nodes {
		edges, err := gs.GetEdges(ctx, node.ID, store.DirectionOutbound, "")
		if err != nil {
			return "", fmt.Errorf("get edges for node %s: %w", node.ID, err)
		}
		for _, edge := range edges {
			key := edge.Source + "|" + edge.Target + "|" + edge.Kind
			if seen[key] {
				continue
			}
			seen[key] = true

			style := edgeStyles[edge.Kind]
			if style == "" {
				style = "solid"
			}

			b.WriteString(fmt.Sprintf("  %q -> %q [label=%q, style=%s, weight=\"%.1f\"];\n",
				edge.Source, edge.Target, edge.Kind, style, edge.Weight))
		}
	}

	b.WriteString("}\n")
	return b.String(), nil
}

// RenderJSON produces a JSON graph representation with nodes and edges arrays.
func RenderJSON(ctx context.Context, gs store.GraphStore) (map[string]interface{}, error) {
	nodes, err := gs.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}

	jsonNodes := make([]map[string]interface{}, 0, len(nodes))
	for _, node := range nodes {
		name := ""
		if n, ok := node.Content["name"].(string); ok {
			name = n
		}
		kind := ""
		if k, ok := node.Content["kind"].(string); ok {
			kind = k
		}
		confidence := 0.6
		if meta, ok := node.Metadata["confidence"].(float64); ok {
			confidence = meta
		}

		jsonNodes = append(jsonNodes, map[string]interface{}{
			"id":         node.ID,
			"name":       name,
			"kind":       kind,
			"confidence": confidence,
		})
	}

	// Collect edges
	seen := make(map[string]bool)
	var jsonEdges []map[string]interface{}
	for _, node := range nodes {
		edges, err := gs.GetEdges(ctx, node.ID, store.DirectionOutbound, "")
		if err != nil {
			return nil, fmt.Errorf("get edges for node %s: %w", node.ID, err)
		}
		for _, edge := range edges {
			key := edge.Source + "|" + edge.Target + "|" + edge.Kind
			if seen[key] {
				continue
			}
			seen[key] = true
			jsonEdges = append(jsonEdges, map[string]interface{}{
				"source": edge.Source,
				"target": edge.Target,
				"kind":   edge.Kind,
				"weight": edge.Weight,
			})
		}
	}

	return map[string]interface{}{
		"nodes":      jsonNodes,
		"edges":      jsonEdges,
		"node_count": len(jsonNodes),
		"edge_count": len(jsonEdges),
	}, nil
}

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
