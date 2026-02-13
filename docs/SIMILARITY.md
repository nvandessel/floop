# Similarity Pipeline

How floop detects and merges duplicate behaviors.

## Overview

When `floop deduplicate` (or the `floop_deduplicate` MCP tool) runs, it compares every pair of behaviors in the store to find duplicates. Similarity is computed using a **3-tier fallback chain** — the first method that produces a result wins:

1. **Embedding similarity** — cosine similarity between vector embeddings
2. **LLM comparison** — structured semantic comparison via a language model
3. **Jaccard word overlap** — rule-based word overlap (always available)

Pairs that meet the similarity threshold are flagged as duplicates and optionally merged.

## Embedding Similarity

When an LLM client implements the `EmbeddingComparer` interface, floop generates embeddings for each behavior's content and computes **cosine similarity** between the resulting vectors.

- Range: -1.0 to 1.0 (in practice, 0.0 to 1.0 for normalized text embeddings)
- Vectors are L2-normalized before comparison
- Returns 0.0 for zero-magnitude or mismatched-length vectors

The **local provider** (`llm.provider = local`) is designed to support offline embedding via GGUF models loaded through yzma. It is currently a stub — the interface exists but embedding generation is not yet functional. When it is ready, it will be the fastest tier in the chain.

Providers that support embeddings: `openai`, `ollama`, `local` (stub).

## LLM Comparison

When embeddings are unavailable (or when the provider does not implement `EmbeddingComparer`), floop falls back to LLM-based comparison via `CompareBehaviors`. The model receives both behaviors and returns a structured result:

- **Semantic similarity** — 0.0 to 1.0 score
- **Intent match** — whether the behaviors target the same underlying intent
- **Merge recommendation** — whether the model recommends merging

The comparison uses `llm.comparison_model` (configurable). This is slower than embedding similarity but can capture nuanced semantic relationships.

## Jaccard Fallback

When no LLM is configured (or when LLM comparison fails and `llm.fallback_to_rules` is enabled), floop uses a weighted Jaccard word overlap:

| Component | Weight | Method |
|-----------|--------|--------|
| When-condition overlap | 40% | Exact value matching across `when` condition sets |
| Content word overlap | 60% | Case-insensitive word tokenization, set intersection / set union |

**When-condition overlap** compares the activation conditions (file patterns, task types, etc.) of both behaviors using exact value matching, with double weighting for matches.

**Content word overlap** tokenizes behavior content into lowercase words and computes the Jaccard index (intersection / union).

The final score is: `0.4 * when_overlap + 0.6 * content_overlap`

This method requires no external services and is always available as a fallback.

## Thresholds

The similarity threshold determines when two behaviors are considered duplicates:

- **Default:** 0.9 (configurable)
- **Auto-merge during learn:** 0.9 (when `--auto-merge` is enabled)
- **Range:** 0.0 (everything matches) to 1.0 (exact match only)

Configure via:

```bash
# CLI flag
floop deduplicate --threshold 0.85

# Config file
floop config set deduplication.similarity_threshold 0.85

# Environment variable
export FLOOP_SIMILARITY_THRESHOLD=0.85
```

## Cross-Store Deduplication

Behaviors live in two stores:

- **Local** — project-scoped (`.floop/`)
- **Global** — user-scoped (`~/.floop/`)

By default, deduplication runs within a single store. Use `--scope both` to compare behaviors across stores:

```bash
# Deduplicate within local store only
floop deduplicate

# Deduplicate within global store only
floop deduplicate --scope global

# Cross-store deduplication (local + global)
floop deduplicate --scope both
```

Cross-store dedup uses the same fallback chain and threshold. When duplicates span stores, the merge target is chosen based on the surviving behavior's store.

## Configuration

### Provider Setup

```yaml
# ~/.floop/config.yaml
llm:
  provider: anthropic          # anthropic, openai, ollama, local, subagent
  enabled: true
  api_key: ${ANTHROPIC_API_KEY}
  comparison_model: claude-sonnet-4-5-20250929
  fallback_to_rules: true      # Fall back to Jaccard when LLM fails

  # Local provider (stub — not yet functional)
  local_lib_path: /path/to/yzma/libs
  local_model_path: /path/to/model.gguf
  local_embedding_model_path: /path/to/embedding-model.gguf
  local_gpu_layers: 0          # 0 = CPU only
  local_context_size: 512

deduplication:
  auto_merge: false
  similarity_threshold: 0.9
```

### Environment Variables

| Variable | Config Key |
|----------|-----------|
| `FLOOP_LLM_PROVIDER` | `llm.provider` |
| `FLOOP_LLM_ENABLED` | `llm.enabled` |
| `FLOOP_SIMILARITY_THRESHOLD` | `deduplication.similarity_threshold` |
| `FLOOP_AUTO_MERGE` | `deduplication.auto_merge` |
| `FLOOP_LOCAL_LIB_PATH` | `llm.local_lib_path` |
| `FLOOP_LOCAL_MODEL_PATH` | `llm.local_model_path` |
| `FLOOP_LOCAL_EMBEDDING_MODEL_PATH` | `llm.local_embedding_model_path` |
| `FLOOP_LOCAL_GPU_LAYERS` | `llm.local_gpu_layers` |
| `FLOOP_LOCAL_CONTEXT_SIZE` | `llm.local_context_size` |

See [CLI Reference — Environment Variables](CLI_REFERENCE.md#environment-variables) for the complete list.

## Decision Logging

All similarity comparisons are logged through the `DecisionLogger` for audit and debugging. Set the log level to see similarity decisions:

```bash
# See which comparison method was used and the resulting scores
export FLOOP_LOG_LEVEL=debug

# See full detail including individual component scores
export FLOOP_LOG_LEVEL=trace
```

Or via config:

```bash
floop config set logging.level debug
```

## See Also

- [CLI Reference — deduplicate](CLI_REFERENCE.md#deduplicate)
- [CLI Reference — config](CLI_REFERENCE.md#config)
- [Floop Usage Guide](FLOOP_USAGE.md)
