# floop Integrations

## Overview

floop integrates with AI coding tools through 3 methods:

1. **MCP Server** — Universal protocol. Tool invokes `floop mcp-server` as a subprocess. Bidirectional: read behaviors + capture corrections.
2. **Hooks** — Tool-specific lifecycle hooks (e.g., Claude Code PreToolUse). Auto-inject behaviors at session start. Read-only.
3. **Static Instructions** — Paste `floop prompt` output into tool's instruction files (AGENTS.md, .cursorrules, etc.). Simplest but manual refresh needed.

## Compatibility Matrix

| Tool | MCP | Hooks | Static | Status |
|------|-----|-------|--------|--------|
| Claude Code | Yes | Yes | Yes | **Tested** |
| Cursor | Yes | — | Yes (.cursorrules) | Researched |
| Cline | Yes | Yes | Yes | Researched |
| Continue.dev | Yes | — | — | Researched |
| Windsurf | Yes | — | Yes (.windsurfrules) | Researched |
| GitHub Copilot | Yes | — | Yes (copilot-instructions.md) | Researched |
| Aider | — | — | Yes (wrapper script) | Researched |
| OpenAI Codex CLI | — | — | Yes (Skills/agents.md) | Researched |

## Tested Integrations

### Claude Code

Full guide: [claude-code.md](./claude-code.md)

Claude Code has full bidirectional support. Hooks auto-inject active behaviors at session start via `floop init`, which configures PreToolUse hooks automatically. MCP server mode enables real-time correction capture and behavior queries. Run `floop init` in your project root to auto-detect and configure both.

### MCP Server (Universal)

Full guide: [mcp-server.md](./mcp-server.md)

The MCP server works with any tool that supports the Model Context Protocol. It communicates over stdio using JSON-RPC, exposing tools for querying active behaviors and capturing corrections. See the tool-specific quick start sections below for config snippets.

## Quick Start by Tool

### Cursor

Config: `.cursor/mcp.json`

```json
{
  "mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"],
      "cwd": "${workspaceFolder}"
    }
  }
}
```

Alternative: paste `floop prompt` output into `.cursorrules`

See [mcp-server.md](./mcp-server.md) for protocol details and troubleshooting.

### Cline

Config: Cline panel > Settings > MCP Servers

```json
{
  "mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"],
      "cwd": "${workspaceFolder}"
    }
  }
}
```

Also supports hooks via Cline's custom instructions. You can paste `floop prompt` output into Cline's system prompt settings for static integration.

See [mcp-server.md](./mcp-server.md) for protocol details and troubleshooting.

### Continue.dev

Config: `.continue/config.json`

```json
{
  "mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"],
      "cwd": "${workspaceFolder}",
      "env": {}
    }
  }
}
```

Continue.dev supports MCP in Agent mode. No hooks or static instruction files are available.

See [mcp-server.md](./mcp-server.md) for protocol details and troubleshooting.

### Windsurf

Config: Windsurf MCP settings

```json
{
  "mcp": {
    "servers": {
      "floop": {
        "command": "floop",
        "args": ["mcp-server"],
        "cwd": "${workspaceFolder}"
      }
    }
  }
}
```

Alternative: paste `floop prompt` output into `.windsurfrules`

See [mcp-server.md](./mcp-server.md) for protocol details and troubleshooting.

### GitHub Copilot

Config: `.vscode/settings.json`

```json
{
  "github.copilot.chat.mcpServers": {
    "floop": {
      "command": "floop",
      "args": ["mcp-server"],
      "cwd": "${workspaceFolder}"
    }
  }
}
```

Alternative: add `floop prompt` output to `.github/copilot-instructions.md`

Note: MCP support in Agent mode is in preview.

See [mcp-server.md](./mcp-server.md) for protocol details and troubleshooting.

### Aider

No MCP support. Use a wrapper script:

```bash
#!/bin/bash
BEHAVIORS=$(floop prompt --format markdown)
aider --message-file <(echo "$BEHAVIORS") "$@"
```

Or add `floop prompt` output to `.aider/conventions.md`.

### OpenAI Codex CLI

No MCP support yet. Add `floop prompt` output to `codex/agents.md` (Skills file).

## Generic MCP Configuration

For any MCP-capable tool not listed above:

```json
{
  "floop": {
    "command": "floop",
    "args": ["mcp-server"],
    "cwd": "/path/to/your/project"
  }
}
```

See [mcp-server.md](./mcp-server.md) for protocol details and troubleshooting.

## Contributing a Guide

If you have tested floop with a tool and want to contribute a full integration guide, follow the pattern in [claude-code.md](./claude-code.md). A good guide covers setup, verification, troubleshooting, and best practices. PRs welcome.

---

**floop** — The feedback loop for AI coding agents
