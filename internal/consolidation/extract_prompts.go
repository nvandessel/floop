package consolidation

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nvandessel/floop/internal/events"
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
)

// summarizeChunkPrompt builds the messages for Pass 1: per-chunk summarization.
func summarizeChunkPrompt(evts []events.Event) []llm.Message {
	var sb strings.Builder
	for _, evt := range evts {
		fmt.Fprintf(&sb, "[%s] %s (%s): %s\n", evt.ID, evt.Actor, evt.Kind, evt.Content)
	}

	return []llm.Message{
		{
			Role: "system",
			Content: `You are analyzing a chunk of conversation events from an AI coding assistant session.
Produce a structured JSON summary of this chunk.

Respond with ONLY valid JSON matching this schema:
{
  "summary": "Brief description of what happened in this chunk",
  "tone": "neutral|curious|frustrated|satisfied|breakthrough",
  "phase": "opening|exploring|building|stuck|resolving|wrapping-up",
  "pattern": "teaching|collaborating|debugging|reviewing|planning",
  "key_moments": [{"event_id": "evt-XX", "type": "correction|decision|discovery|failure", "brief": "..."}],
  "open_threads": ["unresolved topic 1", "..."]
}`,
		},
		{
			Role:    "user",
			Content: sb.String(),
		},
	}
}

// arcSynthesisPrompt builds the messages for Pass 2: arc synthesis across chunks.
func arcSynthesisPrompt(summaries []extractChunkSummary) []llm.Message {
	summaryData, err := json.Marshal(summaries)
	if err != nil {
		slog.Warn("arcSynthesisPrompt: failed to marshal summaries", "error", err)
		summaryData = []byte("[]")
	}

	return []llm.Message{
		{
			Role: "system",
			Content: `You are synthesizing a narrative arc from chunk summaries of an AI coding assistant session.
Produce a structured JSON arc summary.

Respond with ONLY valid JSON matching this schema:
{
  "arc": "Narrative description of the session flow",
  "dominant_tone": "tone or tone1→tone2 for shifts",
  "session_outcome": "resolved|unresolved|partial|abandoned",
  "themes": ["theme1", "theme2"],
  "behavioral_signals": ["Signal about user preferences or patterns"]
}`,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Chunk summaries:\n%s", string(summaryData)),
		},
	}
}

// extractCandidatesPrompt builds the messages for Pass 3: per-chunk candidate extraction.
func extractCandidatesPrompt(evts []events.Event, arc *extractArcSummary, existingBehaviors []models.Behavior) []llm.Message {
	var eventsSB strings.Builder
	for _, evt := range evts {
		fmt.Fprintf(&eventsSB, "[%s] %s (%s): %s\n", evt.ID, evt.Actor, evt.Kind, evt.Content)
	}

	var arcContext string
	if arc != nil {
		arcData, err := json.Marshal(arc)
		if err != nil {
			slog.Warn("extractCandidatesPrompt: failed to marshal arc", "error", err)
			arcData = []byte("{}")
		}
		arcContext = fmt.Sprintf("\n\nSession arc context:\n%s", string(arcData))
	}

	var behaviorsContext string
	if len(existingBehaviors) > 0 {
		var briefs []string
		for _, b := range existingBehaviors {
			briefs = append(briefs, fmt.Sprintf("- [%s] %s: %s", b.ID, b.Kind, b.Content.Canonical))
		}
		behaviorsContext = fmt.Sprintf("\n\nExisting behaviors (avoid duplicates):\n%s", strings.Join(briefs, "\n"))
	}

	var contextNote string
	switch {
	case arc != nil && len(existingBehaviors) > 0:
		contextNote = "\nYou have session arc context and existing behaviors for deduplication."
	case arc != nil:
		contextNote = "\nYou have session arc context to inform your analysis."
	case len(existingBehaviors) > 0:
		contextNote = "\nYou have existing behaviors for deduplication."
	}

	return []llm.Message{
		{
			Role: "system",
			Content: `You are extracting behavioral memory candidates from conversation events.` + contextNote + `

Respond with ONLY valid JSON matching this schema:
{
  "candidates": [
    {
      "source_events": ["evt-XX", "evt-YY"],
      "raw_text": "The relevant excerpt from the conversation",
      "candidate_type": "correction|discovery|decision|failure|workflow",
      "confidence": 0.0-1.0,
      "sentiment": "neutral|curious|frustrated|satisfied|breakthrough",
      "session_phase": "opening|exploring|building|stuck|resolving|wrapping-up",
      "interaction_pattern": "teaching|collaborating|debugging|reviewing|planning",
      "rationale": "Why this is a behavioral signal worth capturing",
      "already_captured": false
    }
  ]
}

Rules:
- Only extract genuine behavioral signals (corrections, discoveries, decisions, failures, workflows)
- Set already_captured=true if an existing behavior already covers this signal
- Be conservative with confidence — only high-confidence signals above 0.7
- Include rationale explaining why each candidate matters`,
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("Events:\n%s%s%s", eventsSB.String(), arcContext, behaviorsContext),
		},
	}
}
