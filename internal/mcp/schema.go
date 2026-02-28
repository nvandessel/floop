// Package mcp provides an MCP (Model Context Protocol) server for floop.
package mcp

import (
	"time"
)

// FloopActiveInput defines the input for floop_active tool.
type FloopActiveInput struct {
	File string `json:"file,omitempty" jsonschema:"Current file path (relative to project root)"`
	Task string `json:"task,omitempty" jsonschema:"Current task type (e.g. 'development', 'testing', 'refactoring')"`
}

// TokenStats provides token budget awareness for active behaviors.
type TokenStats struct {
	TotalCanonicalTokens int `json:"total_canonical_tokens"`
	BudgetDefault        int `json:"budget_default"`
	BehaviorCount        int `json:"behavior_count"`
	FullCount            int `json:"full_count"`
	SummaryCount         int `json:"summary_count"`
	NameOnlyCount        int `json:"name_only_count"`
	OmittedCount         int `json:"omitted_count"`
}

// FloopActiveOutput defines the output for floop_active tool.
type FloopActiveOutput struct {
	Context    map[string]interface{} `json:"context" jsonschema:"Context used for activation"`
	Active     []BehaviorSummary      `json:"active" jsonschema:"List of active behaviors"`
	Count      int                    `json:"count" jsonschema:"Number of active behaviors"`
	TokenStats *TokenStats            `json:"token_stats,omitempty"`
}

// BehaviorSummary provides a simplified view of a behavior.
type BehaviorSummary struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Kind       string                 `json:"kind"`
	Tier       string                 `json:"tier,omitempty"`
	Content    map[string]interface{} `json:"content"`
	Confidence float64                `json:"confidence"`
	When       map[string]interface{} `json:"when,omitempty"`
	Tags       []string               `json:"tags,omitempty"`
	Activation float64                `json:"activation,omitempty"`
	Distance   int                    `json:"distance,omitempty"`
	SeedSource string                 `json:"seed_source,omitempty"`
}

// FloopLearnInput defines the input for floop_learn tool.
type FloopLearnInput struct {
	Wrong     string   `json:"wrong,omitempty" jsonschema:"What the agent did (optional, stored as provenance only)"`
	Right     string   `json:"right" jsonschema:"What should have been done instead,required"`
	File      string   `json:"file,omitempty" jsonschema:"Relevant file path for context"`
	Task      string   `json:"task,omitempty" jsonschema:"Current task type for context"`
	Language  string   `json:"language,omitempty" jsonschema:"Programming language (e.g. 'go', 'python'). Overrides file extension inference"`
	AutoMerge bool     `json:"auto_merge,omitempty" jsonschema:"Enable automatic merging of duplicate behaviors (default: false)"`
	Tags      []string `json:"tags,omitempty" jsonschema:"Additional tags to apply to the behavior, merged with inferred tags (max 5)"`
}

// FloopLearnOutput defines the output for floop_learn tool.
type FloopLearnOutput struct {
	CorrectionID    string   `json:"correction_id" jsonschema:"ID of the captured correction"`
	BehaviorID      string   `json:"behavior_id" jsonschema:"ID of the extracted behavior"`
	Scope           string   `json:"scope" jsonschema:"Where the behavior was stored: 'local' (project-specific) or 'global' (universal)"`
	AutoAccepted    bool     `json:"auto_accepted" jsonschema:"Whether behavior was automatically accepted"`
	Confidence      float64  `json:"confidence" jsonschema:"Placement confidence (0.0-1.0)"`
	RequiresReview  bool     `json:"requires_review" jsonschema:"Whether behavior requires manual review"`
	ReviewReasons   []string `json:"review_reasons,omitempty" jsonschema:"Reasons why review is needed"`
	MergedIntoID    string   `json:"merged_into_id,omitempty" jsonschema:"ID of behavior this was merged into (if auto-merged)"`
	MergeSimilarity float64  `json:"merge_similarity,omitempty" jsonschema:"Similarity score with merged behavior (0.0-1.0)"`
	Message         string   `json:"message" jsonschema:"Human-readable result message"`
}

// FloopListInput defines the input for floop_list tool.
type FloopListInput struct {
	Corrections bool   `json:"corrections,omitempty" jsonschema:"List corrections instead of behaviors (default: false)"`
	Tag         string `json:"tag,omitempty" jsonschema:"Filter behaviors by tag (exact match)"`
}

// FloopListOutput defines the output for floop_list tool.
type FloopListOutput struct {
	Behaviors   []BehaviorListItem   `json:"behaviors,omitempty" jsonschema:"List of behaviors"`
	Corrections []CorrectionListItem `json:"corrections,omitempty" jsonschema:"List of corrections"`
	Count       int                  `json:"count" jsonschema:"Number of items"`
}

// BehaviorListItem provides a list view of a behavior.
type BehaviorListItem struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Confidence float64   `json:"confidence"`
	Tags       []string  `json:"tags,omitempty"`
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
	DryRun    bool    `json:"dry_run,omitempty" jsonschema:"If true, only report duplicates without merging (default: false)"`
	Threshold float64 `json:"threshold,omitempty" jsonschema:"Similarity threshold for duplicate detection (0.0-1.0, default: 0.9)"`
	Scope     string  `json:"scope,omitempty" jsonschema:"Scope of deduplication: 'local', 'global', or 'both' (default: 'both')"`
}

// FloopDeduplicateOutput defines the output for floop_deduplicate tool.
type FloopDeduplicateOutput struct {
	DuplicatesFound int                   `json:"duplicates_found" jsonschema:"Number of duplicate pairs found"`
	Merged          int                   `json:"merged" jsonschema:"Number of behaviors merged"`
	Results         []DeduplicationResult `json:"results,omitempty" jsonschema:"Details of each deduplication action"`
	Message         string                `json:"message" jsonschema:"Human-readable summary"`
}

// DeduplicationResult represents the outcome of deduplicating a single behavior.
type DeduplicationResult struct {
	BehaviorID   string  `json:"behavior_id" jsonschema:"ID of the behavior being deduplicated"`
	BehaviorName string  `json:"behavior_name" jsonschema:"Name of the behavior"`
	Action       string  `json:"action" jsonschema:"Action taken: 'skip', 'merge', or 'none'"`
	MatchID      string  `json:"match_id,omitempty" jsonschema:"ID of the matching duplicate (if found)"`
	MatchName    string  `json:"match_name,omitempty" jsonschema:"Name of the matching duplicate"`
	Similarity   float64 `json:"similarity,omitempty" jsonschema:"Similarity score (0.0-1.0)"`
	MergedID     string  `json:"merged_id,omitempty" jsonschema:"ID of the merged behavior (if merge performed)"`
	Error        string  `json:"error,omitempty" jsonschema:"Error message if operation failed"`
}

// FloopBackupInput defines the input for floop_backup tool.
type FloopBackupInput struct {
	OutputPath string `json:"output_path,omitempty" jsonschema:"Output file path (default: ~/.floop/backups/floop-backup-TIMESTAMP.json)"`
}

// FloopBackupOutput defines the output for floop_backup tool.
type FloopBackupOutput struct {
	Path          string            `json:"path" jsonschema:"Path to the backup file"`
	NodeCount     int               `json:"node_count" jsonschema:"Number of nodes backed up"`
	EdgeCount     int               `json:"edge_count" jsonschema:"Number of edges backed up"`
	Version       int               `json:"version" jsonschema:"Backup format version (1=JSON, 2=gzip+SHA-256)"`
	SchemaVersion int               `json:"schema_version" jsonschema:"Store schema version embedded in backup"`
	Compressed    bool              `json:"compressed" jsonschema:"Whether the backup is gzip compressed"`
	SizeBytes     int64             `json:"size_bytes" jsonschema:"Size of the backup file in bytes"`
	Metadata      map[string]string `json:"metadata,omitempty" jsonschema:"Backup metadata (floop_version, hostname, platform, schema)"`
	Message       string            `json:"message" jsonschema:"Human-readable result message"`
}

// FloopRestoreInput defines the input for floop_restore tool.
type FloopRestoreInput struct {
	InputPath string `json:"input_path" jsonschema:"Path to backup file to restore,required"`
	Mode      string `json:"mode,omitempty" jsonschema:"Restore mode: merge (skip existing, default) or replace (clear first)"`
}

// FloopRestoreOutput defines the output for floop_restore tool.
type FloopRestoreOutput struct {
	NodesRestored int    `json:"nodes_restored" jsonschema:"Number of nodes restored"`
	NodesSkipped  int    `json:"nodes_skipped" jsonschema:"Number of nodes skipped (merge mode)"`
	EdgesRestored int    `json:"edges_restored" jsonschema:"Number of edges restored"`
	EdgesSkipped  int    `json:"edges_skipped" jsonschema:"Number of edges skipped"`
	Message       string `json:"message" jsonschema:"Human-readable result message"`
}

// FloopConnectInput defines the input for floop_connect tool.
type FloopConnectInput struct {
	Source        string  `json:"source" jsonschema:"Source behavior ID,required"`
	Target        string  `json:"target" jsonschema:"Target behavior ID,required"`
	Kind          string  `json:"kind" jsonschema:"Edge type: requires, overrides, conflicts, similar-to, learned-from,required"`
	Weight        float64 `json:"weight,omitempty" jsonschema:"Edge weight (0.0-1.0, default 0.8)"`
	Bidirectional bool    `json:"bidirectional,omitempty" jsonschema:"Create edges in both directions (default: false)"`
}

// FloopConnectOutput defines the output for floop_connect tool.
type FloopConnectOutput struct {
	Source        string  `json:"source" jsonschema:"Source behavior ID"`
	Target        string  `json:"target" jsonschema:"Target behavior ID"`
	Kind          string  `json:"kind" jsonschema:"Edge type"`
	Weight        float64 `json:"weight" jsonschema:"Edge weight"`
	Bidirectional bool    `json:"bidirectional" jsonschema:"Whether reverse edge was also created"`
	Message       string  `json:"message" jsonschema:"Human-readable result message"`
}

// FloopGraphInput defines the input for floop_graph tool.
type FloopGraphInput struct {
	Format string `json:"format,omitempty" jsonschema:"Output format: dot, json, or html (default: json)"`
}

// FloopGraphOutput defines the output for floop_graph tool.
type FloopGraphOutput struct {
	Format    string      `json:"format" jsonschema:"Output format used"`
	Graph     interface{} `json:"graph" jsonschema:"Graph data (DOT string, JSON object, or HTML string)"`
	NodeCount int         `json:"node_count" jsonschema:"Number of nodes in graph"`
	EdgeCount int         `json:"edge_count" jsonschema:"Number of edges in graph"`
}

// FloopValidateInput defines the input for floop_validate tool.
type FloopValidateInput struct {
	// No required inputs - validates current store
}

// FloopValidateOutput defines the output for floop_validate tool.
type FloopValidateOutput struct {
	Valid      bool                    `json:"valid" jsonschema:"True if no validation errors found"`
	ErrorCount int                     `json:"error_count" jsonschema:"Number of validation errors found"`
	Errors     []ValidationErrorOutput `json:"errors,omitempty" jsonschema:"List of validation errors"`
	Message    string                  `json:"message" jsonschema:"Human-readable summary"`
}

// ValidationErrorOutput describes a single validation error.
type ValidationErrorOutput struct {
	BehaviorID string `json:"behavior_id" jsonschema:"ID of the behavior with the issue"`
	Field      string `json:"field" jsonschema:"Relationship field: requires, overrides, or conflicts"`
	RefID      string `json:"ref_id" jsonschema:"The problematic referenced ID"`
	Issue      string `json:"issue" jsonschema:"Issue type: dangling, cycle, or self-reference"`
}

// FloopFeedbackInput defines the input for floop_feedback tool.
type FloopFeedbackInput struct {
	BehaviorID string `json:"behavior_id" jsonschema:"ID of the behavior to provide feedback on,required"`
	Signal     string `json:"signal" jsonschema:"Feedback signal: confirmed (behavior was helpful) or overridden (behavior was contradicted),required"`
}

// FloopFeedbackOutput defines the output for floop_feedback tool.
type FloopFeedbackOutput struct {
	BehaviorID string `json:"behavior_id" jsonschema:"ID of the behavior"`
	Signal     string `json:"signal" jsonschema:"Feedback signal that was recorded"`
	Message    string `json:"message" jsonschema:"Human-readable result message"`
}

// FloopPackInstallInput defines the input for floop_pack_install tool.
type FloopPackInstallInput struct {
	Source   string `json:"source" jsonschema:"Pack source: local path, URL (https://...), or GitHub shorthand (gh:owner/repo[@version]),required"`
	FilePath string `json:"file_path,omitempty" jsonschema:"Deprecated: use source instead. Path to .fpack file to install"`
}

// FloopPackInstallOutput defines the output for floop_pack_install tool.
type FloopPackInstallOutput struct {
	PackID       string   `json:"pack_id" jsonschema:"Installed pack ID"`
	Version      string   `json:"version" jsonschema:"Installed pack version"`
	Added        []string `json:"added" jsonschema:"IDs of newly added behaviors"`
	Updated      []string `json:"updated" jsonschema:"IDs of upgraded behaviors"`
	Skipped      []string `json:"skipped" jsonschema:"IDs of skipped behaviors"`
	EdgesAdded   int      `json:"edges_added" jsonschema:"Number of edges added"`
	EdgesSkipped int      `json:"edges_skipped" jsonschema:"Number of edges skipped"`
	DerivedEdges int      `json:"derived_edges" jsonschema:"Number of edges automatically derived between pack and existing behaviors"`
	Message      string   `json:"message" jsonschema:"Human-readable result message"`
}
