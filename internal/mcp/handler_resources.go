package mcp

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nvandessel/floop/internal/activation"
	"github.com/nvandessel/floop/internal/assembly"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/tiering"
)

// handleBehaviorsResource returns active behaviors formatted for context injection.
// Uses tiered injection to optimize token usage while preserving critical behaviors.
func (s *Server) handleBehaviorsResource(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	// Build context for activation (default task: development)
	ctxBuilder := activation.NewContextBuilder()
	ctxBuilder.WithRepoRoot(s.root)
	ctxBuilder.WithTask("development")
	actCtx := ctxBuilder.Build()

	// Load all behaviors from store
	nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("failed to query behaviors: %w", err)
	}

	// Convert nodes to behaviors
	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		behavior := models.NodeToBehavior(node)
		behaviors = append(behaviors, behavior)
	}

	// Evaluate which behaviors are active
	evaluator := activation.NewEvaluator()
	matches := evaluator.Evaluate(actCtx, behaviors)

	// Resolve conflicts and get final active set
	resolver := activation.NewResolver()
	result := resolver.Resolve(matches)

	if len(result.Active) == 0 {
		return &sdk.ReadResourceResult{
			Contents: []*sdk.ResourceContents{
				{
					URI:      "floop://behaviors/active",
					MIMEType: "text/markdown",
					Text:     "# Learned Behaviors\n\nNo memories for current context yet. Learn from corrections using `floop_learn`.\n",
				},
			},
		}, nil
	}

	// Create tiered injection plan via bridge → ActivationTierMapper
	results, behaviorMap := tiering.BehaviorsToResults(result.Active)
	mapper := tiering.NewActivationTierMapper(tiering.DefaultActivationTierConfig())
	plan := mapper.MapResults(results, behaviorMap, s.floopConfig.TokenBudget.Default)

	// Compile tiered prompt
	compiler := assembly.NewCompiler()
	tieredPrompt := compiler.CompileTiered(plan)

	// Build final output with header
	var sb strings.Builder
	sb.WriteString("# Learned Behaviors\n\n")
	sb.WriteString("Suggestions based on patterns from previous sessions.\n")
	sb.WriteString("Apply these where relevant; override when context requires it.\n\n")

	// Add the compiled tiered content
	if tieredPrompt.Text != "" {
		sb.WriteString(tieredPrompt.Text)
		sb.WriteString("\n\n")
	}

	// Add footer with stats
	sb.WriteString(fmt.Sprintf("---\n*%d memories active", plan.IncludedCount()))
	if len(plan.OmittedBehaviors) > 0 {
		sb.WriteString(fmt.Sprintf(" (%d summarized, %d available via floop://behaviors/expand/{id})",
			len(plan.SummarizedBehaviors), len(plan.OmittedBehaviors)))
	}
	sb.WriteString("*\n")

	return &sdk.ReadResourceResult{
		Contents: []*sdk.ResourceContents{
			{
				URI:      "floop://behaviors/active",
				MIMEType: "text/markdown",
				Text:     sb.String(),
			},
		},
	}, nil
}

// handleBehaviorExpandResource returns full details for a specific behavior.
// This allows agents to retrieve complete content for summarized or omitted behaviors.
func (s *Server) handleBehaviorExpandResource(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	// Extract behavior ID from URI
	// URI format: floop://behaviors/expand/{id}
	uri := req.Params.URI
	prefix := "floop://behaviors/expand/"
	if !strings.HasPrefix(uri, prefix) {
		return nil, fmt.Errorf("invalid URI format: %s", uri)
	}
	behaviorID := strings.TrimPrefix(uri, prefix)
	if behaviorID == "" {
		return nil, fmt.Errorf("behavior ID is required")
	}

	// Query for the specific behavior
	nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{
		"kind": "behavior",
		"id":   behaviorID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query behavior: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("behavior not found: %s", behaviorID)
	}

	behavior := models.NodeToBehavior(nodes[0])

	// Format full behavior details
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Behavior: %s\n\n", behavior.Name))
	sb.WriteString(fmt.Sprintf("**ID:** %s\n", behavior.ID))
	sb.WriteString(fmt.Sprintf("**Kind:** %s\n", behavior.Kind))
	sb.WriteString(fmt.Sprintf("**Confidence:** %.2f\n", behavior.Confidence))
	sb.WriteString(fmt.Sprintf("**Priority:** %d\n\n", behavior.Priority))

	sb.WriteString("## Content\n\n")
	sb.WriteString(behavior.Content.Canonical)
	sb.WriteString("\n")

	if len(behavior.When) > 0 {
		sb.WriteString("\n## Activation Context\n\n")
		for k, v := range behavior.When {
			sb.WriteString(fmt.Sprintf("- **%s:** %v\n", k, v))
		}
	}

	if behavior.Stats.TimesActivated > 0 {
		sb.WriteString("\n## Statistics\n\n")
		sb.WriteString(fmt.Sprintf("- Times Activated: %d\n", behavior.Stats.TimesActivated))
		sb.WriteString(fmt.Sprintf("- Times Followed: %d\n", behavior.Stats.TimesFollowed))
		if behavior.Stats.TimesConfirmed > 0 {
			sb.WriteString(fmt.Sprintf("- Times Confirmed: %d\n", behavior.Stats.TimesConfirmed))
		}
		if behavior.Stats.TimesOverridden > 0 {
			sb.WriteString(fmt.Sprintf("- Times Overridden: %d\n", behavior.Stats.TimesOverridden))
		}
	}

	return &sdk.ReadResourceResult{
		Contents: []*sdk.ResourceContents{
			{
				URI:      uri,
				MIMEType: "text/markdown",
				Text:     sb.String(),
			},
		},
	}, nil
}
