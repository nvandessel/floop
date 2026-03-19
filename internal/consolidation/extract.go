package consolidation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
)

// extractChunkSummary is the enriched summary returned by Pass 1.
// This extends the shared ChunkSummary with tone, phase, and pattern fields
// needed by the three-pass extraction pipeline.
type extractChunkSummary struct {
	Summary     string      `json:"summary"`
	Tone        string      `json:"tone"`
	Phase       string      `json:"phase"`
	Pattern     string      `json:"pattern"`
	KeyMoments  []keyMoment `json:"key_moments"`
	OpenThreads []string    `json:"open_threads"`
	ChunkIndex  int         `json:"chunk_index"`
	EventIDs    []string    `json:"event_ids"`
}

// keyMoment is a notable moment within a chunk.
type keyMoment struct {
	EventID string `json:"event_id"`
	Type    string `json:"type"`
	Brief   string `json:"brief"`
}

// extractArcSummary captures the narrative arc across all chunks (Pass 2 output).
type extractArcSummary struct {
	Arc               string   `json:"arc"`
	DominantTone      string   `json:"dominant_tone"`
	SessionOutcome    string   `json:"session_outcome"`
	Themes            []string `json:"themes"`
	BehavioralSignals []string `json:"behavioral_signals"`
}

// extractResponse is the JSON envelope returned by Pass 3.
type extractResponse struct {
	Candidates []extractCandidate `json:"candidates"`
}

// extractCandidate is a single candidate from the LLM's Pass 3 response.
type extractCandidate struct {
	SourceEvents       []string `json:"source_events"`
	RawText            string   `json:"raw_text"`
	CandidateType      string   `json:"candidate_type"`
	Confidence         float64  `json:"confidence"`
	Sentiment          string   `json:"sentiment"`
	SessionPhase       string   `json:"session_phase"`
	InteractionPattern string   `json:"interaction_pattern"`
	Rationale          string   `json:"rationale"`
	AlreadyCaptured    bool     `json:"already_captured"`
}

// Extract implements three-pass chunked extraction: Summarize, Arc, Extract.
// Each pass can fail independently; on per-chunk failure, falls back to v0 heuristic.
func (c *LLMConsolidator) Extract(ctx context.Context, evts []events.Event) ([]Candidate, error) {
	if len(evts) == 0 {
		return nil, nil
	}

	chunkSize := c.config.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 20
	}

	chunks := chunkEvents(evts, chunkSize)

	c.logDecision(map[string]any{
		"stage":      "extract",
		"pass":       "start",
		"num_events": len(evts),
		"num_chunks": len(chunks),
		"chunk_size": chunkSize,
	})

	// Pass 1: Summarize each chunk (continues past per-chunk failures)
	summaries := c.summarizeChunks(ctx, chunks)

	// Pass 2: Arc synthesis
	var arc *extractArcSummary
	if len(summaries) > 0 {
		var pass2Err error
		arc, pass2Err = c.synthesizeArc(ctx, summaries)
		if pass2Err != nil {
			slog.Warn("extract pass 2 (arc) failed, continuing without arc", "error", pass2Err)
			c.logDecision(map[string]any{
				"stage": "extract",
				"pass":  "arc",
				"error": pass2Err.Error(),
			})
		}
	}

	// Fetch existing behaviors for deduplication context.
	// NOTE: fetchExistingBehaviors is a stub returning nil until store access is wired.
	// Deduplication via already_captured is not yet active.
	existingBehaviors := c.fetchExistingBehaviors(ctx)

	// Pass 3: Extract candidates from each chunk
	var candidates []Candidate
	for i, chunk := range chunks {
		extracted, err := c.extractFromChunk(ctx, chunk, arc, existingBehaviors)
		if err != nil {
			slog.Warn("extract pass 3 failed for chunk, falling back to heuristic",
				"chunk", i, "error", err)
			c.logDecision(map[string]any{
				"stage":    "extract",
				"pass":     "extract",
				"chunk":    i,
				"error":    err.Error(),
				"fallback": "heuristic",
			})
			fallback, fallbackErr := c.heuristic.Extract(ctx, chunk)
			if fallbackErr != nil {
				slog.Warn("heuristic fallback also failed", "chunk", i, "error", fallbackErr)
			}
			candidates = append(candidates, fallback...)
			continue
		}
		candidates = append(candidates, extracted...)
	}

	// Enforce MaxCandidates cap, keeping highest-confidence candidates
	if c.config.MaxCandidates > 0 && len(candidates) > c.config.MaxCandidates {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Confidence > candidates[j].Confidence
		})
		candidates = candidates[:c.config.MaxCandidates]
	}

	c.logDecision(map[string]any{
		"stage":            "extract",
		"pass":             "complete",
		"total_candidates": len(candidates),
	})

	return candidates, nil
}

// summarizeChunks runs Pass 1: per-chunk LLM summarization.
// Continues past per-chunk failures so the arc reflects all successfully-summarized chunks.
func (c *LLMConsolidator) summarizeChunks(ctx context.Context, chunks [][]events.Event) []extractChunkSummary {
	var summaries []extractChunkSummary

	for i, chunk := range chunks {
		messages := summarizeChunkPrompt(chunk)
		response, err := c.client.Complete(ctx, messages)
		if err != nil {
			slog.Warn("extract pass 1: summarize chunk failed, skipping", "chunk", i, "error", err)
			c.logDecision(map[string]any{
				"stage": "extract",
				"pass":  "summarize",
				"chunk": i,
				"error": err.Error(),
			})
			continue
		}

		var summary extractChunkSummary
		if err := json.Unmarshal([]byte(llm.ExtractJSON(response)), &summary); err != nil {
			slog.Warn("extract pass 1: parse chunk summary failed, skipping", "chunk", i, "error", err)
			c.logDecision(map[string]any{
				"stage": "extract",
				"pass":  "summarize",
				"chunk": i,
				"error": err.Error(),
			})
			continue
		}

		// Enrich with chunk metadata
		summary.ChunkIndex = i
		summary.EventIDs = eventIDs(chunk)

		summaries = append(summaries, summary)
	}

	c.logDecision(map[string]any{
		"stage":         "extract",
		"pass":          "summarize",
		"num_summaries": len(summaries),
		"num_chunks":    len(chunks),
	})

	return summaries
}

// synthesizeArc runs Pass 2: single LLM call for session arc.
func (c *LLMConsolidator) synthesizeArc(ctx context.Context, summaries []extractChunkSummary) (*extractArcSummary, error) {
	messages := arcSynthesisPrompt(summaries)
	response, err := c.client.Complete(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("arc synthesis: %w", err)
	}

	var arc extractArcSummary
	if err := json.Unmarshal([]byte(llm.ExtractJSON(response)), &arc); err != nil {
		return nil, fmt.Errorf("parse arc summary: %w", err)
	}

	c.logDecision(map[string]any{
		"stage":           "extract",
		"pass":            "arc",
		"session_outcome": arc.SessionOutcome,
		"themes":          arc.Themes,
	})

	return &arc, nil
}

// extractFromChunk runs Pass 3 for a single chunk: LLM extraction with arc context.
func (c *LLMConsolidator) extractFromChunk(ctx context.Context, chunk []events.Event, arc *extractArcSummary, behaviors []models.Behavior) ([]Candidate, error) {
	messages := extractCandidatesPrompt(chunk, arc, behaviors)
	response, err := c.client.Complete(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("extract from chunk: %w", err)
	}

	var resp extractResponse
	if err := json.Unmarshal([]byte(llm.ExtractJSON(response)), &resp); err != nil {
		return nil, fmt.Errorf("parse extract response: %w", err)
	}

	var candidates []Candidate
	for _, ec := range resp.Candidates {
		// Filter out already-captured candidates
		if ec.AlreadyCaptured {
			continue
		}

		// Skip candidates with empty raw text (nothing actionable)
		if strings.TrimSpace(ec.RawText) == "" {
			continue
		}

		// Clamp confidence to [0.0, 1.0]
		confidence := ec.Confidence
		if confidence < 0 {
			confidence = 0
		} else if confidence > 1 {
			confidence = 1
		}

		// Enforce server-side minimum confidence threshold.
		// The prompt instructs the LLM to only emit high-confidence signals,
		// but a non-compliant LLM may ignore that instruction.
		if c.config.MinConfidence > 0 && confidence < c.config.MinConfidence {
			slog.Debug("extract: filtering low-confidence candidate",
				"confidence", confidence, "min", c.config.MinConfidence, "raw_text", ec.RawText)
			continue
		}

		candidates = append(candidates, Candidate{
			SourceEvents:       ec.SourceEvents,
			RawText:            ec.RawText,
			CandidateType:      ec.CandidateType,
			Confidence:         confidence,
			Sentiment:          ec.Sentiment,
			SessionPhase:       ec.SessionPhase,
			InteractionPattern: ec.InteractionPattern,
			Rationale:          ec.Rationale,
			SessionContext:     buildSessionContext(chunk),
		})
	}

	return candidates, nil
}

// fetchExistingBehaviors returns existing behaviors for deduplication context.
// TODO: Wire store access to populate deduplication context for Pass 3.
// Until then, the "already_captured" filtering in extractFromChunk can only
// fire if the LLM spontaneously sets it without context.
func (c *LLMConsolidator) fetchExistingBehaviors(_ context.Context) []models.Behavior {
	slog.Info("fetchExistingBehaviors: stub — deduplication not yet active")
	return nil
}

// chunkEvents splits events into groups of at most size elements.
func chunkEvents(evts []events.Event, size int) [][]events.Event {
	if size <= 0 {
		size = 20
	}

	var chunks [][]events.Event
	for i := 0; i < len(evts); i += size {
		end := i + size
		if end > len(evts) {
			end = len(evts)
		}
		chunks = append(chunks, evts[i:end])
	}
	return chunks
}

// eventIDs extracts IDs from a slice of events.
func eventIDs(evts []events.Event) []string {
	ids := make([]string, len(evts))
	for i, evt := range evts {
		ids[i] = evt.ID
	}
	return ids
}

// buildSessionContext extracts common session context from a chunk of events.
func buildSessionContext(evts []events.Event) map[string]any {
	if len(evts) == 0 {
		return nil
	}
	ctx := map[string]any{}
	if evts[0].SessionID != "" {
		ctx["session_id"] = evts[0].SessionID
	}
	if evts[0].ProjectID != "" {
		ctx["project_id"] = evts[0].ProjectID
	}
	if p := evts[0].Provenance; p != nil {
		if p.Model != "" {
			ctx["model"] = p.Model
		}
		if p.Branch != "" {
			ctx["branch"] = p.Branch
		}
		if p.TaskContext != "" {
			ctx["task"] = p.TaskContext
		}
	}
	if len(ctx) == 0 {
		return nil
	}
	return ctx
}
