// Package seed provides pre-seeded meta-behaviors for bootstrapping floop.
package seed

import (
	"github.com/nvandessel/floop/internal/store"
)

// SeedVersion is the version of the seed behavior definitions.
// Bump this when seed content changes to trigger updates.
const SeedVersion = "0.4.0"

// coreBehaviors returns the seed behaviors that bootstrap floop's self-teaching.
func coreBehaviors() []store.Node {
	return []store.Node{
		{
			ID:   "seed-capture-corrections",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/capture-corrections-proactively",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "When corrected or when you discover insights, immediately call floop_learn(wrong='what happened', right='what to do instead'). Capture learnings proactively without waiting for permission.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   100,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
		{
			ID:   "seed-know-floop-tools",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/know-your-floop-tools",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "You have persistent memory via floop. Use floop_active to check active behaviors for context, floop_learn to capture corrections and insights, and floop_list to see all stored behaviors.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   100,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
		{
			ID:   "seed-use-floop-active",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/use-floop-active",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "Call floop_active with file and task parameters to get context-aware behaviors. This returns only the behaviors relevant to your current file and task type, rather than all stored behaviors.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   90,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
		{
			ID:   "seed-feedback-signals",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/feedback-signals",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "Use floop_feedback with signal 'confirmed' when a behavior was helpful, or 'overridden' when a behavior was contradicted. This trains confidence scores so useful behaviors surface more often and unhelpful ones fade.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   90,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
		{
			ID:   "seed-session-hooks",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/session-hooks",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "Floop uses session hooks configured via 'floop init' to auto-inject active behaviors at session start. Hooks ensure your agent always has its learned behaviors available without manual loading.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   90,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
		{
			ID:   "seed-behavior-lifecycle",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/behavior-lifecycle",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "Behaviors follow a lifecycle: learned (newly captured) -> confirmed (validated through repeated use) -> deprecated or forgotten (no longer relevant). Confidence scores reflect this progression. Forgotten behaviors are never re-injected.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   90,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
		{
			ID:   "seed-agents-md-setup",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/agents-md-setup",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "Projects need an AGENTS.md file referencing floop for agent integration. Use 'floop init --project' to set this up automatically. AGENTS.md tells agents about floop's MCP tools and learned behaviors.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   90,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
		{
			ID:   "seed-graph-hygiene",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/graph-hygiene",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "Run floop_deduplicate, derive-edges, and floop_validate periodically to keep the behavior graph clean. Deduplication merges similar behaviors, edge derivation discovers relationships, and validation catches consistency issues.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   90,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
		{
			ID:   "seed-skill-packs",
			Kind: store.NodeKindBehavior,
			Content: map[string]interface{}{
				"name": "core/skill-packs",
				"kind": "directive",
				"content": map[string]interface{}{
					"canonical": "Skill packs are portable behavior collections that can be created, shared, and installed. Use 'floop pack create' to bundle behaviors into a shareable pack, and 'floop pack install' to add a pack's behaviors to your store.",
				},
			},
			Metadata: map[string]interface{}{
				"confidence": 1.0,
				"priority":   90,
				"provenance": map[string]interface{}{
					"source_type":     "imported",
					"package":         "floop/core",
					"package_version": SeedVersion,
				},
			},
		},
	}
}
