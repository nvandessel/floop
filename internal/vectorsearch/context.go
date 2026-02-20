package vectorsearch

import (
	"path/filepath"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// ComposeContextQuery concatenates non-empty context fields into an embeddable text string.
// Used to create a query vector from the current ContextSnapshot.
func ComposeContextQuery(ctx models.ContextSnapshot) string {
	var parts []string

	// Add task if present (e.g., "development", "testing")
	if ctx.Task != "" {
		parts = append(parts, ctx.Task)
	}

	// Add language if present
	if ctx.FileLanguage != "" {
		parts = append(parts, ctx.FileLanguage)
	}

	// Add file info if present (just the filename, not full path)
	if ctx.FilePath != "" {
		parts = append(parts, "editing "+filepath.Base(ctx.FilePath))
	}

	// Add project type if present and not unknown
	if ctx.ProjectType != "" && ctx.ProjectType != models.ProjectTypeUnknown {
		parts = append(parts, "in a "+string(ctx.ProjectType)+" project")
	}

	// Add environment if present
	if ctx.Environment != "" {
		parts = append(parts, ctx.Environment+" environment")
	}

	if len(parts) == 0 {
		return "general software development"
	}

	return strings.Join(parts, " ")
}
