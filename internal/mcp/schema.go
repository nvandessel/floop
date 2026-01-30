// Package mcp provides an MCP (Model Context Protocol) server for floop.
package mcp

import (
	"time"
)

// FloopActiveInput defines the input for floop_active tool.
type FloopActiveInput struct {
	File string `json:"file,omitempty" jsonschema:"description=Current file path (relative to project root)"`
	Task string `json:"task,omitempty" jsonschema:"description=Current task type (e.g. 'development', 'testing', 'refactoring')"`
}

// FloopActiveOutput defines the output for floop_active tool.
type FloopActiveOutput struct {
	Context map[string]interface{} `json:"context" jsonschema:"description=Context used for activation"`
	Active  []BehaviorSummary      `json:"active" jsonschema:"description=List of active behaviors"`
	Count   int                    `json:"count" jsonschema:"description=Number of active behaviors"`
}

// BehaviorSummary provides a simplified view of a behavior.
type BehaviorSummary struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Kind       string                 `json:"kind"`
	Content    map[string]interface{} `json:"content"`
	Confidence float64                `json:"confidence"`
	When       map[string]interface{} `json:"when,omitempty"`
}

// FloopLearnInput defines the input for floop_learn tool.
type FloopLearnInput struct {
	Wrong     string `json:"wrong" jsonschema:"description=What the agent did that needs correction,required"`
	Right     string `json:"right" jsonschema:"description=What should have been done instead,required"`
	File      string `json:"file,omitempty" jsonschema:"description=Relevant file path for context"`
	AutoMerge bool   `json:"auto_merge,omitempty" jsonschema:"description=Enable automatic merging of duplicate behaviors (default: false)"`
}

// FloopLearnOutput defines the output for floop_learn tool.
type FloopLearnOutput struct {
	CorrectionID    string   `json:"correction_id" jsonschema:"description=ID of the captured correction"`
	BehaviorID      string   `json:"behavior_id" jsonschema:"description=ID of the extracted behavior"`
	AutoAccepted    bool     `json:"auto_accepted" jsonschema:"description=Whether behavior was automatically accepted"`
	Confidence      float64  `json:"confidence" jsonschema:"description=Placement confidence (0.0-1.0)"`
	RequiresReview  bool     `json:"requires_review" jsonschema:"description=Whether behavior requires manual review"`
	ReviewReasons   []string `json:"review_reasons,omitempty" jsonschema:"description=Reasons why review is needed"`
	MergedIntoID    string   `json:"merged_into_id,omitempty" jsonschema:"description=ID of behavior this was merged into (if auto-merged)"`
	MergeSimilarity float64  `json:"merge_similarity,omitempty" jsonschema:"description=Similarity score with merged behavior (0.0-1.0)"`
	Message         string   `json:"message" jsonschema:"description=Human-readable result message"`
}

// FloopListInput defines the input for floop_list tool.
type FloopListInput struct {
	Corrections bool `json:"corrections,omitempty" jsonschema:"description=List corrections instead of behaviors (default: false)"`
}

// FloopListOutput defines the output for floop_list tool.
type FloopListOutput struct {
	Behaviors   []BehaviorListItem   `json:"behaviors,omitempty" jsonschema:"description=List of behaviors"`
	Corrections []CorrectionListItem `json:"corrections,omitempty" jsonschema:"description=List of corrections"`
	Count       int                  `json:"count" jsonschema:"description=Number of items"`
}

// BehaviorListItem provides a list view of a behavior.
type BehaviorListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Confidence float64   `json:"confidence"`
	Source     string    `json:"source"`
	CreatedAt  time.Time `json:"created_at"`
}

// CorrectionListItem provides a list view of a correction.
type CorrectionListItem struct {
	ID              string    `json:"id"`
	Timestamp       time.Time `json:"timestamp"`
	AgentAction     string    `json:"agent_action"`
	CorrectedAction string    `json:"corrected_action"`
	Processed       bool      `json:"processed"`
}

// FloopDeduplicateInput defines the input for floop_deduplicate tool.
type FloopDeduplicateInput struct {
	DryRun    bool    `json:"dry_run,omitempty" jsonschema:"description=If true, only report duplicates without merging (default: false)"`
	Threshold float64 `json:"threshold,omitempty" jsonschema:"description=Similarity threshold for duplicate detection (0.0-1.0, default: 0.9)"`
	Scope     string  `json:"scope,omitempty" jsonschema:"description=Scope of deduplication: 'local', 'global', or 'both' (default: 'both')"`
}

// FloopDeduplicateOutput defines the output for floop_deduplicate tool.
type FloopDeduplicateOutput struct {
	DuplicatesFound int                   `json:"duplicates_found" jsonschema:"description=Number of duplicate pairs found"`
	Merged          int                   `json:"merged" jsonschema:"description=Number of behaviors merged"`
	Results         []DeduplicationResult `json:"results,omitempty" jsonschema:"description=Details of each deduplication action"`
	Message         string                `json:"message" jsonschema:"description=Human-readable summary"`
}

// DeduplicationResult represents the outcome of deduplicating a single behavior.
type DeduplicationResult struct {
	BehaviorID   string  `json:"behavior_id" jsonschema:"description=ID of the behavior being deduplicated"`
	BehaviorName string  `json:"behavior_name" jsonschema:"description=Name of the behavior"`
	Action       string  `json:"action" jsonschema:"description=Action taken: 'skip', 'merge', or 'none'"`
	MatchID      string  `json:"match_id,omitempty" jsonschema:"description=ID of the matching duplicate (if found)"`
	MatchName    string  `json:"match_name,omitempty" jsonschema:"description=Name of the matching duplicate"`
	Similarity   float64 `json:"similarity,omitempty" jsonschema:"description=Similarity score (0.0-1.0)"`
	MergedID     string  `json:"merged_id,omitempty" jsonschema:"description=ID of the merged behavior (if merge performed)"`
	Error        string  `json:"error,omitempty" jsonschema:"description=Error message if operation failed"`
}
