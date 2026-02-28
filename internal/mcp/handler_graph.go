package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/ratelimit"
	"github.com/nvandessel/floop/internal/store"
	"github.com/nvandessel/floop/internal/visualization"
)

// handleFloopConnect implements the floop_connect tool.
func (s *Server) handleFloopConnect(ctx context.Context, req *sdk.CallToolRequest, args FloopConnectInput) (_ *sdk.CallToolResult, _ FloopConnectOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_connect", start, retErr, sanitizeToolParams("floop_connect", map[string]interface{}{
			"source": args.Source, "target": args.Target, "kind": args.Kind,
			"weight": args.Weight, "bidirectional": args.Bidirectional,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_connect"); err != nil {
		return nil, FloopConnectOutput{}, err
	}

	// Validate required fields
	if args.Source == "" {
		return nil, FloopConnectOutput{}, fmt.Errorf("'source' parameter is required")
	}
	if args.Target == "" {
		return nil, FloopConnectOutput{}, fmt.Errorf("'target' parameter is required")
	}
	if args.Kind == "" {
		return nil, FloopConnectOutput{}, fmt.Errorf("'kind' parameter is required")
	}

	// Validate kind
	edgeKind := store.EdgeKind(args.Kind)
	if !store.ValidUserEdgeKinds[edgeKind] {
		return nil, FloopConnectOutput{}, fmt.Errorf("invalid edge kind: %s (must be one of: requires, overrides, conflicts, similar-to, learned-from)", args.Kind)
	}

	// Default weight
	weight := args.Weight
	if weight == 0 {
		weight = 0.8
	}
	if weight <= 0 || weight > 1.0 {
		return nil, FloopConnectOutput{}, fmt.Errorf("weight must be in (0.0, 1.0], got %f", weight)
	}

	// No self-edges
	if args.Source == args.Target {
		return nil, FloopConnectOutput{}, fmt.Errorf("self-edges are not allowed: source and target are both %s", args.Source)
	}

	// Validate source exists
	sourceNode, err := s.store.GetNode(ctx, args.Source)
	if err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to check source node: %w", err)
	}
	if sourceNode == nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("source node not found: %s", args.Source)
	}

	// Validate target exists
	targetNode, err := s.store.GetNode(ctx, args.Target)
	if err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to check target node: %w", err)
	}
	if targetNode == nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("target node not found: %s", args.Target)
	}

	// Check for duplicate edge
	existing, err := s.store.GetEdges(ctx, args.Source, store.DirectionOutbound, edgeKind)
	if err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to check existing edges: %w", err)
	}
	for _, e := range existing {
		if e.Target == args.Target {
			fmt.Fprintf(os.Stderr, "warning: edge %s -[%s]-> %s already exists (weight: %.2f)\n", args.Source, args.Kind, args.Target, e.Weight)
		}
	}

	// Create edge
	now := time.Now()
	edge := store.Edge{
		Source:    args.Source,
		Target:    args.Target,
		Kind:      edgeKind,
		Weight:    weight,
		CreatedAt: now,
	}

	if err := s.store.AddEdge(ctx, edge); err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to add edge: %w", err)
	}

	// Create reverse edge if bidirectional
	if args.Bidirectional {
		reverseEdge := store.Edge{
			Source:    args.Target,
			Target:    args.Source,
			Kind:      edgeKind,
			Weight:    weight,
			CreatedAt: now,
		}
		if err := s.store.AddEdge(ctx, reverseEdge); err != nil {
			return nil, FloopConnectOutput{}, fmt.Errorf("failed to add reverse edge: %w", err)
		}
	}

	// Sync store
	if err := s.store.Sync(ctx); err != nil {
		return nil, FloopConnectOutput{}, fmt.Errorf("failed to sync store: %w", err)
	}

	// Debounced PageRank refresh after connect
	s.debouncedRefreshPageRank()

	message := fmt.Sprintf("Edge created: %s -[%s (%.2f)]-> %s", args.Source, args.Kind, weight, args.Target)
	if args.Bidirectional {
		message += " (bidirectional)"
	}

	return nil, FloopConnectOutput{
		Source:        args.Source,
		Target:        args.Target,
		Kind:          args.Kind,
		Weight:        weight,
		Bidirectional: args.Bidirectional,
		Message:       message,
	}, nil
}

// handleFloopValidate implements the floop_validate tool.
func (s *Server) handleFloopValidate(ctx context.Context, req *sdk.CallToolRequest, args FloopValidateInput) (_ *sdk.CallToolResult, _ FloopValidateOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_validate", start, retErr, nil, "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_validate"); err != nil {
		return nil, FloopValidateOutput{}, err
	}

	// Check if the store supports validation (MultiGraphStore or SQLiteGraphStore)
	type graphValidator interface {
		ValidateBehaviorGraph(ctx context.Context) ([]store.ValidationError, error)
	}

	validator, ok := s.store.(graphValidator)
	if !ok {
		return nil, FloopValidateOutput{}, fmt.Errorf("store does not support graph validation")
	}

	// Perform validation
	validationErrors, err := validator.ValidateBehaviorGraph(ctx)
	if err != nil {
		return nil, FloopValidateOutput{}, fmt.Errorf("validation failed: %w", err)
	}

	// Convert to output format
	outputErrors := make([]ValidationErrorOutput, len(validationErrors))
	for i, ve := range validationErrors {
		outputErrors[i] = ValidationErrorOutput{
			BehaviorID: ve.BehaviorID,
			Field:      ve.Field,
			RefID:      ve.RefID,
			Issue:      ve.Issue,
		}
	}

	// Build message
	var message string
	if len(validationErrors) == 0 {
		message = "Behavior graph is valid - no issues found"
	} else {
		// Categorize errors
		var dangling, cycles, selfRefs int
		for _, ve := range validationErrors {
			switch ve.Issue {
			case "dangling":
				dangling++
			case "cycle":
				cycles++
			case "self-reference":
				selfRefs++
			}
		}

		parts := []string{}
		if dangling > 0 {
			parts = append(parts, fmt.Sprintf("%d dangling reference(s)", dangling))
		}
		if cycles > 0 {
			parts = append(parts, fmt.Sprintf("%d cycle(s)", cycles))
		}
		if selfRefs > 0 {
			parts = append(parts, fmt.Sprintf("%d self-reference(s)", selfRefs))
		}
		message = fmt.Sprintf("Found %d issue(s): %s", len(validationErrors), strings.Join(parts, ", "))
	}

	return nil, FloopValidateOutput{
		Valid:      len(validationErrors) == 0,
		ErrorCount: len(validationErrors),
		Errors:     outputErrors,
		Message:    message,
	}, nil
}

// handleFloopGraph implements the floop_graph tool.
func (s *Server) handleFloopGraph(ctx context.Context, req *sdk.CallToolRequest, args FloopGraphInput) (_ *sdk.CallToolResult, _ FloopGraphOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_graph", start, retErr, sanitizeToolParams("floop_graph", map[string]interface{}{
			"format": args.Format,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_graph"); err != nil {
		return nil, FloopGraphOutput{}, err
	}

	format := args.Format
	if format == "" {
		format = "json"
	}

	switch visualization.Format(format) {
	case visualization.FormatDOT:
		dot, err := visualization.RenderDOT(ctx, s.store)
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("render DOT: %w", err)
		}
		// Count nodes for output metadata
		nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("query nodes: %w", err)
		}
		return nil, FloopGraphOutput{
			Format:    "dot",
			Graph:     dot,
			NodeCount: len(nodes),
		}, nil

	case visualization.FormatJSON:
		result, err := visualization.RenderJSON(ctx, s.store)
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("render JSON: %w", err)
		}
		nodeCount, _ := result["node_count"].(int)
		edgeCount, _ := result["edge_count"].(int)
		return nil, FloopGraphOutput{
			Format:    "json",
			Graph:     result,
			NodeCount: nodeCount,
			EdgeCount: edgeCount,
		}, nil

	case visualization.FormatHTML:
		s.pageRankMu.RLock()
		pageRank := s.pageRankCache
		s.pageRankMu.RUnlock()

		enrichment := &visualization.EnrichmentData{PageRank: pageRank}
		htmlBytes, err := visualization.RenderHTML(ctx, s.store, enrichment)
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("render HTML: %w", err)
		}

		nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("query nodes: %w", err)
		}
		edges, err := visualization.CollectEdges(ctx, s.store, nodes)
		if err != nil {
			return nil, FloopGraphOutput{}, fmt.Errorf("collect edges: %w", err)
		}

		return nil, FloopGraphOutput{
			Format:    "html",
			Graph:     string(htmlBytes),
			NodeCount: len(nodes),
			EdgeCount: len(edges),
		}, nil

	default:
		return nil, FloopGraphOutput{}, fmt.Errorf("unsupported format %q (use 'dot', 'json', or 'html')", format)
	}
}
