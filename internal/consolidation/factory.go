package consolidation

import (
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/logging"
)

// NewConsolidator creates a Consolidator for the given executor type.
// Supported executors: "llm", "local" (future), "heuristic" (default).
// Unknown executor strings fall back to heuristic.
func NewConsolidator(executor string, client llm.Client, decisions *logging.DecisionLogger) Consolidator {
	switch executor {
	case "llm":
		return NewLLMConsolidator(client, decisions, DefaultLLMConsolidatorConfig())
	case "local":
		// v2, not yet implemented — fall back to heuristic
		return NewHeuristicConsolidator()
	default:
		return NewHeuristicConsolidator()
	}
}
