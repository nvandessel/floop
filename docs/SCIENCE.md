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
| Base-level activation | Confidence score + recency decay |
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

## Hybrid Scoring

Final behavior ranking uses a weighted combination of three signals:

```
Score = 0.5 × context_relevance + 0.3 × activation_level + 0.2 × PageRank
```

- **Context relevance** (lambda1 = 0.5) — How well the behavior's `when` predicates match the current context
- **Activation level** (lambda2 = 0.3) — The behavior's activation after spreading + inhibition
- **PageRank** (lambda3 = 0.2) — Graph centrality, favoring well-connected behaviors as likely more generally useful

The sigmoid squashing function creates sharp distinction between activated and inactive nodes:

```
sigmoid(x) = 1 / (1 + e^(-10(x - 0.3)))
```

This produces near-binary activation: nodes are either clearly "on" or clearly "off," avoiding the ambiguity of intermediate activation levels.

## Related Work

- **HippoRAG** ([github.com/OSU-NLP-Group/HippoRAG](https://github.com/OSU-NLP-Group/HippoRAG)) — Episodic memory organization for LLMs inspired by hippocampal indexing
- **GraphRAG Spreading Activation** ([arxiv 2512.15922](https://arxiv.org/abs/2512.15922)) — Graph-based spreading activation for retrieval-augmented generation
- **Mem0** ([github.com/mem0ai/mem0](https://github.com/mem0ai/mem0)) — Universal memory layer for AI; focused on fact storage rather than behavior learning
- **claude-reflect-system** — Pattern detection for Claude skills; the closest existing tool to floop's learning loop, but without graph structure or spreading activation

## Further Reading

The [origin story](LORE.md) covers how these research threads came together during floop's development.
