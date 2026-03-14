# Memory Consolidation — Future Vision

**Date:** 2026-03-14
**Status:** Roadmap (not specced for implementation)
**Context:** Thinking captured during brainstorming for v0 memory consolidation. These ideas inform future work but are explicitly out of scope for the current implementation.

## Phased Delivery Model

The consolidation system self-bootstraps: each phase generates training data for the next.

```
v0 (heuristics) → v1 (LLM consolidator) → v2 (distilled local model) → v3 (streaming)
```

### v0: Heuristic Consolidator (current implementation)

Ships immediately with existing infrastructure. Rules, regex, embedding similarity. Every extraction is logged with inputs and outputs — this is the seed data.

- Human corrections via `floop_feedback` = negative examples
- Confirmations = positive examples
- Output: noisy but real labeled data

### v1: LLM Consolidator

Strong model (Sonnet/Opus) via API. Runs batch-style: end of session or nightly. Higher quality extraction and classification than heuristics.

- Every decision logged: "given events X, extracted Y, classified as Z"
- Autoresearch validates which decisions improved retrieval quality
- Output: clean, validated labeled data suitable for fine-tuning
- Prompt engineering starts human-curated (~100 items), then autoresearch iterates

### v2: Distilled Local Model

Fine-tuned small model (1-3B, GGUF) or classification head on nomic embeddings. Trained on v1's validated output. Runs via yzma (already supports GGUF inference).

- Autoresearch validates parity with LLM consolidator
- Outcome: fully local, no API dependency
- The model doesn't need to be creative — it needs to reliably classify and extract. SLMs (1-8B) match LLMs on focused classification tasks with ~100 labeled examples (various papers, 2025-2026).

### v3: Streaming Consolidation

v0-v1 are batch/post-session. v3 introduces real-time consolidation — events processed as they arrive rather than in batch after session end.

- Requires careful design around partial context (session still in progress)
- Tradeoff: faster memory availability vs. potentially less accurate (missing end-of-session context)
- May use a lightweight streaming classifier that flags high-confidence extractions immediately, with full consolidation still running post-session

## Custom Model Training Infrastructure

v2 distillation uses existing yzma for inference. The training pipeline itself is separate tooling that doesn't need to live inside floop.

### Training Data Pipeline

```
v0 heuristic outputs + human corrections
  → labeled dataset (event sequences → extracted memories)
  → v1 LLM consolidator outputs + autoresearch validation
  → clean labeled dataset
  → fine-tuning pipeline (separate from floop)
  → GGUF model deployed to ~/.floop/models/
  → v2 LocalModelConsolidator loads via yzma
```

### Key Decisions (Not Yet Made)

- **Training framework**: Standard fine-tuning (LoRA/QLoRA) vs. classification head on frozen embeddings. The classification head approach is simpler — nomic embeddings are already computed, just train a small classifier on top.
- **Model size**: 1-3B parameter range. Smaller is better for local inference. The task is classification, not generation.
- **Training data volume**: Target ~1000 validated examples before attempting distillation. v1 + autoresearch should generate this over weeks of normal usage.
- **C pipeline**: Eventually the hot path (inference at consolidation time) could move to C for performance. Not worth it until the model and task are well-understood. Premature optimization.

## Multi-User Memory Sharing

Multi-*agent* sharing is already the architecture — single global store at `~/.floop/floop.db`, any agent reads/writes via MCP or CLI. Claude Code, Gemini, Codex all see the same behaviors. This is the hive mind model.

Multi-*user* sharing is genuinely future work:

- **Trust boundaries**: Should memories from user A be visible to user B? Probably scoped by project — if you're collaborating on `nic/floop`, you share project-scoped behaviors.
- **Conflict resolution**: Two users learn contradictory things. Which wins? Confidence scores + recency + human arbitration.
- **Transport**: Packs already provide a sharing mechanism. A shared pack updated by multiple users is a lightweight multi-user store.
- **Access control**: Read-only vs. read-write per project scope. Some behaviors are organizational policy (read-only), some are team conventions (collaborative).

## Autoresearch Evolution

The autoresearch loop (inspired by Karpathy's autoresearch concept, 2026) evolves alongside the consolidation system.

### Automation Levels

| Level | Automated | Manual | Phase |
|-------|-----------|--------|-------|
| Manual | Nothing | Everything | Now (floop-bench Runs 1-11) |
| Semi-auto | Tier 1 post-consolidation | Human reviews results | v0 |
| Auto + guardrails | Full loop overnight, proposes changes | Human approves | v1 |
| Full auto | Karpathy-style: hypothesize, test, keep/discard | Weekly summary review | v2 |

### floop-bench → floop-research

floop-bench is the current evaluation harness. It becomes `floop-research` when it gains the ability to autonomously hypothesize and test consolidation parameters.

```bash
# Today (manual)
floop-bench run --arm flash_floop --tasks eval_set

# Future (automated)
floop-research run --experiment consolidation_threshold \
  --variants 0.5,0.6,0.7,0.8,0.9 \
  --tier1-gate "precision >= 0.75" \
  --tier2-eval swebench_subset \
  --overnight
```

Changes needed on the floop-bench side to support the autoresearch loop will be covered in a separate spec (noted in the v0 design doc's resolved questions).

## Code Indexing (Phase 2)

Factual memory — what code IS (structure, APIs, types) — is distinct from experiential memory (what happened when working with code). Code indexing rides the same LanceDB infrastructure but is a separate feature.

- Tools like Serena and lilbee handle code indexing well already
- Floop's value add would be *experiential* code memory: "last time we changed this module, the tests in X broke" — that's episodic, not factual
- May not need to build code indexing at all if the experiential layer provides enough value alongside existing tools
