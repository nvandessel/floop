package consolidation

import (
	"context"
	"fmt"

	"github.com/nvandessel/floop/internal/models"
)

// validKinds is the set of valid BehaviorKind values for classification.
var validKinds = map[string]models.BehaviorKind{
	"directive":  models.BehaviorKindDirective,
	"constraint": models.BehaviorKindConstraint,
	"procedure":  models.BehaviorKindProcedure,
	"preference": models.BehaviorKindPreference,
	"episodic":   models.BehaviorKindEpisodic,
	"workflow":   models.BehaviorKindWorkflow,
}

// validMemoryTypes is the set of valid MemoryType values for classification.
var validMemoryTypes = map[string]models.MemoryType{
	"semantic":   models.MemoryTypeSemantic,
	"episodic":   models.MemoryTypeEpisodic,
	"procedural": models.MemoryTypeProcedural,
}

// parseKind validates and converts a kind string to a BehaviorKind.
func parseKind(s string) (models.BehaviorKind, error) {
	if k, ok := validKinds[s]; ok {
		return k, nil
	}
	return "", fmt.Errorf("invalid kind %q", s)
}

// parseMemoryType validates and converts a memory type string to a MemoryType.
func parseMemoryType(s string) (models.MemoryType, error) {
	if mt, ok := validMemoryTypes[s]; ok {
		return mt, nil
	}
	return "", fmt.Errorf("invalid memory_type %q", s)
}

// toEpisodeData converts parsed JSON episode data to the models type.
func toEpisodeData(ed *episodeDataJSON) *models.EpisodeData {
	if ed == nil {
		return nil
	}
	return &models.EpisodeData{
		SessionID: ed.SessionID,
		Timeframe: ed.Timeframe,
		Actors:    ed.Actors,
		Outcome:   ed.Outcome,
	}
}

// toWorkflowData converts parsed JSON workflow data to the models type.
func toWorkflowData(wd *workflowDataJSON) *models.WorkflowData {
	if wd == nil {
		return nil
	}
	steps := make([]models.WorkflowStep, len(wd.Steps))
	for i, s := range wd.Steps {
		steps[i] = models.WorkflowStep{
			Action:    s.Action,
			Condition: s.Condition,
			OnFailure: s.OnFailure,
		}
	}
	return &models.WorkflowData{
		Steps:    steps,
		Trigger:  wd.Trigger,
		Verified: wd.Verified,
	}
}

// batchCandidates splits candidates into batches. If len(candidates) > threshold,
// each batch has at most maxSize candidates. Otherwise, a single batch is returned.
func batchCandidates(candidates []Candidate, maxSize int) [][]Candidate {
	if maxSize <= 0 {
		maxSize = 20
	}
	// Only batch if we exceed the threshold (maxSize + 10, i.e., >30 for default 20)
	threshold := maxSize + 10
	if len(candidates) <= threshold {
		return [][]Candidate{candidates}
	}

	var batches [][]Candidate
	for i := 0; i < len(candidates); i += maxSize {
		end := i + maxSize
		if end > len(candidates) {
			end = len(candidates)
		}
		batches = append(batches, candidates[i:end])
	}
	return batches
}

// Classify assigns behavior kinds, memory types, canonical forms, summaries,
// and structured data to candidates using batched LLM calls.
// On LLM or parse failure, falls back to the heuristic classifier per batch.
func (c *LLMConsolidator) Classify(ctx context.Context, candidates []Candidate) ([]ClassifiedMemory, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	batches := batchCandidates(candidates, c.config.MaxCandidates)

	var all []ClassifiedMemory
	for batchIdx, batch := range batches {
		classified, err := c.classifyBatch(ctx, batch, batchIdx)
		if err != nil {
			// Log the failure
			c.decisions.Log(map[string]any{
				"stage":     "classify",
				"event":     "llm_fallback",
				"batch":     batchIdx,
				"reason":    err.Error(),
				"batch_len": len(batch),
			})
			// Fallback to v0 heuristic for this batch
			fallback, fbErr := c.heuristic.Classify(ctx, batch)
			if fbErr != nil {
				return nil, fmt.Errorf("classify batch %d: LLM failed (%w), heuristic also failed: %w", batchIdx, err, fbErr)
			}
			all = append(all, fallback...)
			continue
		}
		all = append(all, classified...)
	}

	return all, nil
}

// classifyBatch sends a single batch to the LLM and parses the response.
// Returns an error if the LLM call or parsing fails (after optional retry).
func (c *LLMConsolidator) classifyBatch(ctx context.Context, batch []Candidate, batchIdx int) ([]ClassifiedMemory, error) {
	msgs := ClassifyCandidatesPrompt(batch)

	response, err := c.client.Complete(ctx, msgs)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	classified, err := ParseClassifiedMemories(response, batch)
	if err != nil {
		// Retry once if configured
		if c.config.RetryOnce {
			c.decisions.Log(map[string]any{
				"stage":  "classify",
				"event":  "parse_retry",
				"batch":  batchIdx,
				"reason": err.Error(),
			})
			response2, err2 := c.client.Complete(ctx, msgs)
			if err2 != nil {
				return nil, fmt.Errorf("LLM retry failed: %w", err2)
			}
			classified2, err2 := ParseClassifiedMemories(response2, batch)
			if err2 != nil {
				return nil, fmt.Errorf("parse failed after retry: %w", err2)
			}
			return classified2, nil
		}
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	// Log successful classification
	c.decisions.Log(map[string]any{
		"stage":      "classify",
		"event":      "batch_classified",
		"batch":      batchIdx,
		"count":      len(classified),
		"llm_driven": true,
	})

	return classified, nil
}
