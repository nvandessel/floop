package models

import (
	"time"
)

// SourceType indicates where a behavior came from
type SourceType string

const (
	SourceTypeAuthored SourceType = "authored" // Human wrote it directly
	SourceTypeLearned  SourceType = "learned"  // Extracted from a correction
	SourceTypeImported SourceType = "imported" // From an external package
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
}
