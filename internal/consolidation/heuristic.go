package consolidation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/models"
	"github.com/nvandessel/floop/internal/store"
)

// HeuristicConsolidator implements the Consolidator interface using
// pattern-matching heuristics rather than an LLM.
type HeuristicConsolidator struct{}

// NewHeuristicConsolidator creates a new heuristic-based consolidator.
func NewHeuristicConsolidator() *HeuristicConsolidator {
	return &HeuristicConsolidator{}
}

// patternEntry maps a set of signal phrases to a candidate type and confidence.
type patternEntry struct {
	patterns      []string
	candidateType string
	confidence    float64
}

var signalPatterns = []patternEntry{
	{
		patterns:      []string{"no, don't", "instead of", "not that", "actually use", "wrong", "no don't", "don't do"},
		candidateType: "correction",
		confidence:    0.7,
	},
	{
		patterns:      []string{"didn't work", "failed", "broken"},
		candidateType: "failure",
		confidence:    0.6,
	},
	{
		patterns:      []string{"let's go with", "we'll use", "decided on", "choosing"},
		candidateType: "decision",
		confidence:    0.5,
	},
}

// Extract scans events for heuristic patterns indicating behavioral signals.
func (h *HeuristicConsolidator) Extract(ctx context.Context, evts []events.Event) ([]Candidate, error) {
	var candidates []Candidate

	for _, evt := range evts {
		// Only extract from user messages
		if evt.Actor != events.ActorUser {
			continue
		}

		// Skip short messages
		if len(evt.Content) < 10 {
			continue
		}

		lower := strings.ToLower(evt.Content)

		for _, entry := range signalPatterns {
			if matchesAny(lower, entry.patterns) {
				candidates = append(candidates, Candidate{
					SourceEvents:  []string{evt.ID},
					RawText:       evt.Content,
					CandidateType: entry.candidateType,
					Confidence:    entry.confidence,
					SessionContext: map[string]any{
						"session_id": evt.SessionID,
						"project_id": evt.ProjectID,
					},
				})
				break // one candidate per event
			}
		}
	}

	return candidates, nil
}

// matchesAny returns true if text contains any of the patterns.
func matchesAny(text string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// Classify maps candidate types to behavior kinds and memory types.
func (h *HeuristicConsolidator) Classify(ctx context.Context, candidates []Candidate) ([]ClassifiedMemory, error) {
	memories := make([]ClassifiedMemory, 0, len(candidates))

	for _, c := range candidates {
		kind, memType := classifyCandidate(c.CandidateType)

		content := models.BehaviorContent{
			Canonical: strings.TrimSpace(c.RawText),
			Summary:   truncate(strings.TrimSpace(c.RawText), 60),
			Tags:      extractTags(c.RawText),
		}

		mem := ClassifiedMemory{
			Candidate:  c,
			Kind:       kind,
			MemoryType: memType,
			Scope:      "universal",
			Importance: c.Confidence,
			Content:    content,
		}

		memories = append(memories, mem)
	}

	return memories, nil
}

// classifyCandidate maps a candidate type string to a behavior kind and memory type.
func classifyCandidate(candidateType string) (models.BehaviorKind, string) {
	switch candidateType {
	case "correction":
		return models.BehaviorKindDirective, models.MemoryTypeSemantic
	case "decision":
		return models.BehaviorKindPreference, models.MemoryTypeSemantic
	case "failure":
		return models.BehaviorKindEpisodic, models.MemoryTypeEpisodic
	case "discovery":
		return models.BehaviorKindDirective, models.MemoryTypeSemantic
	case "workflow":
		return models.BehaviorKindProcedure, models.MemoryTypeProcedural
	default:
		return models.BehaviorKindDirective, models.MemoryTypeSemantic
	}
}

// truncate returns s truncated to maxLen characters with "..." appended if it was longer.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractTags splits text on whitespace, filters short words, and returns up to 5 keywords.
func extractTags(text string) []string {
	words := strings.Fields(strings.ToLower(text))
	seen := make(map[string]bool)
	var tags []string

	for _, w := range words {
		// Strip common punctuation
		w = strings.Trim(w, ".,;:!?\"'()[]{}")
		if len(w) < 4 || seen[w] {
			continue
		}
		seen[w] = true
		tags = append(tags, w)
		if len(tags) >= 5 {
			break
		}
	}

	return tags
}

// Relate is a v0 passthrough that returns empty edges and merge proposals.
func (h *HeuristicConsolidator) Relate(ctx context.Context, memories []ClassifiedMemory, s store.GraphStore) ([]store.Edge, []MergeProposal, error) {
	return nil, nil, nil
}

// Promote writes classified memories into the graph store as behavior nodes.
func (h *HeuristicConsolidator) Promote(ctx context.Context, memories []ClassifiedMemory, edges []store.Edge, merges []MergeProposal, s store.GraphStore) error {
	if s == nil {
		return nil
	}

	// Build set of memories that have merge proposals (skip them in v0)
	merged := make(map[int]bool)
	for _, m := range merges {
		for i, mem := range memories {
			if mem.RawText == m.Memory.RawText {
				merged[i] = true
			}
		}
	}

	for i, mem := range memories {
		if merged[i] {
			continue
		}

		node := store.Node{
			ID:   fmt.Sprintf("consolidated-%d-%d", time.Now().UnixNano(), i),
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"canonical":     mem.Content.Canonical,
				"summary":       mem.Content.Summary,
				"tags":          mem.Content.Tags,
				"behavior_type": string(mem.Kind),
				"memory_type":   mem.MemoryType,
				"scope":         mem.Scope,
			},
			Metadata: map[string]interface{}{
				"provenance_source_type":     string(models.SourceTypeConsolidated),
				"provenance_consolidated_by": "heuristic-v0",
				"confidence":                 mem.Confidence,
			},
		}

		if _, err := s.AddNode(ctx, node); err != nil {
			return fmt.Errorf("adding consolidated node: %w", err)
		}
	}

	for _, edge := range edges {
		if err := s.AddEdge(ctx, edge); err != nil {
			return fmt.Errorf("adding edge: %w", err)
		}
	}

	return nil
}
