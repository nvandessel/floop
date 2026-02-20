# Local Embeddings

floop supports local embedding-based retrieval for semantic behavior matching. This runs a small model on your machine — no API keys or network access required at runtime.

## Overview

When enabled, `floop_active` embeds the current context (file, task, language) and finds the most semantically similar behaviors via vector cosine similarity. This complements the existing predicate matching and spreading activation pipeline by finding relevant behaviors even when their `when` conditions don't exactly match.

## Setup

### Interactive

```bash
floop init
```

The interactive setup prompts whether to download and enable local embeddings (option 4).

### Non-interactive

```bash
# Enable embeddings
floop init --global --embeddings

# Skip embeddings
floop init --global --no-embeddings
```

### What Gets Downloaded

Two runtime dependencies are downloaded to `~/.floop/` (~130 MB total):

| Component | Size | Location |
|-----------|------|----------|
| llama.cpp shared libraries | ~50 MB | `~/.floop/lib/` |
| nomic-embed-text-v1.5 (Q4_K_M) | ~81 MB | `~/.floop/models/` |

Downloads happen once and are cached. Subsequent `floop init --embeddings` calls detect existing installations and skip re-downloading.

### Auto-detection

After initial setup, the MCP server auto-detects installed dependencies in `~/.floop/` on startup. No explicit configuration is needed — if the libraries and model are present, embeddings are enabled automatically.

## Configuration

### Config file (`~/.floop/config.yaml`)

```yaml
llm:
  provider: local
  enabled: true
  local_lib_path: /home/user/.floop/lib
  local_embedding_model_path: /home/user/.floop/models/nomic-embed-text-v1.5.Q4_K_M.gguf
```

These values are set automatically by `floop init --embeddings`. You can also set them manually:

```bash
floop config set llm.provider local
floop config set llm.local_lib_path ~/.floop/lib
floop config set llm.local_embedding_model_path ~/.floop/models/nomic-embed-text-v1.5.Q4_K_M.gguf
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `FLOOP_LOCAL_LIB_PATH` | Directory containing llama.cpp shared libraries |
| `FLOOP_LOCAL_EMBEDDING_MODEL_PATH` | Path to GGUF embedding model |
| `FLOOP_LOCAL_GPU_LAYERS` | GPU layer offload count (0 = CPU only) |
| `FLOOP_LOCAL_CONTEXT_SIZE` | Context window size in tokens (default: 512) |

## How It Works

### Embedding lifecycle

1. **Learn-time:** When `floop_learn` creates a new behavior, its canonical text is embedded in the background and stored alongside the behavior in SQLite
2. **Startup backfill:** On MCP server start, any behaviors without embeddings are backfilled in a background goroutine
3. **Retrieval-time:** `floop_active` composes the current context into a query, embeds it, and runs brute-force cosine similarity against all stored embeddings
4. **Safety net:** Behaviors without embeddings are always included in the candidate set — no behavior is silently dropped

### Storage

Embeddings are stored as BLOB columns in the behaviors SQLite table (768 dimensions x 4 bytes = 3,072 bytes per behavior). The embedding model name is tracked alongside each embedding for staleness detection.

### Performance

At typical scales (~200 behaviors x 768 dimensions), brute-force vector search completes in microseconds. No approximate nearest neighbor indices are needed.

## Troubleshooting

### Embeddings not working

Verify dependencies are installed:

```bash
ls ~/.floop/lib/libllama.*    # Should show libllama.so or libllama.dylib
ls ~/.floop/models/*.gguf      # Should show the model file
```

If missing, re-run setup:

```bash
floop init --embeddings
```

### Model not loading

Check that the library path matches your platform:
- **Linux:** `libllama.so` in `~/.floop/lib/`
- **macOS:** `libllama.dylib` in `~/.floop/lib/`

### High memory usage

The embedding model uses ~200 MB RAM when loaded. If this is a concern, disable embeddings:

```bash
floop init --no-embeddings
```

The system falls back to predicate matching and spreading activation (identical to pre-embedding behavior).

## Technical Details

- **Model:** nomic-embed-text-v1.5 (Q4_K_M quantization), 768-dimension output, 2048-token context
- **Runtime:** llama.cpp via [yzma](https://github.com/hybridgroup/yzma) purego bindings (no CGo)
- **Task prefixes:** `search_document:` for behavior text, `search_query:` for context queries (required by nomic-embed-text)
- **Storage:** SQLite BLOB column, ~3 KB per behavior

For the theory behind embedding-based retrieval, see [SCIENCE.md](SCIENCE.md).
