// Package learning implements the correction-to-behavior learning loop.
package learning

import (
	"github.com/nvandessel/feedback-loop/internal/models"
)

// CorrectionCapture detects and structures correction events.
// It provides methods to capture corrections from various sources
// (CLI, conversation transcripts, etc.) and detect potential
// correction patterns in text.
type CorrectionCapture interface {
	// CaptureFromCLI creates a correction from CLI input.
	// This is the primary entry point when an agent self-reports a correction.
	CaptureFromCLI(wrong, right string, ctx models.ContextSnapshot) (*models.Correction, error)

	// MightBeCorrection checks if a message looks like a correction.
	// Returns true if the text contains correction signals like
	// "no,", "don't", "instead", etc.
	MightBeCorrection(text string) bool
}

// NewCorrectionCapture creates a new CorrectionCapture instance.
func NewCorrectionCapture() CorrectionCapture {
	// Stub - implementation will be provided by subagent
	panic("not implemented")
}
