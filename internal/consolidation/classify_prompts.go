package consolidation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/models"
)

const classifySystemPrompt = `You are a memory classifier for an AI agent learning system.

Given a batch of candidate memories extracted from conversation events, classify each one.

## Taxonomy

### Behavior Kinds
- directive: Explicit instruction to do X (e.g., "Always wrap errors with context")
- constraint: Explicit prohibition — never do Y (e.g., "Never mock the database in integration tests")
- procedure: Multi-step process (e.g., "To deploy: build, test, push, verify")
- preference: Stylistic or tooling preference (e.g., "Prefer pathlib.Path over os.path")
- episodic: Record of a specific event, failure, or session outcome
- workflow: Multi-step workflow with conditions and branching

### Memory Types
- semantic: Factual knowledge, rules, preferences (directive, constraint, preference)
- episodic: Event records, session outcomes, failure reports (episodic)
- procedural: Step-by-step processes, workflows (procedure, workflow)

### Scope
- universal: Applies everywhere regardless of project
- project:<namespace/name>: Only applies within a specific project (infer from mentions of "our CI", "this repo", project-specific tooling)

### Importance (0.0 - 1.0)
- 0.9-1.0: Repeated corrections, safety-critical rules
- 0.7-0.8: Explicit corrections with justification
- 0.5-0.6: Stated preferences, decisions
- 0.3-0.4: Offhand remarks, minor style preferences
- 0.1-0.2: Ambient observations

## Output Format

Return ONLY valid JSON matching this schema:
` + "```json" + `
{
  "classified": [
    {
      "source_events": ["evt-42"],
      "kind": "directive",
      "memory_type": "semantic",
      "scope": "universal",
      "importance": 0.85,
      "content": {
        "canonical": "A token-efficient rewrite of the core lesson (not the raw text verbatim)",
        "summary": "60-char max summary for tiered injection",
        "tags": ["tag1", "tag2"]
      },
      "episode_data": null,
      "workflow_data": null,
      "rationale": "Brief explanation of classification reasoning"
    }
  ]
}
` + "```" + `

## Rules
1. canonical MUST be a meaningful rewrite — NOT the raw text copied verbatim
2. summary MUST be ≤60 characters
3. tags: 2-5 semantic tags (understand meaning, don't just split keywords)
4. For episodic kind: populate episode_data with {"session_id": "...", "timeframe": "...", "actors": [...], "outcome": "..."}
5. For workflow kind: populate workflow_data with {"steps": [{"action": "...", "condition": "...", "on_failure": "..."}], "trigger": "...", "verified": false}
6. Return one classified entry per input candidate, in the same order
7. kind must be one of: directive, constraint, procedure, preference, episodic, workflow
8. memory_type must be one of: semantic, episodic, procedural
9. importance must be between 0.0 and 1.0`

// classifyCandidateEntry is the JSON representation of a candidate sent to the LLM.
type classifyCandidateEntry struct {
	Index         int            `json:"index"`
	SourceEvents  []string       `json:"source_events"`
	RawText       string         `json:"raw_text"`
	CandidateType string         `json:"candidate_type"`
	Confidence    float64        `json:"confidence"`
	Context       map[string]any `json:"context,omitempty"`
}

// ClassifyCandidatesPrompt builds the messages for a batched classification LLM call.
func ClassifyCandidatesPrompt(candidates []Candidate) []llm.Message {
	entries := make([]classifyCandidateEntry, len(candidates))
	for i, c := range candidates {
		entries[i] = classifyCandidateEntry{
			Index:         i,
			SourceEvents:  c.SourceEvents,
			RawText:       c.RawText,
			CandidateType: c.CandidateType,
			Confidence:    c.Confidence,
			Context:       c.SessionContext,
		}
	}

	candidatesJSON, _ := json.MarshalIndent(entries, "", "  ")

	userContent := fmt.Sprintf("Classify these %d candidate memories:\n\n%s", len(candidates), string(candidatesJSON))

	return []llm.Message{
		{Role: "system", Content: classifySystemPrompt},
		{Role: "user", Content: userContent},
	}
}

// classifiedResponse is the top-level JSON response from the LLM.
type classifiedResponse struct {
	Classified []classifiedEntry `json:"classified"`
}

// classifiedEntry is a single classified memory from the LLM response.
type classifiedEntry struct {
	SourceEvents []string          `json:"source_events"`
	Kind         string            `json:"kind"`
	MemoryType   string            `json:"memory_type"`
	Scope        string            `json:"scope"`
	Importance   float64           `json:"importance"`
	Content      classifiedContent `json:"content"`
	EpisodeData  *episodeDataJSON  `json:"episode_data"`
	WorkflowData *workflowDataJSON `json:"workflow_data"`
	Rationale    string            `json:"rationale"`
}

// classifiedContent is the content block within a classified entry.
type classifiedContent struct {
	Canonical string   `json:"canonical"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags"`
}

// episodeDataJSON matches the LLM output for episode data.
type episodeDataJSON struct {
	SessionID string   `json:"session_id"`
	Timeframe string   `json:"timeframe"`
	Actors    []string `json:"actors"`
	Outcome   string   `json:"outcome"`
}

// workflowDataJSON matches the LLM output for workflow data.
type workflowDataJSON struct {
	Steps    []workflowStepJSON `json:"steps"`
	Trigger  string             `json:"trigger"`
	Verified bool               `json:"verified"`
}

// workflowStepJSON matches the LLM output for a workflow step.
type workflowStepJSON struct {
	Action    string `json:"action"`
	Condition string `json:"condition,omitempty"`
	OnFailure string `json:"on_failure,omitempty"`
}

// ParseClassifiedMemories parses the LLM response and maps it back to ClassifiedMemory structs.
// It validates enums, importance range, canonical non-empty, and summary length.
func ParseClassifiedMemories(response string, candidates []Candidate) ([]ClassifiedMemory, error) {
	// Strip markdown code fences if present
	response = stripCodeFences(response)

	var resp classifiedResponse
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		return nil, fmt.Errorf("parsing classify response: %w", err)
	}

	if len(resp.Classified) != len(candidates) {
		return nil, fmt.Errorf("expected %d classified entries, got %d", len(candidates), len(resp.Classified))
	}

	memories := make([]ClassifiedMemory, 0, len(resp.Classified))
	for i, entry := range resp.Classified {
		kind, err := parseKind(entry.Kind)
		if err != nil {
			return nil, fmt.Errorf("candidate %d: %w", i, err)
		}

		memType, err := parseMemoryType(entry.MemoryType)
		if err != nil {
			return nil, fmt.Errorf("candidate %d: %w", i, err)
		}

		if entry.Importance < 0 || entry.Importance > 1 {
			return nil, fmt.Errorf("candidate %d: importance %f out of range [0, 1]", i, entry.Importance)
		}

		if strings.TrimSpace(entry.Content.Canonical) == "" {
			return nil, fmt.Errorf("candidate %d: canonical is empty", i)
		}

		summary := entry.Content.Summary
		if len([]rune(summary)) > 60 {
			summary = string([]rune(summary)[:60])
		}

		scope := entry.Scope
		if scope == "" {
			scope = "universal"
		}

		mem := ClassifiedMemory{
			Candidate:  candidates[i],
			Kind:       kind,
			MemoryType: memType,
			Scope:      scope,
			Importance: entry.Importance,
			Content: models.BehaviorContent{
				Canonical: entry.Content.Canonical,
				Summary:   summary,
				Tags:      entry.Content.Tags,
			},
		}

		if entry.EpisodeData != nil {
			mem.EpisodeData = toEpisodeData(entry.EpisodeData)
		}

		if entry.WorkflowData != nil {
			mem.WorkflowData = toWorkflowData(entry.WorkflowData)
		}

		memories = append(memories, mem)
	}

	return memories, nil
}

// stripCodeFences removes markdown code fences from a JSON response.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}
