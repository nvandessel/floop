package consolidation

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/nvandessel/floop/internal/models"
)

// defaultMaxCandidates is the default batch size for classification.
const defaultMaxCandidates = 30

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

// validKindMemoryType maps each BehaviorKind to its required MemoryType.
var validKindMemoryType = map[models.BehaviorKind]models.MemoryType{
	models.BehaviorKindDirective:  models.MemoryTypeSemantic,
	models.BehaviorKindConstraint: models.MemoryTypeSemantic,
	models.BehaviorKindPreference: models.MemoryTypeSemantic,
	models.BehaviorKindProcedure:  models.MemoryTypeProcedural,
	models.BehaviorKindWorkflow:   models.MemoryTypeProcedural,
	models.BehaviorKindEpisodic:   models.MemoryTypeEpisodic,
}

// parseKind validates and converts a kind string to a BehaviorKind (case-insensitive).
func parseKind(s string) (models.BehaviorKind, error) {
	if k, ok := validKinds[strings.ToLower(s)]; ok {
		return k, nil
	}
	return "", fmt.Errorf("invalid kind %q", s)
}

// parseMemoryType validates and converts a memory type string to a MemoryType (case-insensitive).
func parseMemoryType(s string) (models.MemoryType, error) {
	if mt, ok := validMemoryTypes[strings.ToLower(s)]; ok {
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

// batchCandidates splits candidates into batches of at most maxSize.
// If len(candidates) <= maxSize, a single batch is returned.
func batchCandidates(candidates []Candidate, maxSize int) [][]Candidate {
	if maxSize <= 0 {
		maxSize = defaultMaxCandidates
	}
	if len(candidates) <= maxSize {
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
		// Check for context cancellation before processing each batch
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("classify cancelled before batch %d: %w", batchIdx, err)
		}

		classified, err := c.classifyBatch(ctx, batch, batchIdx)
		if err != nil {
			// Log the failure
			c.logDecision(map[string]any{
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
	msgs, err := ClassifyCandidatesPrompt(batch)
	if err != nil {
		return nil, fmt.Errorf("building classify prompt: %w", err)
	}

	response, err := c.client.Complete(ctx, msgs)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	classified, err := ParseClassifiedMemories(response, batch)
	if err != nil {
		// Retry once if configured
		if c.config.RetryOnce {
			c.logDecision(map[string]any{
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
			c.logDecision(map[string]any{
				"stage":      "classify",
				"event":      "batch_classified",
				"batch":      batchIdx,
				"count":      len(classified2),
				"llm_driven": true,
				"retried":    true,
				"prompt":     messagesToStrings(msgs),
				"response":   response2,
				"parsed":     classified2,
			})
			return classified2, nil
		}
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	// Log successful classification with training data
	c.logDecision(map[string]any{
		"stage":      "classify",
		"event":      "batch_classified",
		"batch":      batchIdx,
		"count":      len(classified),
		"llm_driven": true,
		"prompt":     messagesToStrings(msgs),
		"response":   response,
		"parsed":     classified,
	})

	return classified, nil
}

// sourceEventsKey creates a lookup key from a sorted copy of event IDs.
// Order-insensitive: ["b","a"] and ["a","b"] produce the same key.
func sourceEventsKey(events []string) string {
	sorted := make([]string, len(events))
	copy(sorted, events)
	slices.Sort(sorted)
	return strings.Join(sorted, ",")
}

// sourceEventsMatch checks if two source event slices contain the same events (order-insensitive).
func sourceEventsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := make([]string, len(a))
	copy(sortedA, a)
	slices.Sort(sortedA)
	sortedB := make([]string, len(b))
	copy(sortedB, b)
	slices.Sort(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}

// validateScope checks that scope is either "universal" or "project:<namespace/name>"
// where the namespace after the colon is non-empty.
func validateScope(scope string, candidateIdx int) error {
	if scope == "universal" {
		return nil
	}
	if !strings.HasPrefix(scope, "project:") {
		return fmt.Errorf("candidate %d: invalid scope %q (must be \"universal\" or \"project:<namespace/name>\")", candidateIdx, scope)
	}
	if len(scope) <= len("project:") {
		return fmt.Errorf("candidate %d: invalid scope %q: project scope must include a namespace/name after the colon", candidateIdx, scope)
	}
	return nil
}

// validateKindMemoryType checks that kind and memory_type are a valid combination.
func validateKindMemoryType(kind models.BehaviorKind, memType models.MemoryType, candidateIdx int) error {
	if expected, ok := validKindMemoryType[kind]; ok && memType != expected {
		return fmt.Errorf("candidate %d: kind %q requires memory_type %q, got %q", candidateIdx, kind, expected, memType)
	}
	return nil
}

// validateTagCount checks that tag count is within the expected range [2, 5].
func validateTagCount(tags []string, candidateIdx int) error {
	if len(tags) < 2 || len(tags) > 5 {
		return fmt.Errorf("candidate %d: tags count %d out of expected range [2, 5]", candidateIdx, len(tags))
	}
	return nil
}

// validateStructuredData checks that episodic/workflow kinds have their required data.
func validateStructuredData(kind models.BehaviorKind, episodeData *models.EpisodeData, workflowData *models.WorkflowData, candidateIdx int) error {
	if kind == models.BehaviorKindEpisodic && episodeData == nil {
		return fmt.Errorf("candidate %d: kind \"episodic\" requires episode_data", candidateIdx)
	}
	if kind == models.BehaviorKindWorkflow && workflowData == nil {
		return fmt.Errorf("candidate %d: kind \"workflow\" requires workflow_data", candidateIdx)
	}
	return nil
}

// truncateSummary truncates summary to 60 runes, logging if truncation occurs.
func truncateSummary(summary string, candidateIdx int) string {
	if len([]rune(summary)) > 60 {
		slog.Debug("classify: summary truncated to 60 chars",
			"candidate_idx", candidateIdx, "original_len", len([]rune(summary)))
		return string([]rune(summary)[:60])
	}
	return summary
}
