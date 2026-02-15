# Research & Theory

This document covers the cognitive science and research behind floop's design. The spreading activation system isn't a metaphor — it's a direct implementation of memory retrieval models from decades of cognitive science research, adapted for AI agent behavior management.

## Spreading Activation Theory

**Collins & Loftus (1975)** proposed that human semantic memory is organized as a network where concepts are nodes and relationships are edges. When you think of "doctor," activation spreads to related concepts — "hospital," "nurse," "stethoscope" — with strength decreasing over distance. This explains why related concepts come to mind faster (semantic priming) and why context shapes what you remember.

floop applies this directly: behaviors are semantic nodes, and when you're working in a Go test file, activation spreads from "Go" and "testing" seed nodes through the graph, lighting up behaviors about table-driven tests, error handling patterns, and test coverage — while behaviors about Python or documentation stay dormant.

**Key paper:** Collins, A.M. & Loftus, E.F. (1975). A spreading-activation theory of semantic processing. *Psychological Review*, 82(6), 407-428.

## ACT-R Architecture

**John Anderson's ACT-R** (Adaptive Control of Thought—Rational) formalized how memory retrieval works as a computational process. In ACT-R, every memory chunk has a base-level activation that decays over time and receives contextual boosts from the current goal and environment. The chunk with the highest total activation gets retrieved.

floop mirrors this architecture:

| ACT-R Concept | floop Implementation |
|---|---|
| Memory chunks | Behaviors (graph nodes) |
| Base-level activation | ACT-R base-level activation: B_i = ln(n × L^(-d) / (1-d)) |
| Contextual activation | Spreading activation from seed nodes |
| Retrieval threshold | Minimum activation cutoff (epsilon) |
| Partial matching | Fuzzy predicate evaluation on `when` conditions |
| Activation decay | Temporal decay on edge weights (rho parameter) |

**Key work:** Anderson, J.R. (2007). *How Can the Human Mind Occur in the Physical Universe?* Oxford University Press.

## The SYNAPSE Paper

The direct catalyst for floop's activation engine was [SYNAPSE](https://arxiv.org/abs/2601.02744), which demonstrated that spreading activation could be applied to LLM agent episodic memory with dramatic results — **95% token reduction** while maintaining higher accuracy than full-context methods.

SYNAPSE's architecture maps cleanly to floop:

| SYNAPSE | floop |
|---|---|
| Episodic nodes | Corrections (specific interaction memories) |
| Semantic nodes | Behaviors (abstract knowledge) |
| Temporal edges | Edge timestamps + exponential decay |
| Association edges | `similar-to` edges |
| Abstraction edges | `learned-from` edges (correction → behavior) |
| Co-occurrence links | `co-activated` edges (Oja-stabilized Hebbian learning) |

### Parameters

floop's spreading activation parameters are derived from SYNAPSE's tuned values:

| Parameter | Value | SYNAPSE Name | Role |
|---|---|---|---|
| `MaxSteps` | 3 | T | Propagation iterations |
| `DecayFactor` | 0.5 | delta | Energy retention per hop |
| `SpreadFactor` | 0.8 | S | Energy transmission efficiency |
| `MinActivation` | 0.01 | epsilon | Activation threshold |
| `TemporalDecayRate` | 0.01 | rho | Edge weight decay over time |

## Lateral Inhibition

In neuroscience, lateral inhibition is the process by which strongly activated neurons suppress their weaker neighbors. This sharpens signals — it's why you see crisp edges instead of blur, and why one memory dominates over competing alternatives.

floop implements lateral inhibition in its activation engine. When a behavior's activation exceeds the inhibition threshold, it dampens the activation of nearby competing nodes. This prevents activation from dispersing uniformly across the graph and produces focused, decisive behavior retrieval.

Without inhibition, asking "what behaviors matter for Go testing?" might return everything vaguely related to Go. With inhibition, the strongly activated testing behaviors suppress the weakly activated general Go behaviors, giving you a focused, relevant set.

## Relevance Scoring

Final behavior ranking uses a weighted combination of four signals:

```
Score = 0.35 × context + 0.30 × base_level + 0.15 × feedback + 0.20 × priority
```

- **Context** (0.35) — How well the behavior's `when` predicates match the current file, language, and task
- **Base-level activation** (0.30) — ACT-R base-level activation combining frequency and recency (see below)
- **Feedback** (0.15) — Quality ratio from session feedback: confirmed vs overridden signals
- **Priority** (0.20) — User-assigned priority plus kind-based boosts (constraint ×2.0, directive ×1.5, procedure ×1.2)

### ACT-R Base-Level Activation

The base-level score implements Anderson's ACT-R equation:

```
B_i = ln(n × L^(-d) / (1-d))
```

Where *n* is the number of activations, *L* is age in hours, and *d* = 0.5 (standard ACT-R decay). Raw activation values (typically -4 to +2) are normalized to [0, 1] via a sigmoid centered at B_i = -1. New behaviors with no activation history receive a neutral score of 0.5.

### Session Feedback

The `floop_feedback` MCP tool allows agents to signal whether a behavior was helpful (`confirmed`) or contradicted (`overridden`) during a session. These signals feed into the feedback score component (15% weight), creating a closed feedback loop where behaviors that consistently help get reinforced and those that mislead get suppressed.

### Sigmoid Squashing

The sigmoid squashing function creates sharp distinction between activated and inactive nodes:

```
sigmoid(x) = 1 / (1 + e^(-10(x - 0.3)))
```

This produces near-binary activation: nodes are either clearly "on" or clearly "off," avoiding the ambiguity of intermediate activation levels.

## Hebbian Co-Activation Learning

When two behaviors consistently activate together in the same context, they likely have an affinity that the graph should capture. floop uses **Oja-stabilized Hebbian learning** to discover and reinforce these relationships automatically.

### Oja's Rule

Edge weight updates follow Oja's rule, a biologically-inspired learning rule with a built-in stabilization mechanism:

```
ΔW = η × (A_i × A_j − A_j² × W)
```

Where *η* = 0.05 (learning rate), *A_i* and *A_j* are the activations of the co-occurring behaviors, and *W* is the current edge weight. The *A_j² × W* term is Oja's "forgetting factor" — it prevents unbounded weight growth, keeping the system stable without needing explicit normalization.

### Edge Creation Process

Co-activated edges aren't created on the first co-occurrence. Instead, floop tracks co-activation pairs over a 7-day window and only creates a `co-activated` edge after **3 co-occurrences** — ensuring the relationship is stable, not coincidental. After creation, each subsequent co-activation applies the Oja update. Edges that decay below a minimum weight (0.01) are pruned.

Seed-to-seed pairs are excluded: if both behaviors activated because they matched the same context predicates (both are seeds), their co-occurrence reflects context matching, not genuine affinity.

### Override Edge Semantics

When the learning pipeline places a new behavior, it evaluates `isMoreSpecific(a, b)` to determine whether one behavior's `when` conditions are a strict superset of another's. If so, it creates an `overrides` edge from the more-specific behavior to the less-specific one.

Behaviors with empty `when` maps (`{}`) are treated as **unscoped** — they apply everywhere and are not considered "less specific" than scoped behaviors. This means no override edges are created from scoped behaviors to unscoped ones. Without this distinction, every scoped behavior would override every unscoped one, producing O(n*m) spurious edges that inflate outDegree denominators and dilute spreading activation.

## Related Work

- **HippoRAG** ([github.com/OSU-NLP-Group/HippoRAG](https://github.com/OSU-NLP-Group/HippoRAG)) — Episodic memory organization for LLMs inspired by hippocampal indexing
- **GraphRAG Spreading Activation** ([arxiv 2512.15922](https://arxiv.org/abs/2512.15922)) — Graph-based spreading activation for retrieval-augmented generation
- **Mem0** ([github.com/mem0ai/mem0](https://github.com/mem0ai/mem0)) — Universal memory layer for AI; focused on fact storage rather than behavior learning
- **claude-reflect-system** — Pattern detection for Claude skills; the closest existing tool to floop's learning loop, but without graph structure or spreading activation

## Further Reading

The [origin story](LORE.md) covers how these research threads came together during floop's development.
