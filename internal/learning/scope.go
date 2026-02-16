package learning

import (
	"github.com/nvandessel/feedback-loop/internal/constants"
	"github.com/nvandessel/feedback-loop/internal/models"
)

// ClassifyScope determines whether a behavior should be stored locally or globally.
// Delegates to models.ClassifyScope; kept here for backward compatibility with
// existing callers in the learning package.
func ClassifyScope(behavior *models.Behavior) constants.Scope {
	return models.ClassifyScope(behavior)
}
