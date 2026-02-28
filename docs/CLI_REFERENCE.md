# floop CLI Reference

Complete reference for all floop commands. floop manages learned behaviors and conventions for AI coding agents -- it captures corrections, extracts reusable behaviors, and provides context-aware behavior activation for consistent agent operation.

**Version:** 0.8.0

---

## Global Flags

These flags are available on every command.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Output as JSON (for agent consumption) |
| `--root` | string | `.` | Project root directory |
| `--version`, `-v` | bool | `false` | Print version information and exit |

---

## Core

Commands for initializing floop, capturing corrections, and managing hooks.

### init

Initialize floop with hooks and behavior learning.

```
floop init [flags]
```

Configures Claude Code hook settings to use native `floop hook` subcommands, seeds meta-behaviors, and creates the `.floop/` data directory.

**Interactive mode** (no flags): Prompts for installation scope, hooks, and token budget.
**Non-interactive mode** (any flag provided): Uses flag values with sensible defaults. Suitable for scripts and agents.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--global` | bool | `false` | Install hooks globally (`~/.claude/`) |
| `--project` | bool | `false` | Install hooks for this project (`.claude/`) |
| `--hooks` | string | `""` | Which hooks to enable: `all`, `injection-only` (default in non-interactive: `all`) |
| `--token-budget` | int | `2000` | Token budget for behavior injection |
| `--embeddings` | bool | `false` | Download and enable local embeddings for semantic retrieval |
| `--no-embeddings` | bool | `false` | Skip local embeddings setup |

**Examples:**

```bash
# Interactive setup
floop init

# Global install with all defaults
floop init --global

# Project-level install with all defaults
floop init --project

# Both scopes with explicit options
floop init --global --project --hooks=all --token-budget 2000

# Enable local embeddings (downloads ~130 MB on first run)
floop init --global --embeddings

# Skip embeddings setup
floop init --global --no-embeddings
```

**See also:** [upgrade](#upgrade), [config](#config)

---

### learn

Capture a correction and extract a behavior.

```
floop learn --right <text> [--wrong <text>] [flags]
```

Called by agents when they receive a correction. Records the correction, extracts a candidate behavior, and determines whether the behavior can be auto-accepted or requires human review.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--right` | string | *(required)* | What should have been done |
| `--wrong` | string | `""` | What the agent did (optional, stored as provenance only) |
| `--file` | string | `""` | Current file path |
| `--task` | string | `""` | Current task type |
| `--scope` | string | `""` | Override auto-classification: `local` (project) or `global` (user) |
| `--auto-merge` | bool | `true` | Automatically merge similar behaviors (matches MCP behavior) |
| `--tags` | string slice | `nil` | Additional tags to apply, merged with inferred tags (max 5) |

**Tags:** Behaviors are automatically tagged via dictionary-based extraction (e.g., a correction mentioning "git" and "worktree" gets those tags). The `--tags` flag adds user-provided tags on top of inferred tags. Tags are normalized (lowercased, deduplicated), and dictionary synonyms are resolved (e.g., `--tags golang` becomes `go`). User-provided tags always survive the 8-tag cap; inferred tags fill remaining slots.

**Scope classification (MCP):** When invoked via the MCP server (`floop_learn` tool), the `--scope` flag is not used. Instead, behaviors are automatically classified based on their activation conditions: behaviors with `file_path` or `environment` in their When predicate go to local (`.floop/`), while all others go to global (`~/.floop/`). The response includes a `scope` field indicating where the behavior was stored.

**Examples:**

```bash
# Capture a behavior (right only)
floop learn --right "use pathlib.Path instead"

# With optional wrong context
floop learn --wrong "used os.path" --right "use pathlib.Path instead"

# With file context, saved globally
floop learn --right "use logging module" --file main.py --scope global

# With explicit tags for pack filtering
floop learn --right "use uv for Python packages" --tags frond,workflow

# Machine-readable output
floop learn --right "use environment variables" --json
```

**See also:** [detect-correction](#detect-correction), [reprocess](#reprocess), [list](#list), [tags](#tags)

---

### reprocess

Reprocess orphaned corrections into behaviors.

```
floop reprocess [flags]
```

Reads all corrections from `corrections.jsonl`, identifies those that have not been processed (no corresponding behavior exists), and runs them through the learning loop to extract behaviors.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | `false` | Show what would be processed without making changes |
| `--scope` | string | `""` | Override auto-classification: `local` or `global` |
| `--auto-merge` | bool | `true` | Automatically merge similar behaviors (matches MCP behavior) |

**Examples:**

```bash
# Reprocess local corrections
floop reprocess

# Preview what would be processed
floop reprocess --dry-run

# Reprocess and save to global store
floop reprocess --scope global
```

**See also:** [learn](#learn), [list](#list)

---

### --version

Print version information.

```
floop --version
floop -v
```

**Examples:**

```bash
floop --version
# floop version v0.5.7 (commit: abc1234, built: 2026-02-10T15:30:00Z)
```

> **Note:** `floop version` is still accepted for backward compatibility.
> For JSON version output, use: `floop version --json`

---

### upgrade

Upgrade floop hook configuration to native Go subcommands.

```
floop upgrade [flags]
```

Detects hook installations in global (`~/.claude/`) and project (`.claude/`) scopes. Migrates old shell script installations (`.sh` files) to native `floop hook` subcommands, and reports already-native configurations as up to date.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Re-configure hooks even if already native |

**Examples:**

```bash
# Upgrade (migrate old scripts or verify native hooks)
floop upgrade

# Force re-configure hooks
floop upgrade --force

# JSON output
floop upgrade --json
```

**See also:** [init](#init)

---

## Query

Commands for querying, inspecting, and generating output from learned behaviors.

### active

Show behaviors active in the current context.

```
floop active [flags]
```

Lists all behaviors that are currently active based on the current context (file, task, language, etc.). Loads behaviors from both local and global stores.

When local embeddings are configured, `floop active` uses vector similarity search as a pre-filter before applying spreading activation. The vector index automatically selects between brute-force (≤1,000 vectors) and HNSW (>1,000 vectors) backends for optimal performance. See [EMBEDDINGS.md](EMBEDDINGS.md) for details.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--file` | string | `""` | Current file path |
| `--task` | string | `""` | Current task type |
| `--env` | string | `""` | Environment (`dev`, `staging`, `prod`) |

**Examples:**

```bash
# Show behaviors active for a Go file
floop active --file main.go

# Active behaviors for testing tasks
floop active --task testing

# Machine-readable output
floop active --file src/app.py --json
```

**See also:** [list](#list), [why](#why), [prompt](#prompt)

---

### list

List behaviors or corrections.

```
floop list [flags]
```

Lists learned behaviors from the behavior store, or captured corrections when `--corrections` is specified.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--corrections` | bool | `false` | Show captured corrections instead of behaviors |
| `--global` | bool | `false` | Show behaviors from global user store (`~/.floop/`) only |
| `--all` | bool | `false` | Show behaviors from both local and global stores |
| `--tag` | string | `""` | Filter behaviors by tag (exact match) |

**Examples:**

```bash
# List local behaviors
floop list

# List behaviors from global store
floop list --global

# List all behaviors across both stores
floop list --all

# Filter by tag
floop list --tag go

# Show captured corrections
floop list --corrections

# JSON output for scripting
floop list --all --json
```

**See also:** [active](#active), [show](#show), [learn](#learn)

---

### show

Show details of a behavior.

```
floop show <behavior-id>
```

Displays the full details of a specific behavior, including content, activation conditions, provenance, and relationship metadata. Accepts a behavior ID or name. Searches both local and global stores.

No command-specific flags.

**Examples:**

```bash
# Show by ID
floop show b-1706000000000000000

# Show by name
floop show "prefer-pathlib"

# JSON output
floop show b-1706000000000000000 --json
```

**See also:** [list](#list), [why](#why)

---

### why

Explain why a behavior is or is not active.

```
floop why <behavior-id> [flags]
```

Shows the activation status of a behavior and explains why it matches or does not match the current context. Useful for debugging when a behavior is not being applied as expected.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--file` | string | `""` | Current file path |
| `--task` | string | `""` | Current task type |
| `--env` | string | `""` | Environment (`dev`, `staging`, `prod`) |

**Examples:**

```bash
# Explain activation status
floop why b-1706000000000000000

# Explain in context of a specific file
floop why b-1706000000000000000 --file main.go

# JSON output for agent consumption
floop why b-1706000000000000000 --file main.py --task testing --json
```

**See also:** [active](#active), [show](#show)

---

### prompt

Generate a prompt section from active behaviors.

```
floop prompt [flags]
```

Compiles active behaviors into a format suitable for injection into agent system prompts. Supports token budgeting with intelligent tiering (full/summary/omit).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--file` | string | `""` | Current file path |
| `--task` | string | `""` | Current task type |
| `--env` | string | `""` | Environment (`dev`, `staging`, `prod`) |
| `--format` | string | `"markdown"` | Output format: `markdown`, `xml`, `plain` |
| `--max-tokens` | int | `0` | Maximum tokens (0 = unlimited, deprecated: use `--token-budget`) |
| `--token-budget` | int | `0` | Token budget for behavior injection (enables intelligent tiering) |
| `--tiered` | bool | `false` | Use tiered injection (full/summary/omit) instead of simple truncation |

**Examples:**

```bash
# Generate prompt for Go files
floop prompt --file main.go

# Tiered injection with token budget
floop prompt --file main.go --tiered --token-budget 2000

# XML format with budget
floop prompt --file main.go --format xml --token-budget 500

# JSON output for agent tooling
floop prompt --file main.go --json
```

**See also:** [active](#active), [summarize](#summarize), [stats](#stats)

---

## Curation

Commands for managing the lifecycle of individual behaviors.

### forget

Soft-delete a behavior from active use.

```
floop forget <behavior-id> [flags]
```

Marks a behavior as forgotten, removing it from active use. The behavior is not deleted, just marked with kind `forgotten-behavior`. Use `floop restore` to undo this action.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |
| `--reason` | string | `""` | Reason for forgetting |

**Examples:**

```bash
# Forget with confirmation prompt
floop forget b-1706000000000000000

# Skip prompt, provide reason
floop forget b-1706000000000000000 --force --reason "no longer relevant"

# JSON mode (implies --force)
floop forget b-1706000000000000000 --json
```

**See also:** [restore](#restore), [deprecate](#deprecate)

---

### deprecate

Mark a behavior as deprecated.

```
floop deprecate <behavior-id> --reason <text> [flags]
```

Marks a behavior as deprecated but keeps it visible. Deprecated behaviors are not active but can be restored. Optionally link to a replacement behavior.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--reason` | string | *(required)* | Reason for deprecation |
| `--replacement` | string | `""` | ID of behavior that replaces this one |

**Examples:**

```bash
# Deprecate with reason
floop deprecate b-old --reason "superseded by new convention"

# Deprecate with replacement link
floop deprecate b-old --reason "replaced" --replacement b-new

# JSON output
floop deprecate b-old --reason "outdated" --json
```

**See also:** [restore](#restore), [forget](#forget), [merge](#merge)

---

### restore

Restore a deprecated or forgotten behavior.

```
floop restore <behavior-id>
```

Restores a behavior that was previously deprecated or forgotten. Undoes `floop forget` or `floop deprecate`.

No command-specific flags.

**Examples:**

```bash
# Restore a forgotten behavior
floop restore b-1706000000000000000

# JSON output
floop restore b-1706000000000000000 --json
```

**See also:** [forget](#forget), [deprecate](#deprecate)

---

### merge

Merge two behaviors into one.

```
floop merge <source-id> <target-id> [flags]
```

Combines two similar behaviors into one. The source behavior is marked as merged and linked to the target (surviving) behavior. When conditions are merged (union), and the higher confidence/priority values are kept. This action cannot be undone with restore.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |
| `--into` | string | `""` | ID of behavior that should survive (default: second argument) |

**Examples:**

```bash
# Merge source into target (target survives)
floop merge b-duplicate b-canonical

# Explicitly choose survivor
floop merge b-first b-second --into b-first

# Skip confirmation
floop merge b-old b-new --force
```

**See also:** [deduplicate](#deduplicate), [forget](#forget)

---

## Management

Commands for store-level operations: deduplication, validation, and configuration.

### deduplicate

Find and merge duplicate behaviors.

```
floop deduplicate [flags]
```

Analyzes all behaviors in the store, identifies duplicates based on semantic similarity (embedding, LLM, or Jaccard word overlap — see [Similarity Pipeline](SIMILARITY.md)), and can automatically merge them.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | `false` | Show duplicates without merging |
| `--threshold` | float64 | `0.9` | Similarity threshold for duplicate detection (0.0-1.0) |
| `--scope` | string | `"local"` | Store scope: `local`, `global`, or `both` |

**Examples:**

```bash
# Find and merge duplicates in local store
floop deduplicate

# Preview duplicates without merging
floop deduplicate --dry-run

# Use lower similarity threshold
floop deduplicate --threshold 0.8

# Cross-store deduplication
floop deduplicate --scope both

# JSON output
floop deduplicate --dry-run --json
```

**See also:** [merge](#merge), [validate](#validate)

---

### validate

Validate the behavior graph for consistency issues.

```
floop validate [flags]
```

Checks for dangling references (behaviors referencing non-existent IDs), self-references (behaviors that require/override/conflict with themselves), cycles in relationship graphs, and edge property issues (zero weight, missing timestamps).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--scope` | string | `"local"` | Store scope: `local`, `global`, or `both` |

**Examples:**

```bash
# Validate local store
floop validate

# Validate global store
floop validate --scope global

# Validate both stores
floop validate --scope both

# JSON output
floop validate --json
```

**See also:** [deduplicate](#deduplicate), [graph](#graph)

---

### config

Manage floop configuration.

```
floop config <subcommand> [args]
```

View and modify floop configuration settings. Configuration is stored in `~/.floop/config.yaml`.

**Subcommands:**

#### config list

List all configuration settings.

```
floop config list
```

No command-specific flags.

#### config get

Get a configuration value.

```
floop config get <key>
```

#### config set

Set a configuration value.

```
floop config set <key> <value>
```

**Available configuration keys:**

| Key | Type | Description |
|-----|------|-------------|
| `llm.provider` | string | LLM provider: `anthropic`, `openai`, `ollama`, `subagent`, or empty |
| `llm.enabled` | bool | Enable LLM features |
| `llm.api_key` | string | API key for LLM provider |
| `llm.base_url` | string | Custom base URL for LLM API |
| `llm.comparison_model` | string | Model used for behavior comparison |
| `llm.merge_model` | string | Model used for behavior merging |
| `llm.timeout` | duration | Request timeout (e.g., `30s`) |
| `llm.fallback_to_rules` | bool | Fall back to rule-based processing if LLM fails |
| `llm.local_lib_path` | string | Directory containing yzma shared libraries (local provider) |
| `llm.local_model_path` | string | Path to GGUF model for text generation (local provider) |
| `llm.local_embedding_model_path` | string | Path to GGUF model for embeddings; falls back to `local_model_path` (local provider) |
| `llm.local_gpu_layers` | int | GPU layer offload count; 0 = CPU only (local provider) |
| `llm.local_context_size` | int | Context window size in tokens; default 512 (local provider) |
| `deduplication.auto_merge` | bool | Automatically merge duplicates |
| `deduplication.similarity_threshold` | float | Similarity threshold (0.0-1.0) |
| `logging.level` | string | Log verbosity: `info`, `debug`, `trace` |
| `backup.compression` | bool | Enable gzip compression for backups (V2 format); default `true` |
| `backup.auto_backup` | bool | Automatically backup after learn operations; default `true` |
| `backup.retention.max_count` | int | Maximum number of backups to retain; default `10` |
| `backup.retention.max_age` | string | Maximum age of backups (e.g., `30d`, `2w`, `720h`); empty = disabled |
| `backup.retention.max_total_size` | string | Maximum total size of all backups (e.g., `100MB`, `1GB`); empty = disabled |

**Examples:**

```bash
# Show all settings
floop config list

# Get a specific setting
floop config get llm.provider

# Set LLM provider
floop config set llm.provider anthropic

# Set API key
floop config set llm.api_key $ANTHROPIC_API_KEY

# JSON output
floop config list --json
```

**See also:** [init](#init)

---

### Environment Variables

Environment variables override their corresponding config keys. They are applied after the config file is loaded.

| Variable | Config Key | Notes |
|----------|-----------|-------|
| `FLOOP_LLM_PROVIDER` | `llm.provider` | |
| `FLOOP_LLM_ENABLED` | `llm.enabled` | `"true"` or `"1"` to enable |
| `ANTHROPIC_API_KEY` | `llm.api_key` | When `provider=anthropic` |
| `OPENAI_API_KEY` | `llm.api_key` | When `provider=openai` |
| `OLLAMA_HOST` | `llm.base_url` | When `provider=ollama`; default: `http://localhost:11434/v1` |
| `FLOOP_LOCAL_LIB_PATH` | `llm.local_lib_path` | |
| `FLOOP_LOCAL_MODEL_PATH` | `llm.local_model_path` | |
| `FLOOP_LOCAL_EMBEDDING_MODEL_PATH` | `llm.local_embedding_model_path` | |
| `FLOOP_LOCAL_GPU_LAYERS` | `llm.local_gpu_layers` | |
| `FLOOP_LOCAL_CONTEXT_SIZE` | `llm.local_context_size` | |
| `FLOOP_AUTO_MERGE` | `deduplication.auto_merge` | `"true"` or `"1"` to enable |
| `FLOOP_SIMILARITY_THRESHOLD` | `deduplication.similarity_threshold` | |
| `FLOOP_LOG_LEVEL` | `logging.level` | |
| `FLOOP_BACKUP_COMPRESSION` | `backup.compression` | `"true"` or `"1"` to enable (default: enabled) |
| `FLOOP_BACKUP_AUTO` | `backup.auto_backup` | `"true"` or `"1"` to enable (default: enabled) |
| `FLOOP_BACKUP_MAX_COUNT` | `backup.retention.max_count` | Integer; default `10` |
| `FLOOP_BACKUP_MAX_AGE` | `backup.retention.max_age` | Duration string (e.g., `30d`, `2w`) |
| `FLOOP_ENV` | — | Override environment auto-detection |

---

## Token Optimization

Commands for managing token usage and behavior summaries. For details on how the token budget system works (tiering, demotion, configuration), see [TOKEN_BUDGET.md](TOKEN_BUDGET.md).

### summarize

Generate or regenerate summaries for behaviors.

```
floop summarize [behavior-id] [flags]
```

Generates compressed summaries for behaviors to optimize token usage. Summaries are used in tiered injection when the full behavior content would exceed the token budget. Each summary is approximately 60 characters.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | `false` | Generate summaries for all behaviors |
| `--missing` | bool | `false` | Only generate for behaviors without summaries |

**Examples:**

```bash
# Generate summary for a specific behavior
floop summarize b-1706000000000000000

# Generate summaries for all behaviors
floop summarize --all

# Only fill in missing summaries
floop summarize --missing

# JSON output
floop summarize --all --json
```

**See also:** [stats](#stats), [prompt](#prompt)

---

### stats

Show behavior usage statistics.

```
floop stats [flags]
```

Displays usage statistics for learned behaviors including activation counts, follow rates, ranking scores, and token budget utilization. Helps understand which behaviors are most valuable and which may need review.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--top` | int | `0` | Show only top N behaviors (0 = all) |
| `--sort` | string | `"score"` | Sort by: `score`, `activations`, `followed`, `rate`, `confidence`, `priority` |
| `--scope` | string | `"local"` | Scope: `local`, `global`, or `both` |
| `--budget` | int | `2000` | Token budget for injection simulation |

**Examples:**

```bash
# Show all stats
floop stats

# Top 10 behaviors by usage
floop stats --top 10

# Sort by follow rate
floop stats --sort rate

# Simulate different token budget
floop stats --budget 1000

# JSON output for programmatic access
floop stats --json
```

**See also:** [summarize](#summarize), [prompt](#prompt), [list](#list)

---

## Graph

Commands for visualizing and managing the behavior graph.

### graph

Visualize the behavior graph.

```
floop graph [flags]
```

Outputs the behavior graph in DOT (Graphviz), JSON, or interactive HTML format.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--format` | string | `"dot"` | Output format: `dot`, `json`, or `html` |
| `-o`, `--output` | string | | Output file path (html format only) |
| `--no-open` | bool | `false` | Don't open browser after generating HTML |

The `html` format generates a self-contained HTML file with an interactive force-directed graph visualization. Nodes are colored by behavior kind and sized by PageRank score + connection degree. Hover for tooltips, click nodes for a detail panel.

![Graph View](images/graph-view.png)

**Examples:**

```bash
# Generate DOT graph (pipe to Graphviz)
floop graph | dot -Tpng -o graph.png

# JSON format
floop graph --format json

# Interactive HTML graph (opens in browser)
floop graph --format html

# Save HTML to specific path without opening
floop graph --format html -o graph.html --no-open

# Save DOT to file
floop graph > behaviors.dot
```

**See also:** [connect](#connect), [validate](#validate)

---

### connect

Create an edge between two behaviors.

```
floop connect <source> <target> <kind> [flags]
```

Creates a semantic edge between two behaviors in the graph for spreading activation.

**Edge kinds:**

| Kind | Description |
|------|-------------|
| `requires` | Source depends on target |
| `overrides` | Source replaces target in matching context |
| `conflicts` | Source and target cannot both be active |
| `similar-to` | Behaviors are related/similar |
| `learned-from` | Source was derived from target |

> **Note:** `co-activated` edges are system-managed (created automatically by Hebbian learning) and cannot be created manually.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--weight` | float64 | `0.8` | Edge weight (0.0-1.0], exclusive zero |
| `--bidirectional` | bool | `false` | Create edges in both directions |

**Examples:**

```bash
# Connect two similar behaviors
floop connect behavior-abc behavior-xyz similar-to

# Set a custom weight
floop connect behavior-abc behavior-xyz requires --weight 0.9

# Bidirectional connection
floop connect behavior-abc behavior-xyz similar-to --bidirectional

# JSON output
floop connect behavior-abc behavior-xyz conflicts --json
```

**See also:** [graph](#graph), [validate](#validate)

---

### tags

Manage behavior tags.

```
floop tags <subcommand> [flags]
```

Tags are assigned automatically during `floop learn` via dictionary-based extraction. You can also provide explicit tags at learn-time with `--tags` (see [learn](#learn)). The `tags backfill` subcommand retroactively assigns tags to older behaviors that were learned before tagging existed.

#### tags backfill

Extract and assign semantic tags to existing behaviors using dictionary-based extraction.

```
floop tags backfill [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | `false` | Show what tags would be assigned without making changes |
| `--scope` | string | `"both"` | Store scope: `local`, `global`, or `both` |

**Examples:**

```bash
# Preview tag extraction
floop tags backfill --dry-run

# Backfill tags for local store
floop tags backfill

# Backfill across both stores
floop tags backfill --scope both

# JSON output
floop tags backfill --json
```

**See also:** [learn](#learn) (`--tags` flag), [list](#list) (`--tag` flag), [deduplicate](#deduplicate)

---

## Skill Packs

Commands for creating, installing, and managing portable skill packs.

### pack

Parent command for skill pack operations.

```
floop pack <subcommand> [flags]
```

Skill packs are portable behavior collections (`.fpack` files) that can be shared, installed, and updated. Packs use the V2 backup format with pack metadata in the header.

**Subcommands:**

| Subcommand | Description |
|------------|-------------|
| `create` | Create a pack from current behaviors |
| `install` | Install a pack from a file, URL, or GitHub repo |
| `list` | List installed packs |
| `info` | Show details of an installed pack |
| `update` | Update installed packs from their remote sources |
| `remove` | Remove an installed pack |

---

#### pack create

Create a skill pack from current behaviors.

```
floop pack create <output-path> [flags]
```

Exports filtered behaviors and their connecting edges into a portable `.fpack` file. Only edges where both endpoints pass the filter are included.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--id` | string | *(required)* | Pack ID in `namespace/name` format |
| `--version` | string | *(required)* | Pack version |
| `--description` | string | `""` | Pack description |
| `--author` | string | `""` | Pack author |
| `--tags` | string | `""` | Comma-separated pack tags |
| `--source` | string | `""` | Pack source URL |
| `--filter-tags` | string | `""` | Only include behaviors with these tags (comma-separated) |
| `--filter-scope` | string | `""` | Only include behaviors from this scope (`global`/`local`) |
| `--filter-kinds` | string | `""` | Only include behaviors of these kinds (comma-separated) |

**Examples:**

```bash
# Create a pack with all behaviors
floop pack create my-pack.fpack --id my-org/my-pack --version 1.0.0

# Filter by tags
floop pack create go-pack.fpack --id my-org/go-pack --version 1.0.0 --filter-tags go,testing

# Filter by scope
floop pack create global.fpack --id my-org/global --version 1.0.0 --filter-scope global

# With full metadata
floop pack create my-pack.fpack --id my-org/my-pack --version 1.0.0 \
  --description "Best practices for Go development" \
  --author "My Org" --tags go,best-practices \
  --source https://github.com/my-org/packs

# JSON output
floop pack create my-pack.fpack --id my-org/my-pack --version 1.0.0 --json
```

**See also:** [pack install](#pack-install), [pack list](#pack-list)

---

#### pack install

Install a skill pack from a file, URL, or GitHub repo.

```
floop pack install <source> [flags]
```

Installs behaviors from a pack source into the store. Supports local files, HTTP/HTTPS URLs, and GitHub shorthand (`gh:owner/repo`). Follows the seeder pattern: forgotten behaviors are not re-added, existing behaviors are version-gated for updates, and provenance is stamped on each installed behavior.

**Source formats:**

| Format | Example |
|--------|---------|
| Local file | `./my-pack.fpack`, `~/.floop/packs/pack.fpack` |
| HTTP URL | `https://example.com/pack.fpack` |
| GitHub (latest) | `gh:owner/repo` |
| GitHub (version) | `gh:owner/repo@v1.2.3` |

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--derive-edges` | bool | `false` | Derive edges between pack behaviors and existing behaviors |
| `--all-assets` | bool | `false` | Install all `.fpack` assets from a multi-asset GitHub release |

**GitHub authentication:** Set `GITHUB_TOKEN` env var or log in with `gh auth login` to avoid rate limits and access private repos.

**Examples:**

```bash
# Install a local pack file
floop pack install my-pack.fpack

# Install from a URL
floop pack install https://example.com/go-best-practices.fpack

# Install from GitHub (latest release)
floop pack install gh:my-org/my-packs

# Install a specific version from GitHub
floop pack install gh:my-org/my-packs@v1.2.0

# Install all packs from a multi-asset release
floop pack install gh:my-org/my-packs --all-assets

# JSON output
floop pack install gh:my-org/my-packs --json
```

**See also:** [pack create](#pack-create), [pack update](#pack-update), [pack remove](#pack-remove)

---

#### pack list

List installed skill packs.

```
floop pack list
```

Shows all currently installed skill packs from config, including version, behavior count, edge count, and install date.

No command-specific flags.

**Examples:**

```bash
# List installed packs
floop pack list

# JSON output
floop pack list --json
```

**See also:** [pack info](#pack-info), [pack install](#pack-install)

---

#### pack info

Show details of an installed skill pack.

```
floop pack info <pack-id>
```

Displays pack details from config and lists all behaviors from that pack currently in the store.

No command-specific flags.

**Examples:**

```bash
# Show pack info
floop pack info my-org/my-pack

# JSON output
floop pack info my-org/my-pack --json
```

**See also:** [pack list](#pack-list)

---

#### pack update

Update installed packs from their remote sources.

```
floop pack update [pack-id|source] [flags]
```

Updates an installed pack by re-fetching from its recorded source. For GitHub sources, the remote version is checked first; if already up-to-date, the download is skipped.

Can also accept a source string directly (file path, URL, or GitHub shorthand) to update from a specific source.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--derive-edges` | bool | `false` | Derive edges between pack behaviors and existing behaviors |
| `--all` | bool | `false` | Update all installed packs that have remote sources |

**Examples:**

```bash
# Update a specific pack (uses its recorded source)
floop pack update my-org/my-pack

# Update from a specific GitHub version
floop pack update gh:owner/repo@v2.0.0

# Update from a local file
floop pack update my-pack-v2.fpack

# Update all packs with remote sources
floop pack update --all

# JSON output
floop pack update my-org/my-pack --json
```

**See also:** [pack install](#pack-install)

---

#### pack remove

Remove an installed skill pack.

```
floop pack remove <pack-id>
```

Marks all behaviors from the pack as forgotten and removes the pack from the installed packs list in config.

No command-specific flags.

**Examples:**

```bash
# Remove a pack
floop pack remove my-org/my-pack

# JSON output
floop pack remove my-org/my-pack --json
```

**See also:** [pack install](#pack-install), [pack list](#pack-list)

---

## Backup

Commands for backing up and restoring the behavior graph.

### backup

Export the full graph state to a backup file.

```
floop backup [flags]
```

Backs up the complete behavior graph (nodes + edges) to a compressed file with SHA-256 integrity verification (V2 format). Applies retention policy to rotate old backups (default: keep last 10).

Default location: `~/.floop/backups/floop-backup-YYYYMMDD-HHMMSS.json.gz`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--output` | string | `""` | Output file path (default: auto-generated in `~/.floop/backups/`) |
| `--no-compress` | bool | `false` | Create V1 uncompressed `.json` backup instead of V2 compressed `.json.gz` |

**Examples:**

```bash
# Backup to default location (V2 compressed)
floop backup

# Backup to a specific file
floop backup --output my-backup.json.gz

# Create uncompressed V1 backup
floop backup --no-compress

# JSON output
floop backup --json
```

**See also:** [backup list](#backup-list), [backup verify](#backup-verify), [restore-backup](#restore-backup)

---

### backup list

List all backups with metadata.

```
floop backup list
```

Lists all backup files in the default backup directory. For V2 files, reads the header line only (no decompression needed) to show node/edge counts. Shows version, format, size, and filename for each backup.

No command-specific flags.

**Examples:**

```bash
# List all backups
floop backup list

# JSON output
floop backup list --json
```

**See also:** [backup](#backup), [backup verify](#backup-verify)

---

### backup verify

Verify backup file integrity.

```
floop backup verify <file>
```

Checks the SHA-256 checksum of a V2 backup file to detect corruption or tampering. For V1 files, reports that integrity checking is not available (V1 has no checksum).

No command-specific flags.

**Examples:**

```bash
# Verify a V2 backup
floop backup verify ~/.floop/backups/floop-backup-20260211-143005.json.gz

# JSON output
floop backup verify backup.json.gz --json
```

**See also:** [backup](#backup), [backup list](#backup-list)

---

### restore-backup

Restore graph state from a backup file.

```
floop restore-backup <file> [flags]
```

Restores the behavior graph from a backup file. Automatically detects V1 (plain JSON) and V2 (compressed) formats. In `merge` mode (default), existing nodes and edges are skipped. In `replace` mode, the store is cleared before restoring.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--mode` | string | `"merge"` | Restore mode: `merge` or `replace` |

**Examples:**

```bash
# Restore a V2 compressed backup
floop restore-backup ~/.floop/backups/floop-backup-20260206-120000.json.gz

# Restore a legacy V1 backup (auto-detected)
floop restore-backup ~/.floop/backups/floop-backup-20260206-120000.json

# Replace entire store from backup
floop restore-backup backup.json.gz --mode replace

# JSON output
floop restore-backup backup.json.gz --json
```

**See also:** [backup](#backup)

---

## Hooks

Commands called by Claude Code hooks for automatic behavior injection, correction detection, and dynamic context. These are native Go subcommands that replace the old shell script approach, enabling Windows support.

### hook

Parent command for native Claude Code hook subcommands.

```
floop hook <subcommand>
```

These subcommands are called directly by Claude Code's hook system. They read JSON input from stdin (provided by Claude Code) and output behavior injection text to stdout.

#### hook session-start

Inject behaviors at the start of a Claude Code session.

```
floop hook session-start
```

Called by `SessionStart` hook. Loads behaviors from both local and global stores, evaluates activation conditions, and outputs tiered markdown for injection into Claude's context.

#### hook first-prompt

Inject behaviors on the first user prompt (with dedup).

```
floop hook first-prompt
```

Called by `UserPromptSubmit` hook. Reads `{"session_id":"..."}` from stdin. Uses atomic directory creation (`os.Mkdir`) for dedup — only injects on the first prompt per session. Same injection logic as `session-start`.

#### hook dynamic-context

Dynamically inject behaviors based on tool usage.

```
floop hook dynamic-context
```

Called by `PreToolUse` hook. Reads `{"tool_name":"...","tool_input":{...},"session_id":"..."}` from stdin. Routes:
- **Read/Edit/Write tools**: Extracts file path → spreading activation for file-relevant behaviors
- **Bash tool**: Detects task type from command (testing, building, committing, etc.) → spreading activation for task-relevant behaviors
- Other tools: silently exits

#### hook detect-correction

Detect corrections in user prompts and capture them as behaviors.

```
floop hook detect-correction
```

Called by `UserPromptSubmit` hook. Reads `{"prompt":"..."}` from stdin. Uses `MightBeCorrection()` heuristic for fast screening, then LLM extraction (with 5s timeout) if available.

**Examples:**

```bash
# These are typically called by Claude Code hooks, not manually:
echo '{}' | floop hook session-start
echo '{"session_id":"abc"}' | floop hook first-prompt
echo '{"tool_name":"Read","tool_input":{"file_path":"main.go"},"session_id":"abc"}' | floop hook dynamic-context
echo '{"prompt":"No, use fmt.Errorf not errors.New"}' | floop hook detect-correction
```

---

### detect-correction

Detect and capture corrections from user text.

```
floop detect-correction [flags]
```

Analyzes user text to detect corrections and automatically capture them. Used by hooks to automatically detect when a user is correcting the agent. Uses the `MightBeCorrection()` heuristic for fast pattern matching, then falls back to LLM extraction if available.

Also accepts JSON input on stdin: `{"prompt":"..."}`.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--prompt` | string | `""` | User prompt text to analyze |
| `--dry-run` | bool | `false` | Detect only, do not capture |

**Examples:**

```bash
# Detect from flag
floop detect-correction --prompt "No, don't use print, use logging instead"

# Detect from stdin (hook usage)
echo '{"prompt":"Actually, prefer pathlib over os.path"}' | floop detect-correction

# Dry run -- detect without capturing
floop detect-correction --prompt "Wrong, use fmt.Errorf not errors.New" --dry-run

# JSON output
floop detect-correction --prompt "No, use context.Background()" --json
```

**See also:** [learn](#learn)

---

### activate

Run spreading activation for dynamic context injection.

```
floop activate [flags]
```

Evaluates the behavior graph using spreading activation and returns new or upgraded behaviors for injection. Respects session state to prevent re-injection spam.

Designed to be called from Claude Code hooks on `PreToolUse` events (`Read`, `Bash`) to dynamically surface relevant behaviors as the agent's work context evolves.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--file` | string | `""` | File path for context |
| `--task` | string | `""` | Task type for context |
| `--format` | string | `"markdown"` | Output format: `markdown`, `json` |
| `--token-budget` | int | `500` | Token budget for this injection |
| `--session-id` | string | `"default"` | Session ID for state tracking |

**Examples:**

```bash
# Activate for a specific file
floop activate --file main.go

# Activate with task context and budget
floop activate --task testing --token-budget 500

# With session tracking, JSON output
floop activate --file main.py --session-id abc123 --json
```

**See also:** [active](#active), [prompt](#prompt)

---

## Server

### mcp-server

Run floop as an MCP (Model Context Protocol) server.

```
floop mcp-server
```

Starts an MCP server that exposes floop functionality over stdio using JSON-RPC 2.0. Allows AI tools (Continue.dev, Cursor, Cline, Windsurf, GitHub Copilot) to invoke floop tools directly.

**Tools:**

| Tool | Description |
|------|-------------|
| `floop_active` | Get active behaviors for current context |
| `floop_learn` | Capture corrections and extract behaviors (auto-classifies scope) |
| `floop_list` | List all behaviors or corrections |
| `floop_deduplicate` | Find and merge duplicate behaviors |
| `floop_backup` | Export full graph state to backup file |
| `floop_restore` | Import graph state from backup (merge or replace) |
| `floop_connect` | Create edge between two behaviors for spreading activation |
| `floop_validate` | Validate behavior graph for consistency issues |
| `floop_feedback` | Provide session feedback on a behavior (confirmed/overridden) |
| `floop_graph` | Render graph in DOT, JSON, or interactive HTML format |
| `floop_pack_install` | Install a skill pack from a `.fpack` file |

**Resources:**

| URI | Description |
|-----|-------------|
| `floop://behaviors/active` | Active behaviors for current context (auto-loaded, 2000-token budget) |
| `floop://behaviors/expand/{id}` | Full details for a specific behavior (resource template) |

No command-specific flags.

**Examples:**

```bash
# Start the MCP server (runs until disconnected)
floop mcp-server

# In Continue.dev config.json:
# {
#   "mcpServers": {
#     "floop": {
#       "command": "floop",
#       "args": ["mcp-server"],
#       "cwd": "${workspaceFolder}"
#     }
#   }
# }
```

**See also:** [MCP server integration guide](integrations/mcp-server.md), [Claude Code integration guide](integrations/claude-code.md)

## Built-in

### completion

Generate shell autocompletion scripts.

```
floop completion [shell]
```

Generates autocompletion scripts for bash, zsh, fish, or powershell. See each sub-command's help for details on how to use the generated script.

**Subcommands:**

| Subcommand | Description |
|------------|-------------|
| `bash` | Generate the autocompletion script for bash |
| `fish` | Generate the autocompletion script for fish |
| `powershell` | Generate the autocompletion script for powershell |
| `zsh` | Generate the autocompletion script for zsh |

**Examples:**

```bash
# Generate bash completions
floop completion bash > /etc/bash_completion.d/floop

# Generate zsh completions
floop completion zsh > "${fpath[1]}/_floop"
```

**See also:** [init](#init)

---

### help

Display help for any command.

```
floop help [command]
```

Provides help text for any floop command, including usage, flags, and description.

**Examples:**

```bash
# Show top-level help
floop help

# Show help for a specific command
floop help learn

# Show help for a subcommand
floop help completion bash
```

**See also:** [--version](#--version)

---

## Command Index

| Command | Category | Description |
|---------|----------|-------------|
| [activate](#activate) | Hooks | Run spreading activation for dynamic context injection |
| [active](#active) | Query | Show behaviors active in current context |
| [backup](#backup) | Backup | Export full graph state to a backup file |
| [completion](#completion) | Built-in | Generate shell autocompletion scripts |
| [config](#config) | Management | Manage floop configuration |
| [connect](#connect) | Graph | Create an edge between two behaviors |
| [deduplicate](#deduplicate) | Management | Find and merge duplicate behaviors |
| [deprecate](#deprecate) | Curation | Mark a behavior as deprecated |
| [detect-correction](#detect-correction) | Hooks | Detect and capture corrections from user text |
| [forget](#forget) | Curation | Soft-delete a behavior from active use |
| [graph](#graph) | Graph | Visualize the behavior graph |
| [help](#help) | Built-in | Display help for any command |
| [hook](#hook) | Hooks | Native Claude Code hook subcommands (session-start, first-prompt, dynamic-context, detect-correction) |
| [init](#init) | Core | Initialize floop with hooks and behavior learning |
| [learn](#learn) | Core | Capture a correction and extract behavior |
| [list](#list) | Query | List behaviors or corrections |
| [merge](#merge) | Curation | Merge two behaviors into one |
| [mcp-server](#mcp-server) | Server | Run floop as an MCP server |
| [pack](#pack) | Skill Packs | Manage skill packs (create, install, list, info, update, remove) |
| [prompt](#prompt) | Query | Generate prompt section from active behaviors |
| [reprocess](#reprocess) | Core | Reprocess orphaned corrections into behaviors |
| [restore](#restore) | Curation | Restore a deprecated or forgotten behavior |
| [restore-backup](#restore-backup) | Backup | Restore graph state from a backup file |
| [show](#show) | Query | Show details of a behavior |
| [stats](#stats) | Token Optimization | Show behavior usage statistics |
| [summarize](#summarize) | Token Optimization | Generate or regenerate summaries for behaviors |
| [tags](#tags) | Graph | Manage behavior tags |
| [upgrade](#upgrade) | Core | Upgrade hook configuration to native Go subcommands |
| [validate](#validate) | Management | Validate the behavior graph for consistency issues |
| [--version](#--version) | Core | Print version information |
| [why](#why) | Query | Explain why a behavior is or isn't active |
