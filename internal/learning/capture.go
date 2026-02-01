// Package learning implements the correction-to-behavior learning loop.
package learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
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

// correctionCapture is the concrete implementation of CorrectionCapture.
// It detects and structures correction events from various sources.
type correctionCapture struct {
	// correctionSignals contains phrases that often indicate a correction
	correctionSignals []string
}

// NewCorrectionCapture creates a new CorrectionCapture instance with
// default correction signal phrases.
func NewCorrectionCapture() CorrectionCapture {
	return &correctionCapture{
		correctionSignals: []string{
			"no,", "don't", "instead", "actually,", "not like that",
			"that's wrong", "that's not right", "shouldn't",
			"prefer", "better to", "rather than", "use this instead",
			"that's incorrect", "please use", "you should",
		},
	}
}

// CaptureFromCLI creates a correction from CLI input.
// This is the primary entry point when an agent self-reports a correction.
// The correction ID is a content-addressed hash based on the wrong and right inputs.
func (c *correctionCapture) CaptureFromCLI(wrong, right string, ctx models.ContextSnapshot) (*models.Correction, error) {
	id := c.generateID(wrong, right)

	return &models.Correction{
		ID:              id,
		Timestamp:       time.Now(),
		Context:         ctx,
		AgentAction:     wrong,
		HumanResponse:   "", // Not captured in CLI mode
		CorrectedAction: right,
		ConversationID:  "", // Could be passed from agent
		TurnNumber:      0,
		Corrector:       ctx.User,
		Processed:       false,
	}, nil
}

// MightBeCorrection checks if a message looks like a correction.
// Returns true if the text contains any of the correction signal phrases.
func (c *correctionCapture) MightBeCorrection(text string) bool {
	lower := strings.ToLower(text)
	for _, signal := range c.correctionSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// generateID creates a content-addressed hash ID for a correction.
// The ID is based on the first MaxCorrectionPreviewLen characters of both wrong and right strings.
func (c *correctionCapture) generateID(wrong, right string) string {
	wrongPart := wrong
	if len(wrong) > constants.MaxCorrectionPreviewLen {
		wrongPart = wrong[:constants.MaxCorrectionPreviewLen]
	}
	rightPart := right
	if len(right) > constants.MaxCorrectionPreviewLen {
		rightPart = right[:constants.MaxCorrectionPreviewLen]
	}
	content := wrongPart + rightPart
	hash := sha256.Sum256([]byte(content))
	return "correction-" + hex.EncodeToString(hash[:])[:12]
}
