# The floop Origin Story

## The Itch

January 2026. I'd been spending a lot of time with AI coding agents — Claude Code, mostly — and the thing that kept bugging me was that they didn't remember. You'd correct an agent, it'd get it right for the rest of the session, and then next session it's back to square one. Same mistake, same correction, over and over.

There are ways to deal with this. You can put rules in an `AGENTS.md` file, or split them across multiple markdown files, but it's manual — you have to remember to add things, keep them up to date, and they just grow into these unwieldy walls of text. Memory tools exist for storing facts and preferences, which is great, but facts aren't really behaviors. Knowing "the user prefers tabs" is different from knowing "when writing Go tests, use table-driven tests with `t.Run()`." At least that's how I felt, whether that's true or not is a philosphical discussion.

I hadn't tried many of these tools if I'm being honest. They intruiged me, but something felt missing. This wasn't a gap analysis — it was more of a feeling. There was a disconnect between the corrections I was giving and the agent's ability to internalize them. It just had me thinking: what if corrections could become durable, context-aware behaviors that the agent actually carries forward?

That idea excited me enough to start building.

## The Graph

The thinking started taking shape around [Steve Yegge's Beads](https://github.com/steveyegge/beads) — a graph-structured issue tracking system. Seeing behaviors and corrections as nodes in a graph, connected by typed relationships (similar-to, learned-from, requires, conflicts) — that felt right. Not a flat list of rules, but a network where the connections carry meaning.

The first commit landed on January 25, 2026. Core models, a graph store, a CLI skeleton, and a learning pipeline came together within the first week. I started dogfooding immediately — floop was being built with floop, and every correction I gave the agents building it became a behavior in the system they were building.

## The Blast Radius

About ten days in, the basic system worked. Behaviors were being captured, stored, and injected into agent prompts. But the injection was static — every session got the full set of behaviors dumped into the context window. With 38 behaviors it was fine, but it wouldn't scale.

Around the same time, I'd been looking at AI code review tools. [CodeRabbit](https://coderabbit.ai/) and [Greptile](https://greptile.com/) both have this concept of a "blast radius" — when reviewing a diff, they don't just look at the changed lines, they pull in surrounding code and related context to understand the full impact. That got me thinking: what if triggered behaviors had a similar blast radius? When one behavior fires, what if it also pulls in related behaviors that might be relevant, even if they're not a direct match?

Then one afternoon while talking with Claude about these ideas, it found something that cristalized things. The [SYNAPSE paper](https://arxiv.org/abs/2601.02744) (published January 6, 2026). It showed that spreading activation — a decades-old theory from cognitive science about how the brain retrieves memories — could be applied to LLM agent memory, with 95% token reduction while maintaining accuracy. The key insight: you don't need to load everything, you just need to activate the right things, and activation propagates outward through associations.

The blast radius idea from the code review tools and the spreading activation model from cognitive science were basically the same concept from different angles. I was shocked and excited. The fact these ideas I had were rooted in cognitive science was facinating. 

The mapping to floop fell out naturally:

- **Corrections** as episodic nodes (specific interaction memories)
- **Behaviors** as semantic nodes (abstract knowledge extracted from episodes)
- **Edges** as association links (similar-to, learned-from, requires, conflicts)
- **Activation** as context-relevant retrieval — energy propagating from seed nodes through the graph, decaying with distance

The behavior graph wasn't just storage anymore. It was an associative network where context triggers cascading activation, and related behaviors get pulled in alongside direct matches — a blast radius of relevant context.

## Why This Excites Me

What I find exciting about AI agents isn't that they write code — it's that they can adopt and enforce the same design principles I care about: SOLID principles, good ole OOP, clean interfaces that enable real and valid tests that provide value. They do this at a pace I never could on my own. The ideas in my head, produced consistently. But that only works if the agent has the right context. A few more guardrails, the right behaviors loaded at the right time, something closer to how we actually think — with associations and related context, not just keyword matches — that's what levels up agents from "helpful autocomplete" to genuine collaborators. That's what I'm exploring here.

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
