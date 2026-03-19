package consolidation

import (
	"github.com/nvandessel/floop/internal/llm"
	"github.com/nvandessel/floop/internal/logging"
)

// NewConsolidator creates a Consolidator for the given executor type.
// Supported executors: "llm", "local" (future), "heuristic" (default).
// Unknown executor strings fall back to heuristic.
// The model parameter identifies the LLM model for decision logging and run persistence.
func NewConsolidator(executor string, client llm.Client, decisions *logging.DecisionLogger, model string) Consolidator {
	switch executor {
	case "llm":
		if client == nil {
			return NewHeuristicConsolidator()
		}
		cfg := DefaultLLMConsolidatorConfig()
		cfg.Model = model
		return NewLLMConsolidator(client, decisions, cfg)
	case "local":
		// v2, not yet implemented — fall back to heuristic
		return NewHeuristicConsolidator()
	default:
		return NewHeuristicConsolidator()
	}
}
