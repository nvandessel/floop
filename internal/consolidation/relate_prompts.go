package consolidation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/store"
)

// neighborSummary is a compact representation of an existing behavior for LLM prompts.
type neighborSummary struct {
	ID        string `json:"id"`
	Canonical string `json:"canonical"`
	Kind      string `json:"kind"`
}

// RelateMemoriesPrompt builds the message sequence for the LLM relationship-proposal call.
// It provides the classified memories and their nearest neighbors, asking the LLM to
// propose create, merge, or skip actions with edge types and merge strategies.
func RelateMemoriesPrompt(memories []ClassifiedMemory, neighbors map[int][]scoredNode) ([]llm.Message, error) {
	// Build memory summaries.
	type memorySummary struct {
		Index     int    `json:"index"`
		Canonical string `json:"canonical"`
		Kind      string `json:"kind"`
		Type      string `json:"type"`
	}
	memSummaries := make([]memorySummary, len(memories))
	for i, m := range memories {
		memSummaries[i] = memorySummary{
			Index:     i,
			Canonical: m.Content.Canonical,
			Kind:      string(m.Kind),
			Type:      string(m.MemoryType),
		}
	}

	// Build neighbor summaries per memory index.
	neighborMap := make(map[int][]neighborSummary)
	for idx, scoredNodes := range neighbors {
		for _, sn := range scoredNodes {
			kind, _ := sn.Node.Content["kind"].(string)
			canonical := ""
			if cm, ok := sn.Node.Content["content"].(map[string]interface{}); ok {
				canonical, _ = cm["canonical"].(string)
			}
			neighborMap[idx] = append(neighborMap[idx], neighborSummary{
				ID:        sn.Node.ID,
				Canonical: canonical,
				Kind:      kind,
			})
		}
	}

	memJSON, err := json.Marshal(memSummaries)
	if err != nil {
		return nil, fmt.Errorf("marshaling memories: %w", err)
	}
	neighborsJSON, err := json.Marshal(neighborMap)
	if err != nil {
		return nil, fmt.Errorf("marshaling neighbors: %w", err)
	}

	system := `You are a memory consolidation system. Given new memories and their existing neighbors from the behavior graph, propose relationships.

For each memory, choose ONE action:
- "create": new behavior node, with edges to related neighbors
- "merge": combine into an existing neighbor (near-duplicate or strict subset)
- "skip": already fully captured by an existing behavior

For "create" actions, propose edges with these kinds:
- "similar-to": semantically related but distinct
- "overrides": new memory supersedes an existing behavior
- "conflicts": contradicts an existing behavior

For "merge" actions, specify a strategy:
- "absorb": target absorbs the new memory (target is broader)
- "supersede": new memory replaces the target (new is more current)
- "supplement": combine both into a richer merged behavior

Respond with ONLY valid JSON in this exact format:
{
  "relationships": [
    {
      "memory_index": 0,
      "action": "create",
      "edges": [{"target": "bhv-123", "kind": "similar-to", "weight": 0.82}],
      "merge_into": null,
      "rationale": "Related but distinct focus"
    }
  ]
}`

	user := fmt.Sprintf("## New Memories\n%s\n\n## Existing Neighbors\n%s", string(memJSON), string(neighborsJSON))

	return []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, nil
}

// relateResponse is the expected JSON structure from the LLM.
type relateResponse struct {
	Relationships []relateProposal `json:"relationships"`
}

// relateProposal is a single LLM proposal for a memory.
type relateProposal struct {
	MemoryIndex int            `json:"memory_index"`
	Action      string         `json:"action"` // create, merge, skip
	Edges       []proposedEdge `json:"edges"`
	MergeInto   *mergeInfo     `json:"merge_into"`
	Rationale   string         `json:"rationale"`
}

// proposedEdge is an edge proposed by the LLM.
type proposedEdge struct {
	Target string  `json:"target"`
	Kind   string  `json:"kind"`
	Weight float64 `json:"weight"`
}

// mergeInfo describes a proposed merge.
type mergeInfo struct {
	TargetID string `json:"target_id"`
	Strategy string `json:"strategy"` // absorb, supersede, supplement
}

// validEdgeKind maps LLM edge kind strings to store.EdgeKind constants.
var validEdgeKind = map[string]store.EdgeKind{
	"similar-to": store.EdgeKindSimilarTo,
	"overrides":  store.EdgeKindOverrides,
	"conflicts":  store.EdgeKindConflicts,
}

// validMergeStrategies is the set of allowed merge strategies.
var validMergeStrategies = map[string]bool{
	"absorb":     true,
	"supersede":  true,
	"supplement": true,
}

// ParseRelationships parses the raw LLM JSON response into structured proposals.
func ParseRelationships(response string) ([]relateProposal, error) {
	// Strip markdown fences if present.
	cleaned := strings.TrimSpace(response)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		start := 1 // skip opening fence
		end := len(lines)
		if end > start && strings.HasPrefix(strings.TrimSpace(lines[end-1]), "```") {
			end-- // remove closing fence only if present
		}
		if start < end {
			cleaned = strings.Join(lines[start:end], "\n")
		}
	}

	var resp relateResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("parsing relate response: %w", err)
	}

	// Validate each proposal.
	seen := make(map[int]bool, len(resp.Relationships))
	for i, p := range resp.Relationships {
		if seen[p.MemoryIndex] {
			return nil, fmt.Errorf("proposal %d: duplicate memory_index %d", i, p.MemoryIndex)
		}
		seen[p.MemoryIndex] = true

		switch p.Action {
		case "create", "merge", "skip":
			// valid
		default:
			return nil, fmt.Errorf("proposal %d: invalid action %q", i, p.Action)
		}

		if p.Action == "merge" && p.MergeInto == nil {
			return nil, fmt.Errorf("proposal %d: merge action requires merge_into", i)
		}
		if p.MergeInto != nil && p.MergeInto.TargetID == "" {
			return nil, fmt.Errorf("proposal %d: merge_into.target_id must not be empty", i)
		}
		if p.MergeInto != nil && !validMergeStrategies[p.MergeInto.Strategy] {
			return nil, fmt.Errorf("proposal %d: invalid merge strategy %q", i, p.MergeInto.Strategy)
		}

		for j, e := range p.Edges {
			if e.Target == "" {
				return nil, fmt.Errorf("proposal %d edge %d: target must not be empty", i, j)
			}
			if _, ok := validEdgeKind[e.Kind]; !ok {
				return nil, fmt.Errorf("proposal %d edge %d: invalid edge kind %q", i, j, e.Kind)
			}
			if e.Weight <= 0 || e.Weight > 1.0 {
				return nil, fmt.Errorf("proposal %d edge %d: weight must be in (0.0, 1.0], got %f", i, j, e.Weight)
			}
		}
	}

	return resp.Relationships, nil
}
