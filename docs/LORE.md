# The floop Origin Story

## The Problem

January 2026. AI coding agents were everywhere — Claude Code, Cursor, Copilot — but they all had the same maddening flaw: they didn't remember. You'd correct an agent's behavior, and it would stick for the rest of the session. Next session? Gone. Same mistake, same correction, forever.

The workarounds were crude. You could dump rules into an `AGENTS.md` file, but that was manual, static, and grew into an unreadable wall of text. Memory tools like Mem0 stored facts, but facts aren't behaviors — knowing "the user prefers tabs" is different from knowing "when writing Go tests, use table-driven tests with `t.Run()`." And none of them could distinguish which rules mattered *right now* versus rules for a completely different part of the codebase.

The question was: what if agents could actually learn from corrections and apply those lessons in the right context?

## The Spark

The idea crystallized around [Steve Yegge's Beads](https://github.com/steveyegge/beads) — a graph-structured issue tracking system. Beads used nodes and edges to represent relationships between code concepts, and seeing that graph structure clicked something into place. What if corrections and behaviors were nodes in a graph too? Not a flat list of rules, but a connected network where relationships between behaviors carried meaning?

The initial plan was even to use Beads as a backend. That pivot came quickly — SQLite and JSONL were simpler and more portable — but the graph metaphor stuck. Behaviors as nodes. Relationships (similar-to, learned-from, requires, conflicts) as edges. The shape of the data was a graph whether or not the storage engine was.

On January 25, 2026, the first commit landed. The project moved fast — core models, a graph store, a CLI skeleton, and a learning pipeline (capture correction, extract behavior, place in graph) all came together within the first week. The dogfooding loop started immediately: floop was being built with floop. Every correction made to the agents building it became a behavior in the system they were building.

## The Aha Moment

Ten days in, the system worked. Behaviors were being captured, stored, and injected into agent prompts. But the injection was static — every session got the full set of behaviors, front-loaded into the context window. It worked, but it was brute force. With 38 behaviors it was fine. With 200? 500? The token budget would explode.

The first hint of a better approach came from researching AI code review tools. [CodeRabbit](https://coderabbit.ai/) and [Greptile](https://greptile.com/) both had this concept of a "blast radius" — when reviewing a diff, they didn't just look at the changed lines, they pulled in surrounding code and related context to understand the full impact. That made something click: what if triggered behaviors had a similar blast radius? When one behavior fires, what if it also pulled in related behaviors that might be relevant, even if they weren't a direct match for the current context?

Then, right around the same time, the [SYNAPSE paper](https://arxiv.org/abs/2601.02744) dropped (published January 6, 2026). SYNAPSE demonstrated that spreading activation — a decades-old theory from cognitive science about how the brain retrieves memories — could be applied to LLM agent memory. Their results showed 95% token reduction while maintaining higher accuracy than full-context methods. The key insight: you don't need to load everything, you just need to activate the right things — and activation propagates outward through associations, like a blast radius through a graph.

The blast radius concept from the code review tools and the spreading activation model from cognitive science snapped together — the same idea from two different angles.

The mapping to floop was immediate:

- **Corrections** were episodic nodes (specific interaction memories)
- **Behaviors** were semantic nodes (abstract knowledge extracted from episodes)
- **Similar-to edges** were association links (already in the graph)
- **Learned-from edges** were abstraction links (correction → behavior derivation)
- **Activation** was context-relevant retrieval — energy propagating from seed nodes through the graph, decaying with distance, until only the most relevant behaviors were lit up

This was a model change. The behavior graph wasn't just storage anymore — it was a brain-like associative network where context triggered cascading activation, and the most relevant behaviors naturally floated to the top.

Within three days, the spreading activation engine was implemented: seed selection, energy propagation, lateral inhibition (where strongly activated nodes suppress weaker competitors, just like neurons do), and a hybrid scoring function combining context relevance, activation level, and PageRank centrality. The blast radius was built into the graph itself — when a behavior activates, energy propagates outward through its connections, lighting up related behaviors that provide useful context even if they weren't direct matches.

## The Name

"feedback-loop" — or "floop" — because that's what it is. You correct the agent, the correction becomes a behavior, the behavior activates in the right context, the agent does it right the next time. A feedback loop that actually closes.

## What's in the `.floop/` Directory

This repository dogfoods itself. The `.floop/nodes.jsonl` and `.floop/edges.jsonl` files contain 38 real behaviors learned from building floop — things like "always follow TDD workflow: RED → GREEN → REFACTOR", "use floop MCP tools instead of bash commands", and "create separate PRs for each independent workstream." They're kept in version control intentionally as a showcase of what a real floop behavior store looks like.

## Acknowledgments

- **Steve Yegge** and [Beads](https://github.com/steveyegge/beads) — the graph-structured thinking that started it all
- **CodeRabbit** and **Greptile** — the "blast radius" concept from AI code review that sparked the idea of associative context
- **SYNAPSE** ([arxiv 2601.02744](https://arxiv.org/abs/2601.02744)) — the paper that showed spreading activation works for LLM memory
- **Collins & Loftus (1975)** — the original spreading activation theory for semantic memory
- **John Anderson's ACT-R** — the cognitive architecture that formalized activation-based retrieval
