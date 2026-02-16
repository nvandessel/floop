// Package visualization renders behavior graphs in various output formats.
package visualization

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/store"
)

// Format specifies the output format for graph rendering.
type Format string

const (
	FormatDOT  Format = "dot"
	FormatJSON Format = "json"
	FormatHTML Format = "html"
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

// EnrichmentData provides optional data to augment base graph JSON.
type EnrichmentData struct {
	// PageRank maps behavior IDs to their PageRank scores (0.0-1.0).
	PageRank map[string]float64
}

// RenderEnrichedJSON produces a JSON graph with optional enrichment data (e.g. PageRank scores)
// and additional node fields (canonical content) for the HTML visualization.
// If enrichment is nil, it still adds content fields but skips PageRank.
func RenderEnrichedJSON(ctx context.Context, gs store.GraphStore, enrichment *EnrichmentData) (map[string]interface{}, error) {
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

		// Extract canonical content for detail panel
		canonical := ""
		if content, ok := node.Content["content"].(map[string]interface{}); ok {
			if c, ok := content["canonical"].(string); ok {
				canonical = c
			}
		}

		scope := "local"
		if s, ok := node.Metadata["scope"].(string); ok {
			scope = s
		}

		entry := map[string]interface{}{
			"id":         node.ID,
			"name":       name,
			"kind":       kind,
			"confidence": confidence,
			"canonical":  canonical,
			"scope":      scope,
		}

		// Add PageRank if available
		if enrichment != nil && enrichment.PageRank != nil {
			if pr, exists := enrichment.PageRank[node.ID]; exists {
				entry["pagerank"] = pr
			}
		}

		jsonNodes = append(jsonNodes, entry)
	}

	// Build node scope map for edge scope derivation (reuse already-extracted scope)
	nodeScope := make(map[string]string, len(jsonNodes))
	for _, entry := range jsonNodes {
		nodeScope[entry["id"].(string)] = entry["scope"].(string)
	}

	// Collect edges
	edges, err := CollectEdges(ctx, gs, nodes)
	if err != nil {
		return nil, err
	}

	jsonEdges := make([]map[string]interface{}, 0, len(edges))
	for _, edge := range edges {
		jsonEdges = append(jsonEdges, map[string]interface{}{
			"source": edge.Source,
			"target": edge.Target,
			"kind":   edge.Kind,
			"weight": edge.Weight,
			"scope":  deriveEdgeScope(nodeScope[edge.Source], nodeScope[edge.Target]),
		})
	}

	return map[string]interface{}{
		"nodes":      jsonNodes,
		"edges":      jsonEdges,
		"node_count": len(jsonNodes),
		"edge_count": len(jsonEdges),
	}, nil
}

// htmlTemplateData holds data passed to the HTML template.
// ForceGraphSrc is a data: URI for loading the library via <script src>.
// GraphJSON is pre-sanitized JSON (via json.HTMLEscape) safe for inline <script>.
type htmlTemplateData struct {
	ForceGraphSrc template.URL
	GraphJSON     template.JS
}

// RenderHTML produces a self-contained HTML file with an interactive force-directed graph.
func RenderHTML(ctx context.Context, gs store.GraphStore, enrichment *EnrichmentData) ([]byte, error) {
	// Get enriched graph data
	graphData, err := RenderEnrichedJSON(ctx, gs, enrichment)
	if err != nil {
		return nil, fmt.Errorf("render enriched JSON: %w", err)
	}

	// Marshal graph data to JSON for embedding
	graphJSON, err := json.Marshal(graphData)
	if err != nil {
		return nil, fmt.Errorf("marshal graph data: %w", err)
	}

	// Load force-graph library
	jsBytes, err := assets.ReadFile("assets/force-graph.min.js")
	if err != nil {
		return nil, fmt.Errorf("read force-graph.min.js: %w", err)
	}

	// Load and parse HTML template
	tmplBytes, err := templates.ReadFile("templates/graph.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("read HTML template: %w", err)
	}

	tmpl, err := template.New("graph").Parse(string(tmplBytes))
	if err != nil {
		return nil, fmt.Errorf("parse HTML template: %w", err)
	}

	// Escape graph JSON for safe inline <script> embedding.
	// json.HTMLEscape converts <, >, & to unicode escapes (\u003c etc.),
	// preventing </script> breakout from user-controlled behavior content.
	var escaped bytes.Buffer
	json.HTMLEscape(&escaped, graphJSON)

	var buf bytes.Buffer
	data := htmlTemplateData{
		// ForceGraphSrc: trusted embedded asset, base64-encoded — no user input.
		ForceGraphSrc: template.URL("data:text/javascript;base64," + base64.StdEncoding.EncodeToString(jsBytes)), // #nosec G203
		// GraphJSON: pre-sanitized via json.HTMLEscape — </script> breakout impossible.
		GraphJSON: template.JS(escaped.String()), // #nosec G203
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute HTML template: %w", err)
	}

	return buf.Bytes(), nil
}

// CollectEdges gathers deduplicated outbound edges for a set of nodes.
func CollectEdges(ctx context.Context, gs store.GraphStore, nodes []store.Node) ([]store.Edge, error) {
	seen := make(map[string]bool)
	var result []store.Edge
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
			result = append(result, edge)
		}
	}
	return result, nil
}

// deriveEdgeScope determines an edge's scope from its endpoint node scopes.
// If both are the same scope, the edge gets that scope; otherwise "both".
func deriveEdgeScope(sourceScope, targetScope string) string {
	if sourceScope == targetScope {
		return sourceScope
	}
	return "both"
}

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
