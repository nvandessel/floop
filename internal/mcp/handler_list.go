package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/ratelimit"
)

// handleFloopList implements the floop_list tool.
func (s *Server) handleFloopList(ctx context.Context, req *sdk.CallToolRequest, args FloopListInput) (_ *sdk.CallToolResult, _ FloopListOutput, retErr error) {
	start := time.Now()
	defer func() {
		s.auditTool("floop_list", start, retErr, sanitizeToolParams("floop_list", map[string]interface{}{
			"corrections": args.Corrections, "tag": args.Tag,
		}), "local")
	}()

	if err := ratelimit.CheckLimit(s.toolLimiters, "floop_list"); err != nil {
		return nil, FloopListOutput{}, err
	}

	if args.Corrections {
		// List corrections from corrections.jsonl file (not graph store)
		correctionsPath := filepath.Join(s.root, ".floop", "corrections.jsonl")
		file, err := os.Open(correctionsPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, FloopListOutput{
					Corrections: []CorrectionListItem{},
					Count:       0,
				}, nil
			}
			return nil, FloopListOutput{}, fmt.Errorf("failed to open corrections file: %w", err)
		}
		defer file.Close()

		var corrections []CorrectionListItem
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var c models.Correction
			if err := json.Unmarshal([]byte(line), &c); err != nil {
				continue // Skip malformed lines
			}
			corrections = append(corrections, CorrectionListItem{
				ID:              c.ID,
				Timestamp:       c.Timestamp,
				AgentAction:     c.AgentAction,
				CorrectedAction: c.CorrectedAction,
				Processed:       c.Processed,
			})
		}

		return nil, FloopListOutput{
			Corrections: corrections,
			Count:       len(corrections),
		}, nil
	}

	// List behaviors
	nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, FloopListOutput{}, fmt.Errorf("failed to query behaviors: %w", err)
	}

	behaviors := make([]BehaviorListItem, 0, len(nodes))
	for _, node := range nodes {
		behavior := models.NodeToBehavior(node)

		// Filter by tag if specified
		if args.Tag != "" {
			found := false
			for _, t := range behavior.Content.Tags {
				if t == args.Tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Determine source
		source := "unknown"
		if behavior.Provenance.SourceType != "" {
			source = string(behavior.Provenance.SourceType)
		}

		behaviors = append(behaviors, BehaviorListItem{
			ID:         behavior.ID,
			Name:       behavior.Name,
			Kind:       string(behavior.Kind),
			Confidence: behavior.Confidence,
			Tags:       behavior.Content.Tags,
			Source:     source,
			CreatedAt:  behavior.Provenance.CreatedAt,
		})
	}

	return nil, FloopListOutput{
		Behaviors: behaviors,
		Count:     len(behaviors),
	}, nil
}
