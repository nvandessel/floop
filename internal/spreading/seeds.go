package spreading

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/activation"
	"github.com/nvandessel/feedback-loop/internal/learning"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/store"
)

// SeedSelector determines which behaviors should be activation seeds
// based on the current context.
type SeedSelector struct {
	store     store.GraphStore
	evaluator *activation.Evaluator
}

// NewSeedSelector creates a new seed selector.
func NewSeedSelector(s store.GraphStore) *SeedSelector {
	return &SeedSelector{
		store:     s,
		evaluator: activation.NewEvaluator(),
	}
}

// SelectSeeds determines seed nodes from the current context.
// It uses the existing activation.Evaluator to find behaviors matching
// the context, then converts matches to seeds with activation proportional
// to match specificity.
//
// Seed activation scaling:
//   - Specificity 1 (one condition matched) -> activation 0.4
//   - Specificity 2 (two conditions matched) -> activation 0.6
//   - Specificity 3+ (three+ conditions matched) -> activation 0.8-1.0
//   - No 'when' conditions (always-active) -> activation 0.3 (lower, less specific)
//
// Returns seeds sorted by activation descending.
func (s *SeedSelector) SelectSeeds(ctx context.Context, actCtx models.ContextSnapshot) ([]Seed, error) {
	// Step 1: Query all behaviors from the store.
	nodes, err := s.store.QueryNodes(ctx, map[string]interface{}{"kind": "behavior"})
	if err != nil {
		return nil, fmt.Errorf("querying behavior nodes: %w", err)
	}

	if len(nodes) == 0 {
		return []Seed{}, nil
	}

	// Step 2: Convert nodes to Behavior models.
	behaviors := make([]models.Behavior, 0, len(nodes))
	for _, node := range nodes {
		behaviors = append(behaviors, learning.NodeToBehavior(node))
	}

	// Step 3: Evaluate which behaviors match the context.
	matches := s.evaluator.Evaluate(actCtx, behaviors)
	if len(matches) == 0 {
		return []Seed{}, nil
	}

	// Step 4: Convert ActivationResult to Seed.
	seeds := make([]Seed, 0, len(matches))
	for _, match := range matches {
		seeds = append(seeds, Seed{
			BehaviorID: match.Behavior.ID,
			Activation: SpecificityToActivation(match.Specificity),
			Source:     BuildSourceLabel(match.MatchedConditions),
		})
	}

	// Step 5: Sort seeds by activation descending.
	sort.Slice(seeds, func(i, j int) bool {
		return seeds[i].Activation > seeds[j].Activation
	})

	return seeds, nil
}

// SpecificityToActivation maps specificity (number of matched conditions)
// to a seed activation level in [0, 1].
func SpecificityToActivation(specificity int) float64 {
	switch {
	case specificity == 0:
		return 0.3
	case specificity == 1:
		return 0.4
	case specificity == 2:
		return 0.6
	case specificity == 3:
		return 0.8
	default:
		// 4+ conditions: scale linearly from 0.8 toward 1.0,
		// clamped at 1.0.
		v := 0.8 + float64(specificity-3)*0.1
		if v > 1.0 {
			return 1.0
		}
		return v
	}
}

// BuildSourceLabel formats matched conditions into a source label string.
// Format: "context:" + comma-joined "key=value" pairs.
// For always-active behaviors (no matched conditions), returns "context:always".
func BuildSourceLabel(matchedConditions map[string]interface{}) string {
	if len(matchedConditions) == 0 {
		return "context:always"
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(matchedConditions))
	for k := range matchedConditions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, matchedConditions[k]))
	}

	return "context:" + strings.Join(parts, ",")
}
