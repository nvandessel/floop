# Floop Usage Guide

> How to use floop for persistent AI agent memory — via MCP (recommended) or CLI.

## Overview

floop captures corrections, extracts reusable behaviors, and activates them in context. It works via two interfaces:
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
| `floop_deduplicate` | Find and merge duplicate behaviors |
| `floop_graph` | Render behavior graph (DOT or JSON) |
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

### MCP Workflow

```
# Agent automatically gets active behaviors via floop_active at session start
# When corrected, agent calls:
floop_learn(wrong="Used print for debugging", right="Use structured logging")

# Agent can check what's active for a specific file:
floop_active(file="internal/store/file.go", task="development")
```

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

# Specify scope (local or global)
floop learn --wrong "..." --right "..." --scope local
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
# View store statistics and token budget
floop stats
floop stats --budget 3000  # Simulate different budget
floop stats --json         # Machine-readable output

# Deduplicate similar behaviors
floop deduplicate --dry-run    # Preview what would merge
floop deduplicate              # Actually merge duplicates

# Validate graph consistency
floop validate

# Backup and restore
floop backup
floop restore-from-backup --input <backup-file>
```

### Graph Operations

```bash
# Visualize the behavior graph
floop graph              # JSON output
floop graph --format dot # Graphviz DOT format

# Connect related behaviors
floop connect <source> <target> --kind similar-to
floop connect <source> <target> --kind requires --weight 0.9
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

# Validate graph consistency
floop validate
```

For the complete command reference, see [CLI_REFERENCE.md](CLI_REFERENCE.md).
