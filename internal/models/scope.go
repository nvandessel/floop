package models

import (
	"github.com/nvandessel/feedback-loop/internal/constants"
)

// localScopeKeys are When condition keys that indicate project-specific behaviors.
// file_path implies project directory structure; environment implies deployment config.
var localScopeKeys = []string{"file_path", "environment"}

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
