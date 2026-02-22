# Vector Index Layer (HNSW)

**Epic:** feedback-loop-v5l
**Foundation branch:** `feature/vector-index`
**Target:** `main`

## Objective

Replace brute-force O(n) embedding search with HNSW approximate nearest neighbor indexing.
Scale floop's behavior store from 10 to millions of vectors with sub-millisecond search.

## Architecture

- **SQLite remains source of truth** for embeddings (BLOBs in behaviors table)
- HNSW index is a **derived acceleration cache** at `.floop/vector.idx`
- If index file is missing/corrupt, rebuild from SQLite on startup
- Library: [`coder/hnsw`](https://github.com/coder/hnsw) — pure Go, zero CGO, MIT

## Stack

| # | Branch | Bead | Description |
|---|--------|------|-------------|
| 1 | `feature/vector-index/01-core` | `v5l.1` | VectorIndex interface + BruteForceIndex |
| 2 | `feature/vector-index/02-hnsw` | `v5l.2` | HNSWIndex wrapping coder/hnsw |
| 3 | `feature/vector-index/03-tiered` | `v5l.3` | TieredIndex auto-selector |
| 4 | `feature/vector-index/04-wire` | `v5l.4` | Wire into MCP server + embedder |

Each PR targets the previous branch. As stacks merge into `feature/vector-index`,
remaining PRs auto-rebase onto the foundation.

## Commands

```bash
# View all tasks
bd show feedback-loop-v5l

# View specific task with full implementation spec
bd show feedback-loop-v5l.1   # Interface + BruteForce
bd show feedback-loop-v5l.2   # HNSW
bd show feedback-loop-v5l.3   # Tiered
bd show feedback-loop-v5l.4   # Wire MCP

# Build & test
go build ./internal/vectorindex/...
go test -race ./internal/vectorindex/...
go test ./...
```

## Key Files

| File | Role |
|------|------|
| `internal/vectorindex/index.go` | VectorIndex interface |
| `internal/vectorindex/bruteforce.go` | Exhaustive search impl |
| `internal/vectorindex/hnsw.go` | HNSW impl (coder/hnsw) |
| `internal/vectorindex/tiered.go` | Auto-selector |
| `internal/mcp/vector_retrieval.go` | Consumer (uses index.Search) |
| `internal/mcp/server.go` | Lifecycle (init/close index) |
| `internal/vectorsearch/embedder.go` | EmbedAndStore returns vector |
