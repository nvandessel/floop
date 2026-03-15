package models

import (
	"time"
)

// SourceType indicates where a behavior came from
type SourceType string

const (
	SourceTypeAuthored     SourceType = "authored"     // Human wrote it directly
	SourceTypeLearned      SourceType = "learned"      // Extracted from a correction
	SourceTypeImported     SourceType = "imported"     // From an external package
	SourceTypeConsolidated SourceType = "consolidated" // Consolidated from multiple events
)

// Provenance tracks where a behavior came from
type Provenance struct {
	SourceType SourceType `json:"source_type" yaml:"source_type"`
	CreatedAt  time.Time  `json:"created_at" yaml:"created_at"`

	// For authored behaviors
	Author string `json:"author,omitempty" yaml:"author,omitempty"`

	// For learned behaviors
	CorrectionID string `json:"correction_id,omitempty" yaml:"correction_id,omitempty"`

	// For imported behaviors
	Package        string `json:"package,omitempty" yaml:"package,omitempty"`
	PackageVersion string `json:"package_version,omitempty" yaml:"package_version,omitempty"`

	// Consolidation lineage
	ConsolidatedBy string     `json:"consolidated_by,omitempty" yaml:"consolidated_by,omitempty"`
	ConsolidatedAt *time.Time `json:"consolidated_at,omitempty" yaml:"consolidated_at,omitempty"`
	SourceEvents   []string   `json:"source_events,omitempty" yaml:"source_events,omitempty"`
	// Confidence is the consolidator's confidence at extraction time (snapshot).
	// Distinct from Behavior.Confidence which evolves via activation/confirmation.
	Confidence float64 `json:"confidence,omitempty" yaml:"confidence,omitempty"`

	// Agent provenance (optional)
	SourceModel   string `json:"source_model,omitempty" yaml:"source_model,omitempty"`
	SourceAgent   string `json:"source_agent,omitempty" yaml:"source_agent,omitempty"`
	SourceProject string `json:"source_project,omitempty" yaml:"source_project,omitempty"`
	SourceBranch  string `json:"source_branch,omitempty" yaml:"source_branch,omitempty"`
}
