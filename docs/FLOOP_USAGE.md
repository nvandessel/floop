# Floop Usage Guide

> How to use floop for persistent AI agent memory — via MCP (recommended) or CLI.

## Overview

floop captures corrections, extracts reusable behaviors, and activates them in context. New to floop? Start with the [5-minute walkthrough](WALKTHROUGH.md) to see the full learning loop in action.

It works via two interfaces:
- **MCP tools** (recommended) — Direct integration with AI tools like Claude Code, Cursor, etc.
- **CLI commands** — Full control from the terminal, useful for scripting and manual management

## MCP Tools (Recommended)

When configured as an MCP server, floop exposes these tools to your AI agent:

| Tool | Purpose |
|------|---------|
| `floop_active` | Get behaviors relevant to current context (file, task) |
| `floop_learn` | Capture a correction or insight |
| `floop_list` | List all stored behaviors |
| `floop_connect` | Create edges between behaviors |
| `floop_feedback` | Provide session feedback on a behavior (confirmed/overridden) |
| `floop_deduplicate` | Find and merge duplicate behaviors |
| `floop_graph` | Render behavior graph (DOT, JSON, or HTML) |
| `floop_validate` | Check graph consistency |
| `floop_backup` | Export graph state to backup file |
| `floop_restore` | Import graph state from backup |

### MCP Setup

Add floop as an MCP server in your AI tool's config. Example for Claude Code (`~/.claude/settings.json`):

```json
{
  "mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"]
    }
  }
}
```

See [docs/integrations/](integrations/) for more tools.

### MCP Resources

The MCP server also exposes resources that clients can subscribe to:

| URI | Description |
|-----|-------------|
| `floop://behaviors/active` | Active behaviors for current context (auto-loaded, 2000-token budget) |
| `floop://behaviors/expand/{id}` | Full details for a specific behavior (resource template) |

### MCP Workflow

```
# Agent automatically gets active behaviors via floop_active at session start
# When corrected, agent calls:
floop_learn(wrong="Used print for debugging", right="Use structured logging")

# Agent can check what's active for a specific file:
floop_active(file="internal/store/file.go", task="development")
```

**Automatic scope routing:** Via MCP, `floop_learn` automatically classifies behaviors and routes them to the correct store. Behaviors with project-specific conditions (file paths, environment) go to local (`.floop/`), while universal conventions (language, task) go to global (`~/.floop/`). No manual `--scope` flag needed.

## CLI Workflow

### Starting a Session

```bash
# Check active behaviors for your context
floop active --file "src/main.go" --task "development"

# Review your behavior store health
floop stats
```

### Capturing Corrections

```bash
# Basic correction
floop learn --wrong "Used fmt.Println for errors" --right "Use log.Fatal or return error"

# With context
floop learn \
  --wrong "Designed only local storage" \
  --right "Support both global and local scopes" \
  --file "internal/store/file.go" \
  --task "architecture"

# With auto-merge to consolidate similar behaviors
floop learn --wrong "..." --right "..." --auto-merge

# Override auto-classification (default: auto-classify based on When conditions)
floop learn --wrong "..." --right "..." --scope local
floop learn --wrong "..." --right "..." --scope global

# Note: Scope is automatically classified based on the behavior's activation
# conditions. The --scope flag overrides auto-classification when set.
```

### Querying Behaviors

```bash
# List all behaviors
floop list

# List only corrections
floop list --corrections

# Show a specific behavior
floop show <behavior-id>

# Explain why a behavior activated
floop why <behavior-id>

# Build a prompt from active behaviors
floop prompt --file "src/main.go" --task "coding"
```

### Store Management

```bash
# View store statistics and token budget (see docs/TOKEN_BUDGET.md for details)
floop stats
floop stats --budget 3000  # Simulate different budget
floop stats --json         # Machine-readable output

# Deduplicate similar behaviors
floop deduplicate --dry-run    # Preview what would merge
floop deduplicate              # Actually merge duplicates

# Validate graph consistency (dangling refs, cycles, self-refs, edge properties)
floop validate

# Backup and restore (V2: compressed + integrity verification)
floop backup                       # Create compressed .json.gz backup
floop backup --no-compress         # Create uncompressed .json backup
floop backup list                  # List all backups with metadata
floop backup verify <file>         # Verify backup integrity (SHA-256)
floop restore-backup <backup-file> # Restore (auto-detects V1/V2)
```

### Similarity & Deduplication

floop uses a 3-tier fallback chain to detect duplicate behaviors:

1. **Embedding similarity** — cosine similarity between behavior embeddings (fastest, requires an embedding provider)
2. **LLM comparison** — structured semantic comparison when embeddings are unavailable
3. **Jaccard word overlap** — rule-based fallback: 40% when-condition overlap + 60% content word overlap

The first method that produces a result is used. See [Similarity Pipeline](SIMILARITY.md) for details.

```bash
# Preview duplicates (dry run)
floop deduplicate --dry-run

# Use a custom similarity threshold (default: 0.9)
floop deduplicate --threshold 0.85

# Cross-store deduplication (local + global)
floop deduplicate --scope both
```

### Graph Operations

```bash
# Interactive HTML graph (opens in browser)
floop graph --format html

# Save HTML graph to file without opening
floop graph --format html -o graph.html --no-open

# Graphviz DOT format
floop graph --format dot
floop graph --format dot | dot -Tpng -o graph.png

# JSON format
floop graph --format json

# Connect related behaviors (weight must be in (0.0, 1.0])
floop connect <source> <target> similar-to
floop connect <source> <target> requires --weight 0.9
```

### Curation

```bash
# Remove a behavior
floop forget <behavior-id>

# Deprecate (soft-remove) a behavior
floop deprecate <behavior-id>

# Restore a deprecated behavior
floop restore <behavior-id>

# Merge two behaviors into one
floop merge <source-id> <target-id>
```

## Local Embeddings

floop can use local embeddings to semantically match behaviors to your current context. When enabled, `floop_active` uses vector similarity search as a pre-filter before spreading activation, finding behaviors by meaning rather than just keyword matching.

### Setup

```bash
# During initial setup (interactive prompt)
floop init

# Or explicitly enable embeddings
floop init --global --embeddings
```

This downloads two runtime dependencies (~130 MB total, cached in `~/.floop/`):
- **llama.cpp shared libraries** — inference runtime
- **nomic-embed-text-v1.5** (Q4_K_M) — embedding model

### How It Works

1. **At learn-time:** New behaviors are embedded and stored alongside the behavior
2. **At retrieval-time:** The current context (file, task, language) is embedded and compared against stored behavior embeddings using cosine similarity
3. **Fallback:** When embeddings are unavailable, floop uses the standard predicate-matching pipeline

Behaviors without embeddings are always included in candidates — no behavior is silently dropped during migration.

For setup details, see [EMBEDDINGS.md](EMBEDDINGS.md). For the theory behind vector retrieval, see [SCIENCE.md](SCIENCE.md).

## What to Capture

### DO capture:
- Direct corrections ("No, use X instead of Y")
- Design insights and architecture decisions
- Code conventions and patterns
- User preferences and workflow choices

### DON'T capture:
- One-time instructions (not generalizable)
- Simple typo fixes
- Temporary debugging commands

## Verification

```bash
# Check store health
floop stats

# Verify behaviors exist
floop list

# Test activation for a file
floop active --file "any/file.go"

# Validate graph consistency (dangling refs, cycles, self-refs, edge properties)
floop validate
```

For the complete command reference, see [CLI_REFERENCE.md](CLI_REFERENCE.md).
