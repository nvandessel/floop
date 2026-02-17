package models

import (
	"github.com/nvandessel/feedback-loop/internal/constants"
)

// localScopeKeys are When condition keys that indicate project-specific behaviors.
// Only file_path triggers local scope since it implies project directory structure.
// Environment was removed because "environment: development" is universal, not project-specific,
// and caused 95%+ of behaviors to be mis-routed to local scope.
var localScopeKeys = []string{"file_path"}

// ClassifyScope determines whether a behavior should be stored locally or globally
// based on its When conditions. Behaviors with project-specific conditions (file_path
// or environment) are local; everything else (language-only, task-only, empty) is global.
func ClassifyScope(behavior *Behavior) constants.Scope {
	if behavior.When == nil {
		return constants.ScopeGlobal
	}
	for _, key := range localScopeKeys {
		if _, ok := behavior.When[key]; ok {
			return constants.ScopeLocal
		}
	}
	return constants.ScopeGlobal
}
