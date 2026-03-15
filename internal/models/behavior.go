package models

import (
	"time"

	"github.com/nvandessel/floop/internal/store"
)

// BehaviorKind categorizes what type of behavioral guidance this is
type BehaviorKind string

const (
	BehaviorKindDirective  BehaviorKind = "directive"  // Do X
	BehaviorKindConstraint BehaviorKind = "constraint" // Never do Y
	BehaviorKindProcedure  BehaviorKind = "procedure"  // Multi-step process
	BehaviorKindPreference BehaviorKind = "preference" // Prefer X over Y
	BehaviorKindEpisodic   BehaviorKind = "episodic"   // Record of a specific event or session
	BehaviorKindWorkflow   BehaviorKind = "workflow"   // Multi-step workflow with conditions
)

// Behavior status kinds represent lifecycle states set by curation commands.
// Values are defined in internal/store as NodeKind constants.
const (
	BehaviorKindForgotten  BehaviorKind = BehaviorKind(store.NodeKindForgotten)
	BehaviorKindDeprecated BehaviorKind = BehaviorKind(store.NodeKindDeprecated)
	BehaviorKindMerged     BehaviorKind = BehaviorKind(store.NodeKindMerged)
)

// MemoryType classifies behaviors by cognitive category.
type MemoryType string

// Memory type constants for classifying behaviors by cognitive category
const (
	MemoryTypeSemantic   MemoryType = "semantic"
	MemoryTypeEpisodic   MemoryType = "episodic"
	MemoryTypeProcedural MemoryType = "procedural"
)

// MemoryTypeForKind returns the memory type classification for a given BehaviorKind.
func MemoryTypeForKind(kind BehaviorKind) MemoryType {
	switch kind {
	case BehaviorKindEpisodic:
		return MemoryTypeEpisodic
	case BehaviorKindProcedure, BehaviorKindWorkflow:
		return MemoryTypeProcedural
	default:
		return MemoryTypeSemantic
	}
}

// EpisodeData holds type-specific data for episodic behaviors.
type EpisodeData struct {
	SessionID string   `json:"session_id"`
	Timeframe string   `json:"timeframe"`
	Actors    []string `json:"actors"`
	Outcome   string   `json:"outcome"`
}

// WorkflowData holds type-specific data for workflow behaviors.
type WorkflowData struct {
	Steps    []WorkflowStep `json:"steps"`
	Trigger  string         `json:"trigger"`
	Verified bool           `json:"verified"`
}

// WorkflowStep represents a single step in a workflow.
type WorkflowStep struct {
	Action    string `json:"action"`
	Condition string `json:"condition,omitempty"`
	OnFailure string `json:"on_failure,omitempty"`
}

// BehaviorContent holds multiple representations of the behavior's content
type BehaviorContent struct {
	// Canonical is the minimal representation, optimized for token efficiency
	Canonical string `json:"canonical" yaml:"canonical"`

	// Summary is an ultra-compressed single-line reminder (~60 chars)
	// Used for tiered injection when token budget is constrained
	Summary string `json:"summary,omitempty" yaml:"summary,omitempty"`

	// Tags are keyword tags for clustering and categorization
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty"`

	// Structured holds key-value data when the behavior has clear structure
	// e.g., {"prefer": "pathlib.Path"}
	Structured map[string]interface{} `json:"structured,omitempty" yaml:"structured,omitempty"`
}

// Behavior represents a unit of agent behavior
type Behavior struct {
	// Identity
	ID   string       `json:"id" yaml:"id"`
	Name string       `json:"name" yaml:"name"`
	Kind BehaviorKind `json:"kind" yaml:"kind"`

	// Activation - when does this behavior apply?
	// Keys are context fields, values are required values
	// e.g., {"language": "python", "task": ["refactor", "write"]}
	When map[string]interface{} `json:"when,omitempty" yaml:"when,omitempty"`

	// Content
	Content BehaviorContent `json:"content" yaml:"content"`

	// Provenance - where did this come from?
	Provenance Provenance `json:"provenance" yaml:"provenance"`

	// Memory consolidation fields (V9)
	MemoryType   MemoryType    `json:"memory_type,omitempty" yaml:"memory_type,omitempty"`
	EpisodeData  *EpisodeData  `json:"episode_data,omitempty" yaml:"episode_data,omitempty"`
	WorkflowData *WorkflowData `json:"workflow_data,omitempty" yaml:"workflow_data,omitempty"`

	// Confidence score (0.0 - 1.0)
	// Learned behaviors start lower, increase with successful application
	Confidence float64 `json:"confidence" yaml:"confidence"`

	// Priority for conflict resolution (higher wins)
	Priority int `json:"priority" yaml:"priority"`

	// Graph relationships (IDs of other behaviors)
	Requires  []string         `json:"requires,omitempty" yaml:"requires,omitempty"`   // Hard dependencies
	Overrides []string         `json:"overrides,omitempty" yaml:"overrides,omitempty"` // This supersedes those
	Conflicts []string         `json:"conflicts,omitempty" yaml:"conflicts,omitempty"` // Mutual exclusion
	SimilarTo []SimilarityLink `json:"similar_to,omitempty" yaml:"similar_to,omitempty"`

	// Statistics (updated over time)
	Stats BehaviorStats `json:"stats" yaml:"stats"`
}

// SimilarityLink represents a similarity relationship with a score
type SimilarityLink struct {
	ID    string  `json:"id" yaml:"id"`
	Score float64 `json:"score" yaml:"score"`
}

// BehaviorStats tracks usage statistics
type BehaviorStats struct {
	TimesActivated  int        `json:"times_activated" yaml:"times_activated"`
	TimesFollowed   int        `json:"times_followed" yaml:"times_followed"`
	TimesOverridden int        `json:"times_overridden" yaml:"times_overridden"`
	TimesConfirmed  int        `json:"times_confirmed" yaml:"times_confirmed"` // Positive signal when behavior was followed
	LastActivated   *time.Time `json:"last_activated,omitempty" yaml:"last_activated,omitempty"`
	LastConfirmed   *time.Time `json:"last_confirmed,omitempty" yaml:"last_confirmed,omitempty"` // Last time behavior was positively confirmed
	CreatedAt       time.Time  `json:"created_at" yaml:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" yaml:"updated_at"`
}
