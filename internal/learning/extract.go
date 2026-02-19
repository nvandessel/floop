package learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/models"
	"github.com/nvandessel/feedback-loop/internal/sanitize"
	"github.com/nvandessel/feedback-loop/internal/tagging"
)

// BehaviorExtractor transforms corrections into candidate behaviors.
// It analyzes the correction content to infer activation conditions,
// behavior kind, and content structure.
type BehaviorExtractor interface {
	// Extract creates a candidate behavior from a correction.
	// The extracted behavior includes:
	// - Inferred 'when' conditions based on correction context
	// - Behavior kind (directive, constraint, preference, procedure)
	// - Structured content with avoid/prefer patterns
	// - Provenance linking back to the source correction
	Extract(correction models.Correction) (*models.Behavior, error)
}

// behaviorExtractor is the concrete implementation of BehaviorExtractor.
type behaviorExtractor struct {
	// constraintSignals are keywords that indicate a constraint behavior
	constraintSignals []string
	// preferenceSignals are keywords that indicate a preference behavior
	preferenceSignals []string
	// procedureSignals are keywords that indicate a procedure behavior
	procedureSignals []string
	// tagDict maps keywords to normalized tags for semantic feature extraction
	tagDict *tagging.Dictionary
}

// NewBehaviorExtractor creates a new BehaviorExtractor instance.
func NewBehaviorExtractor() BehaviorExtractor {
	return &behaviorExtractor{
		constraintSignals: []string{
			"never", "don't", "do not", "must not", "mustn't",
			"forbidden", "prohibited", "avoid", "stop",
		},
		preferenceSignals: []string{
			"prefer", "instead of", "rather than", "better to",
			"use x over y", "favor", "prioritize",
		},
		procedureSignals: []string{
			"first", "then", "after that", "finally",
			"step 1", "step 2", "workflow", "process",
		},
		tagDict: tagging.NewDictionary(),
	}
}

// Extract creates a candidate behavior from a correction.
func (e *behaviorExtractor) Extract(correction models.Correction) (*models.Behavior, error) {
	// Generate content-addressed ID
	id := e.generateID(correction)

	// Infer the 'when' predicate from context
	when := e.inferWhen(correction.Context)

	// Determine behavior kind
	kind := e.inferKind(correction)

	// Build content with avoid/prefer patterns
	content := e.buildContent(correction)

	// Build provenance linking to the source correction
	provenance := models.Provenance{
		SourceType:   models.SourceTypeLearned,
		CreatedAt:    time.Now(),
		CorrectionID: correction.ID,
	}

	// Generate a human-readable name
	name := e.generateName(correction)

	return &models.Behavior{
		ID:         id,
		Name:       name,
		Kind:       kind,
		When:       when,
		Content:    content,
		Provenance: provenance,
		Confidence: constants.DefaultLearnedConfidence,
		Priority:   0,
		Stats: models.BehaviorStats{
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}, nil
}

// generateID creates a content-addressed hash ID for the behavior.
// The ID is deterministic based on the correction content.
func (e *behaviorExtractor) generateID(correction models.Correction) string {
	// Combine the key fields that define this behavior
	content := correction.AgentAction + correction.CorrectedAction
	hash := sha256.Sum256([]byte(content))
	return "behavior-" + hex.EncodeToString(hash[:])[:12]
}

// inferWhen creates a 'when' predicate from the correction context.
// It extracts relevant context fields that should trigger this behavior.
func (e *behaviorExtractor) inferWhen(ctx models.ContextSnapshot) map[string]interface{} {
	when := make(map[string]interface{})

	// Include language if present
	if ctx.FileLanguage != "" {
		when["language"] = ctx.FileLanguage
	}

	// Include file pattern if we can generalize it
	if ctx.FilePath != "" {
		pattern := e.generalizeFilePath(ctx.FilePath)
		if pattern != "" {
			when["file_path"] = pattern
		}
	}

	// Include task if present and in the known vocabulary
	if ctx.Task != "" && constants.KnownTasks[ctx.Task] {
		when["task"] = ctx.Task
	}

	return when
}

// generalizeFilePath extracts a meaningful pattern from a file path.
// For example, "src/db/migrations/001.go" -> "db/*"
func (e *behaviorExtractor) generalizeFilePath(filePath string) string {
	parts := strings.Split(filePath, "/")
	if len(parts) <= 1 {
		// Just a filename, return empty (will use language instead)
		return ""
	}

	// Find first significant directory (skip common roots)
	skipDirs := map[string]bool{
		"":         true,
		".":        true,
		"src":      true,
		"lib":      true,
		"pkg":      true,
		"app":      true,
		"home":     true,
		"usr":      true,
		"var":      true,
		"internal": true,
	}

	for _, part := range parts {
		if !skipDirs[part] {
			return part + "/*"
		}
	}

	// Fallback: use the parent directory
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/*"
	}

	return ""
}

// inferKind determines the behavior kind from the correction text.
// It analyzes both the agent action and corrected action for signals.
func (e *behaviorExtractor) inferKind(correction models.Correction) models.BehaviorKind {
	lowerCorrected := strings.ToLower(correction.CorrectedAction)
	lowerAgent := strings.ToLower(correction.AgentAction)

	// Check for constraint signals (highest priority)
	for _, signal := range e.constraintSignals {
		if strings.Contains(lowerCorrected, signal) {
			return models.BehaviorKindConstraint
		}
	}

	// Check for procedure signals
	for _, signal := range e.procedureSignals {
		if strings.Contains(lowerCorrected, signal) {
			return models.BehaviorKindProcedure
		}
	}

	// Check for preference signals
	for _, signal := range e.preferenceSignals {
		if strings.Contains(lowerCorrected, signal) {
			return models.BehaviorKindPreference
		}
	}

	// If both agent action and corrected action are present,
	// this is likely a preference (do Y instead of X)
	if correction.AgentAction != "" && correction.CorrectedAction != "" {
		// Check if this looks like an explicit alternative
		if strings.Contains(lowerCorrected, "instead") ||
			strings.Contains(lowerCorrected, "use") ||
			strings.Contains(lowerAgent, "instead") {
			return models.BehaviorKindPreference
		}
	}

	// Default to directive
	return models.BehaviorKindDirective
}

// buildContent creates the BehaviorContent with canonical text and structured patterns.
// All user-supplied content is sanitized to prevent stored prompt injection.
func (e *behaviorExtractor) buildContent(correction models.Correction) models.BehaviorContent {
	// Sanitize user-supplied inputs before building content
	sanitizedCorrected := sanitize.SanitizeBehaviorContent(correction.CorrectedAction)
	sanitizedAgent := sanitize.SanitizeBehaviorContent(correction.AgentAction)

	content := models.BehaviorContent{
		Canonical:  sanitizedCorrected,
		Tags:       tagging.ExtractTags(sanitizedCorrected, e.tagDict),
		Structured: make(map[string]interface{}),
	}

	// Add avoid/prefer patterns
	if sanitizedAgent != "" {
		content.Structured["avoid"] = sanitizedAgent
	}
	content.Structured["prefer"] = sanitizedCorrected

	// Build expanded version with context
	var expanded strings.Builder
	if sanitizedAgent != "" {
		expanded.WriteString("When working on this type of task, ")
		expanded.WriteString("avoid: ")
		expanded.WriteString(sanitizedAgent)
		expanded.WriteString("\n\n")
	}
	expanded.WriteString("Instead: ")
	expanded.WriteString(sanitizedCorrected)

	content.Expanded = expanded.String()

	return content
}

// generateName creates a human-readable name for the behavior.
// The name is a slug-ified version of the corrected action.
func (e *behaviorExtractor) generateName(correction models.Correction) string {
	// Start with the corrected action
	name := correction.CorrectedAction

	// Truncate to reasonable length
	if len(name) > constants.MaxBehaviorNameLen {
		name = name[:constants.MaxBehaviorNameLen]
	}

	// Clean up the name
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\r", " ")
	name = strings.ReplaceAll(name, "\t", " ")

	// Collapse multiple spaces
	for strings.Contains(name, "  ") {
		name = strings.ReplaceAll(name, "  ", " ")
	}

	name = strings.TrimSpace(name)

	// Convert to slug format
	name = strings.ToLower(name)

	// Replace spaces and special characters with hyphens
	replacer := strings.NewReplacer(
		" ", "-",
		"'", "",
		"\"", "",
		".", "-",
		",", "",
		":", "",
		";", "",
		"/", "-",
		"\\", "-",
		"(", "",
		")", "",
		"[", "",
		"]", "",
		"{", "",
		"}", "",
	)
	name = replacer.Replace(name)

	// Remove consecutive hyphens
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}

	// Trim leading/trailing hyphens
	name = strings.Trim(name, "-")

	// Prefix with learned/ to indicate origin
	name = "learned/" + name

	// Apply SanitizeBehaviorName as a final safety pass to ensure the name
	// only contains allowed characters after all transformations.
	name = sanitize.SanitizeBehaviorName(name)

	return name
}
