# floop

[![CI](https://github.com/nvandessel/feedback-loop/actions/workflows/ci.yml/badge.svg)](https://github.com/nvandessel/feedback-loop/actions/workflows/ci.yml)
[![Go 1.25+](https://img.shields.io/badge/go-1.25%2B-blue.svg)](https://go.dev/)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**Persistent memory for AI coding agents.**

floop captures corrections you make to AI agents, extracts reusable behaviors, and activates them in the right context — so your agents learn from mistakes and stay consistent across sessions. It uses spreading activation (inspired by how the brain retrieves memories) to build an associative "blast radius" around your current work — not just direct matches, but related behaviors that provide useful context and bolster the AI's understanding.

## Features

- **Learns from corrections** — Tell the agent what it did wrong and what to do instead; floop turns that into a durable behavior
- **Context-aware activation** — Behaviors fire based on file type, task, and semantic relevance — not a static prompt dump
- **Spreading activation** — Graph-based memory retrieval inspired by cognitive science (Collins & Loftus, ACT-R) — triggered behaviors propagate energy to related nodes, pulling in associative context
- **Token-optimized** — Budget-aware assembly keeps injected context within limits
- **Store management** — Stats, deduplication, backup/restore, and graph visualization keep your behavior store healthy
- **MCP server** — Works with any AI tool that supports the Model Context Protocol
- **CLI-first** — Every operation available as a command with `--json` output for agent consumption

## Quick Start

```bash
# Install
go install github.com/nvandessel/feedback-loop/cmd/floop@latest

# Initialize in your project
cd your-project
floop init

# Teach it something
floop learn --wrong "Used fmt.Println for errors" --right "Use log.Fatal or return error"

# See what it learned
floop list

# See what's active for your current context
floop active
```

### Beyond Quick Start

```bash
# Check your behavior store health
floop stats

# Build an activation prompt for your current context
floop prompt --file src/main.go --task development

# Connect related behaviors
floop connect <source-id> <target-id> --kind similar-to

# Deduplicate your behavior store
floop deduplicate --dry-run
```

### Integrate with your AI tool

Add floop as an MCP server so your AI tool loads behaviors automatically.

**Claude Code** (`~/.claude/settings.json`):
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

See [docs/integrations/](docs/integrations/) for setup guides for Cursor, Windsurf, Copilot, and more.

## How It Works

When you correct your AI agent, floop captures the correction and extracts a **behavior** — a reusable rule with context conditions. Behaviors are stored as nodes in a graph, connected by typed edges (similar-to, learned-from, requires, conflicts). When you start a session, floop builds a context snapshot (file types, task, project) and uses **spreading activation** to propagate energy through the graph from seed nodes. This doesn't just retrieve direct matches — energy cascades outward through associations, pulling in related behaviors that provide useful context. The result is a focused but rich set of behaviors tuned to your current work, like the brain activating related memories through associative networks.

## Documentation

- [CLI reference](docs/CLI_REFERENCE.md) — Complete reference for all commands and flags
- [Usage guide](docs/FLOOP_USAGE.md) — How to use floop via MCP or CLI
- [Similarity pipeline](docs/SIMILARITY.md) — How deduplication and similarity detection work
- [Integration guides](docs/integrations/) — Setup for Claude Code, Cursor, Windsurf, and others
- [Research & theory](docs/SCIENCE.md) — The cognitive science behind spreading activation
- [Origin story](docs/LORE.md) — How floop came to be
- [Contributing](CONTRIBUTING.md) — How to contribute
- [Changelog](CHANGELOG.md) — Release history

## Project Status

This is a hobby project I'm building in my free time. It's in early alpha — things work, but the API may change between minor versions and there's plenty left to build. Contributions and feedback are welcome, but please set expectations accordingly.

## License

[Apache License 2.0](LICENSE)
