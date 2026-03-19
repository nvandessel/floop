package consolidation

import (
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/logging"
)

// LLMConsolidatorConfig configures the LLM-based consolidator.
type LLMConsolidatorConfig struct {
	// Model is the LLM model identifier to use for consolidation.
	Model string

	// ChunkSize is the number of events to process per LLM call.
	ChunkSize int

	// MaxCandidates is the maximum number of candidates to extract per run.
	MaxCandidates int

	// MinConfidence is the server-side minimum confidence threshold for extracted candidates.
	// Candidates below this threshold are filtered out. 0 disables the filter.
	MinConfidence float64

	// TopK is the number of similar behaviors to retrieve during Relate.
	TopK int

	// RetryOnce enables a single retry on transient LLM errors.
	RetryOnce bool
}

// DefaultLLMConsolidatorConfig returns an LLMConsolidatorConfig with sensible defaults.
func DefaultLLMConsolidatorConfig() LLMConsolidatorConfig {
	return LLMConsolidatorConfig{
		Model:         "",
		ChunkSize:     20,
		MaxCandidates: 30,
		MinConfidence: 0.7,
		TopK:          5,
		RetryOnce:     true,
	}
}

// ChunkSummary represents a summarized chunk of events for LLM processing.
type ChunkSummary struct {
	ChunkIndex int      `json:"chunk_index"`
	EventIDs   []string `json:"event_ids"`
	Summary    string   `json:"summary"`
}

// ArcSummary captures a narrative arc across multiple chunks.
type ArcSummary struct {
	ArcID       string   `json:"arc_id"`
	ChunkIDs    []int    `json:"chunk_ids"`
	Description string   `json:"description"`
	Importance  float64  `json:"importance"`
	Tags        []string `json:"tags"`
}

// RelationshipProposal proposes a relationship between a new memory and existing behaviors.
type RelationshipProposal struct {
	MemoryIndex int     `json:"memory_index"`
	TargetID    string  `json:"target_id"`
	Relation    string  `json:"relation"` // "similar_to", "overrides", "specializes"
	Similarity  float64 `json:"similarity"`
}

// MergeDetail describes a merge between a new memory and an existing behavior.
type MergeDetail struct {
	MemoryIndex int    `json:"memory_index"`
	TargetID    string `json:"target_id"`
	Strategy    string `json:"strategy"` // "absorb", "supersede", "supplement"
	Reasoning   string `json:"reasoning"`
}

// ConsolidationRunRecord records metadata about a consolidation run.
type ConsolidationRunRecord struct {
	CandidatesFound int    `json:"candidates_found"`
	Classified      int    `json:"classified"`
	Promoted        int    `json:"promoted"`
	DurationMS      int64  `json:"duration_ms"`
	ProjectID       string `json:"project_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	TokensUsed      int    `json:"tokens_used,omitempty"`
}

// LLMConsolidator implements the Consolidator interface using an LLM client
// for extraction and classification.
type LLMConsolidator struct {
	client    llm.Client
	heuristic *HeuristicConsolidator
	decisions *logging.DecisionLogger
	config    LLMConsolidatorConfig
	runID     string // set by Runner before each pipeline execution
}

// NewLLMConsolidator creates a new LLM-based consolidator.
func NewLLMConsolidator(client llm.Client, decisions *logging.DecisionLogger, config LLMConsolidatorConfig) *LLMConsolidator {
	return &LLMConsolidator{
		client:    client,
		heuristic: NewHeuristicConsolidator(),
		decisions: decisions,
		config:    config,
	}
}

// Model returns the configured LLM model identifier.
func (c *LLMConsolidator) Model() string {
	return c.config.Model
}

// normalizedModel returns the model identifier, falling back to "unknown"
// when unconfigured. Use this for all output paths (logs, DB, node provenance)
// to ensure consistent model attribution.
func (c *LLMConsolidator) normalizedModel() string {
	if c.config.Model == "" {
		return "unknown"
	}
	return c.config.Model
}

// SetRunID sets the run identifier used in decision log entries.
// Called by Runner before each pipeline execution.
func (c *LLMConsolidator) SetRunID(id string) {
	c.runID = id
}

// logDecision writes a decision log entry with the current run_id and model
// automatically injected. All stages should use this instead of c.decisions.Log
// directly so that every JSONL entry is correlated with its run.
func (c *LLMConsolidator) logDecision(fields map[string]any) {
	if c.decisions == nil {
		return
	}
	merged := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		merged[k] = v
	}
	merged["run_id"] = c.runID
	merged["model"] = c.normalizedModel()
	c.decisions.Log(merged)
}

// Extract is implemented in extract.go with three-pass chunked extraction.
// Classify is implemented in classify.go with batched LLM classification.
// Relate is implemented in relate.go with vector search + LLM proposals.
// Promote is implemented in promote.go with merge-aware logic.
